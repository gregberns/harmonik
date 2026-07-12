package daemon_test

// mergetomain_hksfy7f_test.go — regression test for the no-local-worktree
// merge failure introduced by hk-sfy7f.
//
// Scenario (remote-run starvation):
//
//   1. A bead run is dispatched; its worktree lives on a REMOTE worker.
//      preMergeSync fetches refs/heads/run/<id> to box-A but does NOT
//      create a local worktree at WorktreePath(projectDir, runID, ...).
//
//   2. During the multi-minute run, a COMPETING bead merges to main
//      (main advances from SHA_0 → SHA_1).
//
//   3. When this bead's merge step runs, wtPath does not exist on box-A,
//      so the step-2 rebase is skipped.  runTip is still based on SHA_0.
//      mainTip = SHA_1.  The FF-check fails: SHA_1 is not an ancestor of
//      SHA_0-based runTip.
//
//   4. The hk-1u4wp FF-check retry also cannot rebase (still no wtPath),
//      so all maxPushAttempts fail terminally with
//      "non_ff_merge: main advanced concurrently".
//
// The hk-sfy7f fix: when wtPath does not exist, attempt to create a
// temporary local worktree via `git worktree add` so the step-2 rebase
// can proceed.
//
// This test simulates the remote-run state by:
//   (a) creating the run-branch (with an agent commit) in a TEMPORARY
//       worktree that is NOT at the canonical WorktreePath;
//   (b) removing that temporary worktree so no local wtPath exists;
//   (c) calling ExportedMergeRunBranchToMain directly (no full work-loop
//       overhead, since we only care about the merge step).
//
// Assertions:
//
//	(A) merge succeeds (success=true, reason="").
//	(B) refs/heads/main advances past mainSHABefore.
//	(C) The final main tree contains BOTH the competing commit (DIVERGE)
//	    AND the agent's work (work.txt) — the rebase onto the new main
//	    preserved both sets of changes.
//	(D) The final push reached the bare remote (origin/main == new local main).
//
// Spec ref: specs/execution-model.md §4.12.EM-052 step 2 (hk-sfy7f amendment).
// Bead: hk-sfy7f.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workspace"
)

