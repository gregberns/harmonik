package queue

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CompletionGCResult reports one release marker considered by a GC pass.
type CompletionGCResult struct {
	QueueID        string
	ReceiptID      string
	ReceiptRemoved bool
	MarkerRemoved  bool
	Eligible       bool
	Phase          CompletionGCPhase
	Err            error
}

// CompletionGCPhase names the last durable or indeterminate deletion boundary.
type CompletionGCPhase string

// These are the phases a GC pass reports for one receipt pair. Preserved means
// the pass deleted nothing. Ineligible means the pass must not delete yet,
// because the retention time has not passed, or because the platform does not
// attest a synchronized and non-regressed clock. Today the daemon supplies no
// clock attestation, so the clock is the only reason production reports this
// phase. A durable phase means the deletion reached the disk. An indeterminate
// phase means the unlink ran but the directory sync did not confirm it, so a
// later pass must reach the same boundary again.
const (
	CompletionGCPhasePreserved                CompletionGCPhase = "preserved"
	CompletionGCPhaseIneligible               CompletionGCPhase = "ineligible"
	CompletionGCPhaseReceiptAbsentDurable     CompletionGCPhase = "receipt_absent_durable"
	CompletionGCPhaseReceiptSyncIndeterminate CompletionGCPhase = "receipt_sync_indeterminate"
	CompletionGCPhaseMarkerAbsentDurable      CompletionGCPhase = "marker_absent_durable"
	CompletionGCPhaseMarkerSyncIndeterminate  CompletionGCPhase = "marker_sync_indeterminate"
)

// CompletionGCObservation carries the platform-owned UTC trust decision.
// Synchronized means trusted and synchronized. Regressed always refuses GC.
type CompletionGCObservation struct {
	Now          time.Time
	Synchronized bool
	Regressed    bool
}

// CompletionReceiptGCEligible is the pure QM-006 retention decision.
func CompletionReceiptGCEligible(
	marker CompletionReleaseMarker,
	observation CompletionGCObservation,
) (bool, error) {
	if err := validateCompletionReleaseMarker(marker); err != nil {
		return false, err
	}
	if !observation.Synchronized || observation.Regressed {
		return false, nil
	}
	releasedAt, err := time.Parse("2006-01-02T15:04:05.000Z", marker.ReleasedAt)
	if err != nil {
		return false, err
	}
	gcNotBefore, err := time.Parse("2006-01-02T15:04:05.000Z", marker.GCNotBefore)
	if err != nil {
		return false, err
	}
	now := observation.Now.UTC()
	if now.Before(releasedAt) || now.Before(gcNotBefore) {
		return false, nil
	}
	return true, nil
}

// GarbageCollectCompletionReceipts runs one bounded receipt-first GC pass.
// The caller owns the synchronized fact. A false fact performs no I/O.
func GarbageCollectCompletionReceipts(
	projectDir string,
	observation CompletionGCObservation,
) ([]CompletionGCResult, error) {
	if !observation.Synchronized || observation.Regressed {
		return nil, nil
	}
	return garbageCollectCompletionReceipts(projectDir, observation, osNamespaceOps())
}

func garbageCollectCompletionReceipts(
	projectDir string,
	observation CompletionGCObservation,
	ops namespaceOps,
) ([]CompletionGCResult, error) {
	root := completionReceiptsDir(projectDir)
	rootInfo, err := ops.lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("completion receipt root is not a real directory")
	}
	if err := syncDirectory(root, ops); err != nil {
		return nil, err
	}
	entries, err := ops.readDir(root)
	if err != nil {
		return nil, err
	}
	results := make([]CompletionGCResult, 0)
	for _, entry := range entries {
		queueID, receiptID, selected := completionMarkerIdentityFromName(entry.Name())
		if !selected {
			continue
		}
		result := CompletionGCResult{QueueID: queueID, ReceiptID: receiptID}
		gcOneCompletionPair(root, entry, observation, ops, &result)
		results = append(results, result)
	}
	return results, nil
}

