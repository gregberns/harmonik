package daemon_test

// mergetomain_runbranchmissing_hc1jr_test.go — regression test for the silent
// no-merge success (bead hk-no-merge-silent-success-hc1jr).
//
// INCIDENT SHAPE (reported 2026-08-09): a run reached its terminal node, closed
// its bead, and emitted run_completed{success:true} while the implementer's
// commit never reached the target branch. The event stream carried no merge
// event of any kind — not a success, not a failure — so nothing in the record
// said the work had not landed.
//
// MECHANISM: resolveMergeTips resolves the run branch with
// `git rev-parse refs/heads/run/<runID>` in the repo the merge runs in. When
// that ref did not resolve it returned Outcome{NoChange:true}, meaning "the
// agent made no commits". The Run machine treats no-change exactly like a
// successful merge — runexec stepRunMerging routes MergeSuccess and
// MergeNoChange down the same close ladder — so the bead closed and the run
// reported success. But a missing branch is not evidence of a missing commit:
// CreateWorktree always cuts refs/heads/run/<id>, so the branch missing at
// merge time means it never reached this repository (a remote run whose
// code-sync did not deliver it, a cross-repo run pointed at the wrong repo) or
// was deleted under the run. In each of those cases a real commit exists and is
// stranded.
//
// FIX: an unresolvable run branch is now a fail-closed merge outcome
// (`merge_run_branch_missing`), which reopens the bead and fails the run.
//
// Test assertions:
//
//	(i)   CloseBead NOT called — the bead must not close when nothing merged.
//	(ii)  ReopenBead called exactly once.
//	(iii) The reopen reason names merge_run_branch_missing.
//	(iv)  run_failed is emitted and no run_completed{success:true} exists.
//	(v)   The target branch did not advance — the work really is stranded.
//
// Bead: hk-no-merge-silent-success-hc1jr. Refs: hk-cwxow (the no-change
// short-circuits this carves the missing-branch case out of), hk-zmpd (the
// neighbouring fail-closed rebase-drop guard this mirrors).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workspace"
)

// TestMergeToMain_RunBranchMissingIsNotSilentSuccess verifies that a run whose
// commit exists but whose run branch does not resolve in the merge repo fails
// loudly instead of closing the bead as a success.
//
// The worktree factory checks the worktree out DETACHED at headSHA, so no
// refs/heads/run/<runID> exists, and the ordinary committing handler makes the
// commit during the run. That reproduces the production condition — a real
// commit the post-exit guards can see, unreachable through the run branch the
// merge reads — without depending on how the branch went missing.
//
// Bead: hk-no-merge-silent-success-hc1jr.
func TestMergeToMain_RunBranchMissingIsNotSilentSuccess(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-runbranch-missing-hc1jr-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      mergeToMainCommittingHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeToMainDetachedWorktreeFactory(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		//nolint:errcheck,gosec // the loop's only exit is this test's ctx cancel; siblings ignore it too
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

	// ── Assertion (i): the bead must NOT be closed. ───────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 — a run whose work never merged must not close its bead (hk-no-merge-silent-success-hc1jr)", got)
	}

	// ── Assertion (ii): the bead is reopened exactly once. ────────────────────
	if got := ledger.getReopenedCount(); got != 1 {
		t.Errorf("ReopenBead call count = %d; want 1 — an unresolvable run branch must fail closed (hk-no-merge-silent-success-hc1jr)", got)
	}

	// ── Assertion (iii): the reopen reason names the mechanism. ───────────────
	if reason := ledger.getReopenReason(); !strings.Contains(reason, "merge_run_branch_missing") {
		t.Errorf("ReopenBead reason = %q; want it to contain \"merge_run_branch_missing\" (hk-no-merge-silent-success-hc1jr)", reason)
	}

	// ── Assertion (iv): run_failed, and no successful run_completed. ──────────
	if evs := mergeToMainFindEvents(collector, "run_failed"); len(evs) == 0 {
		t.Errorf("no run_failed event found; want one — a missing merge must be loud (hk-no-merge-silent-success-hc1jr); events: %v",
			mergeToMainEventOrder(collector))
	}
	for _, ev := range mergeToMainFindEvents(collector, "run_completed") {
		if mergeToMainRunCompletedSuccess(t, ev) {
			t.Errorf("run_completed{success:true} emitted while nothing merged (hk-no-merge-silent-success-hc1jr); events: %v",
				mergeToMainEventOrder(collector))
		}
	}

	// ── Assertion (v): the target branch really did not advance. ──────────────
	if after := mergeToMainFixtureHeadSHA(t, projectDir, "main"); after != mainSHABefore {
		t.Errorf("main advanced to %s (was %s); the fixture is not reproducing the stranded-work condition", after, mainSHABefore)
	}
}

// mergeToMainDetachedWorktreeFactory builds a run worktree at the canonical
// run-worktree path but checks it out DETACHED, so no refs/heads/run/<runID>
// exists in projectDir. The agent handler still commits inside it during the
// run, so the daemon's post-exit guards see HEAD advance past headSHA while the
// merge cannot reach that commit through the run branch.
func mergeToMainDetachedWorktreeFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath := workspace.WorktreePath(projectDir, runID, workspace.NoWorktreeRootOverride())
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
			return "", nil, fmt.Errorf("mergeToMainDetachedWorktreeFactory: MkdirAll: %w", err)
		}

		//nolint:gosec // G204: fixed git binary, test-owned paths — not user input
		addCmd := exec.CommandContext(ctx, "git", "-C", projectDir, "worktree", "add", "--detach", wtPath, headSHA)
		if out, err := addCmd.CombinedOutput(); err != nil {
			return "", nil, fmt.Errorf("mergeToMainDetachedWorktreeFactory: git worktree add --detach (%s): %w", out, err)
		}
		cleanup := func() {
			//nolint:gosec // G204: fixed git binary, test-owned paths — not user input
			rmCmd := exec.CommandContext(context.WithoutCancel(ctx), "git", "-C", projectDir, "worktree", "remove", "--force", wtPath)
			if rmErr := rmCmd.Run(); rmErr != nil {
				t.Logf("mergeToMainDetachedWorktreeFactory: git worktree remove: %v", rmErr)
			}
		}
		return wtPath, cleanup, nil
	}
}

// mergeToMainRunCompletedSuccess reports the "success" field of a run_completed
// payload.
func mergeToMainRunCompletedSuccess(t *testing.T, ev stubEmittedEvent) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ev.Payload, &m); err != nil {
		t.Fatalf("mergeToMainRunCompletedSuccess: unmarshal: %v", err)
	}
	success, isBool := m["success"].(bool)
	return isBool && success
}
