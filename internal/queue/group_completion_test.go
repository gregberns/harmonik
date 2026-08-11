package queue

import (
	"errors"
	"testing"
	"time"
)

func validGroupCompletionInput() GroupCompletionInput {
	return GroupCompletionInput{
		ExpectedQueueID: "0197c454-0000-7000-8000-000000000003",
		Location:        GroupCompletionLocation{GroupIndex: 1, ItemIndex: 2},
		Outcome:         GroupCompletionOutcomeCompleted,
		CompletedAt:     time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

func TestGroupCompletionInputValidationReasons(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GroupCompletionInput)
		want   GroupCompletionErrorReason
	}{
		{name: "outcome", mutate: func(v *GroupCompletionInput) { v.Outcome = "unknown" }, want: GroupCompletionErrorInvalidOutcome},
		{name: "queue ID", mutate: func(v *GroupCompletionInput) { v.ExpectedQueueID = "" }, want: GroupCompletionErrorInvalidQueueID},
		{name: "time", mutate: func(v *GroupCompletionInput) { v.CompletedAt = time.Time{} }, want: GroupCompletionErrorInvalidCompletionTime},
		{name: "group index", mutate: func(v *GroupCompletionInput) { v.Location.GroupIndex = -1 }, want: GroupCompletionErrorNegativeGroupIndex},
		{name: "item index", mutate: func(v *GroupCompletionInput) { v.Location.ItemIndex = -1 }, want: GroupCompletionErrorNegativeItemIndex},
		{name: "receipt ID", mutate: func(v *GroupCompletionInput) { v.CompletionReceiptID = "bad" }, want: GroupCompletionErrorInvalidReceiptID},
		{name: "unexpected receipt", mutate: func(v *GroupCompletionInput) {
			v.Outcome = GroupCompletionOutcomeFailed
			v.CompletionReceiptID = completionReceiptID
		}, want: GroupCompletionErrorUnexpectedReceiptID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validGroupCompletionInput()
			tc.mutate(&input)
			var got *GroupCompletionInputError
			if err := input.Validate(); !errors.As(err, &got) || got.Reason != tc.want {
				t.Fatalf("error = %#v, want reason %q", err, tc.want)
			}
		})
	}
	input := validGroupCompletionInput()
	input.CompletionReceiptID = completionReceiptID
	if err := input.Validate(); err != nil {
		t.Fatalf("valid input: %v", err)
	}
}

func TestGroupCompletionResultInvariants(t *testing.T) {
	valid := []GroupCompletionResult{
		{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: GroupCompletionNoChangeStaleQueue},
		{Disposition: GroupCompletionDispositionReceiptRequired},
		{Disposition: GroupCompletionDispositionIntermediate, Changed: true, NextQueue: &Queue{}},
		{Disposition: GroupCompletionDispositionPausedByFailure, Changed: true, NextQueue: &Queue{}},
		{Disposition: GroupCompletionDispositionSuccessorActivated, Changed: true, NextQueue: &Queue{}},
		{Disposition: GroupCompletionDispositionQueueCompleted, Changed: true, NextQueue: &Queue{}},
	}
	for _, result := range valid {
		if err := result.Validate(); err != nil {
			t.Errorf("valid %q result: %v", result.Disposition, err)
		}
	}

	invalid := []GroupCompletionResult{
		{},
		{Disposition: GroupCompletionDispositionNoChange},
		{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: "invented"},
		{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: GroupCompletionNoChangeStaleQueue, Changed: true},
		{Disposition: GroupCompletionDispositionReceiptRequired, NextQueue: &Queue{}},
		{Disposition: GroupCompletionDispositionIntermediate, Changed: true},
		{Disposition: GroupCompletionDispositionQueueCompleted, Changed: true, NextQueue: &Queue{}, NoChangeReason: GroupCompletionNoChangeStaleQueue},
	}
	for i, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Errorf("invalid result %d accepted: %+v", i, result)
		}
	}
}

func TestGroupCompletionTypedErrorsRetainFacts(t *testing.T) {
	location := GroupCompletionLocation{GroupIndex: 3, ItemIndex: 4}
	stateErr := &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidItemStatus, Location: location, QueueStatus: QueueStatusActive, GroupStatus: GroupStatusActive, ItemStatus: "bad"}
	var gotState *GroupCompletionStateError
	if !errors.As(stateErr, &gotState) || gotState.Location != location || gotState.ItemStatus != "bad" {
		t.Fatalf("state error lost facts: %+v", gotState)
	}
	conflictErr := &GroupCompletionConflictError{Location: location, Stored: ItemStatusFailed, Requested: GroupCompletionOutcomeCompleted}
	var gotConflict *GroupCompletionConflictError
	if !errors.As(conflictErr, &gotConflict) || gotConflict.Stored != ItemStatusFailed || gotConflict.Requested != GroupCompletionOutcomeCompleted {
		t.Fatalf("conflict error lost facts: %+v", gotConflict)
	}
}
