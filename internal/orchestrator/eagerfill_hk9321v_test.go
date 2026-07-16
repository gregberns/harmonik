package orchestrator_test

// eagerfill_hk9321v_test.go — EM-062/EM-063 pure-decision truth-tables for the
// eager-refill deficit computation, overfetch limit, survivor clamp, and Phase-1
// already-queued screen (M5 slice 3B). These drive the pure orchestrator fns
// directly over narrow inputs; the daemon's lock/kerf-exec/git-Phase-2/AppendItems
// integration is exercised by the sibling tests that remain in package daemon.
//
// Bead: hk-9321v (eagerRefill)
// Spec refs: specs/execution-model.md §4.13 EM-062, EM-063.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/orchestrator"
)

// streamQueue builds an active QueueSnapshot whose active group is a stream with
// the given pending count. A wave group is produced when kind != "stream".
func streamQueue(name, queueID string, groupPos, pending int, kind string) orchestrator.QueueSnapshot {
	return orchestrator.QueueSnapshot{
		Name:    name,
		QueueID: queueID,
		Active:  true,
		ActiveGroup: &orchestrator.GroupSnapshot{
			GroupIndex:   groupPos,
			Kind:         kind,
			PendingCount: pending,
		},
	}
}

// ---------------------------------------------------------------------------
// EagerFillTarget — deficit computation + stream-group selection
// ---------------------------------------------------------------------------

// TestEagerFillTarget_DeficitFromAvailableMinusPending verifies the core math:
// available = maxConcurrent - inFlight; deficit = available - pendingCount.
func TestEagerFillTarget_DeficitFromAvailableMinusPending(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{
			streamQueue("main", "qid-main", 0, 1, "stream"),
		},
	}
	// available = 4 - 1 = 3; deficit = 3 - 1(pending) = 2.
	target, ok := orchestrator.EagerFillTarget(f, 4, 1)
	if !ok {
		t.Fatal("EagerFillTarget ok=false, want a target (deficit 2)")
	}
	if target.QueueName != "main" || target.QueueID != "qid-main" || target.GroupPos != 0 {
		t.Errorf("target = %+v, want main/qid-main/group0", target)
	}
	if target.Deficit != 2 {
		t.Errorf("Deficit = %d, want 2 (available 3 - pending 1)", target.Deficit)
	}
}

// TestEagerFillTarget_NoAvailableSlots verifies the early-out when in-flight has
// saturated max_concurrent (available <= 0).
func TestEagerFillTarget_NoAvailableSlots(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{streamQueue("main", "qid-main", 0, 0, "stream")},
	}
	if _, ok := orchestrator.EagerFillTarget(f, 4, 4); ok {
		t.Error("EagerFillTarget ok=true at available=0, want false")
	}
}

// TestEagerFillTarget_PendingCoversDeficit verifies that a group whose pending
// items already cover the available slots (deficit <= 0) is not a target.
func TestEagerFillTarget_PendingCoversDeficit(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		// available = 3; pending = 3 → deficit 0, skip.
		Queues: []orchestrator.QueueSnapshot{streamQueue("main", "qid-main", 0, 3, "stream")},
	}
	if _, ok := orchestrator.EagerFillTarget(f, 3, 0); ok {
		t.Error("EagerFillTarget ok=true when pending covers deficit, want false")
	}
}

// TestEagerFillTarget_SkipsWaveAndInactive verifies that only ACTIVE queues with
// an active STREAM group qualify: a wave group and an inactive queue are skipped,
// and the first qualifying stream group (in fleet order) is returned.
func TestEagerFillTarget_SkipsWaveAndInactive(t *testing.T) {
	t.Parallel()

	inactive := streamQueue("paused", "qid-paused", 0, 0, "stream")
	inactive.Active = false

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{
			inactive, // inactive → skip
			streamQueue("wavy", "qid-wavy", 0, 0, "wave"),   // wave → skip
			streamQueue("strm", "qid-strm", 2, 0, "stream"), // first qualifying stream
		},
	}
	target, ok := orchestrator.EagerFillTarget(f, 4, 0)
	if !ok {
		t.Fatal("EagerFillTarget ok=false, want the stream queue")
	}
	if target.QueueName != "strm" || target.GroupPos != 2 {
		t.Errorf("target = %+v, want strm/group2 (wave+inactive skipped)", target)
	}
}

