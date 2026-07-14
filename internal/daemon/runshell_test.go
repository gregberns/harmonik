package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// runshell_test.go — the RT7 FakeClock drive-loop conformance test. It exercises
// the shell composition root end-to-end in virtual time (RSM-INV-002, RSM-025):
// a Dispatch that never becomes ready rides the SR9 ready-timeout edge (kill +
// agent_ready_timeout emit + Failed), and the run's failure maps onto the Run
// machine's reopen spine (reopen + failed run terminal). No wall-clock sleeps.

// recordingEffectors captures which effector arms fired, for assertions.
type recordingEffectors struct {
	mu               sync.Mutex
	launched         bool
	killed           bool
	reopened         bool
	emitted          []core.EventType
	terminalSuccess  *bool
	terminalCalled   bool
	reopenReasonSeen string
}

func (r *recordingEffectors) bundle() runEffectors {
	return runEffectors{
		launchAgent: func(_ context.Context, _ runexec.SessionRef, _ string) {
			r.mu.Lock()
			r.launched = true
			r.mu.Unlock()
		},
		killAgent: func(_ context.Context, _ runexec.SessionRef) {
			r.mu.Lock()
			r.killed = true
			r.mu.Unlock()
		},
		emit: func(_ context.Context, typ core.EventType, _ string) {
			r.mu.Lock()
			r.emitted = append(r.emitted, typ)
			r.mu.Unlock()
		},
		createWorktree: func(_ context.Context) []runexec.Event {
			return []runexec.Event{{Kind: runexec.EvProvisioned}}
		},
		reopenBead: func(_ context.Context, reason string) {
			r.mu.Lock()
			r.reopened = true
			r.reopenReasonSeen = reason
			r.mu.Unlock()
		},
		emitRunTerminal: func(_ context.Context, success bool, _ string) {
			r.mu.Lock()
			r.terminalCalled = true
			s := success
			r.terminalSuccess = &s
			r.mu.Unlock()
		},
	}
}

func (r *recordingEffectors) sawEmit(t core.EventType) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.emitted {
		if e == t {
			return true
		}
	}
	return false
}

// TestRunShell_ReadyTimeout_KillsAndFails drives the Dispatch machine to the SR9
// ready-timeout terminal purely by advancing the FakeClock past the composed
// deadlines — no EvLaunched, no EvAgentReady ever arrive. Asserts: the agent is
// killed, agent_ready_timeout is emitted, and the dispatch settles into Failed
// with the agent_ready_timeout reason (never a silent wait).
func TestRunShell_ReadyTimeout_KillsAndFails(t *testing.T) {
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	rec := &recordingEffectors{}
	events := make(chan runexec.Event) // never delivers: agent never signals
	sh := newRunShell(clock, rec.bundle(), events)

	cfg := runexec.DispatchConfig{
		ReadyTimeout:  30 * time.Second,
		ReadyKillReap: 10 * time.Second,
	}

	result := make(chan runexec.DispatchState, 1)
	go func() {
		result <- sh.RunDispatch(context.Background(), cfg, "sess-1", "spec-ref")
	}()

	// Pump virtual time until the dispatch reaches its terminal. Each drive
	// generation arms exactly one deadline ticker; advancing past the largest
	// composed deadline fires whichever timer is live, converging in two edges
	// (agent_ready → ready_kill_reap).
	final := pumpUntilDone(t, clock, result)

	if final.Phase != runexec.DispatchFailed {
		t.Fatalf("phase = %q, want failed", final.Phase)
	}
	if final.Reason != "agent_ready_timeout" {
		t.Fatalf("reason = %q, want agent_ready_timeout", final.Reason)
	}
	if !rec.launched {
		t.Error("launchAgent effector never fired")
	}
	if !rec.killed {
		t.Error("killAgent effector never fired on the ready-timeout edge")
	}
	if !rec.sawEmit(core.EventTypeAgentReadyTimeout) {
		t.Error("agent_ready_timeout was not emitted (silent wait — RSM-INV-002 breach)")
	}
}

// pumpUntilDone advances the FakeClock in small virtual steps, yielding to the
// reactor goroutine between advances so it can arm its deadline ticker before
// the next advance crosses it. Small steps (1s) never leapfrog a deadline
// (≥10s) armed at now+d, so every timer edge fires deterministically; the real
// yield keeps the test converging without racing BlockUntil against a
// goroutine that has already terminated.
func pumpUntilDone(t *testing.T, clock *substrate.FakeClock, result <-chan runexec.DispatchState) runexec.DispatchState {
	t.Helper()
	for i := 0; i < 500; i++ {
		select {
		case st := <-result:
			return st
		case <-time.After(2 * time.Millisecond): // yield: let the goroutine arm/process
		}
		clock.Advance(time.Second)
	}
	t.Fatal("dispatch never reached a terminal (drive loop hung)")
	return runexec.DispatchState{}
}

// TestRunShell_Run_MapsFailureToReopen drives the Run machine through the reopen
// spine: a mode-failure outcome (the shell's mapping of a failed single-mode
// dispatch / review-loop failure) MUST reopen the bead and emit a failed run
// terminal (RSM-009/019/022), landing Done{reopened}.
func TestRunShell_Run_MapsFailureToReopen(t *testing.T) {
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	rec := &recordingEffectors{}
	events := make(chan runexec.Event, 1)
	// The sub-driver return (a failed dispatch mapped to a mode failure) arrives
	// on the tap after provisioning.
	events <- runexec.Event{Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeFailure}

	eff := rec.bundle()
	sh := newRunShell(clock, eff, events)

	m := runexec.NewRun(runexec.RunConfig{
		Mode:         "review_loop",
		ReopenReason: "review_loop_failed",
	})
	final := sh.driveRun(context.Background(), m, "review_loop")

	if final.Phase != runexec.RunDone {
		t.Fatalf("phase = %q, want done", final.Phase)
	}
	if final.DoneOutcome != "reopened" {
		t.Fatalf("outcome = %q, want reopened", final.DoneOutcome)
	}
	if final.Success {
		t.Error("reopened run reported Success=true")
	}
	if !rec.reopened {
		t.Error("reopenBead effector never fired")
	}
	if rec.reopenReasonSeen != "review_loop_failed" {
		t.Errorf("reopen reason = %q, want review_loop_failed", rec.reopenReasonSeen)
	}
	if !rec.terminalCalled || rec.terminalSuccess == nil || *rec.terminalSuccess {
		t.Error("failed run terminal not emitted with success=false")
	}
	if !rec.sawEmit(core.EventTypeRunStarted) {
		t.Error("run_started not emitted at provisioning")
	}
}
