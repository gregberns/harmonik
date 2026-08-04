package queuewiring

// recovery.go — the production driver for failed-queue recovery (QM-052b).
//
// `queue.ResumeFromFailure` is the pure mutation: it re-arms failed items and
// flips the queue back to active. It had no production caller, so a queue that
// parked at `paused-by-failure` had no way out short of a daemon restart plus a
// fresh submit. This file is the effectful half that gives it one: a ledger
// preflight, one QM-001 transaction, and one typed refusal per QM-052b.
//
// Ordering matters and is deliberate. The ledger preflight runs BEFORE any
// candidate mutation, so a queue is never made durably active with items whose
// beads cannot be claimed. A refused preflight leaves the queue, the ledger, and
// the dispatcher untouched.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b, §3.1 QM-001.

import (
	"context"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// RecoveryBeadReader is the consumer-owned slice of the Beads ledger that
// recovery needs: one status lookup per failed item. It is narrower than
// queue.BeadLedger on purpose — recovery has no use for dependency edges, and a
// port that asks for less is easier to fake honestly in a test.
//
// *BRQueueLedger satisfies it.
type RecoveryBeadReader interface {
	LookupStatus(ctx context.Context, id core.BeadID) (queue.BeadStatus, error)
}

// FailedRecoveryRequest names one recovery attempt.
type FailedRecoveryRequest struct {
	// ProjectDir is the durable-write root. Empty runs the in-memory path used
	// by unit tests, matching the rest of QueueStore.
	ProjectDir string

	// Name is the operator's spelling of the queue name. It is normalised here.
	Name string

	// Beads is the QM-052b preflight reader. When nil the preflight is skipped
	// and recovery proceeds on queue state alone. A daemon that has a ledger
	// MUST pass it; the nil case exists for unit tests and for a daemon booted
	// with no Beads adapter.
	Beads RecoveryBeadReader
}

// FailedRecoveryOutcome reports what one committed recovery did.
type FailedRecoveryOutcome struct {
	// Name is the normalised queue name.
	Name string

	// QueueID is the recovered queue's minted ID.
	QueueID string

	// Rearmed lists the bead IDs moved from failed back to pending, in
	// group-then-item order.
	Rearmed []core.BeadID
}

// errRecoveryStatusRaced marks the narrow window where the queue left
// paused-by-failure between the snapshot read and the candidate mutation. It is
// mapped back to a typed refusal by RecoverFailed and never escapes.
var errRecoveryStatusRaced = errors.New("queuewiring: queue left paused-by-failure during recovery")

// RecoverFailed moves one queue out of `paused-by-failure` and back into
// dispatch. It is the production caller of queue.ResumeFromFailure.
//
// Order of work:
//
//  1. Refuse a quarantined queue. QM-001 makes a quarantine sticky, and
//     recovery is a mutation, so it cannot clear one.
//  2. Refuse a missing queue, and refuse any status other than
//     paused-by-failure.
//  3. Run the QM-052b ledger preflight over every failed item. Every bead must
//     be open. A missing bead, a read error, or a non-open bead refuses with no
//     mutation, no write, and no dispatch wake.
//  4. Re-arm the failed items and flip the queue to active inside one QM-001
//     transaction, then wake dispatch.
//
// Every refusal returns a *queue.RecoveryError carrying one of the seven
// QM-052b reasons. A write failure or a stale snapshot leaves memory and disk
// at the prior candidate.
//
// Success means the queue mutation is durable. It does not mean any item has
// dispatched.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b.
func (s *QueueStore) RecoverFailed(ctx context.Context, req FailedRecoveryRequest) (FailedRecoveryOutcome, error) {
	name := queue.NormaliseQueueName(req.Name)

	if quarantineErr := s.QuarantineReason(name); quarantineErr != nil {
		return FailedRecoveryOutcome{}, &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueQuarantined,
			NormalizedName: name,
			Cause:          quarantineErr,
		}
	}

	snapshot := s.Snapshot(name)
	if snapshot.Queue == nil {
		return FailedRecoveryOutcome{}, &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueNotFound,
			NormalizedName: name,
		}
	}
	if snapshot.Queue.Status != queue.QueueStatusPausedByFailure {
		return FailedRecoveryOutcome{}, &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueNotRecoverable,
			NormalizedName: name,
			QueueID:        snapshot.Queue.QueueID,
			ObservedStatus: snapshot.Queue.Status,
		}
	}

	if err := recoveryPreflight(ctx, req.Beads, name, snapshot.Queue); err != nil {
		return FailedRecoveryOutcome{}, err
	}

	var rearmed []core.BeadID
	result := s.Transact(ctx, TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    req.ProjectDir,
		OperationKind: queue.OperationResume,
		WakeRequired:  true,
		Mutate: func(candidate *queue.Queue) error {
			ids, ok := queue.ResumeFromFailure(candidate)
			if !ok {
				return errRecoveryStatusRaced
			}
			rearmed = ids
			return nil
		},
	})
	if err := recoveryTransactionError(name, snapshot.Queue.QueueID, result); err != nil {
		return FailedRecoveryOutcome{}, err
	}

	return FailedRecoveryOutcome{
		Name:    name,
		QueueID: snapshot.Queue.QueueID,
		Rearmed: rearmed,
	}, nil
}

// recoveryPreflight asserts every failed item's bead is open before recovery
// mutates anything. A nil reader skips the check.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b (BI-013f recovery preflight).
func recoveryPreflight(ctx context.Context, beads RecoveryBeadReader, name string, q *queue.Queue) error {
	if beads == nil {
		return nil
	}
	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := q.Groups[gi].Items[ii]
			if item.Status != queue.ItemStatusFailed {
				continue
			}
			status, err := beads.LookupStatus(ctx, item.BeadID)
			if err != nil {
				return &queue.RecoveryError{
					Reason:         queue.RecoveryReasonReadFailed,
					NormalizedName: name,
					QueueID:        q.QueueID,
					BeadID:         item.BeadID,
					Cause:          err,
				}
			}
			if status != queue.BeadStatusOpen {
				return &queue.RecoveryError{
					Reason:         queue.RecoveryReasonBeadNotOpen,
					NormalizedName: name,
					QueueID:        q.QueueID,
					BeadID:         item.BeadID,
					BeadStatus:     status,
				}
			}
		}
	}
	return nil
}

// recoveryTransactionError maps a transaction outcome onto the QM-052b refusal
// reasons. A rejection never reached I/O, so it is a stale snapshot; anything
// else that did not commit durably is a write failure.
func recoveryTransactionError(name, queueID string, result TransactionResult) error {
	if result.Committed() && result.CleanupErr == nil {
		return nil
	}

	if errors.Is(result.Err, errRecoveryStatusRaced) {
		return &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueNotRecoverable,
			NormalizedName: name,
			QueueID:        queueID,
			Cause:          result.Err,
		}
	}

	cause := result.Err
	if cause == nil {
		cause = result.CleanupErr
	}
	if cause == nil {
		cause = fmt.Errorf("transaction outcome %q", result.Outcome)
	}

	reason := queue.RecoveryReasonWriteFailed
	if result.Outcome == queue.OutcomeRejected && !errors.Is(result.Err, ErrQueueQuarantined) {
		reason = queue.RecoveryReasonStaleSnapshot
	}
	if errors.Is(result.Err, ErrQueueQuarantined) {
		reason = queue.RecoveryReasonQueueQuarantined
	}

	return &queue.RecoveryError{
		Reason:         reason,
		NormalizedName: name,
		QueueID:        queueID,
		Cause:          cause,
	}
}
