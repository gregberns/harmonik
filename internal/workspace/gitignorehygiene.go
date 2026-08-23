package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RequiredGitignoreEntries are the six harmonik control-plane patterns that
// MUST appear in the repository's root .gitignore per workspace-model.md
// §4.3 WM-013e.
//
// Order is preserved per the spec: "Required ignore entries (patterns relative
// to repo root; order preserved): .harmonik/lease.lock, .harmonik/sessions/,
// .harmonik/worktrees/, .harmonik/events/, .harmonik/review.json,
// .harmonik/review.iter-*.json"
//
// The .harmonik/events/ entry covers the workspace-local durability JSONL file
// introduced by WM-013b. The .harmonik/review.json and
// .harmonik/review.iter-*.json entries exclude review-loop artifacts (reviewer
// verdict files and their per-iteration archives) from checkpoint commits per
// §4.5.WM-027a — the reviewer verdict is workflow-control state, not work
// product, and MUST NOT pollute the squash-merge commit per WM-019.
var RequiredGitignoreEntries = []string{
	".harmonik/lease.lock",
	".harmonik/sessions/",
	".harmonik/worktrees/",
	".harmonik/events/",
	".harmonik/review.json",
	".harmonik/review.iter-*.json",
}

// GitignoreBranchName is the dedicated git branch on which the workspace manager
// commits missing .gitignore entries per WM-013e.
const GitignoreBranchName = "harmonik/gitignore-init"

// EnsureGitignoreHygiene checks the repository's root .gitignore for the six
// required harmonik control-plane patterns (WM-013e) and adds any missing
// entries.
//
// # Startup obligation
//
// The workspace manager MUST call EnsureGitignoreHygiene BEFORE creating any
// worktree. If the .gitignore is missing required entries, EnsureGitignoreHygiene
// adds them, stages the file, and commits on a dedicated branch named
// [GitignoreBranchName] (`harmonik/gitignore-init`).
//
// # Write-or-fail posture
//
// If the .gitignore file exists but the process lacks write permission,
// EnsureGitignoreHygiene returns [ErrGitignoreWriteForbidden] (wrapped). The
// daemon MUST surface this error to the operator and MUST NOT continue silently.
// Silent continuation with a misconfigured ignore file risks leaking daemon state
// into user commits (per WM-013e rationale).
//
// # Idempotency
//
// EnsureGitignoreHygiene is idempotent: if all six entries are already present,
// the function returns nil without modifying the file or making a commit.
//
// # Pattern matching
//
// The check is line-prefix based: an entry is considered present when the
// .gitignore content contains the entry string on its own line (not as a
// substring of another entry). This avoids false negatives from inline comments
// or surrounding whitespace.
//
// ctx is passed to exec.CommandContext for the git commit invocation.
//
// Spec refs:
//   - workspace-model.md §4.3 WM-013e — gitignore hygiene rule and commit posture.
//   - workspace-model.md §8 — error taxonomy: GitignoreWriteForbidden.
func EnsureGitignoreHygiene(ctx context.Context, repoRoot string) error {
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	existing := ""
	//nolint:gosec // G304: path is constructed from repoRoot + ".gitignore", not user input
	data, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: ReadFile %q: %w", gitignorePath, err)
	}
	if err == nil {
		existing = string(data)
	}

	missing := missingGitignoreEntries(existing)
	if len(missing) == 0 {
		return nil
	}

	toAppend := buildGitignoreBlock(existing, missing)
	//nolint:gosec // G302: .gitignore is repository metadata and must remain group/world-readable
	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%w: cannot write %q: %w", ErrGitignoreWriteForbidden, gitignorePath, err)
		}
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: OpenFile %q: %w", gitignorePath, err)
	}

	if _, err := f.WriteString(toAppend); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: EnsureGitignoreHygiene: WriteString: %w", err), f.Close())
	}
	if err := f.Sync(); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: EnsureGitignoreHygiene: Sync: %w", err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: Close: %w", err)
	}

	desiredContent := existing + toAppend

	if err := gitignoreCommit(ctx, repoRoot, gitignorePath); err != nil {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: commit: %w", err)
	}

	if err := reassertGitignoreWorkingTree(gitignorePath, desiredContent); err != nil {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: %w", err)
	}

	return nil
}

