package queue

import (
	"errors"
	"fmt"
	"time"
)

// GroupCompletionOutcome is the terminal result reported for one queue item.
type GroupCompletionOutcome string

// The complete set of terminal outcomes a caller can report for one item.
const (
	GroupCompletionOutcomeCompleted GroupCompletionOutcome = "completed"
	GroupCompletionOutcomeFailed    GroupCompletionOutcome = "failed"
)

func (v GroupCompletionOutcome) String() string { return string(v) }

func (v GroupCompletionOutcome) valid() bool {
	switch v {
	case GroupCompletionOutcomeCompleted, GroupCompletionOutcomeFailed:
		return true
	default:
		return false
	}
}

// GroupCompletionLocation identifies one item by its stable group index and
// its position inside that group.
type GroupCompletionLocation struct {
	GroupIndex int
	ItemIndex  int
}

// GroupCompletionInput contains every value needed by the pure completion
// decision. CompletionReceiptID is empty on the first call. A
// GroupCompletionDispositionReceiptRequired result asks the caller to mint an
// ID and retry the same decision without changing live state.
type GroupCompletionInput struct {
	ExpectedQueueID     string
	Location            GroupCompletionLocation
	Outcome             GroupCompletionOutcome
	CompletedAt         time.Time
	CompletionReceiptID string
}

// Validate rejects malformed caller values before stored queue state is read.
func (in GroupCompletionInput) Validate() error {
	fail := func(reason GroupCompletionErrorReason, value string) error {
		return &GroupCompletionInputError{Reason: reason, Location: in.Location, Value: value}
	}
	if !in.Outcome.valid() {
		return fail(GroupCompletionErrorInvalidOutcome, string(in.Outcome))
	}
	if err := validateUUIDv7(in.ExpectedQueueID); err != nil {
		return fail(GroupCompletionErrorInvalidQueueID, in.ExpectedQueueID)
	}
	stamp := in.CompletedAt.UTC().Truncate(time.Millisecond)
	if in.CompletedAt.IsZero() || stamp.Year() < 1 || stamp.Year() > 9999 {
		return fail(GroupCompletionErrorInvalidCompletionTime, in.CompletedAt.String())
	}
	if in.Location.GroupIndex < 0 {
		return fail(GroupCompletionErrorNegativeGroupIndex, fmt.Sprint(in.Location.GroupIndex))
	}
	if in.Location.ItemIndex < 0 {
		return fail(GroupCompletionErrorNegativeItemIndex, fmt.Sprint(in.Location.ItemIndex))
	}
	if in.CompletionReceiptID != "" {
		if in.Outcome != GroupCompletionOutcomeCompleted {
			return fail(GroupCompletionErrorUnexpectedReceiptID, in.CompletionReceiptID)
		}
		if err := validateUUIDv7(in.CompletionReceiptID); err != nil {
			return fail(GroupCompletionErrorInvalidReceiptID, in.CompletionReceiptID)
		}
	}
	return nil
}

// GroupCompletionDisposition names the complete set of decision outcomes.
type GroupCompletionDisposition string

// The complete set of decisions the pure completion step can reach.
const (
	GroupCompletionDispositionNoChange           GroupCompletionDisposition = "no-change"
	GroupCompletionDispositionReceiptRequired    GroupCompletionDisposition = "receipt-required"
	GroupCompletionDispositionIntermediate       GroupCompletionDisposition = "intermediate"
	GroupCompletionDispositionPausedByFailure    GroupCompletionDisposition = "paused-by-failure"
	GroupCompletionDispositionSuccessorActivated GroupCompletionDisposition = "successor-activated"
	GroupCompletionDispositionQueueCompleted     GroupCompletionDisposition = "queue-completed"
)

func (v GroupCompletionDisposition) String() string { return string(v) }

// GroupCompletionNoChangeReason explains an expected race or idempotent call.
type GroupCompletionNoChangeReason string

// The complete set of reasons a no-change decision gives. A receipt-required
// decision also leaves the stored queue alone, and it names no reason.
const (
	GroupCompletionNoChangeStaleQueue              GroupCompletionNoChangeReason = "stale-queue"
	GroupCompletionNoChangeMatchingTerminalOutcome GroupCompletionNoChangeReason = "matching-terminal-outcome"
)

func (v GroupCompletionNoChangeReason) String() string { return string(v) }

func (v GroupCompletionNoChangeReason) valid() bool {
	switch v.String() {
	case string(GroupCompletionNoChangeStaleQueue), string(GroupCompletionNoChangeMatchingTerminalOutcome):
		return true
	default:
		return false
	}
}

// GroupCompletionResult is either an unchanged result, a receipt request, or
// one detached changed queue with ordered event intents.
type GroupCompletionResult struct {
	NextQueue      *Queue
	Intents        []EventIntent
	Disposition    GroupCompletionDisposition
	NoChangeReason GroupCompletionNoChangeReason
	Changed        bool
}

