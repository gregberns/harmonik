package daemon_test

// workloop_bounded_retry_hk6pspu_test.go — Bounded-retry tests (hk-6pspu).
//
// Verifies that when ClaimBead permanently fails for a bead (non-blocked error),
// the workloop gives up after maxItemAttempts (3) dispatch attempts and marks the
// item failed. The failing bead must NOT starve the succeeding bead in the same
// wave group.
//
// The attempt counter (Item.Attempts) is incremented at Phase 3 (dispatch stamp)
// in the workloop. On claim failure, the item is reverted to pending but Attempts
// is NOT decremented. After MaxItemAttempts increments, Phase 3 short-circuits
// to evaluateGroupAdvanceWithOutcome(false) without calling ClaimBead.
//
// Spec ref: docs/design/workloop-bounded-retry.md; queue.MaxItemAttempts.
// Bead ref: hk-6pspu, hk-8ai2u.

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// permanentFailLedger is a stubBeadLedger where ClaimBead returns a permanent
// (non-blocked) error for a specific bead. ShowBead always returns CoarseStatusOpen
// so the workloop does not take the hk-n91y0 blocked-detection short-circuit.
//
// claimAttempts counts how many times ClaimBead was called for the failing bead
// (used to assert that the workloop does not call ClaimBead more than
// MaxItemAttempts-1 times before the Phase 3 guard fires).
type permanentFailLedger struct {
	mu            sync.Mutex
	failBead      core.BeadID
	claimAttempts int32 // atomic: total ClaimBead calls for failBead
	claimed       []core.BeadID
	closed        []core.BeadID
	reopened      []core.BeadID
}

func (p *permanentFailLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (p *permanentFailLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	// Always return Open so the workloop does NOT take the blocked-detection path.
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "test-bead"}, nil
}

func (p *permanentFailLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	if id == p.failBead {
		atomic.AddInt32(&p.claimAttempts, 1)
		return errors.New("brcli: InternalError (exit 99): unexpected failure")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claimed = append(p.claimed, id)
	return nil
}

func (p *permanentFailLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = append(p.closed, id)
	return nil
}

func (p *permanentFailLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reopened = append(p.reopened, id)
	return nil
}

func (p *permanentFailLedger) claimedIDs() []core.BeadID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]core.BeadID, len(p.claimed))
	copy(out, p.claimed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_ClaimRetryBoundedCount (hk-8ai2u)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_ClaimRetryBoundedCount verifies bounded retry: when ClaimBead
// permanently fails for beadA (non-blocked error), the workloop gives up after
// MaxItemAttempts (3) dispatch attempts and marks beadA failed. beadB (which
// succeeds) must still be claimed and dispatched. The wave group must reach
// complete-with-failures.
//
// The error returned by ClaimBead is NOT a "blocked" error — it is a generic
// internal error (exit 99). This ensures the workloop takes the transient-retry
// path (revert to pending + sleep) rather than the hk-n91y0 blocked-detection
// short-circuit.
//
// Expected flow for beadA (MaxItemAttempts = 3):
//   - Iteration 1: Phase 3 increments Attempts→1, stamps dispatched → ClaimBead fails → revert to pending
//   - Iteration 2: Phase 3 increments Attempts→2, stamps dispatched → ClaimBead fails → revert to pending
//   - Iteration 3: Phase 3 increments Attempts→3, Attempts >= MaxItemAttempts → maxAttemptsHit → item failed
//
// ClaimBead is therefore called exactly 2 times (not 3) for beadA: the 3rd
// attempt is caught at Phase 3 before the claim write.
//
// Bead ref: hk-8ai2u, hk-6pspu.
func TestWorkLoop_ClaimRetryBoundedCount(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-6pspu-fail-a")   // permanent claim failure
		beadB = core.BeadID("hk-6pspu-normal-b") // should succeed
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "6pspu-bounded-retry-test",
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

	ledger := &permanentFailLedger{failBead: beadA}
	bus := &stubEventCollector{}

	// Use CancelOnQueueExit: the mixed outcome (beadA failed, beadB failed via
	// no-commit guard) transitions the queue to paused-by-failure, which triggers
	// cancelOnQueueExit.
	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"}, // beadB handler succeeds (no commit → reopened)
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		MaxConcurrent:     2,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 60*time.Second)
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
	case <-time.After(55 * time.Second):
		t.Fatal("runWorkLoop did not exit — bounded retry may not be terminating the failing item")
	}

	// ── Assertions ──────────────────────────────────────────────────────────

	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ == nil {
		t.Fatal("queue is nil after workloop exit; expected paused-by-failure queue")
	}

	// 1. Queue must be paused-by-failure.
	if finalQ.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue.Status = %q; want %q", finalQ.Status, queue.QueueStatusPausedByFailure)
	}

	// 2. Group must be complete-with-failures.
	if finalQ.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group 0 status = %q; want %q", finalQ.Groups[0].Status, queue.GroupStatusCompleteWithFailures)
	}

	// 3. beadA's item must be failed with Attempts == MaxItemAttempts.
	itemA := finalQ.Groups[0].Items[0]
	if itemA.Status != queue.ItemStatusFailed {
		t.Errorf("beadA item status = %q; want %q", itemA.Status, queue.ItemStatusFailed)
	}
	if itemA.Attempts != queue.MaxItemAttempts {
		t.Errorf("beadA item Attempts = %d; want %d (MaxItemAttempts)", itemA.Attempts, queue.MaxItemAttempts)
	}
	if itemA.LastFailureReason != "max_attempts_exceeded" {
		t.Errorf("beadA LastFailureReason = %q; want %q", itemA.LastFailureReason, "max_attempts_exceeded")
	}

	// 4. ClaimBead was called at most MaxItemAttempts-1 times for beadA. The last
	//    attempt is caught at Phase 3 before ClaimBead is called.
	claimCalls := int(atomic.LoadInt32(&ledger.claimAttempts))
	if claimCalls > queue.MaxItemAttempts-1 {
		t.Errorf("ClaimBead called %d times for beadA; want <= %d (MaxItemAttempts-1)", claimCalls, queue.MaxItemAttempts-1)
	}

	// 5. beadB must have been claimed — it was dispatched despite beadA failing.
	claimed := ledger.claimedIDs()
	foundB := false
	for _, id := range claimed {
		if id == beadB {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("beadB was never claimed — starved by permanently failing beadA; claimed=%v", claimed)
	}
}
