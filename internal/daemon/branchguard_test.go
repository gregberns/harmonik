//go:build scenario

package daemon_test

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

func branchGuardSetupRepoWithIntegration(t *testing.T) string {
	t.Helper()

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir) // main + one initial commit

	branchGuardGit(t, projectDir, "branch", "integration")

	originDir := t.TempDir()
	branchGuardGit(t, originDir, "init", "--bare", "--initial-branch=main")
	branchGuardGit(t, projectDir, "remote", "add", "origin", originDir)
	branchGuardGit(t, projectDir, "push", "origin", "main")
	branchGuardGit(t, projectDir, "push", "origin", "integration")

	return projectDir
}

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      mergeToMainCommittingHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
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

	select {
	case <-recording.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatal("branchGuardRunBead: timed out waiting for bead close/reopen")
	}

	awaitLoopTeardown(t, loopDone, "work loop")

	return recording, collector
}

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

	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, mainReflogOK := branchGuardReflog(t, projectDir, "refs/heads/main")
	if !mainReflogOK {
		t.Fatal("could not capture main reflog before run")
	}

	ledger, collector := branchGuardRunBead(t, projectDir, beadID, "integration", []string{"main"}, "")

	integrationAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	if integrationAfter == integrationBefore {
		t.Errorf("integration HEAD unchanged after committing run: still %s; want run-branch tip", integrationBefore)
	}

	mainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	if mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved: before=%s after=%s; want UNCHANGED (target was integration)", mainBefore, mainAfter)
	}

	originMainAfter := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	if originMainAfter != originMainBefore {
		t.Errorf("origin/main moved: before=%s after=%s; want UNCHANGED", originMainBefore, originMainAfter)
	}

	mainReflogAfter, ok := branchGuardReflog(t, projectDir, "refs/heads/main")
	if !ok {
		t.Fatal("could not capture main reflog after run")
	}
	if mainReflogAfter != mainReflogBefore {
		t.Errorf("main reflog changed during integration-target run:\nBEFORE:\n%s\nAFTER:\n%s\nwant byte-for-byte unchanged", mainReflogBefore, mainReflogAfter)
	}

	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (integration merge should succeed)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on successful integration merge", got)
	}

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

	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/main")

	ledger, collector := branchGuardRunBead(t, projectDir, beadID, "main", []string{"main"}, "")

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

	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 when daemon refuses protected target", got)
	}
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥1 when daemon refuses protected target", got)
	}

	reopenReason := ledger.getReopenReason()
	if !strings.Contains(reopenReason, "protected") {
		t.Errorf("reopen reason %q does not indicate a protected-branch refusal", reopenReason)
	}

	if evs := mergeToMainFindEvents(collector, "bead_closed"); len(evs) > 0 {
		t.Errorf("bead_closed emitted despite guard refusal; want absent: %v", mergeToMainEventOrder(collector))
	}

	t.Logf("branchguard fail-closed OK: protected target refused (reason=%q), all refs pinned, bead reopened", reopenReason)
}

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

	runID := core.RunID(uuid.MustParse("019628a0-0000-7000-8000-0000000000b5"))

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

	if runTip == mainBefore {
		t.Fatalf("run branch %s is not ahead of main (%s); the refusal below would be vacuous", runBranch, mainBefore)
	}

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

	if outcome.Success {
		t.Errorf("RunBranchToTarget succeeded with target=main protect=[main]; want a branch-guard refusal")
	}
	if outcome.NoChange {
		t.Errorf("outcome.NoChange = true; want a REFUSAL, not a no-change short-circuit (the run branch was ahead of main)")
	}
	if !strings.Contains(outcome.Reason, "merge_target_protected") {
		t.Errorf("outcome.Reason = %q; want it to contain %q (deep guard hk-6r6xv)", outcome.Reason, "merge_target_protected")
	}

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
