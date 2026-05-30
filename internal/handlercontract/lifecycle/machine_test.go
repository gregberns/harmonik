package lifecycle_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// TestTransition_LegalEdges verifies every legal edge in the valid-transitions
// table succeeds.
func TestTransition_LegalEdges(t *testing.T) {
	type edge struct {
		from   lifecycle.LifecycleState
		to     lifecycle.LifecycleState
		reason lifecycle.TransitionReason
	}
	legal := []edge{
		// Spawning outgoing
		{lifecycle.StateSpawning, lifecycle.StateInitializing, lifecycle.ReasonInitComplete},
		{lifecycle.StateSpawning, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateSpawning, lifecycle.StateFailed, lifecycle.ReasonError},
		// Initializing outgoing
		{lifecycle.StateInitializing, lifecycle.StateReady, lifecycle.ReasonInitComplete},
		{lifecycle.StateInitializing, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateInitializing, lifecycle.StateFailed, lifecycle.ReasonError},
		// Ready outgoing
		{lifecycle.StateReady, lifecycle.StateExecuting, lifecycle.ReasonCommandStarted},
		{lifecycle.StateReady, lifecycle.StateSuspended, lifecycle.ReasonPauseRequested},
		{lifecycle.StateReady, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateReady, lifecycle.StateFailed, lifecycle.ReasonSilentHang},
		// Executing outgoing
		{lifecycle.StateExecuting, lifecycle.StateReady, lifecycle.ReasonCommandComplete},
		{lifecycle.StateExecuting, lifecycle.StateSuspended, lifecycle.ReasonPauseRequested},
		{lifecycle.StateExecuting, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateExecuting, lifecycle.StateFailed, lifecycle.ReasonError},
		// Suspended outgoing
		{lifecycle.StateSuspended, lifecycle.StateReady, lifecycle.ReasonResumeRequested},
		{lifecycle.StateSuspended, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested},
		{lifecycle.StateSuspended, lifecycle.StateFailed, lifecycle.ReasonError},
		// Terminating outgoing
		{lifecycle.StateTerminating, lifecycle.StateTerminated, lifecycle.ReasonTerminateComplete},
		{lifecycle.StateTerminating, lifecycle.StateFailed, lifecycle.ReasonError},
	}

	for _, e := range legal {
		// Build a machine already in the 'from' state by driving it there.
		m := buildMachineAt(t, e.from)
		if err := m.Transition(e.to, e.reason, "", ""); err != nil {
			t.Errorf("legal transition %s→%s: unexpected error: %v", e.from, e.to, err)
		}
		if got := m.Current(); got != e.to {
			t.Errorf("after %s→%s: current=%s want %s", e.from, e.to, got, e.to)
		}
	}
}

// TestTransition_IllegalEdges verifies that illegal edges return
// *InvalidStateTransitionError wrapping ErrInvalidStateTransition.
func TestTransition_IllegalEdges(t *testing.T) {
	type edge struct {
		from lifecycle.LifecycleState
		to   lifecycle.LifecycleState
	}
	illegal := []edge{
		// No direct Suspended→Executing
		{lifecycle.StateSuspended, lifecycle.StateExecuting},
		// No direct Spawning→Ready
		{lifecycle.StateSpawning, lifecycle.StateReady},
		// No direct Initializing→Executing
		{lifecycle.StateInitializing, lifecycle.StateExecuting},
		// Terminal→anything
		{lifecycle.StateTerminated, lifecycle.StateReady},
		{lifecycle.StateTerminated, lifecycle.StateSpawning},
		{lifecycle.StateFailed, lifecycle.StateReady},
		{lifecycle.StateFailed, lifecycle.StateSpawning},
		// Terminating→Ready (no going back from Terminating to live)
		{lifecycle.StateTerminating, lifecycle.StateReady},
		// Ready→Initializing (no backward)
		{lifecycle.StateReady, lifecycle.StateInitializing},
		{lifecycle.StateReady, lifecycle.StateSpawning},
	}

	for _, e := range illegal {
		m := buildMachineAt(t, e.from)
		err := m.Transition(e.to, lifecycle.ReasonUserAction, "", "")
		if err == nil {
			t.Errorf("illegal transition %s→%s: expected error, got nil", e.from, e.to)
			continue
		}
		var iste *lifecycle.InvalidStateTransitionError
		if !errors.As(err, &iste) {
			t.Errorf("illegal transition %s→%s: error type %T, want *InvalidStateTransitionError", e.from, e.to, err)
		}
		if !errors.Is(err, lifecycle.ErrInvalidStateTransition) {
			t.Errorf("illegal transition %s→%s: errors.Is(ErrInvalidStateTransition) false", e.from, e.to)
		}
		// State must not have changed.
		if got := m.Current(); got != e.from {
			t.Errorf("after rejected %s→%s: current=%s want %s (state must not change)", e.from, e.to, got, e.from)
		}
	}
}

