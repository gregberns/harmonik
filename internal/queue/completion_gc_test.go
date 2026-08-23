package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func parseCompletionGCTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", value)
	if err != nil {
		t.Fatalf("parse completion timestamp %q: %v", value, err)
	}
	return parsed
}

func completionMarkerBasename(t *testing.T, queueID, receiptID string) string {
	t.Helper()
	basename, err := CompletionReleaseMarkerBasename(queueID, receiptID)
	if err != nil {
		t.Fatalf("marker basename for %q/%q: %v", queueID, receiptID, err)
	}
	return basename
}

func completionGCFixture(t *testing.T) (string, CompletionPlan, CompletionReleaseMarker) {
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
	if err := CleanupReplaceIntent(plan.ProjectDir, QueueNameMain); err != nil {
		t.Fatal(err)
	}
	marker, err := InstallCompletionReleaseMarker(
		plan.ProjectDir,
		prepared.MarkerInputs,
		completionFixtureTime().Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	return plan.ProjectDir, prepared, marker
}

func TestCompletionReceiptGCEligibilityUsesOnlyTrustedRetentionTime(t *testing.T) {
	_, _, marker := completionGCFixture(t)
	releasedAt := parseCompletionGCTime(t, marker.ReleasedAt)
	gcNotBefore := parseCompletionGCTime(t, marker.GCNotBefore)
	for _, tc := range []struct {
		name         string
		now          time.Time
		synchronized bool
		regressed    bool
		want         bool
	}{
		{name: "unsynchronized", now: gcNotBefore.Add(time.Hour), synchronized: false},
		{name: "known regression after retention", now: gcNotBefore.Add(time.Hour), synchronized: true, regressed: true},
		{name: "regressed", now: releasedAt.Add(-time.Millisecond), synchronized: true},
		{name: "before retention", now: gcNotBefore.Add(-time.Millisecond), synchronized: true},
		{name: "at retention", now: gcNotBefore, synchronized: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CompletionReceiptGCEligible(marker, CompletionGCObservation{
				Now: tc.now, Synchronized: tc.synchronized, Regressed: tc.regressed,
			})
			if err != nil || got != tc.want {
				t.Fatalf("eligible = %v, err=%v", got, err)
			}
		})
	}
}

func TestGarbageCollectCompletionReceiptsRemovesReceiptBeforeMarker(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	results, err := GarbageCollectCompletionReceipts(projectDir, CompletionGCObservation{Now: now, Synchronized: true})
	if err != nil || len(results) != 1 || results[0].Err != nil ||
		!results[0].ReceiptRemoved || !results[0].MarkerRemoved ||
		results[0].Phase != CompletionGCPhaseMarkerAbsentDurable {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	root := completionReceiptsDir(projectDir)
	for _, basename := range []string{
		prepared.Binding.Basename,
		prepared.Receipt.QueueID + "--" + prepared.Receipt.ReceiptID + ".release-v1.json",
	} {
		if _, statErr := os.Stat(filepath.Join(root, basename)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("record %q remains: %v", basename, statErr)
		}
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		t.Fatalf("receipt root was removed: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsUnsynchronizedPerformsNoIO(t *testing.T) {
	projectDir := t.TempDir()
	root := completionReceiptsDir(projectDir)
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("wrong type"), 0o600); err != nil {
		t.Fatal(err)
	}
	results, err := GarbageCollectCompletionReceipts(projectDir, CompletionGCObservation{Now: time.Now()})
	if err != nil || len(results) != 0 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
}

func TestGarbageCollectCompletionReceiptsTrustedClockRejectsSymlinkRoot(t *testing.T) {
	projectDir := t.TempDir()
	root := completionReceiptsDir(projectDir)
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	externalPath := filepath.Join(external, completionQueueID+"--"+completionReceiptID+".json")
	want := []byte("external receipt bytes")
	if err := os.WriteFile(externalPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, root); err != nil {
		t.Fatal(err)
	}
	results, err := GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: completionFixtureTime().Add(1000 * time.Hour), Synchronized: true},
	)
	if err == nil || len(results) != 0 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	got, readErr := os.ReadFile(externalPath) //nolint:gosec // path is under t.TempDir
	if readErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("external bytes = %q, err=%v", got, readErr)
	}
}

func TestGarbageCollectCompletionReceiptsNeverCollectsUnreleasedReceipt(t *testing.T) {
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
	receiptPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
	want, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	results, err := GarbageCollectCompletionReceipts(
		plan.ProjectDir,
		CompletionGCObservation{Now: completionFixtureTime().Add(10000 * time.Hour), Synchronized: true},
	)
	if err != nil || len(results) != 0 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	got, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("unreleased receipt = %q, err=%v", got, err)
	}
}

