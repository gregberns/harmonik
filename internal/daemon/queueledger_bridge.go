package daemon

// queueledger_bridge.go — bridges *brcli.Adapter to queue.BeadLedger.
//
// queue.BeadLedger (in internal/queue/validation.go) requires LookupStatus
// and BlocksEdge. brcli.Adapter exposes ShowBead (for status) and
// ListDependencies (for edge data). This file provides a thin adapter so
// the daemon composition root can pass a single brcli.Adapter as the
// queue.BeadLedger for queue.NewHandlerAdapter.
//
// Spec ref: specs/queue-model.md §2.10 (validation pipeline ledger seam).
// Spec ref: specs/beads-integration.md §4.5 BI-015.

import (
	"context"
	"errors"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// brQueueLedger wraps *brcli.Adapter and satisfies queue.BeadLedger.
//
// It is constructed once at daemon.Start (composition root) and shared
// between queue.NewHandlerAdapter and lifecycle.LoadQueueAtStartup.
type brQueueLedger struct {
	adapter *brcli.Adapter
}

// newBRQueueLedger returns a brQueueLedger wrapping adapter.
func newBRQueueLedger(adapter *brcli.Adapter) *brQueueLedger {
	return &brQueueLedger{adapter: adapter}
}

// LookupStatus implements queue.BeadLedger.LookupStatus.
//
// Uses ShowBead to retrieve the bead record and maps core.CoarseStatus to
// queue.BeadStatus. Unknown/closed statuses are treated as not-found for
// queue-submit validation purposes (the submission would fail on a different
// rule if the bead is truly unworkable).
func (b *brQueueLedger) LookupStatus(ctx context.Context, id core.BeadID) (queue.BeadStatus, error) {
	record, err := b.adapter.ShowBead(ctx, id)
	if err != nil {
		// brcli.ErrBeadNotFound → BeadStatusNotFound; other errors surface as-is.
		if errors.Is(err, brcli.ErrBeadNotFound) {
			return queue.BeadStatusNotFound, nil
		}
		return "", err
	}
	switch record.Status {
	case core.CoarseStatusOpen:
		return queue.BeadStatusOpen, nil
	case core.CoarseStatusInProgress:
		return queue.BeadStatusInProgress, nil
	default:
		// Blocked, deferred, closed, etc. — treat as not-found from the
		// queue-submission validation perspective (QM-020: bead must be open).
		// Open question: should draft/pinned map to BeadStatusOpen? Treat as
		// not-found for now to fail-safe (keeps queue items workable-only).
		return queue.BeadStatusNotFound, nil
	}
}

// BlocksEdge implements queue.BeadLedger.BlocksEdge.
//
// Contract (queue.BeadLedger): returns true iff blocker must complete before
// blocked may start — i.e. blocked DEPENDS ON blocker.
//
// Beads edge-direction (verified against live `br dep list` and the brcli
// fixture in listdependencies_test.go): a "blocks" dependency is stored with
// issue_id = the BLOCKED (dependent) bead and depends_on_id = the BLOCKER bead.
// brcli.ListDependencies maps issue_id → FromBeadID and depends_on_id →
// ToBeadID, so the edge for "blocked depends on blocker" is
// {FromBeadID: blocked, ToBeadID: blocker, EdgeKind: blocks}.
//
// We therefore list the dependencies OF blocked (its outgoing edges) and scan
// for a blocks edge whose target (ToBeadID) is blocker. Returns false if either
// bead is unknown.
//
// Fixes hk-dv8qv: the prior implementation listed dependencies of blocker and
// matched FromBeadID==blocker / ToBeadID==blocked, which is the reversed edge
// direction. That made BlocksEdge(a,b) report true when a DEPENDS ON b,
// inverting all queue ledger-dep deferral — roots of a chain were deferred
// while leaves with open blockers were dispatched out of order.
func (b *brQueueLedger) BlocksEdge(ctx context.Context, blocker, blocked core.BeadID) (bool, error) {
	edges, err := b.adapter.ListDependencies(ctx, blocked)
	if err != nil {
		if errors.Is(err, brcli.ErrBeadNotFound) {
			return false, nil
		}
		return false, err
	}
	for _, e := range edges {
		if e.EdgeKind == core.EdgeKindBlocks &&
			e.FromBeadID == blocked &&
			e.ToBeadID == blocker {
			return true, nil
		}
	}
	return false, nil
}
