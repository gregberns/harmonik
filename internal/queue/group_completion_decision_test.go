package queue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

func groupCompletionFixture(statuses ...ItemStatus) Queue {
	items := make([]Item, len(statuses))
	for i, status := range statuses {
		items[i] = Item{BeadID: core.BeadID("hk-item"), Status: status, TemplateParams: map[string]string{"fixed": "yes"}}
	}
	return Queue{
		SchemaVersion: 1,
		QueueID:       "0197c454-0000-7000-8000-000000000003",
		Name:          QueueNameMain,
		Status:        QueueStatusActive,
		Groups: []Group{
			{GroupIndex: 0, Kind: GroupKindWave, Status: GroupStatusActive, Items: items},
			{GroupIndex: 1, Kind: GroupKindWave, Status: GroupStatusPending, Items: []Item{{BeadID: "hk-next", Status: ItemStatusPending}}},
		},
	}
}

func groupCompletionInput(outcome GroupCompletionOutcome, item int) GroupCompletionInput {
	return GroupCompletionInput{
		ExpectedQueueID: "0197c454-0000-7000-8000-000000000003",
		Location:        GroupCompletionLocation{GroupIndex: 0, ItemIndex: item},
		Outcome:         outcome,
		CompletedAt:     time.Date(2026, 8, 10, 12, 0, 0, 987654321, time.FixedZone("offset", 3600)),
	}
}

