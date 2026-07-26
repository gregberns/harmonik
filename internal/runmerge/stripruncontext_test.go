package runmerge_test

// stripruncontext_test.go — tests for StripRunContextFromMerge (hk-4je).
//
// The function is the last gate that keeps .harmonik/run-context/** off the
// merge target: its single caller (prepareInitialMerge, merge.go) turns any
// error into a terminal `strip_run_context_failed` Outcome and aborts the
// merge, while a (false, nil) return lets the fast-forward proceed. So the
// distinction between "no-op" and "error" IS the safety property, and each of
// the three exits is covered here:
//
//   - wtPath does not exist            → (false, nil)  no-op. This is the
//     LEGITIMATE remote-run case: RunBranchToTarget's hk-sfy7f fallback tries
//     `git worktree add` and, when that fails, the merge continues with no
//     local worktree. Stripping is impossible and unnecessary there, so the
//     no-op must survive.
//   - wtPath stat fails for any OTHER  → (false, err). The index state is
//     reason                             unknown; reporting a clean no-op would
//     fast-forward the target with run-context files still in the tree.
//   - run-context tracked in the index → (true, nil) plus a strip commit.
//
// The error case is reproduced portably with ENOTDIR — a regular file used as
// a path component. No chmod, no root/non-root dependency, same result on
// linux and darwin.

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/runmerge"
)

// stripFixtureRepo initialises a git repo in a fresh temp dir and returns its
// path. Reuses the package's mergeToMainFixtureGitRepo so the git identity and
// initial commit match every other merge-path test in this package.
func stripFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mergeToMainFixtureGitRepo(t, dir)
	return dir
}

