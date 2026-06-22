package workspace

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"syscall"
)

// SweepResult holds the outcome of one [SweepStaleLeaseLocks] pass.
type SweepResult struct {
	// Removed is the list of worktree paths whose lease-lock file was removed
	// because the recorded PID was dead (stale per WM-033).
	Removed []string

	// Skipped is the list of worktree paths that were examined but not swept
	// (PID live, no lease-lock, or classification error).
	Skipped []string
}

// SweepStaleLeaseLocks performs the WM-033 startup orphan sweep:
//
//  1. Discovers all worktrees via [DiscoverWorktrees] (WM-013c).
//  2. For each discovered worktree whose lease-lock is present, applies the
//     content-first staleness rule: if the recorded PID is NOT running on this
//     host (os.FindProcess + p.Signal(0) probe), the lease-lock file is removed.
//     Filesystem mtime is NOT consulted; only PID liveness is checked per WM-033.
//  3. After all stale locks are removed, invokes `git worktree prune` against
//     repoRoot to drop stale .git/worktrees/<name>/ metadata entries.
//
// # What the sweep does NOT do
//
//   - It MUST NOT delete worktree directories or branches (WM-033).
//   - It MUST NOT route WM-003a orphan directories to reconciliation (routing
//     is the workspace manager's responsibility after the sweep returns).
//   - It does NOT invoke [ClassifyCrashEvidence] — that is the caller's job for
//     the WM-003a evidence types.
//
// # Context
//
// ctx is passed to exec.CommandContext for the `git worktree prune` invocation.
// If ctx is cancelled, the prune invocation returns an error (best-effort).
//
// # Idempotency
//
// The sweep is idempotent: calling it multiple times produces the same end
// state (stale locks removed, directories intact).
//
// # Cross-spec note (OQ-WM-005)
//
// WM's canonical lock path (`${workspace_path}/.harmonik/lease.lock`) is
// authoritative per WM-013a. HC-044a and PL-006 name different paths; WM does
// NOT write or delete from those paths. Migration artifacts from prior generations
// are treated as unknown files and left in place.
//
// Spec refs:
//   - workspace-model.md §4.8 WM-033 — startup orphan sweep mandate.
//   - workspace-model.md §4.3 WM-013c — discovery mechanism (used in step 1).
//   - workspace-model.md §4.3 WM-013a — lease-lock file format.
func SweepStaleLeaseLocks(ctx context.Context, repoRoot string, cfg WorktreeRootConfig) (SweepResult, error) {
	discovered, err := DiscoverWorktrees(ctx, repoRoot, cfg)
	if err != nil {
		return SweepResult{}, fmt.Errorf("workspace: SweepStaleLeaseLocks: discover: %w", err)
	}

	var result SweepResult

	for _, dw := range discovered {
		if dw.LeaseLock == nil {
			// No lease-lock — directory is either a WM-003a orphan or released.
			// Routing is the caller's responsibility; sweep skips.
			result.Skipped = append(result.Skipped, dw.WorktreePath)
			continue
		}

		stale := isPIDDead(dw.LeaseLock.PID)
		if !stale {
			result.Skipped = append(result.Skipped, dw.WorktreePath)
			continue
		}

		// PID is dead → stale. Remove the lease-lock file per WM-033.
		leaseLockPath := LeaseLockPath(dw.WorktreePath)
		if err := ReleaseLeaseLock(leaseLockPath); err != nil {
			// Non-fatal: log by appending to skipped and continue the sweep.
			result.Skipped = append(result.Skipped, dw.WorktreePath)
			continue
		}
		result.Removed = append(result.Removed, dw.WorktreePath)
	}

	// Post-sweep: run `git worktree prune` to drop stale .git/worktrees/<name>/
	// metadata entries. Operator-issued `git worktree lock` entries are respected
	// (prune skips locked entries). Best-effort: prune error is returned but does
	// NOT undo the lock-file removals already performed.
	pruneCmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		return result, fmt.Errorf("workspace: SweepStaleLeaseLocks: git worktree prune: %w\noutput: %s", err, out)
	}

	return result, nil
}

