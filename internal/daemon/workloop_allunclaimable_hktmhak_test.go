package daemon_test

// workloop_allunclaimable_hktmhak_test.go — All-unclaimable wave test (hk-tmhak).
//
// Verifies that when EVERY bead in a wave group permanently fails ClaimBead,
// the group reaches complete-with-failures terminal state without spinning.
// Each item is retried up to MaxItemAttempts (3) times, then marked failed.
// The queue transitions to paused-by-failure per QM-030 → QM-055.
//
// Spec ref: specs/queue-model.md §5.6 QM-036 (wave admission).
// Bead ref: hk-tmhak.

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

// allFailLedger is a stub BeadLedger where ClaimBead returns an error for ALL
// beads. ShowBead returns CoarseStatusOpen (so pre-claim guards pass).
// claimCalls tracks the total number of ClaimBead invocations as a spin guard.
type allFailLedger struct {
	mu         sync.Mutex
	claimCalls atomic.Int64
	closed     []core.BeadID
	reopened   []core.BeadID
}

func (a *allFailLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (a *allFailLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "all-fail-test"}, nil
}

func (a *allFailLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	a.claimCalls.Add(1)
	return errors.New("brcli: SchemaMismatch (exit 4): stderr=\"Error: Validation failed: claim: cannot claim issue\\n\"")
}

func (a *allFailLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = append(a.closed, id)
	return nil
}

func (a *allFailLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reopened = append(a.reopened, id)
	return nil
}

func (a *allFailLedger) totalClaimCalls() int64 {
	return a.claimCalls.Load()
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_AllWaveItemsUnclaimable (hk-tmhak)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_AllWaveItemsUnclaimable verifies that when every bead in a wave
// group permanently fails ClaimBead, the group reaches complete-with-failures
// terminal state without spinning. Each bead is retried up to MaxItemAttempts
// (3) times, all 3 items are marked failed, the queue is paused-by-failure,
// and total ClaimBead calls are bounded (≤ 3 * MaxItemAttempts = 9).
//
// Bead ref: hk-tmhak.
func TestWorkLoop_AllWaveItemsUnclaimable(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-tmhak-a")
		beadB = core.BeadID("hk-tmhak-b")
		beadC = core.BeadID("hk-tmhak-c")
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "tmhak-all-fail-test",
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
					{BeadID: beadC, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &allFailLedger{}
	bus := &stubEventCollector{}

	// Use CancelOnQueueExit: paused-by-failure is a terminal queue state that
	// triggers this cancel, allowing the work loop to exit cleanly.
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

	// The work loop must exit within a reasonable time — no infinite spin.
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit — all-unclaimable wave caused infinite spin (hk-tmhak)")
	}

	// ── Assert queue terminal state ──────────────────────────────────────────

	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ == nil {
		t.Fatal("queue is nil after work loop exit; expected paused-by-failure")
	}

	if finalQ.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue.Status = %q; want %q", finalQ.Status, queue.QueueStatusPausedByFailure)
	}

	if len(finalQ.Groups) == 0 {
		t.Fatal("queue has no groups")
	}
	g := finalQ.Groups[0]
	if g.Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("group 0 status = %q; want %q", g.Status, queue.GroupStatusCompleteWithFailures)
	}

	// ── Assert all items are failed ──────────────────────────────────────────

	for i, item := range g.Items {
		if item.Status != queue.ItemStatusFailed {
			t.Errorf("item[%d] (bead %s) status = %q; want %q",
				i, item.BeadID, item.Status, queue.ItemStatusFailed)
		}
	}

	// ── Assert bounded ClaimBead calls (no spin) ─────────────────────────────

	// With 3 beads and MaxItemAttempts=3, total claim calls must be ≤ 9.
	maxExpectedClaims := int64(3 * queue.MaxItemAttempts)
	totalClaims := ledger.totalClaimCalls()
	if totalClaims > maxExpectedClaims {
		t.Errorf("totalClaimCalls = %d; want <= %d (spin detected)", totalClaims, maxExpectedClaims)
	}
	if totalClaims == 0 {
		t.Error("totalClaimCalls = 0; expected at least one claim attempt per bead")
	}

	t.Logf("totalClaimCalls=%d (max allowed=%d)", totalClaims, maxExpectedClaims)
}