// stripFixtureGit runs a git command in dir, failing the test on error.
func stripFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// stripFixtureCommitRunContext force-commits a run-context file into the repo,
// mirroring what CHB-023 (sessioncontext) does on the run branch.
func stripFixtureCommitRunContext(t *testing.T, dir, runID string) string {
	t.Helper()
	rcDir := filepath.Join(dir, runmerge.RunContextDirPrefix, runID)
	// 0o750 / 0o600 rather than the 0755 / 0644 the older fixtures in this
	// package use: at those modes gosec G301/G306 fire and would need a lint
	// suppression, and a suppression that a permission change removes outright
	// is masking rather than an idiom.
	if err := os.MkdirAll(rcDir, 0o750); err != nil {
		t.Fatalf("mkdir run-context dir: %v", err)
	}
	rcPath := filepath.Join(rcDir, "context.json")
	if err := os.WriteFile(rcPath, []byte(`{"session_id":"abc"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write context.json: %v", err)
	}
	// -f mirrors CHB-023: the path may be gitignored in a real project.
	stripFixtureGit(t, dir, "add", "-f", "--", runmerge.RunContextDirPrefix)
	stripFixtureGit(t, dir, "commit", "-m", "chore: record run context")
	return rcPath
}

// stripFixtureTrackedRunContext returns the tracked .harmonik/run-context/**
// entries in the repo index.
func stripFixtureTrackedRunContext(t *testing.T, dir string) string {
	t.Helper()
	return stripFixtureGit(t, dir, "ls-files", "--cached", "--", runmerge.RunContextDirPrefix)
}

// TestStripRunContextFromMerge_MissingWorktreeIsNoOp pins the remote-run
// fallback: RunBranchToTarget (merge.go, hk-sfy7f) proceeds with no local
// worktree when `git worktree add` fails, and the strip must then short-circuit
// to a clean no-op rather than failing the merge. ENOENT is the ONLY stat error
// allowed to do that.
func TestStripRunContextFromMerge_MissingWorktreeIsNoOp(t *testing.T) {
	t.Parallel()

	wtPath := filepath.Join(t.TempDir(), "run-worktree-that-was-never-created")

	// Precondition: the path really is absent, i.e. the stat error really is
	// ENOENT and not something else that happens to look like it.
	if _, statErr := os.Stat(wtPath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("fixture: want an fs.ErrNotExist stat error for %s, got %v", wtPath, statErr)
	}

	stripped, err := runmerge.StripRunContextFromMerge(t.Context(), wtPath)
	if err != nil {
		t.Fatalf("StripRunContextFromMerge on an absent worktree must be a clean no-op, got error: %v", err)
	}
	if stripped {
		t.Error("StripRunContextFromMerge reported stripped=true with no worktree to strip")
	}
}

// TestStripRunContextFromMerge_NonNotExistStatErrorFails is the regression test
// for the fix: a stat failure that is NOT "does not exist" leaves the index
// state unknown, so it must surface as an error. Returning (false, nil) here
// would let prepareInitialMerge fast-forward the target with
// .harmonik/run-context/** still present — the one outcome this function exists
// to prevent.
//
// ENOTDIR is used because it is portable and needs no privilege games: a
// regular file used as a path component fails stat with ENOTDIR on every POSIX
// platform, and errors.Is(err, fs.ErrNotExist) is false for it.
func TestStripRunContextFromMerge_NonNotExistStatErrorFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("regular file\n"), 0o600); err != nil {
		t.Fatalf("fixture: write regular file: %v", err)
	}
	// A regular file used as a path COMPONENT → ENOTDIR, not ENOENT.
	wtPath := filepath.Join(filePath, "worktree")

	// Precondition: the stat error must be non-nil and must NOT satisfy
	// fs.ErrNotExist, or the test would prove nothing about the new branch.
	_, statErr := os.Stat(wtPath)
	if statErr == nil {
		t.Fatalf("fixture: os.Stat(%s) unexpectedly succeeded; ENOTDIR setup is wrong", wtPath)
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("fixture: os.Stat(%s) reported fs.ErrNotExist (%v); "+
			"this platform does not produce ENOTDIR for a file used as a path component", wtPath, statErr)
	}

	stripped, err := runmerge.StripRunContextFromMerge(t.Context(), wtPath)
	if err == nil {
		t.Fatal("StripRunContextFromMerge must fail on a non-ENOENT stat error; " +
			"a silent no-op lets the caller fast-forward the target with run-context still present")
	}
	if stripped {
		t.Error("StripRunContextFromMerge reported stripped=true on a stat failure")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("returned error must not be an fs.ErrNotExist: %v", err)
	}
	if !strings.Contains(err.Error(), wtPath) {
		t.Errorf("error should name the worktree path %s, got: %v", wtPath, err)
	}
}

// TestStripRunContextFromMerge_StripsTrackedRunContext covers the normal path:
// a run branch carrying a CHB-023 context.json commit is stripped, the removal
// is committed (so the FF target gets a clean tree), and the working-tree file
// survives (`git rm --cached`).
func TestStripRunContextFromMerge_StripsTrackedRunContext(t *testing.T) {
	t.Parallel()

	dir := stripFixtureRepo(t)
	rcPath := stripFixtureCommitRunContext(t, dir, "0198f0c0-1111-7000-8000-00000000abcd")

	if tracked := stripFixtureTrackedRunContext(t, dir); tracked == "" {
		t.Fatal("fixture: expected run-context to be tracked before the strip")
	}
	headBefore := stripFixtureGit(t, dir, "rev-parse", "HEAD")

	stripped, err := runmerge.StripRunContextFromMerge(t.Context(), dir)
	if err != nil {
		t.Fatalf("StripRunContextFromMerge: %v", err)
	}
	if !stripped {
		t.Fatal("StripRunContextFromMerge reported stripped=false with run-context tracked")
	}

	if tracked := stripFixtureTrackedRunContext(t, dir); tracked != "" {
		t.Errorf("run-context still tracked after the strip:\n%s", tracked)
	}
	if headAfter := stripFixtureGit(t, dir, "rev-parse", "HEAD"); headAfter == headBefore {
		t.Error("HEAD did not advance: the strip commit is what the fast-forward carries to the target")
	}
	// --cached: the removal is index-only, so the crash-recovery reader still
	// sees the file in the worktree for the rest of the merge.
	if _, statErr := os.Stat(rcPath); statErr != nil {
		t.Errorf("git rm --cached must leave the working-tree file in place: %v", statErr)
	}
}

// TestStripRunContextFromMerge_NoTrackedRunContextIsNoOp covers the common case
// where a run never wrote a context.json: nothing tracked, so no strip commit
// and no HEAD movement.
func TestStripRunContextFromMerge_NoTrackedRunContextIsNoOp(t *testing.T) {
	t.Parallel()

	dir := stripFixtureRepo(t)
	headBefore := stripFixtureGit(t, dir, "rev-parse", "HEAD")

	stripped, err := runmerge.StripRunContextFromMerge(t.Context(), dir)
	if err != nil {
		t.Fatalf("StripRunContextFromMerge: %v", err)
	}
	if stripped {
		t.Error("StripRunContextFromMerge reported stripped=true with nothing tracked")
	}
	if headAfter := stripFixtureGit(t, dir, "rev-parse", "HEAD"); headAfter != headBefore {
		t.Errorf("HEAD moved (%s → %s) with nothing to strip", headBefore, headAfter)
	}
}
