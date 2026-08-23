package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

const appendFixtureQueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099"

var appendFixtureAcceptedAt = time.Date(2026, 5, 16, 11, 12, 13, 456000000, time.FixedZone("test", -7*60*60))

type appendFixtureFakeLedger struct {
	statuses map[core.BeadID]queue.BeadStatus
	edges    map[[2]core.BeadID]bool
}

func (f *appendFixtureFakeLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s, ok := f.statuses[id]; ok {
		return s, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *appendFixtureFakeLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

func appendFixtureOpenLedger(ids ...string) *appendFixtureFakeLedger {
	m := make(map[core.BeadID]queue.BeadStatus, len(ids))
	for _, id := range ids {
		m[core.BeadID(id)] = queue.BeadStatusOpen
	}
	return &appendFixtureFakeLedger{
		statuses: m,
		edges:    map[[2]core.BeadID]bool{},
	}
}

func appendFixtureStreamQueue(groupStatus queue.GroupStatus, existingItems []queue.Item) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       appendFixtureQueueID,
		SubmittedAt:   time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     groupStatus,
				Items:      existingItems,
			},
		},
	}
}

func appendFixtureItem(beadID string, status queue.ItemStatus) queue.Item {
	return queue.Item{
		BeadID: core.BeadID(beadID),
		Status: status,
	}
}

func appendFixtureDecodePayload(t *testing.T, evt queue.EventIntent, dst interface{}) {
	t.Helper()
	if err := json.Unmarshal(evt.Payload, dst); err != nil {
		t.Fatalf("decode event payload for %q: %v", evt.Type, err)
	}
}

// TestAppendItemsQM040WaveReject verifies that AppendItems returns a validation
// error with reason append_target_invalid when the target group is a wave group
// (immutable post-submit per QM-040).
//
// Spec ref: queue-model.md §7.1 QM-040.
func TestAppendItemsQM040WaveReject(t *testing.T) {
	t.Parallel()

	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       appendFixtureQueueID,
		SubmittedAt:   time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					appendFixtureItem("hk-aaa01", queue.ItemStatusPending),
				},
			},
		},
	}

	ledger := appendFixtureOpenLedger("hk-new01")
	_, _, err := queue.AppendItems(context.Background(), q, 0, []string{"hk-new01"}, ledger, appendFixtureAcceptedAt)
	if err == nil {
		t.Fatal("expected validation error for wave group, got nil")
	}
	if !queue.IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if got := queue.ValidationReason(err); got != queue.ReasonAppendTargetInvalid {
		t.Errorf("reason = %q, want %q", got, queue.ReasonAppendTargetInvalid)
	}
}

