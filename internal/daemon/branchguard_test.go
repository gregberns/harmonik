//go:build scenario

package daemon_test

// branchguard_test.go — the deployment-gate scenario test for the
// integration-branch productization work (hk-eun55).
//
// Most of these tests run a bead through the REAL work loop
// (daemon.ExportedRunWorkLoop + daemon.ExportedWorkLoopDeps, the same
// composition seam the production dispatch path uses) and assert the
// load-bearing branch-protection guarantees landed by hk-mkxw1 / hk-6r6xv /
// hk-ncwb3 / hk-sul12:
//
//	1. TargetBranchMergeIsolation — with TargetBranch="integration" and
//	   ProtectBranches=["main"], a committing bead MERGES to the integration
//	   branch, and main is provably untouched: refs/heads/main rev-parse,
//	   origin/main, AND main's REFLOG are byte-for-byte unchanged while
//	   refs/heads/integration advances to the run-branch tip.
//
//	2. FailClosed_TargetInProtectSet — with TargetBranch="main" and
//	   ProtectBranches=["main"], the EARLY lands_on gate in the work loop
//	   (hk-ncwb3, workloop.go, the LandsOnProtectedError branch) REFUSES the
//	   bead before a worktree is cut. We assert ZERO git side effects (main,
//	   origin/main, integration all unchanged) and that the bead is reopened,
//	   not closed.
//
//	2b. FailClosed_MergeGuardBackstop — a DIRECT call to
//	   runmerge.RunBranchToTarget with targetBranch="main" and
//	   protectBranches=["main"]. This is the last-line fail-closed guard
//	   (hk-6r6xv, runmerge/merge.go resolveMergeTips). It is NOT reachable
//	   through the work loop any more — see that test's own comment for the
//	   measurement — so it is asserted at its own seam.
//
//	3. BootValidation_RefusesEmptyTargetUnderForbid — daemon.Start hard-errors
//	   (no socket bind) when ForbidUnprotectedDefault is set but TargetBranch is
//	   empty (hk-sul12 boot-time fail-closed validation).
//
// Harness: these tests reuse the mergeToMainFixture* helpers + the committing
// worktree factory + the recording bead ledger + stubEventCollector already
// defined in mergetomain_hkftyvo_test.go (same daemon_test package). They do NOT
// introduce a new harness — they parameterise the existing merge-to-main harness
// with a non-main target branch and a protect-set.
//
// Build tag: scenario. The daemon's pre-merge scenario gate SKIPS //go:build
// scenario tests, so this file is run explicitly via
//
//	go test -tags=scenario -run 'TestBranchGuard' ./internal/daemon/ -count=1
//
// A plain `go test -run TestBranchGuard ./internal/daemon/` reports
// "no tests to run" — the tag is mandatory. `make test-scenario` supplies it.
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052/EM-053 (ordered merge sequence)
//   - hk-6r6xv (fail-closed merge guard), hk-ncwb3 (start_from retarget),
//     hk-mkxw1 (Config branch fields), hk-sul12 (boot-time validation),
//     hk-lgykq (per-bead landing target)
//
// Bead: hk-eun55.

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
	"github.com/gregberns/harmonik/internal/runmerge"
)

// ─────────────────────────────────────────────────────────────────────────────
// branchguard fixtures
// ─────────────────────────────────────────────────────────────────────────────

