package daemon

// codexwalguard_test.go — unit tests for cleanCodexStaleWAL (hk-2pb79).
//
// Covered:
//   - WAL larger than threshold, no open handle => removed AND backup exists.
//   - WAL <= threshold => left in place.
//   - config.yaml present but stale_wal_max_bytes absent => returns an error
//     that wraps ErrMissingCodexStaleWALMaxBytes; WAL untouched.
//   - No config.yaml in projectRoot => returns nil; WAL untouched.
//   - Explicit stale_wal_max_bytes: 0 => a non-empty WAL is removed.
//
// The lsof-held case is environment-dependent (it requires a real process
// holding a real handle) and is deliberately NOT exercised here to avoid
// flakiness — these tests pin the size/threshold/config/backup logic. On a host
// with no `lsof`, fileHasOpenHandle returns an error => removal is skipped; the
// removal-expecting cases skip themselves in that environment.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// writeConfigYAML writes a minimal .harmonik/config.yaml under projectRoot with
// the given body and returns the project root. An empty body writes no file.
func writeConfigYAML(t *testing.T, projectRoot, body string) {
	t.Helper()
	dir := filepath.Join(projectRoot, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// writeWAL writes a state_*.sqlite-wal of size bytes under codexHome and also a
// matching base state file and -shm sidecar. Returns the wal path.
func writeWAL(t *testing.T, codexHome string, size int) string {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codexHome: %v", err)
	}
	wal := filepath.Join(codexHome, "state_abc123.sqlite-wal")
	if err := os.WriteFile(wal, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write wal: %v", err)
	}
	base := filepath.Join(codexHome, "state_abc123.sqlite")
	if err := os.WriteFile(base, []byte("db"), 0o644); err != nil {
		t.Fatalf("write base db: %v", err)
	}
	shm := filepath.Join(codexHome, "state_abc123.sqlite-shm")
	if err := os.WriteFile(shm, []byte("shm"), 0o644); err != nil {
		t.Fatalf("write shm: %v", err)
	}
	return wal
}

// lsofAvailable reports whether lsof is on PATH. When it is not, the guard
// conservatively SKIPS removal (cannot confirm a file is unheld), so the
// removal-expecting cases must skip themselves.
func lsofAvailable() bool {
	_, err := exec.LookPath("lsof")
	return err == nil
}

func TestCleanCodexStaleWAL_LargerThanThreshold_Removed(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1024\n")
	wal := writeWAL(t, codexHome, 4096) // > 1024

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("expected wal removed, stat err = %v", err)
	}
	// A backup dir with a copy of the wal must exist.
	entries, _ := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*", filepath.Base(wal)))
	if len(entries) == 0 {
		t.Fatalf("expected a backup copy of the wal, found none")
	}
}

func TestCleanCodexStaleWAL_WithinThreshold_Left(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1048576\n")
	wal := writeWAL(t, codexHome, 1024) // <= threshold

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); err != nil {
		t.Fatalf("expected wal left in place, stat err = %v", err)
	}
}

func TestCleanCodexStaleWAL_MissingKey_FailsLoud(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	// config.yaml present, but NO codex.stale_wal_max_bytes key.
	writeConfigYAML(t, projectRoot, "codex:\n  something_else: true\n")
	wal := writeWAL(t, codexHome, 4096)

	err := cleanCodexStaleWAL(projectRoot, codexHome)
	if err == nil {
		t.Fatalf("expected error for missing key, got nil")
	}
	var target *ErrMissingCodexStaleWALMaxBytes
	if !errors.As(err, &target) {
		t.Fatalf("expected ErrMissingCodexStaleWALMaxBytes, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("expected wal untouched on missing-key fail, stat err = %v", statErr)
	}
}

func TestCleanCodexStaleWAL_CodexBlockNoKey_FailsLoud(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	// A `codex:` block is present but stale_wal_max_bytes is absent.
	writeConfigYAML(t, projectRoot, "codex:\n  model: gpt-5\n")
	wal := writeWAL(t, codexHome, 4096)

	err := cleanCodexStaleWAL(projectRoot, codexHome)
	var target *ErrMissingCodexStaleWALMaxBytes
	if !errors.As(err, &target) {
		t.Fatalf("expected ErrMissingCodexStaleWALMaxBytes for codex block w/o key, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("expected wal untouched on missing-key fail, stat err = %v", statErr)
	}
}

// TestWALUnchangedStale exercises the re-stat half of the TOCTOU re-check
// directly (no lsof dependency). The lsof half + the full concurrent race are
// covered by inspection — see the re-check block in cleanCodexStaleWAL.
func TestWALUnchangedStale(t *testing.T) {
	codexHome := t.TempDir()
	wal := writeWAL(t, codexHome, 4096)
	pre, err := os.Stat(wal)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Unchanged + still over threshold => safe to remove.
	if !walUnchangedStale(pre, wal, 1024) {
		t.Fatalf("expected unchanged-stale to be true for untouched over-threshold wal")
	}
	// Now below threshold (size 4096 <= maxBytes 8192) => not stale, skip.
	if walUnchangedStale(pre, wal, 8192) {
		t.Fatalf("expected false when size no longer exceeds threshold")
	}
	// A live writer rewrites the wal => mtime changes => skip.
	future := pre.ModTime().Add(time.Hour)
	if err := os.Chtimes(wal, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if walUnchangedStale(pre, wal, 1024) {
		t.Fatalf("expected false when mtime changed after pre-stat")
	}
	// Gone entirely => skip.
	if err := os.Remove(wal); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if walUnchangedStale(pre, wal, 1024) {
		t.Fatalf("expected false when wal no longer exists")
	}
}

func TestCleanCodexStaleWAL_NoConfig_NoOp(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	// No .harmonik/config.yaml written.
	wal := writeWAL(t, codexHome, 4096)

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("expected nil for no-config no-op, got: %v", err)
	}
	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("expected wal untouched when no config.yaml, stat err = %v", statErr)
	}
}

func TestCleanCodexStaleWAL_ZeroThreshold_RemovesNonEmpty(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 0\n")
	wal := writeWAL(t, codexHome, 64) // non-empty, > 0

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("expected non-empty wal removed at threshold 0, stat err = %v", err)
	}
}

func TestCleanCodexStaleWAL_EmptyProjectRoot_NoOp(t *testing.T) {
	if err := cleanCodexStaleWAL("", t.TempDir()); err != nil {
		t.Fatalf("expected nil for empty projectRoot, got: %v", err)
	}
}