// TestAppendItemsQM041TailAppend verifies that successful AppendItems places
// the new items at the tail of the stream group's items list, with
// status: pending, run_id: nil, and appended_at set.
//
// Spec ref: queue-model.md §7.2 QM-041.
func TestAppendItemsQM041TailAppend(t *testing.T) {
	t.Parallel()

	existing := []queue.Item{
		appendFixtureItem("hk-exist01", queue.ItemStatusPending),
	}
	q := appendFixtureStreamQueue(queue.GroupStatusActive, existing)

	ledger := appendFixtureOpenLedger("hk-new01", "hk-new02")
	result, events, err := queue.AppendItems(context.Background(), q, 0,
		[]string{"hk-new01", "hk-new02"}, ledger, appendFixtureAcceptedAt)
	if err != nil {
		t.Fatalf("AppendItems returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("AppendItems returned nil Queue")
	}

	items := result.Groups[0].Items
	if len(items) != 3 {
		t.Fatalf("want 3 items (1 existing + 2 appended), got %d", len(items))
	}

	if items[0].BeadID != "hk-exist01" {
		t.Errorf("items[0].BeadID = %q, want %q", items[0].BeadID, "hk-exist01")
	}

	for i, wantID := range []string{"hk-new01", "hk-new02"} {
		item := items[i+1]
		if string(item.BeadID) != wantID {
			t.Errorf("items[%d].BeadID = %q, want %q", i+1, item.BeadID, wantID)
		}
		if item.Status != queue.ItemStatusPending {
			t.Errorf("items[%d].Status = %q, want pending", i+1, item.Status)
		}
		if item.RunID != nil {
			t.Errorf("items[%d].RunID = %v, want nil", i+1, item.RunID)
		}
		if item.AppendedAt == nil {
			t.Errorf("items[%d].AppendedAt must not be nil for appended item", i+1)
		} else if want := appendFixtureAcceptedAt.UTC(); !item.AppendedAt.Equal(want) || item.AppendedAt.Location() != time.UTC {
			t.Errorf("items[%d].AppendedAt = %v, want exact UTC time %v", i+1, item.AppendedAt, want)
		}
	}

	if len(events) == 0 {
		t.Fatal("expected events, got none")
	}
}

// TestAppendItemsQM042EventEmitted verifies that AppendItems emits exactly one
// queue_appended event with the correct payload fields.
//
// Spec ref: queue-model.md §7.3 QM-042.
func TestAppendItemsQM042EventEmitted(t *testing.T) {
	t.Parallel()

	q := appendFixtureStreamQueue(queue.GroupStatusActive, []queue.Item{
		appendFixtureItem("hk-exist01", queue.ItemStatusPending),
	})

	ledger := appendFixtureOpenLedger("hk-new01", "hk-new02")
	_, events, err := queue.AppendItems(context.Background(), q, 0,
		[]string{"hk-new01", "hk-new02"}, ledger, appendFixtureAcceptedAt)
	if err != nil {
		t.Fatalf("AppendItems error: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	if events[0].Type != "queue_appended" {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, "queue_appended")
	}

	var payload core.QueueAppendedPayload
	appendFixtureDecodePayload(t, events[0], &payload)

	if payload.QueueID != appendFixtureQueueID {
		t.Errorf("payload.QueueID = %q, want %q", payload.QueueID, appendFixtureQueueID)
	}
	if payload.GroupIndex != 0 {
		t.Errorf("payload.GroupIndex = %d, want 0", payload.GroupIndex)
	}
	if len(payload.AppendedBeadIDs) != 2 {
		t.Fatalf("payload.AppendedBeadIDs len = %d, want 2", len(payload.AppendedBeadIDs))
	}
	if payload.AppendedBeadIDs[0] != "hk-new01" || payload.AppendedBeadIDs[1] != "hk-new02" {
		t.Errorf("payload.AppendedBeadIDs = %v, want [hk-new01 hk-new02]", payload.AppendedBeadIDs)
	}
	if want := appendFixtureAcceptedAt.UTC().Format(time.RFC3339Nano); payload.AppendedAt != want {
		t.Errorf("payload.AppendedAt = %q, want %q", payload.AppendedAt, want)
	}
	if !payload.Valid() {
		t.Error("payload.Valid() = false, want true")
	}

	if len(events) != 1 {
		t.Errorf("event count = %d, want 1 (no deferred items)", len(events))
	}
}

// TestAppendItemsQM042DeferredEventsAfterAppended verifies that when an
// appended item is QM-025-deferred (its blocker is in the same group and has
// a blocks edge), a queue_item_deferred_for_ledger_dep event is emitted AFTER
// queue_appended and the item's status is set to deferred-for-ledger-dep.
//
// Spec ref: queue-model.md §7.3 QM-042 (second paragraph).
func TestAppendItemsQM042DeferredEventsAfterAppended(t *testing.T) {
	t.Parallel()

	q := appendFixtureStreamQueue(queue.GroupStatusActive, []queue.Item{
		appendFixtureItem("hk-blocker", queue.ItemStatusPending),
	})

	ledger := &appendFixtureFakeLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			"hk-blocker": queue.BeadStatusOpen,
			"hk-blocked": queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{core.BeadID("hk-blocker"), core.BeadID("hk-blocked")}: true,
		},
	}

	_, events, err := queue.AppendItems(context.Background(), q, 0,
		[]string{"hk-blocked"}, ledger, appendFixtureAcceptedAt)
	if err != nil {
		t.Fatalf("AppendItems error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2 (appended + deferred)", len(events))
	}
	if events[0].Type != "queue_appended" {
		t.Errorf("events[0].Type = %q, want queue_appended", events[0].Type)
	}
	if events[1].Type != "queue_item_deferred_for_ledger_dep" {
		t.Errorf("events[1].Type = %q, want queue_item_deferred_for_ledger_dep", events[1].Type)
	}

	var deferredPayload core.QueueItemDeferredForLedgerDepPayload
	appendFixtureDecodePayload(t, events[1], &deferredPayload)
	if deferredPayload.BeadID != "hk-blocked" {
		t.Errorf("deferred payload BeadID = %q, want hk-blocked", deferredPayload.BeadID)
	}
	if deferredPayload.BlockerBeadID != "hk-blocker" {
		t.Errorf("deferred payload BlockerBeadID = %q, want hk-blocker", deferredPayload.BlockerBeadID)
	}
	if !deferredPayload.Valid() {
		t.Error("deferredPayload.Valid() = false")
	}

	appendedItem := q.Groups[0].Items[len(q.Groups[0].Items)-1]
	if appendedItem.Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("appended item status = %q, want deferred-for-ledger-dep", appendedItem.Status)
	}
}

