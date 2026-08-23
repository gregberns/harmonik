package codex

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func writeConfigYAML(t *testing.T, projectRoot, body string) {
	t.Helper()
	dir := filepath.Join(projectRoot, ".harmonik")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

func writeWAL(t *testing.T, codexHome string, size int) string {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatalf("mkdir codexHome: %v", err)
	}
	wal := filepath.Join(codexHome, "state_abc123.sqlite-wal")
	if err := os.WriteFile(wal, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write wal: %v", err)
	}
	base := filepath.Join(codexHome, "state_abc123.sqlite")
	if err := os.WriteFile(base, []byte("db"), 0o600); err != nil {
		t.Fatalf("write base db: %v", err)
	}
	shm := filepath.Join(codexHome, "state_abc123.sqlite-shm")
	if err := os.WriteFile(shm, []byte("shm"), 0o600); err != nil {
		t.Fatalf("write shm: %v", err)
	}
	return wal
}

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

	if err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("expected wal removed, stat err = %v", err)
	}
	entries, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*", filepath.Base(wal)))
	if globErr != nil {
		t.Fatalf("glob backup dirs: %v", globErr)
	}
	if len(entries) == 0 {
		t.Fatalf("expected a backup copy of the wal, found none")
	}
}

// TestCleanCodexStaleWAL_SmallUnheldWAL_Removed pins the hk-xisvb regression: a
// small (234 KB) stale WAL, well UNDER the 1 MiB threshold, must now be removed
// and backed up. Under the OLD size-gate (`if info.Size() <= maxBytes { continue
// }`) this exact file was skipped (234*1024 = 239616 <= 1048576), making the
// guard a no-op while codex fast-failed fleet-wide. Staleness is a function of
// being left by a killed/slept run, not of size — so the only safety gate is now
// "unheld" + the TOCTOU re-check, and this small WAL is cleaned.
func TestCleanCodexStaleWAL_SmallUnheldWAL_Removed(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1048576\n") // 1 MiB threshold
	const small = 234 * 1024                                                    // 239616 bytes, well under 1 MiB
	wal := writeWAL(t, codexHome, small)

	if info, err := os.Stat(wal); err != nil {
		t.Fatalf("stat wal: %v", err)
	} else if info.Size() > 1048576 {
		t.Fatalf("test setup: small WAL is not actually under the threshold (size=%d)", info.Size())
	}

	if err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("expected small under-threshold wal REMOVED (hk-xisvb), stat err = %v", err)
	}
	entries, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*", filepath.Base(wal)))
	if globErr != nil {
		t.Fatalf("glob backup dirs: %v", globErr)
	}
	if len(entries) == 0 {
		t.Fatalf("expected a backup copy of the small wal, found none")
	}
}

func TestCleanCodexStaleWAL_MissingKey_FailsLoud(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  something_else: true\n")
	wal := writeWAL(t, codexHome, 4096)

	err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome)
	if err == nil {
		t.Fatalf("expected error for missing key, got nil")
	}
	var target *ErrMissingStaleWALMaxBytes
	if !errors.As(err, &target) {
		t.Fatalf("expected ErrMissingStaleWALMaxBytes, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("expected wal untouched on missing-key fail, stat err = %v", statErr)
	}
}

func TestCleanCodexStaleWAL_CodexBlockNoKey_FailsLoud(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  model: gpt-5\n")
	wal := writeWAL(t, codexHome, 4096)

	err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome)
	var target *ErrMissingStaleWALMaxBytes
	if !errors.As(err, &target) {
		t.Fatalf("expected ErrMissingStaleWALMaxBytes for codex block w/o key, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("expected wal untouched on missing-key fail, stat err = %v", statErr)
	}
}