// branchGuardGit runs a git command in dir, failing the test on error.
func branchGuardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("branchGuardGit: git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// branchGuardReflog returns the full `git reflog show <ref>` output (one line
// per reflog entry) so a test can assert it is byte-for-byte unchanged across a
// run. Missing-ref → empty string + false.
func branchGuardReflog(t *testing.T, dir, ref string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "reflog", "show", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// branchGuardSetupRepoWithIntegration builds a project repo with main + an
// "integration" branch (initially pointing at the same commit as main) AND a
// bare origin remote with both branches primed so pushes succeed. Returns the
// project dir.
func branchGuardSetupRepoWithIntegration(t *testing.T) string {
	t.Helper()

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir) // main + one initial commit

	// Create the integration branch at main's tip (hk-ncwb3: worktrees cut from
	// the configured target branch; the merge lands on it).
	branchGuardGit(t, projectDir, "branch", "integration")

	// Bare origin remote, primed with main + integration so `git push origin
	// <target>` in the merge sequence succeeds.
	originDir := t.TempDir()
	branchGuardGit(t, originDir, "init", "--bare", "--initial-branch=main")
	branchGuardGit(t, projectDir, "remote", "add", "origin", originDir)
	branchGuardGit(t, projectDir, "push", "origin", "main")
	branchGuardGit(t, projectDir, "push", "origin", "integration")

	return projectDir
}

// branchGuardDescribedLedger wraps the recording ledger so the dispatched bead
// carries a non-empty Description (e.g. a `## Branching` section). The embedded
// recording ledger supplies Close/Reopen capture; only the two read methods are
// overridden to inject the description.
type branchGuardDescribedLedger struct {
	*mergeToMainRecordingLedger
	description string
}

func (l *branchGuardDescribedLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	recs, err := l.mergeToMainRecordingLedger.Ready(ctx)
	for i := range recs {
		recs[i].Description = l.description
	}
	return recs, err
}

func (l *branchGuardDescribedLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	rec, err := l.mergeToMainRecordingLedger.ShowBead(ctx, id)
	rec.Description = l.description
	return rec, err
}

// branchGuardRunBead drives one bead through the real work loop with the given
// target branch + protect-set, using a committing worktree factory (so the
// run-branch is one commit ahead of the target) and a /bin/sh "exit 0" handler
// (the single-mode auto-close heuristic path). The bead carries description as
// its body (use "" for no ## Branching section). Returns the recording ledger
// and the event collector after the loop has settled.
func branchGuardRunBead(
	t *testing.T,
	projectDir string,
	beadID core.BeadID,
	targetBranch string,
	protectBranches []string,
	description string,
) (*mergeToMainRecordingLedger, *stubEventCollector) {
	t.Helper()

	recording := newMergeToMainRecordingLedger(beadID)
	ledger := &branchGuardDescribedLedger{mergeToMainRecordingLedger: recording, description: description}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeToMainCommittingFactory(t),
		TargetBranch:     targetBranch,
		ProtectBranches:  protectBranches,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the first Close/Reopen (doneCh) or the test timeout.
	select {
	case <-recording.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatal("branchGuardRunBead: timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("branchGuardRunBead: work loop did not exit within 5s")
	}

	return recording, collector
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: target-branch merge isolation (main is provably untouched)
// ─────────────────────────────────────────────────────────────────────────────

// TestBranchGuard_TargetBranchMergeIsolation asserts that with
// TargetBranch="integration" and ProtectBranches=["main"], a committing bead
// merges to integration while main (rev-parse + origin/main + REFLOG) is
// byte-for-byte unchanged.
//
// Bead: hk-eun55 (assertion 1).
func TestBranchGuard_TargetBranchMergeIsolation(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("branchguard-isolation-bead-001")

	projectDir := branchGuardSetupRepoWithIntegration(t)

	// Snapshot main BEFORE the run: local ref, origin/main ref, and full reflog.
	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, mainReflogOK := branchGuardReflog(t, projectDir, "refs/heads/main")
	if !mainReflogOK {
		t.Fatal("could not capture main reflog before run")
	}

	ledger, collector := branchGuardRunBead(t, projectDir, beadID, "integration", []string{"main"}, "")

	// ── Assertion: integration ADVANCED past its prior tip. ───────────────────
	integrationAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	if integrationAfter == integrationBefore {
		t.Errorf("integration HEAD unchanged after committing run: still %s; want run-branch tip", integrationBefore)
	}

	// ── Assertion: main is BYTE-FOR-BYTE unchanged (rev-parse). ───────────────
	mainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	if mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved: before=%s after=%s; want UNCHANGED (target was integration)", mainBefore, mainAfter)
	}

	// ── Assertion: origin/main is unchanged (no push touched main). ───────────
	originMainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	if originMainAfter != originMainBefore {
		t.Errorf("origin/main moved: before=%s after=%s; want UNCHANGED", originMainBefore, originMainAfter)
	}

	// ── Assertion: main REFLOG is byte-for-byte unchanged (no update-ref ever
	//    touched refs/heads/main, even transiently). ───────────────────────────
	mainReflogAfter, ok := branchGuardReflog(t, projectDir, "refs/heads/main")
	if !ok {
		t.Fatal("could not capture main reflog after run")
	}
	if mainReflogAfter != mainReflogBefore {
		t.Errorf("main reflog changed during integration-target run:\nBEFORE:\n%s\nAFTER:\n%s\nwant byte-for-byte unchanged", mainReflogBefore, mainReflogAfter)
	}

	// ── Assertion: bead CLOSED (merge to integration succeeded). ──────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (integration merge should succeed)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on successful integration merge", got)
	}

	// ── Assertion: outcome_emitted{kind=approved}. ────────────────────────────
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Fatalf("no outcome_emitted events; stream: %v", mergeToMainEventOrder(collector))
	}
	if kind := mergeToMainPayloadKind(t, outcomeEvs[0]); kind != "approved" {
		t.Errorf("outcome_emitted kind = %q; want %q", kind, "approved")
	}

	t.Logf("branchguard isolation OK: integration %s → %s; main pinned at %s (reflog unchanged)",
		integrationBefore[:8], integrationAfter[:8], mainBefore[:8])
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: fail-closed guard refuses a protected target (zero git mutations)
// ─────────────────────────────────────────────────────────────────────────────

// TestBranchGuard_FailClosed_TargetInProtectSet asserts that with
// TargetBranch="main" and ProtectBranches=["main"], the daemon refuses the bead
// fail-closed: main, origin/main, and integration are ALL byte-for-byte
// unchanged (ZERO git mutations) and the bead is reopened (NOT closed).
//
// FINDING (hk-eun55): a bead configured with a target branch that is ALSO in the
// protect-set is refused by the EARLY lands_on-protection gate (hk-ncwb3, the
// LandsOnProtectedError branch in internal/daemon/workloop.go) — BEFORE a
// worktree is cut or run_started is emitted — because resolveBranching defaults
// the bead's lands_on to the configured target ("main"), which is protected.
// This is a STRONGER fail-closed than the deep merge guard (hk-6r6xv): the
// protected-target bead never even gets a worktree.
//
// This test therefore asserts the load-bearing invariant the productization gate
// cares about — ZERO git side effects + reopen + not-closed + a protected-branch
// refusal reason. The deep merge guard is asserted at its own seam by
// TestBranchGuard_FailClosed_MergeGuardBackstop below.
//
// Bead: hk-eun55 (assertion 2).
func TestBranchGuard_FailClosed_TargetInProtectSet(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("branchguard-failclosed-bead-001")

	projectDir := branchGuardSetupRepoWithIntegration(t)

	// Snapshot all three refs BEFORE the run.
	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/main")

	// Run with the target == a protected branch. The committing factory still
	// cuts the run-branch from main's tip and commits, so there IS work to
	// merge — the daemon must refuse it regardless. (No ## Branching section →
	// lands_on defaults to the configured target "main", so the early lands_on
	// gate fires.)
	ledger, collector := branchGuardRunBead(t, projectDir, beadID, "main", []string{"main"}, "")

	// ── Assertion: ZERO git mutations — every ref pinned. ─────────────────────
	if mainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main"); mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved despite protect-set refusal: before=%s after=%s", mainBefore, mainAfter)
	}
	if originMainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main"); originMainAfter != originMainBefore {
		t.Errorf("origin/main moved despite protect-set refusal: before=%s after=%s", originMainBefore, originMainAfter)
	}
	if integrationAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration"); integrationAfter != integrationBefore {
		t.Errorf("integration moved despite protect-set refusal: before=%s after=%s", integrationBefore, integrationAfter)
	}
	if mainReflogAfter, _ := branchGuardReflog(t, projectDir, "refs/heads/main"); mainReflogAfter != mainReflogBefore {
		t.Errorf("main reflog changed despite protect-set refusal:\nBEFORE:\n%s\nAFTER:\n%s", mainReflogBefore, mainReflogAfter)
	}

	// ── Assertion: bead REOPENED, NOT closed (fail-closed refusal). ───────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 when daemon refuses protected target", got)
	}
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥1 when daemon refuses protected target", got)
	}

	// ── Assertion: the reopen reason names a protected-branch refusal. The
	//    early lands_on gate (hk-ncwb3) reopens with "protected" in its message.
	//    The deep merge guard is asserted separately by
	//    TestBranchGuard_FailClosed_MergeGuardBackstop. ─────────────────────────
	reopenReason := ledger.getReopenReason()
	if !strings.Contains(reopenReason, "protected") {
		t.Errorf("reopen reason %q does not indicate a protected-branch refusal", reopenReason)
	}

	// ── Assertion: bead_closed event MUST NOT appear. ─────────────────────────
	if evs := mergeToMainFindEvents(collector, "bead_closed"); len(evs) > 0 {
		t.Errorf("bead_closed emitted despite guard refusal; want absent: %v", mergeToMainEventOrder(collector))
	}

	t.Logf("branchguard fail-closed OK: protected target refused (reason=%q), all refs pinned, bead reopened", reopenReason)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2b: deep merge guard (hk-6r6xv) refuses a protected merge target.
