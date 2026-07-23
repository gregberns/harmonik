package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// leaseFixtureMakeLockJSON returns the JSON body for a lease-lock file per
// workspace-model.md §4.3 WM-013a. All fields are required.
//
// leaseFixtureTTLSec is the advisory ttl_sec every fixture lock carries.
const leaseFixtureTTLSec = 3600

// Fields:
//   - run_id:     UUID of the owning run.
//   - pid:        daemon process ID that wrote the lock.
//   - created_at: RFC 3339 wall-clock time the lock was written.
//   - ttl_sec:    advisory lifetime, always leaseFixtureTTLSec (informative;
//     does not enforce auto-expiry, and no test varies it).
//
// Prefixed leaseFixture to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixtureMakeLockJSON(runID string, pid int, createdAt time.Time) []byte {
	return []byte(fmt.Sprintf(
		`{"run_id":%q,"pid":%d,"created_at":%q,"ttl_sec":%d}`,
		runID,
		pid,
		createdAt.UTC().Format(time.RFC3339),
		leaseFixtureTTLSec,
	))
}

// leaseFixtureWriteLockAtomic atomically writes content to target using the
// sequence: temp-file write → fsync → rename → parent-dir fsync.
//
// This matches the atomic-write discipline mandated by workspace-model.md
// §4.3 WM-013a: "The workspace manager MUST write the lease-lock file atomically
// (write-to-temp + rename) and MUST fsync the file before emitting workspace_leased."
//
// Parent-dir fsync is best-effort on macOS (HFS+ / APFS may suppress the
// fsync on directory fds), but the call MUST be made for spec compliance.
//
// Prefixed leaseFixture to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixtureWriteLockAtomic(t *testing.T, target string, content []byte) {
	t.Helper()

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: MkdirAll %q: %v", dir, err)
	}

	// Write to a temp file in the same directory (same filesystem as target,
	// guaranteeing rename(2) is atomic).
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

	// fsync the temp file before rename so the data is durable.
	if err := f.Sync(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Sync (pre-rename): %v",
			withCleanupErrs(err, f.Close(), os.Remove(tmpPath)))
	}
	if err := f.Close(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Close (pre-rename): %v",
			withCleanupErrs(err, os.Remove(tmpPath)))
	}

	// Atomic rename: POSIX rename(2) is atomic within the same filesystem.
	if err := os.Rename(tmpPath, target); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Rename %q → %q: %v", tmpPath, target,
			withCleanupErrs(err, os.Remove(tmpPath)))
	}

	// Parent-directory fsync to durably record the rename.
	// On macOS this is best-effort (APFS may suppress fsync on directory fds),
	// but the call MUST be made for spec compliance per WM-013a.
	dirFD, err := os.Open(dir)
	if err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Open dir %q for fsync: %v", dir, err)
	}
	// Ignore fsync error on directories on macOS — it is best-effort per APFS docs.
	_ = dirFD.Sync()
	if err := dirFD.Close(); err != nil {
		t.Fatalf("leaseFixtureWriteLockAtomic: Close dir fd: %v", err)
	}
}

// leaseFixtureReleaseLock removes the lease-lock file at target, implementing
// the idempotent release contract of WM-013b: "Release itself is idempotent:
// a second release call against an already-released workspace MUST succeed
// without error."
//
// Prefixed leaseFixture to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixtureReleaseLock(t *testing.T, target string) {
	t.Helper()
	err := os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixtureReleaseLock: Remove %q: %v", target, err)
	}
	// Second call must also succeed (idempotent).
	err = os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixtureReleaseLock: idempotent second Remove %q: %v", target, err)
	}
}

// leaseFixtureLeaseLockPath returns the canonical lease-lock path for a workspace.
// Per workspace-model.md §4.3 WM-013a and §6.2:
//
//	${workspace_path}/.harmonik/lease.lock
//
// Prefixed leaseFixture to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixtureLeaseLockPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "lease.lock")
}
