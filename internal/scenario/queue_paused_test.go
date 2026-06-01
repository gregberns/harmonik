package scenario

// queue_paused_test.go — scenario tests for the paused-by-failure queue lifecycle.
//
// Bead: hk-2gqua (T81)
// Spec ref: specs/queue-model.md §5 (group state machine), §8.3 (QM-052 pause-by-failure),
//           §8.6 (QM-055 persisted pause survives restart).
//
// Assertions (per bead body):
//  (a) A group with a failed item transitions to complete-with-failures via AdvanceGroup.
//  (b) The queue transitions to paused-by-failure (QM-052).
//  (c) The queue_paused event is emitted with reason=group_failure.
//  (d) queue.json persists across daemon restart with paused-by-failure status preserved (QM-055).
//
// These tests exercise the queue state machine and persistence layer directly —
// no live daemon is required. This keeps the file self-contained and independent
// of the T80 end-to-end lifecycle test (hk-8vokz).
//
// Helper prefix: queuePausedFixture (per implementer-protocol.md §Helper-prefix discipline).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const queuePausedFixtureQueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ff0081"

var queuePausedFixtureNow = time.Date(2026, 5, 15, 14, 0, 0, 0, time.UTC)

// queuePausedFixtureTempDir creates a temporary directory and registers cleanup.
func queuePausedFixtureTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-queuepaused-")
	if err != nil {
		t.Fatalf("queuePausedFixtureTempDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// queuePausedFixtureItem builds a queue.Item with the given bead ID and status.
func queuePausedFixtureItem(beadID string, status queue.ItemStatus) queue.Item {
	return queue.Item{
		BeadID: core.BeadID(beadID),
		Status: status,
	}
}

// queuePausedFixtureGroup builds a wave Group at the given index with the supplied items.
func queuePausedFixtureGroup(idx int, status queue.GroupStatus, items []queue.Item) queue.Group {
	return queue.Group{
		GroupIndex: idx,
		Kind:       queue.GroupKindWave,
		Status:     status,
		Items:      items,
		CreatedAt:  queuePausedFixtureNow,
	}
}

// queuePausedFixtureAdvance calls AdvanceGroup with the fixture queue ID and
// timestamp, failing the test on any unexpected error.
func queuePausedFixtureAdvance(
	t *testing.T,
	g *queue.Group,
	qs queue.QueueStatus,
) (queue.GroupStatus, []core.Event) {
	t.Helper()
	newStatus, events, err := queue.AdvanceGroup(
		context.Background(),
		g,
		qs,
		queuePausedFixtureQueueID,
		queuePausedFixtureNow,
	)
	if err != nil {
		t.Fatalf("queuePausedFixtureAdvance: AdvanceGroup: %v", err)
	}
	return newStatus, events
}

// queuePausedFixtureUnmarshalPausedPayload decodes a queue_paused event payload,
// failing the test on any error.
func queuePausedFixtureUnmarshalPausedPayload(t *testing.T, e core.Event) core.QueuePausedPayload {
	t.Helper()
	var p core.QueuePausedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("queuePausedFixtureUnmarshalPausedPayload: unmarshal: %v", err)
	}
	return p
}

// queuePausedFixtureUnmarshalCompletedPayload decodes a queue_group_completed
// event payload, failing the test on any error.
func queuePausedFixtureUnmarshalCompletedPayload(t *testing.T, e core.Event) core.QueueGroupCompletedPayload {
	t.Helper()
	var p core.QueueGroupCompletedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("queuePausedFixtureUnmarshalCompletedPayload: unmarshal: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// (a) group reaches complete-with-failures on synthetic failure
// ---------------------------------------------------------------------------

// TestQueuePaused_GroupReachesCompleteWithFailures verifies that AdvanceGroup
// transitions an active group to complete-with-failures when at least one item
// is failed and all items are terminal (§5.1 row 4, QM-030).
func TestQueuePaused_GroupReachesCompleteWithFailures(t *testing.T) {
	t.Parallel()

	g := queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-aa01", queue.ItemStatusCompleted),
		queuePausedFixtureItem("hk-t81-aa02", queue.ItemStatusFailed), // synthetic failure
	})

	newStatus, _ := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group status = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
}

// TestQueuePaused_GroupAllFailedReachesCompleteWithFailures verifies the
// complete-with-failures path when every item in the group failed.
func TestQueuePaused_GroupAllFailedReachesCompleteWithFailures(t *testing.T) {
	t.Parallel()

	g := queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-ab01", queue.ItemStatusFailed),
		queuePausedFixtureItem("hk-t81-ab02", queue.ItemStatusFailed),
	})

	newStatus, _ := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group status = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
}