// ─────────────────────────────────────────────────────────────────────────────

// TestBranchGuard_FailClosed_MergeGuardBackstop asserts the LAST-LINE
// fail-closed guard in internal/runmerge/merge.go: resolveMergeTips refuses the
// merge with reason "merge_target_protected" when the merge target is in the
// protect-set, BEFORE any rebase, update-ref, or push. The test calls
// runmerge.RunBranchToTarget directly with targetBranch="main" and
// protectBranches=["main"], and asserts ZERO git mutations.
//
// WHY THIS IS A DIRECT CALL AND NOT A WORK-LOOP RUN. Until hk-lgykq, the work
// loop passed the daemon-wide TargetBranch to the merge, so a bead could reach
// the merge with a target the EARLY lands_on gate had never seen. That is gone.
// The loop now resolves ONE value — the per-bead lands_on from resolveBranching
// — and uses it for BOTH gates:
//
//   - The early gate (hk-ncwb3, the LandsOnProtectedError branch in
//     internal/daemon/workloop.go) compares that value against
//     env.ProtectBranches.
//   - The deep guard receives the SAME value as mergeTarget and the SAME list as
//     effectiveMergeProtectBranches.
//
// Both comparisons are exact string equality on the same pair, so for a
// same-repo run the two gates cannot disagree: whatever the deep guard would
// refuse, the early gate already refused. (For a cross-repo run the loop skips
// the early gate AND nils the protect-list, so neither gate fires.) There is
// therefore no work-loop fixture left in which the early gate passes and the
// deep guard refuses.
//
// One escape hatch looks open and is not. The loop falls back to the
// daemon-wide target when baseBranch is empty, and baseBranch is empty only
// when resolveBranching errors — which would skip the early gate and still
// reach the deep guard. resolveParentCommit calls the SAME resolveBranching
// with the SAME arguments a few lines earlier and reopens the bead on error, so
// the loop never gets that far. If that earlier call ever moves or goes away,
// re-measure this reasoning before trusting it.
//
// The previous version of this test tried to build one: it set the daemon
// TargetBranch to "main" and the bead's `## Branching` target_branch to
// "integration", on the belief that the merge still used the daemon-wide
// target. It does not — resolveBranching returns lands_on="integration" for
// that body, so the merge targeted "integration", which is not protected, and
// the guard correctly stayed silent. The test then reported a moved integration
// ref and a closed bead, which reads like a fail-open and is not one. The
// product is correct; the fixture was stale.
//
// This is also the exact shape hk-eun55 asked for: "unit test the merge
// function with target=main, protect=[main]; returns a branch-guard refusal and
// ZERO git mutations".
//
// Bead: hk-eun55 (assertion 2, deep backstop). Refs: hk-6r6xv, hk-lgykq.
func TestBranchGuard_FailClosed_MergeGuardBackstop(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("branchguard-mergeguard-bead-001")

	projectDir := branchGuardSetupRepoWithIntegration(t)

	// A fixed UUIDv7 keeps the run-branch and worktree paths deterministic.
	runID := core.RunID(uuid.MustParse("019628a0-0000-7000-8000-0000000000b5"))

	// Cut a real worktree from main's tip and commit one file on the run branch.
	// The run branch must be genuinely AHEAD of main, or the merge would take
	// the no-change short-circuit and the refusal below would prove nothing.
	headSHA := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	_, cleanup, wtErr := mergeToMainCommittingFactory(t)(t.Context(), projectDir, runID.String(), headSHA)
	if wtErr != nil {
		t.Fatalf("could not create committing worktree: %v", wtErr)
	}
	t.Cleanup(cleanup)

	runBranch := "run/" + runID.String()
	runTip := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/"+runBranch)

	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/main")
	integrationReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/integration")

	// ── Precondition: there IS work to merge. ─────────────────────────────────
	if runTip == mainBefore {
		t.Fatalf("run branch %s is not ahead of main (%s); the refusal below would be vacuous", runBranch, mainBefore)
	}

	// The merge target is "main" AND "main" is protected. Every other input is
	// valid, so the ONLY reason this merge can fail is the branch guard.
	outcome := runmerge.RunBranchToTarget(
		t.Context(),
		nil, // nil Submit → runmerge.InlineSubmit
		projectDir,
		runID,
		&stubEventCollector{},
		beadID,
		headSHA,
		"main",           // targetBranch
		[]string{"main"}, // protectBranches
		"br",             // brPath. The guard refuses before any br call.
	)

	// ── Assertion: the guard refused, and named itself. ───────────────────────
	if outcome.Success {
		t.Errorf("RunBranchToTarget succeeded with target=main protect=[main]; want a branch-guard refusal")
	}
	if outcome.NoChange {
		t.Errorf("outcome.NoChange = true; want a REFUSAL, not a no-change short-circuit (the run branch was ahead of main)")
	}
	if !strings.Contains(outcome.Reason, "merge_target_protected") {
		t.Errorf("outcome.Reason = %q; want it to contain %q (deep guard hk-6r6xv)", outcome.Reason, "merge_target_protected")
	}

	// ── Assertion: ZERO git mutations — every ref + reflog pinned. ────────────
	if mainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main"); mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved despite deep-guard refusal: before=%s after=%s", mainBefore, mainAfter)
	}
	if originMainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main"); originMainAfter != originMainBefore {
		t.Errorf("origin/main moved despite deep-guard refusal: before=%s after=%s", originMainBefore, originMainAfter)
	}
	if integrationAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration"); integrationAfter != integrationBefore {
		t.Errorf("integration moved despite deep-guard refusal: before=%s after=%s", integrationBefore, integrationAfter)
	}
	if mainReflogAfter, _ := branchGuardReflog(t, projectDir, "refs/heads/main"); mainReflogAfter != mainReflogBefore {
		t.Errorf("main reflog changed despite deep-guard refusal:\nBEFORE:\n%s\nAFTER:\n%s", mainReflogBefore, mainReflogAfter)
	}
	if integrationReflogAfter, _ := branchGuardReflog(t, projectDir, "refs/heads/integration"); integrationReflogAfter != integrationReflogBefore {
		t.Errorf("integration reflog changed despite deep-guard refusal:\nBEFORE:\n%s\nAFTER:\n%s", integrationReflogBefore, integrationReflogAfter)
	}

	// ── Assertion: the run branch itself is untouched (no rebase ran). ────────
	if runTipAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/"+runBranch); runTipAfter != runTip {
		t.Errorf("run branch %s moved despite deep-guard refusal: before=%s after=%s; the guard must refuse BEFORE the prepare rebase", runBranch, runTip, runTipAfter)
	}

	if !t.Failed() {
		t.Logf("branchguard deep-guard backstop OK: reason=%q, all refs pinned at main=%s", outcome.Reason, mainBefore[:8])
	}
}

