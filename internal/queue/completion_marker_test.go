package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallCompletionReleaseMarkerRootSyncFailureRetriesExactMarker(t *testing.T) {
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
	cut := errors.New("cut marker root sync")
	ops := osNamespaceOps()
	openDir := ops.openDir
	ops.openDir = func(path string) (*os.File, error) {
		if path == completionReceiptsDir(plan.ProjectDir) {
			return nil, cut
		}
		return openDir(path)
	}
	firstTime := completionFixtureTime().Add(time.Minute)
	if _, err := installCompletionReleaseMarker(plan.ProjectDir, prepared.MarkerInputs, firstTime, ops); !errors.Is(err, cut) {
		t.Fatalf("first install error = %v", err)
	}
	retried, err := InstallCompletionReleaseMarker(
		plan.ProjectDir,
		prepared.MarkerInputs,
		firstTime.Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ReleasedAt != "2026-08-10T18:13:13.456Z" {
		t.Fatalf("retry replaced first marker: %+v", retried)
	}
}

func TestInstallCompletionReleaseMarkerKeepsFirstDurableTime(t *testing.T) {
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
	firstTime := completionFixtureTime().Add(time.Minute)
	first, err := InstallCompletionReleaseMarker(plan.ProjectDir, prepared.MarkerInputs, firstTime)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InstallCompletionReleaseMarker(plan.ProjectDir, prepared.MarkerInputs, firstTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second != first || first.ReleasedAt != "2026-08-10T18:13:13.456Z" ||
		first.GCNotBefore != "2026-09-09T18:13:13.456Z" {
		t.Fatalf("markers = first=%+v second=%+v", first, second)
	}
	entries, err := os.ReadDir(completionReceiptsDir(plan.ProjectDir))
	if err != nil || len(entries) != 2 {
		t.Fatalf("receipt root entries = %d, err=%v", len(entries), err)
	}
}

func TestCompletionMarkerNoReplaceAmbiguityReloadsExactInstalledFacts(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	if err := writeCompletionReceipt(plan.ProjectDir, prepared.Receipt.QueueID, prepared.Receipt.TransactionID, prepared.Binding, osNamespaceOps()); err != nil {
		t.Fatal(err)
	}
	ops := osNamespaceOps()
	link := ops.link
	linkCalls := 0
	ops.link = func(oldPath, newPath string) error {
		linkCalls++
		if err := link(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("marker link reported ambiguity after install")
	}
	marker, err := installCompletionReleaseMarker(plan.ProjectDir, prepared.MarkerInputs, completionFixtureTime().Add(time.Minute), ops)
	if err != nil || marker.ReceiptID != prepared.Receipt.ReceiptID {
		t.Fatalf("marker = %+v, err=%v", marker, err)
	}
	if linkCalls != 1 {
		t.Fatalf("marker link calls = %d, want 1", linkCalls)
	}
	basename, err := CompletionReleaseMarkerBasename(prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(completionReceiptsDir(plan.ProjectDir), basename)) //nolint:gosec // path is under t.TempDir and uses validated IDs.
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCompletionReleaseMarker(data)
	if err != nil || decoded != marker {
		t.Fatalf("installed marker = %+v, err=%v", decoded, err)
	}
}

func TestCompletionMarkerNoReplaceAmbiguityPreservesConflictingInstalledFacts(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	if err := writeCompletionReceipt(plan.ProjectDir, prepared.Receipt.QueueID, prepared.Receipt.TransactionID, prepared.Binding, osNamespaceOps()); err != nil {
		t.Fatal(err)
	}
	ops := osNamespaceOps()
	link := ops.link
	conflict := []byte("conflicting installed marker")
	var target string
	ops.link = func(oldPath, newPath string) error {
		target = newPath
		if err := link(oldPath, newPath); err != nil {
			return err
		}
		if err := os.WriteFile(newPath, conflict, 0o600); err != nil {
			return err
		}
		return errors.New("marker link reported ambiguity with conflicting target")
	}
	if _, err := installCompletionReleaseMarker(plan.ProjectDir, prepared.MarkerInputs, completionFixtureTime().Add(time.Minute), ops); err == nil {
		t.Fatal("conflicting installed marker was accepted")
	}
	got, err := os.ReadFile(target) //nolint:gosec // target is captured from the t.TempDir-backed install.
	if err != nil || !bytes.Equal(got, conflict) {
		t.Fatalf("conflicting marker changed: %q, err=%v", got, err)
	}
}

func TestPrepareCompletionReleaseMarkerRejectsUnboundInput(t *testing.T) {
	prepared, err := PrepareCompletion(
		completionFixtureQueue(),
		completionTransactionID,
		completionReceiptID,
		completionFixtureTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared.MarkerInputs.ReceiptSHA256 = "wrong"
	if _, _, err := PrepareCompletionReleaseMarker(prepared.MarkerInputs, completionFixtureTime()); err == nil {
		t.Fatal("invalid marker input was accepted")
	}
}

func TestRecoverCompletionReleaseMarkersConsumesC09PendingHandoff(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	commit := WriteReplacement(t.Context(), plan)
	if !commit.Committed() {
		t.Fatalf("completion commit = %+v", commit)
	}
	replaceRecoveries, err := RecoverReplaceIntents(plan.ProjectDir)
	if err != nil || len(replaceRecoveries) != 1 || replaceRecoveries[0].Completion == nil ||
		!replaceRecoveries[0].Completion.ReleaseMarkerPending {
		t.Fatalf("replace recoveries = %+v, err=%v", replaceRecoveries, err)
	}
	markerRecoveries, err := RecoverCompletionReleaseMarkers(
		plan.ProjectDir,
		func() time.Time { return completionFixtureTime().Add(time.Minute) },
	)
	if err != nil || len(markerRecoveries) != 1 || markerRecoveries[0].Err != nil || markerRecoveries[0].Marker == nil {
		t.Fatalf("marker recoveries = %+v, err=%v", markerRecoveries, err)
	}
	if markerRecoveries[0].Marker.ReceiptID != prepared.Receipt.ReceiptID {
		t.Fatalf("marker = %+v", markerRecoveries[0].Marker)
	}
}

func TestRecoverCompletionReleaseMarkersFindsReceiptOnlyCrashState(t *testing.T) {
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
	timeCalls := 0
	recoveries, err := RecoverCompletionReleaseMarkers(plan.ProjectDir, func() time.Time {
		timeCalls++
		if _, statErr := os.Stat(queuePath(plan.ProjectDir, QueueNameMain)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("release time sampled before canonical absence: %v", statErr)
		}
		if _, statErr := os.Stat(replaceIntentPath(plan.ProjectDir, QueueNameMain)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("release time sampled before intent absence: %v", statErr)
		}
		return completionFixtureTime().Add(time.Minute)
	})
	if err != nil || len(recoveries) != 1 || recoveries[0].Err != nil || recoveries[0].Marker == nil {
		t.Fatalf("recoveries = %+v, err=%v", recoveries, err)
	}
	if timeCalls != 1 {
		t.Fatalf("release time calls = %d", timeCalls)
	}
}

func TestRecoverCompletionReleaseMarkersSamplesOnlyAfterOldIdentityReleases(t *testing.T) {
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
	if err := CleanupReplaceIntent(plan.ProjectDir, QueueNameMain); err != nil {
		t.Fatal(err)
	}
	timeCalls := 0
	recoveries, err := RecoverCompletionReleaseMarkers(plan.ProjectDir, func() time.Time {
		timeCalls++
		return completionFixtureTime().Add(time.Minute)
	})
	if err != nil || len(recoveries) != 1 || recoveries[0].Err == nil || timeCalls != 0 {
		t.Fatalf("owned recovery = %+v, calls=%d, err=%v", recoveries, timeCalls, err)
	}

	newQueue := completionFixtureQueue()
	newQueue.QueueID = "0197c452-0000-7000-8000-000000000099"
	if err := Persist(t.Context(), plan.ProjectDir, &newQueue); err != nil {
		t.Fatal(err)
	}
	recoveries, err = RecoverCompletionReleaseMarkers(plan.ProjectDir, func() time.Time {
		timeCalls++
		return completionFixtureTime().Add(time.Minute)
	})
	if err != nil || len(recoveries) != 1 || recoveries[0].Err != nil || recoveries[0].Marker == nil || timeCalls != 1 {
		t.Fatalf("reused-name recovery = %+v, calls=%d, err=%v", recoveries, timeCalls, err)
	}
}

func TestRecoverCompletionReleaseMarkersRejectsInvalidCanonicalIdentity(t *testing.T) {
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
	if err := CleanupReplaceIntent(plan.ProjectDir, QueueNameMain); err != nil {
		t.Fatal(err)
	}
	canonicalPath := queuePath(plan.ProjectDir, QueueNameMain)
	if err := os.WriteFile(canonicalPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	timeCalls := 0
	recoveries, err := RecoverCompletionReleaseMarkers(plan.ProjectDir, func() time.Time {
		timeCalls++
		return completionFixtureTime()
	})
	if err != nil || len(recoveries) != 1 || recoveries[0].Err == nil || timeCalls != 0 {
		t.Fatalf("recoveries = %+v, calls=%d, err=%v", recoveries, timeCalls, err)
	}
}

func TestRecoverCompletionReleaseMarkersRejectsCanonicalSymlink(t *testing.T) {
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
	if err := CleanupReplaceIntent(plan.ProjectDir, QueueNameMain); err != nil {
		t.Fatal(err)
	}
	canonicalPath := queuePath(plan.ProjectDir, QueueNameMain)
	if err := os.Remove(canonicalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(plan.ProjectDir, "outside.json"), canonicalPath); err != nil {
		t.Fatal(err)
	}
	timeCalls := 0
	recoveries, err := RecoverCompletionReleaseMarkers(plan.ProjectDir, func() time.Time {
		timeCalls++
		return completionFixtureTime()
	})
	if err != nil || len(recoveries) != 1 || recoveries[0].Err == nil || timeCalls != 0 {
		t.Fatalf("recoveries = %+v, calls=%d, err=%v", recoveries, timeCalls, err)
	}
}
