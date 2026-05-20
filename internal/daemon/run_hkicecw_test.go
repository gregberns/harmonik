package daemon_test

// run_hkicecw_test.go — tests for harmonik run <bead-id> behaviour (hk-icecw).
//
// Coverage:
//   - Exit-on-empty: a queue of one item drains and cancelOnQueueDrain fires
//     after CompleteAndUnlink + ClearQueue, causing the work loop to exit
//     without real Claude.
//   - cancelOnQueueDrain is NOT called when the queue fails (paused-by-failure).
//
// Helper prefix: runBeadFixture (derived from hk-icecw concept per
// implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-icecw.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// runBeadFixtureSingleItemQueue builds a one-group, one-item active wave queue
// targeting beadID — the canonical shape produced by harmonik run.
func runBeadFixtureSingleItemQueue(t *testing.T, beadID core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "run-fixture-queue-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}
}

// runBeadFixtureDeps builds workLoopDeps with a QueueStore pre-loaded with q
// and a cancelOnQueueDrain cancel func.  The handler binary exits 0 so the bead
// is closed (success path).
func runBeadFixtureDeps(
	t *testing.T,
	projectDir string,
	bus *stubEventCollector,
	q *queue.Queue,
	cancelOnDrain context.CancelFunc,
) daemon.WorkLoopDepsParams {
	t.Helper()
	qs := daemon.ExportedNewQueueStore()
	if q != nil {
		qs.SetQueue(q)
	}
	ledger := &stubBeadLedger{}
	return daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		CancelOnQueueDrain: cancelOnDrain,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunBead_ExitOnEmpty
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBead_ExitOnEmpty verifies that when cancelOnQueueDrain is set and the
// single-item queue completes successfully, the cancel is invoked and the work
// loop exits without ctx cancellation from outside.
//
// This is the core exit-on-empty guarantee for harmonik run <bead-id>.
//
// Bead ref: hk-icecw.
func TestRunBead_ExitOnEmpty(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hkicecw-exit-empty-001")
	q := runBeadFixtureSingleItemQueue(t, beadID)

	bus := &stubEventCollector{}

	// drainCtx is the context that cancelOnQueueDrain will cancel.
	// In production, this is the daemon's runCtx; here it drives the work loop.
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	p := runBeadFixtureDeps(t, projectDir, bus, q, cancelDrain)
	deps := daemon.ExportedWorkLoopDeps(p)

	// Wrap drainCtx with a test-level timeout so the test does not hang.
	testCtx, testCancel := context.WithTimeout(drainCtx, 20*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// The loop should exit on its own once the cancel fires — no external cancel.
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runWorkLoop did not exit after queue drained (cancelOnQueueDrain not invoked)")
	}

	// The bead must have been closed.
	ledger := p.BrAdapter.(*stubBeadLedger)
	closed := ledger.closedIDs()
	if len(closed) == 0 {
		t.Fatal("expected bead to be closed; none were")
	}
	if closed[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closed[0], beadID)
	}

	// The QueueStore must be nil after completion.
	qs := daemon.ExportedQueueStoreOf(deps)
	if qs.Queue() != nil {
		t.Error("QueueStore.Queue() is non-nil after drain; expected ClearQueue to have been called")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunBead_CancelNotCalledOnFailure
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBead_CancelNotCalledOnFailure verifies that when the bead fails (handler
// exits non-zero), cancelOnQueueDrain is NOT called — the queue is paused-by-failure,
// not completed, so exit-on-empty does not fire.
//
// Bead ref: hk-icecw.
func TestRunBead_CancelNotCalledOnFailure(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hkicecw-no-drain-on-fail-001")
	q := runBeadFixtureSingleItemQueue(t, beadID)

	bus := &stubEventCollector{}

	drainCalled := make(chan struct{}, 1)
	cancelOnDrain := func() { drainCalled <- struct{}{} }

	// Wire cancelOnDrain into params as a raw func — needs context.CancelFunc type.
	// We use the same WorkLoopDepsParams.CancelOnQueueDrain which is context.CancelFunc.
	// context.CancelFunc is func(); our closure matches.
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}
	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 1"}, // handler fails
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelOnDrain,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the bead to be reopened (failure path) — or closed if there's a bug.
	// The loop must NOT exit via cancelOnDrain on failure; it should idle-wait.
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()

	// Poll until the bead is either reopened or closed.
	for {
		select {
		case <-drainCalled:
			t.Error("cancelOnQueueDrain was called on failure — should not fire when queue is paused-by-failure")
			cancel()
			return
		case <-deadline.C:
			// Timer expired without drain being called — that is the expected outcome.
			// Also verify the queue is in paused-by-failure state.
			liveQ := qs.Queue()
			if liveQ != nil && liveQ.Status != queue.QueueStatusPausedByFailure {
				t.Errorf("queue status = %q; want %q", liveQ.Status, queue.QueueStatusPausedByFailure)
			}
			cancel()
			<-loopDone
			return
		case <-time.After(100 * time.Millisecond):
			// Check if the bead was reopened yet; if so, we can verify sooner.
			if len(ledger.reopenedIDs()) > 0 {
				// Bead was reopened (failure path). Give the queue a moment to
				// settle into paused-by-failure, then check drain was NOT called.
				select {
				case <-drainCalled:
					t.Error("cancelOnQueueDrain was called on failure — should not fire")
					cancel()
					return
				case <-time.After(200 * time.Millisecond):
					// Good — drain not called within 200ms of failure path.
					liveQ := qs.Queue()
					if liveQ != nil && liveQ.Status != queue.QueueStatusPausedByFailure {
						t.Errorf("queue status = %q; want %q", liveQ.Status, queue.QueueStatusPausedByFailure)
					}
					cancel()
					<-loopDone
					return
				}
			}
		}
	}
}
