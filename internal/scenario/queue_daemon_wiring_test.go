package scenario

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

func queueDaemonWiringQueueJSON(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

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

type queueDaemonWiringFakeLedger struct{}

func (f *queueDaemonWiringFakeLedger) ShowBead(_ context.Context, _ core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, errors.New("brcli: bead not found")
}

func (f *queueDaemonWiringFakeLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

type queueDaemonWiringFakeEmitter struct{}

func (f *queueDaemonWiringFakeEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return nil
}

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

	if err := queue.Persist(context.Background(), projectDir, &q); err != nil {
		t.Fatalf("(submit) Persist: %v", err)
	}

	if _, err := os.Stat(queueDaemonWiringQueueJSON(projectDir)); err != nil {
		t.Fatalf("queue.json absent after Persist: %v", err)
	}

	ledger := &queueDaemonWiringFakeLedger{}
	emitter := &queueDaemonWiringFakeEmitter{}
	loadedQueues, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		emitter,
		nil,
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}
	if len(loadedQueues) == 0 {
		t.Fatal("LoadQueueAtStartup: returned empty slice; expected at least one queue (queue.json was present)")
	}
	var loaded *queue.Queue
	for _, lq := range loadedQueues {
		if lq != nil && queue.NormaliseQueueName(lq.Name) == queue.QueueNameMain {
			loaded = lq
			break
		}
	}
	if loaded == nil {
		t.Fatal("LoadQueueAtStartup: main queue not found in returned slice")
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

// TestQueueDaemonWiring_Absent_ReturnsNil verifies that LoadQueueAtStartup
// returns (nil, nil) when no queue.json is present — the daemon starts with no
// active queue (QM-002 file-absent outcome).
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
func TestQueueDaemonWiring_Absent_ReturnsNil(t *testing.T) {
	t.Parallel()

	projectDir := queueDaemonWiringProjectDir(t)

	ledger := &queueDaemonWiringFakeLedger{}
	loadedQueues, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup (absent): %v", err)
	}
	if len(loadedQueues) != 0 {
		t.Errorf("LoadQueueAtStartup (absent): want empty slice, got %d queues", len(loadedQueues))
	}
}

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

	if err := queue.Persist(context.Background(), projectDir, &q); err != nil {
		t.Fatalf("(submit) Persist: %v", err)
	}

	ledger := &queueDaemonWiringFakeLedger{}
	emitter := &queueDaemonWiringFakeEmitter{}
	loadedQueues, err := lifecycle.LoadQueueAtStartup(
		context.Background(),
		projectDir,
		ledger,
		emitter,
		nil,
	)
	if err != nil {
		t.Fatalf("(load) LoadQueueAtStartup: %v", err)
	}
	if len(loadedQueues) == 0 {
		t.Fatal("(load) LoadQueueAtStartup returned empty slice; expected loaded queue")
	}
	var loaded *queue.Queue
	for _, lq := range loadedQueues {
		if lq != nil && queue.NormaliseQueueName(lq.Name) == queue.QueueNameMain {
			loaded = lq
			break
		}
	}
	if loaded == nil {
		t.Fatal("(load) main queue not found in LoadQueueAtStartup result")
	}

	runID := "00000000-0000-0000-0000-000000000001"
	loaded.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	loaded.Groups[0].Items[0].RunID = &runID

	loaded.Groups[0].Items[0].Status = queue.ItemStatusCompleted

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

	loaded.Status = queue.QueueStatusCompleted

	if err := queue.CompleteAndUnlink(context.Background(), projectDir, loaded); err != nil {
		t.Fatalf("(unlink) CompleteAndUnlink: %v", err)
	}

	if _, statErr := os.Stat(queueDaemonWiringQueueJSON(projectDir)); statErr == nil {
		t.Error("(unlink QM-003) queues/main.json still present after CompleteAndUnlink; want absent")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("(unlink QM-003) queues/main.json stat error (not IsNotExist): %v", statErr)
	}

	reloaded, reloadErr := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if reloadErr != nil {
		t.Fatalf("(unlink) post-unlink Load: %v", reloadErr)
	}
	if reloaded != nil {
		t.Errorf("(unlink) post-unlink Load: want nil, got queue ID=%q", reloaded.QueueID)
	}
}
