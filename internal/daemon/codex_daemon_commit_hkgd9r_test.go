package daemon_test

// codex_daemon_commit_hkgd9r_test.go — regression for hk-gd9r: daemon-side
// commit fallback for codex ProcessExit harness.
//
// # What this test proves
//
// codex runs with --sandbox workspace-write, which blocks writes to .git. In a
// git worktree the .git entry is a FILE pointing to the MAIN repo's
// .git/worktrees/<run-id>/ directory, which is OUTSIDE the sandbox root, so
// codex self-commit fails 100% of the time
// ("fatal: Unable to create .git/index.lock").
//
// The fix (hk-gd9r) wires ensureCodexRefsTrailer (codexcommit.go:180) as a
// daemon-side commit-after-exit in workloop.go: after the codex process exits
// (CompletionProcessExit) the daemon stages+commits any worktree changes with
// the "Refs: <beadID>" trailer.
//
// This test drives a shell script that:
//   - creates a file in the worktree, AND
//   - exits 0 WITHOUT committing (simulating sandbox-blocked self-commit).
//
// The bead MUST be closed (not reopened), proving that the daemon-side fallback
// created the required trailer commit.
//
// Helper prefix: hkgd9r (per implementer-protocol.md §Helper-prefix discipline).
//
// Spec ref: specs/harness-contract.md §2 N2/N5; specs/process-lifecycle.md §4.
// Bead ref: hk-gd9r.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkgd9rFixtureEditOnlyScript writes a shell script that creates a file in the
// worktree and exits 0 WITHOUT committing — simulating a codex run whose
// sandbox blocks .git writes so self-commit always fails.
//
// The script does NOT emit agent_ready (codex ProcessExit harness never does).
func hkgd9rFixtureEditOnlyScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-edit-no-commit.sh")
	content := "#!/bin/sh\n" +
		"set -e\n" +
		"export PATH=" + os.Getenv("PATH") + "\n" +
		// Write a file in the worktree but do NOT run git add or git commit,
		// simulating the codex sandbox blocking .git writes.
		"echo hkgd9r-codex-output > codex-output.txt\n"
	//nolint:gosec // test-only script; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("hkgd9rFixtureEditOnlyScript: WriteFile: %v", err)
	}
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodexDaemonSideCommitFallback_SingleMode (hk-gd9r regression)
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexDaemonSideCommitFallback_SingleMode verifies that a codex bead whose
// script edits files but does NOT commit is nevertheless closed successfully
// after the daemon-side ensureCodexRefsTrailer fallback creates the required
// trailer commit.
//
// Pre-hk-gd9r: the bead would be reopened with "no_commit_during_implementer"
// because codex could not commit through the sandbox.
// Post-hk-gd9r: the daemon commits on codex's behalf and the bead closes.
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
func TestCodexDaemonSideCommitFallback_SingleMode(t *testing.T) {
	// Build a project dir + git repo (productionWorktreeFactory needs `git worktree add`).
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust away from ~/.claude.json so this test does
	// not contend with a running daemon (same pattern as hk-f6g7 test).
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	const beadID = core.BeadID("hkgd9r-codex-daemon-commit")
	ledger := &stubBeadLedger{ready: []core.BeadID{beadID}}
	collector := &stubEventCollector{}

	// Edit-only script: writes a file but never calls git add/commit.
	// This simulates codex's sandbox blocking .git writes.
	scriptPath := hkgd9rFixtureEditOnlyScript(t)

	// HarnessRegistry: registers CodexHarness (Completion() == CompletionProcessExit).
	// Required so ensureCodexRefsTrailer fires in the workloop.
	harnessReg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          collector,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// CodexAdapter: ForAgent(codex) succeeds → code enters the completion-mode
		// check branch (required for both hk-f6g7 and hk-gd9r paths).
		AdapterRegistry2: NewCodexSealedAdapterRegistryForTest(t),
		// CodexHarness: Completion() == CompletionProcessExit → skips waitAgentReady
		// AND triggers ensureCodexRefsTrailer in the workloop (the hk-gd9r fix).
		HarnessRegistry: harnessReg,
		// LaunchSpecBuilder that stamps resolvedAgentType=codex and runs the edit-only
		// script in the worktree directory.
		LaunchSpecBuilder: daemon.ExportedCodexProcessExitLaunchSpecBuilder(scriptPath),
		// Short agent-ready timeout: the script never emits agent_ready; this must
		// NOT cause a timeout reopen (CompletionProcessExit skips waitAgentReady).
		AgentReadyTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead reaches a terminal state (CloseBead or ReopenBead).
	terminalDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("hkgd9r: work loop did not exit within 5 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()
	t.Logf("hkgd9r: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: CloseBead called (daemon-side fallback committed) ────────
	//
	// Pre-fix: the bead is reopened with "no_commit_during_implementer" because
	// codex never committed (sandbox blocks .git). Post-fix: ensureCodexRefsTrailer
	// stages+commits the worktree changes and the bead closes successfully.
	if len(closedIDs) == 0 {
		t.Errorf("hkgd9r FAIL: CloseBead not called; bead %q never completed\n"+
			"reopenedIDs=%v\n"+
			"If reopenedIDs contains 'no_commit_during_implementer', the daemon-side\n"+
			"commit fallback (hk-gd9r) is absent or not firing.",
			beadID, reopenedIDs)
	} else if closedIDs[0] != beadID {
		t.Errorf("hkgd9r FAIL: closed bead = %q; want %q", closedIDs[0], beadID)
	}

	// ── Assertion 2: ReopenBead NOT called ───────────────────────────────────
	if len(reopenedIDs) > 0 {
		t.Errorf("hkgd9r FAIL: ReopenBead called unexpectedly: %v\n"+
			"A 'no_commit_during_implementer' reopen means ensureCodexRefsTrailer\n"+
			"did not create the daemon-side commit (hk-gd9r fallback absent).",
			reopenedIDs)
	}

	// ── Assertion 3: run_completed emitted ───────────────────────────────────
	hasRunCompleted := false
	for _, et := range emittedTypes {
		if et == string(core.EventTypeRunCompleted) {
			hasRunCompleted = true
			break
		}
	}
	if !hasRunCompleted {
		t.Errorf("hkgd9r FAIL: run_completed event not emitted (events=%v)", emittedTypes)
	}
}
