package queuewiring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// The generation guard covers one queue name, so it cannot see a different
// queue change. Precondition is how a caller reaches evidence outside its own
// queue while still inside the write lock that guards its write — the
// dispatcher's duplicate-bead guard is why it exists.

func preconditionProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	return dir
}

func preconditionQueue(t *testing.T, name string, beadID core.BeadID) *queue.Queue {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       id.String(),
		Name:          name,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
		}},
	}
}

// A precondition that returns an error rejects the transaction and performs no
// I/O, so the queue file is never created.
func TestTransact_PreconditionVetoRejectsWithoutWriting(t *testing.T) {
	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	store.SetQueueByName("alpha", preconditionQueue(t, "alpha", "hk-veto"))

	veto := errors.New("not allowed")
	mutated := false
	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      store.Snapshot("alpha"),
		ProjectDir:    projectDir,
		OperationKind: queue.OperationReservation,
		Precondition:  func(map[string]*queue.Queue) error { return veto },
		Mutate: func(*queue.Queue) error {
			mutated = true
			return nil
		},
	})

	if got.Outcome != queue.OutcomeRejected {
		t.Errorf("outcome = %q; want %q", got.Outcome, queue.OutcomeRejected)
	}
	if !errors.Is(got.Err, veto) {
		t.Errorf("err = %v; want the precondition's own error so the caller can branch on it", got.Err)
	}
	if mutated {
		t.Error("Mutate ran after the precondition vetoed")
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", "alpha.json")); !os.IsNotExist(statErr) {
		t.Error("a vetoed transaction wrote the queue file; it must perform no I/O")
	}
}

// The precondition sees every OTHER queue and never its own, so a caller
// scanning for a conflict cannot match itself.
func TestTransact_PreconditionSeesOtherQueuesOnly(t *testing.T) {
	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	store.SetQueueByName("alpha", preconditionQueue(t, "alpha", "hk-self"))
	store.SetQueueByName("beta", preconditionQueue(t, "beta", "hk-other"))

	var seen []string
	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      store.Snapshot("alpha"),
		ProjectDir:    projectDir,
		OperationKind: queue.OperationReservation,
		Precondition: func(others map[string]*queue.Queue) error {
			for name := range others {
				seen = append(seen, name)
			}
			return nil
		},
		Mutate: func(q *queue.Queue) error {
			q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
			return nil
		},
	})

	if !got.Committed() {
		t.Fatalf("outcome = %q err=%v; want a committed transaction", got.Outcome, got.Err)
	}
	if len(seen) != 1 || seen[0] != "beta" {
		t.Errorf("precondition saw %v; want exactly [beta] — never the queue being mutated", seen)
	}
}

// A nil precondition is the common case and must not change behavior.
func TestTransact_NilPreconditionCommits(t *testing.T) {
	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	store.SetQueueByName("alpha", preconditionQueue(t, "alpha", "hk-nil"))

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      store.Snapshot("alpha"),
		ProjectDir:    projectDir,
		OperationKind: queue.OperationReservation,
		Mutate: func(q *queue.Queue) error {
			q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
			return nil
		},
	})

	if !got.Committed() {
		t.Fatalf("outcome = %q err=%v; want a committed transaction", got.Outcome, got.Err)
	}
}
