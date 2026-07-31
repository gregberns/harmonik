package queuewiring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// QM-001 says the daemon refuses further mutations to a queue after an I/O
// error in the atomic-write sequence. The distinction that matters, and that is
// easy to get wrong, is what counts as an I/O error: a write that was attempted
// and failed does, a request refused before any I/O does not.

// A failed write shuts the queue, and the refusal is sticky — repairing the
// filesystem underneath does not reopen it. Recovery is an operator restart.
func TestTransact_FailedWriteQuarantinesTheQueue(t *testing.T) {
	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	store.SetQueueByName("alpha", preconditionQueue(t, "alpha", "hk-quarantine"))

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	dispatch := func() TransactionResult {
		return store.Transact(context.Background(), TransactionRequest{
			Snapshot:      store.Snapshot("alpha"),
			ProjectDir:    projectDir,
			OperationKind: queue.OperationReservation,
			Mutate: func(q *queue.Queue) error {
				q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
				return nil
			},
		})
	}

	first := dispatch()
	if first.Committed() {
		t.Fatal("the write should have failed against a read-only directory")
	}
	if first.Outcome == queue.OutcomeRejected {
		t.Fatalf("outcome = %q; want an I/O failure outcome, not a pre-I/O refusal", first.Outcome)
	}

	if err := os.Chmod(queuesDir, 0o700); err != nil { //nolint:gosec // proving the quarantine outlives the repair
		t.Fatalf("chmod queues dir: %v", err)
	}
	second := dispatch()
	if !errors.Is(second.Err, ErrQueueQuarantined) {
		t.Errorf("err = %v; want ErrQueueQuarantined — repairing the filesystem must not reopen the queue", second.Err)
	}
}

// A replacement refused before any I/O leaves the queue usable. Quarantining
// here would shut a queue over a malformed request, which is not what QM-001
// describes and would take the daemon down for a caller's mistake.
func TestTransact_PreIORefusalDoesNotQuarantine(t *testing.T) {
	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	store.SetQueueByName("alpha", preconditionQueue(t, "alpha", "hk-refused"))

	// OperationPause with a cancelled candidate status is an invalid pairing:
	// WriteReplacement refuses it while validating, before touching the disk.
	refused := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      store.Snapshot("alpha"),
		ProjectDir:    projectDir,
		OperationKind: queue.OperationPause,
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusCancelled
			return nil
		},
	})
	if refused.Outcome != queue.OutcomeRejected {
		t.Fatalf("outcome = %q; want %q", refused.Outcome, queue.OutcomeRejected)
	}

	// The queue must still accept a well-formed transaction.
	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      store.Snapshot("alpha"),
		ProjectDir:    projectDir,
		OperationKind: queue.OperationPause,
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusPausedByDrain
			return nil
		},
	})
	if errors.Is(got.Err, ErrQueueQuarantined) {
		t.Fatal("a pre-I/O refusal quarantined the queue; nothing about the disk was in doubt")
	}
	if !got.Committed() {
		t.Fatalf("outcome = %q err=%v; want a committed transaction", got.Outcome, got.Err)
	}
}
