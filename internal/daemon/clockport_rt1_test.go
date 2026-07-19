package daemon

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// TestClockAfterAgentReadyReapTimeout_FakeClock drives ONE agent-ready
// kill-reap timeout edge deterministically, with no wall-clock sleeps (RT1 /
// RSM-013). It exercises the exact select shape the migrated run-path reap
// sites now use (workloop.go / reviewloop.go / dot_cascade.go): a watcher.Done()
// channel raced against clockAfter(ctx, clock, agentReadyKillReapTimeout). Here
// the watcher never fires, so the deadline branch MUST win — and it wins only
// because the injected substrate.FakeClock's Advance releases the clockAfter
// sleeper. Under the pre-RT1 time.After the timeout could not be driven in
// virtual time and the test would need a real sleep.
func TestClockAfterAgentReadyReapTimeout_FakeClock(t *testing.T) {
	t.Parallel()

	fc := substrate.NewFakeClock(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))

	// watcherDone stands in for a hung watcher after SIGKILL — it never closes,
	// so the select must resolve on the clock deadline, exactly as the run-path
	// reap guard intends.
	watcherDone := make(chan struct{})

	after := clockAfter(fc, agentReadyKillReapTimeout)

	// The clockAfter goroutine registers a FakeClock sleeper; wait for it so the
	// advance cannot race ahead of the arm (deterministic, no wall-clock sleep).
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

	// Just short of the deadline: the timeout branch must NOT fire yet.
	fc.Advance(agentReadyKillReapTimeout - time.Nanosecond)
	select {
	case fired := <-timedOut:
		t.Fatalf("select resolved early at t<deadline (fired=%v); clockAfter must not wake before the full duration", fired)
	default:
	}

	// Cross the deadline: FakeClock.Advance wakes the sleeper, clockAfter sends,
	// and the timeout branch is taken — all in virtual time.
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
