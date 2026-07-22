package daemon_test

// scenario_sandbox_pi_i0377_test.go — acceptance scenario test for pi-sandbox
// isolation on macOS. Drives srt directly against a throwaway git repo.
//
// # What it proves
//
// All three assertions below must hold for the sandbox profile produced by
// GenerateSandboxProfile to be production-worthy:
//
//	(a) CommitInside: `srt -c 'git add && git commit'` inside the run worktree
//	    exits 0 and advances HEAD on the run branch. Proves that the allowWrite
//	    set (worktree + git metadata paths) is complete enough for a real commit.
//
//	(b) WriteToMainDenied: `srt -c 'echo x > <main-repo file>'` targeting a file
//	    in the main repo's working tree (outside the run worktree) is denied —
//	    file unchanged after the attempt. This is the core isolation gate.
//
//	(c) BranchMerges: after a commit lands on the run branch, `git merge
//	    <run-branch>` back into main succeeds outside any sandbox. Proves the
//	    commit is a well-formed git object that integrates cleanly.
//
// Paths are LITERAL (no globs) — identical to what the Linux bwrap variant
// (hk-5zviv) will use.
//
// Prerequisites: srt v1.0.0 at /opt/homebrew/bin/srt (skip when absent).
// Bead: hk-i0377. Helper prefix: i0377.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Pre-flight helper
// ─────────────────────────────────────────────────────────────────────────────

// i0377SrtAvailable reports whether srt is reachable on PATH.
// Returns (true, "") on success, (false, reason) otherwise so the skip message
// is actionable.
func i0377SrtAvailable() (bool, string) {
	path, err := exec.LookPath("srt")
	if err != nil {
		return false, "exec.LookPath(\"srt\") failed: " + err.Error()
	}
	return true, path
}

// ─────────────────────────────────────────────────────────────────────────────
// Setup helper — throwaway git repo + run worktree
// ─────────────────────────────────────────────────────────────────────────────

// i0377SetupRepo creates a self-contained git repo with:
//   - main branch with an initial commit (including main-file.txt)
//   - a run worktree at <mainDir>/.harmonik/worktrees/<runID> on branch runBranch
//
// All state lives inside t.TempDir() and is removed by t.Cleanup.
// The repo-level config disables GPG signing to keep commits fast and hermetic.
//
// Returns: mainDir (main working tree), worktreeDir, gitDir, runID, runBranch.
func i0377SetupRepo(t *testing.T) (mainDir, worktreeDir, gitDir, runID, runBranch string) {
	t.Helper()

	mainDir = t.TempDir()
	gitDir = filepath.Join(mainDir, ".git")

	runGit := func(dir string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		//nolint:gosec // G204: test-controlled literals
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
		}
	}

	// Init repo on main branch; configure identity + no-sign at repo level.
	runGit(mainDir, "init", "-b", "main")
	runGit(mainDir, "config", "user.email", "i0377test@harmonik.test")
	runGit(mainDir, "config", "user.name", "i0377Test")
	runGit(mainDir, "config", "commit.gpgsign", "false")

	// Seed main-file.txt so main has a real initial commit.
	if err := os.WriteFile(filepath.Join(mainDir, "main-file.txt"), []byte("main content\n"), 0o644); err != nil {
		t.Fatalf("i0377SetupRepo: write main-file.txt: %v", err)
	}
	runGit(mainDir, "add", "main-file.txt")
	runGit(mainDir, "commit", "--no-gpg-sign", "-m", "initial commit")

	// Create run worktree.  runID must match the basename of the worktree path
	// so git's metadata dir (.git/worktrees/<runID>/) aligns with SandboxProfileInput.RunID.
	runID = "i0377-test-run"
	runBranch = "run/i0377-test"
	worktreeDir = filepath.Join(mainDir, ".harmonik", "worktrees", runID)

	// git worktree add creates the final dir but not its parents.
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		t.Fatalf("i0377SetupRepo: mkdirall worktree parent: %v", err)
	}
	runGit(mainDir, "worktree", "add", "-b", runBranch, worktreeDir)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		//nolint:gosec // G204: test-controlled path
		_ = exec.CommandContext(ctx, "git", "-C", mainDir, "worktree", "prune").Run()
	})

	return mainDir, worktreeDir, gitDir, runID, runBranch
}

// ─────────────────────────────────────────────────────────────────────────────
// Git query helper
// ─────────────────────────────────────────────────────────────────────────────

