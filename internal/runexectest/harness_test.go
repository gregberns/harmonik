package runexectest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/replay"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

const (
	sentinelTransportError = "twin_transport_error"
	sentinelDisconnected   = "twin_disconnected"
)

type stimCodec struct{}

func (stimCodec) DecodeLine(line []byte) (replay.StimulusStep, bool, error) {
	var s replay.StimulusStep
	if err := json.Unmarshal(line, &s); err != nil {
		return s, false, err
	}
	if s.Kind == "" {
		return s, false, nil // skip: not reactor-relevant
	}
	return s, true, nil
}

func (stimCodec) ErrorEvent(string) replay.StimulusStep {
	return replay.StimulusStep{Kind: sentinelTransportError}
}

func (stimCodec) DisconnectEvent() replay.StimulusStep {
	return replay.StimulusStep{Kind: sentinelDisconnected}
}

func encodeSchedule(t *testing.T, sched replay.Schedule) string {
	t.Helper()
	var b strings.Builder
	for _, st := range sched.Steps {
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("encode step: %v", err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func deliverableEvents(n int, fault substrate.FaultConfig) int {
	switch fault.Mode {
	case substrate.FaultDropAfter:
		return fault.EventN + 1 // events 1..N + the disconnect sentinel
	case substrate.FaultStall:
		return fault.EventN - 1 // blocks before event N
	case substrate.FaultTruncate:
		return fault.EventN // events 1..N-1 + the error sentinel
	case substrate.FaultDup:
		return n + 1 // full stream + the duplicate of event N
	default:
		return n
	}
}

const (
	harnessReadyTimeout  = 30 * time.Second
	harnessInputAck      = 10 * time.Second
	harnessReadyKillReap = 5 * time.Second
	harnessStaleAfter    = 90 * time.Minute
	harnessStepGuard     = 10_000
)

type driveResult struct {
	Delivered     int    // events the twin delivered
	RunDone       bool   // Run machine reached Done
	DoneOutcome   string // "closed" | "reopened"
	Success       bool   // Run terminal success flag
	Reopens       int    // ActReopenBead count
	RunTerminals  int    // ActEmitRunTerminal count
	ReopenReason  string // last ActReopenBead reason
	ResumeInputs  int    // ActDeliverInput with InputResumePrompt
	Elapsed       time.Duration
	EntryForclose bool // the documented FaultStall@1 no-entry shape
}

type shellSim struct {
	t     *testing.T
	clock *substrate.FakeClock
	disp  *runexec.Dispatch
	run   *runexec.Run

	timers map[runexec.TimerKind]time.Time

	completedClass runexec.ModeOutcomeClass // bridge class for DispatchCompleted
	bridged        bool
	steps          int
	res            driveResult
}

func stdDispatchCfg(resumed bool) runexec.DispatchConfig {
	return runexec.DispatchConfig{
		IsResume:         resumed,
		MaxInputAttempts: 2,
		ReadyTimeout:     harnessReadyTimeout,
		InputAck:         harnessInputAck,
		ReadyKillReap:    harnessReadyKillReap,
	}
}

func stdRunCfg(mode string) runexec.RunConfig {
	return runexec.RunConfig{
		Mode:                 mode,
		MaxMergeAttempts:     3,
		EmitOutcome:          true,
		CloseSummary:         "harness-close",
		BrUnavailableSummary: "harness-close-transient",
		NoMergeCloseSummary:  "harness-no-merge-close",
		ReopenReason:         "harness-reopen",
	}
}

func completedBridgeClass(sum replay.RunSummary) runexec.ModeOutcomeClass {
	switch sum.TerminalType {
	case "run_completed", "review_loop_cycle_complete":
		return runexec.ModeSuccess
	default: // run_failed and friends
		return runexec.ModeFailure
	}
}

func newShellSim(t *testing.T, clock *substrate.FakeClock, sum replay.RunSummary) *shellSim {
	t.Helper()
	h := &shellSim{
		t:              t,
		clock:          clock,
		disp:           runexec.NewDispatch(stdDispatchCfg(sum.Resumed)),
		run:            runexec.NewRun(stdRunCfg(sum.Mode)),
		timers:         map[runexec.TimerKind]time.Time{},
		completedClass: completedBridgeClass(sum),
	}
	h.feedRun(runexec.Event{Kind: runexec.EvStartRun, Mode: sum.Mode, At: clock.Now()})
	return h
}

func drive(t *testing.T, clock *substrate.FakeClock, sum replay.RunSummary, fault substrate.FaultConfig) driveResult {
	t.Helper()
	sched := replay.SynthesizeSchedule(sum)
	h := newShellSim(t, clock, sum)
	start := clock.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tw := substrate.NewTwin[replay.StimulusStep](
		strings.NewReader(encodeSchedule(t, sched)), fault, stimCodec{})
	ch := tw.Events(ctx)

	want := deliverableEvents(len(sched.Steps), fault)
	for i := 0; i < want; i++ {
		step, ok := <-ch
		if !ok {
			t.Fatalf("twin closed early: got %d of %d deliverable events", i, want)
		}
		h.res.Delivered++
		h.feedStep(step)
		if !h.run.InFlight() {
			break
		}
	}
	cancel() // release a stalled twin goroutine

	h.pumpToTerminal()
	h.res.Elapsed = clock.Since(start)
	h.res.RunDone = !h.run.InFlight()
	st := h.run.State()
	h.res.DoneOutcome = st.DoneOutcome
	h.res.Success = st.Success
	return h.res
}

func (h *shellSim) feedStep(step replay.StimulusStep) {
	h.advance(time.Duration(step.DelayMs) * time.Millisecond)
	if !h.run.InFlight() {
		return
	}
	switch step.Kind {
	case sentinelTransportError, sentinelDisconnected:
		h.feedDisp(runexec.Event{Kind: runexec.EvAborted, Reason: step.Kind, At: h.clock.Now()})
	case string(runexec.EvTimerFired):
		h.fireEarliest()
	default:
		ev := runexec.Event{Kind: runexec.EventKind(step.Kind), At: h.clock.Now(), Session: "s1"}
		h.feedDisp(ev)
		h.feedRunKind(ev)
	}
}

var runKinds = map[runexec.EventKind]bool{
	runexec.EvAgentCompleted: true,
	runexec.EvCleanExit:      true,
}

func (h *shellSim) feedRunKind(ev runexec.Event) {
	if runKinds[ev.Kind] {
		h.feedRun(ev)
	}
}

func (h *shellSim) advance(d time.Duration) {
	target := h.clock.Now().Add(d)
	for {
		kind, deadline, ok := h.earliestDeadline()
		if !ok || deadline.After(target) {
			break
		}
		h.clock.Advance(deadline.Sub(h.clock.Now()))
		h.fireDeadline(kind)
		if !h.run.InFlight() {
			return
		}
	}
	h.clock.Advance(target.Sub(h.clock.Now()))
}

const watchdogKind runexec.TimerKind = "harness_stale_watchdog"

func (h *shellSim) earliestDeadline() (runexec.TimerKind, time.Time, bool) {
	var bestKind runexec.TimerKind
	var best time.Time
	found := false
	consider := func(k runexec.TimerKind, at time.Time) {
		if !found || at.Before(best) {
			bestKind, best, found = k, at, true
		}
	}
	for k, at := range h.timers {
		consider(k, at)
	}
	if ds := h.disp.State(); ds.Phase == runexec.DispatchWorking {
		consider(watchdogKind, ds.LastProgressAt.Add(harnessStaleAfter))
	}
	return bestKind, best, found
}

func (h *shellSim) fireDeadline(kind runexec.TimerKind) {
	if kind == watchdogKind {
		h.feedDisp(runexec.Event{Kind: runexec.EvHeartbeatStale, At: h.clock.Now()})
		return
	}
	delete(h.timers, kind)
	h.feedDisp(runexec.Event{Kind: runexec.EvTimerFired, Timer: kind, At: h.clock.Now()})
}

func (h *shellSim) fireEarliest() {
	kind, deadline, ok := h.earliestDeadline()
	if !ok {
		return
	}
	h.clock.Advance(deadline.Sub(h.clock.Now()))
	h.fireDeadline(kind)
}

func (h *shellSim) pumpToTerminal() {
	for h.run.InFlight() {
		h.guardStep()
		_, _, ok := h.earliestDeadline()
		if !ok {
			if h.res.Delivered == 0 && h.disp.State().Phase == runexec.DispatchIdle {
				h.res.EntryForclose = true
				return
			}
			h.t.Fatalf("SILENCE: run in flight (run=%s dispatch=%s) with no armed deadline (RSM-INV-001/002)",
				h.run.State().Phase, h.disp.State().Phase)
		}
		h.fireEarliest()
	}
}

func (h *shellSim) feedDisp(ev runexec.Event) {
	h.guardStep()
	h.execActions(h.disp.Step(ev))
	h.bridgeDispatchTerminal(ev.At)
}

var dispatchTerminals = map[runexec.DispatchPhase]bool{
	runexec.DispatchCompleted: true,
	runexec.DispatchExited:    true,
	runexec.DispatchStalled:   true,
	runexec.DispatchFailed:    true,
	runexec.DispatchAborted:   true,
}

func (h *shellSim) bridgeDispatchTerminal(at time.Time) {
	st := h.disp.State()
	if h.bridged || !dispatchTerminals[st.Phase] {
		return
	}
	h.bridged = true
	class := runexec.ModeFailure
	if st.Phase == runexec.DispatchCompleted {
		class = h.completedClass
	}
	h.feedRun(runexec.Event{
		Kind: runexec.EvModeOutcome, ModeOutcome: class,
		Reason: st.Reason, At: at,
	})
}

func (h *shellSim) feedRun(ev runexec.Event) {
	h.guardStep()
	h.execActions(h.run.Step(ev))
}

func (h *shellSim) execActions(actions []runexec.Action) {
	for _, a := range actions {
		h.execAction(a)
	}
}

func (h *shellSim) execAction(a runexec.Action) {
	now := h.clock.Now()
	switch a.Kind {
	case runexec.ActArmTimer:
		h.timers[a.Timer] = now.Add(a.D)
	case runexec.ActCancelTimer:
		delete(h.timers, a.Timer)
	case runexec.ActCreateWorktree:
		h.feedRun(runexec.Event{Kind: runexec.EvProvisioned, At: now})
	case runexec.ActCheckEscape:
		h.feedRun(runexec.Event{Kind: runexec.EvGuardsPassed, At: now})
	case runexec.ActRunGate:
		h.feedRun(runexec.Event{Kind: runexec.EvGatePassed, At: now})
	case runexec.ActPrepareMerge, runexec.ActSubmitMerge:
		h.feedRun(runexec.Event{Kind: runexec.EvMergeResult, Merge: runexec.MergeSuccess, At: now})
	case runexec.ActCloseBead:
		h.feedRun(runexec.Event{Kind: runexec.EvCloseResult, Close: runexec.CloseClosed, At: now})
	case runexec.ActReopenBead:
		h.res.Reopens++
		h.res.ReopenReason = a.Reason
	case runexec.ActEmitRunTerminal:
		h.res.RunTerminals++
	case runexec.ActDeliverInput:
		if a.InputKind == runexec.InputResumePrompt {
			h.res.ResumeInputs++
		}
	default:
	}
}

func (h *shellSim) guardStep() {
	h.steps++
	if h.steps > harnessStepGuard {
		h.t.Fatalf("livelock: step guard (%d) exceeded", harnessStepGuard)
	}
}

func loadSummaries(t *testing.T) []replay.RunSummary {
	t.Helper()
	return loadSummariesFrom(t, corpusRunsDir())
}
