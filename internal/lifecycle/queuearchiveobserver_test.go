package lifecycle_test

// queuearchiveobserver_test.go — claims defended:
//
//   - The observer never removes an archive, whatever the retention setting.
//   - With no retention number set, the observer says so and marks nothing
//     over retention. It does NOT quietly apply a number of its own.
//   - With an operator-set number, the count over retention is per queue and
//     names the oldest archives first.
//   - Age comes from the injected clock, not from the wall clock.
//
// Every fixture is produced by queue.ArchiveFailedQueue, the real writer.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

func observerFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir .harmonik/queues: %v", err)
	}
	return dir
}

// archiveViaRealWriter creates a live queue file and archives it through
// queue.ArchiveFailedQueue, then forces the archive's mtime so age and
// ordering are deterministic.
func archiveViaRealWriter(t *testing.T, projectDir, queueName string, at time.Time) string {
	t.Helper()
	live := filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")
	if err := os.WriteFile(live, []byte(`{"queue_id":"fixture"}`), 0o600); err != nil {
		t.Fatalf("write live queue file: %v", err)
	}
	path, err := queue.ArchiveFailedQueue(context.Background(), projectDir, queueName, at)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue: %v", err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("chtimes %q: %v", path, err)
	}
	return path
}

func noEnv(string) string { return "" }

// TestObserveQueueArchives_RemovesNothingWhenRetentionIsSet is the load-bearing
// claim. Even with an operator-set retention number and archives far beyond it,
// every file is still on disk when the observer returns. The daemon reports;
// the operator or the captain removes.
func TestObserveQueueArchives_RemovesNothingWhenRetentionIsSet(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	var written []string
	for i := 0; i < 8; i++ {
		written = append(written, archiveViaRealWriter(t, dir, "main", base.Add(time.Duration(i)*time.Hour)))
	}

	keep := 2
	report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
		Now:          base.Add(24 * time.Hour),
		KeepPerQueue: &keep,
		Getenv:       noEnv,
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.OverRetention != 6 {
		t.Errorf("OverRetention = %d; want 6 (8 archives, keep 2)", report.OverRetention)
	}
	for _, path := range written {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("archive %q was removed; ObserveQueueArchives must not delete: %v", path, statErr)
		}
	}
}

// TestObserveQueueArchives_NoRetentionSetMeansNothingIsOver asserts the
// observer is honest about an absent policy. There is no compiled-in five.
// Nothing is over a limit that nobody has chosen.
func TestObserveQueueArchives_NoRetentionSetMeansNothingIsOver(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		archiveViaRealWriter(t, dir, "main", base.Add(time.Duration(i)*time.Hour))
	}

	report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
		Now:    base.Add(48 * time.Hour),
		Getenv: noEnv,
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.Count != 20 {
		t.Fatalf("Count = %d; want 20", report.Count)
	}
	if report.RetentionConfigured {
		t.Error("RetentionConfigured = true with no operator setting; want false")
	}
	if report.RetentionKeep != 0 {
		t.Errorf("RetentionKeep = %d; want 0 — the observer must not invent a number", report.RetentionKeep)
	}
	if report.OverRetention != 0 || len(report.OverRetentionPaths) != 0 {
		t.Errorf("OverRetention = %d / %v; want 0 with no retention set", report.OverRetention, report.OverRetentionPaths)
	}
}

