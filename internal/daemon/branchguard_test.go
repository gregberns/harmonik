//go:build scenario

package daemon_test

// branchguard_test.go — the deployment-gate scenario test for the
// integration-branch productization work (hk-eun55).
//
// These tests run a bead through the REAL work loop (daemon.ExportedRunWorkLoop
// + daemon.ExportedWorkLoopDeps, the same composition seam the production
// dispatch path uses) and assert the load-bearing branch-protection guarantees
// landed by hk-mkxw1 / hk-6r6xv / hk-ncwb3 / hk-sul12:
//
//	1. TargetBranchMergeIsolation — with TargetBranch="integration" and
//	   ProtectBranches=["main"], a committing bead MERGES to the integration
//	   branch, and main is provably untouched: refs/heads/main rev-parse,
//	   origin/main, AND main's REFLOG are byte-for-byte unchanged while
//	   refs/heads/integration advances to the run-branch tip.
//
//	2. FailClosed_TargetInProtectSet — with TargetBranch="main" and
//	   ProtectBranches=["main"], the fail-closed guard at the top of
//	   mergeRunBranchToMain (hk-6r6xv) REFUSES the merge BEFORE any
//	   update-ref/push. We assert ZERO git side effects (main, origin/main,
//	   integration all unchanged), the bead is reopened (not closed), and the
//	   emitted outcome is rejected with reason "merge_target_protected".
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
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052/EM-053 (ordered merge sequence)
//   - hk-6r6xv (fail-closed merge guard), hk-ncwb3 (start_from retarget),
//     hk-mkxw1 (Config branch fields), hk-sul12 (boot-time validation)
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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
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
// protect-set is refused by the EARLIER lands_on-protection gate (hk-ncwb3,
// workloop.go ~1688) — BEFORE a worktree is cut or run_started is emitted —
// because resolveBranching defaults the bead's lands_on to the configured target
// ("main"), which is protected. This is a STRONGER fail-closed than the deep
// merge-function guard (hk-6r6xv): the protected-target bead never even gets a
// worktree. The merge-function guard remains the last-line backstop for any bead
// that slips past the lands_on gate; it is unit-covered separately. This test
// therefore asserts the load-bearing invariant the productization gate cares
// about — ZERO git side effects + reopen + not-closed + a protected-branch
// refusal reason — and accepts whichever guard fires.
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
	//    lands_on gate (hk-ncwb3) reopens with "protected" in its message; the
	//    deep merge guard (hk-6r6xv) reopens with "merge_target_protected".
	//    Accept whichever fired — both are valid fail-closed paths. ────────────
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
// Test 2b: deep merge-function guard (hk-6r6xv) refuses a protected daemon
// target even when the bead's lands_on slips past the early lands_on gate.
// ─────────────────────────────────────────────────────────────────────────────

// TestBranchGuard_FailClosed_MergeGuardBackstop exercises the LAST-LINE
// fail-closed guard at the top of mergeRunBranchToMain (hk-6r6xv): the daemon's
// configured TargetBranch is "main" (protected), but the bead's `## Branching`
// section sets lands_on/start_from to "integration" — a non-protected branch —
// so the EARLY lands_on gate (hk-ncwb3) passes and a worktree IS cut and
// committed. The merge then targets the daemon's protected TargetBranch="main"
// and the deep guard must refuse it BEFORE any update-ref/push, leaving ZERO git
// mutations and reopening the bead with "merge_target_protected".
//
// This is the unit-level guarantee the bead names ("unit test
// mergeRunBranchToMain target=main/protect=[main] returns branch_guard + ZERO
// git mutations"), exercised end-to-end through the real work loop.
//
// Bead: hk-eun55 (assertion 2, deep backstop).
func TestBranchGuard_FailClosed_MergeGuardBackstop(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("branchguard-mergeguard-bead-001")

	projectDir := branchGuardSetupRepoWithIntegration(t)

	// Bead body lands on integration (NOT protected) so the early lands_on gate
	// passes; the worktree cuts from integration and commits there.
	body := "## Summary\n\nbranchguard backstop.\n\n## Branching\n\n```yaml\nstart_from: integration\ntarget_branch: integration\n```\n"

	mainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := branchGuardGit(t, projectDir, "rev-parse", "refs/heads/integration")
	mainReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/main")
	integrationReflogBefore, _ := branchGuardReflog(t, projectDir, "refs/heads/integration")

	// Daemon target = "main" (protected). The merge call uses deps.targetBranch
	// ("main"), so the deep hk-6r6xv guard fires when the merge is attempted.
	ledger, collector := branchGuardRunBead(t, projectDir, beadID, "main", []string{"main"}, body)

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

	// ── Assertion: bead REOPENED, NOT closed. ─────────────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 when deep guard refuses protected target", got)
	}
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥1 when deep guard refuses protected target", got)
	}

	// ── Assertion: outcome_emitted{kind=rejected, reason=merge_target_protected}.
	//    The deep guard path emits an outcome before reopening (unlike the early
	//    lands_on gate). ────────────────────────────────────────────────────────
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Fatalf("no outcome_emitted events; stream: %v", mergeToMainEventOrder(collector))
	}
	if kind := mergeToMainPayloadKind(t, outcomeEvs[0]); kind != "rejected" {
		t.Errorf("outcome_emitted kind = %q; want %q", kind, "rejected")
	}
	if reason := mergeToMainPayloadReason(t, outcomeEvs[0]); !strings.Contains(reason, "merge_target_protected") {
		t.Errorf("outcome_emitted reason %q does not contain %q (deep guard hk-6r6xv)", reason, "merge_target_protected")
	}

	// ── Assertion: bead_closed event MUST NOT appear. ─────────────────────────
	if evs := mergeToMainFindEvents(collector, "bead_closed"); len(evs) > 0 {
		t.Errorf("bead_closed emitted despite deep-guard refusal; want absent: %v", mergeToMainEventOrder(collector))
	}

	t.Logf("branchguard deep-guard backstop OK: protected daemon target refused at merge, all refs pinned, bead reopened")
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
		WorkflowModeDefault:      core.WorkflowModeReviewLoop,
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
