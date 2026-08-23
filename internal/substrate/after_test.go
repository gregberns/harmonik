package substrate_test

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// TestClockAfterAgentReadyReapTimeout_FakeClock drives ONE agent-ready
// kill-reap timeout edge deterministically, with no wall-clock sleeps (RT1 /
// RSM-013). It exercises the exact select shape the migrated run-path reap
// sites now use (workloop.go / reviewloop.go / dot_cascade.go): a watcher.Done()
// channel raced against substrate.After(clock, agentReadyKillReapTimeout). Here
// the watcher never fires, so the deadline branch MUST win — and it wins only
// because the injected substrate.FakeClock's Advance releases the substrate.After
// sleeper. Under the pre-RT1 time.After the timeout could not be driven in
// virtual time and the test would need a real sleep.
func TestClockAfterAgentReadyReapTimeout_FakeClock(t *testing.T) {
	t.Parallel()

	const reapTimeout = 10 * time.Second

	fc := substrate.NewFakeClock(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))

	watcherDone := make(chan struct{})

	after := substrate.After(fc, reapTimeout)

	fc.BlockUntil(1)

	timedOut := make(chan bool, 1)
	go func() {
		select {
		case <-watcherDone:
			timedOut <- false
		case <-after:
			timedOut <- true
		}
	}()

	fc.Advance(reapTimeout - time.Nanosecond)
	select {
	case fired := <-timedOut:
		t.Fatalf("select resolved early at t<deadline (fired=%v); substrate.After must not wake before the full duration", fired)
	default:
	}

	fc.Advance(time.Nanosecond)

	select {
	case fired := <-timedOut:
		if !fired {
			t.Fatalf("expected the agent-ready reap TIMEOUT branch to win, got watcher.Done()")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the FakeClock-driven deadline to fire (deadlock?)")
	}
}
