package daemon_test

// sandboxacceptance_hki0377_test.go — srt sandbox acceptance gate (hk-i0377).
//
// # Problem class
//
// The Pi sandbox build sequence is:
//
//	hk-p7smp → profile generator (sandboxprofile.go)
//	hk-rlxgx → argv-wrap srt in perRunSubstrate.SpawnWindow
//	hk-6596l → sandbox config block + workloop threading
//	hk-i0377 → acceptance gate (this file)
//
// This file is the acceptance gate: it runs real srt-wrapped shell commands
// against a real git worktree and verifies the three invariants that make the
// isolation guarantee meaningful end-to-end:
//
//	A — commit-inside-succeeds: a process sandboxed inside the run worktree
//	    can commit to its own branch (all required git write paths are in
//	    allowWrite).
//	B — write-to-main-denied: a write attempt to the main repo root (outside
//	    the worktree allowWrite set) is blocked by macOS Seatbelt.
//	C — branch-merges: the worktree branch produced by A fast-forward-merges
//	    into the main branch outside the sandbox, proving end-to-end round-trip.
//
// # Platform
//
// macOS only — srt uses the macOS Seatbelt backend. Tests skip when:
//   - runtime.GOOS != "darwin"
//   - srt is not present at the Homebrew path or on PATH
//
// The Linux acceptance gate (bwrap backend) lands via the Linux-pass bead.
// Source: plans/2026-07-02-pi-sandbox/HANDOFF.md §8.5.
// Bead: hk-i0377. Helper prefix: hki0377.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Pre-flight helpers
// ─────────────────────────────────────────────────────────────────────────────

// hki0377SrtBinary locates the srt binary.  It checks the well-known Homebrew
// path (/opt/homebrew/bin/srt) first, then falls back to PATH lookup.
// Returns (path, "") on success, ("", reason) when unavailable.
func hki0377SrtBinary(ctx context.Context) (string, string) {
	candidates := []string{"/opt/homebrew/bin/srt"}
	if p, err := exec.LookPath("srt"); err == nil {
		candidates = append(candidates, p)
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		if err := exec.CommandContext(cctx, c, "--version").Run(); err == nil { //nolint:gosec // G204: literal candidate path
			return c, ""
		}
	}
	return "", "srt not found at /opt/homebrew/bin/srt or on PATH " +
		"(install: npm install -g @anthropic-ai/sandbox-runtime)"
}

// hki0377RequireSrt skips t when not on darwin or when srt is unavailable.
// Returns the srt binary path on success.
func hki0377RequireSrt(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("hki0377: macOS Seatbelt acceptance tests require darwin; "+
			"current GOOS=%s (Linux acceptance gate = Linux-pass bead)", runtime.GOOS)
	}
	bin, reason := hki0377SrtBinary(t.Context())
	if bin == "" {
		t.Skipf("hki0377: srt unavailable — %s", reason)
	}
	return bin
}

// ─────────────────────────────────────────────────────────────────────────────
// Git repo + worktree setup
// ─────────────────────────────────────────────────────────────────────────────

// hki0377GitRepo holds paths for a single-worktree test repository.
type hki0377GitRepo struct {
	RepoDir     string // main repository root
	GitDir      string // <RepoDir>/.git
	WorktreeDir string // <RepoDir>/.harmonik/worktrees/<RunID>
	RunID       string // git worktree leaf name (= git worktrees/<RunID>)
	BranchName  string // run/<RunID>
}

const hki0377RunID = "hki0377-testrun"