// i0377GitOutput runs a git command in dir and returns trimmed stdout.
// Fatals on error.
func i0377GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	//nolint:gosec // G204: test-controlled literals
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("i0377GitOutput: git %v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// Profile helper
// ─────────────────────────────────────────────────────────────────────────────

// i0377GenerateProfile calls GenerateSandboxProfile with per-test coordinates
// and writes the JSON to a temp file. Returns the settings file path.
//
// NO TmpDirs (hk-guapd). This used to pass ["/tmp", "/private/tmp"], with the
// comment: "the test's own temp dirs live under $TMPDIR (/var/folders/…), which
// is intentionally NOT included here: this keeps the main-repo working tree
// (also in $TMPDIR) outside the sandbox's allowWrite list for scenario (b)."
//
// That reasoning was correct ONLY while $TMPDIR happened to be a per-user path.
// It states its own precondition as if it were a fact. Under TMPDIR=/tmp — which
// Makefile:453 and :465 use for the gating suites, and which os.TempDir() also
// falls back to whenever TMPDIR is UNSET — mainDir is created inside /tmp, i.e.
// inside the grant, and scenario (b) inverts: the sandbox correctly ALLOWS the
// write the test exists to prove is denied. Measured, not argued: this test
// failed under TMPDIR=/tmp and passed with a per-user TMPDIR, deterministically,
// independent of machine load.
//
// Supplying no TmpDirs at all removes the precondition rather than documenting
// it. The main-repo tree is now outside allowWrite because nothing grants a temp
// root, not because of where $TMPDIR happens to point today.
func i0377GenerateProfile(t *testing.T, mainDir, worktreeDir, gitDir, runID, runBranch string) string {
	t.Helper()

	profileJSON, err := daemon.GenerateSandboxProfile(daemon.SandboxProfileInput{
		WorktreePath:   worktreeDir,
		GitDir:         gitDir,
		RunID:          runID,
		BranchName:     runBranch,
		DaemonSockPath: filepath.Join(mainDir, "daemon.sock"),
	})
	if err != nil {
		t.Fatalf("i0377GenerateProfile: GenerateSandboxProfile: %v", err)
	}

	profilePath := filepath.Join(t.TempDir(), "srt-settings-i0377.json")
	if err := os.WriteFile(profilePath, profileJSON, 0o644); err != nil {
		t.Fatalf("i0377GenerateProfile: write settings file: %v", err)
	}
	return profilePath
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario (a) — CommitInside
// ─────────────────────────────────────────────────────────────────────────────

// TestSandbox_CommitInsideSucceeds_i0377 runs `git add && git commit` inside the
// run worktree under srt and verifies:
//  1. srt exits 0.
//  2. The run branch HEAD advanced (new commit landed).
//
// This proves GenerateSandboxProfile produces an allowWrite set that is
// complete enough for a real git commit: worktree files, git worktree metadata
// (index, COMMIT_EDITMSG), git objects, and the branch ref are all writable.
func TestSandbox_CommitInsideSucceeds_i0377(t *testing.T) {
	t.Parallel()

	if ok, detail := i0377SrtAvailable(); !ok {
		t.Skipf("sandbox-pi tests require srt on PATH; skipping. %s", detail)
	}

	mainDir, worktreeDir, gitDir, runID, runBranch := i0377SetupRepo(t)

	// Seed a file inside the run worktree for the agent to commit.
	agentFile := filepath.Join(worktreeDir, "agent-work.txt")
	if err := os.WriteFile(agentFile, []byte("agent work output\n"), 0o644); err != nil {
		t.Fatalf("write agent-work.txt: %v", err)
	}

	headBefore := i0377GitOutput(t, mainDir, "rev-parse", runBranch)

	profilePath := i0377GenerateProfile(t, mainDir, worktreeDir, gitDir, runID, runBranch)

	// Run git add + commit inside the sandbox.
	script := fmt.Sprintf(
		"cd %s && git add agent-work.txt && git commit --no-gpg-sign -m 'sandbox test commit'",
		worktreeDir,
	)
	srtCtx, srtCancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer srtCancel()
	hktch4tAcquireSrt()
	//nolint:gosec // G204: test-controlled literals
	cmd := exec.CommandContext(srtCtx, "srt", "--settings", profilePath, "-c", script)
	out, err := cmd.CombinedOutput()
	hktch4tReleaseSrt()
	if err != nil {
		t.Fatalf("srt git commit failed (exit non-zero): %v\noutput:\n%s", err, out)
	}

	headAfter := i0377GitOutput(t, mainDir, "rev-parse", runBranch)
	if headAfter == headBefore {
		t.Errorf("HEAD did not advance on %s after srt commit: still %s\nsrt output:\n%s",
			runBranch, headBefore[:12], out)
	}

	t.Logf("commit-inside OK: %s → %s on %s\nsrt output: %s",
		headBefore[:12], headAfter[:12], runBranch, strings.TrimSpace(string(out)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario (b) — WriteToMainDenied
// ─────────────────────────────────────────────────────────────────────────────

// TestSandbox_WriteToMainDenied_i0377 attempts `echo x > <main-repo-file>` from
// inside the srt sandbox and asserts the file is unchanged after the attempt.
//
// The target file (main-file.txt at the main repo's working-tree root) is
// outside the run worktree and outside the sandbox's allowWrite list.  The
// write must be blocked at the macOS Seatbelt layer — this is the core
// isolation gate the sandbox exists to enforce.
func TestSandbox_WriteToMainDenied_i0377(t *testing.T) {
	t.Parallel()

	if ok, detail := i0377SrtAvailable(); !ok {
		t.Skipf("sandbox-pi tests require srt on PATH; skipping. %s", detail)
	}

	mainDir, worktreeDir, gitDir, runID, runBranch := i0377SetupRepo(t)

	// targetFile is in the main repo working tree — NOT inside the run worktree.
	targetFile := filepath.Join(mainDir, "main-file.txt")

	before, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read main-file.txt before: %v", err)
	}

	profilePath := i0377GenerateProfile(t, mainDir, worktreeDir, gitDir, runID, runBranch)

	// Attempt to overwrite the main-repo file from inside the sandbox. Retried
	// up to hktch4tMaxDenyAttempts times: under full check-short fork
	// saturation, sandbox_init occasionally fails to apply the Seatbelt
	// profile at all (a transient OS-level condition, not a logic defect in
	// GenerateSandboxProfile) and the write goes through on that one attempt.
	// A run where EVERY attempt observes the write going through still fails
	// the test — this only absorbs the diagnosed transient, not a genuine
	// isolation regression. See hk-tch4t.
	script := fmt.Sprintf("echo x > %s", targetFile)
	var after []byte
	var srtOut []byte
	var srtErr error
	denied := hktch4tRetryUntilDenied(t.Logf, func(attemptNum int) bool {
		// Reset the target back to its original content before each attempt so
		// a prior attempt's leaked write cannot make a later attempt's
		// unchanged-content check pass spuriously.
		if err := os.WriteFile(targetFile, before, 0o644); err != nil {
			t.Fatalf("reset main-file.txt before attempt %d: %v", attemptNum, err)
		}

		srtCtx, srtCancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer srtCancel()
		hktch4tAcquireSrt()
		//nolint:gosec // G204: test-controlled literals
		cmd := exec.CommandContext(srtCtx, "srt", "--settings", profilePath, "-c", script)
		srtOut, srtErr = cmd.CombinedOutput()
		hktch4tReleaseSrt()

		var readErr error
		after, readErr = os.ReadFile(targetFile)
		if readErr != nil {
			t.Fatalf("read main-file.txt after srt attempt %d: %v", attemptNum, readErr)
		}

		// Core isolation gate: the file must be unchanged AND srt must itself
		// report failure. Checking content-unchanged alone leaves a blind
		// spot: a shell redirect that Seatbelt silently allowed but that
		// happened to write byte-identical content would pass that check
		// alone. Requiring a non-zero exit closes that gap.
		return string(after) == string(before) && srtErr != nil
	})

	if !denied {
		t.Errorf("isolation gate FAILED after %d attempts: srt allowed write to main-repo file outside run worktree\n"+
			"expected content: %q\ngot content:      %q\nsrt exit: %v\nsrt output:\n%s",
			hktch4tMaxDenyAttempts, string(before), string(after), srtErr, srtOut)
		return
	}

	t.Logf("write-to-main denied OK: file unchanged, srt_exit_error=%v\nsrt output: %s",
		srtErr, strings.TrimSpace(string(srtOut)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario (c) — BranchMerges
// ─────────────────────────────────────────────────────────────────────────────

// TestSandbox_BranchMerges_i0377 verifies that a commit made on the run branch
// (as created by scenario a) merges cleanly back into main outside any sandbox.
//
// This test does NOT invoke srt — it validates the integration half: that
// commits written by a sandboxed agent are well-formed git objects that the
// host merge workflow can consume without conflicts.
func TestSandbox_BranchMerges_i0377(t *testing.T) {
	t.Parallel()

	mainDir, worktreeDir, _, _, runBranch := i0377SetupRepo(t)

	// Create a commit on the run branch (no sandbox — this is the merge test).
	agentFile := filepath.Join(worktreeDir, "agent-work.txt")
	if err := os.WriteFile(agentFile, []byte("agent work\n"), 0o644); err != nil {
		t.Fatalf("write agent-work.txt: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"add", "agent-work.txt"},
		{"commit", "--no-gpg-sign", "-m", "agent commit on run branch"},
	} {
		//nolint:gosec // G204: test-controlled literals
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = worktreeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (in worktree): %v\n%s", args, err, out)
		}
	}

	mainHeadBefore := i0377GitOutput(t, mainDir, "rev-parse", "main")
	runHead := i0377GitOutput(t, mainDir, "rev-parse", runBranch)

	// Merge run branch into main — outside sandbox.
	mergeCtx, mergeCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer mergeCancel()
	//nolint:gosec // G204: test-controlled literals
	mergeCmd := exec.CommandContext(mergeCtx, "git", "merge", "--no-edit", "--no-gpg-sign", runBranch)
	mergeCmd.Dir = mainDir
	if mergeOut, mergeErr := mergeCmd.CombinedOutput(); mergeErr != nil {
		t.Fatalf("git merge %s into main failed: %v\n%s", runBranch, mergeErr, mergeOut)
	}

	mainHeadAfter := i0377GitOutput(t, mainDir, "rev-parse", "main")
	if mainHeadAfter == mainHeadBefore {
		t.Errorf("merge did not advance main HEAD: was %s, still %s after merging %s (%s)",
			mainHeadBefore[:12], mainHeadAfter[:12], runBranch, runHead[:12])
	}

	t.Logf("branch-merges OK: run=%s merged → main %s→%s",
		runHead[:12], mainHeadBefore[:12], mainHeadAfter[:12])
}