// Validate rejects internally inconsistent result values.
func (r GroupCompletionResult) Validate() error {
	switch r.Disposition {
	case GroupCompletionDispositionNoChange:
		if !r.validAsNoChange() {
			return errors.New("queue: invalid no-change completion result")
		}
	case GroupCompletionDispositionReceiptRequired:
		if !r.validAsReceiptRequired() {
			return errors.New("queue: invalid receipt-required completion result")
		}
	case GroupCompletionDispositionIntermediate,
		GroupCompletionDispositionPausedByFailure,
		GroupCompletionDispositionSuccessorActivated,
		GroupCompletionDispositionQueueCompleted:
		if !r.validAsChanged() {
			return errors.New("queue: invalid changed completion result")
		}
	default:
		return fmt.Errorf("queue: invalid completion disposition %q", r.Disposition)
	}
	return nil
}

// leavesQueueAlone is true when the result asks the caller to write nothing.
func (r GroupCompletionResult) leavesQueueAlone() bool {
	return !r.Changed && r.NextQueue == nil && len(r.Intents) == 0
}

// validAsNoChange is true for a result that reports an expected race or a
// repeated call. Such a result writes nothing and it names the reason.
func (r GroupCompletionResult) validAsNoChange() bool {
	return r.leavesQueueAlone() && r.NoChangeReason.valid()
}

// validAsReceiptRequired is true for a result that asks the caller for an ID
// and then a retry. Such a result writes nothing. It is a request and not a
// race, so it names no reason.
func (r GroupCompletionResult) validAsReceiptRequired() bool {
	return r.leavesQueueAlone() && r.NoChangeReason == ""
}

// validAsChanged is true for a result that carries the one detached next
// queue. Such a result is not a race, so it names no reason.
func (r GroupCompletionResult) validAsChanged() bool {
	return r.Changed && r.NextQueue != nil && r.NoChangeReason == ""
}

// GroupCompletionErrorReason classifies invalid input and corrupt stored state.
type GroupCompletionErrorReason string

// The complete set of reasons a decision refuses the request or the state.
const (
	GroupCompletionErrorInvalidOutcome        GroupCompletionErrorReason = "invalid-outcome"
	GroupCompletionErrorInvalidQueueID        GroupCompletionErrorReason = "invalid-expected-queue-id"
	GroupCompletionErrorInvalidReceiptID      GroupCompletionErrorReason = "invalid-receipt-id"
	GroupCompletionErrorUnexpectedReceiptID   GroupCompletionErrorReason = "unexpected-receipt-id"
	GroupCompletionErrorInvalidCompletionTime GroupCompletionErrorReason = "invalid-completion-time"
	GroupCompletionErrorNegativeGroupIndex    GroupCompletionErrorReason = "negative-group-index"
	GroupCompletionErrorNegativeItemIndex     GroupCompletionErrorReason = "negative-item-index"
	GroupCompletionErrorMissingGroup          GroupCompletionErrorReason = "missing-group"
	GroupCompletionErrorDuplicateGroup        GroupCompletionErrorReason = "duplicate-group"
	GroupCompletionErrorInvalidGroupIndex     GroupCompletionErrorReason = "invalid-group-index"
	GroupCompletionErrorItemIndexOutOfRange   GroupCompletionErrorReason = "item-index-out-of-range"
	GroupCompletionErrorInvalidQueueStatus    GroupCompletionErrorReason = "invalid-queue-status"
	GroupCompletionErrorInvalidGroupStatus    GroupCompletionErrorReason = "invalid-group-status"
	GroupCompletionErrorInvalidGroupKind      GroupCompletionErrorReason = "invalid-group-kind"
	GroupCompletionErrorInvalidItemStatus     GroupCompletionErrorReason = "invalid-item-status"
)

func (v GroupCompletionErrorReason) String() string { return string(v) }

// GroupCompletionInputError reports a malformed decision request.
type GroupCompletionInputError struct {
	Reason   GroupCompletionErrorReason
	Location GroupCompletionLocation
	Value    string
}

func (e *GroupCompletionInputError) Error() string {
	return fmt.Sprintf("queue: group completion input %s at group %d item %d: %q", e.Reason, e.Location.GroupIndex, e.Location.ItemIndex, e.Value)
}

// GroupCompletionStateError reports corrupt or unsupported persisted state.
type GroupCompletionStateError struct {
	Reason      GroupCompletionErrorReason
	Location    GroupCompletionLocation
	QueueStatus QueueStatus
	GroupStatus GroupStatus
	GroupKind   GroupKind
	ItemStatus  ItemStatus
}

func (e *GroupCompletionStateError) Error() string {
	return fmt.Sprintf("queue: group completion state %s at group %d item %d (queue=%q group=%q kind=%q item=%q)", e.Reason, e.Location.GroupIndex, e.Location.ItemIndex, e.QueueStatus, e.GroupStatus, e.GroupKind, e.ItemStatus)
}

// GroupCompletionConflictError reports a second terminal outcome that differs
// from the already stored outcome.
type GroupCompletionConflictError struct {
	Location  GroupCompletionLocation
	Stored    ItemStatus
	Requested GroupCompletionOutcome
}

func (e *GroupCompletionConflictError) Error() string {
	return fmt.Sprintf("queue: group completion conflict at group %d item %d: stored %q, requested %q", e.Location.GroupIndex, e.Location.ItemIndex, e.Stored, e.Requested)
}