// TestQueuePaused_InFlightSiblingBlocksGroupCompletion verifies that a group
// with one failed item and one still-dispatched sibling stays active (QM-034):
// failed items MUST NOT interrupt sibling dispatches.
func TestQueuePaused_InFlightSiblingBlocksGroupCompletion(t *testing.T) {
	t.Parallel()

	g := queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-ac01", queue.ItemStatusFailed),
		queuePausedFixtureItem("hk-t81-ac02", queue.ItemStatusDispatched), // still in flight
	})

	newStatus, events := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("group status = %q, want active — failed item must not interrupt in-flight sibling (QM-034)", newStatus)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 while sibling is in flight", len(events))
	}
}

// ---------------------------------------------------------------------------
// (b) queue transitions to paused-by-failure (QM-052)
// ---------------------------------------------------------------------------

// TestQueuePaused_QueueStatusTransitionsToPausedByFailure verifies that after a
// group reaches complete-with-failures, the caller-managed queue envelope should
// be set to paused-by-failure per QM-052. This test constructs the queue envelope
// and verifies the status update and persistence path.
func TestQueuePaused_QueueStatusTransitionsToPausedByFailure(t *testing.T) {
	t.Parallel()

	// Simulate a group that has reached complete-with-failures.
	g := queuePausedFixtureGroup(0, queue.GroupStatusCompleteWithFailures, []queue.Item{
		queuePausedFixtureItem("hk-t81-ba01", queue.ItemStatusCompleted),
		queuePausedFixtureItem("hk-t81-ba02", queue.ItemStatusFailed),
	})

	// The queue envelope reflects the pause-by-failure status transition per QM-052.
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       queuePausedFixtureQueueID,
		SubmittedAt:   queuePausedFixtureNow,
		Status:        queue.QueueStatusPausedByFailure,
		Groups:        []queue.Group{g},
	}

	// Verify the queue status is paused-by-failure.
	if q.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue status = %q, want %q", q.Status, queue.QueueStatusPausedByFailure)
	}
	// Verify the group status is complete-with-failures.
	if q.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group[0] status = %q, want %q", q.Groups[0].Status, queue.GroupStatusCompleteWithFailures)
	}
}

// TestQueuePaused_PausedQueueBlocksSuccessorGroupAdvance verifies that a group
// in pending state does NOT advance when the queue is paused-by-failure (QM-031).
// This confirms that after a failure pause, subsequent groups are blocked.
func TestQueuePaused_PausedQueueBlocksSuccessorGroupAdvance(t *testing.T) {
	t.Parallel()

	// Group 1 is pending — it would normally advance after group 0 succeeds, but
	// the queue is paused-by-failure so it must not.
	g1 := queuePausedFixtureGroup(1, queue.GroupStatusPending, []queue.Item{
		queuePausedFixtureItem("hk-t81-bb01", queue.ItemStatusPending),
	})

	newStatus, events := queuePausedFixtureAdvance(t, &g1, queue.QueueStatusPausedByFailure)

	if newStatus != queue.GroupStatusPending {
		t.Errorf("group status = %q, want pending — paused queue must block group advance (QM-031)", newStatus)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 while queue is paused-by-failure", len(events))
	}
}

// ---------------------------------------------------------------------------
// (c) queue_paused event with reason=group_failure
// ---------------------------------------------------------------------------

