package daemon

// draindetect_test.go — unit tests for the drain-fact oracle (hk-95uf /
// hk-pfr4). These are pure in-package unit tests in internal/daemon (NOT
// daemon-boot scenario tests), so they carry no 30-minute commit-budget risk.
//
// Phase A: queue / run-state / ready axes (via GenuineDrain bridge).
// Phase B: ledger/epic axis (via GenuineDrain bridge).
// Phase C (hk-pfr4): GatherDrainFacts — new axes (InProgress / Draft /
//
//	Deferred / NeedsDecomposition / NeedsAttention), no-short-circuit,
//	Unsure-as-flag, counts + lists.

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

// --- Phase C tests: GatherDrainFacts (hk-pfr4) ---------------------------

// fullLister is an openBeadLister fake that serves beads by status and also
// lets tests assert which statuses were queried.
type fullLister struct {
	byStatus map[string][]core.BeadRecord
	errByStatus map[string]error // per-status errors
	queried  []string
}

func (f *fullLister) ListBeadsByStatus(_ context.Context, status string) ([]core.BeadRecord, error) {
	f.queried = append(f.queried, status)
	if f.errByStatus != nil {
		if err, ok := f.errByStatus[status]; ok {
			return nil, err
		}
	}
	return f.byStatus[status], nil
}

// minimalBead builds a BeadRecord with enough fields for filter / projection
// logic (does NOT satisfy BeadRecord.Valid() since AuditTrailRef is empty).
func minimalBead(id core.BeadID, status core.CoarseStatus, btype string, labels ...string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:   id,
		Title:    string(id),
		BeadType: btype,
		Status:   status,
		Labels:   labels,
	}
}

// drainedFullLister returns a fullLister that answers every status with nil
// (empty) — positively drained on all ledger axes.
func drainedFullLister() *fullLister {
	return &fullLister{byStatus: map[string][]core.BeadRecord{}}
}

// TestGatherDrainFacts_InProgressAxisPopulated asserts the in_progress axis is
// populated from the ledger — a bucket GenuineDrain dropped entirely.
func TestGatherDrainFacts_InProgressAxisPopulated(t *testing.T) {
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusInProgress): {
			minimalBead("hk-running-1", core.CoarseStatusInProgress, "task"),
			minimalBead("hk-running-2", core.CoarseStatusInProgress, "bug"),
		},
	}}
	d := NewDrainDetector(drainedReady(), lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.InProgress.Count != 2 {
		t.Errorf("InProgress.Count = %d; want 2", facts.InProgress.Count)
	}
	if len(facts.InProgress.Beads) != 2 {
		t.Errorf("len(InProgress.Beads) = %d; want 2", len(facts.InProgress.Beads))
	}
}

// TestGatherDrainFacts_DraftAndDeferredAxes asserts draft and deferred buckets
// are populated (both are dropped by GenuineDrain / br ready).
func TestGatherDrainFacts_DraftAndDeferredAxes(t *testing.T) {
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusDraft):    {minimalBead("hk-draft", core.CoarseStatusDraft, "task")},
		string(core.CoarseStatusDeferred): {minimalBead("hk-def", core.CoarseStatusDeferred, "task")},
	}}
	d := NewDrainDetector(drainedReady(), lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.Draft.Count != 1 {
		t.Errorf("Draft.Count = %d; want 1", facts.Draft.Count)
	}
	if facts.Deferred.Count != 1 {
		t.Errorf("Deferred.Count = %d; want 1", facts.Deferred.Count)
	}
}

// TestGatherDrainFacts_NeedsAttentionVisible asserts that beads carrying the
// needs-attention label surface in the NeedsAttention axis. ReadyAll silently
// excludes them; the fact bundle makes the exclusion visible.
func TestGatherDrainFacts_NeedsAttentionVisible(t *testing.T) {
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen): {
			minimalBead("hk-attention", core.CoarseStatusOpen, "task", labelNeedsAttention),
			minimalBead("hk-normal", core.CoarseStatusOpen, "task"),
		},
	}}
	d := NewDrainDetector(drainedReady(), lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.NeedsAttention.Count != 1 {
		t.Errorf("NeedsAttention.Count = %d; want 1", facts.NeedsAttention.Count)
	}
	if len(facts.NeedsAttention.Beads) != 1 || facts.NeedsAttention.Beads[0].ID != "hk-attention" {
		t.Errorf("NeedsAttention.Beads = %v; want [{hk-attention ...}]", facts.NeedsAttention.Beads)
	}
}

// TestGatherDrainFacts_NeedsDecomposition asserts that a childless open epic
// is reported in NeedsDecomposition — the one generative axis. An open epic
// WITH a child must NOT appear in NeedsDecomposition.
func TestGatherDrainFacts_NeedsDecomposition(t *testing.T) {
	const (
		epicWithChild    = core.BeadID("hk-epic-with-child")
		epicWithoutChild = core.BeadID("hk-epic-childless")
		childA           = core.BeadID("hk-child")
	)
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen): {
			openEpic(epicWithChild),
			openEpic(epicWithoutChild),
		},
		string(core.CoarseStatusBlocked): {blockedChild(childA)},
	}}
	ledger := &fakeLedger{edges: map[[2]core.BeadID]bool{
		{epicWithChild, childA}: true,
	}}
	d := NewDrainDetector(drainedReady(), lister, ledger, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if len(facts.NeedsDecomposition) != 1 || facts.NeedsDecomposition[0] != epicWithoutChild {
		t.Errorf("NeedsDecomposition = %v; want [%s]", facts.NeedsDecomposition, epicWithoutChild)
	}
}

