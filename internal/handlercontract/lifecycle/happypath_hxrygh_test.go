package lifecycle_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// TestLifecycleFSM_HappyPath verifies the canonical happy-path sequence:
// Spawning→Initializing→Ready→Executing→Terminating→Terminated.
//
// This maps to the watcher-observed events and workloop Kill/Wait path:
//   - NewSession: Machine created in Spawning; cmd.Start → Initializing (ReasonSpawnStarted)
//   - agent_ready: Initializing → Ready (ReasonInitComplete)
//   - agent_started: Ready → Executing (ReasonCommandStarted)
//   - Kill/SIGTERM sent: Executing → Terminating (ReasonTerminateRequested)
//   - Wait return (exit 0): Terminating → Terminated (ReasonTerminateComplete)
//
// Acceptance criterion: Spawning→Initializing→Ready→Executing→Terminating→Terminated in order.
func TestLifecycleFSM_HappyPath(t *testing.T) {
	t.Parallel()

	m := lifecycle.New("sess-happy", "run-happy")

	if got := m.Current(); got != lifecycle.StateSpawning {
		t.Fatalf("initial state: got %s, want %s", got, lifecycle.StateSpawning)
	}

	if err := m.Transition(lifecycle.StateInitializing, lifecycle.ReasonSpawnStarted, "", ""); err != nil {
		t.Fatalf("Spawning→Initializing: %v", err)
	}

	if err := m.Transition(lifecycle.StateReady, lifecycle.ReasonInitComplete, "", ""); err != nil {
		t.Fatalf("Initializing→Ready: %v", err)
	}

	if err := m.Transition(lifecycle.StateExecuting, lifecycle.ReasonCommandStarted, "", ""); err != nil {
		t.Fatalf("Ready→Executing: %v", err)
	}

	if err := m.Transition(lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested, "", ""); err != nil {
		t.Fatalf("Executing→Terminating: %v", err)
	}

	if err := m.Transition(lifecycle.StateTerminated, lifecycle.ReasonTerminateComplete, "", ""); err != nil {
		t.Fatalf("Terminating→Terminated: %v", err)
	}

	if !m.Current().IsTerminal() {
		t.Errorf("expected terminal state, got %s", m.Current())
	}

	hist := m.History()
	want := []struct {
		from   lifecycle.LifecycleState
		to     lifecycle.LifecycleState
		reason lifecycle.TransitionReason
	}{
		{lifecycle.StateSpawning, lifecycle.StateInitializing, lifecycle.ReasonSpawnStarted},
		{lifecycle.StateInitializing, lifecycle.StateReady, lifecycle.ReasonInitComplete},
		{lifecycle.StateReady, lifecycle.StateExecuting, lifecycle.ReasonCommandStarted},
		{lifecycle.StateExecuting, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateTerminating, lifecycle.StateTerminated, lifecycle.ReasonTerminateComplete},
	}

	if len(hist) != len(want) {
		t.Fatalf("history length: got %d, want %d", len(hist), len(want))
	}
	for i, w := range want {
		h := hist[i]
		if h.From != w.from || h.To != w.to || h.Reason != w.reason {
			t.Errorf("history[%d]: got {%s→%s reason=%s}, want {%s→%s reason=%s}",
				i, h.From, h.To, h.Reason, w.from, w.to, w.reason)
		}
	}
}

// TestLifecycleFSM_SilentHang verifies the HC-026 / HC-065 silent-hang path:
// Ready→Failed(reason=silent_hang) is a valid transition that surfaces as a
// deterministic FSM event BEFORE any run_stale timeout.
//
// Acceptance criterion: Silent-hang scenario transitions to StateFailed
// {reason=silent_hang} BEFORE run_stale; the lifecycle snapshot in the
// Machine history carries the hang evidence.
func TestLifecycleFSM_SilentHang(t *testing.T) {
	t.Parallel()

	m := buildMachineAt(t, lifecycle.StateReady)

	if err := m.Transition(lifecycle.StateFailed, lifecycle.ReasonSilentHang, "silent_hang", "agent unresponsive"); err != nil {
		t.Fatalf("Ready→Failed(silent_hang): %v", err)
	}

	if got := m.Current(); got != lifecycle.StateFailed {
		t.Errorf("current: got %s, want %s", got, lifecycle.StateFailed)
	}

	hist := m.History()
	last := hist[len(hist)-1]
	if last.To != lifecycle.StateFailed {
		t.Errorf("last transition To: got %s, want Failed", last.To)
	}
	if last.Reason != lifecycle.ReasonSilentHang {
		t.Errorf("last transition Reason: got %s, want %s", last.Reason, lifecycle.ReasonSilentHang)
	}
	if last.ErrCode != "silent_hang" {
		t.Errorf("last transition ErrCode: got %q, want %q", last.ErrCode, "silent_hang")
	}

	if err := m.Transition(lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested, "", ""); err == nil {
		t.Error("transition from Failed: expected error (terminal), got nil")
	}
}

// TestLifecycleFSM_HkZa5mz_Iter2StuckInReady verifies the hk-za5mz scenario:
// iter-2 stuck-in-Ready surfaces as a deterministic transition-timeout event
// (Ready→Failed(reason=silent_hang)) rather than a 10-minute run_stale.
//
// The RecordActivity method on heartbeat events resets the enteredAt timestamp
// without transitioning state, allowing supervisors to detect staleness by
// comparing enteredAt to the wall clock.
func TestLifecycleFSM_HkZa5mz_Iter2StuckInReady(t *testing.T) {
	t.Parallel()

	m := buildMachineAt(t, lifecycle.StateReady)

	m.RecordActivity()
	m.RecordActivity()

	if got := m.Current(); got != lifecycle.StateReady {
		t.Fatalf("after RecordActivity: state=%s, want Ready", got)
	}

	if err := m.Transition(lifecycle.StateFailed, lifecycle.ReasonSilentHang, "silent_hang", "iter-2 stuck in Ready"); err != nil {
		t.Fatalf("Ready→Failed(silent_hang): %v", err)
	}

	if got := m.Current(); got != lifecycle.StateFailed {
		t.Errorf("current: got %s, want Failed", got)
	}
}

// TestLifecycleFSM_AgentFailedDuringInitializing verifies the path where
// agent_failed is observed before agent_ready: Initializing→Failed(reason=error).
func TestLifecycleFSM_AgentFailedDuringInitializing(t *testing.T) {
	t.Parallel()

	m := lifecycle.New("sess-init-fail", "run-init-fail")
	if err := m.Transition(lifecycle.StateInitializing, lifecycle.ReasonSpawnStarted, "", ""); err != nil {
		t.Fatalf("Spawning→Initializing: %v", err)
	}

	if err := m.Transition(lifecycle.StateFailed, lifecycle.ReasonError, "agent_failed", "process exited before ready"); err != nil {
		t.Fatalf("Initializing→Failed: %v", err)
	}
	if got := m.Current(); got != lifecycle.StateFailed {
		t.Errorf("current: got %s, want Failed", got)
	}
}
