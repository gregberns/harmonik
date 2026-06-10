package daemon_test

// workloop_qcancel_ppt32_test.go — queue-cancel drain on SIGINT/timeout (hk-ppt32).
//
// Symptom: when harmonik run is cancelled (ctx cancelled) while the queue is still
// active (no goroutines dispatched, or items still pending), the daemon exits leaving
// queue.json with status=active. The next harmonik run sees the active queue and
// refuses to start per QM-027. Operators had to manually remove .harmonik/queue.json.
//
// Fix: runWorkLoop calls drainCancelledQueue on every clean exit. When the queue is
// still active at ctx-cancel time, drainCancelledQueue transitions it to
// QueueStatusCancelled, persists it, then archives it as queue.json.cancelled-<ts>.
// The next harmonik run's Load() returns nil → QM-027 guard is not tripped.
//
// This test mocks SIGINT by cancelling the workloop context immediately after
// a queue is loaded but before any items are dispatched. It asserts:
//   (a) drainCancelledQueue archived queue.json (file absent on disk after loop exits).
//   (b) qs.Queue() is nil after exitClean (in-memory queue cleared).
//   (c) A second queue load after exit succeeds with no blocking active queue.
//
// Helper prefix: queueCancelFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ppt32).
//
// Spec ref: specs/queue-model.md §8 (shutdown drain).
// Bead ref: hk-ppt32.

import (
	"context"
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

// queueCancelFixturePendingQueue builds a minimal one-group wave queue with all
// items in pending status (no items dispatched).
func queueCancelFixturePendingQueue(t *testing.T, beadIDs ...core.BeadID) *queue.Queue {
	t.Helper()
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID: id,
			Status: queue.ItemStatusPending,
		}
	}
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "cancel-test-queue-" + t.Name(),
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
}

// queueCancelFixtureQueuePath returns the canonical queue.json path under projectDir.
func queueCancelFixtureQueuePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queue.json")
}

// queueCancelFixtureHasActiveQueue returns true when queue.json exists and
// contains status=active. Used to assert the file is absent / non-active after cancel.
func queueCancelFixtureHasActiveQueue(t *testing.T, projectDir string) bool {
	t.Helper()
	q, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Logf("queueCancelFixtureHasActiveQueue: Load error (treating as absent): %v", err)
		return false
	}
	if q == nil {
		return false
	}
	return q.Status == queue.QueueStatusActive
}

// ─────────────────────────────────────────────────────────────────────────────
// TestQueueCancel_TransitionsToCancelled
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueCancel_TransitionsToCancelled verifies that when the workloop context
// is cancelled while the queue is still active (SIGINT / operator timeout), the
// daemon drains the queue: queue.json is archived (absent from canonical path),
// the in-memory QueueStore is cleared, and a subsequent queue.Load returns nil so
// the next harmonik run can start cleanly without the QM-027 guard blocking it.
//
// Spec ref: specs/queue-model.md §8.
// Bead ref: hk-ppt32.
func TestQueueCancel_TransitionsToCancelled(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-ppt32-cancel-test-bead-001")

	// Persist the queue to disk so queue.json exists, mirroring the production
	// path where run.go calls queue.Persist before starting the daemon.
	q := queueCancelFixturePendingQueue(t, beadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist initial queue: %v", err)
	}

	// Verify queue.json exists and is active before the test begins.
	if !queueCancelFixtureHasActiveQueue(t, projectDir) {
		t.Fatal("precondition: expected active queue.json before workloop start")
	}

	// Wire a QueueStore pre-loaded with the queue.
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	// Use a stubBeadLedger that never returns ready beads — the workloop should
	// idle on the queue path without dispatching any items.
	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// Empty registry: /bin/sh exit-0 handler never delivers agent_ready, so
		// bypass the waitAgentReady gate (hk-ngw3d; hk-6hzci).
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// Cancel the context immediately — simulates SIGINT before any dispatch.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before workloop even enters the loop

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("workloop did not exit within 5s after immediate context cancel")
	}

	// (a) queue.json must be absent from the canonical path (archived by drainCancelledQueue).
	canonicalPath := queueCancelFixtureQueuePath(projectDir)
	if _, statErr := os.Stat(canonicalPath); statErr == nil {
		t.Errorf("queue.json still exists at canonical path after cancel; expected it to be archived")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("unexpected error checking queue.json: %v", statErr)
	}

	// Verify at least one .cancelled-* archive file was created.
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	entries, readDirErr := os.ReadDir(harmonikDir)
	if readDirErr != nil {
		t.Fatalf("ReadDir .harmonik: %v", readDirErr)
	}
	foundArchive := false
	for _, e := range entries {
		if len(e.Name()) > len("queue.json.cancelled-") &&
			e.Name()[:len("queue.json.cancelled-")] == "queue.json.cancelled-" {
			foundArchive = true
			break
		}
	}
	if !foundArchive {
		t.Error("no queue.json.cancelled-* archive file found after cancel")
	}

	// (b) In-memory QueueStore must be cleared.
	if got := qs.Queue(); got != nil {
		t.Errorf("QueueStore.Queue() = %+v; want nil after cancel", got)
	}

	// (c) A subsequent queue.Load returns nil so QM-027 is not tripped.
	reloaded, loadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Errorf("queue.Load after cancel: %v", loadErr)
	}
	if reloaded != nil {
		t.Errorf("queue.Load after cancel: got non-nil queue (status=%s); want nil", reloaded.Status)
	}
}

