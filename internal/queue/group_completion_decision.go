package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// DecideGroupCompletion applies one terminal item outcome to a detached queue
// value and returns the full queue transition without performing effects.
func DecideGroupCompletion(prior Queue, input GroupCompletionInput) (GroupCompletionResult, error) {
	if err := input.Validate(); err != nil {
		return GroupCompletionResult{}, err
	}
	if prior.QueueID != input.ExpectedQueueID {
		return GroupCompletionResult{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: GroupCompletionNoChangeStaleQueue}, nil
	}
	resolved, early, err := resolveGroupCompletionTarget(prior, input)
	if err != nil || early != nil {
		if early != nil {
			return *early, err
		}
		return GroupCompletionResult{}, err
	}

	next := CloneQueue(&prior)
	groupPos := resolved.groupPos
	target := &next.Groups[groupPos]
	itemChanged := target.Items[input.Location.ItemIndex].Status != resolved.itemStatus
	if err := CompleteItem(&target.Items[input.Location.ItemIndex], input.Outcome); err != nil {
		return GroupCompletionResult{}, err
	}
	stamp := input.CompletedAt.UTC().Truncate(time.Millisecond)
	newStatus, intents, err := AdvanceGroup(context.Background(), target, next.Status, next.QueueID, stamp)
	if err != nil {
		return GroupCompletionResult{}, fmt.Errorf("queue: decide group completion: %w", err)
	}
	return decideAdvancedGroupCompletion(next, groupPos, input, itemChanged, stamp, newStatus, intents)
}

func decideAdvancedGroupCompletion(
	next *Queue,
	groupPos int,
	input GroupCompletionInput,
	itemChanged bool,
	stamp time.Time,
	newStatus GroupStatus,
	intents []EventIntent,
) (GroupCompletionResult, error) {
	target := &next.Groups[groupPos]
	if newStatus == GroupStatusActive {
		return decideIntermediateGroupCompletion(next, input, itemChanged, intents)
	}
	if err := CompleteActiveGroup(target, stamp); err != nil {
		return GroupCompletionResult{}, fmt.Errorf("queue: apply group completion status %q: %w", newStatus, err)
	}
	if target.Status != newStatus {
		return GroupCompletionResult{}, fmt.Errorf("queue: group completion status %q does not match decision %q", target.Status, newStatus)
	}
	if newStatus == GroupStatusCompleteWithFailures {
		return decideFailedGroupCompletion(next, target, input, intents)
	}
	return decideSuccessfulGroupCompletion(next, groupPos, input, stamp, intents)
}

func decideIntermediateGroupCompletion(next *Queue, input GroupCompletionInput, itemChanged bool, intents []EventIntent) (GroupCompletionResult, error) {
	if input.CompletionReceiptID != "" {
		return GroupCompletionResult{}, unexpectedCompletionReceipt(input)
	}
	if !itemChanged {
		return GroupCompletionResult{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: GroupCompletionNoChangeMatchingTerminalOutcome}, nil
	}
	return changedCompletionResult(next, intents, GroupCompletionDispositionIntermediate), nil
}

func decideFailedGroupCompletion(next *Queue, target *Group, input GroupCompletionInput, intents []EventIntent) (GroupCompletionResult, error) {
	if input.CompletionReceiptID != "" {
		return GroupCompletionResult{}, unexpectedCompletionReceipt(input)
	}
	if err := PauseQueueForGroupFailure(next, target.GroupIndex); err != nil {
		return GroupCompletionResult{}, err
	}
	return changedCompletionResult(next, intents, GroupCompletionDispositionPausedByFailure), nil
}

