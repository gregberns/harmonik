package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// QueueRecoveryHandler is the socket-facing seam for `queue-recover`.
//
// The error is expected to be a *queue.RecoveryError so the dispatcher can put
// the QM-052b code on the wire. An untyped error is still reported, with error
// code zero.
type QueueRecoveryHandler interface {
	// HandleQueueRecover recovers the named queue from paused-by-failure. An
	// empty queueName selects the reserved default queue.
	HandleQueueRecover(ctx context.Context, queueName string) (QueueRecoverResult, error)

	// HandleQueueDrop disposes of a paused-by-failure queue's failed items
	// without re-arming them: it archives the queue file (the same disk
	// contract `queue cancel` uses) and reaps the daemon's in-memory slot, so
	// the queue leaves paused-by-failure by ceasing to exist under that name
	// rather than by retrying the stale work. An empty queueName selects the
	// reserved default queue.
	HandleQueueDrop(ctx context.Context, queueName string) (QueueDropResult, error)
}

// QueueRecoverResult is the success payload of `queue-recover`.
//
// Result is "accepted" for a recovery this call committed and "no-op" for a
// queue that was already recovered behind the same receipt. The two are the
// same success to the transaction layer and a different answer to an operator,
// so they are named apart on the wire.
//
// Receipt is the durable proof. It is always present on a success. A payload
// that claims a result and carries no receipt describes a recovery nothing can
// re-check, and the CLI refuses to print it as a success.
//
// RearmedCount is carried next to Rearmed so a caller that only wants the shape
// of the answer does not have to length-check a list that may be empty for a
// legitimate reason (a queue can park by failure and later have its items
// re-armed by another path).
type QueueRecoverResult struct {
	Queue        string                      `json:"queue"`
	QueueID      string                      `json:"queue_id"`
	Result       string                      `json:"result"`
	Receipt      queue.FailedRecoveryReceipt `json:"receipt"`
	Rearmed      []core.BeadID               `json:"rearmed"`
	RearmedCount int                         `json:"rearmed_count"`
}

// QueueDropResult is the success payload of `queue-drop`.
//
// Dropped names the failed beads whose entries were removed by archiving the
// queue. Unlike recovery, drop carries no receipt: it produces no new queue
// state to re-check later, only the archived file's path.
type QueueDropResult struct {
	Queue        string        `json:"queue"`
	QueueID      string        `json:"queue_id"`
	Dropped      []core.BeadID `json:"dropped"`
	DroppedCount int           `json:"dropped_count"`
	ArchivePath  string        `json:"archive_path"`
}

// QueueRecoveryController implements QueueRecoveryHandler over the live
// QueueStore. It is built once at the composition root.
type QueueRecoveryController struct {
	store      *queuewiring.QueueStore
	projectDir string
	beads      queuewiring.RecoveryBeadReader
}

// NewQueueRecoveryController wires a controller to the live store.
//
// beads may be nil when the daemon booted without a Beads adapter; the QM-052b
// ledger preflight is then skipped and recovery decides on queue state alone.
func NewQueueRecoveryController(store *queuewiring.QueueStore, projectDir string, beads queuewiring.RecoveryBeadReader) *QueueRecoveryController {
	return &QueueRecoveryController{store: store, projectDir: projectDir, beads: beads}
}

// HandleQueueRecover implements QueueRecoveryHandler.
func (c *QueueRecoveryController) HandleQueueRecover(ctx context.Context, queueName string) (QueueRecoverResult, error) {
	if c == nil || c.store == nil {
		return QueueRecoverResult{}, fmt.Errorf("queue-recover: no queue store")
	}
	if queueName == "" {
		queueName = queue.QueueNameMain
	}
	outcome, err := c.store.RecoverFailed(ctx, queuewiring.FailedRecoveryRequest{
		ProjectDir: c.projectDir,
		Name:       queueName,
		Beads:      c.beads,
	})
	if err != nil {
		return QueueRecoverResult{}, err
	}
	result := "accepted"
	if outcome.AlreadyRecovered {
		result = "no-op"
	}
	return QueueRecoverResult{
		Queue:        outcome.Name,
		QueueID:      outcome.QueueID,
		Result:       result,
		Receipt:      outcome.Receipt,
		Rearmed:      outcome.Rearmed,
		RearmedCount: len(outcome.Rearmed),
	}, nil
}