func reassertGitignoreWorkingTree(gitignorePath, desired string) error {
	//nolint:gosec // G304: gitignorePath is repoRoot + ".gitignore", not user input.
	cur, err := os.ReadFile(gitignorePath)
	if err == nil && string(cur) == desired {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reassert .gitignore: read %q: %w", gitignorePath, err)
	}
	//nolint:gosec // G306: 0644 matches the append-write mode above.
	if err := os.WriteFile(gitignorePath, []byte(desired), 0o644); err != nil {
		return fmt.Errorf("reassert .gitignore: write %q: %w", gitignorePath, err)
	}
	return nil
}

// MissingGitignoreEntries reports which of [RequiredGitignoreEntries] are
// absent from the given .gitignore content string.
//
// The check is line-based: an entry is present when the content contains the
// entry on its own line. Callers may use this for dry-run or reporting purposes.
func MissingGitignoreEntries(content string) []string {
	return missingGitignoreEntries(content)
}

func missingGitignoreEntries(content string) []string {
	var missing []string
	for _, entry := range RequiredGitignoreEntries {
		if !gitignoreEntryPresent(content, entry) {
			missing = append(missing, entry)
		}
	}
	return missing
}

func gitignoreEntryPresent(content, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

func buildGitignoreBlock(existing string, missing []string) string {
	var sb strings.Builder

	if existing != "" && !strings.HasSuffix(existing, "\n") {
		sb.WriteString("\n")
	}
	if existing != "" {
		sb.WriteString("\n# harmonik control-plane paths (added by workspace manager per WM-013e)\n")
	} else {
		sb.WriteString("# harmonik control-plane paths (added by workspace manager per WM-013e)\n")
	}

	for _, entry := range missing {
		sb.WriteString(entry)
		sb.WriteString("\n")
	}
	return sb.String()
}

func gitignoreCommit(ctx context.Context, repoRoot, gitignorePath string) (retErr error) {
	origRef, origErr := gitignoreCapturedHead(ctx, repoRoot)
	if origErr != nil {
		return origErr
	}

	if err := checkoutGitignoreBranch(ctx, repoRoot); err != nil {
		return err
	}

	if origRef != GitignoreBranchName {
		defer func() {
			if rErr := restoreGitignoreHead(ctx, repoRoot, origRef); rErr != nil && retErr == nil {
				retErr = rErr
			}
		}()
	}

	current, err := currentGitBranch(ctx, repoRoot)
	if err != nil {
		return err
	}
	if current != GitignoreBranchName {
		return fmt.Errorf("refusing to commit .gitignore onto branch %q: WM-013e requires the dedicated %q branch", current, GitignoreBranchName)
	}

	addCmd := exec.CommandContext(ctx, "git", "add", gitignorePath)
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add .gitignore: %w\noutput: %s", err, out)
	}

	commitMsg := "harmonik: ensure .gitignore covers control-plane paths (WM-013e)"
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = repoRoot
	if out, err := commitCmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit .gitignore: %w\noutput: %s", err, out)
	}
	return nil
}

func currentGitBranch(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func checkoutGitignoreBranch(ctx context.Context, repoRoot string) error {
	if current, err := currentGitBranch(ctx, repoRoot); err == nil && current == GitignoreBranchName {
		return nil
	}

	exists := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"rev-parse", "--verify", "--quiet", "refs/heads/"+GitignoreBranchName).Run() == nil

	args := []string{"-C", repoRoot, "checkout"}
	if exists {
		args = append(args, GitignoreBranchName)
	} else {
		args = append(args, "-b", GitignoreBranchName)
	}
	if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: git args are internally constructed, not user-tainted
		return fmt.Errorf("git checkout %s: %w\noutput: %s", GitignoreBranchName, err, out)
	}
	return nil
}

func gitignoreCapturedHead(ctx context.Context, repoRoot string) (string, error) {
	if out, err := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"symbolic-ref", "--short", "-q", "HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("workspace: gitignoreCommit: capture original HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func restoreGitignoreHead(ctx context.Context, repoRoot, ref string) error {
	if out, err := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"checkout", ref).CombinedOutput(); err != nil {
		return fmt.Errorf("workspace: gitignoreCommit: restore original HEAD %q: %w\noutput: %s", ref, err, out)
	}
	return nil
}

// IsGitignoreWriteForbidden reports whether err wraps [ErrGitignoreWriteForbidden].
func IsGitignoreWriteForbidden(err error) bool {
	return errors.Is(err, ErrGitignoreWriteForbidden)
}
