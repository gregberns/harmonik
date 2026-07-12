package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

type fakeBulkLister struct {
	inflight    []core.BeadRecord
	open        []core.BeadRecord
	inflightErr error
	openErr     error
}

func (f *fakeBulkLister) ListInFlightBeads(context.Context) ([]core.BeadRecord, error) {
	return f.inflight, f.inflightErr
}

func (f *fakeBulkLister) ListBeadsByStatus(_ context.Context, _ string) ([]core.BeadRecord, error) {
	return f.open, f.openErr
}

func rec(id string, st core.CoarseStatus) core.BeadRecord {
	return core.BeadRecord{BeadID: core.BeadID(id), Title: "t", BeadType: "task", Status: st, AuditTrailRef: "a"}
}

// hk-hju8n: an open or in_progress bead is returned from the snapshot; anything
// absent (deleted/closed/blocked) yields ErrBeadNotFound so the reconcile skips
// its reset — the same conservative outcome as the per-bead reader.
func TestCachedOrphanStatusReader_HitAndMiss(t *testing.T) {
	c := newCachedOrphanStatusReader(context.Background(), &fakeBulkLister{
		inflight: []core.BeadRecord{rec("hk-inflight", core.CoarseStatusInProgress)},
		open:     []core.BeadRecord{rec("hk-open", core.CoarseStatusOpen)},
	})
	if c == nil {
		t.Fatal("expected non-nil cache")
	}

	for _, id := range []core.BeadID{"hk-inflight", "hk-open"} {
		got, err := c.ShowBead(context.Background(), id)
		if err != nil {
			t.Fatalf("ShowBead(%s): unexpected err %v", id, err)
		}
		if got.BeadID != id {
			t.Fatalf("ShowBead(%s): got id %s", id, got.BeadID)
		}
	}

	// Deleted / non-resettable bead → ErrBeadNotFound (skip reset).
	if _, err := c.ShowBead(context.Background(), "hk-deleted"); !errors.Is(err, brcli.ErrBeadNotFound) {
		t.Fatalf("ShowBead(missing): want ErrBeadNotFound, got %v", err)
	}
}

// A bulk-list error yields a nil cache so the caller falls back to the
// per-bead reader (no silent regression that skips legitimate resets).
func TestCachedOrphanStatusReader_ListErrorFallsBack(t *testing.T) {
	if c := newCachedOrphanStatusReader(context.Background(), &fakeBulkLister{inflightErr: errors.New("boom")}); c != nil {
		t.Fatal("expected nil cache on ListInFlightBeads error")
	}
	if c := newCachedOrphanStatusReader(context.Background(), &fakeBulkLister{openErr: errors.New("boom")}); c != nil {
		t.Fatal("expected nil cache on ListBeadsByStatus error")
	}
}
