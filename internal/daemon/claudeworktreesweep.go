package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultClaudeWorktreesSubpath is the default path of the sub-agent worktree
// root relative to the project root. The orchestrator creates per-agent
// worktrees here; they accumulate across SIGKILL recoveries (Gap-11).
//
// The path mirrors the convention used by the Claude Code harness:
//
//	<project>/.claude/worktrees/agent-<hex-id>/
const DefaultClaudeWorktreesSubpath = ".claude/worktrees"

// EnvSweepClaudeWorktrees is the environment variable that enables destructive
// removal of orphan .claude/worktrees entries. When unset or empty the sweep
// runs in dry-run mode for RECENT orphans. Age-stale orphans (older than
// EnvClaudeWorktreeMaxAgeDays) are always removed regardless of this flag.
// Set to "1" to enable immediate removal of all orphans regardless of age.
//
// Example: HARMONIK_SWEEP_CLAUDE_WORKTREES=1
const EnvSweepClaudeWorktrees = "HARMONIK_SWEEP_CLAUDE_WORKTREES"

// EnvClaudeWorktreeMaxAgeDays is the environment variable that sets the
// age threshold (in days) above which a .claude/worktrees/agent-* entry is
// treated as a stale orphan and removed, even without EnvSweepClaudeWorktrees.
// This covers git-locked entries (which the Claude Code harness never unlocks
// on crash) as well as unregistered directories. Default: 3 days.
//
// Example: HARMONIK_CLAUDE_WORKTREE_MAX_AGE_DAYS=3
const EnvClaudeWorktreeMaxAgeDays = "HARMONIK_CLAUDE_WORKTREE_MAX_AGE_DAYS"

// DefaultClaudeWorktreeMaxAgeDays is the default age threshold for stale
// .claude/worktrees/agent-* entries. A locked worktree that has not been
// touched in this many days is force-removed even without the env var.
const DefaultClaudeWorktreeMaxAgeDays = 3

// ClaudeWorktreeSweepResult summarises one pass of [SweepClaudeWorktrees].
type ClaudeWorktreeSweepResult struct {
	// Orphans is the list of worktree directory paths identified as orphans.
	// Populated in both dry-run and live modes.
	Orphans []string

	// Removed is the list of orphan paths that were actually deleted.
	// Empty in dry-run mode (Enabled == false).
	Removed []string

	// Skipped is the list of paths that were inspected but not classified as
	// orphans (agent process live, or git worktree metadata intact).
	Skipped []string

	// Enabled reports whether live-removal mode was active during this pass.
	Enabled bool
}

// SweepClaudeWorktrees walks the .claude/worktrees/ directory under
// projectDir and identifies orphan sub-agent worktrees.
//
// Removal rules (applied in order):
//
//  1. registered in git AND NOT locked → active agent, skip.
//  2. locked AND directory mtime ≤ [EnvClaudeWorktreeMaxAgeDays] → recently
//     active agent (the Claude Code harness uses git worktree lock to mark
//     active worktrees), skip.
//  3. locked AND directory mtime > max-age → stale-locked orphan: the agent
//     crashed and the Claude Code harness never removed the lock. Force-remove
//     via `git worktree remove --force --force` (the double flag overrides the
//     git lock), falling back to os.RemoveAll if git fails.
//  4. not registered AND (age > max-age OR EnvSweepClaudeWorktrees=1) → orphan:
//     remove via os.RemoveAll.
//  5. not registered AND age ≤ max-age AND EnvSweepClaudeWorktrees≠1 → dry-run:
//     report in Orphans but do not remove.
//
// This function does NOT touch .harmonik/worktrees/ or any other sweep path;
// it is a strictly parallel sweep path per bead hk-yhq3m (Gap-11).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — orchestrator must not leak
// sub-agent worktrees across SIGKILL recovery cycles.
// Bead ref: hk-yhq3m, hk-qe736.
func SweepClaudeWorktrees(ctx context.Context, projectDir string, logger *log.Logger) (ClaudeWorktreeSweepResult, error) {
	enabled := os.Getenv(EnvSweepClaudeWorktrees) == "1"
	maxAge := claudeWorktreesMaxAge()
	result := ClaudeWorktreeSweepResult{Enabled: enabled}

	claudeWorktreesRoot := filepath.Join(projectDir, DefaultClaudeWorktreesSubpath)

	entries, err := os.ReadDir(claudeWorktreesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			// No .claude/worktrees directory — nothing to sweep.
			return result, nil
		}
		return result, fmt.Errorf("daemon: SweepClaudeWorktrees: ReadDir %q: %w", claudeWorktreesRoot, err)
	}

	// Build registered and locked sets once for the whole repo.
	registeredPaths, lockedPaths, gitErr := claudeWorktreeListRegisteredAndLocked(ctx, projectDir)
	if gitErr != nil {
		// Non-fatal: proceed with empty sets (conservative — all entries
		// appear unregistered and will be age-checked or env-var-gated).
		if logger != nil {
			logger.Printf("daemon: SweepClaudeWorktrees: git worktree list failed (proceeding without): %v", gitErr)
		}
		registeredPaths = make(map[string]bool)
		lockedPaths = make(map[string]bool)
	}

	var needsGitPrune bool

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only entries matching the "agent-<hex>" naming convention are swept.
		if !claudeWorktreeNameValid(name) {
			continue
		}

		entryPath := filepath.Join(claudeWorktreesRoot, name)

		// Resolve symlinks for the git-registered check (macOS /var → /private/var).
		resolvedPath := entryPath
		if rp, err := filepath.EvalSymlinks(entryPath); err == nil {
			resolvedPath = rp
		}

		registered := registeredPaths[entryPath] || registeredPaths[resolvedPath]
		isLocked := lockedPaths[entryPath] || lockedPaths[resolvedPath]

		// Get directory mtime for age classification.
		info, statErr := entry.Info()
		if statErr != nil {
			if logger != nil {
				logger.Printf("daemon: SweepClaudeWorktrees: stat %q: %v (skipping)", entryPath, statErr)
			}
			result.Skipped = append(result.Skipped, entryPath)
			continue
		}
		ageStale := maxAge > 0 && time.Since(info.ModTime()) > maxAge

		// Rule (1): registered and unlocked → active agent.
		if registered && !isLocked {
			result.Skipped = append(result.Skipped, entryPath)
			continue
		}

		// Rule (2): locked but not old enough → recently active agent.
		if isLocked && !ageStale {
			result.Skipped = append(result.Skipped, entryPath)
			continue
		}

		// Rules (3)-(5): orphan candidate (locked+stale, or not registered).
		result.Orphans = append(result.Orphans, entryPath)

		removeNow := ageStale || enabled
		if !removeNow {
			// Rule (5): dry-run.
			if logger != nil {
				logger.Printf("daemon: SweepClaudeWorktrees: dry-run: would remove orphan %q (set %s=1 to enable)", entryPath, EnvSweepClaudeWorktrees)
			}
			continue
		}

		var removed bool

		if isLocked {
			// Rule (3): force-remove locked worktree. Double --force overrides
			// `git worktree lock` (single --force is not sufficient for locked entries).
			cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "worktree", "remove", "--force", "--force", entryPath)
			if out, removeErr := cmd.CombinedOutput(); removeErr != nil {
				if logger != nil {
					logger.Printf("daemon: SweepClaudeWorktrees: git worktree remove %q: %v\noutput: %s (falling back to RemoveAll)", entryPath, removeErr, out)
				}
			} else {
				removed = true
				needsGitPrune = true
			}
		}

		if !removed {
			// Rule (4) primary path, or rule (3) fallback after git failure.
			if removeErr := os.RemoveAll(entryPath); removeErr != nil {
				if logger != nil {
					logger.Printf("daemon: SweepClaudeWorktrees: RemoveAll %q: %v", entryPath, removeErr)
				}
				continue
			}
			if isLocked {
				// git admin entry still exists; prune will clean it up.
				needsGitPrune = true
			}
		}

		if logger != nil {
			logger.Printf("daemon: SweepClaudeWorktrees: removed orphan worktree %q (locked=%v, age=%v)", entryPath, isLocked, time.Since(info.ModTime()).Round(time.Hour))
		}
		result.Removed = append(result.Removed, entryPath)
	}

	// Drop stale git admin entries left by any locked worktrees removed above.
	if needsGitPrune {
		pruneCmd := exec.CommandContext(ctx, "git", "-C", projectDir, "worktree", "prune")
		if out, pruneErr := pruneCmd.CombinedOutput(); pruneErr != nil && logger != nil {
			logger.Printf("daemon: SweepClaudeWorktrees: git worktree prune: %v\noutput: %s", pruneErr, out)
		}
	}

	return result, nil
}