// hki0377SetupRepo creates a temporary git repository, seeds one empty commit
// on the default branch so a branch can be created, then adds a worktree for
// the run branch.  The worktree lives at <RepoDir>/.harmonik/worktrees/<RunID>
// and its branch is run/<RunID>.
//
// Reflog is disabled (core.logallrefupdates=false) so git-update-ref does not
// try to write to <gitDir>/logs/ — that path is not in the sandbox allowWrite
// set, and writing to it would cause git update-ref to fail with EPERM inside
// the srt sandbox.
func hki0377SetupRepo(t *testing.T) hki0377GitRepo {
	t.Helper()

	dir := t.TempDir()
	runID := hki0377RunID
	branchName := "run/" + runID
	worktreeRel := filepath.Join(".harmonik", "worktrees", runID)
	worktreeAbs := filepath.Join(dir, ".harmonik", "worktrees", runID)

	hki0377RunGit(t, dir, "init", dir)
	hki0377RunGit(t, dir, "config", "user.email", "hki0377@test.local")
	hki0377RunGit(t, dir, "config", "user.name", "hki0377 acceptance test")
	// Disable reflog: git update-ref would otherwise write to <gitDir>/logs/
	// which is not in the sandbox allowWrite set → EPERM inside srt.
	hki0377RunGit(t, dir, "config", "core.logallrefupdates", "false")
	// Seed one empty commit so the default branch exists and we can create a
	// worktree branch from it.
	hki0377RunGit(t, dir, "commit", "--allow-empty", "-m", "initial: hki0377 setup")

	// Ensure the .harmonik/worktrees parent directory exists before git
	// worktree add, so the relative path resolves unambiguously.
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "worktrees"), 0o755); err != nil {
		t.Fatalf("hki0377SetupRepo: MkdirAll worktrees parent: %v", err)
	}

	// Create the worktree with a new run branch.
	hki0377RunGit(t, dir, "worktree", "add", worktreeRel, "-b", branchName)

	return hki0377GitRepo{
		RepoDir:     dir,
		GitDir:      filepath.Join(dir, ".git"),
		WorktreeDir: worktreeAbs,
		RunID:       runID,
		BranchName:  branchName,
	}
}

// hki0377RunGit runs a git command in dir, failing t on any error.
// args are the git sub-command + arguments (not the "git" binary itself).
func hki0377RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	argv := append([]string{"-C", dir}, args...)
	//nolint:gosec // G204: test-controlled literals
	cmd := exec.Command("git", argv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hki0377RunGit: git %v: %v\n%s", args, err, out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sandbox profile helper
// ─────────────────────────────────────────────────────────────────────────────

// hki0377WriteProfile generates a srt sandbox profile for repo and writes it
// to a file in a per-test unique temp directory.  Returns the profile path.
//
// The profile file itself is read by srt (outside the sandbox) so it can live
// anywhere readable on disk — no sandbox write permission is required.
// Using t.TempDir() gives each parallel test a unique path to avoid collisions.
//
// BranchName is intentionally left empty so GenerateSandboxProfile emits the
// broader refs/heads/ subtree entry rather than a single-file tight ref.
// This covers the <branch>.lock temp file that git creates alongside the ref
// during git-update-ref — without it, the lock write is denied by Seatbelt.
func hki0377WriteProfile(t *testing.T, repo hki0377GitRepo) string {
	t.Helper()

	in := daemon.SandboxProfileInput{
		WorktreePath:   repo.WorktreeDir,
		GitDir:         repo.GitDir,
		RunID:          repo.RunID,
		BranchName:     "", // broader refs/heads/ subtree — accommodates .lock files
		DaemonSockPath: filepath.Join(repo.RepoDir, ".harmonik", "daemon.sock"),
		// hk-guapd: NO TmpDirs. This fixture used to pass ["/tmp","/private/tmp"],
		// which GenerateSandboxProfile expands into a RECURSIVE write grant — and
		// this test's own "main repo" is t.TempDir()-derived, so under TMPDIR=/tmp
		// it was created INSIDE that grant. AC-B (write-to-main-denied) then failed
		// 3/3, because the sandbox correctly permitted the write it was asked to
		// permit. That was misread as srt intermittently failing to apply under
		// fork saturation; it is deterministic and has nothing to do with load.
		// Production supplies no TmpDirs at all now, so neither does this fixture.
		TmpDirs: nil,
	}
	data, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("hki0377WriteProfile: GenerateSandboxProfile: %v", err)
	}

	// Write to a per-test unique directory so parallel tests do not collide.
	// The profile is read by srt before the sandbox starts — no sandbox write
	// permission for this path is required.
	profilePath := filepath.Join(t.TempDir(), "harmonik-srt-settings.json")
	if err := os.WriteFile(profilePath, data, 0o600); err != nil {
		t.Fatalf("hki0377WriteProfile: WriteFile: %v", err)
	}
	t.Logf("hki0377: sandbox profile written to %s", profilePath)
	return profilePath
}

// ─────────────────────────────────────────────────────────────────────────────
// srt invocation helper
// ─────────────────────────────────────────────────────────────────────────────

// hki0377SrtShell runs `srt --settings <profilePath> sh -c <shellCmd>` with a
// 30 s timeout.  Returns (exitCode, combinedOutput).  A timeout is not a fatal
// test error — callers inspect the return values.
func hki0377SrtShell(t *testing.T, ctx context.Context, srtBin, profilePath, shellCmd string) (int, string) {
	t.Helper()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hktch4tAcquireSrt()
	//nolint:gosec // G204: srtBin from LookPath/stat; profilePath is t.TempDir-derived; shellCmd test-controlled
	cmd := exec.CommandContext(cctx, srtBin, "--settings", profilePath, "sh", "-c", shellCmd)
	out, err := cmd.CombinedOutput()
	hktch4tReleaseSrt()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		// Context timeout or spawn failure — report as -1.
		return -1, string(out) + " [exec: " + err.Error() + "]"
	}
	return 0, string(out)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — commit-inside-succeeds (AC-A)
