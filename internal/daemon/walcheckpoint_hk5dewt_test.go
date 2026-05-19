package daemon

// walcheckpoint_hk5dewt_test.go — unit tests for the WAL-checkpoint pre-flight
// helper (hk-5dewt).
//
// Test helper prefix: walCkpt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// walCkptMakeWAL creates a fake beads.db-wal of the given size under a
// temporary directory hierarchy (.beads/beads.db-wal) and returns the
// project directory and WAL path. The directory is cleaned up by t.TempDir.
func walCkptMakeWAL(t *testing.T, sizeBytes int64) (projectDir, walPath string) {
	t.Helper()
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("walCkptMakeWAL: mkdir .beads: %v", err)
	}

	walPath = filepath.Join(beadsDir, "beads.db-wal")
	//nolint:gosec // G304: path constructed from t.TempDir(); not user input
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("walCkptMakeWAL: create beads.db-wal: %v", err)
	}
	if err := f.Truncate(sizeBytes); err != nil {
		_ = f.Close()
		t.Fatalf("walCkptMakeWAL: truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("walCkptMakeWAL: close: %v", err)
	}
	return dir, walPath
}

// walCkptStatSize returns the current size of path, failing the test on error.
func walCkptStatSize(t *testing.T, path string) int64 {
	t.Helper()
	//nolint:gosec // G304: path constructed from test helper; not user input
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("walCkptStatSize: stat %s: %v", path, err)
	}
	return info.Size()
}

// TestWALCheckpointPreflight_SkipWhenAbsent asserts that when beads.db-wal
// does not exist, runWALCheckpointPreflight returns nil.
func TestWALCheckpointPreflight_SkipWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := runWALCheckpointPreflight(context.Background(), dir); err != nil {
		t.Fatalf("expected nil error when WAL absent, got: %v", err)
	}
}

// TestWALCheckpointPreflight_SkipWhenBelowThreshold asserts that a WAL file
// smaller than walCheckpointThreshold (1 MB) causes the function to return nil
// without touching the WAL.
func TestWALCheckpointPreflight_SkipWhenBelowThreshold(t *testing.T) {
	smallSize := int64(walCheckpointThreshold - 1)
	projectDir, walPath := walCkptMakeWAL(t, smallSize)

	if err := runWALCheckpointPreflight(context.Background(), projectDir); err != nil {
		t.Fatalf("expected nil error for sub-threshold WAL, got: %v", err)
	}
	// WAL file must remain untouched (no truncation attempted).
	got := walCkptStatSize(t, walPath)
	if got != smallSize {
		t.Errorf("WAL size changed unexpectedly: got %d, want %d", got, smallSize)
	}
}

// TestWALCheckpointPreflight_TruncatesLargeWAL creates a fake 5 MB WAL file
// alongside a real SQLite database and verifies that after runWALCheckpointPreflight
// the WAL is truncated to 0 bytes.
//
// This test requires sqlite3 on PATH. If sqlite3 is absent the test is skipped.
func TestWALCheckpointPreflight_TruncatesLargeWAL(t *testing.T) {
	// Skip if sqlite3 is unavailable on this host.
	sqlite3Path, lookErr := exec.LookPath("sqlite3")
	if lookErr != nil {
		t.Skip("sqlite3 not on PATH; skipping WAL-truncation test")
	}

	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	walPath := filepath.Join(beadsDir, "beads.db-wal")

	// Create a minimal WAL-mode SQLite3 database so PRAGMA wal_checkpoint succeeds.
	walCkptInitDB(t, sqlite3Path, dbPath)

	// Replace the sqlite3-created WAL with a fake 5 MB file to ensure the
	// threshold check fires. A real WAL from a single-insert DB can be tiny.
	const fakeWALSize = 5 * (1 << 20) // 5 MB
	//nolint:gosec // G304: path constructed from t.TempDir(); not user input
	wf, err := os.OpenFile(walPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create fake WAL: %v", err)
	}
	if err := wf.Truncate(fakeWALSize); err != nil {
		_ = wf.Close()
		t.Fatalf("truncate fake WAL: %v", err)
	}
	if err := wf.Close(); err != nil {
		t.Fatalf("close fake WAL: %v", err)
	}

	preStat := walCkptStatSize(t, walPath)
	if preStat != fakeWALSize {
		t.Fatalf("pre-checkpoint WAL size = %d, want %d", preStat, fakeWALSize)
	}

	if err := runWALCheckpointPreflight(context.Background(), dir); err != nil {
		t.Fatalf("runWALCheckpointPreflight returned error: %v", err)
	}

	// After PRAGMA wal_checkpoint(TRUNCATE) the WAL must be 0 bytes (or absent).
	//nolint:gosec // G304: walPath constructed from t.TempDir(); not user input
	postInfo, statErr := os.Stat(walPath)
	if statErr != nil && !os.IsNotExist(statErr) {
		t.Fatalf("stat WAL after checkpoint: %v", statErr)
	}
	var postSize int64
	if statErr == nil {
		postSize = postInfo.Size()
	}
	if postSize != 0 {
		t.Errorf("WAL size after checkpoint = %d, want 0", postSize)
	}
}

// walCkptInitDB creates a minimal WAL-mode SQLite3 database at dbPath using
// the sqlite3 binary at sqlite3Path.
func walCkptInitDB(t *testing.T, sqlite3Path, dbPath string) {
	t.Helper()
	//nolint:gosec // G204: sqlite3Path resolved via exec.LookPath; dbPath from t.TempDir
	cmd := exec.CommandContext(t.Context(), sqlite3Path, dbPath,
		"PRAGMA journal_mode=WAL; CREATE TABLE t (id INTEGER PRIMARY KEY); INSERT INTO t VALUES (1);",
	)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("walCkptInitDB: sqlite3 init failed: %v\noutput: %s", runErr, out)
	}
}
