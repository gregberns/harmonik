package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// LeaseLockPath returns the canonical lease-lock file path for a workspace.
//
// Per workspace-model.md §4.3 WM-013a and §6.2:
//
//	${workspace_path}/.harmonik/lease.lock
//
// The lock file's existence represents the lease; its absence represents a
// released workspace. Birth is at workspace_leased; death is at
// merged/discarded per WM-013b.
//
// Cross-spec note: HC-044a names a different path (${workspace_path}/.lock);
// WM's path is authoritative per OQ-WM-005 (BLOCKING-CROSS-SPEC).
func LeaseLockPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "lease.lock")
}

// WriteLeaseLockAtomic writes lock to the canonical lease-lock path target
// using the atomic-write discipline mandated by workspace-model.md §4.3 WM-013a:
//
//  1. Write JSON content to a sibling temp file (.tmp-<pid>).
//  2. fsync the temp file so the data is durable before rename.
//  3. rename(2) the temp file to target (POSIX rename is atomic within one fs).
//  4. fsync the parent directory to durably record the rename on-disk.
//
// The caller MUST call WriteLeaseLockAtomic before emitting workspace_leased
// (WM-016); the four-step ordering gate of WM-016 requires the lease-lock to
// be durable on disk before the event fires.
//
// Step 4 (parent-dir fsync) is best-effort on macOS/APFS — APFS may suppress
// the fsync on directory fds — but the call MUST be made for spec compliance.
//
// Returns an error if lock.Valid() is false, or if any I/O step fails.
func WriteLeaseLockAtomic(target string, lock *core.LeaseLockFile) error {
	if !lock.Valid() {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: invalid LeaseLockFile (run_id=%v pid=%d ttl_sec=%d)", lock.RunID, lock.PID, lock.TTLSec)
	}

	content, err := marshalLeaseLock(lock)
	if err != nil {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: marshal: %w", err)
	}

	dir := filepath.Dir(target)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path is constructed from workspace_path + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Write: %w", err)
	}

	// Step 2: fsync the temp file before rename so data is durable.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Sync (pre-rename): %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Close (pre-rename): %w", err)
	}

	// Step 3: atomic rename — POSIX rename(2) is atomic within the same filesystem.
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Rename %q → %q: %w", tmpPath, target, err)
	}

	// Step 4: parent-dir fsync to durably record the rename.
	// Best-effort on macOS/APFS per spec; sync error is intentionally suppressed.
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per WM-013a
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("workspace: WriteLeaseLockAtomic: Close dir fd: %w", err)
	}

	return nil
}

// ReadLeaseLock reads and parses the lease-lock file at target.
//
// Returns (nil, nil) when target does not exist — the caller interprets
// absence as "not leased" per WM-013a. Returns an error for I/O or parse
// failures other than os.IsNotExist.
func ReadLeaseLock(target string) (*core.LeaseLockFile, error) {
	//nolint:gosec // G304: path is constructed from workspace_path + known relative segments, not user input
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // caller interprets nil as "not leased" per WM-013a
		}
		return nil, fmt.Errorf("workspace: ReadLeaseLock: ReadFile %q: %w", target, err)
	}

	var v struct {
		RunID     string `json:"run_id"`
		PID       int    `json:"pid"`
		CreatedAt string `json:"created_at"`
		TTLSec    int    `json:"ttl_sec"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("workspace: ReadLeaseLock: Unmarshal %q: %w", target, err)
	}

	u, err := uuid.Parse(v.RunID)
	if err != nil {
		return nil, fmt.Errorf("workspace: ReadLeaseLock: parse run_id %q: %w", v.RunID, err)
	}

	createdAt, err := time.Parse(time.RFC3339, v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("workspace: ReadLeaseLock: parse created_at %q: %w", v.CreatedAt, err)
	}

	lock := &core.LeaseLockFile{
		RunID:     core.RunID(u),
		PID:       v.PID,
		CreatedAt: createdAt,
		TTLSec:    v.TTLSec,
	}
	if !lock.Valid() {
		return nil, fmt.Errorf("workspace: ReadLeaseLock: parsed lock at %q is not valid", target)
	}
	return lock, nil
}

// marshalLeaseLock encodes lock to JSON per WM-013a:
//
//	{"run_id":"<uuid>","pid":<int>,"created_at":"<rfc3339>","ttl_sec":<int>}
func marshalLeaseLock(lock *core.LeaseLockFile) ([]byte, error) {
	v := struct {
		RunID     string `json:"run_id"`
		PID       int    `json:"pid"`
		CreatedAt string `json:"created_at"`
		TTLSec    int    `json:"ttl_sec"`
	}{
		RunID:     lock.RunID.String(),
		PID:       lock.PID,
		CreatedAt: lock.CreatedAt.UTC().Format(time.RFC3339),
		TTLSec:    lock.TTLSec,
	}
	return json.Marshal(v)
}
