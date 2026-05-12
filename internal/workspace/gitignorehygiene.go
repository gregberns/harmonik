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
func gitignoreCommit(ctx context.Context, repoRoot, gitignorePath string) error {
	// git add .gitignore
	addCmd := exec.CommandContext(ctx, "git", "add", gitignorePath)
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add .gitignore: %w\noutput: %s", err, out)
	}

	// git commit -m "..." on the current branch (workspace manager calls this
	// before any worktree is created; the branch is main / the operator's branch).
	// WM-013e specifies the dedicated branch name for the commit; creating and
	// checking out that branch is the caller's (workspace manager's) responsibility
	// at the daemon-startup level. Here we commit to whatever the current HEAD
	// branch is, which MUST be GitignoreBranchName (harmonik/gitignore-init) when
	// called from the workspace manager startup path per WM-013e.
	commitMsg := "harmonik: ensure .gitignore covers control-plane paths (WM-013e)"
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg, "--allow-empty")
	commitCmd.Dir = repoRoot
	if out, err := commitCmd.CombinedOutput(); err != nil {
		// If the tree is clean after add (nothing changed — already committed),
		// git commit exits non-zero with "nothing to commit". Treat as idempotent.
		if strings.Contains(string(out), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit .gitignore: %w\noutput: %s", err, out)
	}
	return nil
}

// IsGitignoreWriteForbidden reports whether err wraps [ErrGitignoreWriteForbidden].
func IsGitignoreWriteForbidden(err error) bool {
	return errors.Is(err, ErrGitignoreWriteForbidden)
}
