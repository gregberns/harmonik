package queue_test

// state_test.go — transition-table conformance tests for the group state
// machine (specs/queue-model.md §5).
//
// Coverage:
//   - Every row in §5.1 transition table (pending→active, active→complete-success,
//     active→complete-with-failures, terminal no-op).
//   - QM-030: all-terminal gate blocks active→terminal until every item is done.
//   - QM-031: pending→active guard: queue must be active.
//   - QM-032: no re-entry of terminal states.
//   - QM-034: failed items do not interrupt sibling dispatches
//     (active group with in-flight items stays active).
//   - QM-035: stream out-of-order dispatch — deferred items skipped, not HOL-blocking (hk-cb5ow, hk-9a27q).
//   - QM-036: wave unordered admission with deferred siblings skipped.
//   - ErrGroupNil / ErrQueueIDEmpty sentinel errors.
//
// Helper prefix: stateFixture (derived from "state.go" concept per
// implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// -----------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------

const stateFixtureQueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001"

var stateFixtureNow = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

// stateFixtureItem returns an Item with the given BeadID and status.
func stateFixtureItem(beadID string, status queue.ItemStatus) queue.Item {
	return queue.Item{
		BeadID: core.BeadID(beadID),
		Status: status,
	}
}

// stateFixtureGroup builds a minimal Group record for a wave or stream.
func stateFixtureGroup(idx int, kind queue.GroupKind, status queue.GroupStatus, items []queue.Item) queue.Group {
	return queue.Group{
		GroupIndex: idx,
		Kind:       kind,
		Status:     status,
		Items:      items,
		CreatedAt:  stateFixtureNow,
	}
}

// stateFixtureAdvance is a convenience wrapper that calls AdvanceGroup with
// stateFixtureNow and stateFixtureQueueID.
func stateFixtureAdvance(
	t *testing.T,
	g *queue.Group,
	qs queue.QueueStatus,
) (queue.GroupStatus, []core.Event) {
	t.Helper()
	newStatus, events, err := queue.AdvanceGroup(
		context.Background(),
		g,
		qs,
		stateFixtureQueueID,
		stateFixtureNow,
	)
	if err != nil {
		t.Fatalf("AdvanceGroup: unexpected error: %v", err)
	}
	return newStatus, events
}

// stateFixtureEventType returns the Type field of events[i], failing if out of bounds.
func stateFixtureEventType(t *testing.T, events []core.Event, i int) string {
	t.Helper()
	if i >= len(events) {
		t.Fatalf("expected at least %d event(s), got %d", i+1, len(events))
	}
	return events[i].Type
}

// stateFixturePayloadFinalStatus unmarshals the FinalStatus field from a
// queue_group_completed event's payload, failing on any error.
func stateFixturePayloadFinalStatus(t *testing.T, e core.Event) string {
	t.Helper()
	var p core.QueueGroupCompletedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("unmarshal QueueGroupCompletedPayload: %v", err)
	}
	return p.FinalStatus
}

// stateFixturePayloadPausedReason unmarshals the Reason field from a
// queue_paused event's payload, failing on any error.
func stateFixturePayloadPausedReason(t *testing.T, e core.Event) string {
	t.Helper()
	var p core.QueuePausedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("unmarshal QueuePausedPayload: %v", err)
	}
	return p.Reason
}

// -----------------------------------------------------------------------
// §5.1 row 1 — pending → active (queue-submit: group_index 0)
// -----------------------------------------------------------------------

// TestAdvanceGroup_PendingToActive_QueueActive verifies that a pending group
// transitions to active when the queue is active (QM-031).
func TestAdvanceGroup_PendingToActive_QueueActive(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusPending, []queue.Item{
		stateFixtureItem("hk-aaa01", queue.ItemStatusPending),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusActive)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if got := stateFixtureEventType(t, events, 0); got != "queue_group_started" {
		t.Errorf("events[0].Type = %q, want %q", got, "queue_group_started")
	}
}