// TestWALUnchanged exercises the re-stat half of the TOCTOU re-check directly (no
// lsof dependency). It is now byte-threshold-FREE (hk-xisvb): the helper's sole
// job is "did a live writer touch this file between the lsof check and the
// remove?", detected via size OR mtime change. The lsof half + the full
// concurrent race are covered by inspection — see the re-check block in
// cleanCodexStaleWAL.
func TestWALUnchanged(t *testing.T) {
	codexHome := t.TempDir()
	wal := writeWAL(t, codexHome, 4096)
	pre, err := os.Stat(wal)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if !walUnchanged(pre, wal) {
		t.Fatalf("expected walUnchanged to be true for an untouched wal")
	}
	if err := os.WriteFile(wal, make([]byte, 8192), 0o600); err != nil {
		t.Fatalf("rewrite wal larger: %v", err)
	}
	if walUnchanged(pre, wal) {
		t.Fatalf("expected false when size changed after pre-stat")
	}
	if err := os.WriteFile(wal, make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("restore wal size: %v", err)
	}
	future := pre.ModTime().Add(time.Hour)
	if err := os.Chtimes(wal, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if walUnchanged(pre, wal) {
		t.Fatalf("expected false when mtime changed after pre-stat")
	}
	if err := os.Remove(wal); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if walUnchanged(pre, wal) {
		t.Fatalf("expected false when wal no longer exists")
	}
}

func TestCleanCodexStaleWAL_NoConfig_NoOp(t *testing.T) {
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	wal := writeWAL(t, codexHome, 4096)

	if err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome); err != nil {
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

	if err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("expected non-empty wal removed at threshold 0, stat err = %v", err)
	}
}

func TestCleanCodexStaleWAL_EmptyProjectRoot_NoOp(t *testing.T) {
	if err := cleanCodexStaleWAL(t.Context(), "", t.TempDir()); err != nil {
		t.Fatalf("expected nil for empty projectRoot, got: %v", err)
	}
}

// TestReapCodexWALBackupDirs_KeepsLastN pins the reap behavior directly: given
// more than walBackupKeepLast pre-existing .wal-backup-* dirs, only the newest
// walBackupKeepLast survive and the rest are removed.
func TestReapCodexWALBackupDirs_KeepsLastN(t *testing.T) {
	codexHome := t.TempDir()
	total := walBackupKeepLast + 3
	var dirs []string
	for i := 0; i < total; i++ {
		dir := filepath.Join(codexHome, fmt.Sprintf(".wal-backup-%d", i))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		dirs = append(dirs, dir)
	}

	reapCodexWALBackupDirs(t.Context(), codexHome)

	remaining, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*"))
	if globErr != nil {
		t.Fatalf("glob backup dirs: %v", globErr)
	}
	if len(remaining) != walBackupKeepLast {
		t.Fatalf("expected %d backup dirs to remain, got %d: %v", walBackupKeepLast, len(remaining), remaining)
	}
	for i := 0; i < total-walBackupKeepLast; i++ {
		if _, err := os.Stat(dirs[i]); !os.IsNotExist(err) {
			t.Fatalf("expected oldest backup dir %s reaped, stat err = %v", dirs[i], err)
		}
	}
	for i := total - walBackupKeepLast; i < total; i++ {
		if _, err := os.Stat(dirs[i]); err != nil {
			t.Fatalf("expected newest backup dir %s to survive, stat err = %v", dirs[i], err)
		}
	}
}

// TestCleanCodexStaleWAL_CopyFailure_DropsEmptyBackupDir pins the "drop empty
// backup dir on copy-fail" behavior: if the wal copy into the freshly-created
// backup dir fails, the guard removes the now-useless empty backup dir rather
// than leaving a stub behind. Simulated by making the wal unreadable so
// copyFileForBackup's os.ReadFile fails.
func TestCleanCodexStaleWAL_CopyFailure_DropsEmptyBackupDir(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not block reads")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1024\n")
	wal := writeWAL(t, codexHome, 4096)

	if err := os.Chmod(wal, 0o000); err != nil {
		t.Fatalf("chmod wal unreadable: %v", err)
	}
	defer func() {
		if chmodErr := os.Chmod(wal, 0o600); chmodErr != nil {
			t.Errorf("restore wal perms: %v", chmodErr)
		}
	}()

	if err := cleanCodexStaleWAL(t.Context(), projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*"))
	if globErr != nil {
		t.Fatalf("glob backup dirs: %v", globErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover backup dirs after copy failure, found: %v", entries)
	}
}
