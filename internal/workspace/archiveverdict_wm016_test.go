package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Tests for ArchiveVerdict and ReviewVerdictArchivePath per
// workspace-model.md §4.7.WM-027a §(c) (bead hk-7om2q.16 / T-WM-016).
//
// Helper prefix: archiveVerdictFixture (distinct from reviewVerdictFixture
// used by the T-WM-015 tests in reviewverdict_wm027a_test.go).

// archiveVerdictFixtureMakeWorkspace creates a temp directory representing a
// workspace and ensures the .harmonik subdirectory exists.
func archiveVerdictFixtureMakeWorkspace(t *testing.T) string {
	t.Helper()
	workspacePath := t.TempDir()
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("archiveVerdictFixtureMakeWorkspace: MkdirAll: %v", err)
	}
	return workspacePath
}

// archiveVerdictFixtureWriteSource writes a minimal valid verdict JSON payload
// to ${workspacePath}/.harmonik/review.json so ArchiveVerdict has a source.
func archiveVerdictFixtureWriteSource(t *testing.T, workspacePath string) {
	t.Helper()
	payload := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks good."}`)
	target := ReviewVerdictPath(workspacePath)
	//nolint:gosec // G306: test fixture; 0644 is appropriate
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		t.Fatalf("archiveVerdictFixtureWriteSource: WriteFile: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReviewVerdictArchivePath — path helper shape
// ─────────────────────────────────────────────────────────────────────────────

// TestWM016_ArchivePathShape verifies that ReviewVerdictArchivePath produces
// the canonical path ${workspace_path}/.harmonik/review.iter-<N>.json.
func TestWM016_ArchivePathShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		iterN int
		want  string
	}{
		{1, ".harmonik/review.iter-1.json"},
		{2, ".harmonik/review.iter-2.json"},
		{3, ".harmonik/review.iter-3.json"},
	}
	for _, tc := range cases {
		got := ReviewVerdictArchivePath("/ws", tc.iterN)
		want := filepath.Join("/ws", tc.want)
		if got != want {
			t.Errorf("ReviewVerdictArchivePath(%d): got %q, want %q", tc.iterN, got, want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ArchiveVerdict — happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestWM016_ArchivePlacesFileAtCorrectPath verifies that ArchiveVerdict renames
// review.json to review.iter-<N>.json at the correct canonical path.
func TestWM016_ArchivePlacesFileAtCorrectPath(t *testing.T) {
	t.Parallel()
	workspacePath := archiveVerdictFixtureMakeWorkspace(t)
	archiveVerdictFixtureWriteSource(t, workspacePath)

	if err := ArchiveVerdict(workspacePath, 1); err != nil {
		t.Fatalf("ArchiveVerdict: unexpected error: %v", err)
	}

	// Source must be gone.
	src := ReviewVerdictPath(workspacePath)
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source %q still exists after archive (want: absent)", src)
	}

	// Destination must exist.
	dst := ReviewVerdictArchivePath(workspacePath, 1)
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination %q absent after archive: %v", dst, err)
	}
}

// TestWM016_ArchivePreservesContent verifies that the archived file's content
// is identical to the original review.json.
func TestWM016_ArchivePreservesContent(t *testing.T) {
	t.Parallel()
	workspacePath := archiveVerdictFixtureMakeWorkspace(t)
	payload := []byte(`{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["FLAG-A"],"notes":"Needs work."}`)
	target := ReviewVerdictPath(workspacePath)
	//nolint:gosec // G306: test fixture; 0644 is appropriate
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}

	if err := ArchiveVerdict(workspacePath, 2); err != nil {
		t.Fatalf("ArchiveVerdict: %v", err)
	}

	dst := ReviewVerdictArchivePath(workspacePath, 2)
	//nolint:gosec // G304: test fixture path from t.TempDir(); not user input
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile archived file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("archived content mismatch: got %q, want %q", got, payload)
	}
}

// TestWM016_ArchiveIterationN3 verifies iteration cap boundary (N=3).
func TestWM016_ArchiveIterationN3(t *testing.T) {
	t.Parallel()
	workspacePath := archiveVerdictFixtureMakeWorkspace(t)
	archiveVerdictFixtureWriteSource(t, workspacePath)

	if err := ArchiveVerdict(workspacePath, 3); err != nil {
		t.Fatalf("ArchiveVerdict(3): unexpected error: %v", err)
	}
	dst := ReviewVerdictArchivePath(workspacePath, 3)
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination review.iter-3.json absent: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ArchiveVerdict — double-archive at same N returns error
// ─────────────────────────────────────────────────────────────────────────────

// TestWM016_DoubleArchiveSameNReturnsError verifies that calling ArchiveVerdict
// twice with the same iterationN returns an error on the second call.
func TestWM016_DoubleArchiveSameNReturnsError(t *testing.T) {
	t.Parallel()
	workspacePath := archiveVerdictFixtureMakeWorkspace(t)
	archiveVerdictFixtureWriteSource(t, workspacePath)

	// First archive — succeeds.
	if err := ArchiveVerdict(workspacePath, 1); err != nil {
		t.Fatalf("first ArchiveVerdict: unexpected error: %v", err)
	}

	// Write a new source file to simulate the next iteration's reviewer writing
	// review.json again.
	archiveVerdictFixtureWriteSource(t, workspacePath)

	// Second archive at the same N=1 — MUST fail because review.iter-1.json exists.
	err := ArchiveVerdict(workspacePath, 1)
	if err == nil {
		t.Fatal("second ArchiveVerdict at same N=1: expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ArchiveVerdict — absent source returns ErrNotFound
// ─────────────────────────────────────────────────────────────────────────────

// TestWM016_AbsentSourceReturnsErrNotFound verifies that ArchiveVerdict returns
// an error wrapping ErrNotFound when review.json does not exist.
func TestWM016_AbsentSourceReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	workspacePath := archiveVerdictFixtureMakeWorkspace(t)
	// Deliberately do NOT write review.json.

	err := ArchiveVerdict(workspacePath, 1)
	if err == nil {
		t.Fatal("ArchiveVerdict on absent source: expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("ArchiveVerdict on absent source: got %v; want errors.Is(err, ErrNotFound) == true", err)
	}
}

// TestWM016_ErrNotFoundClassString verifies that Class(ErrNotFound) returns
// the expected string for observability payloads.
func TestWM016_ErrNotFoundClassString(t *testing.T) {
	t.Parallel()
	got := Class(ErrNotFound)
	if got != "NotFound" {
		t.Errorf("Class(ErrNotFound) = %q; want %q", got, "NotFound")
	}
}
