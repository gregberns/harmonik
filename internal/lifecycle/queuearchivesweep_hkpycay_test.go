package lifecycle

// queuearchivesweep_hkpycay_test.go — unit tests for SweepQueueArchives.
//
// Helper prefix: archiveSweep (bead hk-pycay).

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// archiveSweepMakeHarmonikDir creates a .harmonik directory under a temp dir
// and returns the project dir path.
func archiveSweepMakeHarmonikDir(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("archiveSweepMakeHarmonikDir: mkdir: %v", err)
	}
	return projectDir
}

// archiveSweepCreateFile creates an empty file at .harmonik/<name> under projectDir.
func archiveSweepCreateFile(t *testing.T, projectDir, name string) {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", name)
	//nolint:gosec // G304: path constructed from t.TempDir() + .harmonik/ + test filename
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("archiveSweepCreateFile: create %q: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("archiveSweepCreateFile: close %q: %v", path, err)
	}
}

// archiveSweepListArchives returns the archive filenames present in
// .harmonik/ for the given projectDir, sorted.
func archiveSweepListArchives(t *testing.T, projectDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectDir, ".harmonik"))
	if err != nil {
		t.Fatalf("archiveSweepListArchives: ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > len("queue.json.") &&
			e.Name()[:len("queue.json.")] == "queue.json." {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSweepQueueArchives_KeepNewestFive verifies that when 10 archives of a
// single category are present, only the 5 newest are kept and the 5 oldest are
// deleted.
func TestSweepQueueArchives_KeepNewestFive(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	// Create 10 failed-* archives with lexicographically ordered timestamps.
	for i := range 10 {
		archiveSweepCreateFile(t, projectDir,
			// Timestamps: ...0001 through ...0010 — lex order == creation order.
			"queue.json.failed-2026051900000"+string(rune('0'+i+1)))
	}

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}
	if result.Deleted != 5 {
		t.Errorf("Deleted: got %d, want 5", result.Deleted)
	}
	if result.Retained != 5 {
		t.Errorf("Retained: got %d, want 5", result.Retained)
	}

	remaining := archiveSweepListArchives(t, projectDir)
	if len(remaining) != 5 {
		t.Fatalf("remaining archive count: got %d, want 5; files: %v", len(remaining), remaining)
	}
	// The 5 newest end with '6' through ...'0' (i.e. the last 5 in lex order).
	// Last character of each: 6, 7, 8, 9, : — but we used '0'+i+1 so i=5..9 →
	// characters '6'..'9' and ':'.  Accept any 5 that are the lexicographically
	// greatest.
	for _, name := range remaining {
		last := name[len(name)-1]
		if last < '6' {
			t.Errorf("retained archive %q looks older than expected (last char %q)", name, last)
		}
	}
}

// TestSweepQueueArchives_MultipleCategories verifies the sweep operates
// independently per category and that every category in the bead's list is
// handled.
func TestSweepQueueArchives_MultipleCategories(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	categories := []string{
		"failed",
		"cancelled",
		"panicked",
		"no-work",
		"silent-heartbeat",
		"pane-misdirect",
		"v49-stuck",
		"claude-crashed",
		"parallel-fail",
		"silent-after-runstarted",
	}

	// Create 10 archives for each category.
	for _, cat := range categories {
		for i := range 10 {
			archiveSweepCreateFile(t, projectDir,
				"queue.json."+cat+"-2026051900000"+string(rune('0'+i+1)))
		}
	}

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}

	wantDeleted := len(categories) * 5
	wantRetained := len(categories) * 5
	if result.Deleted != wantDeleted {
		t.Errorf("Deleted: got %d, want %d", result.Deleted, wantDeleted)
	}
	if result.Retained != wantRetained {
		t.Errorf("Retained: got %d, want %d", result.Retained, wantRetained)
	}

	remaining := archiveSweepListArchives(t, projectDir)
	if len(remaining) != wantRetained {
		t.Errorf("remaining count: got %d, want %d; files: %v", len(remaining), wantRetained, remaining)
	}
}

// TestSweepQueueArchives_FewerThanKeepCount verifies that when fewer archives
// exist than the keep count, none are deleted.
func TestSweepQueueArchives_FewerThanKeepCount(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	for i := range 3 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.failed-202605190000"+string(rune('0'+i+1)))
	}

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted: got %d, want 0", result.Deleted)
	}
	if result.Retained != 3 {
		t.Errorf("Retained: got %d, want 3", result.Retained)
	}
}

