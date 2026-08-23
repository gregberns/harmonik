package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

type bulkBeadLister interface {
	ListInFlightBeads(ctx context.Context) ([]core.BeadRecord, error)
	ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error)
}

type cachedOrphanStatusReader struct {
	byID map[core.BeadID]core.BeadRecord
}

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
