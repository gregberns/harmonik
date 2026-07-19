package daemon

// dispatchsegment_test.go — RT8 FakeClock conformance tests for the
// launch/ready/brief dispatch segment (dispatchsegment.go), the census
// fault-injection seed (RSM-030): a resume whose agent stalls on relaunch
// (no readiness signal is ever recognized) MUST ride the SR9 ready-timeout
// edge — kill + agent_ready_timeout + a Failed terminal — within the
// virtual-time ready bound, and that failure MUST map onto the Run machine's
// reopen spine. Never silence (RSM-INV-001/002, RSM-024/025).
//
// All tests are pure virtual time (substrate.FakeClock + the runshell_test.go
// pumpUntilDone pump) — the determinism the deleted wall-clock waitAgentReady
// + resume-grace caulk could never offer.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// segRecordingEmitter is a minimal handlercontract.EventEmitter that records
// every emission, standing in for the sealed bus under the perRunEventTap.
type segRecordingEmitter struct {
	mu    sync.Mutex
	calls []segEmitCall
}

type segEmitCall struct {
	typ     core.EventType
	runID   *core.RunID
	withRun bool
}

func (e *segRecordingEmitter) Emit(_ context.Context, typ core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, segEmitCall{typ: typ})
	return nil
}

func (e *segRecordingEmitter) EmitWithRunID(_ context.Context, runID core.RunID, typ core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := runID
	e.calls = append(e.calls, segEmitCall{typ: typ, runID: &r, withRun: true})
	return nil
}

func (e *segRecordingEmitter) readyEmit() (found, withRun bool, runID *core.RunID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.calls {
		if c.typ == core.EventTypeAgentReady {
			return true, c.withRun, c.runID
		}
	}
	return false, false, nil
}

// segStubAdapter satisfies handlercontract.Adapter for the segment's ready
// pump: only DetectReady is ever invoked, so the embedded nil interface backs
// the remaining methods (a call would panic, proving the segment stayed inside
// its contract).
type segStubAdapter struct {
	handlercontract.Adapter
	ready func(core.EventEnvelope) bool
}

func (a segStubAdapter) DetectReady(env core.EventEnvelope) bool { return a.ready(env) }

func segTestRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// runStalledResumeSegment drives one resume-shaped segment (tmux path: no
// watcher; transitional probe armed) whose agent never yields a recognized
// readiness signal, in pure virtual time, and returns the terminal state, the
// virtual elapsed duration, and the recorded side effects.
func runStalledResumeSegment(t *testing.T, cfg runexec.DispatchConfig, emitReadyTimeout bool) (final runexec.DispatchState, elapsed time.Duration, rec *segRecordingEmitter, killed, timeoutEmitted *bool) {
	t.Helper()
	start := time.Unix(0, 0)
	clock := substrate.NewFakeClock(start)
	runID := segTestRunID(t)
	rec = &segRecordingEmitter{}
	tap, tapCh := newPerRunEventTap(rec, runID)

	var killedVal, timeoutEmittedVal bool
	killed, timeoutEmitted = &killedVal, &timeoutEmittedVal
	seg := &dispatchSegment{
		clock: clock,
		runID: runID,
		cfg:   cfg,
		// Stalled relaunch (the census fault): DetectReady never fires — not
		// even for the probe's synthetic agent_ready — modeling an agent whose
		// readiness signal never materializes after resume.
		adapter:     segStubAdapter{ready: func(core.EventEnvelope) bool { return false }},
		probeResume: true,
		tap:         tap,
		tapCh:       tapCh,
		launch:      func(context.Context) (<-chan struct{}, error) { return nil, nil },
		killReady:   func(context.Context) { killedVal = true },
	}
	if emitReadyTimeout {
		seg.emitReadyTimeout = func(context.Context) { timeoutEmittedVal = true }
	}

	result := make(chan runexec.DispatchState, 1)
	go func() { result <- seg.run(context.Background()) }()
	final = pumpUntilDone(t, clock, result)
	return final, clock.Now().Sub(start), rec, killed, timeoutEmitted
}

// TestDispatchSegment_ResumeStalled_TimeoutThenReopenWithinBound is the census
// fault-injection seed for the builtin review-loop resume: no ready signal ⇒
// agent_ready_timeout + bead reopen, both within the composed virtual-time
// bound (RSM-024's ready sub-bound + the kill-reap window) — never silence.
func TestDispatchSegment_ResumeStalled_TimeoutThenReopenWithinBound(t *testing.T) {
	cfg := runexec.DispatchConfig{
		IsResume:         true,
		MaxInputAttempts: 1,
		ReadyTimeout:     30 * time.Second,
		InputAck:         dispatchSegmentInputAckWindow,
		ReadyKillReap:    10 * time.Second,
	}
	final, elapsed, rec, killed, timeoutEmitted := runStalledResumeSegment(t, cfg, true)

	if final.Phase != runexec.DispatchFailed {
		t.Fatalf("phase = %q, want failed", final.Phase)
	}
	if final.Reason != "agent_ready_timeout" {
		t.Fatalf("reason = %q, want agent_ready_timeout", final.Reason)
	}
	if !*killed {
		t.Error("killReady hook never fired on the ready-timeout edge")
	}
	if !*timeoutEmitted {
		t.Error("agent_ready_timeout was not emitted (silent wait — RSM-INV-002 breach)")
	}
	// The bound (RSM-024): the resume settles its failure terminal within the
	// ready sub-bound + the kill-reap window. The pump advances 1 virtual
	// second per step, so allow one step of slack.
	if bound := cfg.ReadyTimeout + cfg.ReadyKillReap + time.Second; elapsed > bound {
		t.Errorf("terminal took %v of virtual time, want ≤ %v (RSM-024 bound breach)", elapsed, bound)
	}
	// The probe DID fire (run_id-stamped, M3-D7) — the stall is the agent's,
	// not the probe's — and the machine still failed closed.
	if found, withRun, _ := rec.readyEmit(); !found || !withRun {
		t.Errorf("transitional probe emit: found=%v withRunID=%v, want a run_id-stamped agent_ready", found, withRun)
	}

	// Reopen half (RSM-025 fail-closed): the sub-driver maps the failed
	// dispatch onto EvModeOutcome{failure}; the Run machine MUST reopen the
	// bead and emit a failed run terminal — the segment's timeout composes
	// into the run-level reopen within the same virtual clock.
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	rrec := &recordingEffectors{}
	events := make(chan runexec.Event, 1)
	events <- runexec.Event{Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeFailure, Reason: "agent_ready_timeout"}
	sh := newRunShell(clock, rrec.bundle(), events)
	m := runexec.NewRun(runexec.RunConfig{Mode: "review_loop", ReopenReason: "review_loop_failed"})
	runFinal := sh.driveRun(context.Background(), m, "review_loop")
	if runFinal.Phase != runexec.RunDone || runFinal.DoneOutcome != "reopened" {
		t.Fatalf("run terminal = %q/%q, want done/reopened", runFinal.Phase, runFinal.DoneOutcome)
	}
	if !rrec.reopened {
		t.Error("reopenBead effector never fired after the ready-timeout dispatch failure")
	}
}