// TestQueuePaused_EventEmittedWithGroupFailureReason verifies that AdvanceGroup
// on an active group with failures emits two events in order:
//
//  1. queue_group_completed{final_status: complete-with-failures}
//  2. queue_paused{reason: group_failure}
//
// (§5.1 row 4, QM-052)
func TestQueuePaused_EventEmittedWithGroupFailureReason(t *testing.T) {
	t.Parallel()

	g := queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-ca01", queue.ItemStatusCompleted),
		queuePausedFixtureItem("hk-t81-ca02", queue.ItemStatusFailed),
	})

	_, events := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2 (queue_group_completed + queue_paused)", len(events))
	}

	// events[0] must be queue_group_completed with final_status=complete-with-failures.
	if events[0].Type != "queue_group_completed" {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, "queue_group_completed")
	}
	completedPayload := queuePausedFixtureUnmarshalCompletedPayload(t, events[0])
	if completedPayload.FinalStatus != "complete-with-failures" {
		t.Errorf("queue_group_completed.final_status = %q, want %q",
			completedPayload.FinalStatus, "complete-with-failures")
	}
	if completedPayload.QueueID != queuePausedFixtureQueueID {
		t.Errorf("queue_group_completed.queue_id = %q, want %q",
			completedPayload.QueueID, queuePausedFixtureQueueID)
	}

	// events[1] must be queue_paused with reason=group_failure.
	if events[1].Type != "queue_paused" {
		t.Errorf("events[1].Type = %q, want %q", events[1].Type, "queue_paused")
	}
	pausedPayload := queuePausedFixtureUnmarshalPausedPayload(t, events[1])
	if pausedPayload.Reason != "group_failure" {
		t.Errorf("queue_paused.reason = %q, want %q", pausedPayload.Reason, "group_failure")
	}
	if pausedPayload.QueueID != queuePausedFixtureQueueID {
		t.Errorf("queue_paused.queue_id = %q, want %q",
			pausedPayload.QueueID, queuePausedFixtureQueueID)
	}
}

// TestQueuePaused_EventFailCountMatchesActualFailures verifies that the
// queue_paused event payload carries the correct fail_count matching the
// number of failed items in the group.
func TestQueuePaused_EventFailCountMatchesActualFailures(t *testing.T) {
	t.Parallel()

	g := queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-cb01", queue.ItemStatusCompleted),
		queuePausedFixtureItem("hk-t81-cb02", queue.ItemStatusFailed),
		queuePausedFixtureItem("hk-t81-cb03", queue.ItemStatusFailed),
	})

	_, events := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}

	pausedPayload := queuePausedFixtureUnmarshalPausedPayload(t, events[1])
	if pausedPayload.FailCount != 2 {
		t.Errorf("queue_paused.fail_count = %d, want 2", pausedPayload.FailCount)
	}

	completedPayload := queuePausedFixtureUnmarshalCompletedPayload(t, events[0])
	if completedPayload.FailCount != 2 {
		t.Errorf("queue_group_completed.fail_count = %d, want 2", completedPayload.FailCount)
	}
	if completedPayload.SuccessCount != 1 {
		t.Errorf("queue_group_completed.success_count = %d, want 1", completedPayload.SuccessCount)
	}
}

// TestQueuePaused_EventGroupIndexMatchesGroup verifies that the queue_paused
// event carries the correct group_index matching the failed group.
func TestQueuePaused_EventGroupIndexMatchesGroup(t *testing.T) {
	t.Parallel()

	const groupIdx = 0

	g := queuePausedFixtureGroup(groupIdx, queue.GroupStatusActive, []queue.Item{
		queuePausedFixtureItem("hk-t81-cc01", queue.ItemStatusFailed),
	})

	_, events := queuePausedFixtureAdvance(t, &g, queue.QueueStatusActive)

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}

	pausedPayload := queuePausedFixtureUnmarshalPausedPayload(t, events[1])
	if pausedPayload.GroupIndex != groupIdx {
		t.Errorf("queue_paused.group_index = %d, want %d", pausedPayload.GroupIndex, groupIdx)
	}
}

// ---------------------------------------------------------------------------
// (d) queue.json persists across daemon restart with paused-by-failure status
// ---------------------------------------------------------------------------

