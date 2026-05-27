package daemon_test

// workloop_hkn91y0_test.go — Wave-mode HOL-blocking fix tests (hk-n91y0).
//
// Verifies that when ClaimBead fails because a bead is "blocked" in the Beads
// ledger, the workloop marks the queue item failed and allows other pending
// items in the wave to be dispatched (no live-lock / starvation).
//
// Without the fix, the workloop would revert the item to ItemStatusPending and
// immediately retry it on every poll cycle, starving other pending items.
//
// Spec ref: specs/queue-model.md §5.6 QM-036 (wave admission).
// Bead ref: hk-n91y0.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// blockedBeadLedger is a stubBeadLedger variant where ClaimBead returns an error
// for a specific bead and ShowBead returns CoarseStatusBlocked for that bead.
// All other beads behave normally.  claimedIDs records every successful claim.
type blockedBeadLedger struct {
	mu               sync.Mutex
	blockedBead      core.BeadID
	showBlockedAsOpen bool // when true, ShowBead returns Open status for the blocked bead
	claimed          []core.BeadID // beads where ClaimBead was called and succeeded
	closed           []core.BeadID
	reopened         []core.BeadID
}

func (b *blockedBeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch, no br-ready beads
}

func (b *blockedBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == b.blockedBead && b.showBlockedAsOpen {
		return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "blocked-by-deps"}, nil
	}
	if id == b.blockedBead {
		return core.BeadRecord{BeadID: id, Status: core.CoarseStatusBlocked}, nil
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (b *blockedBeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	if id == b.blockedBead {
		return errors.New("brcli: SchemaMismatch (exit 4): stderr=\"Error: Validation failed: claim: cannot claim blocked issue: hk-dep1, hk-dep2\\n\"")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claimed = append(b.claimed, id)
	return nil
}

func (b *blockedBeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = append(b.closed, id)
	return nil
}

func (b *blockedBeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reopened = append(b.reopened, id)
	return nil
}

func (b *blockedBeadLedger) claimedIDs() []core.BeadID {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.BeadID, len(b.claimed))
	copy(out, b.claimed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWaveBlockedBead_DoesNotStarveOtherItems (hk-n91y0)
// ─────────────────────────────────────────────────────────────────────────────

// TestWaveBlockedBead_DoesNotStarveOtherItems verifies that when beadA is
// blocked in the Beads ledger (ClaimBead returns an error, ShowBead returns
// CoarseStatusBlocked), the workloop does NOT retry beadA in a live-lock but
// instead marks it failed and dispatches beadB normally.
//
// The queue must end up paused-by-failure (beadA failed, beadB succeeded) and
// the CancelOnQueueDrain signal must fire so the workloop exits.
//
// Bead ref: hk-n91y0.
func TestWaveBlockedBead_DoesNotStarveOtherItems(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-n91y0-blocked-a") // will have claim rejected (blocked)
		beadB = core.BeadID("hk-n91y0-normal-b")  // should be dispatched normally
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "n91y0-test-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadA, Status: queue.ItemStatusPending}, // blocked
					{BeadID: beadB, Status: queue.ItemStatusPending}, // normal
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &blockedBeadLedger{blockedBead: beadA}
	bus := &stubEventCollector{}
	// Use CancelOnQueueExit, not CancelOnQueueDrain: the mixed outcome (beadA
	// failed, beadB succeeded) transitions the queue to paused-by-failure, which
	// triggers cancelOnQueueExit.  CancelOnQueueDrain only fires on full success.
	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"}, // beadB succeeds
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		MaxConcurrent:     2,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit after blocked-bead wave queue reached terminal state — hk-n91y0 live-lock may still be present")
	}

	// beadB must have been claimed — it was dispatched despite beadA being blocked.
	//
	// Note: the handler ("/bin/sh -c exit 0") makes no git commit, so the
	// no-commit guard (hk-mmh8f) causes ReopenBead to be called instead of
	// CloseBead.  We therefore check claimedIDs (successful ClaimBead calls)
	// rather than closedIDs, since claiming is the observable proof that beadB
	// was NOT starved by the blocked beadA.
	claimed := ledger.claimedIDs()
	foundB := false
	foundA := false
	for _, id := range claimed {
		switch id {
		case beadB:
			foundB = true
		case beadA:
			foundA = true
		}
	}
	if !foundB {
		t.Errorf("beadB was never claimed — it was starved by the blocked beadA (hk-n91y0 regression); claimed=%v", claimed)
	}
	if foundA {
		// beadA's ClaimBead always returns an error — it should never appear in claimed.
		t.Errorf("beadA was claimed despite ClaimBead returning an error; claimed=%v", claimed)
	}

	// Queue must be paused-by-failure: beadA's blocked claim was marked failed,
	// and beadB also failed (no-commit guard).  CancelOnQueueExit fires when the
	// queue transitions to paused-by-failure, which is what triggered loopDone.
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ != nil {
		if finalQ.Status != queue.QueueStatusPausedByFailure {
			t.Errorf("queue.Status = %q; want paused-by-failure (blocked-claim item should be marked failed)", finalQ.Status)
		}
		if finalQ.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
			t.Errorf("group 0 status = %q; want complete-with-failures", finalQ.Groups[0].Status)
		}
	}
}

// TestWaveBlockedByDeps_OpenStatusButClaimRejected verifies that when a bead
// has status=open but br claim rejects it because its dependencies are open,
// the workloop detects the "cannot claim blocked issue" error message and
// marks the item failed instead of retrying forever.
//
// This is the real-world scenario: br show returns "OPEN" but br claim fails
// with exit 4 "cannot claim blocked issue: hk-dep1, hk-dep2".
func TestWaveBlockedByDeps_OpenStatusButClaimRejected(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-n91y0-open-blocked-a")
		beadB = core.BeadID("hk-n91y0-open-normal-b")
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "n91y0-open-blocked-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadA, Status: queue.ItemStatusPending},
					{BeadID: beadB, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &blockedBeadLedger{blockedBead: beadA, showBlockedAsOpen: true}
	bus := &stubEventCollector{}
	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		MaxConcurrent:     2,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit — open-but-blocked bead caused live-lock (error-message detection not working)")
	}

	claimed := ledger.claimedIDs()
	foundB := false
	for _, id := range claimed {
		if id == beadB {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("beadB was never claimed — starved by open-but-blocked beadA; claimed=%v", claimed)
	}
}
