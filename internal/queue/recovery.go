package queue

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// RecoveryReason enumerates why a failed-queue recovery was refused. The seven
// values are a closed wire enum: each maps 1:1 to a code in the -32030..-32036
// block. Adding a value needs a spec amendment and a code allocation.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b.
type RecoveryReason string

const (
	// RecoveryReasonQueueNotFound — no queue is loaded under that normalized name.
	RecoveryReasonQueueNotFound RecoveryReason = "queue_not_found"

	// RecoveryReasonQueueNotRecoverable — the queue exists but is not parked at
	// paused-by-failure. An active, drained, budget-paused, completed, or
	// cancelled queue lands here. ObservedStatus carries what was found.
	RecoveryReasonQueueNotRecoverable RecoveryReason = "queue_not_recoverable"

	// RecoveryReasonQueueQuarantined — an earlier write to this queue failed, so
	// QM-001 refuses further mutations until an operator repairs it. Recovery
	// does not clear a quarantine.
	RecoveryReasonQueueQuarantined RecoveryReason = "queue_quarantined"

	// RecoveryReasonBeadNotOpen — a failed item's bead is not open in the
	// ledger, so re-arming it would produce a queue that cannot claim its work.
	// BeadID and BeadStatus carry the offender.
	RecoveryReasonBeadNotOpen RecoveryReason = "recovery_bead_not_open"

	// RecoveryReasonReadFailed — the ledger preflight could not read a bead.
	// This is an unknown answer, not a negative one, so recovery refuses.
	RecoveryReasonReadFailed RecoveryReason = "recovery_read_failed"

	// RecoveryReasonWriteFailed — the transaction reached I/O and did not commit
	// durably. Memory and disk stay at the prior candidate.
	RecoveryReasonWriteFailed RecoveryReason = "recovery_write_failed"

	// RecoveryReasonStaleSnapshot — the queue changed under the recovery
	// transaction, so it rejected before any I/O. The caller may look again.
	RecoveryReasonStaleSnapshot RecoveryReason = "recovery_stale_snapshot"
)

// JSON-RPC error codes for RecoveryReason. The -32030..-32036 block is the
// queue-recovery block. It deliberately does not reuse the queue-validation
// block -32010..-32019 (QM-029b) or the handler reservation -32020..-32029:
// a caller must be able to tell "your submit was invalid" from "your recovery
// was refused" by the code alone.
//
// These are wire constants. Once assigned they do not move.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b.
const (
	ErrorCodeRecoveryQueueNotFound       = -32030
	ErrorCodeRecoveryQueueNotRecoverable = -32031
	ErrorCodeRecoveryQueueQuarantined    = -32032
	ErrorCodeRecoveryBeadNotOpen         = -32033
	ErrorCodeRecoveryReadFailed          = -32034
	ErrorCodeRecoveryWriteFailed         = -32035
	ErrorCodeRecoveryStaleSnapshot       = -32036
)

// RecoveryError is the single typed rejection value of a failed-queue recovery.
// Every refusal path returns one. The optional fields are populated only when
// the reason gives them meaning, so an empty field is an absence and not a
// default.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b.
type RecoveryError struct {
	// Reason is the closed-enum cause. Always set.
	Reason RecoveryReason

	// NormalizedName is the queue name after NormaliseQueueName. Always set.
	NormalizedName string

	// QueueID is the parked queue's minted ID. Empty when no queue was found.
	QueueID string

	// ObservedStatus is the status that blocked recovery. Set only for
	// RecoveryReasonQueueNotRecoverable.
	ObservedStatus QueueStatus

	// BeadID and BeadStatus name the offending item. Set only for
	// RecoveryReasonBeadNotOpen and RecoveryReasonReadFailed.
	BeadID     core.BeadID
	BeadStatus BeadStatus

	// Cause is the underlying I/O or ledger error, for diagnosis. It never
	// changes the meaning of Reason.
	Cause error
}