func decideSuccessfulGroupCompletion(next *Queue, groupPos int, input GroupCompletionInput, stamp time.Time, intents []EventIntent) (GroupCompletionResult, error) {
	if groupPos+1 < len(next.Groups) {
		i := groupPos + 1
		if next.Groups[i].Status != GroupStatusPending {
			return GroupCompletionResult{}, &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidGroupStatus, Location: GroupCompletionLocation{GroupIndex: next.Groups[i].GroupIndex, ItemIndex: -1}, QueueStatus: next.Status, GroupStatus: next.Groups[i].Status}
		}
		if input.CompletionReceiptID != "" {
			return GroupCompletionResult{}, unexpectedCompletionReceipt(input)
		}
		successorStatus, successorIntents, successorErr := AdvanceGroup(context.Background(), &next.Groups[i], next.Status, next.QueueID, stamp)
		if successorErr != nil {
			return GroupCompletionResult{}, fmt.Errorf("queue: decide successor group: %w", successorErr)
		}
		if successorStatus == GroupStatusActive {
			if err := ActivatePendingGroup(&next.Groups[i], stamp); err != nil {
				return GroupCompletionResult{}, err
			}
		}
		intents = append(intents, successorIntents...)
		return changedCompletionResult(next, intents, GroupCompletionDispositionSuccessorActivated), nil
	}
	for i := range next.Groups {
		if next.Groups[i].Status != GroupStatusCompleteSuccess {
			return GroupCompletionResult{}, &GroupCompletionStateError{
				Reason: GroupCompletionErrorInvalidGroupStatus, Location: GroupCompletionLocation{GroupIndex: next.Groups[i].GroupIndex, ItemIndex: -1},
				QueueStatus: next.Status, GroupStatus: next.Groups[i].Status,
			}
		}
	}

	if input.CompletionReceiptID == "" {
		return GroupCompletionResult{Disposition: GroupCompletionDispositionReceiptRequired}, nil
	}
	bound, err := bindFinalCompletionReceipt(intents, input.CompletionReceiptID)
	if err != nil {
		return GroupCompletionResult{}, err
	}
	if err := CompleteQueue(next); err != nil {
		return GroupCompletionResult{}, err
	}
	return changedCompletionResult(next, bound, GroupCompletionDispositionQueueCompleted), nil
}

type groupCompletionTarget struct {
	groupPos   int
	itemStatus ItemStatus
}

func resolveGroupCompletionTarget(prior Queue, input GroupCompletionInput) (groupCompletionTarget, *GroupCompletionResult, error) {
	location := input.Location
	stateError := func(reason GroupCompletionErrorReason, group GroupStatus, item ItemStatus) error {
		return &GroupCompletionStateError{Reason: reason, Location: location, QueueStatus: prior.Status, GroupStatus: group, ItemStatus: item}
	}
	groupPos := -1
	seen := make(map[int]struct{}, len(prior.Groups))
	for i := range prior.Groups {
		if _, exists := seen[prior.Groups[i].GroupIndex]; exists {
			return groupCompletionTarget{}, nil, stateError(GroupCompletionErrorDuplicateGroup, prior.Groups[i].Status, "")
		}
		seen[prior.Groups[i].GroupIndex] = struct{}{}
		if prior.Groups[i].GroupIndex == location.GroupIndex {
			groupPos = i
		}
	}
	if groupPos < 0 {
		return groupCompletionTarget{}, nil, stateError(GroupCompletionErrorMissingGroup, "", "")
	}
	if err := validateGroupCompletionQueue(prior, groupPos); err != nil {
		return groupCompletionTarget{}, nil, err
	}
	group := &prior.Groups[groupPos]
	if location.ItemIndex >= len(group.Items) {
		return groupCompletionTarget{}, nil, stateError(GroupCompletionErrorItemIndexOutOfRange, group.Status, "")
	}
	item := &group.Items[location.ItemIndex]
	want := ItemStatusCompleted
	if input.Outcome == GroupCompletionOutcomeFailed {
		want = ItemStatusFailed
	}
	if itemIsTerminal(item.Status) && item.Status != want {
		return groupCompletionTarget{}, nil, &GroupCompletionConflictError{Location: location, Stored: item.Status, Requested: input.Outcome}
	}
	if groupIsTerminal(group.Status) {
		if input.CompletionReceiptID != "" {
			return groupCompletionTarget{}, nil, unexpectedCompletionReceipt(input)
		}
		if item.Status == want {
			result := GroupCompletionResult{Disposition: GroupCompletionDispositionNoChange, NoChangeReason: GroupCompletionNoChangeMatchingTerminalOutcome}
			return groupCompletionTarget{}, &result, nil
		}
		return groupCompletionTarget{}, nil, stateError(GroupCompletionErrorInvalidGroupStatus, group.Status, item.Status)
	}
	if prior.Status != QueueStatusActive {
		return groupCompletionTarget{}, nil, stateError(GroupCompletionErrorInvalidQueueStatus, group.Status, item.Status)
	}
	return groupCompletionTarget{groupPos: groupPos, itemStatus: want}, nil, nil
}