// TestQueueCancel_AlreadyTerminal_NoOp verifies that drainCancelledQueue is a
// no-op when the queue has already reached a terminal state (paused-by-failure)
// before ctx was cancelled — e.g. when evaluateGroupAdvanceWithOutcome fired
// in-flight. The canonical queue.json (paused-by-failure) must be untouched.
//
// Bead ref: hk-ppt32.
func TestQueueCancel_AlreadyTerminal_NoOp(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Build a queue already in paused-by-failure state.
	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "cancel-terminal-test-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteWithFailures,
				Items: []queue.Item{
					{BeadID: "hk-ppt32-terminal-bead", Status: queue.ItemStatusFailed},
				},
				CreatedAt: now,
			},
		},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist paused-by-failure queue: %v", err)
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("workloop did not exit within 5s")
	}

	// queue.json must still exist with paused-by-failure (not archived by drainCancelledQueue).
	reloaded, loadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("queue.Load: %v", loadErr)
	}
	if reloaded == nil {
		t.Fatal("queue.json unexpectedly absent; expected paused-by-failure queue to remain")
	}
	if reloaded.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue.Status = %q; want paused-by-failure", reloaded.Status)
	}
}

// TestQueueCancel_NamedQueue_ArchivedOnShutdown verifies the hk-u6m4l fix:
// drainCancelledQueue must drain ALL active queues, not just "main". Prior to
// the fix, named queues (e.g. "cp") survived daemon shutdown with status=active
// on disk, blocking future submits with queue_already_active (-32010).
//
// The test simulates a daemon shutdown with a named queue "cp" still active
// (no items dispatched). After the workloop exits it asserts:
//
//	(a) .harmonik/queues/cp.json is absent (archived by drainCancelledQueue).
//	(b) QueueStore.QueueByName("cp") is nil after exit.
//	(c) queue.Load for "cp" returns nil so QM-027 is not tripped on resubmit.
//
// Bead ref: hk-u6m4l.
func TestQueueCancel_NamedQueue_ArchivedOnShutdown(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const queueName = "cp"
	const beadID = core.BeadID("hk-u6m4l-named-queue-cancel-bead-001")

	// Build a named queue with status=active (one pending item, no dispatch).
	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "u6m4l-named-queue-" + t.Name(),
		Name:          queueName,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist named queue: %v", err)
	}

	// Precondition: named queue file is present and active.
	loaded, err := queue.Load(context.Background(), projectDir, queueName)
	if err != nil || loaded == nil || loaded.Status != queue.QueueStatusActive {
		t.Fatalf("precondition: expected active %q queue on disk; loaded=%v err=%v", queueName, loaded, err)
	}

	// Wire a QueueStore pre-loaded with the named queue.
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q) // SetQueue normalises to q.Name = "cp"

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           bus,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// Empty registry: /bin/sh exit-0 handler never delivers agent_ready, so
		// bypass the waitAgentReady gate (hk-ngw3d; hk-6hzci).
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// Cancel immediately — simulates SIGINT before any dispatch.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("workloop did not exit within 5s after immediate context cancel")
	}

	// (a) .harmonik/queues/cp.json must be absent (archived by drainCancelledQueue).
	reloaded, loadErr := queue.Load(context.Background(), projectDir, queueName)
	if loadErr != nil {
		t.Errorf("queue.Load(%q) after cancel: %v", queueName, loadErr)
	}
	if reloaded != nil {
		t.Errorf("queue.Load(%q) after cancel: got non-nil queue (status=%s); want nil — drainCancelledQueue did not archive named queue",
			queueName, reloaded.Status)
	}

	// (b) In-memory QueueStore slot for "cp" must be cleared.
	if got := qs.QueueByName(queueName); got != nil {
		t.Errorf("QueueStore.QueueByName(%q) = %+v; want nil after cancel", queueName, got)
	}
}