// TestSweepQueueArchives_NoHarmonikDir verifies that a missing .harmonik/
// directory returns zero counts and no error.
func TestSweepQueueArchives_NoHarmonikDir(t *testing.T) {
	projectDir := t.TempDir() // no .harmonik/ created

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if result.Deleted != 0 || result.Retained != 0 {
		t.Errorf("expected zero result for missing dir, got %+v", result)
	}
}

// TestSweepQueueArchives_NonArchiveFilesUntouched verifies that non-archive
// files in .harmonik/ (e.g. daemon.pid, daemon.sock, queue.json itself) are
// not touched.
func TestSweepQueueArchives_NonArchiveFilesUntouched(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	// Non-archive files.
	for _, name := range []string{"daemon.pid", "daemon.sock", "queue.json", "events"} {
		archiveSweepCreateFile(t, projectDir, name)
	}
	// One archive to exercise the sweep logic at all.
	archiveSweepCreateFile(t, projectDir, "queue.json.failed-20260519001647")

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 5})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted: got %d, want 0 (nothing old enough to delete)", result.Deleted)
	}

	// Non-archive files must still exist.
	for _, name := range []string{"daemon.pid", "daemon.sock", "queue.json"} {
		if _, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", name)); statErr != nil {
			t.Errorf("non-archive file %q was unexpectedly removed: %v", name, statErr)
		}
	}
}

// TestSweepQueueArchives_DefaultKeepCount verifies that when KeepCount is 0,
// the default of 5 is used.
func TestSweepQueueArchives_DefaultKeepCount(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	// Create 8 cancelled archives.
	for i := range 8 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.cancelled-202605190000"+string(rune('0'+i+1)))
	}

	// KeepCount == 0 → default 5.
	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}
	if result.Deleted != 3 {
		t.Errorf("Deleted: got %d, want 3", result.Deleted)
	}
	if result.Retained != 5 {
		t.Errorf("Retained: got %d, want 5", result.Retained)
	}
}

// TestSweepQueueArchives_EnvVarOverride verifies that
// HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT overrides the default keep count.
func TestSweepQueueArchives_EnvVarOverride(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	// Create 10 failed archives.
	for i := range 10 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.failed-2026051900000"+string(rune('0'+i+1)))
	}

	t.Setenv(queueArchiveEnvVar, "3")

	// KeepCount == 0 → env var → 3.
	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{})
	if err != nil {
		t.Fatalf("SweepQueueArchives: unexpected error: %v", err)
	}
	if result.Deleted != 7 {
		t.Errorf("Deleted: got %d, want 7", result.Deleted)
	}
	if result.Retained != 3 {
		t.Errorf("Retained: got %d, want 3", result.Retained)
	}
}

// TestArchiveCategory verifies the category extraction helper for all
// known naming patterns.
func TestArchiveCategory(t *testing.T) {
	cases := []struct {
		name    string
		wantCat string
	}{
		{"queue.json.failed-20260518223853", "failed"},
		{"queue.json.cancelled-20260519162812", "cancelled"},
		{"queue.json.panicked-20260518160205", "panicked"},
		{"queue.json.no-work-164935", "no-work"},
		{"queue.json.silent-heartbeat-20260519155547", "silent-heartbeat"},
		{"queue.json.pane-misdirect-20260519002522", "pane-misdirect"},
		{"queue.json.v49-stuck-20260519001647", "v49-stuck"},
		{"queue.json.claude-crashed-2026-05-20", "claude-crashed-2026-05"},
		{"queue.json.parallel-fail-2026-05-20", "parallel-fail-2026-05"},
		{"queue.json.silent-after-runstarted-20260520", "silent-after-runstarted"},
		// Edge: no hyphen in remainder → whole remainder is category.
		{"queue.json.backup", "backup"},
		// Not an archive file — should return "".
		{"queue.json", ""},
		{"daemon.pid", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := archiveCategory(tc.name)
			if got != tc.wantCat {
				t.Errorf("archiveCategory(%q) = %q, want %q", tc.name, got, tc.wantCat)
			}
		})
	}
}
