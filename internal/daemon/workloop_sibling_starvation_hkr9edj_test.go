package daemon_test

// workloop_sibling_starvation_hkr9edj_test.go — explicit sibling-starvation
// repro for the hk-l2xd1 stranded in_progress auto-reset (hk-r9edj follow-up).
//
// A stranded in_progress bead (no run in RunRegistry or on disk) sitting at
// the head of a stream group is the exact hk-l2xd1 scenario: the pre-claim
// guard (BI-013c) sets the head item to deferred-for-ledger-dep, and
// queue.ReevaluateDeferred immediately un-defers it (no real in-group
// blockers) before the next selection — a ~2.5s spin. queue.streamEligible's
// deferred-item skip (hk-cb5ow) means a dep-free tail sibling is NOT
// permanently head-of-line blocked by this spin, so the tail sibling does
// still get claimed. What the spin DOES starve is the head bead itself: with
// no auto-reset resetter wired, it never recovers from in_progress — it can
// spin indefinitely, holding a live-lock that only a daemon restart clears
// (the original hk-l2xd1 incident). The resetter breaks that live-lock.
//
// This file proves both halves directly:
//  1. TestHKR9EDJ_SiblingStarvation_WithoutResetter: no resetter wired → the
//     stranded head bead is never reclaimed/recovered (ClaimBead(head) stays
//     0 for the whole run) even though the dep-free tail sibling eventually
//     gets claimed once — i.e. the head bead itself is starved of recovery,
//     which is exactly the live-lock hk-l2xd1 fixes.
//  2. TestHKR9EDJ_SiblingStarvation_WithResetter: resetter wired → the
//     stranded head bead is reset to open (recovers) and the sibling item is
//     also claimed.
//
// Helper prefix: hkr9edjSibling.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// hkr9edjSiblingLedger is a stub beadLedger whose ShowBead status is keyed per
// bead ID. ClaimBead records per-bead-ID call counts so a test can assert
// which specific sibling was (or was not) claimed.
type hkr9edjSiblingLedger struct {
	mu      sync.Mutex
	status  map[core.BeadID]core.CoarseStatus
	claimed map[core.BeadID]int
}

func newHKR9EDJSiblingLedger(status map[core.BeadID]core.CoarseStatus) *hkr9edjSiblingLedger {
	return &hkr9edjSiblingLedger{
		status:  status,
		claimed: make(map[core.BeadID]int),
	}
}

func (l *hkr9edjSiblingLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil // queue-only dispatch
}

func (l *hkr9edjSiblingLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	st := l.status[id]
	l.mu.Unlock()
	return core.BeadRecord{BeadID: id, Status: st}, nil
}

func (l *hkr9edjSiblingLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	l.mu.Lock()
	l.claimed[id]++
	l.mu.Unlock()
	return nil
}

func (l *hkr9edjSiblingLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *hkr9edjSiblingLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

func (l *hkr9edjSiblingLedger) claimCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claimed[id]
}

// hkr9edjSiblingQueue returns a single stream group with a stranded head item
// (headID, in_progress with no run) followed by a normal sibling (tailID).
func hkr9edjSiblingQueue(headID, tailID core.BeadID) *queue.Queue {
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hkr9edj-sibling-starvation",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: headID, Status: queue.ItemStatusPending},
					{BeadID: tailID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}
}

// TestHKR9EDJ_SiblingStarvation_WithoutResetter: no auto-reset resetter wired
// (pre-hk-l2xd1 behavior) → the stranded head bead spins on
// deferred-for-ledger-dep / un-defer and is never reclaimed for the whole
// run — the live-lock hk-l2xd1 fixes. It does not assert a hard block on the
// tail sibling: hk-cb5ow's deferred-item skip already lets a dep-free tail
// item progress despite the spin, so the residual bug this pins is squarely
// "the head bead never recovers," not "the sibling is unreachable."
func TestHKR9EDJ_SiblingStarvation_WithoutResetter(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	headID := core.BeadID("hk-r9edj-starve-head")
	tailID := core.BeadID("hk-r9edj-starve-tail")

	ledger := newHKR9EDJSiblingLedger(map[core.BeadID]core.CoarseStatus{
		headID: core.CoarseStatusInProgress, // stranded: no run anywhere
		tailID: core.CoarseStatusOpen,
	})

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hkr9edjSiblingQueue(headID, tailID))
	bus := &stubEventCollector{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// StrandedInProgressResetter intentionally nil — reproduces the
		// pre-hk-l2xd1 head-of-line starvation.
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 8*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Give the loop several dispatch ticks to spin on the stranded head item.
	<-testCtx.Done()

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel")
	}

	if n := ledger.claimCount(headID); n != 0 {
		t.Errorf("head bead ClaimBead count = %d; want 0 — stranded in_progress bead must never be "+
			"claimed directly, and (without the resetter) must never recover either (hk-r9edj)", n)
	}
}

// TestHKR9EDJ_SiblingStarvation_WithResetter: the hk-l2xd1 auto-reset resetter
// is wired → the stranded head bead is reset to open, its queue item becomes
// claimable again, and the tail sibling — no longer blocked head-of-line — is
// claimed.
func TestHKR9EDJ_SiblingStarvation_WithResetter(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	headID := core.BeadID("hk-r9edj-relief-head")
	tailID := core.BeadID("hk-r9edj-relief-tail")

	ledger := newHKR9EDJSiblingLedger(map[core.BeadID]core.CoarseStatus{
		headID: core.CoarseStatusInProgress, // stranded: no run anywhere
		tailID: core.CoarseStatusOpen,
	})
	resetter := newHKL2XD1FakeResetter()

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hkr9edjSiblingQueue(headID, tailID))
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
		StrandedResetDaemonNS:      1_700_000_000_000_000_002,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait for the reset to fire, then flip the ledger's view of the head
	// bead to open — simulating `br reset` landing — so the next dispatch
	// tick can claim it and, more importantly, so the tail sibling is no
	// longer stuck behind a permanently in_progress head.
	select {
	case <-resetter.resetSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ResetBead not called within 15s — stranded-bead auto-reset did not fire")
	}
	ledger.mu.Lock()
	ledger.status[headID] = core.CoarseStatusOpen
	ledger.mu.Unlock()

	// Poll for the tail sibling to be claimed — proof the head-of-line
	// blockage was relieved.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ledger.claimCount(tailID) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
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

	if n := ledger.claimCount(tailID); n == 0 {
		t.Errorf("tail sibling ClaimBead count = %d; want >0 — auto-reset must relieve sibling starvation (hk-r9edj)", n)
	}
}
