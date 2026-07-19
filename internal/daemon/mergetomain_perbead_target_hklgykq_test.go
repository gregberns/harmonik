package daemon_test

// mergetomain_perbead_target_hklgykq_test.go — durable regression test for
// hk-lgykq: a bead whose `## Branching` section names a specific integration
// branch as its target MUST land its commit on THAT branch, NOT the daemon-wide
// target (main).
//
// This mirrors alia's harness assertion T10 (hk-xke2i): per-bead integration
// targeting is a first-class landing property, independent of the daemon's
// configured TargetBranch. The discriminator is the pair of assertions below —
// BEFORE the hk-lgykq fix the run commit lands on main (daemon-wide target);
// AFTER, it lands on the bead-directed integration branch and main is untouched.
//
// Harness: reuses the isolated E2E merge-to-main pattern (real git fixture +
// bare origin remote + recording ledger + stub event collector + committing
// worktree factory) from mergetomain_hkftyvo_test.go / mergetomain_hkcwxow_test.go
// and drives one bead through daemon.ExportedRunWorkLoop — the same composition
// seam the production dispatch path uses. No new harness is introduced.
//
// This file carries NO build tag, so it runs under the plain
//
//	go test ./internal/daemon/ -run TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch
//
// (unlike branchguard_test.go, which is //go:build scenario).
//
// Spec refs: specs/execution-model.md §4.12 EM-052/EM-053; branching.go
// resolveBranching (BI-009b `target_branch` bead-body key → lands_on).
// Beads: hk-lgykq, hk-xke2i (T10).

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// perBeadTargetDescribedLedger wraps the recording ledger so the dispatched
// bead carries a non-empty Description containing a `## Branching` section. Only
// the two read methods are overridden to inject the description; Close/Reopen
// capture is inherited from the embedded recording ledger.
type perBeadTargetDescribedLedger struct {
	*mergeToMainRecordingLedger
	description string
}

func (l *perBeadTargetDescribedLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	recs, err := l.mergeToMainRecordingLedger.Ready(ctx)
	for i := range recs {
		recs[i].Description = l.description
	}
	return recs, err
}

func (l *perBeadTargetDescribedLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	rec, err := l.mergeToMainRecordingLedger.ShowBead(ctx, id)
	rec.Description = l.description
	return rec, err
}

// perBeadTargetGit runs a git command in dir, failing the test on error, and
// returns the trimmed stdout.
func perBeadTargetGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("perBeadTargetGit: git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch proves that a bead whose
// `## Branching` section directs it to `integration/lgykq-e2e` lands its commit
// on that branch while the daemon-wide target (main) is left untouched, and the
// bead is closed as a clean success.
//
// Beads: hk-lgykq, hk-xke2i (T10).
func TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-perbead-target-lgykq-001")
	const integrationBranch = "integration/lgykq-e2e"

	// ── Fixture: project repo (main + one commit) + bare origin remote. ────────
	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	originDir := t.TempDir()
	perBeadTargetGit(t, originDir, "init", "--bare", "--initial-branch=main")
	perBeadTargetGit(t, projectDir, "remote", "add", "origin", originDir)
	perBeadTargetGit(t, projectDir, "push", "origin", "main")

	// ── Create the integration branch (off main) in BOTH project + origin so the
	//    landing helpers (which fail-close on a missing ref) find a valid ref. ──
	perBeadTargetGit(t, projectDir, "branch", integrationBranch)
	perBeadTargetGit(t, projectDir, "push", "origin", integrationBranch)

	// ── Snapshot the pre-run tips (the discriminator baseline). ────────────────
	mainBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/"+integrationBranch)

	// ── Bead body: `## Branching` section directing this bead to the integration
	//    branch (start_from + target_branch, the exact BI-009b fenced-YAML keys
	//    parseBranchingSection accepts). Daemon target stays "main". ────────────
	body := "## Summary\n\nper-bead integration targeting (hk-lgykq / T10).\n\n" +
		"## Branching\n\n```yaml\nstart_from: " + integrationBranch + "\ntarget_branch: " + integrationBranch + "\n```\n"

	recording := newMergeToMainRecordingLedger(beadID)
	ledger := &perBeadTargetDescribedLedger{mergeToMainRecordingLedger: recording, description: body}
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
		TargetBranch:     "main", // daemon-wide target — the bead must OVERRIDE this
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-recording.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatal("timed out waiting for bead close/reopen")
	}

	select {
	case err := <-loopDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("work loop returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("work loop did not exit within 5s")
	}

	// ── Assertion 1: the integration branch ADVANCED and now CONTAINS the run's
	//    committed work. This is the "lands on the bead-directed branch" half. ──
	integrationAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/"+integrationBranch)
	if integrationAfter == integrationBefore {
		t.Errorf("integration branch %q unchanged (%s); want it to advance to carry the run commit (hk-lgykq)",
			integrationBranch, integrationBefore)
	}
	// The committing factory commits "feat: agent work" onto the run-branch; it
	// must be reachable from the integration branch tip after the land.
	intLog := perBeadTargetGit(t, projectDir, "log", "--oneline", "refs/heads/"+integrationBranch)
	if !strings.Contains(intLog, "agent work") {
		t.Errorf("integration branch %q log does not contain the run commit (\"agent work\"):\n%s", integrationBranch, intLog)
	}

	// ── Assertion 2: main did NOT advance (the discriminator). Before the
	//    hk-lgykq fix the commit lands here; after, main is byte-for-byte pinned. ─
	mainAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/main")
	if mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved: before=%s after=%s; want UNCHANGED (bead targeted %q, not main) — hk-lgykq regression",
			mainBefore, mainAfter, integrationBranch)
	}
	originMainAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	if originMainAfter != originMainBefore {
		t.Errorf("origin/main moved: before=%s after=%s; want UNCHANGED (no push touched main)", originMainBefore, originMainAfter)
	}
	// The run commit must NOT be reachable from main.
	mainLog := perBeadTargetGit(t, projectDir, "log", "--oneline", "refs/heads/main")
	if strings.Contains(mainLog, "agent work") {
		t.Errorf("main log contains the run commit (\"agent work\"); it must land on %q only — hk-lgykq regression:\n%s",
			integrationBranch, mainLog)
	}

	// ── Assertion 3: bead CLOSED as a clean success (not reopened). ────────────
	if got := recording.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (clean successful land on %q)", got, integrationBranch)
	}
	if got := recording.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on a successful per-bead-target land", got)
	}

	t.Logf("hk-lgykq/T10 OK: bead landed on %s (%s → %s); main pinned at %s",
		integrationBranch, integrationBefore[:8], integrationAfter[:8], mainBefore[:8])
}