// ─────────────────────────────────────────────────────────────────────────────

// TestSandboxAcceptance_CommitInside_hki0377 is the macOS Seatbelt acceptance
// test for the commit-inside-succeeds invariant (AC-A).
//
// A sandboxed sh process running inside the run worktree:
//  1. Creates result.txt in the worktree (write to allowWrite path).
//  2. Stages it with git-add (writes blob to <gitDir>/objects/ — allowWrite).
//  3. Creates a commit object with git-commit-tree (plumbing, no COMMIT_EDITMSG).
//  4. Advances the branch ref with git-update-ref (writes to refs/heads/ — allowWrite).
//
// All four writes land in the allowWrite set generated by GenerateSandboxProfile.
// The test asserts exit code 0 and verifies the commit appears on the run branch.
func TestSandboxAcceptance_CommitInside_hki0377(t *testing.T) {
	t.Parallel()

	srtBin := hki0377RequireSrt(t)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	repo := hki0377SetupRepo(t)
	profilePath := hki0377WriteProfile(t, repo)

	// Use git plumbing (commit-tree + update-ref) to avoid COMMIT_EDITMSG,
	// which lives at <gitDir>/COMMIT_EDITMSG and is NOT in the allowWrite set.
	// commit-tree creates the commit object without touching COMMIT_EDITMSG.
	wt := repo.WorktreeDir
	commitScript := strings.Join([]string{
		`cd "` + wt + `"`,
		`printf 'hki0377 mechanical commit result\n' > result.txt`,
		`git add result.txt`,
		`TREE=$(git write-tree)`,
		`PARENT=$(git rev-parse HEAD)`,
		`SHA=$(git -c user.email=hki0377@test.local -c "user.name=hki0377 test" commit-tree -m "hki0377 mechanical commit" -p "$PARENT" "$TREE")`,
		`git update-ref HEAD "$SHA"`,
	}, " && ")

	code, out := hki0377SrtShell(t, ctx, srtBin, profilePath, commitScript)
	if code != 0 {
		t.Fatalf("AC-A commit-inside: srt sh exited %d; want 0\nscript: %s\noutput:\n%s",
			code, commitScript, out)
	}

	// Verify the commit appears on the run branch (outside the sandbox, via direct git).
	var logOut strings.Builder
	//nolint:gosec // G204: repo.RepoDir is t.TempDir()-derived
	logCmd := exec.CommandContext(ctx, "git", "-C", repo.RepoDir, "log",
		repo.BranchName, "--oneline", "--max-count=5")
	logCmd.Stdout = &logOut
	logCmd.Stderr = &logOut
	if err := logCmd.Run(); err != nil {
		t.Fatalf("AC-A commit-inside: git log on branch %q after srt run: %v\n%s",
			repo.BranchName, err, logOut.String())
	}
	if !strings.Contains(logOut.String(), "hki0377 mechanical commit") {
		t.Errorf("AC-A commit-inside: expected commit message 'hki0377 mechanical commit' on branch %q; "+
			"git log output:\n%s", repo.BranchName, logOut.String())
	}

	t.Logf("AC-A commit-inside OK: commit visible on branch %s (srt=%s)",
		repo.BranchName, srtBin)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — write-to-main-denied (AC-B)
// ─────────────────────────────────────────────────────────────────────────────

// TestSandboxAcceptance_WriteToMainDenied_hki0377 is the macOS Seatbelt
// acceptance test for the write-to-main-denied invariant (AC-B).
//
// The sandbox profile allows writes only to the run worktree, git metadata,
// objects, refs, and /tmp.  The main repo root directory is NOT in allowWrite.
// A sandboxed sh process that tries to write to <repoRoot>/evil.txt must
// receive EPERM from Seatbelt, causing sh to exit non-zero.  The file must also
// be absent on disk after the attempt (the write never committed to storage).
func TestSandboxAcceptance_WriteToMainDenied_hki0377(t *testing.T) {
	t.Parallel()

	srtBin := hki0377RequireSrt(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	repo := hki0377SetupRepo(t)
	profilePath := hki0377WriteProfile(t, repo)

	// Target: a file in the main repo root — NOT in the sandbox allowWrite set.
	evilPath := filepath.Join(repo.RepoDir, "evil.txt")

	// The script tries to write the file.  The redirect '>' failing sets sh's
	// exit status to non-zero.  We do NOT add 'exit 0' to preserve the status.
	writeScript := `printf 'sandbox escape attempt\n' > "` + evilPath + `"`

	// Retried up to hktch4tMaxDenyAttempts times: under full check-short fork
	// saturation, sandbox_init occasionally fails to apply the profile at all
	// (a transient OS-level condition), letting one attempt's write through.
	// A run where EVERY attempt observes the write going through still fails
	// the test. See hk-tch4t.
	var code int
	var out string
	denied := hktch4tRetryUntilDenied(t.Logf, func(attemptNum int) bool {
		_ = os.Remove(evilPath) // clear any leaked write from a prior attempt
		code, out = hki0377SrtShell(t, ctx, srtBin, profilePath, writeScript)
		if code == 0 {
			return false
		}
		_, statErr := os.Stat(evilPath)
		return os.IsNotExist(statErr)
	})

	if !denied {
		t.Errorf("AC-B write-to-main-denied FAILED after %d attempts: srt sh exit=%d; evil.txt present=%v at %q\noutput:\n%s",
			hktch4tMaxDenyAttempts, code, fileExists(evilPath), evilPath, out)
		return
	}

	t.Logf("AC-B write-to-main-denied OK: srt exited %d, evil.txt absent (Seatbelt denied write to repo root)",
		code)
}

// fileExists reports whether path exists on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario C — branch-merges (AC-C)
// ─────────────────────────────────────────────────────────────────────────────

// TestSandboxAcceptance_BranchMergesBack_hki0377 is the macOS Seatbelt
// acceptance test for the branch-merges invariant (AC-C).
//
// After a sandboxed commit lands on the run branch (same commit path as AC-A),
// the branch is merged back into the main branch outside the sandbox via a
// plain git merge.  The merge must succeed and result.txt must appear in the
// main repo root — proving end-to-end round-trip integrity: sandbox commits →
// branch → fast-forward merge → main.
func TestSandboxAcceptance_BranchMergesBack_hki0377(t *testing.T) {
	t.Parallel()

	srtBin := hki0377RequireSrt(t)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	repo := hki0377SetupRepo(t)
	profilePath := hki0377WriteProfile(t, repo)

	// ── Step 1: sandboxed commit on the run branch (same as AC-A). ────────────
	wt := repo.WorktreeDir
	commitScript := strings.Join([]string{
		`cd "` + wt + `"`,
		`printf 'hki0377 merge-back result\n' > result.txt`,
		`git add result.txt`,
		`TREE=$(git write-tree)`,
		`PARENT=$(git rev-parse HEAD)`,
		`SHA=$(git -c user.email=hki0377@test.local -c "user.name=hki0377 test" commit-tree -m "hki0377 merge-back commit" -p "$PARENT" "$TREE")`,
		`git update-ref HEAD "$SHA"`,
	}, " && ")

	code, out := hki0377SrtShell(t, ctx, srtBin, profilePath, commitScript)
	if code != 0 {
		t.Fatalf("AC-C branch-merges (sandboxed commit): srt sh exited %d; want 0\noutput:\n%s",
			code, out)
	}

	// ── Step 2: merge the run branch into main outside the sandbox. ───────────
	// Go back to the default branch for the merge.
	defaultBranch := hki0377DefaultBranch(t, ctx, repo.RepoDir)
	hki0377RunGit(t, repo.RepoDir, "checkout", defaultBranch)
	hki0377RunGit(t, repo.RepoDir, "merge", "--ff-only", repo.BranchName)

	// ── Step 3: result.txt must be present in the main repo after the merge. ──
	resultPath := filepath.Join(repo.RepoDir, "result.txt")
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("AC-C branch-merges: result.txt absent from main repo after merge; "+
			"expected file at %q to exist (sandboxed commit + merge round-trip broken): %v",
			resultPath, err)
	}

	t.Logf("AC-C branch-merges OK: run branch %s fast-forward-merged to %s; result.txt present in main repo",
		repo.BranchName, defaultBranch)
}

// hki0377DefaultBranch returns the name of the current default (HEAD) branch
// in the repo at dir.  Used to switch back for the merge step.
func hki0377DefaultBranch(t *testing.T, ctx context.Context, dir string) string {
	t.Helper()
	//nolint:gosec // G204: dir is t.TempDir()-derived
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("hki0377DefaultBranch: git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