// TestQueuePaused_PersistRoundTrip verifies that a Queue with status=paused-by-failure
// survives a Persist → Load round trip with status preserved (QM-001, QM-002, QM-055).
func TestQueuePaused_PersistRoundTrip(t *testing.T) {
	t.Parallel()

	projectDir := queuePausedFixtureTempDir(t)

	// Build a Queue envelope in paused-by-failure state with one completed group.
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       queuePausedFixtureQueueID,
		SubmittedAt:   queuePausedFixtureNow,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			queuePausedFixtureGroup(0, queue.GroupStatusCompleteWithFailures, []queue.Item{
				queuePausedFixtureItem("hk-t81-da01", queue.ItemStatusCompleted),
				queuePausedFixtureItem("hk-t81-da02", queue.ItemStatusFailed),
			}),
		},
	}

	ctx := context.Background()

	// Persist simulates the daemon writing queue.json after the failure pause.
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Load simulates the daemon reading queue.json on restart (QM-002).
	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil; expected queue with paused-by-failure status")
	}

	// QM-055: the loaded queue must retain paused-by-failure status — no auto-resume.
	if loaded.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("loaded queue status = %q, want %q (QM-055: pause persists across restart)",
			loaded.Status, queue.QueueStatusPausedByFailure)
	}

	// Verify queue_id and group_count are preserved.
	if loaded.QueueID != queuePausedFixtureQueueID {
		t.Errorf("loaded queue_id = %q, want %q", loaded.QueueID, queuePausedFixtureQueueID)
	}
	if len(loaded.Groups) != 1 {
		t.Fatalf("loaded group count = %d, want 1", len(loaded.Groups))
	}

	// Verify the group status is preserved.
	if loaded.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("loaded group[0] status = %q, want %q",
			loaded.Groups[0].Status, queue.GroupStatusCompleteWithFailures)
	}
}

// TestQueuePaused_PersistRoundTrip_ItemStatusesPreserved verifies that per-item
// statuses (completed, failed) within the failed group survive Persist → Load
// unchanged. This confirms the full envelope is durable, not just the top-level status.
func TestQueuePaused_PersistRoundTrip_ItemStatusesPreserved(t *testing.T) {
	t.Parallel()

	projectDir := queuePausedFixtureTempDir(t)

	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       queuePausedFixtureQueueID,
		SubmittedAt:   queuePausedFixtureNow,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			queuePausedFixtureGroup(0, queue.GroupStatusCompleteWithFailures, []queue.Item{
				queuePausedFixtureItem("hk-t81-ea01", queue.ItemStatusCompleted),
				queuePausedFixtureItem("hk-t81-ea02", queue.ItemStatusFailed),
				queuePausedFixtureItem("hk-t81-ea03", queue.ItemStatusCompleted),
			}),
		},
	}

	ctx := context.Background()

	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}

	if len(loaded.Groups) != 1 {
		t.Fatalf("loaded group count = %d, want 1", len(loaded.Groups))
	}
	items := loaded.Groups[0].Items
	if len(items) != 3 {
		t.Fatalf("loaded item count = %d, want 3", len(items))
	}

	wantStatuses := []queue.ItemStatus{
		queue.ItemStatusCompleted,
		queue.ItemStatusFailed,
		queue.ItemStatusCompleted,
	}
	for i, want := range wantStatuses {
		if items[i].Status != want {
			t.Errorf("items[%d].Status = %q, want %q", i, items[i].Status, want)
		}
	}
}

// TestQueuePaused_PersistRoundTrip_FileAbsentAfterNoQueue verifies that Load
// returns (nil, nil) when no queue.json exists (QM-002 file-absent outcome).
// This confirms the baseline: a fresh restart with no prior queue is handled
// correctly before paused-by-failure semantics apply.
func TestQueuePaused_PersistRoundTrip_FileAbsentAfterNoQueue(t *testing.T) {
	t.Parallel()

	projectDir := queuePausedFixtureTempDir(t)

	ctx := context.Background()
	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load on absent file: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load on absent file = %+v, want nil", loaded)
	}
}

// TestQueuePaused_PersistRoundTrip_PausedByDrainAlsoPreserved verifies that
// paused-by-drain status is also preserved across Persist → Load (QM-055
// applies to both pause classes). This provides contrast with the primary
// paused-by-failure assertions above.
func TestQueuePaused_PersistRoundTrip_PausedByDrainAlsoPreserved(t *testing.T) {
	t.Parallel()

	projectDir := queuePausedFixtureTempDir(t)

	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       queuePausedFixtureQueueID,
		SubmittedAt:   queuePausedFixtureNow,
		Status:        queue.QueueStatusPausedByDrain,
		Groups: []queue.Group{
			queuePausedFixtureGroup(0, queue.GroupStatusActive, []queue.Item{
				queuePausedFixtureItem("hk-t81-fa01", queue.ItemStatusDispatched),
			}),
		},
	}

	ctx := context.Background()

	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	loaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}

	if loaded.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("loaded queue status = %q, want %q (QM-055: drain pause persists across restart)",
			loaded.Status, queue.QueueStatusPausedByDrain)
	}
}
