package daemon_test

// mergetomain_hkj1aq5_test.go — integration test for the pre-merge rebase step
// introduced by hk-j1aq5 (EM-052 step 2).
//
// Test assertions per §10.2 EM-052–EM-053 rebase obligations:
//   (i)  Rebase succeeds when main advances without conflicts.
//   (j)  refs/heads/main advances to the rebased run-branch tip.
//   (k)  outcome_emitted{kind=approved} is emitted (full success path).
//
// Helper prefix: mergeToMainFixture (shared with mergetomain_hkftyvo_test.go).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052 step 2, EM-053
//
// Bead: hk-j1aq5.

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestMergeToMain_ConcurrentAdvanceRebaseSuccess verifies that when main has
// advanced with a non-conflicting commit after the worktree was cut, the daemon:
//
//	(i)  rebases the run-branch onto the new main successfully,
//	(j)  advances refs/heads/main to the rebased run-branch tip,
//	(k)  emits outcome_emitted{kind=approved} and closes the bead.
//
// Setup: agent commits work.txt; main advances with DIVERGE (different file,
// no conflict). The daemon rebases, FF-merges, and pushes to a bare origin.
//
// Spec refs: specs/execution-model.md §4.12 EM-052 step 2.
// Bead: hk-j1aq5.
func TestMergeToMain_ConcurrentAdvanceRebaseSuccess(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-rebase-success-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote (origin) so git push succeeds.
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

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Factory: create the run-branch + commit work.txt, then advance main with
	// a non-conflicting DIVERGE commit. The rebase in mergeRunBranchToMain
	// will succeed because DIVERGE and work.txt are different files.
	rebaseSuccessFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := mergeToMainCommittingFactory(t)(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		// Advance main with a non-conflicting commit; also push so origin is up to date.
		mergeToMainFixtureAdvanceMain(t, projectDir)
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		if out, pushErr := pushAdvCmd.CombinedOutput(); pushErr != nil {
			cleanup()
			return "", nil, &testSetupError{"git push origin main (after advance): " + string(out)}
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
		WorktreeFactory:  rebaseSuccessFactory,
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

	// ── Assertion (j): main advanced beyond mainSHABefore. ───────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main HEAD unchanged after rebase-success run: still %s", mainSHABefore)
	}

	// ── Assertion (i/k): CloseBead called, ReopenBead NOT called. ─────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 on rebase-success path", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on rebase-success path", got)
	}

	// ── Assertion (k): outcome_emitted{kind=approved}. ───────────────────────
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Errorf("no outcome_emitted events found; event stream: %v", mergeToMainEventOrder(collector))
	} else {
		kind := mergeToMainPayloadKind(t, outcomeEvs[0])
		if kind != "approved" {
			t.Errorf("outcome_emitted kind = %q; want %q", kind, "approved")
		}
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("merge-to-main concurrent-advance rebase-success OK: main %s → %s, events: %v",
		mainSHABefore[:8], mainSHAAfter[:8], types)
}

// testSetupError wraps a string as an error for fixture setup failures.
type testSetupError struct{ msg string }

func (e *testSetupError) Error() string { return e.msg }