// IsLeaseLockStale reports whether the lease-lock identified by the given PID
// is stale per the WM-033 content-first staleness rule:
//
//   - PID dead → stale regardless of mtime.
//   - PID live → NOT stale (mtime NOT consulted at MVH).
//
// Note: this function does NOT probe the argv of the owning process for
// harmonik-daemon identity (the "mtime tiebreaker" path of WM-033). The
// argv-probe path applies only when the PID is live and a different daemon
// generation is suspected; that disambiguation is a post-MVH concern. At MVH,
// a live PID is treated as non-stale.
//
// Spec ref: workspace-model.md §4.8 WM-033 — content-first, mtime tiebreaker.
func IsLeaseLockStale(pid int) bool {
	return isPIDDead(pid)
}

// RemoveStaleWorktreeResult holds the outcome of one [RemoveStaleWorktrees] pass.
type RemoveStaleWorktreeResult struct {
	// Removed is the list of worktree paths that were successfully removed
	// by `git worktree remove --force --force`.
	Removed []string

	// Failed is the list of worktree paths where removal was attempted but
	// the `git worktree remove` command failed. Non-fatal per-path.
	Failed []string
}

// RemoveStaleWorktrees removes stale harmonik worktree directories from the
// given paths by running `git worktree remove --force --force` against each.
//
// This is intended to be called with the Removed list from
// [SweepStaleLeaseLocks]: paths whose lease-lock recorded a dead PID. Because
// `git worktree prune` only removes worktree metadata for directories that
// are no longer present on disk, registered stale worktrees require an
// explicit `git worktree remove` to deregister from git AND delete the
// directory simultaneously (hk-ldzp).
//
// For each path that is successfully removed, [PruneWorktreeTrust] is called
// to GC the per-worktree trust key from ~/.claude.json, preventing unbounded
// growth of the trust "projects" map (hk-bfvby).
//
// Errors are non-fatal per-path: a failure on one path does not prevent
// attempting the remaining paths. All failures are collected in
// RemoveStaleWorktreeResult.Failed.
//
// ctx is passed to exec.CommandContext for each `git worktree remove`
// invocation. Cancellation stops processing the remaining paths.
//
// Bead ref: hk-ldzp — daemon worktree/disk GC.
func RemoveStaleWorktrees(ctx context.Context, repoRoot string, paths []string, logger *log.Logger) RemoveStaleWorktreeResult {
	var result RemoveStaleWorktreeResult
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			// Context cancelled: stop processing.
			break
		}
		cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", p)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			if logger != nil {
				logger.Printf("workspace: RemoveStaleWorktrees: git worktree remove %q: %v\noutput: %s", p, err, out)
			}
			result.Failed = append(result.Failed, p)
			continue
		}
		// GC the per-worktree trust key from ~/.claude.json (hk-bfvby): each
		// worktree gets an ephemeral trust entry and without this GC the map
		// grows unbounded. Best-effort: failure is non-fatal.
		if err := PruneWorktreeTrust(p); err != nil && logger != nil {
			logger.Printf("workspace: RemoveStaleWorktrees: PruneWorktreeTrust %q: %v (non-fatal)", p, err)
		}
		if logger != nil {
			logger.Printf("workspace: RemoveStaleWorktrees: removed stale worktree %q", p)
		}
		result.Removed = append(result.Removed, p)
	}
	return result
}

// isPIDDead reports whether pid is not running on this host.
//
// Uses syscall.Kill(pid, 0): signal 0 does not kill the process but causes
// an error if the process does not exist. On UNIX:
//   - err == nil: process is live and we have permission to signal it.
//   - err == syscall.EPERM: process is live (we lack permission; not our own).
//   - err == syscall.ESRCH: process does not exist → dead.
//   - Any other error: treat as dead (conservative; safe for sweep purposes).
//
// This is a best-effort probe: race conditions between the probe and actual
// process termination are inherent; WM-033 accepts this.
func isPIDDead(pid int) bool {
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		// Signal accepted → process is live → NOT stale.
		return false
	}
	if err == syscall.EPERM {
		// Permission denied → process exists but we can't signal it → NOT stale.
		return false
	}
	// ESRCH or any other error → process does not exist → dead.
	return true
}
