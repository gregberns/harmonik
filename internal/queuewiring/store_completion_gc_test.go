package queuewiring

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

func TestQueueStoreSerializesCompletionMarkerInstallWithGC(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	installEntered := make(chan struct{})
	releaseInstall := make(chan struct{})
	completionDone := make(chan queue.CompletionResult, 1)
	go func() {
		completionDone <- store.complete(t.Context(), queue.CompletionRequest{
			Snapshot:      store.Snapshot(queue.QueueNameMain),
			ProjectDir:    projectDir,
			TransactionID: storeCompletionTransactionID,
			ReceiptID:     storeCompletionReceiptID,
			CompletedAt:   stamp,
			ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
			Observe:       func(queue.CompletionReceipt) error { return nil },
		}, queue.CleanupCompletedCanonical, queue.CleanupReplaceIntent,
			func(string, queue.CompletionReleaseMarkerInputs, time.Time) (queue.CompletionReleaseMarker, error) {
				close(installEntered)
				<-releaseInstall
				return queue.CompletionReleaseMarker{}, errors.New("stop after serialization proof")
			},
		)
	}()
	<-installEntered
	if store.completionMu.TryLock() {
		store.completionMu.Unlock()
		t.Fatal("marker installation did not own the completion domain")
	}
	gcEntered := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		_, gcErr := store.garbageCollectCompletionReceipts(
			projectDir,
			queue.CompletionGCObservation{},
			func(string, queue.CompletionGCObservation) ([]queue.CompletionGCResult, error) {
				close(gcEntered)
				return nil, nil
			},
		)
		if gcErr != nil {
			t.Errorf("GC error = %v", gcErr)
		}
		close(gcDone)
	}()
	close(releaseInstall)
	<-completionDone
	<-gcEntered
	<-gcDone
}

func TestQueueStoreCompletionGCUntrustedObservationPerformsNoIO(t *testing.T) {
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts")
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("wrong type"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewQueueStore()
	results, err := store.GarbageCollectCompletionReceipts(projectDir, queue.CompletionGCObservation{})
	if err != nil || len(results) != 0 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
}