// claudeWorktreesMaxAge returns the age threshold above which a
// .claude/worktrees/agent-* entry is treated as stale. Reads
// [EnvClaudeWorktreeMaxAgeDays]; defaults to [DefaultClaudeWorktreeMaxAgeDays].
func claudeWorktreesMaxAge() time.Duration {
	if v := os.Getenv(EnvClaudeWorktreeMaxAgeDays); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return DefaultClaudeWorktreeMaxAgeDays * 24 * time.Hour
}

// claudeWorktreeNameValid reports whether name matches the "agent-<hex>"
// convention used by the Claude Code orchestrator when creating sub-agent
// worktrees. The hex suffix is one or more hex digits (lower-case or
// upper-case) or the letter 'a'–'f'/'A'–'F' mixed with decimal digits.
//
// Rather than a strict hex regex, we accept any name that starts with
// "agent-" and has a non-empty suffix composed of [A-Za-z0-9] characters.
// This matches real entries like "agent-a8e49d3ccd1f65def" while rejecting
// plain files, hidden entries, and the ".harmonik" tree.
func claudeWorktreeNameValid(name string) bool {
	const prefix = "agent-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := name[len(prefix):]
	if len(suffix) == 0 {
		return false
	}
	for _, c := range suffix {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// claudeWorktreeListRegisteredAndLocked returns the set of absolute paths that
// have git worktree metadata under repoRoot (registered) and, separately, the
// subset of those that carry a `git worktree lock` annotation (locked).
//
// The Claude Code harness uses `git worktree lock` to mark active sub-agent
// worktrees. A locked entry is NOT an orphan unless it is also age-stale
// (the harness does not unlock on crash). Only paths with NO git metadata at
// all are unconditionally eligible for orphan classification.
//
// Porcelain format parsed:
//
//	worktree /path/to/wt
//	HEAD abc123
//	branch refs/heads/foo
//	locked optional-reason   ← locked entry; still registered
func claudeWorktreeListRegisteredAndLocked(ctx context.Context, repoRoot string) (registered map[string]bool, locked map[string]bool, err error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		return nil, nil, fmt.Errorf("daemon: claudeWorktreeListRegisteredAndLocked: git worktree list: %w", cmdErr)
	}

	registered = make(map[string]bool)
	locked = make(map[string]bool)
	var currentPath string
	var currentLocked bool

	flush := func() {
		if currentPath != "" {
			registered[currentPath] = true
			if currentLocked {
				locked[currentPath] = true
			}
		}
		currentPath = ""
		currentLocked = false
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			flush()
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "locked") {
			currentLocked = true
		}
	}
	flush()

	return registered, locked, nil
}