func TestGarbageCollectCompletionReceiptsBeforeRetentionChangesNothing(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	root := completionReceiptsDir(projectDir)
	receiptPath := filepath.Join(root, prepared.Binding.Basename)
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	markerPath := filepath.Join(root, markerBase)
	wantReceipt, err := os.ReadFile(receiptPath) //nolint:gosec // paths are under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	wantMarker, err := os.ReadFile(markerPath) //nolint:gosec // paths are under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	gcNotBefore := parseCompletionGCTime(t, marker.GCNotBefore)
	results, err := GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: gcNotBefore.Add(-time.Millisecond), Synchronized: true},
	)
	if err != nil || len(results) != 1 || results[0].Err != nil ||
		results[0].Eligible || results[0].Phase != CompletionGCPhaseIneligible {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	gotReceipt, receiptErr := os.ReadFile(receiptPath) //nolint:gosec // paths are under t.TempDir
	gotMarker, markerErr := os.ReadFile(markerPath)    //nolint:gosec // paths are under t.TempDir
	if receiptErr != nil || markerErr != nil ||
		!bytes.Equal(gotReceipt, wantReceipt) || !bytes.Equal(gotMarker, wantMarker) {
		t.Fatalf("records changed: receipt_err=%v marker_err=%v", receiptErr, markerErr)
	}
}

func TestGarbageCollectCompletionReceiptsRetriesAfterReceiptSyncFailure(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	ops := osNamespaceOps()
	openDir := ops.openDir
	rootSyncs := 0
	cut := errors.New("cut receipt absence sync")
	ops.openDir = func(path string) (*os.File, error) {
		if path == completionReceiptsDir(projectDir) {
			rootSyncs++
			if rootSyncs == 2 {
				return nil, cut
			}
		}
		return openDir(path)
	}
	results, err := garbageCollectCompletionReceipts(projectDir, CompletionGCObservation{Now: now, Synchronized: true}, ops)
	if err != nil || len(results) != 1 || !errors.Is(results[0].Err, cut) {
		t.Fatalf("first results = %+v, err=%v", results, err)
	}
	if results[0].Phase != CompletionGCPhaseReceiptSyncIndeterminate {
		t.Fatalf("phase = %q", results[0].Phase)
	}
	root := completionReceiptsDir(projectDir)
	if _, statErr := os.Stat(filepath.Join(root, prepared.Binding.Basename)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("receipt removal was not observed: %v", statErr)
	}
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	if _, statErr := os.Stat(filepath.Join(root, markerBase)); statErr != nil {
		t.Fatalf("marker did not preserve retry state: %v", statErr)
	}
	results, err = GarbageCollectCompletionReceipts(projectDir, CompletionGCObservation{Now: now, Synchronized: true})
	if err != nil || len(results) != 1 || results[0].Err != nil || !results[0].MarkerRemoved {
		t.Fatalf("retry results = %+v, err=%v", results, err)
	}
}