func TestAppendItemsSeveralDeferredItemsKeepExactIntentOrder(t *testing.T) {
	t.Parallel()

	q := appendFixtureStreamQueue(queue.GroupStatusActive, []queue.Item{
		appendFixtureItem("hk-blocker", queue.ItemStatusPending),
	})
	ledger := &appendFixtureFakeLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			"hk-blocker": queue.BeadStatusOpen,
			"hk-first":   queue.BeadStatusOpen,
			"hk-second":  queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{core.BeadID("hk-blocker"), core.BeadID("hk-first")}:  true,
			{core.BeadID("hk-blocker"), core.BeadID("hk-second")}: true,
		},
	}

	_, intents, err := queue.AppendItems(t.Context(), q, 0, []string{"hk-first", "hk-second"}, ledger, appendFixtureAcceptedAt)
	if err != nil {
		t.Fatalf("AppendItems: %v", err)
	}
	if len(intents) != 3 {
		t.Fatalf("intent count = %d, want 3", len(intents))
	}
	wantTypes := []core.EventType{
		core.EventTypeQueueAppended,
		core.EventTypeQueueItemDeferredForLedgerDep,
		core.EventTypeQueueItemDeferredForLedgerDep,
	}
	for i, want := range wantTypes {
		if intents[i].Type != want {
			t.Fatalf("intent[%d].Type = %q, want %q", i, intents[i].Type, want)
		}
	}
	var first, second core.QueueItemDeferredForLedgerDepPayload
	appendFixtureDecodePayload(t, intents[1], &first)
	appendFixtureDecodePayload(t, intents[2], &second)
	if first.BeadID != "hk-first" || second.BeadID != "hk-second" {
		t.Fatalf("deferred order = [%q %q], want [hk-first hk-second]", first.BeadID, second.BeadID)
	}
	if first.DetectedAt != "2026-05-16T18:12:13.456Z" || second.DetectedAt != first.DetectedAt {
		t.Fatalf("detected times = [%q %q], want one supplied UTC time", first.DetectedAt, second.DetectedAt)
	}
}

