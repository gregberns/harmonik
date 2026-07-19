package workspace

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// SweepResult holds the outcome of one [SweepStaleLeaseLocks] pass.
type SweepResult struct {
	// Removed is the list of worktree paths whose lease-lock file was removed
	// because the recorded PID was dead (stale per WM-033).
	Removed []string

	// Skipped is the list of worktree paths that were examined but not swept
	// (PID live, no lease-lock, or classification error).
	Skipped []string

	// NoLock is the subset of Skipped paths that had NO lease-lock file at all
	// (absent, not leased). These are candidates for age-based removal via
	// [RemoveAgedNoLockWorktrees] when their directory is older than a threshold.
	// Paths with a live PID are in Skipped but NOT in NoLock.
	NoLock []string
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
		if dw.LeaseLockUnreadable {
			// Lease-lock file present but its content is unrecoverable
			// (corrupt/truncated) — state UNKNOWN. Fail safe: skip, and do NOT
			// add to NoLock. A worktree with an unreadable lock is never routed
			// to age-based force-removal, because we cannot prove it is unleased.
			result.Skipped = append(result.Skipped, dw.WorktreePath)
			continue
		}
		if dw.LeaseLock == nil {
			// No lease-lock — directory is either a WM-003a orphan or released.
			// Routing is the caller's responsibility; sweep skips.
			result.Skipped = append(result.Skipped, dw.WorktreePath)
			result.NoLock = append(result.NoLock, dw.WorktreePath)
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

// RemoveAgedNoLockWorktrees removes .harmonik/worktrees/ directories from
// paths that have no lease-lock and whose directory mtime is older than maxAge.
// These are orphaned worktrees whose lease-lock was already cleared by a prior
// sweep pass (or never written, e.g., leaked reviewer worktrees) and that were
// not caught by [RemoveStaleWorktrees] because they never had a dead-PID lock.
//
// Age is measured from the MOST-RECENT file mtime found ANYWHERE within the
// worktree tree — NOT the top-directory mtime. The top-dir mtime only changes on
// entry create/delete/rename, so an agent editing a file IN PLACE (the common
// case) never bumps it; using it as the activity proxy would force-remove a
// worktree holding freshly-edited, uncommitted work. Walking the tree for the
// newest mtime captures in-place edits, so a recently-touched worktree is
// correctly seen as active and skipped. maxAge == 0 disables the pass and returns
// an empty result. This is the C1-simplified conservative variant: it uses recent
// activity as a proxy for "not an active run" rather than the full
// DiscoverActiveRuns survive-check (hk-qe736).
//
// Errors are non-fatal per-path, consistent with [RemoveStaleWorktrees]: a path
// whose activity cannot be determined is conservatively SKIPPED (not removed).
func RemoveAgedNoLockWorktrees(ctx context.Context, repoRoot string, paths []string, maxAge time.Duration, logger *log.Logger) RemoveStaleWorktreeResult {
	if maxAge == 0 {
		return RemoveStaleWorktreeResult{}
	}
	now := time.Now()
	var aged []string
	for _, p := range paths {
		newest, err := newestMTimeInTree(p)
		if err != nil {
			// Cannot determine activity → conservatively skip (never remove a
			// worktree we could not fully scan; it may hold recent work).
			if logger != nil {
				logger.Printf("workspace: RemoveAgedNoLockWorktrees: skipping %q; cannot scan tree for activity: %v", p, err)
			}
			continue
		}
		if now.Sub(newest) > maxAge {
			aged = append(aged, p)
		}
	}
	if len(aged) == 0 {
		return RemoveStaleWorktreeResult{}
	}
	return RemoveStaleWorktrees(ctx, repoRoot, aged, logger)
}

// newestMTimeInTree walks the directory tree rooted at root and returns the
// most-recent modification time among the root and every entry beneath it. It is
// the activity proxy for [RemoveAgedNoLockWorktrees]: unlike the top-dir mtime,
// it reflects in-place file edits anywhere in the worktree. A walk error (e.g. a
// vanished entry, permission failure) is returned so the caller can conservatively
// skip the path rather than risk removing a worktree with recent activity.
func newestMTimeInTree(root string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return newest, nil
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
