package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrCompletionReceiptIdentityIntegrity means receipt facts for one queue ID
// are corrupt, unsupported, conflicting, or ambiguous.
var ErrCompletionReceiptIdentityIntegrity = errors.New("queue: completion receipt identity integrity")

// ReadCompletionReceiptForStatus returns the one exact receipt that can answer
// status for queueID. receiptID narrows the lookup when it is non-empty.
func ReadCompletionReceiptForStatus(
	projectDir string,
	queueID string,
	receiptID string,
) (CompletionReceipt, bool, error) {
	if err := validateUUIDv7(queueID); err != nil {
		return CompletionReceipt{}, false, fmt.Errorf("%w: queue id: %w", ErrCompletionReceiptIdentityIntegrity, err)
	}
	if receiptID != "" {
		if err := validateUUIDv7(receiptID); err != nil {
			return CompletionReceipt{}, false, fmt.Errorf("%w: receipt id: %w", ErrCompletionReceiptIdentityIntegrity, err)
		}
	}
	root := completionReceiptsDir(projectDir)
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return CompletionReceipt{}, false, nil
	}
	if err != nil {
		return CompletionReceipt{}, false, fmt.Errorf("read completion receipt root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return CompletionReceipt{}, false, fmt.Errorf("%w: completion receipt root is not a real directory", ErrCompletionReceiptIdentityIntegrity)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return CompletionReceipt{}, false, fmt.Errorf("read completion receipt root: %w", err)
	}
	prefix := queueID + "--"
	matches := make([]CompletionReceipt, 0, len(entries))
	for _, entry := range entries {
		candidateReceiptID, selected, selectErr := selectCompletionStatusEntry(entry, prefix, receiptID)
		if selectErr != nil {
			return CompletionReceipt{}, false, selectErr
		}
		if !selected {
			continue
		}
		name := entry.Name()
		receipt, readErr := readCompletionStatusReceipt(root, name, queueID, candidateReceiptID)
		if readErr != nil {
			return CompletionReceipt{}, false, readErr
		}
		matches = append(matches, receipt)
	}
	if len(matches) == 0 {
		return CompletionReceipt{}, false, nil
	}
	if len(matches) != 1 {
		return CompletionReceipt{}, false, fmt.Errorf("%w: queue id %q has %d receipts", ErrCompletionReceiptIdentityIntegrity, queueID, len(matches))
	}
	return matches[0], true, nil
}

func selectCompletionStatusEntry(
	entry os.DirEntry,
	prefix string,
	receiptID string,
) (candidateReceiptID string, selected bool, err error) {
	name := entry.Name()
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") ||
		strings.HasSuffix(name, ".release-v1.json") || strings.Contains(name, ".tmp-") {
		return "", false, nil
	}
	info, infoErr := entry.Info()
	if infoErr != nil {
		return "", false, fmt.Errorf("%w: inspect completion receipt %q: %w", ErrCompletionReceiptIdentityIntegrity, name, infoErr)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("%w: completion receipt %q is not a regular file", ErrCompletionReceiptIdentityIntegrity, name)
	}
	candidateReceiptID = strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
	if receiptID != "" && candidateReceiptID != receiptID {
		return "", false, nil
	}
	return candidateReceiptID, true, nil
}

func readCompletionStatusReceipt(
	root string,
	name string,
	queueID string,
	receiptID string,
) (CompletionReceipt, error) {
	data, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // the entry came from this exact root
	if err != nil {
		return CompletionReceipt{}, fmt.Errorf("read completion receipt: %w", err)
	}
	receipt, err := DecodeCompletionReceipt(data)
	if err != nil {
		return CompletionReceipt{}, fmt.Errorf("%w: decode receipt %q: %w", ErrCompletionReceiptIdentityIntegrity, name, err)
	}
	if receipt.QueueID != queueID || receipt.ReceiptID != receiptID {
		return CompletionReceipt{}, fmt.Errorf("%w: receipt %q does not match its filename", ErrCompletionReceiptIdentityIntegrity, name)
	}
	expectedName, err := CompletionReceiptBasename(receipt.QueueID, receipt.ReceiptID)
	if err != nil || expectedName != name {
		return CompletionReceipt{}, fmt.Errorf("%w: invalid receipt basename %q", ErrCompletionReceiptIdentityIntegrity, name)
	}
	return receipt, nil
}
