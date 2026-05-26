package daemon_test

// workloop_hk24xn1_test.go — wake-on-submit tests (hk-24xn1).
//
// Coverage:
//   - WakeCh: SetQueue signals the channel; a receive completes immediately.
//   - WakeCh coalesces bursts: rapid SetQueue calls produce at most one
//     buffered signal without blocking.
//
// Spec ref: specs/execution-model.md §7.4 (TS-1 dispatch loop).
// Bead ref: hk-24xn1.

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// TestQueueStore_WakeCh_SignaledOnSetQueue verifies that SetQueue delivers a
// signal on WakeCh and that the signal arrives without blocking.
//
// Bead ref: hk-24xn1.
func TestQueueStore_WakeCh_SignaledOnSetQueue(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	wakeC := qs.WakeCh()

	// No signal yet.
	select {
	case <-wakeC:
		t.Fatal("received spurious wake before SetQueue")
	default:
	}

	q := hk24xn1FixtureQueue(t, "hk24xn1-signal-bead-001")
	qs.SetQueue(q)

	// Signal should be available without blocking.
	select {
	case <-wakeC:
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WakeCh was not signaled within 100ms of SetQueue")
	}
}

// TestQueueStore_WakeCh_CoalescesBursts verifies that rapid successive SetQueue
// calls do not block and produce at most one buffered signal (buffer = 1).
//
// Bead ref: hk-24xn1.
func TestQueueStore_WakeCh_CoalescesBursts(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	q := hk24xn1FixtureQueue(t, "hk24xn1-coalesce-bead-001")
	// Three rapid SetQueue calls should not block and should leave exactly one
	// signal buffered.
	qs.SetQueue(q)
	qs.SetQueue(q)
	qs.SetQueue(q)

	// Drain one signal.
	select {
	case <-qs.WakeCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no wake signal after SetQueue burst")
	}

	// Second drain attempt: buffer is now empty — should not receive again
	// without another SetQueue.
	select {
	case <-qs.WakeCh():
		t.Fatal("unexpected second wake signal; buffer should be drained")
	default:
	}
}

// TestQueueStore_WakeCh_NilBeforeNewQueueStore verifies that ExportedNewQueueStore
// returns a store whose WakeCh is non-nil (channel is initialized in constructor).
//
// Bead ref: hk-24xn1.
func TestQueueStore_WakeCh_NonNilAfterConstruction(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	if qs.WakeCh() == nil {
		t.Fatal("WakeCh returned nil channel; expected non-nil from newQueueStore")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// hk24xn1FixtureQueue builds a minimal single-item wave queue with the given
// bead ID. The group is active so EligibleItems returns the item immediately.
// Mirrors queueDispatchFixtureWaveQueue without the variadic signature.
//
// Bead ref: hk-24xn1.
func hk24xn1FixtureQueue(t *testing.T, beadID core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk24xn1-queue-" + string(beadID),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}
}
