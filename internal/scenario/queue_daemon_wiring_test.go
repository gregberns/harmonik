package scenario

// queue_daemon_wiring_test.go — integration test for the daemon composition-root
// queue wiring: QueueStore instantiation, PL-005 step-8a load, and
// CompleteAndUnlink (queue.json unlink on completion).
//
// This test exercises the three gaps described in bead hk-gi471:
//   1. QueueStore is instantiated and populated from LoadQueueAtStartup.
//   2. LoadQueueAtStartup (PL-005 step 8a) loads queue.json BEFORE dispatch.
//   3. CompleteAndUnlink removes queue.json when the last group reaches
//      complete-success (QM-003 / QM-053).
//
// The test drives the path entirely at the library level (no real daemon process,
// no real `br` binary) using a fake BeadLedger stub.  This keeps the test
// deterministic and fast while exercising the exact code paths wired into
// daemon.Start (queueStore + LoadQueueAtStartup + CompleteAndUnlink).
//
// Helper prefix: queueDaemonWiring (this file).
//
// Spec refs:
//   - specs/queue-model.md §3.2 QM-002 (startup load)
//   - specs/queue-model.md §3.3 QM-003 (unlink on completion)
//   - specs/queue-model.md §8.4 QM-053 (CompleteAndUnlink sequence)
//   - specs/queue-model.md §9.1 QM-060 (single-writer QueueStore)
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// queueDaemonWiring fixture helpers
// ---------------------------------------------------------------------------

// queueDaemonWiringProjectDir creates a temporary project root with a
// .harmonik/ subdirectory for queue.json I/O. Registered for t.Cleanup.
func queueDaemonWiringProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("queueDaemonWiringProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// queueDaemonWiringQueueJSON returns the expected path to queue.json under projectDir.
func queueDaemonWiringQueueJSON(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queue.json")
}

// queueDaemonWiringSingleItemQueue builds a minimal Queue with one group and
// one item — sufficient to exercise the submit → drain → unlink path.
func queueDaemonWiringSingleItemQueue(t *testing.T) queue.Queue {
	t.Helper()
	now := time.Now().UTC()
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qdw-test-queue-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-qdw-item0"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: now,
			},
		},
	}
}

// queueDaemonWiringFakeLedger is a minimal lifecycle.BeadLedger stub.
// ShowBead returns not-found for all IDs (no dispatched items to cross-check).
type queueDaemonWiringFakeLedger struct{}

func (f *queueDaemonWiringFakeLedger) ShowBead(_ context.Context, _ core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, errors.New("brcli: bead not found")
}

// queueDaemonWiringFakeEmitter is a no-op lifecycle.QueueEventEmitter stub.
type queueDaemonWiringFakeEmitter struct{}

func (f *queueDaemonWiringFakeEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return nil
}

// ---------------------------------------------------------------------------
// TestQueueDaemonWiring_LoadAtStartup
// ---------------------------------------------------------------------------

// TestQueueDaemonWiring_LoadAtStartup exercises PL-005 step 8a (QM-002):
// queue.json written before daemon startup is loaded by LoadQueueAtStartup
// and returned as a non-nil *queue.Queue with the expected QueueID.
//
// This is the equivalent of "submit" in the submit → drain → unlink cycle:
// the queue is on disk (as if submitted by a prior orchestrator call) and the
// daemon's startup path loads it into the QueueStore.
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
func TestQueueDaemonWiring_LoadAtStartup(t *testing.T) {
	t.Parallel()

	projectDir := queueDaemonWiringProjectDir(t)
	q := queueDaemonWiringSingleItemQueue(t)

	// "Submit": persist the queue as if queue-submit wrote it.
	if err := queue.Persist(context.Background(), projectDir, &q); err != nil {
		t.Fatalf("(submit) Persist: %v", err)
	}

	// Verify queue.json was written.
	if _, err := os.Stat(queueDaemonWiringQueueJSON(projectDir)); err != nil {
		t.Fatalf("queue.json absent after Persist: %v", err)
	}

	// "Step 8a": daemon startup calls LoadQueueAtStartup.
	ledger := &queueDaemonWiringFakeLedger{}
	emitter := &queueDaemonWiringFakeEmitter{}
	loaded, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		emitter,
		nil, // slog.Default()
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadQueueAtStartup: returned nil queue; expected non-nil (queue.json was present)")
	}
	if loaded.QueueID != q.QueueID {
		t.Errorf("loaded.QueueID = %q; want %q", loaded.QueueID, q.QueueID)
	}
	if loaded.Status != queue.QueueStatusActive {
		t.Errorf("loaded.Status = %q; want active", loaded.Status)
	}
	if len(loaded.Groups) != 1 {
		t.Fatalf("loaded.Groups = %d; want 1", len(loaded.Groups))
	}
	if len(loaded.Groups[0].Items) != 1 {
		t.Fatalf("loaded.Groups[0].Items = %d; want 1", len(loaded.Groups[0].Items))
	}
}

// ---------------------------------------------------------------------------
// TestQueueDaemonWiring_Absent_ReturnsNil
// ---------------------------------------------------------------------------