// TestObserveQueueArchives_OperatorNumberComesFromTheEnvironment asserts the
// operator-set number is read from HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT and that
// the over-retention list is per queue, oldest first.
func TestObserveQueueArchives_OperatorNumberComesFromTheEnvironment(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	mainOldest := archiveViaRealWriter(t, dir, "main", base)
	mainMiddle := archiveViaRealWriter(t, dir, "main", base.Add(time.Hour))
	archiveViaRealWriter(t, dir, "main", base.Add(2*time.Hour))
	// crew-paul has one archive, under any retention of 1 or more.
	archiveViaRealWriter(t, dir, "crew-paul", base.Add(3*time.Hour))

	report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
		Now: base.Add(10 * time.Hour),
		Getenv: func(k string) string {
			if k == lifecycle.EnvQueueArchiveKeepCount {
				return "1"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if !report.RetentionConfigured || report.RetentionKeep != 1 {
		t.Fatalf("RetentionConfigured/Keep = %t/%d; want true/1", report.RetentionConfigured, report.RetentionKeep)
	}
	want := []string{mainOldest, mainMiddle}
	if len(report.OverRetentionPaths) != len(want) {
		t.Fatalf("OverRetentionPaths = %v; want %v", report.OverRetentionPaths, want)
	}
	for i := range want {
		if report.OverRetentionPaths[i] != want[i] {
			t.Fatalf("OverRetentionPaths = %v; want %v (oldest first, per queue)", report.OverRetentionPaths, want)
		}
	}
}

// TestObserveQueueArchives_UnparseableRetentionIsTreatedAsUnset asserts a bad
// setting does not silently become a number. An operator who typed something
// wrong gets "no policy", not a guess.
func TestObserveQueueArchives_UnparseableRetentionIsTreatedAsUnset(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		archiveViaRealWriter(t, dir, "main", base.Add(time.Duration(i)*time.Hour))
	}

	for _, bad := range []string{"lots", "0", "-3", " "} {
		report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
			Now: base.Add(10 * time.Hour),
			Getenv: func(k string) string {
				if k == lifecycle.EnvQueueArchiveKeepCount {
					return bad
				}
				return ""
			},
		})
		if err != nil {
			t.Fatalf("ObserveQueueArchives(%q): %v", bad, err)
		}
		if report.RetentionConfigured {
			t.Errorf("%s=%q was accepted as a retention policy; want it treated as unset",
				lifecycle.EnvQueueArchiveKeepCount, bad)
		}
	}
}

// TestObserveQueueArchives_AgeComesFromTheInjectedClock asserts the observer
// measures age against the caller's clock rather than reading time.Now.
func TestObserveQueueArchives_AgeComesFromTheInjectedClock(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	archiveViaRealWriter(t, dir, "main", base)
	archiveViaRealWriter(t, dir, "main", base.Add(3*time.Hour))

	report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
		Now:    base.Add(10 * time.Hour),
		Getenv: noEnv,
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.OldestAge != 10*time.Hour {
		t.Errorf("OldestAge = %v; want 10h measured from the injected clock", report.OldestAge)
	}
	if report.NewestAge != 7*time.Hour {
		t.Errorf("NewestAge = %v; want 7h measured from the injected clock", report.NewestAge)
	}
}

// TestObserveQueueArchives_ZeroNowIsAnError asserts a caller cannot get a
// silent time.Now() fallback by forgetting the clock.
func TestObserveQueueArchives_ZeroNowIsAnError(t *testing.T) {
	dir := observerFixtureDir(t)
	if _, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{Getenv: noEnv}); err == nil {
		t.Error("ObserveQueueArchives with a zero Now returned nil error; want a required-clock error")
	}
}

// TestObserveQueueArchives_MissingQueuesDirIsNotAFault asserts an
// uninitialised project reports zero archives rather than an error.
func TestObserveQueueArchives_MissingQueuesDirIsNotAFault(t *testing.T) {
	report, err := lifecycle.ObserveQueueArchives(t.TempDir(), lifecycle.ObserveQueueArchivesConfig{
		Now:    time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Getenv: noEnv,
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives on a project with no .harmonik/queues: %v", err)
	}
	if report.Count != 0 || report.TotalBytes != 0 {
		t.Errorf("report = %+v; want an empty report", report)
	}
}

// TestObserveQueueArchives_TotalBytesSumsTheRealFiles asserts the size figure
// is measured, not assumed.
func TestObserveQueueArchives_TotalBytesSumsTheRealFiles(t *testing.T) {
	dir := observerFixtureDir(t)
	base := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	a := archiveViaRealWriter(t, dir, "main", base)
	b := archiveViaRealWriter(t, dir, "crew-paul", base.Add(time.Hour))

	var want int64
	for _, p := range []string{a, b} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %q: %v", p, err)
		}
		want += info.Size()
	}

	report, err := lifecycle.ObserveQueueArchives(dir, lifecycle.ObserveQueueArchivesConfig{
		Now:    base.Add(5 * time.Hour),
		Getenv: noEnv,
	})
	if err != nil {
		t.Fatalf("ObserveQueueArchives: %v", err)
	}
	if report.TotalBytes != want {
		t.Errorf("TotalBytes = %d; want %d", report.TotalBytes, want)
	}
}
