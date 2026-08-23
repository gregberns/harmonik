package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const leaseFixtureTTLSec = 3600

func leaseFixtureMakeLockJSON(runID string, pid int, createdAt time.Time) []byte {
	return []byte(fmt.Sprintf(
		`{"run_id":%q,"pid":%d,"created_at":%q,"ttl_sec":%d}`,
		runID,
		pid,
		createdAt.UTC().Format(time.RFC3339),
		leaseFixtureTTLSec,
	))
}

func leaseFixtureWriteLockAtomic(t *testing.T, target string, content []byte) {
	t.Helper()

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: MkdirAll %q: %v", dir, err)
	}

	tmpPath := target + fmt.Sprintf(".tmp-%d", os.Getpid())
	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: OpenFile %q: %v", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Write: %v",
			withCleanupErrs(err, f.Close(), os.Remove(tmpPath)))
	}

	if err := f.Sync(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Sync (pre-rename): %v",
			withCleanupErrs(err, f.Close(), os.Remove(tmpPath)))
	}
	if err := f.Close(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Close (pre-rename): %v",
			withCleanupErrs(err, os.Remove(tmpPath)))
	}

	if err := os.Rename(tmpPath, target); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Rename %q → %q: %v", tmpPath, target,
			withCleanupErrs(err, os.Remove(tmpPath)))
	}

	// Parent-directory fsync to durably record the rename.
	// On macOS this is best-effort (APFS may suppress fsync on directory fds),
	// but the call MUST be made for spec compliance per WM-013a.
	//nolint:gosec // G304: dir is derived from the controlled test fixture lock path.
	dirFD, err := os.Open(dir)
	if err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Open dir %q for fsync: %v", dir, err)
	}
	if syncErr := dirFD.Sync(); syncErr != nil {
		t.Errorf("leaseFixtureWriteLockAtomic: Sync dir: %v", syncErr)
	}
	if err := dirFD.Close(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Close dir fd: %v", err)
	}
}

func leaseFixtureReleaseLock(t *testing.T, target string) {
	t.Helper()
	err := os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixtureReleaseLock: Remove %q: %v", target, err)
	}
	err = os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixtureReleaseLock: idempotent second Remove %q: %v", target, err)
	}
}

func leaseFixtureLeaseLockPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "lease.lock")
}