func TestDecideGroupCompletionMainDispositions(t *testing.T) {
	t.Run("intermediate", func(t *testing.T) {
		prior := groupCompletionFixture(ItemStatusDispatched, ItemStatusDispatched)
		result, err := DecideGroupCompletion(prior, groupCompletionInput(GroupCompletionOutcomeCompleted, 0))
		if err != nil || result.Disposition != GroupCompletionDispositionIntermediate || !result.Changed {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if prior.Groups[0].Items[0].Status != ItemStatusDispatched || result.NextQueue.Groups[0].Items[0].Status != ItemStatusCompleted {
			t.Fatal("decision did not detach and change only its result")
		}
		result.NextQueue.Groups[0].Items[0].TemplateParams["fixed"] = "changed"
		if prior.Groups[0].Items[0].TemplateParams["fixed"] != "yes" {
			t.Fatal("decision result aliases input map")
		}
	})

	t.Run("paused", func(t *testing.T) {
		result, err := DecideGroupCompletion(groupCompletionFixture(ItemStatusCompleted, ItemStatusDispatched), groupCompletionInput(GroupCompletionOutcomeFailed, 1))
		if err != nil || result.Disposition != GroupCompletionDispositionPausedByFailure || result.NextQueue.Status != QueueStatusPausedByFailure || len(result.Intents) != 2 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if result.Intents[0].Type != core.EventTypeQueueGroupCompleted || result.Intents[1].Type != core.EventTypeQueuePaused {
			t.Fatalf("intent order = %q, %q", result.Intents[0].Type, result.Intents[1].Type)
		}
		var completed core.QueueGroupCompletedPayload
		if err := json.Unmarshal(result.Intents[0].Payload, &completed); err != nil || completed.CompletedAt != "2026-08-10T11:00:00.987Z" || completed.CompletionReceiptID != "" {
			t.Fatalf("completed payload=%+v err=%v", completed, err)
		}
		var paused core.QueuePausedPayload
		if err := json.Unmarshal(result.Intents[1].Payload, &paused); err != nil || paused.PausedAt != "2026-08-10T11:00:00.987Z" {
			t.Fatalf("paused payload=%+v err=%v", paused, err)
		}
	})

	t.Run("successor", func(t *testing.T) {
		result, err := DecideGroupCompletion(groupCompletionFixture(ItemStatusCompleted, ItemStatusDispatched), groupCompletionInput(GroupCompletionOutcomeCompleted, 1))
		if err != nil || result.Disposition != GroupCompletionDispositionSuccessorActivated || result.NextQueue.Groups[1].Status != GroupStatusActive || len(result.Intents) != 2 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if result.Intents[0].Type != core.EventTypeQueueGroupCompleted || result.Intents[1].Type != core.EventTypeQueueGroupStarted {
			t.Fatalf("intent order = %q, %q", result.Intents[0].Type, result.Intents[1].Type)
		}
		var completed core.QueueGroupCompletedPayload
		var started core.QueueGroupStartedPayload
		if err := json.Unmarshal(result.Intents[0].Payload, &completed); err != nil || completed.CompletedAt != "2026-08-10T11:00:00.987Z" || completed.CompletionReceiptID != "" {
			t.Fatalf("completed payload=%+v err=%v", completed, err)
		}
		if err := json.Unmarshal(result.Intents[1].Payload, &started); err != nil || started.StartedAt != "2026-08-10T11:00:00.987Z" {
			t.Fatalf("started payload=%+v err=%v", started, err)
		}
		want := time.Date(2026, 8, 10, 11, 0, 0, 987000000, time.UTC)
		if result.NextQueue.Groups[0].CompletedAt == nil || !result.NextQueue.Groups[0].CompletedAt.Equal(want) || result.NextQueue.Groups[1].StartedAt == nil || !result.NextQueue.Groups[1].StartedAt.Equal(want) {
			t.Fatalf("transition times = %v / %v", result.NextQueue.Groups[0].CompletedAt, result.NextQueue.Groups[1].StartedAt)
		}
	})

	t.Run("final receipt handshake", func(t *testing.T) {
		prior := groupCompletionFixture(ItemStatusCompleted)
		prior.Groups = prior.Groups[:1]
		input := groupCompletionInput(GroupCompletionOutcomeCompleted, 0)
		first, err := DecideGroupCompletion(prior, input)
		if err != nil || first.Disposition != GroupCompletionDispositionReceiptRequired || first.Changed {
			t.Fatalf("first=%+v err=%v", first, err)
		}
		input.CompletionReceiptID = completionReceiptID
		final, err := DecideGroupCompletion(prior, input)
		if err != nil || final.Disposition != GroupCompletionDispositionQueueCompleted || final.NextQueue.Status != QueueStatusCompleted || len(final.Intents) != 1 {
			t.Fatalf("final=%+v err=%v", final, err)
		}
		var payload core.QueueGroupCompletedPayload
		if err := json.Unmarshal(final.Intents[0].Payload, &payload); err != nil || payload.CompletionReceiptID != completionReceiptID {
			t.Fatalf("payload=%+v err=%v", payload, err)
		}
		if payload.CompletedAt != "2026-08-10T11:00:00.987Z" {
			t.Fatalf("final completed_at=%q", payload.CompletedAt)
		}
	})
}

func TestDecideGroupCompletionIdempotencyAndErrors(t *testing.T) {
	prior := groupCompletionFixture(ItemStatusFailed)
	result, err := DecideGroupCompletion(prior, groupCompletionInput(GroupCompletionOutcomeFailed, 0))
	if err != nil || result.Disposition != GroupCompletionDispositionPausedByFailure {
		t.Fatalf("matching terminal outcome did not finish aggregate transition: %+v %v", result, err)
	}

	terminal := groupCompletionFixture(ItemStatusFailed)
	terminal.Groups[0].Status = GroupStatusCompleteWithFailures
	terminal.Status = QueueStatusPausedByFailure
	sameID, err := DecideGroupCompletion(terminal, groupCompletionInput(GroupCompletionOutcomeFailed, 0))
	if err != nil || sameID.NoChangeReason != GroupCompletionNoChangeMatchingTerminalOutcome {
		t.Fatalf("same-ID terminal replay=%+v err=%v", sameID, err)
	}
	input := groupCompletionInput(GroupCompletionOutcomeFailed, 0)
	input.ExpectedQueueID = "0197c454-0000-7000-8000-000000000099"
	stale, err := DecideGroupCompletion(terminal, input)
	if err != nil || stale.NoChangeReason != GroupCompletionNoChangeStaleQueue {
		t.Fatalf("stale=%+v err=%v", stale, err)
	}

	conflictInput := groupCompletionInput(GroupCompletionOutcomeCompleted, 0)
	_, err = DecideGroupCompletion(prior, conflictInput)
	var conflict *GroupCompletionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("conflict error = %v", err)
	}

	bad := groupCompletionInput(GroupCompletionOutcomeCompleted, 4)
	_, err = DecideGroupCompletion(prior, bad)
	var stateErr *GroupCompletionStateError
	if !errors.As(err, &stateErr) || stateErr.Reason != GroupCompletionErrorItemIndexOutOfRange {
		t.Fatalf("state error = %#v", err)
	}

	matching := groupCompletionFixture(ItemStatusFailed, ItemStatusDispatched)
	unchanged, err := DecideGroupCompletion(matching, groupCompletionInput(GroupCompletionOutcomeFailed, 0))
	if err != nil || unchanged.NoChangeReason != GroupCompletionNoChangeMatchingTerminalOutcome || unchanged.Changed {
		t.Fatalf("matching no-change = %+v, err=%v", unchanged, err)
	}

	withReceipt := groupCompletionInput(GroupCompletionOutcomeCompleted, 0)
	withReceipt.CompletionReceiptID = completionReceiptID
	_, err = DecideGroupCompletion(groupCompletionFixture(ItemStatusDispatched, ItemStatusDispatched), withReceipt)
	var inputErr *GroupCompletionInputError
	if !errors.As(err, &inputErr) || inputErr.Reason != GroupCompletionErrorUnexpectedReceiptID {
		t.Fatalf("unexpected receipt error = %#v", err)
	}
	pausedReceipt := groupCompletionInput(GroupCompletionOutcomeCompleted, 1)
	pausedReceipt.CompletionReceiptID = completionReceiptID
	_, err = DecideGroupCompletion(groupCompletionFixture(ItemStatusFailed, ItemStatusDispatched), pausedReceipt)
	if !errors.As(err, &inputErr) || inputErr.Reason != GroupCompletionErrorUnexpectedReceiptID {
		t.Fatalf("paused receipt error = %#v", err)
	}
	terminalSuccess := groupCompletionFixture(ItemStatusCompleted)
	terminalSuccess.Groups[0].Status = GroupStatusCompleteSuccess
	terminalSuccess.Status = QueueStatusCompleted
	terminalReceipt := groupCompletionInput(GroupCompletionOutcomeCompleted, 0)
	terminalReceipt.CompletionReceiptID = completionReceiptID
	_, err = DecideGroupCompletion(terminalSuccess, terminalReceipt)
	if !errors.As(err, &inputErr) || inputErr.Reason != GroupCompletionErrorUnexpectedReceiptID {
		t.Fatalf("terminal receipt error = %#v", err)
	}
}

