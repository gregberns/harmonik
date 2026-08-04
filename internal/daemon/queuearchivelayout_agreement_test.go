package daemon

// queuearchivelayout_agreement_test.go — the three readers of the failed-queue
// archive layout must agree with the writer.
//
// Why this file exists: the layout used to be spelled by hand in four places.
// The boot sweep's spelling was wrong for its whole life and nothing caught it,
// because its own test built a fixture that matched the sweep's broken pattern
// instead of calling the real writer. Every fixture here is produced by
// [queue.ArchiveFailedQueue], the function the daemon actually uses, so a
// reader that drifts from the writer fails here.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// writeRealFailedArchive creates a live queue file for queueName and then
// archives it through the REAL archive writer. It returns the archive path the
// writer chose. Nothing here hand-builds an archive name.
func writeRealFailedArchive(t *testing.T, projectDir, queueName string, at time.Time) string {
	t.Helper()

	live := filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")
	if err := os.WriteFile(live, []byte(`{"queue_id":"fixture"}`), 0o600); err != nil {
		t.Fatalf("write live queue file %q: %v", live, err)
	}
	archivePath, err := queue.ArchiveFailedQueue(context.Background(), projectDir, queueName, at)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue(%q): %v", queueName, err)
	}
	if archivePath == "" {
		t.Fatalf("ArchiveFailedQueue(%q) returned an empty path; the live file was not archived", queueName)
	}
	if _, statErr := os.Stat(archivePath); statErr != nil {
		t.Fatalf("archive %q does not exist after ArchiveFailedQueue: %v", archivePath, statErr)
	}
	return archivePath
}

