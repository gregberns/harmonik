package daemon_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

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
		QueueID:       newTestQueueID(),
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

// TestQueueShutdown_PersistsRestartDrain verifies that a cancelled work-loop
// context preserves the queue and its automatic restart intent.
//
// Spec ref: specs/queue-model.md §8.
// Bead ref: hk-ppt32.
func TestQueueShutdown_PersistsRestartDrain(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-ppt32-cancel-test-bead-001")

	q := queueCancelFixturePendingQueue(t, beadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist initial queue: %v", err)
	}

	if !queueCancelFixtureHasActiveQueue(t, projectDir) {
		t.Fatal("precondition: expected active queue.json before workloop start")
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.TestRuntimeParams{
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
	deps := daemon.ExportedTestRuntime(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before workloop even enters the loop

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	awaitLoopTeardown(t, loopDone, "work loop")

	reloaded, loadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("queue.Load after shutdown: %v", loadErr)
	}
	if reloaded == nil || reloaded.Status != queue.QueueStatusPausedByDrain || !reloaded.ResumeOnStart {
		t.Fatalf("durable queue after shutdown = %+v; want paused-by-drain with resume_on_start", reloaded)
	}
	got := qs.Queue()
	if got == nil || got.Status != queue.QueueStatusPausedByDrain || !got.ResumeOnStart {
		t.Fatalf("stored queue after shutdown = %+v; want paused-by-drain with resume_on_start", got)
	}
}

// TestQueueCancel_AlreadyTerminal_NoOp verifies that shutdown drain is a
// no-op when the queue has already reached a terminal state (paused-by-failure)
// before ctx was cancelled — e.g. when evaluateGroupAdvanceWithOutcome fired
// in-flight. The canonical queue.json (paused-by-failure) must be untouched.
//
// Bead ref: hk-ppt32.
func TestQueueCancel_AlreadyTerminal_NoOp(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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

	p := daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	}
	deps := daemon.ExportedTestRuntime(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	awaitLoopTeardown(t, loopDone, "work loop")

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

// TestQueueShutdown_NamedQueuePersistsRestartDrain verifies that shutdown
// applies the same restart contract to every named queue.
//
// The test simulates a daemon shutdown with a named queue "cp" still active
// (no items dispatched). After the workloop exits it asserts:
//
// Bead ref: hk-u6m4l.
func TestQueueShutdown_NamedQueuePersistsRestartDrain(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const queueName = "cp"
	const beadID = core.BeadID("hk-u6m4l-named-queue-cancel-bead-001")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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

	loaded, err := queue.Load(context.Background(), projectDir, queueName)
	if err != nil || loaded == nil || loaded.Status != queue.QueueStatusActive {
		t.Fatalf("precondition: expected active %q queue on disk; loaded=%v err=%v", queueName, loaded, err)
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q) // SetQueue normalises to q.Name = "cp"

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.TestRuntimeParams{
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
	deps := daemon.ExportedTestRuntime(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	awaitLoopTeardown(t, loopDone, "work loop")

	reloaded, loadErr := queue.Load(context.Background(), projectDir, queueName)
	if loadErr != nil {
		t.Fatalf("queue.Load(%q) after shutdown: %v", queueName, loadErr)
	}
	if reloaded == nil || reloaded.Status != queue.QueueStatusPausedByDrain || !reloaded.ResumeOnStart {
		t.Fatalf("durable named queue after shutdown = %+v; want paused-by-drain with resume_on_start", reloaded)
	}
	got := qs.QueueByName(queueName)
	if got == nil || got.Status != queue.QueueStatusPausedByDrain || !got.ResumeOnStart {
		t.Fatalf("stored named queue after shutdown = %+v; want paused-by-drain with resume_on_start", got)
	}
}
