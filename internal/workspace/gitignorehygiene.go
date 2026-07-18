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

// EnsureGitignoreHygiene checks the repository's root .gitignore for the four
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
// EnsureGitignoreHygiene is idempotent: if all four entries are already present,
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

	// Read the current .gitignore content (empty string if absent).
	existing := ""
	//nolint:gosec // G304: path is constructed from repoRoot + ".gitignore", not user input
	data, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: ReadFile %q: %w", gitignorePath, err)
	}
	if err == nil {
		existing = string(data)
	}

	// Identify missing entries.
	missing := missingGitignoreEntries(existing)
	if len(missing) == 0 {
		// All entries present; idempotent no-op.
		return nil
	}

	// Append missing entries with a harmonik-managed section header.
	toAppend := buildGitignoreBlock(existing, missing)
	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%w: cannot write %q: %w", ErrGitignoreWriteForbidden, gitignorePath, err)
		}
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: OpenFile %q: %w", gitignorePath, err)
	}

	if _, err := f.WriteString(toAppend); err != nil {
		_ = f.Close()
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: WriteString: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: Sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: Close: %w", err)
	}

	// Stage and commit on the dedicated branch per WM-013e.
	if err := gitignoreCommit(ctx, repoRoot, gitignorePath); err != nil {
		return fmt.Errorf("workspace: EnsureGitignoreHygiene: commit: %w", err)
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

// missingGitignoreEntries is the internal implementation.
func missingGitignoreEntries(content string) []string {
	var missing []string
	for _, entry := range RequiredGitignoreEntries {
		if !gitignoreEntryPresent(content, entry) {
			missing = append(missing, entry)
		}
	}
	return missing
}

// gitignoreEntryPresent reports whether entry appears on its own line in content.
func gitignoreEntryPresent(content, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

// buildGitignoreBlock constructs the text to append for the missing entries.
// Adds a blank-line separator when the existing content is non-empty and does
// not end with a newline, to ensure clean formatting.
func buildGitignoreBlock(existing string, missing []string) string {
	var sb strings.Builder

	// Ensure separation from existing content.
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

// gitignoreCommit stages the .gitignore and commits it on the
// [GitignoreBranchName] branch per WM-013e.
//
// WM-013e mandates that the daemon-state .gitignore commit land on the dedicated
// `harmonik/gitignore-init` branch, NEVER on the operator's working branch
// (main / whatever HEAD happens to be) — committing daemon state onto the user's
// branch pollutes their history. gitignoreCommit therefore checks out (creating
// if absent) the dedicated branch BEFORE staging, and hard-refuses to commit if
// HEAD is still on any non-harmonik branch after the checkout.
func gitignoreCommit(ctx context.Context, repoRoot, gitignorePath string) error {
	// Move HEAD onto the dedicated init branch so the commit cannot land on the
	// operator's branch.
	if err := checkoutGitignoreBranch(ctx, repoRoot); err != nil {
		return err
	}

	// Safety net: after the checkout, HEAD MUST be the dedicated branch. If it is
	// not (checkout silently no-oped, detached HEAD, etc.), REFUSE rather than
	// inject a daemon-state commit onto whatever branch is checked out.
	current, err := currentGitBranch(ctx, repoRoot)
	if err != nil {
		return err
	}
	if current != GitignoreBranchName {
		return fmt.Errorf("refusing to commit .gitignore onto branch %q: WM-013e requires the dedicated %q branch", current, GitignoreBranchName)
	}

	// git add .gitignore
	addCmd := exec.CommandContext(ctx, "git", "add", gitignorePath)
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add .gitignore: %w\noutput: %s", err, out)
	}

	// git commit — NO --allow-empty: an empty daemon-state commit is never
	// desirable (it would create noise commits carrying no .gitignore change).
	// If the tree is clean after add (nothing changed — already committed on this
	// branch), git commit exits non-zero with "nothing to commit"; treat that as
	// an idempotent no-op.
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

// currentGitBranch returns the abbreviated symbolic name of HEAD in repoRoot.
// A detached HEAD yields "HEAD" (which is not GitignoreBranchName, so callers
// treat it as a non-harmonik branch and refuse).
func currentGitBranch(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// checkoutGitignoreBranch checks out [GitignoreBranchName], creating it from the
// current HEAD when it does not yet exist, so the subsequent .gitignore commit
// lands on the dedicated branch rather than the operator's working branch
// (WM-013e). If HEAD is already on the dedicated branch this is a no-op.
func checkoutGitignoreBranch(ctx context.Context, repoRoot string) error {
	if current, err := currentGitBranch(ctx, repoRoot); err == nil && current == GitignoreBranchName {
		return nil
	}

	// Does the dedicated branch already exist? --verify --quiet exits non-zero
	// (no output) when the ref is absent.
	exists := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"rev-parse", "--verify", "--quiet", "refs/heads/"+GitignoreBranchName).Run() == nil

	args := []string{"-C", repoRoot, "checkout"}
	if exists {
		args = append(args, GitignoreBranchName)
	} else {
		args = append(args, "-b", GitignoreBranchName)
	}
	if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w\noutput: %s", GitignoreBranchName, err, out)
	}
	return nil
}

// IsGitignoreWriteForbidden reports whether err wraps [ErrGitignoreWriteForbidden].
func IsGitignoreWriteForbidden(err error) bool {
	return errors.Is(err, ErrGitignoreWriteForbidden)
}