// TestEagerFillTarget_NilActiveGroup verifies a queue with no active group is
// skipped without panicking.
func TestEagerFillTarget_NilActiveGroup(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{{Name: "empty", Active: true, ActiveGroup: nil}},
	}
	if _, ok := orchestrator.EagerFillTarget(f, 4, 0); ok {
		t.Error("EagerFillTarget ok=true for nil active group, want false")
	}
}

// ---------------------------------------------------------------------------
// OverfetchLimit — deficit × OVERFETCH_FACTOR (2)
// ---------------------------------------------------------------------------

func TestOverfetchLimit(t *testing.T) {
	t.Parallel()
	cases := []struct{ deficit, want int }{
		{0, 0}, {1, 2}, {3, 6}, {5, 10},
	}
	for _, c := range cases {
		if got := orchestrator.OverfetchLimit(c.deficit); got != c.want {
			t.Errorf("OverfetchLimit(%d) = %d, want %d", c.deficit, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ClampSurvivors — cap at deficit, preserve priority order
// ---------------------------------------------------------------------------

func TestClampSurvivors(t *testing.T) {
	t.Parallel()
	all := []core.BeadID{"hk-a", "hk-b", "hk-c", "hk-d"}

	// deficit < len → clamp, order preserved.
	got := orchestrator.ClampSurvivors(all, 2)
	if len(got) != 2 || got[0] != "hk-a" || got[1] != "hk-b" {
		t.Errorf("ClampSurvivors(len4, 2) = %v, want [hk-a hk-b]", got)
	}

	// deficit >= len → unchanged.
	if got := orchestrator.ClampSurvivors(all, 10); len(got) != 4 {
		t.Errorf("ClampSurvivors(len4, 10) = %v, want all 4", got)
	}

	// deficit == len → unchanged (boundary).
	if got := orchestrator.ClampSurvivors(all, 4); len(got) != 4 {
		t.Errorf("ClampSurvivors(len4, 4) = %v, want all 4", got)
	}

	// non-positive deficit → empty.
	if got := orchestrator.ClampSurvivors(all, 0); len(got) != 0 {
		t.Errorf("ClampSurvivors(len4, 0) = %v, want empty", got)
	}
	if got := orchestrator.ClampSurvivors(all, -1); len(got) != 0 {
		t.Errorf("ClampSurvivors(len4, -1) = %v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// ScreenAlreadyQueued — EM-063 Phase-1 set-membership filter
// ---------------------------------------------------------------------------

// TestScreenAlreadyQueued_DropsInSetPreservesOrder verifies that candidates in
// the in-queue set are dropped while the rest pass through in original order.
func TestScreenAlreadyQueued_DropsInSetPreservesOrder(t *testing.T) {
	t.Parallel()

	inQueue := map[core.BeadID]struct{}{"hk-in-1": {}, "hk-in-2": {}}
	candidates := []core.BeadID{"hk-in-1", "hk-new-a", "hk-in-2", "hk-new-b"}

	got := orchestrator.ScreenAlreadyQueued(candidates, inQueue)
	if len(got) != 2 || got[0] != "hk-new-a" || got[1] != "hk-new-b" {
		t.Errorf("ScreenAlreadyQueued = %v, want [hk-new-a hk-new-b]", got)
	}
}

// TestScreenAlreadyQueued_NilSetDropsNothing verifies a nil/empty in-queue set
// passes every candidate through.
func TestScreenAlreadyQueued_NilSetDropsNothing(t *testing.T) {
	t.Parallel()

	candidates := []core.BeadID{"hk-a", "hk-b", "hk-c"}
	got := orchestrator.ScreenAlreadyQueued(candidates, nil)
	if len(got) != 3 {
		t.Errorf("ScreenAlreadyQueued(nil set) = %v, want all 3", got)
	}
}

// TestScreenAlreadyQueued_AllQueuedYieldsEmpty verifies that when every candidate
// is already queued the result is empty.
func TestScreenAlreadyQueued_AllQueuedYieldsEmpty(t *testing.T) {
	t.Parallel()

	inQueue := map[core.BeadID]struct{}{"hk-a": {}, "hk-b": {}}
	got := orchestrator.ScreenAlreadyQueued([]core.BeadID{"hk-a", "hk-b"}, inQueue)
	if len(got) != 0 {
		t.Errorf("ScreenAlreadyQueued(all queued) = %v, want empty", got)
	}
}