// Error renders the reason, the queue, and whichever context field the reason
// gives meaning to.
func (e *RecoveryError) Error() string {
	msg := fmt.Sprintf("queue recovery refused: %s: queue %q", e.Reason, e.NormalizedName)
	switch e.Reason {
	case RecoveryReasonQueueNotRecoverable:
		msg += fmt.Sprintf(": status is %q, want %q", e.ObservedStatus, QueueStatusPausedByFailure)
	case RecoveryReasonBeadNotOpen:
		msg += fmt.Sprintf(": bead %s is %q, want %q", e.BeadID, e.BeadStatus, BeadStatusOpen)
	case RecoveryReasonReadFailed:
		msg += fmt.Sprintf(": bead %s", e.BeadID)
	case RecoveryReasonQueueNotFound, RecoveryReasonQueueQuarantined,
		RecoveryReasonWriteFailed, RecoveryReasonStaleSnapshot:
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *RecoveryError) Unwrap() error { return e.Cause }

// Code returns the JSON-RPC error code for this rejection.
func (e *RecoveryError) Code() int {
	code, _ := RecoveryJSONRPCError(e.Reason)
	return code
}

// RecoveryJSONRPCError maps a RecoveryReason to its wire code and message.
// An unknown reason returns (0, "") so a caller cannot silently ship a code it
// did not allocate.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b.
func RecoveryJSONRPCError(reason RecoveryReason) (code int, message string) {
	switch reason {
	case RecoveryReasonQueueNotFound:
		return ErrorCodeRecoveryQueueNotFound, string(RecoveryReasonQueueNotFound)
	case RecoveryReasonQueueNotRecoverable:
		return ErrorCodeRecoveryQueueNotRecoverable, string(RecoveryReasonQueueNotRecoverable)
	case RecoveryReasonQueueQuarantined:
		return ErrorCodeRecoveryQueueQuarantined, string(RecoveryReasonQueueQuarantined)
	case RecoveryReasonBeadNotOpen:
		return ErrorCodeRecoveryBeadNotOpen, string(RecoveryReasonBeadNotOpen)
	case RecoveryReasonReadFailed:
		return ErrorCodeRecoveryReadFailed, string(RecoveryReasonReadFailed)
	case RecoveryReasonWriteFailed:
		return ErrorCodeRecoveryWriteFailed, string(RecoveryReasonWriteFailed)
	case RecoveryReasonStaleSnapshot:
		return ErrorCodeRecoveryStaleSnapshot, string(RecoveryReasonStaleSnapshot)
	default:
		return 0, ""
	}
}

// RecoveryCodeOf returns the recovery code carried by err, or 0 when err is not
// a recovery rejection. Callers on the socket boundary use it to fill the wire
// error_code field without type-switching by hand.
func RecoveryCodeOf(err error) int {
	var rec *RecoveryError
	if errors.As(err, &rec) {
		return rec.Code()
	}
	return 0
}

// ResumeRefusedError is returned when a drain-release resume is aimed at a
// queue that is parked by failure, not by drain.
//
// It exists because the two pauses are different state machines. A drain
// release only flips Queue.status. Failure recovery rewrites per-item status,
// attempt counts, and failure reasons, and it reopens groups. Letting the drain
// verb answer "resumed" for a failure-parked queue reports success while
// nothing dispatches, which is a wrong answer rather than an error.
//
// Spec ref: specs/queue-model.md §8.3 QM-052, §8.5 QM-054.
type ResumeRefusedError struct {
	// NormalizedName is the queue the operator aimed at.
	NormalizedName string

	// QueueID is that queue's minted ID.
	QueueID string

	// ObservedStatus is the pause that blocked the release. Always
	// QueueStatusPausedByFailure today; carried so the message stays true if
	// another non-drain pause joins the refusal later.
	ObservedStatus QueueStatus
}

// Error names the wrong verb and the right one.
func (e *ResumeRefusedError) Error() string {
	return fmt.Sprintf(
		"queue %q is %s, not %s: resume releases a drain pause only; run `harmonik queue recover %s` to re-arm the failed items",
		e.NormalizedName, e.ObservedStatus, QueueStatusPausedByDrain, e.NormalizedName)
}

// UnknownQueueError is returned when an operator command names a queue that
// does not exist.
//
// It exists because a misspelled queue name used to be a silent no-op. The
// per-queue pause path emitted its events against whatever string it was
// handed, no consumer matched a queue to them, and the CLI still printed
// "paused" and exited 0. Pause is the emergency stop: an operator who types it
// during an incident and reads success has been told the dispatching stopped
// when it did not. A typo must be an error, not a success that changes nothing.
//
// KnownNames lets the message name the queues that DO exist, so a near-miss is
// obvious without a second command.
//
// Spec ref: specs/queue-model.md §8.3 QM-052, §8.5 QM-054.
type UnknownQueueError struct {
	// Verb is the operator verb that was refused ("pause", "resume", "cancel").
	Verb string

	// PastTense is how Verb reads after "no queue was". Optional: an empty
	// value renders as Verb+"d", which is right for "pause" and "resume" and
	// wrong for "cancel". Callers whose verb does not take a bare "d" set it.
	PastTense string

	// NormalizedName is the queue name the operator aimed at. Empty when the
	// operator aimed at a QueueID instead.
	NormalizedName string

	// QueueID is the queue_id the operator aimed at, set INSTEAD of
	// NormalizedName when the caller identified the queue by id rather than by
	// name (`harmonik queue cancel --queue-id`). The message then reads "no
	// queue with id" rather than "no queue named", because a uuid is not a name
	// and describing one as a name misreports what the caller typed back at
	// them — which is the whole job of this error.
	QueueID string

	// KnownNames are the queues that exist right now, sorted. May be empty.
	KnownNames []string
}

// Error names the queue that does not exist and the ones that do.
//
// The trailer lists NAMES even when the caller aimed at an id. "Which queues
// exist" is the same fact either way, a list of uuids is unreadable, and the
// name is what every other verb takes.
func (e *UnknownQueueError) Error() string {
	known := "none are loaded"
	if len(e.KnownNames) > 0 {
		known = strings.Join(e.KnownNames, ", ")
	}
	past := e.PastTense
	if past == "" {
		past = e.Verb + "d"
	}
	target := fmt.Sprintf("no queue named %q", e.NormalizedName)
	if e.QueueID != "" {
		target = fmt.Sprintf("no queue with id %q", e.QueueID)
	}
	return fmt.Sprintf(
		"%s: %s changed nothing and no queue was %s; queues that exist: %s",
		target, "`harmonik queue "+e.Verb+"`", past, known)
}