// TestAdvanceGroup_PendingToActive_QueuePausedByFailure verifies that a
// pending group does NOT advance when the queue is paused-by-failure (QM-031).
func TestAdvanceGroup_PendingToActive_QueuePausedByFailure(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(1, queue.GroupKindWave, queue.GroupStatusPending, []queue.Item{
		stateFixtureItem("hk-aaa02", queue.ItemStatusPending),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusPausedByFailure)

	if newStatus != queue.GroupStatusPending {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusPending)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// TestAdvanceGroup_PendingToActive_QueuePausedByDrain verifies that a pending
// group does NOT advance when the queue is paused-by-drain (QM-031).
func TestAdvanceGroup_PendingToActive_QueuePausedByDrain(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(1, queue.GroupKindStream, queue.GroupStatusPending, nil)
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusPausedByDrain)

	if newStatus != queue.GroupStatusPending {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusPending)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// -----------------------------------------------------------------------
// §5.1 row 3 — active → complete-success
// -----------------------------------------------------------------------

// TestAdvanceGroup_ActiveToCompleteSuccess verifies that an active group with
// all completed items transitions to complete-success (QM-030).
func TestAdvanceGroup_ActiveToCompleteSuccess(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-bbb01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-bbb02", queue.ItemStatusCompleted),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteSuccess {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteSuccess)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if got := stateFixtureEventType(t, events, 0); got != "queue_group_completed" {
		t.Errorf("events[0].Type = %q, want %q", got, "queue_group_completed")
	}
	if fs := stateFixturePayloadFinalStatus(t, events[0]); fs != "complete-success" {
		t.Errorf("final_status = %q, want %q", fs, "complete-success")
	}
}

// TestAdvanceGroup_ActiveToCompleteSuccess_EmptyItems verifies that an active
// group with zero items (vacuously all-terminal) transitions to complete-success.
func TestAdvanceGroup_ActiveToCompleteSuccess_EmptyItems(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, nil)
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteSuccess {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteSuccess)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
}

// -----------------------------------------------------------------------
// §5.1 row 4 — active → complete-with-failures
// -----------------------------------------------------------------------

// TestAdvanceGroup_ActiveToCompleteWithFailures verifies that an active group
// with at least one failed item transitions to complete-with-failures and emits
// both queue_group_completed and queue_paused in order (§5.1 row 4, QM-052).
func TestAdvanceGroup_ActiveToCompleteWithFailures(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-ccc01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-ccc02", queue.ItemStatusFailed),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if got := stateFixtureEventType(t, events, 0); got != "queue_group_completed" {
		t.Errorf("events[0].Type = %q, want %q", got, "queue_group_completed")
	}
	if fs := stateFixturePayloadFinalStatus(t, events[0]); fs != "complete-with-failures" {
		t.Errorf("final_status = %q, want %q", fs, "complete-with-failures")
	}
	if got := stateFixtureEventType(t, events, 1); got != "queue_paused" {
		t.Errorf("events[1].Type = %q, want %q", got, "queue_paused")
	}
	if reason := stateFixturePayloadPausedReason(t, events[1]); reason != "group_failure" {
		t.Errorf("queue_paused.reason = %q, want %q", reason, "group_failure")
	}
}

// TestAdvanceGroup_ActiveToCompleteWithFailures_AllFailed verifies the
// complete-with-failures path when every item failed.
func TestAdvanceGroup_ActiveToCompleteWithFailures_AllFailed(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-ddd01", queue.ItemStatusFailed),
		stateFixtureItem("hk-ddd02", queue.ItemStatusFailed),
	})
	newStatus, _, _ := queue.AdvanceGroup(
		context.Background(), &g, queue.QueueStatusActive,
		stateFixtureQueueID, stateFixtureNow,
	)
	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
}

// -----------------------------------------------------------------------
// QM-030 — all-terminal gate
// -----------------------------------------------------------------------

