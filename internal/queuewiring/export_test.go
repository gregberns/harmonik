package queuewiring

import (
	"context"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ExportedQueueOpConsumerHandlePauseStatus invokes the unexported
// handleOperatorPauseStatus method for tests in package queuewiring_test.
//
// Bead ref: hk-7urls.
func ExportedQueueOpConsumerHandlePauseStatus(c *QueueOperatorEventConsumer, ctx context.Context, evt core.Event) error {
	return c.handleOperatorPauseStatus(ctx, evt)
}

// ExportedQueueOpConsumerHandleResuming invokes the unexported
// handleOperatorResuming method for tests in package queuewiring_test.
//
// Bead ref: hk-7urls.
func ExportedQueueOpConsumerHandleResuming(c *QueueOperatorEventConsumer, ctx context.Context, evt core.Event) error {
	return c.handleOperatorResuming(ctx, evt)
}

// ExportedQueueLedger is the read seam tests use to exercise the production
// BRQueueLedger.BlocksEdge / LookupStatus against a real brcli.Adapter (wired
// to a mock `br` binary). It mirrors the queue.BeadLedger surface.
type ExportedQueueLedger interface {
	LookupStatus(ctx context.Context, id core.BeadID) (queue.BeadStatus, error)
	BlocksEdge(ctx context.Context, blocker, blocked core.BeadID) (bool, error)
}

// ExportedNewBRQueueLedger constructs the production BRQueueLedger over adapter
// so package queuewiring_test can verify the ledger-dep edge direction (hk-dv8qv).
func ExportedNewBRQueueLedger(adapter *brcli.Adapter) ExportedQueueLedger {
	return NewBRQueueLedger(adapter)
}
