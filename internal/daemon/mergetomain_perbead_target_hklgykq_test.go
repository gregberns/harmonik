package daemon_test

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

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	originDir := t.TempDir()
	perBeadTargetGit(t, originDir, "init", "--bare", "--initial-branch=main")
	perBeadTargetGit(t, projectDir, "remote", "add", "origin", originDir)
	perBeadTargetGit(t, projectDir, "push", "origin", "main")

	perBeadTargetGit(t, projectDir, "branch", integrationBranch)
	perBeadTargetGit(t, projectDir, "push", "origin", integrationBranch)

	mainBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/main")
	originMainBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	integrationBefore := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/"+integrationBranch)

	body := "## Summary\n\nper-bead integration targeting (hk-lgykq / T10).\n\n" +
		"## Branching\n\n```yaml\nstart_from: " + integrationBranch + "\ntarget_branch: " + integrationBranch + "\n```\n"

	recording := newMergeToMainRecordingLedger(beadID)
	ledger := &perBeadTargetDescribedLedger{mergeToMainRecordingLedger: recording, description: body}
	collector := &stubEventCollector{}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      mergeToMainCommittingHandlerArgs(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
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

	if err := awaitLoopTeardownErr(t, loopDone, "work loop"); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("work loop returned unexpected error: %v", err)
	}

	integrationAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/"+integrationBranch)
	if integrationAfter == integrationBefore {
		t.Errorf("integration branch %q unchanged (%s); want it to advance to carry the run commit (hk-lgykq)",
			integrationBranch, integrationBefore)
	}
	intLog := perBeadTargetGit(t, projectDir, "log", "--oneline", "refs/heads/"+integrationBranch)
	if !strings.Contains(intLog, "agent work") {
		t.Errorf("integration branch %q log does not contain the run commit (\"agent work\"):\n%s", integrationBranch, intLog)
	}

	mainAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/heads/main")
	if mainAfter != mainBefore {
		t.Errorf("refs/heads/main moved: before=%s after=%s; want UNCHANGED (bead targeted %q, not main) — hk-lgykq regression",
			mainBefore, mainAfter, integrationBranch)
	}
	originMainAfter := perBeadTargetGit(t, projectDir, "rev-parse", "refs/remotes/origin/main")
	if originMainAfter != originMainBefore {
		t.Errorf("origin/main moved: before=%s after=%s; want UNCHANGED (no push touched main)", originMainBefore, originMainAfter)
	}
	mainLog := perBeadTargetGit(t, projectDir, "log", "--oneline", "refs/heads/main")
	if strings.Contains(mainLog, "agent work") {
		t.Errorf("main log contains the run commit (\"agent work\"); it must land on %q only — hk-lgykq regression:\n%s",
			integrationBranch, mainLog)
	}

	if got := recording.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (clean successful land on %q)", got, integrationBranch)
	}
	if got := recording.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on a successful per-bead-target land", got)
	}

	t.Logf("hk-lgykq/T10 OK: bead landed on %s (%s → %s); main pinned at %s",
		integrationBranch, integrationBefore[:8], integrationAfter[:8], mainBefore[:8])
}