// TestAdvanceGroup_QM030_DispatchedBlocksTransition verifies that an active
// group with a still-dispatched item stays active (QM-030, QM-034).
func TestAdvanceGroup_QM030_DispatchedBlocksTransition(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-eee01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-eee02", queue.ItemStatusDispatched), // still in flight
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("newStatus = %q, want %q (dispatched item should block transition)", newStatus, queue.GroupStatusActive)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// TestAdvanceGroup_QM030_DeferredBlocksTransition verifies that a
// deferred-for-ledger-dep item is NOT terminal and blocks group completion
// per §2.8 normative sentence.
func TestAdvanceGroup_QM030_DeferredBlocksTransition(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-fff01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-fff02", queue.ItemStatusDeferredForLedgerDep),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("newStatus = %q, want %q (deferred item should block transition)", newStatus, queue.GroupStatusActive)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// TestAdvanceGroup_QM030_PendingBlocksTransition verifies that a still-pending
// item blocks group completion.
func TestAdvanceGroup_QM030_PendingBlocksTransition(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-ggg01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-ggg02", queue.ItemStatusPending),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("newStatus = %q, want %q (pending item should block transition)", newStatus, queue.GroupStatusActive)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// -----------------------------------------------------------------------
// QM-032 — no re-entry of terminal states
// -----------------------------------------------------------------------

// TestAdvanceGroup_QM032_CompleteSuccessIsAbsorbing verifies that calling
// AdvanceGroup on a complete-success group returns unchanged with no events.
func TestAdvanceGroup_QM032_CompleteSuccessIsAbsorbing(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusCompleteSuccess, []queue.Item{
		stateFixtureItem("hk-hhh01", queue.ItemStatusCompleted),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteSuccess {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteSuccess)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 (terminal state must be absorbing)", len(events))
	}
}

// TestAdvanceGroup_QM032_CompleteWithFailuresIsAbsorbing verifies that calling
// AdvanceGroup on a complete-with-failures group returns unchanged with no events.
func TestAdvanceGroup_QM032_CompleteWithFailuresIsAbsorbing(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusCompleteWithFailures, []queue.Item{
		stateFixtureItem("hk-iii01", queue.ItemStatusFailed),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0 (terminal state must be absorbing)", len(events))
	}
}

// -----------------------------------------------------------------------
// QM-034 — failed items do not interrupt sibling dispatches
// -----------------------------------------------------------------------

// TestAdvanceGroup_QM034_FailedSiblingDoesNotInterrupt verifies that a group
// with one failed item and one still-dispatched sibling remains active.
// The group is NOT terminal while the sibling is in flight.
func TestAdvanceGroup_QM034_FailedSiblingDoesNotInterrupt(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-jjj01", queue.ItemStatusFailed),
		stateFixtureItem("hk-jjj02", queue.ItemStatusDispatched), // sibling still running
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusActive {
		t.Errorf("newStatus = %q, want active — failed sibling must not interrupt in-flight siblings (QM-034)", newStatus)
	}
	if len(events) != 0 {
		t.Errorf("event count = %d, want 0", len(events))
	}
}

// TestAdvanceGroup_QM034_AllTerminalWithFailure verifies that once the in-flight
// sibling resolves, the group transitions to complete-with-failures.
func TestAdvanceGroup_QM034_AllTerminalWithFailure(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-kkk01", queue.ItemStatusFailed),
		stateFixtureItem("hk-kkk02", queue.ItemStatusCompleted), // sibling resolved
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("newStatus = %q, want %q", newStatus, queue.GroupStatusCompleteWithFailures)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
}

// -----------------------------------------------------------------------
// QM-035 — stream out-of-order dispatch (hk-cb5ow)
// -----------------------------------------------------------------------

// TestEligibleItems_Stream_DeferredHeadSkipped verifies that a stream with a
// deferred-for-ledger-dep head item skips it and returns the next pending
// tail item (QM-035 + hk-cb5ow out-of-order dispatch fix).
func TestEligibleItems_Stream_DeferredHeadSkipped(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-lll01", queue.ItemStatusDeferredForLedgerDep), // head: deferred, skipped
		stateFixtureItem("hk-lll02", queue.ItemStatusPending),              // tail: dep-free, eligible
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems = %d items, want 1 (deferred head skipped, tail pending eligible)", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-lll02" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-lll02")
	}
}

// TestEligibleItems_Stream_MultipleDeferredThenPending verifies that multiple
// consecutive deferred items are all skipped and the first pending tail item
// is returned (hk-cb5ow).
func TestEligibleItems_Stream_MultipleDeferredThenPending(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-lll03", queue.ItemStatusDeferredForLedgerDep), // deferred, skipped
		stateFixtureItem("hk-lll04", queue.ItemStatusDeferredForLedgerDep), // deferred, skipped
		stateFixtureItem("hk-lll05", queue.ItemStatusPending),              // first pending: eligible
		stateFixtureItem("hk-lll06", queue.ItemStatusPending),              // second pending: not returned
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems = %d items, want 1 (only first pending after deferred items)", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-lll05" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-lll05")
	}
}

// TestEligibleItems_Stream_AllDeferred verifies that a stream where all items
// are deferred returns nil (nothing to dispatch).
func TestEligibleItems_Stream_AllDeferred(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-lll07", queue.ItemStatusDeferredForLedgerDep),
		stateFixtureItem("hk-lll08", queue.ItemStatusDeferredForLedgerDep),
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 0 {
		t.Errorf("EligibleItems = %d items, want 0 (all items deferred)", len(eligible))
	}
}

// TestEligibleItems_Stream_DispatchedHeadSkipped verifies that a stream with
// an in-flight (dispatched) head skips to the next pending item (QM-035,
// hk-9a27q). A dispatched head does NOT HOL-block subsequent pending items;
// this enables --max-concurrent > 1 on stream groups.
func TestEligibleItems_Stream_DispatchedHeadSkipped(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-mmm01", queue.ItemStatusDispatched), // head: in flight — skipped
		stateFixtureItem("hk-mmm02", queue.ItemStatusPending),    // tail: eligible
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems = %d items, want 1 (dispatched head skipped, tail pending eligible)", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-mmm02" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-mmm02")
	}
}

