package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const completionReceiptRetention = 720 * time.Hour

// CompletionMarkerRecovery reports one markerless receipt considered during
// startup recovery.
type CompletionMarkerRecovery struct {
	QueueID   string
	ReceiptID string
	Marker    *CompletionReleaseMarker
	Err       error
}

// PrepareCompletionReleaseMarker fixes the immutable retention record from
// receipt-bound inputs and a trusted time sampled after ownership release.
func PrepareCompletionReleaseMarker(
	inputs CompletionReleaseMarkerInputs,
	releasedAt time.Time,
) (CompletionReleaseMarker, []byte, error) {
	if err := validateCompletionReleaseMarkerInputs(inputs); err != nil {
		return CompletionReleaseMarker{}, nil, err
	}
	releasedAt = releasedAt.UTC().Truncate(time.Millisecond)
	gcNotBefore := releasedAt.Add(completionReceiptRetention)
	marker := CompletionReleaseMarker{
		RecordType:           "completion-release",
		SchemaVersion:        1,
		QueueID:              inputs.QueueID,
		ReceiptID:            inputs.ReceiptID,
		TransactionID:        inputs.TransactionID,
		ReceiptSHA256:        inputs.ReceiptSHA256,
		CompletedQueueSHA256: inputs.CompletedQueueSHA256,
		ReleasedAt:           releasedAt.Format("2006-01-02T15:04:05.000Z"),
		GCNotBefore:          gcNotBefore.Format("2006-01-02T15:04:05.000Z"),
	}
	if err := validateCompletionReleaseMarker(marker); err != nil {
		return CompletionReleaseMarker{}, nil, err
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return CompletionReleaseMarker{}, nil, fmt.Errorf("marshal completion release marker: %w", err)
	}
	return marker, data, nil
}

func validateCompletionReleaseMarkerInputs(inputs CompletionReleaseMarkerInputs) error {
	if validateUUIDv7(inputs.QueueID) != nil ||
		validateUUIDv7(inputs.ReceiptID) != nil ||
		validateUUIDv7(inputs.TransactionID) != nil ||
		!validSHA256(inputs.ReceiptSHA256) ||
		!validSHA256(inputs.CompletedQueueSHA256) {
		return errors.New("invalid completion release marker inputs")
	}
	return nil
}

// InstallCompletionReleaseMarker installs one marker without replacement. A
// valid existing marker bound to the same receipt wins unchanged.
func InstallCompletionReleaseMarker(
	projectDir string,
	inputs CompletionReleaseMarkerInputs,
	releasedAt time.Time,
) (CompletionReleaseMarker, error) {
	return installCompletionReleaseMarker(projectDir, inputs, releasedAt, osNamespaceOps())
}

func installCompletionReleaseMarker(
	projectDir string,
	inputs CompletionReleaseMarkerInputs,
	releasedAt time.Time,
	ops namespaceOps,
) (CompletionReleaseMarker, error) {
	marker, data, err := PrepareCompletionReleaseMarker(inputs, releasedAt)
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	root := completionReceiptsDir(projectDir)
	if err := validateCompletionMarkerReceipt(root, inputs, ops); err != nil {
		return CompletionReleaseMarker{}, err
	}
	basename, err := CompletionReleaseMarkerBasename(inputs.QueueID, inputs.ReceiptID)
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	path := filepath.Join(root, basename)
	markerInfo, statErr := ops.lstat(path)
	if statErr == nil && !markerInfo.Mode().IsRegular() {
		return CompletionReleaseMarker{}, errors.New("completion release marker is not a regular file")
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return CompletionReleaseMarker{}, statErr
	}
	existing, present, err := readOptional(path, ops)
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	if present {
		accepted, acceptErr := acceptExistingCompletionReleaseMarker(existing, inputs)
		if acceptErr != nil {
			return CompletionReleaseMarker{}, acceptErr
		}
		if err := syncDirectory(root, ops); err != nil {
			return CompletionReleaseMarker{}, err
		}
		return accepted, nil
	}
	install := durableNoReplace(path, data, ops)
	if install.State != noReplaceInstalled {
		if install.Err != nil {
			return CompletionReleaseMarker{}, install.Err
		}
		return CompletionReleaseMarker{}, errors.New("completion release marker was not installed")
	}
	if err := syncDirectory(root, ops); err != nil {
		return CompletionReleaseMarker{}, err
	}
	return marker, nil
}

