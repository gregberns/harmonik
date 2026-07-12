package lifecycle

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// branchReapRunPrefix is the refs/heads prefix for task branches.
const branchReapRunPrefix = "refs/heads/run/"

// branchReapAgentPrefix is the refs/heads prefix for legacy worktree-agent-*
// branches (old naming convention retired in favour of run/*).
const branchReapAgentPrefix = "refs/heads/worktree-agent-"

// BranchReapOptions configures a branch reap pass.
type BranchReapOptions struct {
	// RepoDir is the absolute path to the git repository root.
	RepoDir string

	// TargetBranch is the branch against which merge status is checked.
	// A run/* or worktree-agent-* branch whose tip is reachable from TargetBranch
	// is classified as "merged" and is safe to delete. Defaults to "main".
	TargetBranch string

	// OrphanMaxAge is the minimum age before an UNMERGED branch is eligible for
	// deletion as an orphan (i.e., no active worktree exists for it). Unmerged
	// branches newer than this value are always skipped.
	// Defaults to 30 days.
	OrphanMaxAge time.Duration

	// DryRun, when true, identifies candidates but does not delete them.
	DryRun bool
}

// BranchReapEvent is one branch_reaped record emitted per deleted branch.
type BranchReapEvent struct {
	Event    string    `json:"event"`     // always "branch_reaped"
	Branch   string    `json:"branch"`    // short branch name
	Reason   string    `json:"reason"`    // "merged" | "orphaned_run" | "orphaned_agent"
	Age      string    `json:"age"`       // human-readable age, e.g. "47d"
	ReapedAt time.Time `json:"reaped_at"` // when the delete was issued (or would be, on dry-run)
}

// BranchReapResult summarizes a branch reap pass.
type BranchReapResult struct {
	// Scanned is the total number of candidate branches enumerated.
	Scanned int
	// Reaped is the short names of branches actually deleted (or would-delete on dry-run).
	Reaped []string
	// Skipped is the number of candidates left untouched (active worktree, too recent, or error).
	Skipped int
	// Events is one BranchReapEvent per deleted branch.
	Events []BranchReapEvent
}

// branchCandidate is one enumerated branch considered for reaping.
type branchCandidate struct {
	// shortName is the short branch name, e.g. "run/019f5535-..." or "worktree-agent-abc".
	shortName string
	// creatorDate is the date of the branch tip commit.
	creatorDate time.Time
	// isAgentBranch is true for worktree-agent-* branches (legacy naming).
	isAgentBranch bool
}

// ReapBranches enumerates run/* and worktree-agent-* branches and deletes:
//
//  1. Any branch whose tip is fully merged into TargetBranch (git merge-base check).
//  2. Any branch older than OrphanMaxAge that has no active registered worktree.
//
// Safety constraints:
//   - Only run/* and worktree-agent-* are ever touched; all other refs are
//     invisible to this function.
//   - Branches checked out in a registered git worktree are never deleted
//     regardless of age or merge status.
//   - When DryRun is true, the result lists candidates but performs no deletes.
//
// A nil or empty RepoDir returns an error. A missing TargetBranch defaults to
// "main"; a zero OrphanMaxAge defaults to 30 days.
func ReapBranches(ctx context.Context, opts BranchReapOptions) (BranchReapResult, error) {
	var result BranchReapResult

	if opts.RepoDir == "" {
		return result, fmt.Errorf("lifecycle: ReapBranches: RepoDir is required")
	}
	if opts.TargetBranch == "" {
		opts.TargetBranch = "main"
	}
	if opts.OrphanMaxAge <= 0 {
		opts.OrphanMaxAge = 30 * 24 * time.Hour
	}

	// Step 1: enumerate all run/* and worktree-agent-* branches.
	candidates, err := listBranchCandidates(ctx, opts.RepoDir)
	if err != nil {
		return result, fmt.Errorf("lifecycle: ReapBranches: list candidates: %w", err)
	}
	result.Scanned = len(candidates)

	if len(candidates) == 0 {
		return result, nil
	}

	// Step 2: collect the set of branches fully merged into TargetBranch.
	mergedSet, err := listMergedBranchSet(ctx, opts.RepoDir, opts.TargetBranch)
	if err != nil {
		// A missing TargetBranch (e.g. empty repo) is non-fatal — treat as no merges.
		mergedSet = map[string]struct{}{}
	}

	// Step 3: collect the set of branch names currently checked out in registered worktrees.
	activeSet, err := listActiveWorktreeBranches(ctx, opts.RepoDir)
	if err != nil {
		// git worktree list failure is non-fatal; conservatively treat all as active.
		activeSet = map[string]struct{}{}
	}

	now := time.Now().UTC()

	for _, c := range candidates {
		_, merged := mergedSet[c.shortName]
		_, active := activeSet[c.shortName]

		if active {
			// Branch is checked out in a live registered worktree — never reap.
			result.Skipped++
			continue
		}

		age := now.Sub(c.creatorDate)
		reason := ""

		switch {
		case merged:
			reason = "merged"
		case c.isAgentBranch && age >= opts.OrphanMaxAge:
			// Legacy worktree-agent-* branches: all orphaned; retire on MaxAge.
			reason = "orphaned_agent"
		case !c.isAgentBranch && age >= opts.OrphanMaxAge:
			// Unmerged run/* older than MaxAge with no active worktree.
			reason = "orphaned_run"
		default:
			result.Skipped++
			continue
		}

		ageStr := formatAge(age)
		ev := BranchReapEvent{
			Event:    "branch_reaped",
			Branch:   c.shortName,
			Reason:   reason,
			Age:      ageStr,
			ReapedAt: now,
		}

		if !opts.DryRun {
			if delErr := deleteBranch(ctx, opts.RepoDir, c.shortName); delErr != nil {
				// Best-effort: log the skip but do not abort the pass.
				result.Skipped++
				continue
			}
		}

		result.Reaped = append(result.Reaped, c.shortName)
		result.Events = append(result.Events, ev)
	}

	return result, nil
}