// TestTerminalStates verifies IsTerminal and that no further transitions are
// accepted from Terminated or Failed.
func TestTerminalStates(t *testing.T) {
	for _, term := range []lifecycle.LifecycleState{lifecycle.StateTerminated, lifecycle.StateFailed} {
		if !term.IsTerminal() {
			t.Errorf("%s.IsTerminal() = false, want true", term)
		}
		m := buildMachineAt(t, term)
		err := m.Transition(lifecycle.StateReady, lifecycle.ReasonUserAction, "", "")
		if err == nil {
			t.Errorf("transition from terminal %s: expected error, got nil", term)
		}
	}
	for _, live := range []lifecycle.LifecycleState{
		lifecycle.StateSpawning,
		lifecycle.StateInitializing,
		lifecycle.StateReady,
		lifecycle.StateExecuting,
		lifecycle.StateSuspended,
		lifecycle.StateTerminating,
	} {
		if live.IsTerminal() {
			t.Errorf("%s.IsTerminal() = true, want false", live)
		}
	}
}

// TestFailedTransition_ErrFields verifies that ErrCode/ErrMsg are only
// recorded when transitioning to StateFailed.
func TestFailedTransition_ErrFields(t *testing.T) {
	m := lifecycle.New("sess-1", "run-1")
	_ = m.Transition(lifecycle.StateInitializing, lifecycle.ReasonInitComplete, "", "")
	_ = m.Transition(lifecycle.StateReady, lifecycle.ReasonInitComplete, "", "")
	if err := m.Transition(lifecycle.StateFailed, lifecycle.ReasonSilentHang, "SILENT_HANG", "agent did not respond"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := m.History()
	last := h[len(h)-1]
	if last.ErrCode != "SILENT_HANG" {
		t.Errorf("ErrCode=%q want SILENT_HANG", last.ErrCode)
	}
	if last.ErrMsg != "agent did not respond" {
		t.Errorf("ErrMsg=%q want 'agent did not respond'", last.ErrMsg)
	}
}

// TestHistory_RingEviction verifies that after 51 transitions only the 50
// most-recent entries are retained (oldest evicted) — HC-067.
func TestHistory_RingEviction(t *testing.T) {
	// Drive a cycle Ready↔Executing 26 times = 52 transitions total starting
	// from Spawning→Initializing→Ready (2 transitions), then 50 Ready→Executing
	// and back cycles. We want to do exactly 51 transitions into the ring.
	//
	// Strategy: Spawning→Initializing (1), Initializing→Ready (2),
	// then alternate Ready→Executing→Ready 24 times (48 transitions) = 50
	// total, plus one more Ready→Executing (51st), then Ready→Executing (52nd).
	// After 52 transitions the ring must hold exactly 50, oldest evicted.

	m := lifecycle.New("sess-evict", "run-evict")
	transitions := 0

	step := func(to lifecycle.LifecycleState, reason lifecycle.TransitionReason) {
		t.Helper()
		if err := m.Transition(to, reason, "", ""); err != nil {
			t.Fatalf("transition to %s failed: %v", to, err)
		}
		transitions++
	}

	step(lifecycle.StateInitializing, lifecycle.ReasonInitComplete) // 1
	step(lifecycle.StateReady, lifecycle.ReasonInitComplete)        // 2

	// Now cycle Ready↔Executing until we've recorded 51 transitions total.
	for transitions < 51 {
		step(lifecycle.StateExecuting, lifecycle.ReasonCommandStarted)
		if transitions < 51 {
			step(lifecycle.StateReady, lifecycle.ReasonCommandComplete)
		}
	}

	// Ring must hold 50 entries.
	h := m.History()
	if len(h) != 50 {
		t.Fatalf("history len=%d want 50 after %d transitions", len(h), transitions)
	}

	// Do one more to confirm eviction is stable.
	if m.Current() == lifecycle.StateExecuting {
		step(lifecycle.StateReady, lifecycle.ReasonCommandComplete) // 52nd
	} else {
		step(lifecycle.StateExecuting, lifecycle.ReasonCommandStarted) // 52nd
	}
	h = m.History()
	if len(h) != 50 {
		t.Fatalf("history len=%d want 50 after %d transitions", len(h), transitions)
	}
}

// TestHistory_OrderOldestFirst verifies that History returns entries oldest→newest.
func TestHistory_OrderOldestFirst(t *testing.T) {
	m := lifecycle.New("sess-ord", "run-ord")
	_ = m.Transition(lifecycle.StateInitializing, lifecycle.ReasonSpawnStarted, "", "")
	_ = m.Transition(lifecycle.StateReady, lifecycle.ReasonInitComplete, "", "")
	_ = m.Transition(lifecycle.StateExecuting, lifecycle.ReasonCommandStarted, "", "")

	h := m.History()
	if len(h) != 3 {
		t.Fatalf("len=%d want 3", len(h))
	}
	if h[0].To != lifecycle.StateInitializing {
		t.Errorf("h[0].To=%s want Initializing", h[0].To)
	}
	if h[1].To != lifecycle.StateReady {
		t.Errorf("h[1].To=%s want Ready", h[1].To)
	}
	if h[2].To != lifecycle.StateExecuting {
		t.Errorf("h[2].To=%s want Executing", h[2].To)
	}
}

// TestConcurrentTransition is a smoke test verifying that concurrent calls to
// Transition do not race. One goroutine drives the machine; a second
// goroutine reads Current concurrently.
func TestConcurrentTransition(t *testing.T) {
	m := lifecycle.New("sess-conc", "run-conc")

	var wg sync.WaitGroup
	const readers = 10

	// Drive the machine through a short legal path in a separate goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = m.Transition(lifecycle.StateInitializing, lifecycle.ReasonInitComplete, "", "")
		_ = m.Transition(lifecycle.StateReady, lifecycle.ReasonInitComplete, "", "")
		_ = m.Transition(lifecycle.StateExecuting, lifecycle.ReasonCommandStarted, "", "")
		_ = m.Transition(lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested, "", "")
		_ = m.Transition(lifecycle.StateTerminated, lifecycle.ReasonTerminateComplete, "", "")

	}()

	// Concurrent readers.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.Current()
				_ = m.History()
			}
		}()
	}

	wg.Wait()
}