// HandleQueueDrop implements QueueRecoveryHandler.
//
// Order of work:
//
//  1. Refuse a missing queue.
//  2. Refuse a quarantined queue name.
//  3. Refuse any status other than paused-by-failure, or a paused-by-failure
//     queue whose failed group is not its LAST group — dropping would
//     otherwise discard legitimate pending work behind the failure, which
//     this operation must never do silently.
//  4. Refuse if no bead ledger is wired — the bead-still-open check is the
//     one guard against turning drop into a quiet way to hide a live
//     failure, and it must never be silently skipped.
//  5. Refuse if any failed item's bead is still open or in_progress.
//  6. Archive the queue file and reap the in-memory slot. No dispatch wake
//     is needed: the queue no longer exists under this name.
func (c *QueueRecoveryController) HandleQueueDrop(ctx context.Context, queueName string) (QueueDropResult, error) {
	if c == nil || c.store == nil {
		return QueueDropResult{}, fmt.Errorf("queue-drop: no queue store")
	}
	if queueName == "" {
		queueName = queue.QueueNameMain
	}
	name := queue.NormaliseQueueName(queueName)

	if quarantineErr := c.store.QuarantineReason(name); quarantineErr != nil {
		return QueueDropResult{}, &queue.DropError{
			Reason:         queue.DropReasonQueueQuarantined,
			NormalizedName: name,
			Cause:          quarantineErr,
		}
	}

	snapshot := c.store.Snapshot(name)
	plan, err := queue.PlanFailedDrop(name, snapshot.Queue)
	if err != nil {
		return QueueDropResult{}, err
	}

	if c.beads == nil {
		return QueueDropResult{}, &queue.DropError{
			Reason:         queue.DropReasonLedgerUnavailable,
			NormalizedName: name,
			QueueID:        snapshot.Queue.QueueID,
		}
	}
	for _, id := range plan.DroppedItems {
		status, lookupErr := c.beads.LookupStatus(ctx, id)
		if lookupErr != nil {
			return QueueDropResult{}, &queue.DropError{
				Reason:         queue.DropReasonReadFailed,
				NormalizedName: name,
				QueueID:        snapshot.Queue.QueueID,
				BeadID:         id,
				Cause:          lookupErr,
			}
		}
		if status == queue.BeadStatusOpen || status == queue.BeadStatusInProgress {
			return QueueDropResult{}, &queue.DropError{
				Reason:         queue.DropReasonBeadNotClosed,
				NormalizedName: name,
				QueueID:        snapshot.Queue.QueueID,
				BeadID:         id,
				BeadStatus:     status,
			}
		}
	}

	archivePath, archErr := queue.ArchiveFailedQueue(ctx, c.projectDir, name, time.Now())
	if archErr != nil {
		return QueueDropResult{}, &queue.DropError{
			Reason:         queue.DropReasonWriteFailed,
			NormalizedName: name,
			QueueID:        snapshot.Queue.QueueID,
			Cause:          archErr,
		}
	}
	c.store.ClearQueueByName(name)

	return QueueDropResult{
		Queue:        name,
		QueueID:      snapshot.Queue.QueueID,
		Dropped:      plan.DroppedItems,
		DroppedCount: len(plan.DroppedItems),
		ArchivePath:  archivePath,
	}, nil
}

func (d *socketDispatch) queueRecover(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.recoverh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: QueueRecoveryHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := d.recoverh.HandleQueueRecover(ctx, req.Queue)
	if err != nil {
		return socketrouter.Result{
			OK:        false,
			Err:       fmt.Sprintf("daemon: queue-recover: %v", err),
			ErrorCode: queue.RecoveryCodeOf(err),
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: queue-recover: marshal result: %v", marshalErr)}
	}
	return socketrouter.Result{OK: true, Payload: payload}
}

func (d *socketDispatch) queueDrop(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.recoverh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: QueueRecoveryHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := d.recoverh.HandleQueueDrop(ctx, req.Queue)
	if err != nil {
		return socketrouter.Result{
			OK:        false,
			Err:       fmt.Sprintf("daemon: queue-drop: %v", err),
			ErrorCode: queue.DropCodeOf(err),
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: queue-drop: marshal result: %v", marshalErr)}
	}
	return socketrouter.Result{OK: true, Payload: payload}
}