func validateGroupCompletionQueue(prior Queue, target int) error {
	for i := range prior.Groups {
		group := prior.Groups[i]
		if group.GroupIndex != i {
			return &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidGroupIndex, Location: GroupCompletionLocation{GroupIndex: group.GroupIndex, ItemIndex: -1}, QueueStatus: prior.Status, GroupStatus: group.Status}
		}
		if !validCompletionGroupStatus(group.Status) {
			return &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidGroupStatus, Location: GroupCompletionLocation{GroupIndex: group.GroupIndex, ItemIndex: -1}, QueueStatus: prior.Status, GroupStatus: group.Status}
		}
		if !validCompletionGroupKind(group.Kind) {
			return &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidGroupKind, Location: GroupCompletionLocation{GroupIndex: group.GroupIndex, ItemIndex: -1}, QueueStatus: prior.Status, GroupStatus: group.Status, GroupKind: group.Kind}
		}
		for j := range group.Items {
			if !validCompletionItemStatus(group.Items[j].Status) {
				return &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidItemStatus, Location: GroupCompletionLocation{GroupIndex: group.GroupIndex, ItemIndex: j}, QueueStatus: prior.Status, GroupStatus: group.Status, ItemStatus: group.Items[j].Status}
			}
		}
		want := GroupStatusPending
		if i < target {
			want = GroupStatusCompleteSuccess
		} else if i == target {
			want = GroupStatusActive
		}
		if !groupIsTerminal(prior.Groups[target].Status) && group.Status != want {
			return &GroupCompletionStateError{Reason: GroupCompletionErrorInvalidGroupStatus, Location: GroupCompletionLocation{GroupIndex: group.GroupIndex, ItemIndex: -1}, QueueStatus: prior.Status, GroupStatus: group.Status}
		}
	}
	return nil
}

func changedCompletionResult(next *Queue, intents []EventIntent, disposition GroupCompletionDisposition) GroupCompletionResult {
	return GroupCompletionResult{NextQueue: next, Intents: intents, Disposition: disposition, Changed: true}
}

func validCompletionItemStatus(status ItemStatus) bool {
	switch status {
	case ItemStatusPending, ItemStatusDispatched, ItemStatusCompleted, ItemStatusFailed, ItemStatusDeferredForLedgerDep:
		return true
	default:
		return false
	}
}

func validCompletionGroupStatus(status GroupStatus) bool {
	switch status {
	case GroupStatusPending, GroupStatusActive, GroupStatusCompleteSuccess, GroupStatusCompleteWithFailures:
		return true
	default:
		return false
	}
}

func validCompletionGroupKind(kind GroupKind) bool {
	switch kind {
	case GroupKindWave, GroupKindStream:
		return true
	default:
		return false
	}
}

func unexpectedCompletionReceipt(input GroupCompletionInput) error {
	return &GroupCompletionInputError{Reason: GroupCompletionErrorUnexpectedReceiptID, Location: input.Location, Value: input.CompletionReceiptID}
}

func bindFinalCompletionReceipt(intents []EventIntent, receiptID string) ([]EventIntent, error) {
	if len(intents) != 1 || intents[0].Type != core.EventTypeQueueGroupCompleted {
		return nil, errors.New("queue: final completion has no unique group-completed intent")
	}
	var payload core.QueueGroupCompletedPayload
	if err := json.Unmarshal(intents[0].Payload, &payload); err != nil {
		return nil, fmt.Errorf("queue: decode final completion intent: %w", err)
	}
	payload.CompletionReceiptID = receiptID
	intent, err := NewEventIntent(core.EventTypeQueueGroupCompleted, &payload)
	if err != nil {
		return nil, fmt.Errorf("queue: bind final completion receipt: %w", err)
	}
	return []EventIntent{intent}, nil
}