// TestFailedArchiveLayout_AllThreeReadersFindWhatTheWriterWrote is the
// anti-drift test. It writes archives with the real writer and asserts that the
// disk-snapshot reader, the drain detector, and the boot-time observer all
// return exactly that set.
//
// This test fails if any one reader is changed to look somewhere else — for
// example back at `.harmonik/queue.json.failed-*`, the spelling the old boot
// sweep used, which matched nothing on a real store.
func TestFailedArchiveLayout_AllThreeReadersFindWhatTheWriterWrote(t *testing.T) {
	projectDir := emptyTestProjectDir(t)
	at := time.Date(2026, 5, 19, 16, 14, 37, 0, time.UTC)

	want := []string{
		writeRealFailedArchive(t, projectDir, "main", at),
		writeRealFailedArchive(t, projectDir, "crew-paul", at.Add(time.Minute)),
	}
	sort.Strings(want)

	// Reader 1 — the daemon-down disk snapshot.
	gotDisk, err := diskFailedArchives(projectDir)
	if err != nil {
		t.Fatalf("diskFailedArchives: %v", err)
	}
	assertSamePaths(t, "diskFailedArchives", gotDisk, want)

	// Reader 2 — the drain detector's defense #3.
	d := NewDrainDetector(drainedReady(), drainedLister(), drainedLedger(), NewRunRegistry(), queuewiring.NewQueueStore(), projectDir)
	gotDrain, err := d.failedArchives()
	if err != nil {
		t.Fatalf("DrainDetector.failedArchives: %v", err)
	}
	assertSamePaths(t, "DrainDetector.failedArchives", gotDrain, want)

	// Reader 3 — the boot-time observer.
	report, err := lifecycle.ObserveQueueArchives(projectDir, lifecycle.ObserveQueueArchivesConfig{
		Now:    at.Add(time.Hour),
		Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	gotObserver := make([]string, 0, len(report.Archives))
	for _, a := range report.Archives {
		gotObserver = append(gotObserver, a.Path)
	}
	assertSamePaths(t, "ObserveQueueArchives", gotObserver, want)
}

// TestFailedArchiveLayout_ObserverIgnoresTheOldBrokenLocation pins the specific
// regression. A file at the location the retired boot sweep scanned
// (.harmonik/queue.json.failed-<ts>) is NOT an archive. If a reader is
// "repaired" by pointing it back there, it starts counting this decoy and the
// test fails.
func TestFailedArchiveLayout_ObserverIgnoresTheOldBrokenLocation(t *testing.T) {
	projectDir := emptyTestProjectDir(t)
	at := time.Date(2026, 5, 19, 16, 14, 37, 0, time.UTC)

	decoy := filepath.Join(projectDir, ".harmonik", "queue.json.failed-20260519161437")
	if err := os.WriteFile(decoy, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write decoy: %v", err)
	}
	genuine := writeRealFailedArchive(t, projectDir, "main", at)

	report, err := lifecycle.ObserveQueueArchives(projectDir, lifecycle.ObserveQueueArchivesConfig{
		Now:    at.Add(time.Hour),
		Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.Count != 1 {
		t.Fatalf("Count = %d; want 1 (only the real archive)", report.Count)
	}
	if report.Archives[0].Path != genuine {
		t.Errorf("observed %q; want the real archive %q", report.Archives[0].Path, genuine)
	}

	gotDisk, err := diskFailedArchives(projectDir)
	if err != nil {
		t.Fatalf("diskFailedArchives: %v", err)
	}
	assertSamePaths(t, "diskFailedArchives", gotDisk, []string{genuine})
}

// TestFailedArchiveLayout_QueueNameSurvivesTheRoundTrip asserts the observer
// attributes each archive to the queue that produced it. Retention is counted
// per queue, so a wrong name silently mis-groups the report.
func TestFailedArchiveLayout_QueueNameSurvivesTheRoundTrip(t *testing.T) {
	projectDir := emptyTestProjectDir(t)
	at := time.Date(2026, 5, 19, 16, 14, 37, 0, time.UTC)

	writeRealFailedArchive(t, projectDir, "crew-paul", at)

	report, err := lifecycle.ObserveQueueArchives(projectDir, lifecycle.ObserveQueueArchivesConfig{
		Now:    at.Add(time.Hour),
		Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.Count != 1 {
		t.Fatalf("Count = %d; want 1", report.Count)
	}
	if got := report.Archives[0].QueueName; got != "crew-paul" {
		t.Errorf("QueueName = %q; want %q", got, "crew-paul")
	}
	if len(report.QueueNames) != 1 || report.QueueNames[0] != "crew-paul" {
		t.Errorf("QueueNames = %v; want [crew-paul]", report.QueueNames)
	}
}

// TestOrphanSweepResult_ReportsArchivesAndDeletesNone asserts the boot sweep's
// result carries the archive counts and that every archive is still on disk
// afterwards. The sweep observes. It must never delete.
//
// A retention number IS set here on purpose. Under an unset retention there is
// nothing to delete, so a test that leaves it unset would pass even if the
// observer had a deletion path. The number makes the deletion path reachable,
// which is the only way this assertion has any force.
func TestOrphanSweepResult_ReportsArchivesAndDeletesNone(t *testing.T) {
	projectDir := emptyTestProjectDir(t)
	at := time.Date(2026, 5, 19, 16, 14, 37, 0, time.UTC)

	names := []string{"main", "main", "main", "crew-paul"}
	written := make([]string, 0, len(names))
	for i, name := range names {
		written = append(written, writeRealFailedArchive(t, projectDir, name, at.Add(time.Duration(i)*time.Minute)))
	}

	keep := 1
	report, err := lifecycle.ObserveQueueArchives(projectDir, lifecycle.ObserveQueueArchivesConfig{
		Now:          at.Add(time.Hour),
		KeepPerQueue: &keep,
		Getenv:       func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.Count != len(written) {
		t.Errorf("Count = %d; want %d", report.Count, len(written))
	}
	if report.OverRetention != 2 {
		t.Fatalf("OverRetention = %d; want 2 (3 main archives, keep 1) — without candidates this test proves nothing", report.OverRetention)
	}
	for _, path := range written {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("archive %q was removed; the sweep must not delete: %v", path, statErr)
		}
	}
}

func assertSamePaths(t *testing.T, who string, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	sort.Strings(gotSorted)
	if len(gotSorted) != len(want) {
		t.Fatalf("%s returned %d archive(s) %v; want %d %v", who, len(gotSorted), gotSorted, len(want), want)
	}
	for i := range want {
		if gotSorted[i] != want[i] {
			t.Fatalf("%s returned %v; want %v", who, gotSorted, want)
		}
	}
}
