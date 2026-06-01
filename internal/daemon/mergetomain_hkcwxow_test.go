package daemon_test

// mergetomain_hkcwxow_test.go — regression test for the false-positive
// non_ff_merge when an agent makes no commits and main has advanced past the
// fork point (bead hk-cwxow), AS CORRECTED BY hk-4ie1z.
//
// Scenario:
//   1. Run is forked from main at commit X (headSHA = X).
//   2. Agent exits 0 without committing anything (runTip == X).
//   3. A concurrent commit advances main to Y (Y ≠ X) for UNRELATED reasons
//      (no `Refs: <thisBead>` trailer — this bead's work never landed).
//   4. The hk-cwxow property still holds: the daemon must NOT report a
//      false-positive non_ff_merge.
//
// hk-4ie1z correction: the ORIGINAL hk-cwxow test asserted the run was
// CLOSED AS SUCCESS in this scenario (step 5: "runTip == headSHA → noChange,
// bead is closed"). That was the bug: an agent that produced no commit was
// falsely closed as success merely because main had advanced for unrelated
// reasons (a sibling bead landing). Under hk-4ie1z the single-mode no-commit
// guard now fires a `no_commit` REOPEN unless THIS bead's own work is on main
// (Refs-trailer check) — so this no-work run REOPENS, it does not auto-close.
//
// The hk-cwxow property (no false non_ff_merge) is preserved a fortiori: the
// no-commit guard fires BEFORE mergeRunBranchToMain is ever reached, so the
// merge helper cannot emit a spurious non_ff_merge for this run. (The merge
// helper's own runTip == headSHA → noChange short-circuit, workloop.go ~2804,
// remains in place and independently covers the false-non_ff concern.)
//
// Test assertions (post-hk-4ie1z):
//   (i)   ReopenBead called exactly once with a `no_commit` reason.
//   (ii)  CloseBead NOT called (no-commit run must not be auto-closed success).
//   (iii) NO false-positive non_ff_merge reason (the hk-cwxow property).
//   (iv)  run_completed{success:false} emitted.
//
// Spec refs: specs/execution-model.md §4.12 EM-052; EM-015d (HEAD-advance).
// Beads: hk-cwxow, hk-4ie1z.

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestMergeToMain_NoWorkAgentMainAdvanced verifies that when an agent makes no
// commits (runTip == headSHA) but main has moved forward since the fork for
// UNRELATED reasons, the daemon REOPENS the bead as `no_commit` (hk-4ie1z) and
// never reports a false-positive non_ff_merge (hk-cwxow).
//
// Beads: hk-cwxow, hk-4ie1z.
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

	// ── Assertion (i): ReopenBead called exactly once with a no_commit reason. ─
	// hk-4ie1z: a no-commit run whose work is NOT on main must REOPEN, not close,
	// even though a (sibling/unrelated) commit advanced main.
	if got := ledger.getReopenedCount(); got != 1 {
		t.Errorf("ReopenBead call count = %d; want 1 (no-commit run must be reopened, not auto-closed — hk-4ie1z)", got)
	}
	if reason := ledger.getReopenReason(); !strings.Contains(reason, "no_commit") {
		t.Errorf("ReopenBead reason = %q; want a no_commit reason (hk-4ie1z guard)", reason)
	}

	// ── Assertion (ii): CloseBead NOT called. ─────────────────────────────────
	// The pre-hk-4ie1z bug auto-closed this run as success; that must not happen.
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 — a no-commit run must NOT be falsely closed as success (hk-4ie1z)", got)
	}

	// ── Assertion (iii): no false-positive non_ff_merge (the hk-cwxow property). ─
	if reason := ledger.getReopenReason(); strings.Contains(reason, "non_ff") {
		t.Errorf("ReopenBead reason = %q; contains non_ff — false-positive non_ff_merge regression (hk-cwxow)", reason)
	}

	// ── Assertion (iv): run_failed emitted (no run_completed-success). ────────
	// emitDone(false, …) on the no-commit guard path surfaces as run_failed; the
	// run must NOT emit a successful run_completed (that was the false-close).
	if evs := mergeToMainFindEvents(collector, "run_completed"); len(evs) > 0 {
		var m map[string]interface{}
		if err := json.Unmarshal(evs[0].Payload, &m); err != nil {
			t.Fatalf("run_completed payload unmarshal: %v", err)
		}
		if success, _ := m["success"].(bool); success {
			t.Errorf("run_completed success = true; want a run_failed for a no-commit run reopened as no_commit (hk-4ie1z)")
		}
	}
	if evs := mergeToMainFindEvents(collector, "run_failed"); len(evs) == 0 {
		t.Errorf("no run_failed event found; want one for the no_commit reopen path (hk-4ie1z)")
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-cwxow/hk-4ie1z regression OK: agent-no-work + unrelated-main-advance → no_commit REOPEN (no false non_ff, no false close), events: %v", types)
}