// TestBranchGuard_MergeTargetFollowsBeadLandsOn pins the measurement the test
// above depends on: for a bead body whose `## Branching` section names
// target_branch: integration, resolveBranching returns lands_on="integration"
// EVEN WHEN the daemon-wide target branch is "main".
//
// The work loop feeds that resolved value to BOTH the early lands_on gate and
// the deep merge guard (hk-lgykq). So a protect-set of ["main"] cannot make the
// deep guard fire on this bead — the merge never targets "main". Keep this test
// next to the backstop test: if it ever fails, the per-bead landing rule has
// changed and the "no work-loop fixture reaches the deep guard" reasoning above
// must be re-measured.
//
// Bead: hk-eun55. Refs: hk-lgykq, hk-ncwb3.
func TestBranchGuard_MergeTargetFollowsBeadLandsOn(t *testing.T) {
	t.Parallel()

	body := "## Summary\n\nbranchguard backstop.\n\n## Branching\n\n```yaml\nstart_from: integration\ntarget_branch: integration\n```\n"

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, t.TempDir(), "main")
	if err != nil {
		t.Fatalf("ExportedResolveBranching: %v", err)
	}
	if cfg.LandsOn != "integration" {
		t.Errorf("lands_on = %q; want %q. The bead body must win over the daemon-wide target. "+
			"If it now resolves to \"main\", the deep merge guard is reachable from the work loop again "+
			"and TestBranchGuard_FailClosed_MergeGuardBackstop must be rewritten as a work-loop run.",
			cfg.LandsOn, "integration")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: boot-time validation refuses empty target under forbid-default
// ─────────────────────────────────────────────────────────────────────────────

// TestBranchGuard_BootValidation_RefusesEmptyTargetUnderForbid asserts that
// daemon.Start hard-errors (no socket bind) when ForbidUnprotectedDefault is set
// but TargetBranch is empty (hk-sul12 boot-time fail-closed validation). This is
// the deploy gate's "you cannot run a forbid-default daemon without an explicit
// non-default target branch" guarantee.
//
// Bead: hk-eun55 (assertion 3).
func TestBranchGuard_BootValidation_RefusesEmptyTargetUnderForbid(t *testing.T) {
	t.Parallel()

	projectDir := mergeToMainFixtureProjectDir(t)
	eventsDir := filepath.Join(projectDir, ".harmonik", "events")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}

	cfg := daemon.Config{
		WorkflowModeDefault:      core.WorkflowModeDot,
		ForbidUnprotectedDefault: true,
		TargetBranch:             "", // deliberately absent → resolves to "main", which is the default
	}
	err := daemon.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("daemon.Start returned nil; want a hard error when ForbidUnprotectedDefault=true and TargetBranch empty (no socket bind)")
	}
	if !strings.Contains(err.Error(), "--forbid-default-main") {
		t.Errorf("error %q does not mention --forbid-default-main; want an actionable boot-validation message", err.Error())
	}
}
