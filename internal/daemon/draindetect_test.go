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

// fakeLister is an openBeadLister fake. byStatus maps a br status string to the
// records ListBeadsByStatus returns for it; err (when non-nil) is returned for
// every call (the fail-closed path). A missing status key yields nil (empty).
type fakeLister struct {
	byStatus map[string][]core.BeadRecord
	err      error
}

func (f *fakeLister) ListBeadsByStatus(_ context.Context, status string) ([]core.BeadRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byStatus[status], nil
}

// fakeLedger is a queue.BeadLedger fake reusing the internal/queue edge-map
// convention (edges[[2]{blocker,blocked}]==true means blocker must complete
// before blocked may start). edgeErr (when non-nil) is returned for every
// BlocksEdge call (the fail-closed path). LookupStatus is an unused stub —
// scanOpenEpics keys only off BlocksEdge.
type fakeLedger struct {
	edges   map[[2]core.BeadID]bool
	edgeErr error
}

func (f *fakeLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (f *fakeLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	if f.edgeErr != nil {
		return false, f.edgeErr
	}
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// drainedLister / drainedLedger are the empty (positively-drained) epic-axis
// seams: no open epics, no blocked children, no blocks edges. Phase A tests use
// them now that the epic axis is real (the Phase-A DRAINED stub is gone, so a
// nil seam would fail-close to UNSURE).
func drainedLister() *fakeLister { return &fakeLister{} }
func drainedLedger() *fakeLedger { return &fakeLedger{} }

// openEpic / blockedChild build minimal epic-axis BeadRecords. An open epic
// reports status "open" with type "epic"; a child waiting on an open blocker
// reports status "blocked".
func openEpic(id core.BeadID) core.BeadRecord {
	return core.BeadRecord{BeadID: id, BeadType: beadTypeEpic, Status: core.CoarseStatusOpen}
}
func blockedChild(id core.BeadID) core.BeadRecord {
	return core.BeadRecord{BeadID: id, BeadType: "task", Status: core.CoarseStatusBlocked}
}

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
	d := NewDrainDetector(ready, drainedLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

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
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), qs, emptyTestProjectDir(t))

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
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), dir)

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
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), runs, NewQueueStore(), emptyTestProjectDir(t))

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
	d := NewDrainDetector(&fakeReady{err: sentinel}, drainedLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

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
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), qs, emptyTestProjectDir(t))

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
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateDrained {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateDrained, got.Reasons)
	}
}

// --- Phase B tests: ledger/epic axis (scanOpenEpics) ---------------------

// TestGenuineDrain_OpenEpicWithReadyChild asserts the load-bearing epic axis:
// an OPEN epic whose otherwise-ready child the ledger says it blocks (the child
// reports "blocked" while the epic is open, so `br ready` cannot see it) ⇒
// HAS_WORK. This is the false-DRAINED that Phase A's stub could not catch and
// the reason M1 (quiesce) sequences after this bead.
func TestGenuineDrain_OpenEpicWithReadyChild(t *testing.T) {
	const epic, child = core.BeadID("hk-epic"), core.BeadID("hk-child")
	lister := &fakeLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen):    {openEpic(epic)},
		string(core.CoarseStatusBlocked): {blockedChild(child)},
	}}
	ledger := &fakeLedger{edges: map[[2]core.BeadID]bool{{epic, child}: true}}
	d := NewDrainDetector(drainedReady(), lister, ledger, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
}

// TestGenuineDrain_LedgerLookupErrorIsUnsure asserts a BlocksEdge evaluation
// error ⇒ UNSURE (fail-closed) and surfaces the wrapped error. There IS an open
// epic and a blocked child, so the axis must evaluate BlocksEdge and cannot
// short-circuit to DRAINED.
func TestGenuineDrain_LedgerLookupErrorIsUnsure(t *testing.T) {
	sentinel := errors.New("br dep list: db locked")
	const epic, child = core.BeadID("hk-epic"), core.BeadID("hk-child")
	lister := &fakeLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen):    {openEpic(epic)},
		string(core.CoarseStatusBlocked): {blockedChild(child)},
	}}
	d := NewDrainDetector(drainedReady(), lister, &fakeLedger{edgeErr: sentinel}, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if got.State != DrainStateUnsure {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateUnsure, got.Reasons)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want wrapped %v", err, sentinel)
	}
}

// TestGenuineDrain_ListerErrorIsUnsure asserts a bead-list (br list --status)
// error in the epic axis ⇒ UNSURE (fail-closed) with the wrapped error.
func TestGenuineDrain_ListerErrorIsUnsure(t *testing.T) {
	sentinel := errors.New("br list: db locked")
	d := NewDrainDetector(drainedReady(), &fakeLister{err: sentinel}, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if got.State != DrainStateUnsure {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateUnsure, got.Reasons)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want wrapped %v", err, sentinel)
	}
}

// TestGenuineDrain_NilEpicSeamIsUnsure asserts the defensive fail-closed guard:
// a nil lister/ledger seam cannot prove the epic axis empty, so the verdict is
// UNSURE — a nil seam must never license a DRAINED (the load-bearing invariant
// that lets M1 wire this oracle to the sleep decision).
func TestGenuineDrain_NilEpicSeamIsUnsure(t *testing.T) {
	d := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateUnsure {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateUnsure, got.Reasons)
	}
}

// TestGenuineDrain_DeferredLedgerDepItemIsWork asserts the ledger-dep axis's
// queue portion (defense #2): any queue item in status deferred-for-ledger-dep
// ⇒ HAS_WORK, even with every other axis drained.
func TestGenuineDrain_DeferredLedgerDepItemIsWork(t *testing.T) {
	qs := NewQueueStore()
	qs.SetQueue(&queue.Queue{
		Name:   queue.QueueNameMain,
		Status: queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusActive,
			Items: []queue.Item{{BeadID: "hk-deferred", Status: queue.ItemStatusDeferredForLedgerDep}},
		}},
	})
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), qs, emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
}

// TestGenuineDrain_KerfNotConsulted asserts the oracle's dispatch authority is
// `br ready --limit 0` (the ReadyAll seam) and the ledger — NEVER `kerf next`.
// kerf next false-reports empty for works lacking a bead_filter, so a bead it
// would hide must still drive HAS_WORK. DrainDetector has no kerf seam by
// construction; this guards that a ready-but-kerf-hidden bead is still seen.
func TestGenuineDrain_KerfNotConsulted(t *testing.T) {
	ready := &fakeReady{records: []core.BeadRecord{{BeadID: "hk-kerf-hidden"}}}
	d := NewDrainDetector(ready, drainedLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateHasWork {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateHasWork, got.Reasons)
	}
	if ready.readyAllN == 0 {
		t.Errorf("ReadyAll (br ready --limit 0) was never consulted — it is the dispatch authority, not kerf")
	}
}

// TestGenuineDrain_TrulyDrainedReturnsDrained is the full positive: every axis
// empty ⇒ DRAINED. It includes an OPEN epic with NO blocked child to prove a
// childless open epic does not block drain (only an open epic WITH a ready-but-
// blocked child is work).
func TestGenuineDrain_TrulyDrainedReturnsDrained(t *testing.T) {
	lister := &fakeLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen): {openEpic("hk-childless-epic")},
	}}
	d := NewDrainDetector(drainedReady(), lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	got, err := d.GenuineDrain(context.Background())
	if err != nil {
		t.Fatalf("GenuineDrain: unexpected error: %v", err)
	}
	if got.State != DrainStateDrained {
		t.Fatalf("State = %q; want %q (reasons: %v)", got.State, DrainStateDrained, got.Reasons)
	}
}