// TestMergeToMain_NoLocalWorktreeRebase verifies that mergeRunBranchToMain
// succeeds when the canonical wtPath does not exist on box-A (remote-run case)
// and main has advanced past the run-branch fork point (hk-sfy7f).
func TestMergeToMain_NoLocalWorktreeRebase(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-noworktree-rebase-bead-sfy7f")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Bare remote so the push (step 5 of EM-052) succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	addRemoteCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addRemoteCmd.Dir = projectDir
	if out, err := addRemoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	// Push the initial main commit to origin.
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (prime): %v\n%s", err, out)
	}

	// Record the fork-point SHA (headSHA at dispatch time).
	headSHA := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	// ── Step 2: Simulate a competing merge landing during the run window. ────
	// Advance main with a non-conflicting commit (DIVERGE file).  Also push
	// to origin so remote and local are aligned at SHA_1.
	mergeToMainFixtureAdvanceMain(t, projectDir)
	pushAdvancedCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushAdvancedCmd.Dir = projectDir
	if out, err := pushAdvancedCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (after advance): %v\n%s", err, out)
	}

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	// ── Step 3: Create the run-branch with agent work, without a local wtPath. ─
	//
	// In production, preMergeSync fetches refs/heads/run/<id> from the worker.
	// Here we simulate that by creating a worktree at a TEMP location (not the
	// canonical WorktreePath), committing agent work, then removing the worktree.
	// The branch ref survives the worktree removal.
	runID := core.RunID(uuid.New())
	runBranch := workspace.TaskBranchName(runID.String())

	// Canonical wtPath that mergeRunBranchToMain will look up — must NOT exist.
	canonWtPath := workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())
	if _, err := os.Stat(canonWtPath); err == nil {
		t.Fatalf("canonWtPath %s unexpectedly exists before test setup", canonWtPath)
	}

	// Create a temp worktree (NOT at canonWtPath) to commit agent work.
	tmpWt := t.TempDir()
	addWtCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", runBranch, tmpWt, headSHA)
	addWtCmd.Dir = projectDir
	if out, err := addWtCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add (temp commit worktree): %v\n%s", err, out)
	}

	// Write and commit agent work inside the temp worktree.
	workFile := filepath.Join(tmpWt, "work.txt")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(workFile, []byte("agent work (hk-sfy7f test)\n"), 0o644); err != nil {
		t.Fatalf("WriteFile work.txt: %v", err)
	}
	for _, args := range [][]string{
		{"add", "work.txt"},
		{"commit", "-m", "feat: agent work (hk-sfy7f)", "--trailer", "Harmonik-Run-ID: " + runID.String()},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = tmpWt
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (in temp worktree): %v\n%s", args, err, out)
		}
	}

	// Remove the temp worktree — branch ref survives, no local wtPath remains.
	rmWtCmd := exec.CommandContext(t.Context(), "git", "worktree", "remove", "--force", tmpWt)
	rmWtCmd.Dir = projectDir
	if out, err := rmWtCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree remove (cleanup temp): %v\n%s", err, out)
	}

	// Confirm: canonWtPath does not exist (the precondition for the bug).
	if _, err := os.Stat(canonWtPath); err == nil {
		t.Fatalf("canonWtPath %s should not exist after removing temp worktree", canonWtPath)
	}
	// Confirm: run-branch exists in the repo (simulates preMergeSync result).
	verifyCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "refs/heads/"+runBranch)
	verifyCmd.Dir = projectDir
	if out, err := verifyCmd.CombinedOutput(); err != nil {
		t.Fatalf("run-branch %s not found in projectDir after temp worktree removal: %v\n%s", runBranch, err, out)
	}

	// ── Step 4: call mergeRunBranchToMain directly. ─────────────────────────
	collector := &stubEventCollector{}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedMergeRunBranchToMain(
		ctx,
		projectDir,
		runID,
		collector,
		beadID,
		headSHA,
		"main",
		nil,  // protectBranches
		"",   // brPath
	)

	// ── Assertion (A): merge succeeded. ─────────────────────────────────────
	if !result.Success() {
		t.Errorf("mergeRunBranchToMain returned failure: reason=%q (want success; hk-sfy7f fix missing or broken)", result.Reason())
	}
	if result.NoChange() {
		t.Errorf("mergeRunBranchToMain returned noChange=true; expected real merge (agent commit must land)")
	}

	// ── Assertion (B): refs/heads/main advanced. ─────────────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("refs/heads/main unchanged after merge (still %s); expected it to advance", mainSHABefore)
	}

	// ── Assertion (C): final tree contains BOTH the competing commit AND the
	// agent work — the rebase incorporated both. ─────────────────────────────
	for _, f := range []string{"DIVERGE", "work.txt"} {
		showCmd := exec.CommandContext(t.Context(), "git", "show", "main:"+f)
		showCmd.Dir = projectDir
		if out, err := showCmd.CombinedOutput(); err != nil {
			t.Errorf("git show main:%s: %v\n%s (main should contain BOTH competing advance and agent work)", f, err, out)
		}
	}

	// ── Assertion (D): remote's main matches the new local main. ─────────────
	remoteRevCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "main")
	remoteRevCmd.Dir = originDir
	remoteRevOut, remoteRevErr := remoteRevCmd.Output()
	if remoteRevErr != nil {
		t.Fatalf("git rev-parse main (origin): %v", remoteRevErr)
	}
	remoteSHA := strings.TrimRight(string(remoteRevOut), "\n")
	if remoteSHA != mainSHAAfter {
		t.Errorf("origin main = %s; want %s (local main after no-worktree rebase)", remoteSHA[:8], mainSHAAfter[:8])
	}

	t.Logf("hk-sfy7f no-local-worktree rebase OK: main %s → %s, remote %s",
		mainSHABefore[:8], mainSHAAfter[:8], remoteSHA[:8])
}
