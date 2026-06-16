package daemon_test

// mergetomain_hkzmpd_test.go — regression test for the rebase-drop data-loss
// bug (hk-zmpd / re-file of hk-cmry).
//
// INCIDENT (2026-06-16 ~15:45Z): the daemon's merge-to-main rebase step
// silently dropped a reviewed/committed run-branch commit (hk-8prq, sha
// 6894b4de) when main had advanced with the same patch. git rebase exits 0 in
// this case (the commit is considered "already applied"), producing a run-branch
// tip equal to the target tip — but the daemon previously detected no problem
// and closed the bead as success, pushing a main that was missing the reviewed
// work and breaking the build.
//
// FIX (hk-zmpd): after the rebase, if the run-branch tip equals the target
// branch tip, the daemon now fails-closed with "rebase_dropped_commits" and
// reopens the bead so the reviewed work is salvageable from the run-branch.
//
// Test assertions:
//
//	(i)   ReopenBead called exactly once (the drop is detected → fail-closed).
//	(ii)  CloseBead NOT called (bead must not be falsely closed as success).
//	(iii) Reopen reason contains "rebase_dropped_commits".
//	(iv)  run_failed event emitted (not a successful run_completed).
//
// Bead: hk-zmpd.
// Refs: hk-cmry (falsely subsumed predecessor), hk-8prq (6894b4de dropped incident).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestMergeToMain_RebaseDropsCommit verifies that when the daemon's rebase of
// the run-branch onto an advanced main silently drops all run-branch commits
// (because the same patch was already applied to main), the daemon fails-closed
// rather than pushing a main that lost reviewed work.
//
// Setup:
//  1. Agent commits "rebase-drop-work\n" to work.txt on the run-branch.
//  2. Main advances with the EXACT SAME content change to work.txt — simulating
//     a later bead that re-applied the same change via cherry-pick or direct
//     commit.
//  3. git rebase main exits 0 but drops the run-branch commit as empty, leaving
//     the run-branch tip equal to the main tip.
//
// The daemon must detect this and reopen the bead (not close it as success).
//
// Bead: hk-zmpd.
func TestMergeToMain_RebaseDropsCommit(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-rebasedrop-hkzmpd-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote so the push path is reachable (even though the
	// rebase-drop guard fires before the push in this scenario).
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
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// rebaseDropFactory creates the run-branch with a reviewed commit, then
	// advances main with the EXACT SAME patch so that git rebase silently drops
	// the run-branch commit as "already applied".
	rebaseDropFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		// Create the real git worktree (run-branch is cut at current main tip).
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// Agent commits a file onto the run-branch.
		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(workFile, []byte("rebase-drop-work\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"rebaseDropFactory: WriteFile work.txt: " + err2.Error()}
		}
		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"rebaseDropFactory: git add: " + string(out)}
		}
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: reviewed agent work",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"rebaseDropFactory: git commit: " + string(out)}
		}

		// Advance main with THE SAME content change — this causes the rebase to
		// drop the run-branch commit as empty (already applied).
		advanceMainWithSamePatch(t, projectDir, "work.txt", "rebase-drop-work\n")

		// Push the advanced main to origin so push-retry detection works correctly.
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		if out, err2 := pushAdvCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"rebaseDropFactory: git push origin main: " + string(out)}
		}

		return wtPath, cleanup, nil
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  rebaseDropFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (i): ReopenBead called exactly once. ────────────────────────
	if got := ledger.getReopenedCount(); got != 1 {
		t.Errorf("ReopenBead call count = %d; want 1 (rebase-drop must be detected and reopened — hk-zmpd)", got)
	}

	// ── Assertion (ii): CloseBead NOT called. ─────────────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 — bead must NOT be falsely closed when rebase drops commits (hk-zmpd)", got)
	}

	// ── Assertion (iii): reopen reason contains "rebase_dropped_commits". ─────
	if reason := ledger.getReopenReason(); !strings.Contains(reason, "rebase_dropped_commits") {
		t.Errorf("ReopenBead reason = %q; want it to contain \"rebase_dropped_commits\" (hk-zmpd)", reason)
	}

	// ── Assertion (iv): run_failed emitted (no successful run_completed). ──────
	if evs := mergeToMainFindEvents(collector, "run_failed"); len(evs) == 0 {
		t.Errorf("no run_failed event found; want one for the rebase-drop reopen path (hk-zmpd); events: %v",
			mergeToMainEventOrder(collector))
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-zmpd rebase-drop guard OK: run_failed + ReopenBead(rebase_dropped_commits), events: %v", types)
}

// advanceMainWithSamePatch writes content to fileName on the main branch of
// repoRoot and commits it. This simulates a concurrent bead landing the same
// patch that the run-branch already has, causing git rebase to drop the
// run-branch commit as "already applied".
func advanceMainWithSamePatch(t *testing.T, repoRoot, fileName, content string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("advanceMainWithSamePatch: git %v: %v\n%s", args, err, out)
		}
	}
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(filepath.Join(repoRoot, fileName), []byte(content), 0o644); err != nil {
		t.Fatalf("advanceMainWithSamePatch: WriteFile %s: %v", fileName, err)
	}
	run("add", fileName)
	run("commit", "-m", "concurrent: same patch as run-branch (simulates rebase-drop scenario)")
}