func validateCompletionMarkerReceipt(root string, inputs CompletionReleaseMarkerInputs, ops namespaceOps) error {
	rootInfo, err := ops.lstat(root)
	if err != nil {
		return fmt.Errorf("inspect completion receipt root for marker: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("completion receipt root is not a real directory")
	}
	receiptBasename, err := CompletionReceiptBasename(inputs.QueueID, inputs.ReceiptID)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(root, receiptBasename)
	receiptInfo, err := ops.lstat(receiptPath)
	if err != nil {
		return fmt.Errorf("inspect completion receipt for marker: %w", err)
	}
	if !receiptInfo.Mode().IsRegular() {
		return errors.New("completion receipt for marker is not a regular file")
	}
	data, err := ops.readFile(receiptPath)
	if err != nil {
		return fmt.Errorf("read completion receipt for marker: %w", err)
	}
	receipt, err := DecodeCompletionReceipt(data)
	if err != nil {
		return err
	}
	if receipt.QueueID != inputs.QueueID || receipt.ReceiptID != inputs.ReceiptID ||
		receipt.TransactionID != inputs.TransactionID || digestHex(data) != inputs.ReceiptSHA256 ||
		receipt.CompletedQueueSHA256 != inputs.CompletedQueueSHA256 {
		return errors.New("completion release marker inputs do not match receipt")
	}
	return nil
}

func acceptExistingCompletionReleaseMarker(
	data []byte,
	inputs CompletionReleaseMarkerInputs,
) (CompletionReleaseMarker, error) {
	marker, err := DecodeCompletionReleaseMarker(data)
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	if marker.QueueID != inputs.QueueID || marker.ReceiptID != inputs.ReceiptID ||
		marker.TransactionID != inputs.TransactionID || marker.ReceiptSHA256 != inputs.ReceiptSHA256 ||
		marker.CompletedQueueSHA256 != inputs.CompletedQueueSHA256 {
		return CompletionReleaseMarker{}, errors.New("completion release marker conflicts with receipt")
	}
	return marker, nil
}

// RecoverCompletionReleaseMarkers installs markers for receipts whose old
// queue identity and final intent are both absent. Callers run this before any
// in-memory queue owner is installed.
func RecoverCompletionReleaseMarkers(
	projectDir string,
	releaseTime func() time.Time,
) ([]CompletionMarkerRecovery, error) {
	if releaseTime == nil {
		return nil, errors.New("completion marker recovery requires a release time source")
	}
	root := completionReceiptsDir(projectDir)
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("completion receipt root is not a real directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	results := make([]CompletionMarkerRecovery, 0)
	for _, entry := range entries {
		queueID, receiptID, selected := completionReceiptIdentityFromName(entry.Name())
		if !selected {
			continue
		}
		result := CompletionMarkerRecovery{QueueID: queueID, ReceiptID: receiptID}
		marker, recoverErr := recoverOneCompletionReleaseMarker(projectDir, queueID, receiptID, releaseTime)
		if recoverErr != nil {
			result.Err = recoverErr
		} else if marker != (CompletionReleaseMarker{}) {
			result.Marker = &marker
		}
		results = append(results, result)
	}
	return results, nil
}

func completionReceiptIdentityFromName(name string) (string, string, bool) {
	if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".release-v1.json") ||
		strings.Contains(name, ".tmp-") {
		return "", "", false
	}
	identity := strings.TrimSuffix(name, ".json")
	parts := strings.Split(identity, "--")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func recoverOneCompletionReleaseMarker(
	projectDir, queueID, receiptID string,
	releaseTime func() time.Time,
) (CompletionReleaseMarker, error) {
	receipt, found, err := ReadCompletionReceiptForStatus(projectDir, queueID, receiptID)
	if err != nil || !found {
		if err != nil {
			return CompletionReleaseMarker{}, err
		}
		return CompletionReleaseMarker{}, errors.New("completion receipt disappeared during marker recovery")
	}
	markerBasename, err := CompletionReleaseMarkerBasename(queueID, receiptID)
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	markerPath := filepath.Join(completionReceiptsDir(projectDir), markerBasename)
	if _, err := os.Lstat(markerPath); err == nil {
		return CompletionReleaseMarker{}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return CompletionReleaseMarker{}, err
	}
	if _, present, err := readOptional(replaceIntentPath(projectDir, receipt.NormalizedName), osNamespaceOps()); err != nil {
		return CompletionReleaseMarker{}, err
	} else if present {
		return CompletionReleaseMarker{}, errors.New("completion intent still owns the receipt")
	}
	canonicalPath := queuePath(projectDir, receipt.NormalizedName)
	canonicalInfo, statErr := os.Lstat(canonicalPath)
	present := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return CompletionReleaseMarker{}, statErr
	}
	if present && !canonicalInfo.Mode().IsRegular() {
		return CompletionReleaseMarker{}, errors.New("canonical queue is not a regular file")
	}
	if present {
		canonical, readErr := os.ReadFile(canonicalPath) //nolint:gosec // validated receipt name selects the exact canonical
		if readErr != nil {
			return CompletionReleaseMarker{}, readErr
		}
		live, decodeErr := UnmarshalQueue(canonical)
		if decodeErr != nil {
			return CompletionReleaseMarker{}, fmt.Errorf("classify completion marker canonical: %w", decodeErr)
		}
		if validateUUIDv7(live.QueueID) != nil || live.Name != receipt.NormalizedName ||
			NormaliseQueueName(live.Name) != live.Name {
			return CompletionReleaseMarker{}, errors.New("canonical queue has invalid identity")
		}
		if live.QueueID == receipt.QueueID {
			return CompletionReleaseMarker{}, errors.New("completed queue identity still owns its canonical name")
		}
	}
	receiptBytes, err := os.ReadFile(filepath.Join(completionReceiptsDir(projectDir), receipt.QueueID+"--"+receipt.ReceiptID+".json")) //nolint:gosec // decoded canonical IDs select the path
	if err != nil {
		return CompletionReleaseMarker{}, err
	}
	inputs := CompletionReleaseMarkerInputs{
		QueueID:              receipt.QueueID,
		ReceiptID:            receipt.ReceiptID,
		TransactionID:        receipt.TransactionID,
		ReceiptSHA256:        digestHex(receiptBytes),
		CompletedQueueSHA256: receipt.CompletedQueueSHA256,
	}
	return InstallCompletionReleaseMarker(projectDir, inputs, releaseTime())
}