// buildMachineAt drives a freshly-constructed Machine to the target state via
// the shortest legal path. Used by table-driven tests that need to start
// from an arbitrary state.
func buildMachineAt(t *testing.T, target lifecycle.LifecycleState) *lifecycle.Machine {
	t.Helper()
	m := lifecycle.New("sess-build", "run-build")
	if target == lifecycle.StateSpawning {
		return m
	}
	path := shortestPath(target)
	for _, e := range path {
		if err := m.Transition(e.to, e.reason, e.errCode, e.errMsg); err != nil {
			t.Fatalf("buildMachineAt(%s): transition %s→%s failed: %v", target, e.from, e.to, err)
		}
	}
	return m
}

type pathEdge struct {
	from    lifecycle.LifecycleState
	to      lifecycle.LifecycleState
	reason  lifecycle.TransitionReason
	errCode string
	errMsg  string
}

// shortestPath returns the minimal sequence of transitions to reach target
// from StateSpawning.
func shortestPath(target lifecycle.LifecycleState) []pathEdge {
	init2ready := []pathEdge{
		{lifecycle.StateSpawning, lifecycle.StateInitializing, lifecycle.ReasonInitComplete, "", ""},
		{lifecycle.StateInitializing, lifecycle.StateReady, lifecycle.ReasonInitComplete, "", ""},
	}
	switch target {
	case lifecycle.StateInitializing:
		return init2ready[:1]
	case lifecycle.StateReady:
		return init2ready
	case lifecycle.StateExecuting:
		return append(init2ready, pathEdge{lifecycle.StateReady, lifecycle.StateExecuting, lifecycle.ReasonCommandStarted, "", ""})
	case lifecycle.StateSuspended:
		return append(init2ready, pathEdge{lifecycle.StateReady, lifecycle.StateSuspended, lifecycle.ReasonPauseRequested, "", ""})
	case lifecycle.StateTerminating:
		return append(init2ready, pathEdge{lifecycle.StateReady, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested, "", ""})
	case lifecycle.StateTerminated:
		return append(init2ready,
			pathEdge{lifecycle.StateReady, lifecycle.StateTerminating, lifecycle.ReasonTerminateRequested, "", ""},
			pathEdge{lifecycle.StateTerminating, lifecycle.StateTerminated, lifecycle.ReasonTerminateComplete, "", ""},
		)
	case lifecycle.StateFailed:
		return append(init2ready, pathEdge{lifecycle.StateReady, lifecycle.StateFailed, lifecycle.ReasonError, "E_TEST", "test failure"})
	default:
		return nil
	}
}
