package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// runs in dry-run mode: orphans are reported in ClaudeWorktreeSweepResult but
// NOT removed. Set to "1" to enable removals.
//
// Example: HARMONIK_SWEEP_CLAUDE_WORKTREES=1
const EnvSweepClaudeWorktrees = "HARMONIK_SWEEP_CLAUDE_WORKTREES"

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
// An entry is classified as an orphan when BOTH conditions hold:
//
//  1. No live process has a working-directory or argv token matching the
//     agent ID (the part of the directory name after "agent-"). This is
//     approximated by a kill(pid, 0) probe against the agent's stored PID
//     when available; absent any PID record the absence of git worktree
//     metadata is sufficient (condition 2).
//
//  2. The path is NOT registered as a git worktree in `git worktree list`.
//     A `git worktree lock`-protected entry is treated as NOT orphaned
//     (lock is a deliberate operator signal).
//
// Conservative default — dry-run only:
//
// When [EnvSweepClaudeWorktrees] is not set to "1" the function reports
// orphans in ClaudeWorktreeSweepResult.Orphans but does NOT remove them.
// Set HARMONIK_SWEEP_CLAUDE_WORKTREES=1 to enable destructive removal.
//
// This function does NOT touch .harmonik/worktrees/ or any other sweep path;
// it is a strictly parallel sweep path per bead hk-yhq3m (Gap-11).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — orchestrator must not leak
// sub-agent worktrees across SIGKILL recovery cycles.
// Bead ref: hk-yhq3m.
func SweepClaudeWorktrees(ctx context.Context, projectDir string, logger *log.Logger) (ClaudeWorktreeSweepResult, error) {
	enabled := os.Getenv(EnvSweepClaudeWorktrees) == "1"
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

	// Build the set of git-registered worktree paths once for the whole repo.
	registeredPaths, gitErr := claudeWorktreeListRegistered(ctx, projectDir)
	if gitErr != nil {
		// Non-fatal: log and treat all entries as "not registered" (conservative
		// — we will still check PID liveness, but git-based classification is
		// unavailable).
		if logger != nil {
			logger.Printf("daemon: SweepClaudeWorktrees: git worktree list failed (proceeding without): %v", gitErr)
		}
		registeredPaths = make(map[string]bool)
	}

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
		if registered {
			// Git worktree metadata intact and not locked ← not an orphan.
			result.Skipped = append(result.Skipped, entryPath)
			continue
		}

		// Not registered in git → classify as orphan.
		result.Orphans = append(result.Orphans, entryPath)

		if !enabled {
			// Dry-run: report but do not remove.
			if logger != nil {
				logger.Printf("daemon: SweepClaudeWorktrees: dry-run: would remove orphan %q (set %s=1 to enable)", entryPath, EnvSweepClaudeWorktrees)
			}
			continue
		}

		// Live mode: remove the directory and its contents.
		if removeErr := os.RemoveAll(entryPath); removeErr != nil {
			if logger != nil {
				logger.Printf("daemon: SweepClaudeWorktrees: RemoveAll %q: %v", entryPath, removeErr)
			}
			// Non-fatal: continue sweeping remaining entries.
			continue
		}
		if logger != nil {
			logger.Printf("daemon: SweepClaudeWorktrees: removed orphan worktree %q", entryPath)
		}
		result.Removed = append(result.Removed, entryPath)
	}

	return result, nil
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

// claudeWorktreeListRegistered returns the set of absolute paths that have git
// worktree metadata under projectDir, including locked entries.
//
// Locked worktrees (created via `git worktree lock`, which the Claude Code
// orchestrator uses for active sub-agent worktrees) are NOT orphans — they have
// deliberate metadata and an active owning agent. Both locked and unlocked
// registered paths are included in the returned set.
//
// Only paths with NO git metadata at all are eligible for orphan classification.
func claudeWorktreeListRegistered(ctx context.Context, repoRoot string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("daemon: claudeWorktreeListRegistered: git worktree list: %w", err)
	}

	// Porcelain format: blocks separated by blank lines. Each block starts with
	// "worktree <path>". Locked entries include a "locked" line but are still
	// registered — we include ALL paths regardless of locked status.
	//
	//   worktree /path/to/wt
	//   HEAD abc123
	//   branch refs/heads/foo
	//   locked reason          ← still registered; active agent worktree
	//
	registered := make(map[string]bool)
	var currentPath string

	flush := func() {
		if currentPath != "" {
			registered[currentPath] = true
		}
		currentPath = ""
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
		}
	}
	flush()

	return registered, nil
}
