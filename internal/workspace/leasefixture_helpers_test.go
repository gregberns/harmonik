package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// leaseFixture_makeLockJSON returns the JSON body for a lease-lock file per
// workspace-model.md §4.3 WM-013a. All fields are required.
//
// Fields:
//   - run_id:     UUID of the owning run.
//   - pid:        daemon process ID that wrote the lock.
//   - created_at: RFC 3339 wall-clock time the lock was written.
//   - ttl_sec:    advisory lifetime (informative; does not enforce auto-expiry).
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_makeLockJSON(runID string, pid int, createdAt time.Time, ttlSec int) []byte {
	return []byte(fmt.Sprintf(
		`{"run_id":%q,"pid":%d,"created_at":%q,"ttl_sec":%d}`,
		runID,
		pid,
		createdAt.UTC().Format(time.RFC3339),
		ttlSec,
	))
}

// leaseFixture_writeLockAtomic atomically writes content to target using the
// sequence: temp-file write → fsync → rename → parent-dir fsync.
//
// This matches the atomic-write discipline mandated by workspace-model.md
// §4.3 WM-013a: "The workspace manager MUST write the lease-lock file atomically
// (write-to-temp + rename) and MUST fsync the file before emitting workspace_leased."
//
// Parent-dir fsync is best-effort on macOS (HFS+ / APFS may suppress the
// fsync on directory fds), but the call MUST be made for spec compliance.
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_writeLockAtomic(t *testing.T, target string, content []byte) {
	t.Helper()

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("leaseFixture_writeLockAtomic: MkdirAll %q: %v", dir, err)
	}

	// Write to a temp file in the same directory (same filesystem as target,
	// guaranteeing rename(2) is atomic).
	tmpPath := target + fmt.Sprintf(".tmp-%d", os.Getpid())
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatalf("leaseFixture_writeLockAtomic: OpenFile %q: %v", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		t.Fatalf("leaseFixture_writeLockAtomic: Write: %v", err)
	}

	// fsync the temp file before rename so the data is durable.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		t.Fatalf("leaseFixture_writeLockAtomic: Sync (pre-rename): %v", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		t.Fatalf("leaseFixture_writeLockAtomic: Close (pre-rename): %v", err)
	}

	// Atomic rename: POSIX rename(2) is atomic within the same filesystem.
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		t.Fatalf("leaseFixture_writeLockAtomic: Rename %q → %q: %v", tmpPath, target, err)
	}

	// Parent-directory fsync to durably record the rename.
	// On macOS this is best-effort (APFS may suppress fsync on directory fds),
	// but the call MUST be made for spec compliance per WM-013a.
	dirFD, err := os.Open(dir)
	if err != nil {
		t.Fatalf("leaseFixture_writeLockAtomic: Open dir %q for fsync: %v", dir, err)
	}
	// Ignore fsync error on directories on macOS — it is best-effort per APFS docs.
	_ = dirFD.Sync()
	if err := dirFD.Close(); err != nil {
		t.Fatalf("leaseFixture_writeLockAtomic: Close dir fd: %v", err)
	}
}

// leaseFixture_releaseLock removes the lease-lock file at target, implementing
// the idempotent release contract of WM-013b: "Release itself is idempotent:
// a second release call against an already-released workspace MUST succeed
// without error."
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_releaseLock(t *testing.T, target string) {
	t.Helper()
	err := os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixture_releaseLock: Remove %q: %v", target, err)
	}
	// Second call must also succeed (idempotent).
	err = os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("leaseFixture_releaseLock: idempotent second Remove %q: %v", target, err)
	}
}

// leaseFixture_leaseLockPath returns the canonical lease-lock path for a workspace.
// Per workspace-model.md §4.3 WM-013a and §6.2:
//
//	${workspace_path}/.harmonik/lease.lock
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_leaseLockPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "lease.lock")
}

// leaseFixture_workspaceLocalEventsDir returns the directory containing
// workspace-local JSONL files per WM-013b.
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_workspaceLocalEventsDir(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "events")
}

// leaseFixture_workspaceLocalEventsFile returns the workspace-local durability
// JSONL file path for the given workspace_id per WM-013b and §6.2:
//
//	${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_workspaceLocalEventsFile(workspacePath, workspaceID string) string {
	return filepath.Join(leaseFixture_workspaceLocalEventsDir(workspacePath),
		"workspace-"+workspaceID+".jsonl")
}

// leaseFixture_writeReleaseMarker appends the lease_released JSONL marker to the
// workspace-local events file per WM-013b and fsyncs the file.
//
// The marker format per WM-013b post-escalation path:
//
//	{"event":"lease_released","run_id":"<run_id>","workspace_id":"<workspace_id>","reason":"<reason>","released_at":"<rfc3339>"}
//
// Prefixed leaseFixture_ to avoid sibling-package collisions (bead hk-8mwo.67).
func leaseFixture_writeReleaseMarker(t *testing.T, workspacePath, runID, workspaceID, reason string) {
	t.Helper()

	eventsDir := leaseFixture_workspaceLocalEventsDir(workspacePath)
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("leaseFixture_writeReleaseMarker: MkdirAll %q: %v", eventsDir, err)
	}

	eventsFile := leaseFixture_workspaceLocalEventsFile(workspacePath, workspaceID)
	marker := fmt.Sprintf(
		`{"event":"lease_released","run_id":%q,"workspace_id":%q,"reason":%q,"released_at":%q}`,
		runID, workspaceID, reason, time.Now().UTC().Format(time.RFC3339),
	) + "\n"

	f, err := os.OpenFile(eventsFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("leaseFixture_writeReleaseMarker: OpenFile %q: %v", eventsFile, err)
	}
	if _, err := f.Write([]byte(marker)); err != nil {
		_ = f.Close()
		t.Fatalf("leaseFixture_writeReleaseMarker: Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("leaseFixture_writeReleaseMarker: Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("leaseFixture_writeReleaseMarker: Close: %v", err)
	}
}