// TestGatherDrainFacts_NoShortCircuitOnMultipleEdges asserts the no-short-
// circuit invariant: ALL open-epic → child edges are emitted. GenuineDrain's
// scanOpenEpics returned on the first blocked edge; GatherDrainFacts must
// walk to completion.
func TestGatherDrainFacts_NoShortCircuitOnMultipleEdges(t *testing.T) {
	const (
		epic   = core.BeadID("hk-epic")
		childA = core.BeadID("hk-child-a")
		childB = core.BeadID("hk-child-b")
	)
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusOpen): {openEpic(epic)},
		string(core.CoarseStatusBlocked): {
			blockedChild(childA),
			blockedChild(childB),
		},
	}}
	ledger := &fakeLedger{edges: map[[2]core.BeadID]bool{
		{epic, childA}: true,
		{epic, childB}: true,
	}}
	d := NewDrainDetector(drainedReady(), lister, ledger, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if len(facts.BlockedByOpenEpic) != 2 {
		t.Errorf("BlockedByOpenEpic len = %d; want 2 (all edges, no short-circuit)", len(facts.BlockedByOpenEpic))
	}
}

// TestGatherDrainFacts_UnsureIsFlagNotVerdict asserts that an axis read error
// sets facts.Unsure = true but does NOT prevent other axes from being populated.
// GenuineDrain short-circuited to UNSURE; GatherDrainFacts must continue.
func TestGatherDrainFacts_UnsureIsFlagNotVerdict(t *testing.T) {
	sentinel := errors.New("br ready: db locked")
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusInProgress): {
			minimalBead("hk-running", core.CoarseStatusInProgress, "task"),
		},
	}}
	d := NewDrainDetector(&fakeReady{err: sentinel}, lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want wrapped %v", err, sentinel)
	}
	if !facts.Unsure {
		t.Errorf("facts.Unsure = false; want true (ready axis failed)")
	}
	if len(facts.UnsureReasons) == 0 {
		t.Errorf("UnsureReasons is empty; want at least one reason")
	}
	// In-progress axis must still be populated despite the ready-axis error.
	if facts.InProgress.Count != 1 {
		t.Errorf("InProgress.Count = %d; want 1 (axes continue past error)", facts.InProgress.Count)
	}
}

// TestGatherDrainFacts_QueueAxisNonTerminalItem asserts the queued axis
// captures non-terminal items correctly (regression against scanQueues path).
func TestGatherDrainFacts_QueueAxisNonTerminalItem(t *testing.T) {
	qs := NewQueueStore()
	qs.SetQueue(&queue.Queue{
		Name:   queue.QueueNameMain,
		Status: queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-pending", Status: queue.ItemStatusPending},
				{BeadID: "hk-done", Status: queue.ItemStatusCompleted},
			},
		}},
	})
	d := NewDrainDetector(drainedReady(), drainedFullLister(), drainedLedger(), NewRunRegistry(), qs, emptyTestProjectDir(t))

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.Queued.Count != 1 {
		t.Errorf("Queued.Count = %d; want 1 (one non-terminal item)", facts.Queued.Count)
	}
	if len(facts.Queued.NonTerminalItems) != 1 || facts.Queued.NonTerminalItems[0].BeadID != "hk-pending" {
		t.Errorf("Queued.NonTerminalItems = %v; want [{main hk-pending pending}]", facts.Queued.NonTerminalItems)
	}
}

// TestGatherDrainFacts_WorktreePathsPopulated asserts live worktree paths
// appear in RunAxis (the old liveWorktrees returned a count; new shape returns
// paths so the captain can identify stale-vs-live worktrees).
func TestGatherDrainFacts_WorktreePathsPopulated(t *testing.T) {
	dir := emptyTestProjectDir(t)
	wtDir := filepath.Join(dir, ".harmonik", "worktrees", "019e-fake-run")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	d := NewDrainDetector(drainedReady(), drainedFullLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), dir)

	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.Runs.LiveWorktrees != 1 {
		t.Errorf("Runs.LiveWorktrees = %d; want 1", facts.Runs.LiveWorktrees)
	}
	if len(facts.Runs.WorktreePaths) != 1 {
		t.Errorf("Runs.WorktreePaths = %v; want 1 path", facts.Runs.WorktreePaths)
	}
}

// TestGatherDrainFacts_GatheredAtIsSet asserts GatheredAt is non-zero.
func TestGatherDrainFacts_GatheredAtIsSet(t *testing.T) {
	d := NewDrainDetector(drainedReady(), drainedFullLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	facts, err := d.GatherDrainFacts(context.Background())
	if err != nil {
		t.Fatalf("GatherDrainFacts: unexpected error: %v", err)
	}
	if facts.GatheredAt.IsZero() {
		t.Errorf("GatheredAt is zero; want a non-zero timestamp")
	}
}