// listBranchCandidates enumerates all run/* and worktree-agent-* branches in
// repoDir, returning their short names and tip-commit creator dates.
//
// Uses `git for-each-ref --format='%(refname:short) %(creatordate:unix)'` against
// the two ref prefixes. An empty repository (no refs) returns nil, nil.
func listBranchCandidates(ctx context.Context, repoDir string) ([]branchCandidate, error) {
	//nolint:gosec // G204: arguments are hard-coded constants and repoDir resolved at startup; not user input
	cmd := exec.CommandContext(ctx, "git",
		"-C", repoDir,
		"for-each-ref",
		"--format=%(refname:short) %(creatordate:unix)",
		branchReapRunPrefix,
		branchReapAgentPrefix+"*",
	)
	out, err := cmd.Output()
	if err != nil {
		// git exits non-zero when no refs match — treat as empty.
		return nil, nil
	}
	if len(out) == 0 {
		return nil, nil
	}

	var candidates []branchCandidate
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		var epochSecs int64
		if _, parseErr := fmt.Sscanf(parts[1], "%d", &epochSecs); parseErr != nil {
			continue
		}
		candidates = append(candidates, branchCandidate{
			shortName:     name,
			creatorDate:   time.Unix(epochSecs, 0).UTC(),
			isAgentBranch: strings.HasPrefix(name, "worktree-agent-"),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lifecycle: listBranchCandidates: scan: %w", err)
	}
	return candidates, nil
}

// listMergedBranchSet returns a set of short branch names that are fully merged
// into targetBranch (i.e., their tip commit is reachable from targetBranch).
//
// Uses `git for-each-ref --merged=<target>` restricted to the two candidate
// prefixes. A missing or unknown targetBranch causes git to exit non-zero;
// callers treat that as an empty set.
func listMergedBranchSet(ctx context.Context, repoDir, targetBranch string) (map[string]struct{}, error) {
	//nolint:gosec // G204: targetBranch is caller-supplied but validated to be non-empty; repoDir resolved at startup
	cmd := exec.CommandContext(ctx, "git",
		"-C", repoDir,
		"for-each-ref",
		"--merged="+targetBranch,
		"--format=%(refname:short)",
		branchReapRunPrefix,
		branchReapAgentPrefix+"*",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: listMergedBranchSet: git for-each-ref --merged: %w", err)
	}

	set := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			set[name] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lifecycle: listMergedBranchSet: scan: %w", err)
	}
	return set, nil
}

// listActiveWorktreeBranches returns a set of short branch names that are
// currently checked out in a registered git worktree. Branches in this set
// MUST NOT be deleted by the reaper.
//
// Parses `git worktree list --porcelain` and extracts "branch refs/heads/<name>"
// lines. A detached HEAD worktree contributes no branch name.
func listActiveWorktreeBranches(ctx context.Context, repoDir string) (map[string]struct{}, error) {
	//nolint:gosec // G204: arguments are hard-coded constants and repoDir resolved at startup; not user input
	cmd := exec.CommandContext(ctx, "git",
		"-C", repoDir,
		"worktree", "list", "--porcelain",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lifecycle: listActiveWorktreeBranches: git worktree list: %w", err)
	}

	const branchPrefix = "branch refs/heads/"
	set := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, branchPrefix) {
			name := strings.TrimSpace(line[len(branchPrefix):])
			if name != "" {
				set[name] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lifecycle: listActiveWorktreeBranches: scan: %w", err)
	}
	return set, nil
}

// deleteBranch deletes the named branch from repoDir using `git branch -D`.
// It uses -D (force-delete) because the caller has already verified the branch
// is either merged or an aged orphan with no active worktree. The callers of
// this function must guarantee the branch is NOT checked out in any worktree
// before invoking — see the activeSet guard in ReapBranches.
func deleteBranch(ctx context.Context, repoDir, shortName string) error {
	//nolint:gosec // G204: shortName is a git ref name validated by git for-each-ref; repoDir resolved at startup
	cmd := exec.CommandContext(ctx, "git",
		"-C", repoDir,
		"branch", "-D", shortName,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("lifecycle: deleteBranch %q: %w\noutput: %s", shortName, err, out)
	}
	return nil
}

// formatAge formats a duration as a human-readable age string, e.g. "47d" or "3h".
func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days >= 1 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d.Hours())
	if hours >= 1 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