// TestQueueDaemonWiring_Absent_ReturnsNil verifies that LoadQueueAtStartup
// returns (nil, nil) when no queue.json is present — the daemon starts with no
// active queue (QM-002 file-absent outcome).
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
func TestQueueDaemonWiring_Absent_ReturnsNil(t *testing.T) {
	t.Parallel()

	projectDir := queueDaemonWiringProjectDir(t)
	// No queue.json written.

	ledger := &queueDaemonWiringFakeLedger{}
	loaded, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup (absent): %v", err)
	}
	if loaded != nil {
		t.Errorf("LoadQueueAtStartup (absent): want nil, got non-nil queue (ID=%q)", loaded.QueueID)
	}
}

// ---------------------------------------------------------------------------
// TestQueueDaemonWiring_DrainAndUnlink
// ---------------------------------------------------------------------------

// TestQueueDaemonWiring_DrainAndUnlink exercises the full submit → drain →
// unlink cycle:
//
//  1. Submit: Persist a single-item queue to queue.json.
//  2. Load: LoadQueueAtStartup returns the queue into the QueueStore.
//  3. Drain: simulate dispatch by marking the item completed and advancing
//     the group to complete-success.
//  4. Unlink: CompleteAndUnlink removes queue.json (QM-003 / QM-053).
//
// The QueueStore (daemon.QueueStore) is not instantiated here because it lives
// in internal/daemon (circular import from internal/scenario); the test exercises
// the underlying operations directly.  The production composition root wires all
// three into daemon.Start; this test asserts the operations behave correctly in
// isolation.
//
// Spec refs:
//   - specs/queue-model.md §3.3 QM-003 (unlink on completion)
//   - specs/queue-model.md §8.4 QM-053 (CompleteAndUnlink sequence)
//   - specs/queue-model.md §5.2 QM-030 (all-terminal gate)
//   - specs/queue-model.md §9.1 QM-060 (single-writer — LoadQueueAtStartup owns
//     the startup write path; QueueStore serialises mutations at runtime)
func TestQueueDaemonWiring_DrainAndUnlink(t *testing.T) {
	t.Parallel()

	projectDir := queueDaemonWiringProjectDir(t)
	q := queueDaemonWiringSingleItemQueue(t)

	// ── Step 1: Submit — persist the queue as if queue-submit ran. ─────────────
	if err := queue.Persist(context.Background(), projectDir, &q); err != nil {
		t.Fatalf("(submit) Persist: %v", err)
	}

	// ── Step 2: Load — daemon startup calls LoadQueueAtStartup (PL-005 step 8a). ──
	ledger := &queueDaemonWiringFakeLedger{}
	emitter := &queueDaemonWiringFakeEmitter{}
	loaded, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		emitter,
		nil,
	)
	if err != nil {
		t.Fatalf("(load) LoadQueueAtStartup: %v", err)
	}
	if loaded == nil {
		t.Fatal("(load) LoadQueueAtStartup returned nil; expected loaded queue")
	}

	// ── Step 3: Drain — simulate the work loop dispatching and completing the item. ──
	// Mark item 0 as dispatched (QueueStore.LockForMutation path in workloop.go).
	runID := "00000000-0000-0000-0000-000000000001"
	loaded.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	loaded.Groups[0].Items[0].RunID = &runID

	// Mark item 0 as completed (evaluateGroupAdvanceWithOutcome path).
	loaded.Groups[0].Items[0].Status = queue.ItemStatusCompleted

	// Advance group 0: all items terminal → complete-success (QM-030).
	now := time.Now().UTC()
	newGroupStatus, events, advErr := queue.AdvanceGroup(
		context.Background(),
		&loaded.Groups[0],
		loaded.Status,
		loaded.QueueID,
		now,
	)
	if advErr != nil {
		t.Fatalf("(drain) AdvanceGroup: %v", advErr)
	}
	if newGroupStatus != queue.GroupStatusCompleteSuccess {
		t.Fatalf("(drain) group 0 status = %q; want complete-success", newGroupStatus)
	}
	if len(events) != 1 || events[0].Type != "queue_group_completed" {
		t.Errorf("(drain) expected 1 queue_group_completed event; got %v", len(events))
	}
	loaded.Groups[0].Status = newGroupStatus

	// All groups terminal → queue itself can be completed.
	loaded.Status = queue.QueueStatusCompleted

	// ── Step 4: Unlink — CompleteAndUnlink persists final status then removes queue.json. ──
	if err := queue.CompleteAndUnlink(context.Background(), projectDir, loaded); err != nil {
		t.Fatalf("(unlink) CompleteAndUnlink: %v", err)
	}

	// queue.json MUST be absent after CompleteAndUnlink (QM-003).
	if _, statErr := os.Stat(queueDaemonWiringQueueJSON(projectDir)); statErr == nil {
		t.Error("(unlink QM-003) queue.json still present after CompleteAndUnlink; want absent")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("(unlink QM-003) queue.json stat error (not IsNotExist): %v", statErr)
	}

	// Verify reload returns nil (queue.json absent → no active queue).
	reloaded, reloadErr := queue.Load(context.Background(), projectDir)
	if reloadErr != nil {
		t.Fatalf("(unlink) post-unlink Load: %v", reloadErr)
	}
	if reloaded != nil {
		t.Errorf("(unlink) post-unlink Load: want nil, got queue ID=%q", reloaded.QueueID)
	}
}