// TestAppendItemsQM043ActiveInFlightSafe verifies that AppendItems succeeds
// when the stream group is active and contains in-flight (dispatched) items.
// The in-flight items must be unmodified after the append.
//
// Spec ref: queue-model.md §7.4 QM-043.
func TestAppendItemsQM043ActiveInFlightSafe(t *testing.T) {
	t.Parallel()

	runIDStr := "run-0001"
	existing := []queue.Item{
		{
			BeadID: "hk-inflight01",
			Status: queue.ItemStatusDispatched,
			RunID:  &runIDStr,
		},
	}
	q := appendFixtureStreamQueue(queue.GroupStatusActive, existing)

	ledger := appendFixtureOpenLedger("hk-new01")
	result, events, err := queue.AppendItems(context.Background(), q, 0,
		[]string{"hk-new01"}, ledger, appendFixtureAcceptedAt)
	if err != nil {
		t.Fatalf("AppendItems unexpectedly failed for active stream: %v", err)
	}

	if result.Groups[0].Items[0].Status != queue.ItemStatusDispatched {
		t.Errorf("in-flight item status changed: got %q, want dispatched",
			result.Groups[0].Items[0].Status)
	}
	if result.Groups[0].Items[0].RunID == nil || *result.Groups[0].Items[0].RunID != runIDStr {
		t.Errorf("in-flight item RunID changed; want %q", runIDStr)
	}

	if len(result.Groups[0].Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(result.Groups[0].Items))
	}
	if result.Groups[0].Items[1].BeadID != "hk-new01" {
		t.Errorf("appended item BeadID = %q, want hk-new01", result.Groups[0].Items[1].BeadID)
	}

	if len(events) == 0 || events[0].Type != "queue_appended" {
		t.Errorf("expected queue_appended event; got events: %v", events)
	}
}

// TestAppendItemsQM044TerminalGroupReject verifies that AppendItems rejects
// appends to a group that has already reached a terminal GroupStatus
// (complete-success or complete-with-failures) per QM-044.
//
// Spec ref: queue-model.md §7.5 QM-044.
func TestAppendItemsQM044TerminalGroupReject(t *testing.T) {
	t.Parallel()

	terminalStatuses := []queue.GroupStatus{
		queue.GroupStatusCompleteSuccess,
		queue.GroupStatusCompleteWithFailures,
	}

	for _, gs := range terminalStatuses {
		t.Run(string(gs), func(t *testing.T) {
			t.Parallel()

			q := appendFixtureStreamQueue(gs, []queue.Item{
				appendFixtureItem("hk-done01", queue.ItemStatusCompleted),
			})

			ledger := appendFixtureOpenLedger("hk-new01")
			_, _, err := queue.AppendItems(context.Background(), q, 0,
				[]string{"hk-new01"}, ledger, appendFixtureAcceptedAt)
			if err == nil {
				t.Fatalf("expected rejection for terminal group status %q, got nil", gs)
			}
			if !queue.IsValidationError(err) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if got := queue.ValidationReason(err); got != queue.ReasonAppendTargetInvalid {
				t.Errorf("reason = %q, want %q", got, queue.ReasonAppendTargetInvalid)
			}
		})
	}
}

// TestAppendItemsNilQueue verifies that AppendItems returns ErrAppendQueueNil
// when the queue argument is nil.
func TestAppendItemsNilQueue(t *testing.T) {
	t.Parallel()

	ledger := appendFixtureOpenLedger()
	_, _, err := queue.AppendItems(context.Background(), nil, 0, []string{"hk-x"}, ledger, appendFixtureAcceptedAt)
	if !errors.Is(err, queue.ErrAppendQueueNil) {
		t.Errorf("err = %v, want ErrAppendQueueNil", err)
	}
}

// TestAppendItemsEmptyBeadIDs verifies that AppendItems returns
// ErrAppendEmptyBeadIDs when the beadIDs slice is empty.
func TestAppendItemsEmptyBeadIDs(t *testing.T) {
	t.Parallel()

	q := appendFixtureStreamQueue(queue.GroupStatusActive, nil)
	ledger := appendFixtureOpenLedger()
	_, _, err := queue.AppendItems(context.Background(), q, 0, []string{}, ledger, appendFixtureAcceptedAt)
	if !errors.Is(err, queue.ErrAppendEmptyBeadIDs) {
		t.Errorf("err = %v, want ErrAppendEmptyBeadIDs", err)
	}
}