// TestDispatchSegment_DotResume_ReadyTimeoutEdge proves the DOT back-edge
// resume rides the SAME structural edge (M3-D7: "DOT gains the bound"): the
// segment configuration dispatchDotAgenticNode builds for an
// implementer-resume node (IsResume + probe + emitAgentReadyTimeout hook)
// reaches Failed(agent_ready_timeout) within the virtual bound. Pre-RT8 the
// DOT resume had no resume-ready accommodation and its bound lived in a
// wall-clock time.After; both now come from the machine's TimerAgentReady.
func TestDispatchSegment_DotResume_ReadyTimeoutEdge(t *testing.T) {
	cfg := runexec.DispatchConfig{
		IsResume:         true, // phase == ReviewLoopPhaseImplementerResume (dot_cascade.go)
		MaxInputAttempts: 1,
		ReadyTimeout:     45 * time.Second,
		InputAck:         dispatchSegmentInputAckWindow,
		ReadyKillReap:    10 * time.Second,
	}
	final, elapsed, _, killed, timeoutEmitted := runStalledResumeSegment(t, cfg, true)

	if final.Phase != runexec.DispatchFailed || final.Reason != "agent_ready_timeout" {
		t.Fatalf("terminal = %q/%q, want failed/agent_ready_timeout", final.Phase, final.Reason)
	}
	if !*killed || !*timeoutEmitted {
		t.Errorf("killed=%v timeoutEmitted=%v, want both true (RSM-005 outgoing action set)", *killed, *timeoutEmitted)
	}
	if bound := cfg.ReadyTimeout + cfg.ReadyKillReap + time.Second; elapsed > bound {
		t.Errorf("DOT resume terminal took %v of virtual time, want ≤ %v", elapsed, bound)
	}
}

// TestDispatchSegment_ResumeProbe_RunIDStampedReadyDelivers proves the happy
// resume path: with no relay signal, the transitional probe's run_id-stamped
// agent_ready (M3-D7; the RSM-029-allowlisted attribution divergence) is
// recognized, the brief is delivered on the machine's post-ready edge, and
// the segment settles into Working long before the ready bound.
func TestDispatchSegment_ResumeProbe_RunIDStampedReadyDelivers(t *testing.T) {
	start := time.Unix(0, 0)
	clock := substrate.NewFakeClock(start)
	runID := segTestRunID(t)
	rec := &segRecordingEmitter{}
	tap, tapCh := newPerRunEventTap(rec, runID)

	delivered := false
	seg := &dispatchSegment{
		clock: clock,
		runID: runID,
		cfg: runexec.DispatchConfig{
			IsResume:         true,
			MaxInputAttempts: 1,
			ReadyTimeout:     30 * time.Second,
			InputAck:         dispatchSegmentInputAckWindow,
			ReadyKillReap:    10 * time.Second,
		},
		adapter: segStubAdapter{ready: func(env core.EventEnvelope) bool {
			return env.Type == string(core.EventTypeAgentReady)
		}},
		probeResume: true,
		tap:         tap,
		tapCh:       tapCh,
		launch:      func(context.Context) (<-chan struct{}, error) { return nil, nil },
		deliver:     func(context.Context) { delivered = true },
		killReady:   func(context.Context) { t.Error("killReady fired on the happy resume path") },
	}

	result := make(chan runexec.DispatchState, 1)
	go func() { result <- seg.run(context.Background()) }()
	final := pumpUntilDone(t, clock, result)

	if final.Phase != runexec.DispatchWorking {
		t.Fatalf("phase = %q, want working", final.Phase)
	}
	if !delivered {
		t.Error("deliver hook never fired after the probe's agent_ready")
	}
	found, withRun, gotRunID := rec.readyEmit()
	if !found || !withRun || gotRunID == nil || *gotRunID != runID {
		t.Errorf("probe agent_ready: found=%v withRunID=%v runID=%v, want run_id-stamped with %v",
			found, withRun, gotRunID, runID)
	}
	if elapsed := clock.Now().Sub(start); elapsed >= 30*time.Second {
		t.Errorf("probe path took %v (the full ready bound) — probe never won", elapsed)
	}
}
