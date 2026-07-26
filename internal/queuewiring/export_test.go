package queuewiring

// export_test.go — test seams for package queuewiring_test.
//
// These four shims were lifted verbatim from internal/daemon/export_test.go by
// P2 unit E3a: their sole callers moved into this package with the code they
// exercise, and their bodies reach unexported methods (handleOperatorPauseStatus,
// handleOperatorResuming) that package daemon can no longer see.
//
// Shims whose callers STAYED in internal/daemon (ExportedNewQueueStore,
// ExportedQueueStoreOf, ExportedQueueStoreSetQueue,
// ExportedNewQueueOperatorEventConsumer, ExportedQueueOperatorEventConsumerConfig)
// stay there, re-typed to *queuewiring.… — see internal/daemon/export_test.go.
//
// Plan ref: plans/2026-07-21-p2-extraction/E3-queue-wiring.md §4 STEP 5.

import (
	"context"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// QueueOperatorEventConsumer test seams (hk-7urls)
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// BRQueueLedger test seam (hk-dv8qv — ledger-dep direction regression)
// ─────────────────────────────────────────────────────────────────────────────

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
