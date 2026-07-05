package daemon_test

// dot_approve_rebase_drop_hkvbv3b_test.go — regression test for hk-vbv3b.
//
// # The bug (hk-i0377 post-merge loop)
//
// hk-i0377's run merged the acceptance test but the DOT workflow then re-launched
// a fresh reviewer after EACH of 4 consecutive APPROVE verdicts instead of
// advancing to dot:close — an infinite review re-dispatch loop burning a reviewer
// launch ~every 3.5min.
//
// Root cause: when the DOT cascade reaches the close terminal via APPROVE
// (review→close[APPROVE]), driveDotWorkflow returns
// dotWorkflowResult{success:true, terminalNodeID:"close"}.
// The workloop's rebase_dropped_commits exemption (hk-whru3) only covers the
// advisory-RC path (dotResult.advisoryRC=true). For a genuine APPROVE terminal,
// advisoryRC=false, so when a prior run already merged the same patch and the
// merge step fires rebase_dropped_commits, the bead is reopened — re-queuing
// indefinitely with a fresh reviewer every 3.5min.
//
// Fix (hk-vbv3b): extend the rebase_dropped_commits exemption to also cover
// dotResult.terminalNodeID == "close" (genuine APPROVE terminal) and
// dotResult.approveVerdict != nil (hk-8ps7q approved-and-done path).
//
// Test assertions:
//
//	(i)   CloseBead called exactly once (reconcile-close path).
//	(ii)  ReopenBead NOT called (no infinite review re-dispatch loop).
//	(iii) run_completed event emitted (not run_failed).
//
// Bead: hk-vbv3b.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// hkvbv3bHandlerScript writes a handler script for the genuine-APPROVE +
// rebase-drop scenario. The counter file lives in projDir (stable at test setup
// time); the verdict file is written to $HARMONIK_WORKSPACE_PATH (set at
// runtime by the workloop to the worktree path).
//
// Invocation sequence:
//
//	inv 1 (implement iter-1): commits work → HEAD advances past preHeadSHA.
//	inv 2 (review    iter-1): writes APPROVE verdict → cascade routes review→close.
//
// After inv 2, driveDotWorkflow processes the non-agentic close node and
// reaches the terminal, returning {success:true, terminalNodeID:"close"}.
//
// The same content committed in inv 1 ("approve-work\n" → vbv3b_work.txt)
// must already be on main (committed by the factory) so that git rebase drops
// the run-branch commit as "already applied" (runTip == mainTip → rebase_dropped_commits).
func hkvbv3bHandlerScript(t *testing.T, projDir string) string {
	t.Helper()
	projEsc := strings.ReplaceAll(projDir, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
PROJ='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$PROJ}"
CNT_FILE="$PROJ/.harmonik/vbv3b_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implement iter-1: commit work so HEAD advances past preHeadSHA.
    # The factory pre-advanced main with the SAME content, so rebase will drop
    # this commit as "already applied" (hk-zmpd: runTip == mainTip).
    printf 'approve-work\n' > "$WS/vbv3b_work.txt"
    git -C "$WS" add "vbv3b_work.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "feat: vbv3b approve work" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Review iter-1: genuine APPROVE → cascade routes review→close terminal.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, projEsc, approve)
	p := filepath.Join(t.TempDir(), "vbv3b_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("hkvbv3bHandlerScript: WriteFile: %v", err)
	}
	return p
}

// TestApproveTerminal_RebaseDropCommit_ReconcileCloses_hkvbv3b verifies that
// when the DOT cascade terminates via a genuine APPROVE (review→close terminal)
// AND the run-branch commits are already applied to main (hk-zmpd:
// rebase_dropped_commits), the workloop reconcile-closes the bead (CloseBead)
// rather than re-queuing it (ReopenBead).
//
// Setup:
//  1. Factory creates the run-branch worktree and leaves it at initial state.
//  2. Factory advances main with the EXACT SAME content the handler commits in
//     inv 1 — simulating a prior run that already merged the same patch.
//  3. Handler drives the DOT cascade (2 invocations): implement commits work,
//     reviewer writes APPROVE. Cascade terminates at close terminal.
//  4. git rebase exits 0 but drops the run-branch commit as "already applied",
//     leaving runTip == mainTip → hk-zmpd guard fires: rebase_dropped_commits.
//
// Assertions:
//
//	(i)   CloseBead called exactly once.
//	(ii)  ReopenBead NOT called (infinite loop absent).
//	(iii) run_completed emitted (not run_failed).
//
// Bead: hk-vbv3b.
func TestApproveTerminal_RebaseDropCommit_ReconcileCloses_hkvbv3b(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("approve-terminal-rebase-drop-hkvbv3b-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Write the standard-bead DOT graph so the workloop picks it up in DOT mode.
	// Gate always passes (true exits 0) — the handler commits in inv 1 so the
	// gate runs on a green working tree.
	dotSrc := w2owGatedLoopDOT("true")
	dotPath := filepath.Join(projectDir, "workflow.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(dotSrc), 0o644); err != nil {
		t.Fatalf("TestApproveTerminal_RebaseDropCommit_ReconcileCloses_hkvbv3b: WriteFile workflow.dot: %v", err)
	}
	// Fail fast on a structurally invalid DOT (catches copy/paste errors).
	if _, err := workflow.LoadDotWorkflow(dotPath); err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	// Bare remote — the merge path dials origin even when the rebase_dropped_commits
	// guard fires before the push.
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

	// approveRebaseDropFactory creates the run-branch worktree (no pre-commit),
	// then advances main with the SAME content that handler inv 1 will commit —
	// so that git rebase drops the run-branch commit as "already applied"
	// (hk-zmpd: runTip == mainTip → rebase_dropped_commits).
	approveRebaseDropFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// Advance main with the SAME content the handler commits in inv 1.
		advanceMainWithSamePatch(t, projectDir, "vbv3b_work.txt", "approve-work\n")

		// Push the advanced main to origin.
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		if out, err2 := pushAdvCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"approveRebaseDropFactory: git push origin main: " + string(out)}
		}

		return wtPath, cleanup, nil
	}

	scriptPath := hkvbv3bHandlerScript(t, projectDir)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:     approveRebaseDropFactory,
		WorkflowModeDefault: core.WorkflowModeDot,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
		t.Errorf("CloseBead call count = %d; want 1 (genuine APPROVE + rebase_dropped_commits must reconcile-close — hk-vbv3b)", got)
	}

	// ── Assertion (ii): ReopenBead NOT called. ────────────────────────────────
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 — APPROVE rebase-drop must NOT reopen (hk-vbv3b); reason: %q",
			got, ledger.getReopenReason())
	}

	// ── Assertion (iii): run_completed emitted. ───────────────────────────────
	if evs := mergeToMainFindEvents(collector, "run_completed"); len(evs) == 0 {
		t.Errorf("no run_completed event found; want one for APPROVE reconcile-close (hk-vbv3b); events: %v",
			mergeToMainEventOrder(collector))
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-vbv3b APPROVE reconcile-close OK: CloseBead=1 ReopenBead=0, events: %v", types)
}
