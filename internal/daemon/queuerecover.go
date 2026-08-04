package daemon

// queuerecover.go — the `queue-recover` socket op.
//
// A queue that reaches `paused-by-failure` (QM-052) stops dispatching. Before
// this op the only exit was a daemon restart plus a fresh submit, so a single
// failed item ended a whole pass. `queue-recover` is the operator's way out: it
// re-arms the failed items and returns the queue to active through the QM-052b
// transaction.
//
// It is a DISTINCT verb from `operator-resume`, not a mode of it. The two touch
// different state. `operator-resume` releases a drain pause and only flips
// Queue.status. Recovery rewrites per-item status, attempt counts, and failure
// reasons, and it reopens groups. Keeping them apart is what lets
// `harmonik queue resume` refuse a failure-parked queue with a typed error
// instead of reporting a success that dispatches nothing.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b; specs/process-lifecycle.md
// §4.4 PL-003a (method registry).

import (
	"context"
	"encoding/json"
	"fmt"

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

// queueRecover is the socket adapter for the `queue-recover` op. It puts the
// QM-052b code from a *queue.RecoveryError onto the wire so a caller can tell
// the seven refusal causes apart without parsing the message.
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
