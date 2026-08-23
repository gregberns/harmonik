package queuewiring

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
	// group-then-item order. It is read back off the durable receipt, not off
	// the in-memory mutation, so the list an operator sees is the list the
	// receipt will still name after a restart.
	Rearmed []core.BeadID

	// Receipt is the durable proof of this recovery. A committed recovery
	// always carries one. It is what makes the answer re-checkable after the
	// daemon is gone.
	Receipt queue.FailedRecoveryReceipt

	// AlreadyRecovered is true when this call found the queue already active
	// behind Receipt and mutated nothing. Repeating a recovery is safe and
	// returns the same receipt; it is not a second recovery and must not read
	// as one.
	AlreadyRecovered bool
}

var errRecoveryStatusRaced = errors.New("queuewiring: queue left paused-by-failure during recovery")

// RecoverFailed moves one queue out of `paused-by-failure` and back into
// dispatch. It is the single production entry point for queue recovery.
//
// Order of work:
//
//  1. Refuse a missing queue.
//  2. Answer an already-recovered queue with its existing receipt and no
//     mutation. Repeating the request is safe by design (QM-058a).
//  3. Refuse any other status than paused-by-failure. A quarantined queue in
//     any other status refuses with queue_quarantined, because failed recovery
//     is the ONLY operation permitted to act on a quarantined queue, and only
//     while it is paused by failure (QM-059).
//  4. Run the QM-052b ledger preflight over every failed item. Every bead must
//     be open. A missing bead, a read error, or a non-open bead refuses with no
//     mutation, no write, and no dispatch wake.
//  5. Commit the receipt-bound recovery transaction, then wake dispatch.
//
// Every refusal returns a *queue.RecoveryError carrying one of the seven
// QM-052b reasons. A write failure or a stale snapshot leaves memory and disk
// at the prior candidate.
//
// Success means the queue mutation AND its receipt are durable. It does not
// mean any item has dispatched.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b, QM-058a, QM-059.
func (s *QueueStore) RecoverFailed(ctx context.Context, req FailedRecoveryRequest) (FailedRecoveryOutcome, error) {
	name := queue.NormaliseQueueName(req.Name)

	snapshot := s.Snapshot(name)
	if snapshot.Queue == nil {
		return FailedRecoveryOutcome{}, &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueNotFound,
			NormalizedName: name,
		}
	}

	alreadyRecovered := snapshot.Queue.Status == queue.QueueStatusActive &&
		snapshot.Queue.FailedRecoveryReceiptID != nil

	if !alreadyRecovered && snapshot.Queue.Status != queue.QueueStatusPausedByFailure {
		if quarantineErr := s.QuarantineReason(name); quarantineErr != nil {
			return FailedRecoveryOutcome{}, &queue.RecoveryError{
				Reason:         queue.RecoveryReasonQueueQuarantined,
				NormalizedName: name,
				QueueID:        snapshot.Queue.QueueID,
				ObservedStatus: snapshot.Queue.Status,
				Cause:          quarantineErr,
			}
		}
		return FailedRecoveryOutcome{}, &queue.RecoveryError{
			Reason:         queue.RecoveryReasonQueueNotRecoverable,
			NormalizedName: name,
			QueueID:        snapshot.Queue.QueueID,
			ObservedStatus: snapshot.Queue.Status,
		}
	}

	if !alreadyRecovered {
		if err := recoveryPreflight(ctx, req.Beads, name, snapshot.Queue); err != nil {
			return FailedRecoveryOutcome{}, err
		}
	}

	result := s.commitFailedRecovery(ctx, req.ProjectDir, name)
	if err := recoveryTransactionError(name, snapshot.Queue.QueueID, result.TransactionResult); err != nil {
		return FailedRecoveryOutcome{}, err
	}

	return FailedRecoveryOutcome{
		Name:             name,
		QueueID:          snapshot.Queue.QueueID,
		Rearmed:          rearmedFromReceipt(result.Receipt),
		Receipt:          result.Receipt,
		AlreadyRecovered: result.AlreadyRecovered,
	}, nil
}

func rearmedFromReceipt(receipt queue.FailedRecoveryReceipt) []core.BeadID {
	ids := make([]core.BeadID, 0, len(receipt.RecoveredItems))
	for _, item := range receipt.RecoveredItems {
		ids = append(ids, core.BeadID(item.BeadID))
	}
	return ids
}

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
