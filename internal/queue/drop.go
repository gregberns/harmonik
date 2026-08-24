package queue

import (
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// DropReason enumerates why a failed-queue drop was refused, or is the
// no-op-classification hook for the one case that isn't a refusal.
//
// Drop is deliberately narrower than recovery: it never re-arms an item, and
// it never touches a queue whose failure is not its last group, so a queue
// with legitimate pending work behind the failure refuses rather than losing
// that work. Spec ref: hk-a4pam (this rule has no queue-model.md section yet;
// see the commit that introduces this file for why).
type DropReason string

const (
	// DropReasonQueueNotFound — no queue is loaded under that normalized name.
	DropReasonQueueNotFound DropReason = "queue_not_found"

	// DropReasonQueueNotRecoverable — the queue exists but is not parked at
	// paused-by-failure (or its shape has no complete-with-failures group,
	// which should not happen to a healthy paused-by-failure queue).
	DropReasonQueueNotRecoverable DropReason = "queue_not_recoverable"

	// DropReasonQueueQuarantined — an earlier write to this queue failed, so
	// QM-001 refuses further mutations until an operator repairs it.
	DropReasonQueueQuarantined DropReason = "queue_quarantined"

	// DropReasonTrailingGroups — the failed group is not the queue's last
	// group. Dropping would discard the pending groups behind it, so this
	// tool refuses rather than doing that silently.
	DropReasonTrailingGroups DropReason = "drop_trailing_groups"

	// DropReasonBeadNotClosed — a failed item's bead is still open or
	// in_progress in the ledger. Dropping it would hide a live failure, so
	// this is the one refusal drop never bypasses.
	DropReasonBeadNotClosed DropReason = "drop_bead_not_closed"

	// DropReasonLedgerUnavailable — no bead ledger is wired, so the
	// bead-still-open safety check cannot run. Drop fails closed rather than
	// skip the one check that keeps it from hiding a live failure.
	DropReasonLedgerUnavailable DropReason = "drop_ledger_unavailable"

	// DropReasonReadFailed — the ledger preflight could not read a bead. An
	// unknown answer, not a negative one, so drop refuses.
	DropReasonReadFailed DropReason = "drop_read_failed"

	// DropReasonWriteFailed — the archive step did not complete.
	DropReasonWriteFailed DropReason = "drop_write_failed"
)

// JSON-RPC error codes for DropReason. The -32037..-32044 block continues
// directly after the queue-recovery block (-32030..-32036, QM-052b) — it is
// a distinct operation and must not share a code with a recovery refusal.
//
// These are wire constants. Once assigned they do not move.
const (
	ErrorCodeDropQueueNotFound       = -32037
	ErrorCodeDropQueueNotRecoverable = -32038
	ErrorCodeDropQueueQuarantined    = -32039
	ErrorCodeDropTrailingGroups      = -32040
	ErrorCodeDropBeadNotClosed       = -32041
	ErrorCodeDropLedgerUnavailable   = -32042
	ErrorCodeDropReadFailed          = -32043
	ErrorCodeDropWriteFailed         = -32044
)

// DropError is the single typed rejection value of a failed-queue drop.
type DropError struct {
	// Reason is the closed-enum cause. Always set.
	Reason DropReason

	// NormalizedName is the queue name after NormaliseQueueName. Always set.
	NormalizedName string

	// QueueID is the parked queue's minted ID. Empty when no queue was found.
	QueueID string

	// ObservedStatus is the status that blocked the drop. Set only for
	// DropReasonQueueNotRecoverable.
	ObservedStatus QueueStatus

	// BeadID and BeadStatus name the offending item. Set only for
	// DropReasonBeadNotClosed and DropReasonReadFailed.
	BeadID     core.BeadID
	BeadStatus BeadStatus

	// Cause is the underlying I/O or ledger error, for diagnosis.
	Cause error
}

// Error renders the reason, the queue, and whichever context field the
// reason gives meaning to.
func (e *DropError) Error() string {
	msg := fmt.Sprintf("queue drop refused: %s: queue %q", e.Reason, e.NormalizedName)
	switch e.Reason {
	case DropReasonQueueNotRecoverable:
		msg += fmt.Sprintf(": status is %q, want %q", e.ObservedStatus, QueueStatusPausedByFailure)
	case DropReasonBeadNotClosed:
		msg += fmt.Sprintf(": bead %s is %q, drop requires it to be closed (or unknown)", e.BeadID, e.BeadStatus)
	case DropReasonReadFailed:
		msg += fmt.Sprintf(": bead %s", e.BeadID)
	case DropReasonTrailingGroups:
		msg += ": pending groups sit behind the failure; drop only disposes of a failure that is the queue's last group"
	case DropReasonQueueNotFound, DropReasonQueueQuarantined,
		DropReasonLedgerUnavailable, DropReasonWriteFailed:
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *DropError) Unwrap() error { return e.Cause }

// Code returns the JSON-RPC error code for this rejection.
func (e *DropError) Code() int {
	code, _ := DropJSONRPCError(e.Reason)
	return code
}

// DropJSONRPCError maps a DropReason to its wire code and message. An
// unknown reason returns (0, "") so a caller cannot silently ship a code it
// did not allocate.
func DropJSONRPCError(reason DropReason) (code int, message string) {
	switch reason {
	case DropReasonQueueNotFound:
		return ErrorCodeDropQueueNotFound, string(DropReasonQueueNotFound)
	case DropReasonQueueNotRecoverable:
		return ErrorCodeDropQueueNotRecoverable, string(DropReasonQueueNotRecoverable)
	case DropReasonQueueQuarantined:
		return ErrorCodeDropQueueQuarantined, string(DropReasonQueueQuarantined)
	case DropReasonTrailingGroups:
		return ErrorCodeDropTrailingGroups, string(DropReasonTrailingGroups)
	case DropReasonBeadNotClosed:
		return ErrorCodeDropBeadNotClosed, string(DropReasonBeadNotClosed)
	case DropReasonLedgerUnavailable:
		return ErrorCodeDropLedgerUnavailable, string(DropReasonLedgerUnavailable)
	case DropReasonReadFailed:
		return ErrorCodeDropReadFailed, string(DropReasonReadFailed)
	case DropReasonWriteFailed:
		return ErrorCodeDropWriteFailed, string(DropReasonWriteFailed)
	default:
		return 0, ""
	}
}

// DropCodeOf returns the drop code carried by err, or 0 when err is not a
// drop rejection.
func DropCodeOf(err error) int {
	var drop *DropError
	if errors.As(err, &drop) {
		return drop.Code()
	}
	return 0
}

// FailedDropPlan names the failed items a drop would remove and the group
// that carries them.
type FailedDropPlan struct {
	// GroupIndex is the group_index of the queue's failed (complete-with-
	// failures) group — always its last group, by DropReasonTrailingGroups.
	GroupIndex int

	// DroppedItems are the failed items' bead IDs, in item order.
	DroppedItems []core.BeadID
}

// PlanFailedDrop validates that q is a droppable paused-by-failure queue and
// returns the failed items its last group carries.
//
// It performs no ledger check and no I/O: the caller runs the
// bead-must-be-closed preflight (a drop must never become a quiet way to
// make a live failure disappear) and the archive itself. A queue whose
// failure is not its last group refuses with DropReasonTrailingGroups —
// dropping would otherwise discard the pending groups behind it.
func PlanFailedDrop(name string, q *Queue) (FailedDropPlan, error) {
	if q == nil {
		return FailedDropPlan{}, &DropError{Reason: DropReasonQueueNotFound, NormalizedName: name}
	}
	if q.Status != QueueStatusPausedByFailure {
		return FailedDropPlan{}, &DropError{
			Reason:         DropReasonQueueNotRecoverable,
			NormalizedName: name,
			QueueID:        q.QueueID,
			ObservedStatus: q.Status,
		}
	}
	failedIndex := -1
	for i := range q.Groups {
		if q.Groups[i].Status == GroupStatusCompleteWithFailures {
			failedIndex = i
			break
		}
	}
	if failedIndex < 0 {
		return FailedDropPlan{}, &DropError{
			Reason:         DropReasonQueueNotRecoverable,
			NormalizedName: name,
			QueueID:        q.QueueID,
			ObservedStatus: q.Status,
		}
	}
	if failedIndex != len(q.Groups)-1 {
		return FailedDropPlan{}, &DropError{
			Reason:         DropReasonTrailingGroups,
			NormalizedName: name,
			QueueID:        q.QueueID,
			ObservedStatus: q.Status,
		}
	}
	var dropped []core.BeadID
	for _, item := range q.Groups[failedIndex].Items {
		if item.Status == ItemStatusFailed {
			dropped = append(dropped, item.BeadID)
		}
	}
	return FailedDropPlan{GroupIndex: q.Groups[failedIndex].GroupIndex, DroppedItems: dropped}, nil
}
