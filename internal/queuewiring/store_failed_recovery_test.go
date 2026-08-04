package queuewiring

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

func TestTransact_FailedRecoveryWritesReceiptBeforeInstall(t *testing.T) {
	t.Parallel()

	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	q := preconditionQueue(t, "alpha", "hk-recovery")
	q.Status = queue.QueueStatusPausedByFailure
	store.SetQueueByName("alpha", q)

	receiptID := "0190b3c4-9001-7000-8000-000000000010"
	priorBytes, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	candidate := *q
	candidate.Status = queue.QueueStatusActive
	candidate.FailedRecoveryReceiptID = &receiptID
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	priorDigest := sha256.Sum256(priorBytes)
	candidateDigest := sha256.Sum256(candidateBytes)
	receiptBytes := []byte(`{"schema_version":1,"record_type":"failed-recovery","queue_id":"` + q.QueueID + `","receipt_id":"0190b3c4-9001-7000-8000-000000000010","transaction_id":"0190b3c4-9001-7000-8000-000000000011","normalized_name":"alpha","prior_queue_sha256":"` + hex.EncodeToString(priorDigest[:]) + `","recovered_queue_sha256":"` + hex.EncodeToString(candidateDigest[:]) + `","recovered_items":[],"recovered_at":"2026-08-02T18:22:11.482Z"}`)
	digest := sha256.Sum256(receiptBytes)
	binding := &queue.FailedRecoveryReceiptBinding{
		ReceiptID:            receiptID,
		TransactionID:        "0190b3c4-9001-7000-8000-000000000011",
		Basename:             q.QueueID + "--" + receiptID + ".json",
		SchemaVersion:        1,
		CanonicalBytesBase64: base64.StdEncoding.EncodeToString(receiptBytes),
		SHA256:               hex.EncodeToString(digest[:]),
	}

	got := store.Transact(context.Background(), queue.TransactionRequest{
		Snapshot:                     store.Snapshot("alpha"),
		ProjectDir:                   projectDir,
		TransactionID:                binding.TransactionID,
		OperationKind:                queue.OperationFailedRecovery,
		FailedRecoveryReceiptBinding: binding,
		Mutate: func(candidate *queue.Queue) error {
			candidate.Status = queue.QueueStatusActive
			candidate.FailedRecoveryReceiptID = &receiptID
			return nil
		},
	})
	if !got.Committed() || got.CleanupErr != nil {
		t.Fatalf("Transact outcome = %q cleanup=%v err=%v, want committed", got.Outcome, got.CleanupErr, got.Err)
	}
	if got.Snapshot.Queue == nil || got.Snapshot.Queue.FailedRecoveryReceiptID == nil {
		t.Fatal("installed queue has no failed recovery receipt ID")
	}
	path := filepath.Join(projectDir, ".harmonik", "queues", ".failed-recovery-receipts", binding.Basename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if string(data) != string(receiptBytes) {
		t.Errorf("receipt bytes = %q, want %q", data, receiptBytes)
	}
}

func TestRecoverFailed_ReturnsSameReceiptWithoutSecondMutation(t *testing.T) {
	t.Parallel()

	projectDir := preconditionProjectDir(t)
	store := NewQueueStore()
	q := preconditionQueue(t, "alpha", "hk-recovery-idempotent")
	runID := "0190b3c4-9001-7000-8000-000000000012"
	q.Status = queue.QueueStatusPausedByFailure
	q.Groups[0].Status = queue.GroupStatusCompleteWithFailures
	q.Groups[0].Items[0].Status = queue.ItemStatusFailed
	q.Groups[0].Items[0].RunID = &runID
	store.SetQueueByName("alpha", q)

	first := store.commitFailedRecovery(context.Background(), projectDir, "alpha")
	if !first.Committed() || first.CleanupErr != nil {
		t.Fatalf("first recovery outcome = %q cleanup=%v err=%v", first.Outcome, first.CleanupErr, first.Err)
	}
	if first.Receipt.ReceiptID == "" || len(first.Receipt.RecoveredItems) != 1 {
		t.Fatalf("first receipt = %+v, want one recovered item", first.Receipt)
	}
	if got := first.Receipt.RecoveredItems[0].RetiredRunID; got == nil || *got != runID {
		t.Errorf("retired run ID = %v, want %q", got, runID)
	}
	if got := first.Snapshot.Queue.Groups[0].Items[0].RunID; got != nil {
		t.Errorf("recovered item run ID = %q, want nil", *got)
	}

	second := store.commitFailedRecovery(context.Background(), projectDir, "alpha")
	if !second.Committed() || second.CleanupErr != nil {
		t.Fatalf("second recovery outcome = %q cleanup=%v err=%v", second.Outcome, second.CleanupErr, second.Err)
	}
	if second.Receipt.ReceiptID != first.Receipt.ReceiptID {
		t.Errorf("second receipt ID = %q, want %q", second.Receipt.ReceiptID, first.Receipt.ReceiptID)
	}
	if second.Snapshot.Generation != first.Snapshot.Generation {
		t.Errorf("second recovery generation = %d, want unchanged %d", second.Snapshot.Generation, first.Snapshot.Generation)
	}
}
