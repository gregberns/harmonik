package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// bulkBeadLister is the read slice needed to snapshot the resettable-bead set in
// two bulk `br list` calls instead of one `br show` subprocess per bead. Both
// *brcli.Adapter methods satisfy it.
type bulkBeadLister interface {
	ListInFlightBeads(ctx context.Context) ([]core.BeadRecord, error)
	ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error)
}

// cachedOrphanStatusReader is a beadStatusReader backed by a one-shot snapshot
// of the only two statuses reconcileOrphanedRunsOnResume ever acts on: open and
// in_progress (resetGuardedBead resets a bead ONLY when it reads open or
// in_progress, and skips every other case). ShowBead is then an O(1) map lookup
// with zero subprocesses.
//
// hk-hju8n — socket-bind wedge fix. The reconcile's terminated-but-locked pass
// (hk-hjvl4) iterates EVERY bead that ever had a terminal run in the full
// durable event-log history. On a long-lived fleet that is hundreds of beads,
// most long since deleted from br. The old orphanStatusReader (raw *brcli.Adapter)
// spawned a `br show` subprocess for each — ~0.3–0.85 s apiece — so the
// synchronous reconcile, which runs BEFORE the socket listener starts, took
// minutes and was killed by the supervisor watchdog before the daemon ever bound
// its socket. Every respawn restarted the scan from scratch: a permanent wedge
// that worsens as the event log grows. Snapshotting open+in_progress up front
// collapses the whole pass to two `br list` calls.
//
// Semantics match the per-bead reader for the reset decision. A bead absent
// from the snapshot (deleted,
// closed, blocked, deferred, or any non-open/in_progress state) yields
// brcli.ErrBeadNotFound, which resetGuardedBead treats as "skip the reset (never
// risk reopening a landed bead)" — the same conservative outcome the old reader
// produced for those beads (either an ISSUE_NOT_FOUND error or a non-open/
// in_progress status that fell through to the skip branch). Only beads the
// authoritative bulk lists report as open or in_progress are ever reset.
//
// The one behavioural delta is a marginally widened TOCTOU window: the snapshot
// is taken once up front, so a bead closed EXTERNALLY (e.g. a manual operator
// `br close`) between the snapshot and its resetGuardedBead call would still be
// reset, where a per-bead `br show` would have seen it closed and skipped. This
// window is the synchronous pre-listener startup only — the daemon is not yet
// serving and owns terminal transitions — so it is a few seconds of exposure to
// a concurrent external close: practically negligible, and the reset itself only
// reverts in_progress→open (it never reopens a closed bead's terminal record).
type cachedOrphanStatusReader struct {
	byID map[core.BeadID]core.BeadRecord
}

// newCachedOrphanStatusReader snapshots the open + in_progress beads via two
// bulk list calls. On ANY bulk-list error it returns nil so the caller falls
// back to the uncached reader — behaviour then matches pre-hk-hju8n exactly
// (per-bead br show), never a silent regression that skips legitimate resets.
func newCachedOrphanStatusReader(ctx context.Context, lister bulkBeadLister) *cachedOrphanStatusReader {
	inflight, err := lister.ListInFlightBeads(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: newCachedOrphanStatusReader: ListInFlightBeads failed: %v — falling back to per-bead reader\n", err)
		return nil
	}
	open, err := lister.ListBeadsByStatus(ctx, string(core.CoarseStatusOpen))
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: newCachedOrphanStatusReader: ListBeadsByStatus(open) failed: %v — falling back to per-bead reader\n", err)
		return nil
	}

	byID := make(map[core.BeadID]core.BeadRecord, len(inflight)+len(open))
	for _, rec := range inflight {
		byID[rec.BeadID] = rec
	}
	for _, rec := range open {
		byID[rec.BeadID] = rec
	}
	return &cachedOrphanStatusReader{byID: byID}
}

// ShowBead returns the snapshotted record for an open/in_progress bead, or
// brcli.ErrBeadNotFound for anything else (skip-reset, conservative).
func (c *cachedOrphanStatusReader) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if rec, ok := c.byID[id]; ok {
		return rec, nil
	}
	return core.BeadRecord{}, brcli.ErrBeadNotFound
}
