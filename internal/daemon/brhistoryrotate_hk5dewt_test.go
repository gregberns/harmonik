package daemon

// brhistoryrotate_hk5dewt_test.go — unit tests for the .br_history/ rotation
// pre-flight helper (hk-5dewt).
//
// Test helper prefix: brHist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// brHistMakeHistoryDir creates <projectDir>/.beads/.br_history/ and returns
// projectDir.  projectDir is rooted under t.TempDir().
func brHistMakeHistoryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	historyDir := filepath.Join(dir, ".beads", ".br_history")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("brHistMakeHistoryDir: mkdir: %v", err)
	}
	return dir
}

// brHistPopulate creates n fake snapshot files in <projectDir>/.beads/.br_history/,
// assigning distinct mtimes spaced 1 second apart (oldest first).  Returns the
// history dir path.
func brHistPopulate(t *testing.T, projectDir string, n int) string {
	t.Helper()
	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	base := time.Now().Add(-time.Duration(n) * time.Second)
	for i := range n {
		name := filepath.Join(historyDir, fmt.Sprintf("snapshot%d.json", i))
		//nolint:gosec // G304: path constructed from t.TempDir(); not user input
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("brHistPopulate: create %s: %v", name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("brHistPopulate: close %s: %v", name, err)
		}
		// Stagger mtime so sort order is deterministic.
		mtime := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(name, mtime, mtime); err != nil {
			t.Fatalf("brHistPopulate: chtimes %s: %v", name, err)
		}
	}
	return historyDir
}

// brHistCountDir returns the number of entries in dir, failing the test on error.
func brHistCountDir(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("brHistCountDir: ReadDir %s: %v", dir, err)
	}
	return len(entries)
}

// TestBrHistoryRotation_SkipWhenDirAbsent verifies that a missing .br_history
// dir causes the function to return nil without creating anything.
func TestBrHistoryRotation_SkipWhenDirAbsent(t *testing.T) {
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := runBrHistoryRotationPreflight(context.Background(), dir, brHistoryRotationDefaultKeep); err != nil {
		t.Fatalf("expected nil when dir absent, got: %v", err)
	}
	// .br_history-archive must not have been created.
	archiveDir := filepath.Join(dir, ".beads", ".br_history-archive")
	if _, statErr := os.Stat(archiveDir); !os.IsNotExist(statErr) {
		t.Errorf("archive dir should not exist when history dir absent; stat err: %v", statErr)
	}
}

// TestBrHistoryRotation_SkipWhenWithinLimit verifies that <= keepLatest entries
// causes a no-op (no archive dir created, entry count unchanged).
func TestBrHistoryRotation_SkipWhenWithinLimit(t *testing.T) {
	projectDir := brHistMakeHistoryDir(t)
	brHistPopulate(t, projectDir, 20) // exactly at the keep limit

	if err := runBrHistoryRotationPreflight(context.Background(), projectDir, 20); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	archiveDir := filepath.Join(projectDir, ".beads", ".br_history-archive")

	if got := brHistCountDir(t, historyDir); got != 20 {
		t.Errorf("history dir count = %d, want 20", got)
	}
	if _, statErr := os.Stat(archiveDir); !os.IsNotExist(statErr) {
		t.Errorf("archive dir should not exist when within limit; stat err: %v", statErr)
	}
}

// TestBrHistoryRotation_ArchivesOldEntries is the primary correctness test.
// It creates 30 fake history files with distinct mtimes, calls the helper with
// keepLatest=20, and asserts that:
//   - 10 entries are moved to .br_history-archive/
//   - 20 entries remain in .br_history/
//   - The 20 *newest* entries are the ones retained
func TestBrHistoryRotation_ArchivesOldEntries(t *testing.T) {
	const total = 30
	const keep = 20
	const wantArchived = total - keep

	projectDir := brHistMakeHistoryDir(t)
	historyDir := brHistPopulate(t, projectDir, total)
	archiveDir := filepath.Join(projectDir, ".beads", ".br_history-archive")

	if err := runBrHistoryRotationPreflight(context.Background(), projectDir, keep); err != nil {
		t.Fatalf("runBrHistoryRotationPreflight returned error: %v", err)
	}

	remaining := brHistCountDir(t, historyDir)
	if remaining != keep {
		t.Errorf("history dir has %d entries after rotation, want %d", remaining, keep)
	}

	archived := brHistCountDir(t, archiveDir)
	if archived != wantArchived {
		t.Errorf("archive dir has %d entries, want %d", archived, wantArchived)
	}

	// Verify that the archived entries are the 10 *oldest* (lowest mtime index).
	// brHistPopulate names files snapshot0.json … snapshot29.json with index 0
	// being oldest. After rotation snapshot0–snapshot9 must be in the archive dir.
	archiveEntries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archive: %v", err)
	}
	for _, e := range archiveEntries {
		// Each archive entry is named like "snapshot0.json.archived-<ts>".
		// The original name is everything before ".archived-".
		original := strings.SplitN(e.Name(), ".archived-", 2)[0]
		// Original names with index 0–9 are the oldest.
		var idx int
		if _, scanErr := fmt.Sscanf(original, "snapshot%d.json", &idx); scanErr != nil {
			t.Errorf("unexpected archive entry name %q", e.Name())
			continue
		}
		if idx >= keep {
			t.Errorf("archived entry %q has index %d >= keep %d (should have been retained)", e.Name(), idx, keep)
		}
	}
}

// TestBrHistoryRotation_IdempotentAfterRotation verifies that running the
// pre-flight twice on an already-rotated directory (≤20 entries) is a no-op.
func TestBrHistoryRotation_IdempotentAfterRotation(t *testing.T) {
	const keep = 20

	projectDir := brHistMakeHistoryDir(t)
	brHistPopulate(t, projectDir, 30)

	// First rotation: archives 10 entries.
	if err := runBrHistoryRotationPreflight(context.Background(), projectDir, keep); err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	// Second rotation: should be no-op (20 entries remain, at limit).
	if err := runBrHistoryRotationPreflight(context.Background(), projectDir, keep); err != nil {
		t.Fatalf("second rotation: %v", err)
	}

	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	if got := brHistCountDir(t, historyDir); got != keep {
		t.Errorf("after second rotation history dir has %d entries, want %d", got, keep)
	}
}
