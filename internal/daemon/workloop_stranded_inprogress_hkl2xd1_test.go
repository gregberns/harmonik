package daemon_test

// workloop_stranded_inprogress_hkl2xd1_test.go — stranded in_progress bead
// auto-reset (hk-l2xd1).
//
// Covers the live-lock scenario observed on 2026-06-25: a bead is in_progress
// in the ledger but has no active run in either the in-memory RunRegistry or
// the on-disk .harmonik/runs/ directory. The dispatch loop observes in_progress
// at pre-claim (BI-013c), emits bead_claim_skipped, and—in the absence of the
// fix—sets the queue item to deferred-for-ledger-dep. Because the item has no
// blocking siblings, ReevaluateDeferred un-defers it immediately and the loop
// spins every ~2.5s, starving sibling queue items.
//
// Fix: when in_progress is observed, no run is registered in-memory, and no
// on-disk record exists, the daemon calls ResetBead (bead → open) and leaves
// the queue item as pending. The next dispatch tick sees the bead as open and
// claims it normally.
//
// Tests:
//   - TestHKL2XD1_StrandedInProgress_AutoReset: resetter wired → ResetBead called,
//     queue item stays pending.
//   - TestHKL2XD1_StrandedInProgress_NoResetter: resetter nil (backward-compat) →
//     ResetBead not called, item goes to deferred-for-ledger-dep.
//   - TestHKL2XD1_StrandedInProgress_LiveRun: run active in RunRegistry → resetter
//     not called, item goes to deferred-for-ledger-dep.

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// hkl2xd1StrandedLedger is a stub beadLedger whose ShowBead always returns
// in_progress. ClaimBead records calls so tests can assert it was not invoked.
type hkl2xd1StrandedLedger struct {
	claimCalls atomic.Int64
	showSeen   chan struct{}
	showOnce   sync.Once
}

func newHKL2XD1StrandedLedger() *hkl2xd1StrandedLedger {
	return &hkl2xd1StrandedLedger{showSeen: make(chan struct{})}
}

func (l *hkl2xd1StrandedLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil // queue-only dispatch
}

func (l *hkl2xd1StrandedLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.showOnce.Do(func() { close(l.showSeen) })
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusInProgress}, nil
}

func (l *hkl2xd1StrandedLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimCalls.Add(1)
	return nil
}

func (l *hkl2xd1StrandedLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *hkl2xd1StrandedLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// hkl2xd1FakeResetter records ResetBead calls.
type hkl2xd1FakeResetter struct {
	mu         sync.Mutex
	resetCalls []core.BeadID
	resetSeen  chan struct{}
	resetOnce  sync.Once
}

func newHKL2XD1FakeResetter() *hkl2xd1FakeResetter {
	return &hkl2xd1FakeResetter{resetSeen: make(chan struct{})}
}

func (r *hkl2xd1FakeResetter) ResetBead(
	_ context.Context, _ string, _ brcli.TimeoutConfig,
	beadID core.BeadID, _ core.ProjectHash, _ int64,
) error {
	r.mu.Lock()
	r.resetCalls = append(r.resetCalls, beadID)
	r.mu.Unlock()
	r.resetOnce.Do(func() { close(r.resetSeen) })
	return nil
}

func (r *hkl2xd1FakeResetter) called() []core.BeadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.BeadID, len(r.resetCalls))
	copy(out, r.resetCalls)
	return out
}

// hkl2xd1BuildQueue returns a queue with a single pending item for beadID.
func hkl2xd1BuildQueue(beadID core.BeadID) *queue.Queue {
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hkl2xd1-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
				CreatedAt:  now,
			},
		},
	}
}

