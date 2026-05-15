package daemon_test

// worktreerefresh_hk4goy3_test.go — integration tests for the §4.12.EM-054
// working-tree-refresh requirement (bead hk-4goy3).
//
// After mergeRunBranchToMain advances refs/heads/main via git update-ref +
// push, the daemon MUST run git reset --hard HEAD in the project root so that
// the project working tree reflects the new HEAD. Without this step, files
// modified by the run-branch commit appear as "modified" in git status even
// though HEAD already contains the new content.
//
// Test assertions per §10.2 EM-054 obligation:
//   (i)  After a successful merge, git status --porcelain is empty for the
//        file the run-branch commit modified (working tree matches HEAD).
//   (ii) CloseBead is still called (merge succeeded; refresh failure must not
//        reopen the bead).
//
// Helper prefix: wtRefreshFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-4goy3).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-054
//
// Bead: hk-4goy3.

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// wtRefreshFixtureOriginDir creates a bare git repo and wires it as "origin"
// for the project repo so git push succeeds during tests.
func wtRefreshFixtureOriginDir(t *testing.T, projectDir string) {
	t.Helper()
	originDir := t.TempDir()

	initCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("wtRefreshFixtureOriginDir: git init --bare: %v\n%s", err, out)
	}

	addCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("wtRefreshFixtureOriginDir: git remote add origin: %v\n%s", err, out)
	}

	pushCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushCmd.Dir = projectDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		t.Fatalf("wtRefreshFixtureOriginDir: git push origin main (initial): %v\n%s", err, out)
	}
}

// wtRefreshFixtureGitStatus returns the output of git status --porcelain for
// the given file in repoRoot. Returns "" when the file is clean.
func wtRefreshFixtureGitStatus(t *testing.T, repoRoot, relPath string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "status", "--porcelain", relPath)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("wtRefreshFixtureGitStatus: git status --porcelain %s: %v", relPath, err)
	}
	return strings.TrimRight(string(out), "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: working-tree is synced after successful merge (EM-054 assertion i)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkingTreeRefresh_AfterSuccessfulMerge verifies that after the daemon
// executes the merge-to-main sequence (EM-052), the project working tree is
// refreshed (EM-054): git status --porcelain for the file committed by the
// run-branch is empty (the working tree matches HEAD).
//
// The test uses mergeToMainCommittingFactory so the run-branch adds a
// "work.txt" file. Before EM-054 the project working tree would show
// "work.txt" as an untracked/modified file after the merge.
//
// Spec refs: specs/execution-model.md §4.12 EM-054.
// Bead: hk-4goy3.
func TestWorkingTreeRefresh_AfterSuccessfulMerge(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("worktreerefresh-em054-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)
	wtRefreshFixtureOriginDir(t, projectDir)

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// The handler exits 0 — triggers the auto-close heuristic (branch 2).
	// The worktreeFactory commits "work.txt" onto the run-branch so the merge
	// is non-trivial and git reset --hard HEAD must update the working tree.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:       ledger,
		Bus:             collector,
		ProjectDir:      projectDir,
		HandlerBinary:   "/bin/sh",
		HandlerArgs:     []string{"-c", "exit 0"},
		IntentLogDir:    filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorktreeFactory: mergeToMainCommittingFactory(t),
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

	// ── Assertion: CloseBead called exactly once (merge succeeded). ───────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on success path", got)
	}

	// ── Assertion (i): working tree is clean for work.txt after merge. ────────
	//
	// Before EM-054: after update-ref advanced main to the run-branch tip, the
	// project working tree still had the OLD content of README (and did not yet
	// have work.txt). git status would show work.txt untracked or README as
	// modified. After EM-054's git reset --hard HEAD, work.txt must exist in
	// the working tree and be clean.
	status := wtRefreshFixtureGitStatus(t, projectDir, "work.txt")
	if status != "" {
		t.Errorf("git status --porcelain work.txt = %q after merge-to-main; want empty (EM-054: working tree must match HEAD)", status)
	}

	// Also verify that no working_tree_refresh_failed event was emitted
	// (refresh succeeded, so no warning event).
	refreshFailed := mergeToMainFindEvents(collector, "working_tree_refresh_failed")
	if len(refreshFailed) > 0 {
		t.Errorf("working_tree_refresh_failed emitted on success path; want absent: %v", refreshFailed)
	}

	t.Logf("EM-054 working-tree-refresh OK: work.txt is clean in project working tree after merge")
}