// TestEligibleItems_Stream_HeadPending verifies that a stream with a pending
// head item returns exactly that item (QM-035).
func TestEligibleItems_Stream_HeadPending(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-nnn01", queue.ItemStatusPending), // head: eligible
		stateFixtureItem("hk-nnn02", queue.ItemStatusPending), // tail: not returned
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems = %d items, want 1 (only head)", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-nnn01" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-nnn01")
	}
}

// TestEligibleItems_Stream_SkipTerminatedHead verifies that a stream with
// a completed head skips to the next non-terminal item.
func TestEligibleItems_Stream_SkipTerminatedHead(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindStream, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-ooo01", queue.ItemStatusCompleted), // head: terminal, skip
		stateFixtureItem("hk-ooo02", queue.ItemStatusPending),   // first non-terminal: eligible
		stateFixtureItem("hk-ooo03", queue.ItemStatusPending),   // tail: not returned
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 1 {
		t.Fatalf("EligibleItems = %d items, want 1", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-ooo02" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-ooo02")
	}
}

// -----------------------------------------------------------------------
// QM-036 — wave unordered admission
// -----------------------------------------------------------------------

// TestEligibleItems_Wave_AllPending verifies that a wave returns all pending
// items; order is preserved from the items list (QM-036).
func TestEligibleItems_Wave_AllPending(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-ppp01", queue.ItemStatusPending),
		stateFixtureItem("hk-ppp02", queue.ItemStatusPending),
		stateFixtureItem("hk-ppp03", queue.ItemStatusPending),
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 3 {
		t.Errorf("EligibleItems = %d items, want 3", len(eligible))
	}
}

// TestEligibleItems_Wave_SkipDeferred verifies that deferred-for-ledger-dep
// siblings are skipped while non-deferred siblings remain eligible (QM-036).
func TestEligibleItems_Wave_SkipDeferred(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-qqq01", queue.ItemStatusPending),              // eligible
		stateFixtureItem("hk-qqq02", queue.ItemStatusDeferredForLedgerDep), // skip
		stateFixtureItem("hk-qqq03", queue.ItemStatusPending),              // eligible
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 2 {
		t.Fatalf("EligibleItems = %d items, want 2", len(eligible))
	}
	if string(eligible[0].BeadID) != "hk-qqq01" {
		t.Errorf("eligible[0].BeadID = %q, want %q", eligible[0].BeadID, "hk-qqq01")
	}
	if string(eligible[1].BeadID) != "hk-qqq03" {
		t.Errorf("eligible[1].BeadID = %q, want %q", eligible[1].BeadID, "hk-qqq03")
	}
}

// TestEligibleItems_Wave_NoneEligibleWhenAllDispatched verifies that a wave
// with all dispatched items returns no eligible items.
func TestEligibleItems_Wave_NoneEligibleWhenAllDispatched(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-rrr01", queue.ItemStatusDispatched),
		stateFixtureItem("hk-rrr02", queue.ItemStatusDispatched),
	})
	eligible := queue.EligibleItems(&g)
	if len(eligible) != 0 {
		t.Errorf("EligibleItems = %d items, want 0 (all dispatched)", len(eligible))
	}
}

// -----------------------------------------------------------------------
// EligibleItems — nil and inactive group guards
// -----------------------------------------------------------------------

// TestEligibleItems_NilGroup verifies that EligibleItems returns nil for a nil
// group.
func TestEligibleItems_NilGroup(t *testing.T) {
	t.Parallel()
	if eligible := queue.EligibleItems(nil); eligible != nil {
		t.Errorf("EligibleItems(nil) = %v, want nil", eligible)
	}
}

// TestEligibleItems_PendingGroup verifies that EligibleItems returns nil for a
// group that is not yet active.
func TestEligibleItems_PendingGroup(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusPending, []queue.Item{
		stateFixtureItem("hk-sss01", queue.ItemStatusPending),
	})
	if eligible := queue.EligibleItems(&g); eligible != nil {
		t.Errorf("EligibleItems on pending group = %v, want nil", eligible)
	}
}

