package daemon_test

// workloop_50item_wave_hkoylis_test.go — 50-item wave with stuck head (hk-oylis).
//
// Property test: in a 50-item wave group, if item 0 permanently fails ClaimBead,
// all other 49 items still get dispatched and complete. A stuck head must not
// block siblings in a wave (unlike a stream, which is HOL-blocked).
//
// Spec ref: specs/queue-model.md §5.6 QM-036 (wave admission).
// Bead ref: hk-oylis.

import (
	"context"
	"errors"
	"fmt"
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

// stuckHeadLedger is a stub BeadLedger where ClaimBead returns an error for a
// single "stuck" bead (bead-0) and succeeds for all others. ShowBead returns
// CoarseStatusOpen for all beads so pre-claim guards pass.
//
// Helper prefix: stuckHead (per implementer-protocol.md §Helper-prefix; bead hk-oylis).
type stuckHeadLedger struct {
	mu              sync.Mutex
	stuckBead       core.BeadID
	stuckClaimCalls atomic.Int64
	claimed         map[core.BeadID]struct{} // beads where ClaimBead succeeded
	closed          []core.BeadID
	reopened        []core.BeadID
}

func newStuckHeadLedger(stuckBead core.BeadID) *stuckHeadLedger {
	return &stuckHeadLedger{
		stuckBead: stuckBead,
		claimed:   make(map[core.BeadID]struct{}),
	}
}

func (s *stuckHeadLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (s *stuckHeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "50-item-wave-test"}, nil
}

func (s *stuckHeadLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	if id == s.stuckBead {
		s.stuckClaimCalls.Add(1)
		return errors.New("brcli: SchemaMismatch (exit 4): stderr=\"Error: Validation failed: claim: cannot claim issue\\n\"")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed[id] = struct{}{}
	return nil
}

func (s *stuckHeadLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, id)
	return nil
}

func (s *stuckHeadLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reopened = append(s.reopened, id)
	return nil
}

func (s *stuckHeadLedger) claimedSet() map[core.BeadID]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[core.BeadID]struct{}, len(s.claimed))
	for k, v := range s.claimed {
		out[k] = v
	}
	return out
}

func (s *stuckHeadLedger) totalStuckClaimCalls() int64 {
	return s.stuckClaimCalls.Load()
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_FiftyItemWaveStuckHead (hk-oylis)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_FiftyItemWaveStuckHead verifies that in a 50-item wave group,
// if item 0 permanently fails ClaimBead, all other 49 items still get dispatched.
// A stuck head must not block siblings in a wave.
//
// bead-0: ClaimBead always fails → retried up to MaxItemAttempts (3), then Failed.
// beads 1-49: ClaimBead succeeds → dispatched → handler exit 0 with no commit →
//
//	no-commit guard fires → item marked Failed (expected; the property under
//	test is dispatch/claim, not handler success).
//
// The group reaches complete-with-failures (all 50 failed). The queue is
// paused-by-failure. CancelOnQueueExit fires and the work loop exits.
//
// Key assertions:
//   - All 49 non-stuck beads were claimed (not blocked by bead-0).
//   - bead-0 was never successfully claimed.
//   - ClaimBead calls for bead-0 ≤ MaxItemAttempts (3).
//   - Group is complete-with-failures.
//   - Queue is paused-by-failure.
//   - Test completes within the timeout.
//
// Bead ref: hk-oylis.
func TestWorkLoop_FiftyItemWaveStuckHead(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const itemCount = 50

	// Build bead IDs: bead-0 is the stuck head, beads 1-49 are normal.
	beadIDs := make([]core.BeadID, itemCount)
	for i := range itemCount {
		beadIDs[i] = core.BeadID(fmt.Sprintf("hk-oylis-%02d", i))
	}
	stuckBeadID := beadIDs[0]

	// Build queue with a single wave group containing all 50 items.
	now := time.Now()
	items := make([]queue.Item, itemCount)
	for i, id := range beadIDs {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "oylis-50item-wave-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := newStuckHeadLedger(stuckBeadID)
	bus := &stubEventCollector{}

	// CancelOnQueueExit: paused-by-failure is a terminal queue state that
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
		MaxConcurrent:     50, // all wave items can dispatch concurrently
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// 90s budget: 50 items with concurrent worktree creation + handler + cleanup.
	// Each round is ~5-10s, and bead-0 retries up to 3 times. Generous margin.
	testCtx, testCancel := context.WithTimeout(exitCtx, 90*time.Second)
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
	case <-time.After(85 * time.Second):
		t.Fatal("runWorkLoop did not exit — 50-item wave with stuck head caused infinite spin or excessive delay (hk-oylis)")
	}

	// ── Assert all 49 non-stuck beads were claimed ──────────────────────────

	claimed := ledger.claimedSet()
	for i := 1; i < itemCount; i++ {
		if _, ok := claimed[beadIDs[i]]; !ok {
			t.Errorf("bead %s (index %d) was never claimed — blocked by stuck head bead-0 (hk-oylis regression)",
				beadIDs[i], i)
		}
	}
	if _, ok := claimed[stuckBeadID]; ok {
		t.Errorf("stuck bead %s was claimed despite ClaimBead always failing", stuckBeadID)
	}

	// ── Assert bounded ClaimBead calls for the stuck bead ───────────────────

	stuckClaims := ledger.totalStuckClaimCalls()
	if stuckClaims > int64(queue.MaxItemAttempts) {
		t.Errorf("stuck bead ClaimBead calls = %d; want <= %d (spin detected)",
			stuckClaims, queue.MaxItemAttempts)
	}
	if stuckClaims == 0 {
		t.Error("stuck bead ClaimBead calls = 0; expected at least one attempt")
	}

	// ── Assert queue terminal state ─────────────────────────────────────────

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

	// ── Assert item-level terminal states ───────────────────────────────────

	for i, item := range g.Items {
		if item.Status != queue.ItemStatusFailed {
			t.Errorf("item[%d] (bead %s) status = %q; want %q",
				i, item.BeadID, item.Status, queue.ItemStatusFailed)
		}
	}

	// bead-0 specifically: Attempts should be exactly MaxItemAttempts (3).
	item0 := g.Items[0]
	if item0.Attempts < queue.MaxItemAttempts {
		t.Errorf("bead-0 Attempts = %d; want >= %d", item0.Attempts, queue.MaxItemAttempts)
	}

	t.Logf("50-item wave: stuck_bead_claims=%d, non_stuck_claimed=%d/49, group_status=%s",
		stuckClaims, len(claimed), g.Status)
}
