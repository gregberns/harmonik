package handlercontract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// OrphanWorkspaceSubReason is the agent_failed sub_reason emitted when HC-044a
// detects a workspace held by a prior-generation orphan handler subprocess.
//
// Spec: specs/handler-contract.md §4.10.HC-044a, §8.2.
const OrphanWorkspaceSubReason = "workspace_held_by_orphan"

// workerPidfileContent is the JSON shape written to the worker pidfile.
// The file is written atomically at subprocess spawn and removed on clean
// session termination per HC-044a.
//
// Fields:
//   - pid       — the PID of the handler subprocess.
//   - run_id    — the run_id whose session owns this subprocess.
//   - written_at — RFC3339 wall-clock timestamp of the write (informational).
type workerPidfileContent struct {
	PID       int    `json:"pid"`
	RunID     string `json:"run_id"`
	WrittenAt string `json:"written_at"`
}

// WorkerPidfilePath returns the canonical pidfile path for a handler subprocess
// session per specs/handler-contract.md §4.10.HC-044a:
//
//	<projectRoot>/.harmonik/worktrees/<runID>/.lock
//
// This path is handler-side (per-run) and is distinct from:
//   - the daemon pidfile at <projectRoot>/.harmonik/daemon.pid (process-lifecycle.md §4.1)
//   - the workspace lease-lock at <workspacePath>/.harmonik/lease.lock (workspace-model.md §4.3 WM-013a)
//
// The projectRoot is the directory containing the project's .harmonik/ directory.
func WorkerPidfilePath(projectRoot, runID string) string {
	return filepath.Join(projectRoot, ".harmonik", "worktrees", runID, ".lock")
}

// WriteWorkerPidfileAtomic writes the worker pidfile at target atomically using
// the temp+rename+fsync discipline:
//
//  1. Write JSON content to a sibling temp file (.tmp-<pid>).
//  2. fsync the temp file so data is durable before rename.
//  3. rename(2) the temp file to target (POSIX rename is atomic within one fs).
//  4. fsync the parent directory to durably record the rename.
//
// The caller MUST call WriteWorkerPidfileAtomic immediately after the subprocess
// is spawned, before the watcher is started, so that any concurrent Launch call
// for the same workspace will detect the pidfile and fail-fast per HC-044a.
//
// Use WorkerPidfilePath to construct target from projectRoot and runID.
//
// Returns an error if any I/O step fails.
func WriteWorkerPidfileAtomic(target, runID string, pid int) error {
	content, err := json.Marshal(workerPidfileContent{
		PID:       pid,
		RunID:     runID,
		WrittenAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: marshal: %w", err)
	}

	dir := filepath.Dir(target)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path is constructed from projectRoot + known relative segments + run_id, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Write: %w", err)
	}

	// Step 2: fsync temp file before rename.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Sync (pre-rename): %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Close (pre-rename): %w", err)
	}

	// Step 3: atomic rename.
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Rename %q → %q: %w", tmpPath, target, err)
	}

	// Step 4: parent-dir fsync — best-effort on macOS/APFS per spec.
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per HC-044a / WM-013a precedent
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("handlercontract: WriteWorkerPidfileAtomic: Close dir fd: %w", err)
	}

	return nil
}

// RemoveWorkerPidfile removes the worker pidfile at target.
//
// The caller MUST call RemoveWorkerPidfile when the handler subprocess exits
// cleanly so that the pidfile does not appear as a false orphan on the next
// Launch call per HC-044a. Removal is idempotent: a missing file is not an
// error.
//
// Use WorkerPidfilePath to construct target from projectRoot and runID.
func RemoveWorkerPidfile(target string) error {
	err := os.Remove(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("handlercontract: RemoveWorkerPidfile: Remove %q: %w", target, err)
	}
	return nil
}

// OrphanCheckResult is the outcome of CheckOrphanHeldWorkspace.
type OrphanCheckResult struct {
	// Held reports whether the workspace is held by an orphan subprocess.
	// When true, OrphanPID carries the PID from the pidfile.
	Held bool

	// OrphanPID is the PID recorded in the stale pidfile.
	// Only meaningful when Held is true.
	OrphanPID int

	// OrphanRunID is the run_id recorded in the stale pidfile.
	// Only meaningful when Held is true.
	OrphanRunID string

	// Stale reports that the pidfile was reclaimed (PID not live or recycled).
	// When true, the caller MAY proceed; Held is false when Stale is true.
	Stale bool
}