// TestHKL2XD1_StrandedInProgress_AutoReset: resetter wired, no active run →
// ResetBead is called, ClaimBead is never called, queue item stays pending.
func TestHKL2XD1_StrandedInProgress_AutoReset(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	beadID := core.BeadID("hk-l2xd1-stranded")
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hkl2xd1BuildQueue(beadID))

	ledger := newHKL2XD1StrandedLedger()
	resetter := newHKL2XD1FakeResetter()
	bus := &stubEventCollector{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:                  ledger,
		Bus:                        bus,
		ProjectDir:                 projectDir,
		HandlerBinary:              "/bin/sh",
		HandlerArgs:                []string{"-c", "exit 0"},
		IntentLogDir:               filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:                 qs,
		MaxConcurrent:              1,
		AdapterRegistry2:           NewSealedAdapterRegistryForTest(t),
		StrandedInProgressResetter: resetter,
		StrandedResetDaemonNS:      1_700_000_000_000_000_001,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait for ResetBead to fire.
	select {
	case <-resetter.resetSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ResetBead not called within 15s — stranded-bead auto-reset did not fire (hk-l2xd1)")
	}

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel")
	}

	// ResetBead must have been called for the stranded bead.
	if calls := resetter.called(); len(calls) == 0 || calls[0] != beadID {
		t.Errorf("ResetBead calls = %v; want [%s] (hk-l2xd1)", calls, beadID)
	}

	// ClaimBead must NOT be called — the bead is in_progress, not open.
	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — stranded bead must not be claimed (hk-l2xd1)", n)
	}

	// Queue item must remain pending (not deferred-for-ledger-dep) because the
	// auto-reset path skips the deferred step on success.
	if liveQ := daemon.ExportedQueueStoreOf(deps).Queue(); liveQ != nil &&
		len(liveQ.Groups) > 0 && len(liveQ.Groups[0].Items) > 0 {
		item := liveQ.Groups[0].Items[0]
		if item.Status == queue.ItemStatusDeferredForLedgerDep {
			t.Errorf("queue item status = deferred-for-ledger-dep; want pending — auto-reset must not defer (hk-l2xd1)")
		}
	}
}

// TestHKL2XD1_StrandedInProgress_NoResetter: StrandedInProgressResetter is
// nil → backward-compat: no ResetBead call, item goes to deferred-for-ledger-dep.
func TestHKL2XD1_StrandedInProgress_NoResetter(t *testing.T) {
	t.Parallel()
	// Reuse the existing BI-013c test body: nil resetter → DeferredForLedgerDep.
	bi013cRunTest(t, "hk-l2xd1-no-resetter", core.CoarseStatusInProgress, queue.ItemStatusDeferredForLedgerDep)
}

// TestHKL2XD1_StrandedInProgress_LiveRun: an in-flight run for the bead is
// registered in the RunRegistry → resetter must NOT be called (the run is live).
func TestHKL2XD1_StrandedInProgress_LiveRun(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	beadID := core.BeadID("hk-l2xd1-live-run")
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hkl2xd1BuildQueue(beadID))

	ledger := newHKL2XD1StrandedLedger()
	resetter := newHKL2XD1FakeResetter()
	bus := &stubEventCollector{}

	// Pre-register a fake run for this bead in the RunRegistry so HasBeadRun
	// returns true — simulating a concurrent dispatch goroutine.
	reg := daemon.NewRunRegistry()
	rawUUID, _ := uuid.Parse("01900000-0000-7000-8000-000000000001")
	runID := core.RunID(rawUUID)
	reg.Register(runID, &daemon.RunHandle{BeadID: beadID})

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:                  ledger,
		Bus:                        bus,
		ProjectDir:                 projectDir,
		HandlerBinary:              "/bin/sh",
		HandlerArgs:                []string{"-c", "exit 0"},
		IntentLogDir:               filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:                 qs,
		MaxConcurrent:              2, // allow dispatch past capacity gate while 1 fake run is registered
		AdapterRegistry2:           NewSealedAdapterRegistryForTest(t),
		RunRegistry:                reg,
		StrandedInProgressResetter: resetter,
		StrandedResetDaemonNS:      1_700_000_000_000_000_001,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait for ShowBead to be called (confirming BI-013c fired).
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead not called within 15s")
	}

	// Give the loop a brief window to (incorrectly) call ResetBead if the guard
	// is not working, then verify it was not called.
	time.Sleep(200 * time.Millisecond)

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel")
	}

	// Resetter must NOT have been called — the run is live.
	if calls := resetter.called(); len(calls) != 0 {
		t.Errorf("ResetBead called %d time(s) = %v; want 0 — live-run bead must not be auto-reset (hk-l2xd1)", len(calls), calls)
	}
}