func TestDecideGroupCompletionCorruptStateReasons(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Queue)
		want   GroupCompletionErrorReason
	}{
		{name: "queue status", mutate: func(q *Queue) { q.Status = QueueStatusCancelled }, want: GroupCompletionErrorInvalidQueueStatus},
		{name: "missing group", mutate: func(q *Queue) { q.Groups[0].GroupIndex = 9 }, want: GroupCompletionErrorMissingGroup},
		{name: "duplicate group", mutate: func(q *Queue) { q.Groups[1].GroupIndex = 0 }, want: GroupCompletionErrorDuplicateGroup},
		{name: "group status", mutate: func(q *Queue) { q.Groups[0].Status = GroupStatusPending }, want: GroupCompletionErrorInvalidGroupStatus},
		{name: "item status", mutate: func(q *Queue) { q.Groups[0].Items[0].Status = "corrupt" }, want: GroupCompletionErrorInvalidItemStatus},
		{name: "sibling item status", mutate: func(q *Queue) { q.Groups[1].Items[0].Status = "corrupt" }, want: GroupCompletionErrorInvalidItemStatus},
		{name: "non-dense group", mutate: func(q *Queue) { q.Groups[1].GroupIndex = 2 }, want: GroupCompletionErrorInvalidGroupIndex},
		{name: "later group status", mutate: func(q *Queue) { q.Groups[1].Status = GroupStatusActive }, want: GroupCompletionErrorInvalidGroupStatus},
		{name: "successor group kind", mutate: func(q *Queue) { q.Groups[1].Kind = "corrupt" }, want: GroupCompletionErrorInvalidGroupKind},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prior := groupCompletionFixture(ItemStatusDispatched)
			tc.mutate(&prior)
			_, err := DecideGroupCompletion(prior, groupCompletionInput(GroupCompletionOutcomeCompleted, 0))
			var got *GroupCompletionStateError
			if !errors.As(err, &got) || got.Reason != tc.want {
				t.Fatalf("error=%#v, want %q", err, tc.want)
			}
		})
	}
}
