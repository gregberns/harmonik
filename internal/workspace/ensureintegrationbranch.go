package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// EnsureIntegrationBranch creates branch at base when branch does not exist.
// Concurrent callers converge through git update-ref's zero-old-value compare.
func EnsureIntegrationBranch(ctx context.Context, repoRoot, branch, base string) error {
	if repoRoot == "" || branch == "" || base == "" {
		return fmt.Errorf("workspace: ensure integration branch: repo, branch, and base are required")
	}
	if integrationBranchExists(ctx, repoRoot, branch) {
		return nil
	}
	//nolint:gosec // Git receives a structured argument. No shell interprets the ref.
	baseCmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", base+"^{commit}")
	baseCmd.Dir = repoRoot
	baseOut, err := baseCmd.Output()
	if err != nil {
		return fmt.Errorf("workspace: ensure integration branch %q: resolve base %q: %w", branch, base, err)
	}
	baseSHA := strings.TrimSpace(string(baseOut))
	//nolint:gosec // Git receives structured arguments. No shell interprets the ref.
	createCmd := exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+branch, baseSHA, strings.Repeat("0", 40))
	createCmd.Dir = repoRoot
	if out, createErr := createCmd.CombinedOutput(); createErr != nil {
		if integrationBranchExists(ctx, repoRoot, branch) {
			return nil
		}
		return fmt.Errorf("workspace: ensure integration branch %q from %q: %w: %s",
			branch, base, createErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func integrationBranchExists(ctx context.Context, repoRoot, branch string) bool {
	//nolint:gosec // Git receives a structured argument. No shell interprets the ref.
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	return cmd.Run() == nil
}
