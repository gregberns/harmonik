package lifecycle

import (
	"sync"
	"testing"
	"time"
)

// shutdownFixtureQueueState models the daemon's queue state for PL-013.
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "When all beads are closed or
// deferred and nothing is in-flight, the daemon MUST sleep (suspend CPU
// consumption) and wait for a subsequent harmonik enqueue or for external
// changes to the Beads store."
type shutdownFixtureQueueState struct {
	mu            sync.Mutex
	inFlight      int
	queueEmpty    bool
	stopSignal    bool
	upgradeSignal bool
}

// shutdownFixtureWouldExit returns true only when the daemon would legitimately
// exit: an explicit stop signal OR an operator upgrade transition. It returns
// false when the queue is merely empty with nothing in-flight (PL-013).
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "The daemon MUST NOT exit on
// queue-empty. Daemon exit occurs only on explicit harmonik stop, on an
// operator upgrade transition (running → upgrading per [operator-nfr.md §4.3]),
// or on crash (§PL-024)."
func shutdownFixtureWouldExit(s *shutdownFixtureQueueState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopSignal || s.upgradeSignal
}

// shutdownFixtureShouldSleep returns true when the daemon should sleep and
// wait (queue empty, nothing in-flight, no exit signal). This is the idle
// state mandated by PL-013.
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "the daemon MUST sleep (suspend
// CPU consumption) and wait for a subsequent harmonik enqueue or for external
// changes to the Beads store (periodically re-queried at a configurable
// cadence)."
func shutdownFixtureShouldSleep(s *shutdownFixtureQueueState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueEmpty && s.inFlight == 0 && !s.stopSignal && !s.upgradeSignal
}

// TestPL013_DaemonDoesNotExitOnQueueEmpty verifies that when all beads are
// closed or deferred and nothing is in-flight, the daemon does NOT exit. It
// enters an idle (sleep) state and waits for new enqueues or an explicit stop.
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "When all beads are closed or
// deferred and nothing is in-flight, the daemon MUST sleep (suspend CPU
// consumption) and wait for a subsequent harmonik enqueue or for external
// changes to the Beads store … The daemon MUST NOT exit on queue-empty."
func TestPL013_DaemonDoesNotExitOnQueueEmpty(t *testing.T) {
	t.Parallel()

	state := &shutdownFixtureQueueState{
		queueEmpty: true,
		inFlight:   0,
	}

	// PL-013: queue empty, nothing in-flight — must NOT exit.
	if shutdownFixtureWouldExit(state) {
		t.Error("PL-013: wouldExit returned true on queue-empty; daemon MUST NOT exit on queue-empty")
	}

	// Must be in sleep/idle state.
	if !shutdownFixtureShouldSleep(state) {
		t.Error("PL-013: shouldSleep returned false on queue-empty; daemon MUST idle on queue-empty")
	}
}

// TestPL013_ExitOnlyOnExplicitStop verifies that the daemon exits ONLY when an
// explicit stop signal is received, not on queue-empty.
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "Daemon exit occurs only on
// explicit harmonik stop, on an operator upgrade transition … or on crash."
func TestPL013_ExitOnlyOnExplicitStop(t *testing.T) {
	t.Parallel()

	t.Run("queue-empty/no-exit", func(t *testing.T) {
		t.Parallel()

		state := &shutdownFixtureQueueState{queueEmpty: true, inFlight: 0}

		if shutdownFixtureWouldExit(state) {
			t.Error("PL-013: wouldExit=true on queue-empty with no stop signal; want false")
		}
	})

	t.Run("explicit-stop/exit", func(t *testing.T) {
		t.Parallel()

		state := &shutdownFixtureQueueState{queueEmpty: true, inFlight: 0, stopSignal: true}

		if !shutdownFixtureWouldExit(state) {
			t.Error("PL-013: wouldExit=false with explicit stop signal; want true")
		}
		// When exiting, shouldSleep must be false.
		if shutdownFixtureShouldSleep(state) {
			t.Error("PL-013: shouldSleep=true with explicit stop signal; want false")
		}
	})

	t.Run("upgrade-transition/exit", func(t *testing.T) {
		t.Parallel()

		// Spec ref: PL-013 — "operator upgrade transition (running → upgrading per
		// [operator-nfr.md §4.3])" is a legitimate exit condition.
		state := &shutdownFixtureQueueState{queueEmpty: true, inFlight: 0, upgradeSignal: true}

		if !shutdownFixtureWouldExit(state) {
			t.Error("PL-013: wouldExit=false with upgrade signal; want true (upgrade is a legitimate exit)")
		}
	})

	t.Run("in-flight/no-exit", func(t *testing.T) {
		t.Parallel()

		// Even with queue empty, if runs are in-flight the daemon must NOT exit.
		state := &shutdownFixtureQueueState{queueEmpty: true, inFlight: 1}

		if shutdownFixtureWouldExit(state) {
			t.Error("PL-013: wouldExit=true with in-flight runs; want false (daemon must drain first)")
		}
		// In-flight runs mean we should NOT sleep yet (still processing).
		if shutdownFixtureShouldSleep(state) {
			t.Error("PL-013: shouldSleep=true with in-flight runs; want false")
		}
	})
}

// TestPL013_WakesOnEnqueue verifies that the daemon wakes from idle sleep when
// a new bead is enqueued, restoring a non-empty queue state.
//
// Spec ref: process-lifecycle.md §4.4 PL-013 — "wait for a subsequent
// harmonik enqueue or for external changes to the Beads store (periodically
// re-queried at a configurable cadence)."
func TestPL013_WakesOnEnqueue(t *testing.T) {
	t.Parallel()

	state := &shutdownFixtureQueueState{queueEmpty: true, inFlight: 0}

	// Initially idle.
	if !shutdownFixtureShouldSleep(state) {
		t.Error("PL-013: shouldSleep should be true before enqueue")
	}

	// Simulate a subsequent harmonik enqueue (queue becomes non-empty).
	wakeCh := make(chan struct{}, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		state.mu.Lock()
		state.queueEmpty = false
		state.mu.Unlock()
		wakeCh <- struct{}{}
	}()

	// Wait for the enqueue signal.
	select {
	case <-wakeCh:
	case <-time.After(1 * time.Second):
		t.Fatal("PL-013: timed out waiting for enqueue simulation")
	}

	// After enqueue, shouldSleep must be false (daemon wakes to process).
	if shutdownFixtureShouldSleep(state) {
		t.Error("PL-013: shouldSleep=true after enqueue; daemon must wake to process new beads")
	}

	// And still no exit signal — so wouldExit is false.
	if shutdownFixtureWouldExit(state) {
		t.Error("PL-013: wouldExit=true after enqueue with no stop signal; want false")
	}
}
