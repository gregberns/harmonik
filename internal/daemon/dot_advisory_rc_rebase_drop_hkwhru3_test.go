package daemon_test

// dot_advisory_rc_rebase_drop_hkwhru3_test.go — regression test for hk-whru3.
//
// # The bug (logmine iter20 S3-F1)
//
// Bead hk-w2ow ran 4× over 4 days with identical summary
// "REQUEST_CHANGES was advisory-only (commit gate green; HEAD final, nothing
// committable remained)" — completing success=true each time — yet was re-dispatched
// after each run, never reconciling closed.
//
// Root cause: when the advisory-RC exemption (hk-w2ow) fires, the merge step tries
// to rebase the run-branch onto main. If a prior run already merged the same patch,
// git rebase identifies the run-branch commit as "already applied" and drops it
// (runTip == mainTip after rebase). The hk-zmpd guard then returns
// mergeOutcome{success:false, reason:"rebase_dropped_commits:…"}, and the workloop
// calls ReopenBead — re-queuing indefinitely.
//
// Fix (hk-whru3): when dotResult.advisoryRC=true AND mergeRes.reason contains
// "rebase_dropped_commits", fall through to CloseBead (reconcile-close) instead of
// calling ReopenBead. The committed work is already on main from a prior run;
// re-queuing does nothing useful.
//
// Test assertions:
//
//	(i)   CloseBead called exactly once (reconcile-close path).
//	(ii)  ReopenBead NOT called (no re-dispatch loop).
//	(iii) run_completed event emitted (not run_failed).
//
// Bead: hk-whru3.

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

// hkwhru3HandlerScript writes a handler script for the advisory-RC + rebase-drop
// scenario. The counter file lives in projDir (stable at test setup time); the
// verdict file is written to $HARMONIK_WORKSPACE_PATH (set at runtime by the
// workloop to the worktree path).
//
// Invocation sequence:
//
//	inv 1 (implement iter-1): commits advisory work → HEAD advances past preHeadSHA.
//	inv 2 (review    iter-1): writes advisory REQUEST_CHANGES verdict.
//	inv 3 (implement iter-2): exits 0 — no new commit (advisory feedback).
//
// The advisory-RC exemption fires at the review iter-2 entry (before inv 4 would
// run), producing success=true + advisoryRC=true.
//
// The same content committed in inv 1 ("advisory-rc-work\n" → whru3_work.txt)
// must already be on main (committed by the factory) so that git rebase drops the
// run-branch commit as "already applied" (runTip == mainTip → rebase_dropped_commits).
func hkwhru3HandlerScript(t *testing.T, projDir string) string {
	t.Helper()
	projEsc := strings.ReplaceAll(projDir, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
PROJ='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$PROJ}"
CNT_FILE="$PROJ/.harmonik/whru3_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implement iter-1: commit the advisory work so HEAD advances past preHeadSHA.
    # The factory pre-advanced main with the SAME content, so rebase will drop
    # this commit as "already applied" (hk-zmpd: runTip == mainTip).
    printf 'advisory-rc-work\n' > "$WS/whru3_work.txt"
    git -C "$WS" add "whru3_work.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "feat: advisory rc work" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Review iter-1: advisory-only REQUEST_CHANGES.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implement iter-2: no new commit (advisory feedback was noop).
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, projEsc, rc)
	p := filepath.Join(t.TempDir(), "whru3_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("hkwhru3HandlerScript: WriteFile: %v", err)
	}
	return p
}

// TestAdvisoryRC_RebaseDropCommit_ReconcileCloses_hkwhru3 verifies that when the
// advisory-RC exemption (hk-w2ow) fires AND the run-branch commits are already
// applied to main (hk-zmpd: rebase_dropped_commits), the workloop reconcile-closes
// the bead (CloseBead) rather than re-queuing it (ReopenBead).
//
// Setup:
//  1. Factory creates the run-branch worktree and commits "advisory work" onto it.
//  2. Factory advances main with the EXACT SAME content change — simulating a prior
//     run that already merged the same patch to main.
//  3. Handler script drives the advisory-RC cascade (3 invocations); the advisory
//     exemption fires at review iter-2 entry before any 4th dispatch.
//  4. git rebase exits 0 but drops the run-branch commit as "already applied",
//     leaving runTip == mainTip → hk-zmpd guard fires: rebase_dropped_commits.
//
// Assertions:
//
//	(i)   CloseBead called exactly once.
//	(ii)  ReopenBead NOT called.
//	(iii) run_completed emitted (not run_failed).
//
// Bead: hk-whru3.
func TestAdvisoryRC_RebaseDropCommit_ReconcileCloses_hkwhru3(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("advisory-rc-rebase-drop-hkwhru3-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Write the standard-bead DOT graph so the workloop picks it up in DOT mode.
	// Gate always passes (true exits 0) — green gate is required for advisory-RC.
	dotSrc := w2owGatedLoopDOT("true")
	dotPath := filepath.Join(projectDir, "workflow.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(dotSrc), 0o644); err != nil {
		t.Fatalf("TestAdvisoryRC_RebaseDropCommit_ReconcileCloses_hkwhru3: WriteFile workflow.dot: %v", err)
	}
	// Fail fast if the DOT is structurally invalid (catches copy/paste errors).
	if _, err := workflow.LoadDotWorkflow(dotPath); err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	// Set up a bare remote so the push path is reachable (the hk-zmpd guard fires
	// before the push in this scenario, but the merge path still dials origin).
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

	// advisoryRCRebaseDropFactory creates the run-branch worktree (NO pre-commit),
	// then advances main with the SAME content that handler inv 1 will commit —
	// so that git rebase drops the run-branch commit as "already applied"
	// (hk-zmpd: runTip == mainTip → rebase_dropped_commits). No pre-commit here:
	// the handler commits in inv 1 so HEAD advances past preHeadSHA (the
	// per-node no-commit guard on iteration 1 requires HEAD to advance).
	advisoryRCRebaseDropFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		// Create the real git worktree — run-branch cut at current main tip.
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// Advance main with the SAME content the handler commits in inv 1.
		// git rebase will drop the run-branch commit as "already applied".
		advanceMainWithSamePatch(t, projectDir, "whru3_work.txt", "advisory-rc-work\n")

		// Push the advanced main to origin so merge-path push-detection works.
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		if out, err2 := pushAdvCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"advisoryRCRebaseDropFactory: git push origin main: " + string(out)}
		}

		return wtPath, cleanup, nil
	}

	scriptPath := hkwhru3HandlerScript(t, projectDir)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:     advisoryRCRebaseDropFactory,
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
		t.Errorf("CloseBead call count = %d; want 1 (advisory-RC + rebase_dropped_commits must reconcile-close — hk-whru3)", got)
	}

	// ── Assertion (ii): ReopenBead NOT called. ────────────────────────────────
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 — advisory-RC rebase-drop must NOT reopen (hk-whru3); reason: %q",
			got, ledger.getReopenReason())
	}

	// ── Assertion (iii): run_completed emitted. ───────────────────────────────
	if evs := mergeToMainFindEvents(collector, "run_completed"); len(evs) == 0 {
		t.Errorf("no run_completed event found; want one for advisory-RC reconcile-close (hk-whru3); events: %v",
			mergeToMainEventOrder(collector))
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-whru3 advisory-RC reconcile-close OK: CloseBead=1 ReopenBead=0, events: %v", types)
}
