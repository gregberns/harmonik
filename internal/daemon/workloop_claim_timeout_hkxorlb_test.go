package daemon_test

// workloop_claim_timeout_hkxorlb_test.go — ClaimBead timeout test (hk-xorlb).
//
// Verifies that when ClaimBead returns context.DeadlineExceeded for a bead
// (simulating a br timeout), the workloop treats it as a transient error and
// retries bounded by maxItemAttempts (3). The failing bead must NOT spin
// forever — it must reach ItemStatusFailed after MaxItemAttempts.
//
// Spec ref: docs/design/workloop-bounded-retry.md; queue.MaxItemAttempts.
// Bead ref: hk-xorlb.

import (
	"context"
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

// claimTimeoutLedger is a stub BeadLedger where ClaimBead returns
// context.DeadlineExceeded for a specific bead. All other beads claim
// normally. ShowBead returns CoarseStatusOpen for all beads.
type claimTimeoutLedger struct {
	mu            sync.Mutex
	timeoutBead   core.BeadID
	claimAttempts int32 // atomic: ClaimBead calls for timeoutBead
	claimed       []core.BeadID
	closed        []core.BeadID
	reopened      []core.BeadID
}

func (c *claimTimeoutLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (c *claimTimeoutLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "timeout-test"}, nil
}

func (c *claimTimeoutLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	if id == c.timeoutBead {
		atomic.AddInt32(&c.claimAttempts, 1)
		return context.DeadlineExceeded
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claimed = append(c.claimed, id)
	return nil
}

func (c *claimTimeoutLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, id)
	return nil
}

func (c *claimTimeoutLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reopened = append(c.reopened, id)
	return nil
}

func (c *claimTimeoutLedger) claimedIDs() []core.BeadID {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]core.BeadID, len(c.claimed))
	copy(out, c.claimed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_ClaimBeadTimeout (hk-xorlb)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_ClaimBeadTimeout verifies that when ClaimBead returns
// context.DeadlineExceeded for beadA, the workloop retries it bounded by
// MaxItemAttempts (3) and then marks the item failed. beadB (which claims
// normally) must still be dispatched — the timeout on beadA must not starve
// other items in the wave.
//
// Expected flow for beadA (MaxItemAttempts = 3):
//   - Iteration 1: Phase 3 increments Attempts→1, stamps dispatched → ClaimBead returns DeadlineExceeded → revert to pending
//   - Iteration 2: Phase 3 increments Attempts→2, stamps dispatched → ClaimBead returns DeadlineExceeded → revert to pending
//   - Iteration 3: Phase 3 increments Attempts→3, Attempts >= MaxItemAttempts → maxAttemptsHit → item failed
//
// ClaimBead is called exactly 2 times (not 3): the 3rd attempt is caught at
// Phase 3 before the claim write.
//
// Bead ref: hk-xorlb.
func TestWorkLoop_ClaimBeadTimeout(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-xorlb-timeout-a") // ClaimBead returns DeadlineExceeded
		beadB = core.BeadID("hk-xorlb-normal-b")  // should succeed
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "xorlb-claim-timeout-test",
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

	ledger := &claimTimeoutLedger{timeoutBead: beadA}
	bus := &stubEventCollector{}

	// Use CancelOnQueueExit: mixed outcome (beadA failed, beadB no-commit)
	// transitions the queue to paused-by-failure, which fires cancelOnQueueExit.
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
		t.Fatal("runWorkLoop did not exit — ClaimBead DeadlineExceeded may be causing infinite retry (hk-xorlb)")
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

	// 4. ClaimBead was called at most MaxItemAttempts-1 times for beadA.
	claimCalls := int(atomic.LoadInt32(&ledger.claimAttempts))
	if claimCalls > queue.MaxItemAttempts-1 {
		t.Errorf("ClaimBead called %d times for beadA; want <= %d (MaxItemAttempts-1)", claimCalls, queue.MaxItemAttempts-1)
	}

	// 5. beadB must have been claimed — it was dispatched despite beadA timing out.
	claimed := ledger.claimedIDs()
	foundB := false
	for _, id := range claimed {
		if id == beadB {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("beadB was never claimed — starved by timing-out beadA; claimed=%v", claimed)
	}
}
