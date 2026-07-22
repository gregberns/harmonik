// Package gitprobe holds the read-only git probes that both internal/daemon and
// the extracted harness packages need.
//
// # Why this package exists
//
// P2 unit E1a extracts the codex harness out of internal/daemon. A compiler probe
// of the codex file set found exactly two symbols reaching back into daemon, and
// resolveWorktreeHEADVia was one of them. It is not harness-private: its callers
// span codexcommit, picommit, dot_cascade, reviewloop, workloop and pasteinject,
// and the pi harness (E1c) reaches the identical symbol. Injecting it as a
// function field would have paid for a seam twice and invented one the extraction
// plan says not to invent; duplicating it would have forked a git probe. So the
// trio moves here, where daemon and harness both import it and neither owns it.
//
// The package is a leaf: stdlib plus the pre-existing tmux.CommandRunner seam. It
// adds no dependency that its callers did not already have.
//
// Bead: hk-04q2j-adjacent P2/E1a extraction. Origin: internal/daemon/pasteinject.go
// (hk-rs-b9-liveness-1m9n) and internal/daemon/sessioncontext_chb023.go (CHB-023).
package gitprobe

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ResolveWorktreeHEAD returns the current HEAD commit SHA in the worktree at
// wtPath, probing box A's local filesystem with a bare exec.
func ResolveWorktreeHEAD(ctx context.Context, wtPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitprobe: ResolveWorktreeHEAD: git rev-parse HEAD in %q: %w", wtPath, err)
	}
	sha := trimTrailingNewlines(string(out))
	if sha == "" {
		return "", fmt.Errorf("gitprobe: ResolveWorktreeHEAD: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return sha, nil
}

// ResolveWorktreeHEADVia is like ResolveWorktreeHEAD but routes the git probe
// through runner instead of bare exec.CommandContext. Uses `git -C <wtPath>
// rev-parse HEAD` (the -C form works for both local and SSH runners).
//
// When runner is nil the call delegates to the bare-local ResolveWorktreeHEAD, so
// callers can pass the per-run runner unconditionally and get byte-identical local
// behaviour for LOCAL runs (nil runner) — NFR7.
func ResolveWorktreeHEADVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	if runner == nil {
		return ResolveWorktreeHEAD(ctx, wtPath)
	}
	out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("gitprobe: ResolveWorktreeHEADVia: git -C %q rev-parse HEAD: %w", wtPath, err)
	}
	sha := trimTrailingNewlines(string(out))
	if sha == "" {
		return "", fmt.Errorf("gitprobe: ResolveWorktreeHEADVia: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return sha, nil
}

// RevParse runs `git rev-parse <ref>` in repoRoot and returns the trimmed
// SHA on success. On non-zero exit it returns an error.
func RevParse(ctx context.Context, repoRoot, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	sha := strings.TrimRight(string(out), "\n")
	if sha == "" {
		return "", fmt.Errorf("git rev-parse %s: empty output", ref)
	}
	return sha, nil
}

// RunnerIsLocalFS reports whether r operates on box A's local filesystem — i.e.
// the worktree paths it is given are directly stat-able with os.Stat. A nil runner
// (defensive) and tmux.LocalRunner both qualify; an SSHRunner (or any other
// transport) does NOT, because its worktree lives on a remote worker.
func RunnerIsLocalFS(r tmux.CommandRunner) bool {
	switch r.(type) {
	case nil, tmux.LocalRunner:
		return true
	default:
		return false
	}
}

// trimTrailingNewlines strips every trailing newline from git's stdout.
func trimTrailingNewlines(s string) string {
	return strings.TrimRight(s, "\n")
}