// -----------------------------------------------------------------------
// Sentinel error cases
// -----------------------------------------------------------------------

// TestAdvanceGroup_NilGroup verifies that AdvanceGroup returns ErrGroupNil for
// a nil group.
func TestAdvanceGroup_NilGroup(t *testing.T) {
	t.Parallel()
	_, _, err := queue.AdvanceGroup(
		context.Background(), nil,
		queue.QueueStatusActive, stateFixtureQueueID, stateFixtureNow,
	)
	if err != queue.ErrGroupNil {
		t.Errorf("err = %v, want ErrGroupNil", err)
	}
}

// TestAdvanceGroup_EmptyQueueID verifies that AdvanceGroup returns
// ErrQueueIDEmpty when queueID is empty.
func TestAdvanceGroup_EmptyQueueID(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusPending, nil)
	_, _, err := queue.AdvanceGroup(
		context.Background(), &g,
		queue.QueueStatusActive, "", stateFixtureNow,
	)
	if err != queue.ErrQueueIDEmpty {
		t.Errorf("err = %v, want ErrQueueIDEmpty", err)
	}
}

// TestAdvanceGroup_CancelledContext verifies that AdvanceGroup respects
// context cancellation.
func TestAdvanceGroup_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusPending, nil)
	_, _, err := queue.AdvanceGroup(ctx, &g, queue.QueueStatusActive, stateFixtureQueueID, stateFixtureNow)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// -----------------------------------------------------------------------
// Event shape checks — ensure returned events carry correct payload fields
// -----------------------------------------------------------------------

// TestAdvanceGroup_GroupStartedPayload verifies the queue_group_started payload
// fields are populated correctly from the Group record.
func TestAdvanceGroup_GroupStartedPayload(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(2, queue.GroupKindStream, queue.GroupStatusPending, []queue.Item{
		stateFixtureItem("hk-ttt01", queue.ItemStatusPending),
		stateFixtureItem("hk-ttt02", queue.ItemStatusPending),
	})
	_, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	var p core.QueueGroupStartedPayload
	if err := json.Unmarshal(events[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.QueueID != stateFixtureQueueID {
		t.Errorf("QueueID = %q, want %q", p.QueueID, stateFixtureQueueID)
	}
	if p.GroupIndex != 2 {
		t.Errorf("GroupIndex = %d, want 2", p.GroupIndex)
	}
	if p.GroupKind != "stream" {
		t.Errorf("GroupKind = %q, want %q", p.GroupKind, "stream")
	}
	if p.ItemCount != 2 {
		t.Errorf("ItemCount = %d, want 2", p.ItemCount)
	}
	if p.StartedAt == "" {
		t.Error("StartedAt is empty")
	}
}

// TestAdvanceGroup_GroupCompletedPayload verifies the queue_group_completed
// payload counts are correct.
func TestAdvanceGroup_GroupCompletedPayload(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusActive, []queue.Item{
		stateFixtureItem("hk-uuu01", queue.ItemStatusCompleted),
		stateFixtureItem("hk-uuu02", queue.ItemStatusCompleted),
		stateFixtureItem("hk-uuu03", queue.ItemStatusFailed),
	})
	newStatus, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)

	if newStatus != queue.GroupStatusCompleteWithFailures {
		t.Errorf("newStatus = %q, want complete-with-failures", newStatus)
	}
	var p core.QueueGroupCompletedPayload
	if err := json.Unmarshal(events[0].Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", p.SuccessCount)
	}
	if p.FailCount != 1 {
		t.Errorf("FailCount = %d, want 1", p.FailCount)
	}
}

// TestAdvanceGroup_EventEnvelope verifies the common Event envelope fields
// (SourceSubsystem, SchemaVersion, Type, non-nil Payload).
func TestAdvanceGroup_EventEnvelope(t *testing.T) {
	t.Parallel()
	g := stateFixtureGroup(0, queue.GroupKindWave, queue.GroupStatusPending, []queue.Item{
		stateFixtureItem("hk-vvv01", queue.ItemStatusPending),
	})
	_, events := stateFixtureAdvance(t, &g, queue.QueueStatusActive)
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	e := events[0]
	if e.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", e.SchemaVersion)
	}
	if e.SourceSubsystem == "" {
		t.Error("SourceSubsystem is empty")
	}
	if e.Payload == nil {
		t.Error("Payload is nil")
	}
}
