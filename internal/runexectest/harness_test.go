package runexectest_test

// harness_test.go — the shell-sim drive harness for the RT11 fault matrix and
// the N=10 relaunch oracle. It is the runexec peer of keepertest's discrete
// runner: a substrate.Twin replays a synthesized dispatch schedule (NDJSON of
// replay.StimulusStep) into the PURE Dispatch + Run reactors while the harness
// plays runshell's part — arming/cancelling virtual timers from the machines'
// own ArmTimer/CancelTimer actions, answering Run port actions with clean
// substrate results, modelling the shell-side frozen commit watchdog (M3-D3),
// and bridging a Dispatch terminal into the Run machine's mode outcome.
//
// ALL time is virtual (substrate.FakeClock): the harness never sleeps. A
// stalled stream is handled deterministically — the harness knows the fault
// config it injected, so it reads exactly the deliverable prefix and then
// pumps armed virtual deadlines until the Run machine terminates. "In flight
// with nothing armed" is converted into an explicit SILENCE test failure
// (RSM-INV-001/002: silence is forbidden), except the documented
// entry-foreclosed FaultStall@1 shape (keeper T12 precedent), which asserts
// the no-entry shape instead.

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

// Sentinel kinds the codec returns for the twin's synthetic fault events. The
// shell classifies a transport failure of the dispatch source as an abort of
// the session (the reactors' vocabulary has EvAborted for exactly this).
const (
	sentinelTransportError = "twin_transport_error"
	sentinelDisconnected   = "twin_disconnected"
)

// stimCodec decodes NDJSON replay.StimulusStep lines for substrate.Twin.
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

// encodeSchedule renders a synthesized schedule as the Twin's NDJSON corpus.
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

// deliverableEvents is the number of events the Twin will emit for a schedule
// of n steps under the given fault (substrate/replay.go semantics): the
// harness reads exactly this many, so stall detection is deterministic — no
// wall-clock idle timers.
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

// Harness virtual-time policy scalars. staleAfter models the shell-side frozen
// commit watchdog window (M3-D3: today's 90m watchdog ceiling).
const (
	harnessReadyTimeout  = 30 * time.Second
	harnessInputAck      = 10 * time.Second
	harnessReadyKillReap = 5 * time.Second
	harnessStaleAfter    = 90 * time.Minute
	harnessStepGuard     = 10_000
)

// driveResult is one cell's observed outcome.
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

// shellSim is the per-cell harness state.
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

// stdDispatchCfg returns the harness Dispatch policy.
func stdDispatchCfg(resumed bool) runexec.DispatchConfig {
	return runexec.DispatchConfig{
		IsResume:         resumed,
		MaxInputAttempts: 2,
		ReadyTimeout:     harnessReadyTimeout,
		InputAck:         harnessInputAck,
		ReadyKillReap:    harnessReadyKillReap,
	}
}

// stdRunCfg returns the harness Run policy (observable strings are harness
// templates; the matrix asserts presence, not production byte-parity — that is
// L1's job).
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

// completedBridgeClass maps a summary's recorded terminal to the mode-outcome
// class the shell would report when the dispatch COMPLETES cleanly.
func completedBridgeClass(sum replay.RunSummary) runexec.ModeOutcomeClass {
	switch sum.TerminalType {
	case "run_completed", "review_loop_cycle_complete":
		return runexec.ModeSuccess
	default: // run_failed and friends
		return runexec.ModeFailure
	}
}

// newShellSim builds the per-cell machines on the shared clock and runs the
// Run-machine prologue (EvStartRun → provisioned → Dispatching).
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

// drive replays one schedule under one fault config through the machines and
// returns the observed cell result. It never sleeps: it reads exactly the
// deliverable prefix, then pumps virtual deadlines to a terminal.
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

// feedStep advances virtual time for the step's delay (firing any deadline
// that falls due first), then routes the stimulus into the machines.
func (h *shellSim) feedStep(step replay.StimulusStep) {
	h.advance(time.Duration(step.DelayMs) * time.Millisecond)
	if !h.run.InFlight() {
		return
	}
	switch step.Kind {
	case sentinelTransportError, sentinelDisconnected:
		// The shell classifies a dead dispatch source as a session abort.
		h.feedDisp(runexec.Event{Kind: runexec.EvAborted, Reason: step.Kind, At: h.clock.Now()})
	case string(runexec.EvTimerFired):
		// "The pending reactor timer fires next": jump to the earliest armed
		// deadline. With nothing armed the stimulus is inert (the pump phase
		// still enforces never-silence).
		h.fireEarliest()
	default:
		ev := runexec.Event{Kind: runexec.EventKind(step.Kind), At: h.clock.Now(), Session: "s1"}
		h.feedDisp(ev)
		h.feedRunKind(ev)
	}
}

