package daemon

// draindetect_test.go — RED-then-GREEN unit tests for the genuine-drain oracle
// (hk-95uf). These are pure in-package unit tests in internal/daemon (NOT
// daemon-boot scenario tests), so they carry no 30-minute commit-budget risk.
//
// Phase A (this file's first block): queue / run-state / ready axes — the
// minimal-viable oracle. Phase B tests (ledger/epic axis) live alongside in the
// second block once scanOpenEpics is implemented.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// --- fakes ---------------------------------------------------------------

// fakeReady is a readySource fake. When err is non-nil ReadyAll returns it;
// otherwise it returns records. It also records that ReadyAll (the --limit 0
// path) was the method invoked, never the paginated Ready — defense #1.
type fakeReady struct {
	records   []core.BeadRecord
	err       error
	readyAllN int
}

func (f *fakeReady) ReadyAll(_ context.Context) ([]core.BeadRecord, error) {
	f.readyAllN++
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

// drainedReady is a readySource that always reports zero dispatchable beads.
func drainedReady() *fakeReady { return &fakeReady{records: []core.BeadRecord{}} }

// emptyTestProjectDir returns a temp dir with an empty .harmonik/queues and
// .harmonik/worktrees, the on-disk baseline of a fully-drained project.
func emptyTestProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"queues", "worktrees"} {
		if err := os.MkdirAll(filepath.Join(dir, ".harmonik", sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return dir
}

// --- Phase A tests -------------------------------------------------------

// TestGenuineDrain_PaginatedReadyHidesWork asserts the oracle keys off ReadyAll
// (`br ready --limit 0`), not a default-paginated empty. The seam exposes ONLY
// ReadyAll (defense #1), so a non-empty ReadyAll ⇒ HAS_WORK even when a naive
// paginated query might have returned empty.
func TestGenuineDrain_PaginatedReadyHidesWork(t *testing.T) {
	ready := &fakeReady{records: []core.BeadRecord{{BeadID: "hk-hidden"}}}
	d := NewDrainDetector(ready, nil, nil, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
	if ready.readyAllN == 0 {
		t.Errorf("ReadyAll was never called — oracle must use the un-paginated path")
	}
}

// TestGenuineDrain_PausedByFailureIsStuck asserts a paused-by-failure queue ⇒
// HAS_WORK (defense #3, in-memory portion).
func TestGenuineDrain_PausedByFailureIsStuck(t *testing.T) {
	qs := NewQueueStore()
	qs.SetQueue(&queue.Queue{
		Name:   queue.QueueNameMain,
		Status: queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{{
			GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusActive,
			Items: []queue.Item{{BeadID: "hk-a", Status: queue.ItemStatusFailed}},
		}},
	})
	d := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), qs, emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
}

// TestGenuineDrain_FailedArchiveFileIsStuck asserts an un-reconciled
// `.json.failed-*` archive on disk ⇒ HAS_WORK (defense #3, on-disk portion),
// even though the queue is absent from the in-memory store.
func TestGenuineDrain_FailedArchiveFileIsStuck(t *testing.T) {
	dir := emptyTestProjectDir(t)
	archive := filepath.Join(dir, ".harmonik", "queues", "main.json.failed-20260101000000")
	if err := os.WriteFile(archive, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	d := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), NewQueueStore(), dir)

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
}

// TestGenuineDrain_InFlightRunBlocksDrain asserts a registered in-flight run ⇒
// HAS_WORK (defense #4).
func TestGenuineDrain_InFlightRunBlocksDrain(t *testing.T) {
	runs := NewRunRegistry()
	runs.Register(
		core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000abc")),
		&RunHandle{BeadID: "hk-running"},
	)
	d := NewDrainDetector(drainedReady(), nil, nil, runs, NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
}

// TestGenuineDrain_BrExecErrorIsUnsure asserts a br-ready exec error ⇒ UNSURE
// and surfaces the error (fail-closed toward staying awake).
func TestGenuineDrain_BrExecErrorIsUnsure(t *testing.T) {
	sentinel := errors.New("br ready: db locked")
	d := NewDrainDetector(&fakeReady{err: sentinel}, nil, nil, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if got.State != DrainStateUnsure {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateUnsure, got.Reasons)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want wrapped %v", err, sentinel)
	}
}

// TestGenuineDrain_TerminalUnrolledRaceIsUnsure asserts the "all items terminal
// but queue status not yet rolled to completed" race ⇒ UNSURE, never DRAINED.
func TestGenuineDrain_TerminalUnrolledRaceIsUnsure(t *testing.T) {
	qs := NewQueueStore()
	qs.SetQueue(&queue.Queue{
		Name:   queue.QueueNameMain,
		Status: queue.QueueStatusActive, // not yet rolled to completed
		Groups: []queue.Group{{
			GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-a", Status: queue.ItemStatusCompleted},
				{BeadID: "hk-b", Status: queue.ItemStatusCompleted},
			},
		}},
	})
	d := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), qs, emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateUnsure {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateUnsure, got.Reasons)
	}
}

// TestGenuineDrain_PhaseADrained asserts the queue/run/ready axes alone return
// DRAINED when every axis shows positive emptiness. (Phase B extends this with
// the epic axis in TestGenuineDrain_TrulyDrainedReturnsDrained.)
func TestGenuineDrain_PhaseADrained(t *testing.T) {
	d := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateDrained {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateDrained, got.Reasons)
	}
}
