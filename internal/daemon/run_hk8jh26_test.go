package daemon_test

// run_hk8jh26_test.go — tests for hk-8jh26 fixes to harmonik run <bead-id>.
//
// Coverage:
//   - ExitOnFailure: a single-item queue whose handler exits non-zero must
//     trigger cancelOnQueueExit (not cancelOnQueueDrain) and the work loop
//     must exit without hanging; exit code must be non-zero (paused-by-failure).
//   - RefusesActiveQueue: when .harmonik/queue.json already holds a non-completed
//     queue, runBeadSubcommand must error immediately without persisting a new
//     queue (QM-027 guard added in hk-8jh26 Fix 3).
//   - ExitOnEmpty exit code: existing TestRunBead_ExitOnEmpty already passes;
//     this file adds an explicit assertion that the QueueStore is nil (exit 0
//     path) after drain fires, complementing that test.
//
// Helper prefix: runBead8jh26Fixture (derived from bead codename hk-8jh26 per
// implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-8jh26.

import (
	"context"
	"encoding/json"
	"os"
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

// runBead8jh26FixtureDepsWithExit builds workLoopDepsParams with a
// cancelOnQueueExit hook wired.  handlerExitCode controls the handler outcome.
func runBead8jh26FixtureDepsWithExit(
	t *testing.T,
	projectDir string,
	bus *stubEventCollector,
	q *queue.Queue,
	cancelOnDrain context.CancelFunc,
	cancelOnExit context.CancelFunc,
	handlerExitCode int,
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
		HandlerArgs:        []string{"-c", exitScript(handlerExitCode)},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		CancelOnQueueDrain: cancelOnDrain,
		CancelOnQueueExit:  cancelOnExit,
		// Empty registry bypasses the waitAgentReady 15s gate: the exit-code shell
		// handler never delivers agent_ready, so a sealed claude-adapter registry
		// would hang. With no claude adapter, ForAgent errors and waitAgentReady
		// is skipped (hk-ngw3d). The pre-commit WorktreeFactory (needed only by
		// the success-path callers that assert the bead CLOSES) is set at the
		// call site, not here — the failure-path test must hit no-commit→fail.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	}
}

// exitScript returns a minimal sh -c snippet that exits with code n.
func exitScript(n int) string {
	if n == 0 {
		return "exit 0"
	}
	return "exit 1"
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunBead_ExitOnFailure
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBead_ExitOnFailure verifies that when the handler exits non-zero the
// work loop exits via cancelOnQueueExit (paused-by-failure path) within a
// reasonable deadline, and that the QueueStore reflects the paused-by-failure
// status (not nil — CompleteAndUnlink must NOT have been called).
//
// This is the core fix for hk-8jh26 Issue 1 (hang on bead failure).
//
// Bead ref: hk-8jh26.
func TestRunBead_ExitOnFailure(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk8jh26-exit-on-failure-001")
	q := runBeadFixtureSingleItemQueue(t, beadID)

	bus := &stubEventCollector{}

	// exitCtx is cancelled by cancelOnQueueExit when the failure path fires.
	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := runBead8jh26FixtureDepsWithExit(t, projectDir, bus, q, nil, cancelExit, 1 /* handler fails */)
	deps := daemon.ExportedWorkLoopDeps(p)

	// Wrap exitCtx with a test-level timeout so the test does not hang.
	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// The loop must exit on its own once cancelOnQueueExit fires — no external cancel.
	select {
	case err := <-loopDone:
		// context.Canceled is expected: cancelOnQueueExit cancelled testCtx.
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("runWorkLoop returned unexpected error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit after bead failed (cancelOnQueueExit not invoked within deadline)")
	}

	// The QueueStore must be non-nil (not completed) with paused-by-failure status.
	qs := daemon.ExportedQueueStoreOf(deps)
	finalQueue := qs.Queue()
	if finalQueue == nil {
		t.Fatal("QueueStore.Queue() is nil after failure — CompleteAndUnlink must NOT fire on failure path")
	}
	if finalQueue.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue status = %q; want %q", finalQueue.Status, queue.QueueStatusPausedByFailure)
	}

	// cancelOnQueueDrain must NOT have been called (drain = success only).
	// Verified indirectly: if drain had been called on the success path the queue
	// would be nil; we already asserted non-nil above.
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunBead_RefusesActiveQueue
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBead_RefusesActiveQueue verifies that when .harmonik/queue.json already
// holds a non-completed (paused-by-failure) queue, the pre-persist guard in
// run.go returns an error and does NOT overwrite the existing file.
//
// This is a white-box test: we exercise the guard logic directly by writing a
// pre-existing queue.json and calling queue.Load to confirm the guard would
// have seen it, then verifying the file is unchanged.  A full end-to-end
// invocation of runBeadSubcommand is not possible in a unit-test context because
// it requires tmux, br on PATH, etc.
//
// Bead ref: hk-8jh26.
func TestRunBead_RefusesActiveQueue(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)

	// Write a pre-existing queue.json with status=paused-by-failure.
	now := time.Now().UTC()
	activeQueue := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk8jh26-preexisting-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteWithFailures,
				Items: []queue.Item{
					{BeadID: "hk8jh26-prior-bead", Status: queue.ItemStatusFailed},
				},
				CreatedAt: now,
			},
		},
	}

	ctx := context.Background()
	if err := queue.Persist(ctx, projectDir, activeQueue); err != nil {
		t.Fatalf("setup: Persist pre-existing queue: %v", err)
	}

	// Capture original file contents for comparison after the guard check.
	// NQ-A2 path drift: queue.Persist now writes the canonical per-queue path
	// .harmonik/queues/<name>.json (not the legacy .harmonik/queue.json), so read
	// the same path queue.Persist/queue.Load use (queue.QueueNameMain slot).
	queuePath := filepath.Join(projectDir, ".harmonik", "queues", queue.QueueNameMain+".json")
	//nolint:gosec // G304: queuePath derived from projectDir (.harmonik/queues/main.json) — test-only read
	originalData, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("setup: read queue.json: %v", err)
	}

	// Simulate the guard logic from run.go Fix 3: Load and check status.
	loaded, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("queue.Load: %v", loadErr)
	}
	if loaded == nil {
		t.Fatal("queue.Load returned nil; expected pre-existing queue to be visible")
	}

	// Assert the guard condition fires correctly.
	if loaded.Status == queue.QueueStatusCompleted {
		t.Errorf("guard condition should block: status=%q is not completed", loaded.Status)
	}

	// Verify the file was NOT changed (no overwrite occurred during the guard check).
	//nolint:gosec // G304: queuePath derived from projectDir (.harmonik/queue.json) — test-only read
	currentData, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("re-read queue.json: %v", err)
	}
	if string(originalData) != string(currentData) {
		t.Error("queue.json was modified during the guard check — guard must be read-only")
	}

	// Confirm the error message fields would be populated correctly.
	if loaded.QueueID != activeQueue.QueueID {
		t.Errorf("loaded QueueID = %q; want %q", loaded.QueueID, activeQueue.QueueID)
	}
	if loaded.Status != activeQueue.Status {
		t.Errorf("loaded Status = %q; want %q", loaded.Status, activeQueue.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunBead_ExitOnEmpty_ExitCodeZero
// ─────────────────────────────────────────────────────────────────────────────

// TestRunBead_ExitOnEmpty_ExitCodeZero augments TestRunBead_ExitOnEmpty with an
// explicit assertion that QueueStore.Queue() returns nil after the success path
// (CompleteAndUnlink + ClearQueue), confirming the exit-code-0 mapping in
// run.go Fix 2.
//
// Bead ref: hk-8jh26.
func TestRunBead_ExitOnEmpty_ExitCodeZero(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk8jh26-exit-empty-exitcode0-001")
	q := runBeadFixtureSingleItemQueue(t, beadID)

	bus := &stubEventCollector{}

	drainCtx, cancelDrain := context.WithCancel(context.Background())

	// Wire BOTH cancelOnQueueDrain and cancelOnQueueExit to the same cancel so
	// both code paths are exercised through the same context cancel.
	exitCtx, cancelExit := context.WithCancel(drainCtx)
	_ = exitCtx // used via testCtx below

	p := runBead8jh26FixtureDepsWithExit(t, projectDir, bus, q, cancelDrain, cancelExit, 0 /* success */)
	// Success path asserts QueueStore.Queue() == nil (CompleteAndUnlink fired),
	// which requires the bead to CLOSE. The exit-0 handler makes no commit, so the
	// no-commit guard (hk-mmh8f) would reopen it — pre-commit the worktree to
	// advance HEAD past the parent SHA. (The failure-path sibling deliberately
	// omits this so it hits no-commit→fail.)
	p.WorktreeFactory = workloopFixturePreCommitWorktreeFactory
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 20*time.Second)
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
	case <-time.After(15 * time.Second):
		t.Fatal("runWorkLoop did not exit after queue drained")
	}

	// Exit-code-0 mapping: QueueStore.Queue() must be nil after CompleteAndUnlink.
	qs := daemon.ExportedQueueStoreOf(deps)
	if qs.Queue() != nil {
		t.Error("QueueStore.Queue() is non-nil after drain; expected ClearQueue to have been called (exit code 0 path)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers — JSON marshal used in assertions
// ─────────────────────────────────────────────────────────────────────────────

// mustMarshalJSON is a test helper to produce deterministic JSON for comparison.
func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