// runKinds is the set of stimulus kinds the Run machine consumes directly.
var runKinds = map[runexec.EventKind]bool{
	runexec.EvAgentCompleted: true,
	runexec.EvCleanExit:      true,
}

// feedRunKind forwards a stimulus to the Run machine when it is Run-consumed.
func (h *shellSim) feedRunKind(ev runexec.Event) {
	if runKinds[ev.Kind] {
		h.feedRun(ev)
	}
}

// advance moves virtual time by d, firing every armed deadline that falls in
// the interval, in deadline order (deterministic interleaving).
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

// watchdogKind is the harness-local pseudo-timer for the shell-side frozen
// commit watchdog (M3-D3): a Working session with no agent progress for
// staleAfter is fed EvHeartbeatStale by the shell, not by a reactor timer.
const watchdogKind runexec.TimerKind = "harness_stale_watchdog"

// earliestDeadline returns the soonest armed deadline: reactor timers plus the
// shell watchdog when the session is Working.
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

// fireDeadline feeds the event for one due deadline.
func (h *shellSim) fireDeadline(kind runexec.TimerKind) {
	if kind == watchdogKind {
		h.feedDisp(runexec.Event{Kind: runexec.EvHeartbeatStale, At: h.clock.Now()})
		return
	}
	delete(h.timers, kind)
	h.feedDisp(runexec.Event{Kind: runexec.EvTimerFired, Timer: kind, At: h.clock.Now()})
}

// fireEarliest jumps virtual time to the earliest armed deadline and fires it.
func (h *shellSim) fireEarliest() {
	kind, deadline, ok := h.earliestDeadline()
	if !ok {
		return
	}
	h.clock.Advance(deadline.Sub(h.clock.Now()))
	h.fireDeadline(kind)
}

// pumpToTerminal drives the machines to the Run terminal after the stream has
// ended or stalled, by firing armed virtual deadlines in order. In-flight with
// NOTHING armed is the forbidden silence — an explicit failure — except the
// documented entry-foreclosed shape (FaultStall@1: no stimulus ever arrived,
// the dispatch never entered, so there is no session to bound; keeper T12
// precedent).
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

// feedDisp steps the Dispatch machine, executes its actions, and bridges a
// terminal into the Run machine's mode outcome exactly once.
func (h *shellSim) feedDisp(ev runexec.Event) {
	h.guardStep()
	h.execActions(h.disp.Step(ev))
	h.bridgeDispatchTerminal(ev.At)
}

// dispatchTerminals mirrors runexec's terminal set (RSM-003) for the bridge.
var dispatchTerminals = map[runexec.DispatchPhase]bool{
	runexec.DispatchCompleted: true,
	runexec.DispatchExited:    true,
	runexec.DispatchStalled:   true,
	runexec.DispatchFailed:    true,
	runexec.DispatchAborted:   true,
}

// bridgeDispatchTerminal maps the Dispatch terminal to the shell's mode
// outcome: Completed uses the summary-derived class; every failure terminal is
// a ModeFailure (the fail-closed reopen route, RSM-025).
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

// feedRun steps the Run machine and executes its actions.
func (h *shellSim) feedRun(ev runexec.Event) {
	h.guardStep()
	h.execActions(h.run.Step(ev))
}

// execActions plays the effector: timers become virtual deadlines; Run port
// actions get clean substrate answers; emissions are counted.
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
		// ActLaunchAgent / ActKillAgent / ActEmit / ActReAmendTrailer /
		// ActDriveLifecycleTerminated: recorded implicitly; no reply needed.
	}
}

// guardStep converts a livelock into an explicit failure.
func (h *shellSim) guardStep() {
	h.steps++
	if h.steps > harnessStepGuard {
		h.t.Fatalf("livelock: step guard (%d) exceeded", harnessStepGuard)
	}
}

// loadSummaries reads every corpus summary in the pinned baseline, keyed by
// stratum, in stable order.
func loadSummaries(t *testing.T) []replay.RunSummary {
	t.Helper()
	return loadSummariesFrom(t, corpusRunsDir())
}