func TestGarbageCollectCompletionReceiptsPreservesMismatchedPair(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	receiptPath := filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)
	data, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-2] ^= 1
	if err := os.WriteFile(receiptPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	results, err := GarbageCollectCompletionReceipts(projectDir, CompletionGCObservation{Now: now, Synchronized: true})
	if err != nil || len(results) != 1 || results[0].Err == nil || results[0].ReceiptRemoved || results[0].MarkerRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	if _, statErr := os.Stat(receiptPath); statErr != nil {
		t.Fatalf("mismatched receipt was removed: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsRemovesEligibleMarkerOnlyOrphan(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	root := completionReceiptsDir(projectDir)
	if err := os.Remove(filepath.Join(root, prepared.Binding.Basename)); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(root, osNamespaceOps()); err != nil {
		t.Fatal(err)
	}
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	results, err := GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
	)
	if err != nil || len(results) != 1 || results[0].Err != nil ||
		results[0].ReceiptRemoved || !results[0].MarkerRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
}

func TestGarbageCollectCompletionReceiptsMarkerSyncFailureIsRetryable(t *testing.T) {
	projectDir, _, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	ops := osNamespaceOps()
	openDir := ops.openDir
	rootSyncs := 0
	cut := errors.New("cut marker absence sync")
	ops.openDir = func(path string) (*os.File, error) {
		if path == completionReceiptsDir(projectDir) {
			rootSyncs++
			if rootSyncs == 3 {
				return nil, cut
			}
		}
		return openDir(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || !errors.Is(results[0].Err, cut) ||
		!results[0].ReceiptRemoved || results[0].MarkerRemoved ||
		results[0].Phase != CompletionGCPhaseMarkerSyncIndeterminate {
		t.Fatalf("first results = %+v, err=%v", results, err)
	}
	results, err = GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
	)
	if err != nil || len(results) != 0 {
		t.Fatalf("retry results = %+v, err=%v", results, err)
	}
	if _, statErr := os.Stat(completionReceiptsDir(projectDir)); statErr != nil {
		t.Fatalf("retry removed receipt root: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsMarkerUnlinkFailureKeepsReceiptAbsence(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	markerPath := filepath.Join(completionReceiptsDir(projectDir), markerBase)
	ops := osNamespaceOps()
	remove := ops.remove
	cut := errors.New("cut marker unlink")
	ops.remove = func(path string) error {
		if path == markerPath {
			return cut
		}
		return remove(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || !errors.Is(results[0].Err, cut) ||
		results[0].Phase != CompletionGCPhaseReceiptAbsentDurable || !results[0].ReceiptRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("marker did not retain retry state: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsReceiptUnlinkFailurePreservesPair(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	receiptPath := filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)
	ops := osNamespaceOps()
	remove := ops.remove
	cut := errors.New("cut receipt unlink")
	ops.remove = func(path string) error {
		if path == receiptPath {
			return cut
		}
		return remove(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || !errors.Is(results[0].Err, cut) ||
		results[0].Phase != CompletionGCPhasePreserved || results[0].ReceiptRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	if _, statErr := os.Stat(receiptPath); statErr != nil {
		t.Fatalf("receipt was removed: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsConvergesWhenReceiptUnlinkReturnsErrorAfterRemoval(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	receiptPath := filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)
	ops := osNamespaceOps()
	remove := ops.remove
	cut := errors.New("receipt unlink reported failure after removal")
	ops.remove = func(path string) error {
		if path == receiptPath {
			if err := remove(path); err != nil {
				return err
			}
			return cut
		}
		return remove(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || results[0].Err != nil ||
		!results[0].ReceiptRemoved || !results[0].MarkerRemoved ||
		results[0].Phase != CompletionGCPhaseMarkerAbsentDurable {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
}

func TestGarbageCollectCompletionReceiptsRefusesReceiptChangedAtDeletionCAS(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	receiptPath := filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)
	ops := osNamespaceOps()
	readFile := ops.readFile
	receiptReads := 0
	changed := []byte("changed receipt at deletion boundary")
	ops.readFile = func(path string) ([]byte, error) {
		if path == receiptPath {
			receiptReads++
			if receiptReads == 2 {
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					return nil, err
				}
				return append([]byte(nil), changed...), nil
			}
		}
		return readFile(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || results[0].Err == nil ||
		results[0].Phase != CompletionGCPhasePreserved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	got, readErr := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if readErr != nil || !bytes.Equal(got, changed) {
		t.Fatalf("changed receipt = %q, err=%v", got, readErr)
	}
}

func TestGarbageCollectCompletionReceiptsRefusesMarkerChangedAtDeletionCAS(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	markerPath := filepath.Join(completionReceiptsDir(projectDir), markerBase)
	ops := osNamespaceOps()
	readFile := ops.readFile
	markerReads := 0
	changed := []byte("changed marker at deletion boundary")
	ops.readFile = func(path string) ([]byte, error) {
		if path == markerPath {
			markerReads++
			if markerReads == 2 {
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					return nil, err
				}
				return append([]byte(nil), changed...), nil
			}
		}
		return readFile(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if err != nil || len(results) != 1 || results[0].Err == nil ||
		results[0].Phase != CompletionGCPhaseReceiptAbsentDurable || !results[0].ReceiptRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	got, readErr := os.ReadFile(markerPath) //nolint:gosec // path is under t.TempDir
	if readErr != nil || !bytes.Equal(got, changed) {
		t.Fatalf("changed marker = %q, err=%v", got, readErr)
	}
}

func TestGarbageCollectCompletionReceiptsInitialSyncFailureDeletesNothing(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	ops := osNamespaceOps()
	openDir := ops.openDir
	cut := errors.New("cut initial root sync")
	ops.openDir = func(path string) (*os.File, error) {
		if path == completionReceiptsDir(projectDir) {
			return nil, cut
		}
		return openDir(path)
	}
	results, err := garbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
		ops,
	)
	if !errors.Is(err, cut) || len(results) != 0 {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	if _, statErr := os.Stat(filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)); statErr != nil {
		t.Fatalf("receipt changed before initial sync: %v", statErr)
	}
}

func TestGarbageCollectCompletionReceiptsPreservesNonRegularMarker(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	markerPath := filepath.Join(completionReceiptsDir(projectDir), markerBase)
	if err := os.Remove(markerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(markerPath, 0o700); err != nil {
		t.Fatal(err)
	}
	results, err := GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
	)
	if err != nil || len(results) != 1 || results[0].Err == nil ||
		results[0].Phase != CompletionGCPhasePreserved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
}

func TestGarbageCollectCompletionReceiptsPreservesNonRegularReceiptAndMarker(t *testing.T) {
	projectDir, prepared, marker := completionGCFixture(t)
	receiptPath := filepath.Join(completionReceiptsDir(projectDir), prepared.Binding.Basename)
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(receiptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	now := parseCompletionGCTime(t, marker.GCNotBefore)
	results, err := GarbageCollectCompletionReceipts(
		projectDir,
		CompletionGCObservation{Now: now, Synchronized: true},
	)
	if err != nil || len(results) != 1 || results[0].Err == nil ||
		results[0].Phase != CompletionGCPhasePreserved || results[0].MarkerRemoved {
		t.Fatalf("results = %+v, err=%v", results, err)
	}
	markerBase := completionMarkerBasename(t, prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	if _, statErr := os.Stat(filepath.Join(completionReceiptsDir(projectDir), markerBase)); statErr != nil {
		t.Fatalf("marker was removed after non-regular receipt: %v", statErr)
	}
}