func completionMarkerIdentityFromName(name string) (queueID, receiptID string, selected bool) {
	const suffix = ".release-v1.json"
	if !strings.HasSuffix(name, suffix) || strings.Contains(name, ".tmp-") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(name, suffix), "--")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func gcOneCompletionPair(
	root string,
	entry os.DirEntry,
	observation CompletionGCObservation,
	ops namespaceOps,
	result *CompletionGCResult,
) {
	info, err := entry.Info()
	if err != nil || !info.Mode().IsRegular() {
		result.Phase = CompletionGCPhasePreserved
		result.Err = errors.Join(err, errors.New("completion release marker is not a regular file"))
		return
	}
	markerPath := filepath.Join(root, entry.Name())
	markerBytes, err := ops.readFile(markerPath)
	if err != nil {
		result.Phase = CompletionGCPhasePreserved
		result.Err = err
		return
	}
	marker, err := DecodeCompletionReleaseMarker(markerBytes)
	if err != nil || marker.QueueID != result.QueueID || marker.ReceiptID != result.ReceiptID {
		result.Phase = CompletionGCPhasePreserved
		result.Err = errors.Join(err, errors.New("completion release marker identity disagrees with filename"))
		return
	}
	eligible, err := CompletionReceiptGCEligible(marker, observation)
	if err != nil || !eligible {
		if err != nil {
			result.Phase = CompletionGCPhasePreserved
		} else {
			result.Phase = CompletionGCPhaseIneligible
		}
		result.Err = err
		return
	}
	result.Eligible = true
	receiptPath := filepath.Join(root, result.QueueID+"--"+result.ReceiptID+".json")
	receiptBytes, receiptPresent, err := readOptionalRegular(receiptPath, ops)
	if err != nil {
		result.Phase = CompletionGCPhasePreserved
		result.Err = err
		return
	}
	if receiptPresent {
		if err := validateCompletionGCPair(receiptBytes, marker); err != nil {
			result.Phase = CompletionGCPhasePreserved
			result.Err = err
			return
		}
		if err := removeExactAndSync(root, receiptPath, receiptBytes, ops); err != nil {
			result.Phase = completionGCSyncFailurePhase(
				receiptPath,
				CompletionGCPhaseReceiptSyncIndeterminate,
				CompletionGCPhasePreserved,
				ops,
			)
			result.Err = err
			return
		}
		result.ReceiptRemoved = true
	}
	result.Phase = CompletionGCPhaseReceiptAbsentDurable
	if err := removeExactAndSync(root, markerPath, markerBytes, ops); err != nil {
		result.Phase = completionGCSyncFailurePhase(
			markerPath,
			CompletionGCPhaseMarkerSyncIndeterminate,
			CompletionGCPhaseReceiptAbsentDurable,
			ops,
		)
		result.Err = err
		return
	}
	result.MarkerRemoved = true
	result.Phase = CompletionGCPhaseMarkerAbsentDurable
}

func completionGCSyncFailurePhase(
	path string,
	absentPhase CompletionGCPhase,
	presentPhase CompletionGCPhase,
	ops namespaceOps,
) CompletionGCPhase {
	_, present, err := readOptionalRegular(path, ops)
	if err == nil && !present {
		return absentPhase
	}
	return presentPhase
}

func readOptionalRegular(path string, ops namespaceOps) (data []byte, present bool, err error) {
	info, err := ops.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("completion receipt is not a regular file")
	}
	data, err = ops.readFile(path)
	return data, err == nil, err
}

func validateCompletionGCPair(receiptBytes []byte, marker CompletionReleaseMarker) error {
	receipt, err := DecodeCompletionReceipt(receiptBytes)
	if err != nil {
		return err
	}
	if receipt.QueueID != marker.QueueID || receipt.ReceiptID != marker.ReceiptID ||
		receipt.TransactionID != marker.TransactionID || digestHex(receiptBytes) != marker.ReceiptSHA256 ||
		receipt.CompletedQueueSHA256 != marker.CompletedQueueSHA256 {
		return errors.New("completion receipt and release marker disagree")
	}
	return nil
}

func removeExactAndSync(root, path string, expected []byte, ops namespaceOps) error {
	current, present, err := readOptionalRegular(path, ops)
	if err != nil {
		return err
	}
	if !present {
		return syncDirectory(root, ops)
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("refuse to remove changed completion record %q", filepath.Base(path))
	}
	removeErr := ops.remove(path)
	if removeErr != nil {
		_, stillPresent, reloadErr := readOptionalRegular(path, ops)
		if reloadErr != nil || stillPresent {
			return errors.Join(removeErr, reloadErr)
		}
	}
	return syncDirectory(root, ops)
}