// CheckOrphanHeldWorkspace implements the HC-044a orphan-detection check.
//
// Before Launch returns a Session, the daemon MUST call this function to verify
// that the target workspace is not held by a prior-generation handler subprocess.
//
// Detection algorithm (per §4.10.HC-044a):
//
//  1. Read the pidfile at WorkerPidfilePath(projectRoot, runID).
//  2. If absent → no orphan; return Held=false.
//  3. If PID is not live (kill(pid,0) returns ESRCH) → stale pidfile; return Stale=true.
//  4. If PID is live AND ownedByDaemon(pid) → this is our own subprocess
//     (e.g., a double-launch race caught by HC-004's idempotency check);
//     return Held=false.
//  5. If PID is live AND NOT ownedByDaemon(pid) → orphan from prior generation;
//     return Held=true, OrphanPID=pid.
//
// ownedByCurrentGen is a set of PIDs that the current daemon generation owns
// (i.e., subprocesses spawned after this daemon started). An empty or nil set
// means the caller asserts that no PIDs are owned; any live PID in the pidfile
// will then be treated as a potential orphan only if it is actually live.
//
// Note: this function does NOT remove the stale pidfile. The caller SHOULD
// remove stale pidfiles (Stale=true) after verifying the stale condition to
// prevent accumulated junk. The caller MUST NOT remove live pidfiles (Held=true).
//
// Returns ErrStructural wrapping the orphan detail when Held is true.
// Returns (result, nil) in all other cases.
func CheckOrphanHeldWorkspace(projectRoot, runID string, ownedByCurrentGen map[int]bool) (OrphanCheckResult, error) {
	pidfilePath := WorkerPidfilePath(projectRoot, runID)

	//nolint:gosec // G304: path is constructed from projectRoot + known relative segments + run_id, not user input
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No pidfile → no orphan.
			return OrphanCheckResult{}, nil
		}
		return OrphanCheckResult{}, fmt.Errorf("handlercontract: CheckOrphanHeldWorkspace: ReadFile %q: %w", pidfilePath, err)
	}

	var pf workerPidfileContent
	if err := json.Unmarshal(data, &pf); err != nil {
		// Unparseable pidfile: treat as stale to allow reclaim per HC-044a
		// ("stale pidfiles MAY be reclaimed by the new generation").
		return OrphanCheckResult{Stale: true}, nil
	}

	if pf.PID <= 0 {
		// Invalid PID: treat as stale.
		return OrphanCheckResult{Stale: true}, nil
	}

	// Liveness probe via kill(pid, 0) — platform-equivalent per HC-044a.
	liveErr := syscall.Kill(pf.PID, 0)
	if liveErr != nil {
		// ESRCH = no such process; EPERM = process exists but we lack permission
		// to signal it (still live). Other errors are treated conservatively as live.
		if isProcessNotFound(liveErr) {
			// PID is not live → stale pidfile.
			return OrphanCheckResult{Stale: true}, nil
		}
		// EPERM or unknown error: assume live (conservative / fail-safe).
	}

	// PID is live. If it's owned by the current daemon generation, it is not
	// an orphan — this would be a concurrent-launch scenario caught by HC-004.
	if ownedByCurrentGen[pf.PID] {
		return OrphanCheckResult{}, nil
	}

	// Live PID not owned by current generation → orphan from prior generation.
	// HC-044a: Launch MUST return ErrStructural with sub-reason workspace_held_by_orphan.
	detail := fmt.Errorf(
		"handlercontract: workspace held by orphan handler subprocess: pid=%d run_id=%q pidfile=%q: %w",
		pf.PID, pf.RunID, pidfilePath, ErrStructural,
	)
	return OrphanCheckResult{
		Held:        true,
		OrphanPID:   pf.PID,
		OrphanRunID: pf.RunID,
	}, detail
}

// isProcessNotFound reports whether err from syscall.Kill represents a
// "no such process" condition (ESRCH on POSIX platforms).
func isProcessNotFound(err error) bool {
	return err == syscall.ESRCH
}
