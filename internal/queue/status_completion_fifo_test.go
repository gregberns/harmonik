//go:build !windows

package queue

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadCompletionReceiptForStatusRejectsMatchingFIFO(t *testing.T) {
	projectDir, prepared := receiptBackedStatusFixture(t)
	root := completionReceiptsDir(projectDir)
	target := filepath.Join(root, prepared.Binding.Basename)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(target, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadCompletionReceiptForStatus(projectDir, prepared.Receipt.QueueID, "")
	if !errors.Is(err, ErrCompletionReceiptIdentityIntegrity) {
		t.Fatalf("error = %v", err)
	}
}
