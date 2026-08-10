package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleQueueStatusPrefersLiveQueueOverReceipt(t *testing.T) {
	projectDir, prepared := receiptBackedStatusFixture(t)
	live := completionFixtureQueue()
	live.Status = QueueStatusPausedByFailure
	if err := Persist(context.Background(), projectDir, &live); err != nil {
		t.Fatal(err)
	}

	resp, rpcErr := HandleQueueStatus(t.Context(), projectDir, QueueStatusRequest{QueueID: prepared.Receipt.QueueID})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if resp.Queue == nil || resp.Queue.QueueID != live.QueueID || resp.Queue.Status != QueueStatusPausedByFailure || resp.Completed {
		t.Fatalf("response = %+v", resp)
	}
}

func receiptBackedStatusFixture(t *testing.T) (string, CompletionPlan) {
	t.Helper()
	plan, prepared := completionReplacementFixture(t)
	if err := writeCompletionReceipt(
		plan.ProjectDir,
		prepared.Receipt.QueueID,
		prepared.Receipt.TransactionID,
		prepared.Binding,
		osNamespaceOps(),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(queuePath(plan.ProjectDir, QueueNameMain)); err != nil {
		t.Fatal(err)
	}
	return plan.ProjectDir, prepared
}

func TestHandleQueueStatusReturnsReceiptBackedCompletion(t *testing.T) {
	projectDir, prepared := receiptBackedStatusFixture(t)
	watched := 0
	resp, rpcErr := HandleQueueStatus(t.Context(), projectDir, QueueStatusRequest{
		QueueID:           prepared.Receipt.QueueID,
		WatchedGroupIndex: &watched,
	})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if resp.Queue != nil || !resp.Completed || resp.FinalStatus != GroupStatusCompleteSuccess {
		t.Fatalf("response = %+v", resp)
	}
	if resp.FinalGroupIndex == nil || *resp.FinalGroupIndex != prepared.Receipt.FinalGroupIndex ||
		resp.SuccessCount == nil || *resp.SuccessCount != prepared.Receipt.SuccessCount ||
		resp.FailCount == nil || *resp.FailCount != 0 ||
		resp.CompletionReceiptID != prepared.Receipt.ReceiptID {
		t.Fatalf("receipt-backed fields = %+v", resp)
	}
	if resp.CompletedAt == nil || resp.CompletedAt.Format("2006-01-02T15:04:05.999Z07:00") != prepared.Receipt.CompletedAt {
		t.Fatalf("completed_at = %v, want %s", resp.CompletedAt, prepared.Receipt.CompletedAt)
	}

	before, err := os.ReadDir(completionReceiptsDir(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	repeated, rpcErr := HandleQueueStatus(t.Context(), projectDir, QueueStatusRequest{QueueID: prepared.Receipt.QueueID})
	if rpcErr != nil || !repeated.Completed || repeated.CompletionReceiptID != resp.CompletionReceiptID {
		t.Fatalf("repeated response = %+v, err=%v", repeated, rpcErr)
	}
	after, err := os.ReadDir(completionReceiptsDir(projectDir))
	if err != nil || len(after) != len(before) {
		t.Fatalf("status mutated receipt root: before=%d after=%d err=%v", len(before), len(after), err)
	}
}

func TestHandleQueueStatusReceiptDoesNotCoverWatchedGroup(t *testing.T) {
	projectDir, prepared := receiptBackedStatusFixture(t)
	watched := prepared.Receipt.FinalGroupIndex + 1
	resp, rpcErr := HandleQueueStatus(t.Context(), projectDir, QueueStatusRequest{
		QueueID:           prepared.Receipt.QueueID,
		WatchedGroupIndex: &watched,
	})
	if rpcErr != nil || resp.Queue != nil || resp.Completed {
		t.Fatalf("response = %+v, err=%v", resp, rpcErr)
	}
}

func TestReadCompletionReceiptForStatusRejectsIdentityErrors(t *testing.T) {
	t.Run("invalid queue id", func(t *testing.T) {
		_, _, err := ReadCompletionReceiptForStatus(t.TempDir(), "wrong", "")
		if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("wrong optional receipt id", func(t *testing.T) {
		projectDir, prepared := receiptBackedStatusFixture(t)
		_, found, err := ReadCompletionReceiptForStatus(
			projectDir,
			prepared.Receipt.QueueID,
			"0197c452-0000-7000-8000-000000000099",
		)
		if err != nil || found {
			t.Fatalf("result = (%v, %v)", found, err)
		}
	})

	t.Run("corrupt matching receipt", func(t *testing.T) {
		projectDir, prepared := receiptBackedStatusFixture(t)
		root := completionReceiptsDir(projectDir)
		if err := os.Remove(filepath.Join(root, prepared.Binding.Basename)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, prepared.Binding.Basename), []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadCompletionReceiptForStatus(projectDir, prepared.Receipt.QueueID, "")
		if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("multiple matching receipts", func(t *testing.T) {
		projectDir, prepared := receiptBackedStatusFixture(t)
		second, err := PrepareCompletion(
			completionFixtureQueue(),
			completionTransactionID,
			"0197c452-0000-7000-8000-000000000099",
			completionFixtureTime(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeCompletionReceipt(
			projectDir,
			second.Receipt.QueueID,
			second.Receipt.TransactionID,
			second.Binding,
			osNamespaceOps(),
		); err != nil {
			t.Fatal(err)
		}
		_, _, err = ReadCompletionReceiptForStatus(projectDir, prepared.Receipt.QueueID, "")
		if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("matching symlink", func(t *testing.T) {
		projectDir, prepared := receiptBackedStatusFixture(t)
		root := completionReceiptsDir(projectDir)
		target := filepath.Join(root, prepared.Binding.Basename)
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(projectDir, "outside.json"), target); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadCompletionReceiptForStatus(projectDir, prepared.Receipt.QueueID, "")
		if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("matching directory", func(t *testing.T) {
		projectDir, prepared := receiptBackedStatusFixture(t)
		root := completionReceiptsDir(projectDir)
		target := filepath.Join(root, prepared.Binding.Basename)
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadCompletionReceiptForStatus(projectDir, prepared.Receipt.QueueID, "")
		if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})

}
