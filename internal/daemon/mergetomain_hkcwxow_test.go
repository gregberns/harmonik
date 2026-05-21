package daemon_test

// mergetomain_hkcwxow_test.go — regression test for the false-positive
// non_ff_merge when an agent makes no commits and main has advanced past the
// fork point (bead hk-cwxow).
//
// Scenario:
//   1. Run is forked from main at commit X (headSHA = X).
//   2. Agent exits 0 without committing anything (runTip == X).
//   3. A concurrent commit advances main to Y (Y ≠ X).
//   4. Without the fix, is-ancestor(Y, X) fails → daemon reports non_ff_merge.
//   5. With the fix, runTip == headSHA is detected → noChange, bead is closed.
//
// Test assertions:
//   (i)  CloseBead called exactly once (noChange path closes the bead).
//   (ii) ReopenBead NOT called (false-positive "non_ff_merge" must not happen).
//   (iii) run_completed{success:true} emitted.
//
// Spec refs: specs/execution-model.md §4.12 EM-052.
// Bead: hk-cwxow.

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestMergeToMain_NoWorkAgentMainAdvanced verifies that when an agent makes no
// commits (runTip == headSHA) but main has moved forward since the fork, the
// daemon treats the run as noChange (closes the bead, does not report non_ff_merge).
//
// Bead: hk-cwxow.
func TestMergeToMain_NoWorkAgentMainAdvanced(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-nowork-mainadvanced-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	primeCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	primeCmd.Dir = projectDir
	if out, err := primeCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// noWorkButMainAdvancedFactory creates the real worktree (which cuts the
	// run-branch at the current main tip) but makes NO commit, then advances
	// main so that mainTip ≠ runTip yet runTip == headSHA.
	noWorkButMainAdvancedFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		// Advance main after the worktree/branch is cut — agent did no work.
		mergeToMainFixtureAdvanceMain(t, projectDir)
		// Also push the advanced main to origin so that the push in mergeRunBranchToMain
		// (noChange path skips it) doesn't need to succeed.
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		_ = pushAdvCmd.Run() // best-effort; noChange path never pushes
		return wtPath, cleanup, nil
	}

	// Handler exits 0 without any work — triggers auto-close heuristic.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  noWorkButMainAdvancedFactory,
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

	// ── Assertion (i): CloseBead called exactly once. ─────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (noChange path must close bead)", got)
	}

	// ── Assertion (ii): ReopenBead NOT called (no false-positive non_ff_merge). ─
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 — false-positive non_ff_merge regression (hk-cwxow)", got)
	}

	// ── Assertion (iii): run_completed{success:true}. ─────────────────────────
	runCompletedEvs := mergeToMainFindEvents(collector, "run_completed")
	if len(runCompletedEvs) == 0 {
		t.Error("no run_completed events found")
	} else {
		var m map[string]interface{}
		if err := json.Unmarshal(runCompletedEvs[0].Payload, &m); err != nil {
			t.Fatalf("run_completed payload unmarshal: %v", err)
		}
		success, _ := m["success"].(bool)
		if !success {
			t.Errorf("run_completed success = false; want true for noChange path")
		}
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-cwxow regression OK: agent-no-work + main-advanced → noChange, events: %v", types)
}
