package daemon_test

// process_exit_harness_hkf6g7_test.go — regression for hk-f6g7: ProcessExit
// harness (codex) must NOT hit HC-056 waitAgentReady timeout.
//
// # What this test proves
//
// Before hk-f6g7, waitAgentReady was called UNCONDITIONALLY in single-mode
// (workloop.go), review-loop (reviewloop.go), and DOT (dot_cascade.go).  The
// codex harness (Completion() == CompletionProcessExit) never emits agent_ready
// because it has no hook relay; so every codex run hit ErrAgentReadyTimeout
// (HC-056, 90 s default) and was either reopened (single-mode) or errored
// (review-loop/dot).  Codex was not end-to-end runnable.
//
// After hk-f6g7 the three sites gate on Completion() before calling
// waitAgentReady.  This test verifies the fix for single-mode: a bead dispatched
// via ExportedRunWorkLoop with:
//   - AdapterRegistry2 containing the real CodexAdapter (so ForAgent succeeds →
//     code enters the completion-mode check branch rather than the earlier nil skip),
//   - HarnessRegistry containing CodexHarness (Completion() == CompletionProcessExit),
//   - a shell script that commits and exits WITHOUT emitting agent_ready,
//   - AgentReadyTimeout of 2 s (fast failure if waitAgentReady is called),
//
// must close (not reopen) the bead and emit run_completed.
//
// Helper prefix: hkf6g7 (per implementer-protocol.md §Helper-prefix discipline).
//
// Spec ref: specs/handler-contract.md §4.9 HC-056; specs/harness-contract.md §2 N5.
// Bead ref: hk-f6g7.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkf6g7FixtureCommitScript writes a shell script that makes a single git commit
// with the required "Refs: <beadID>" trailer and exits 0 immediately — no
// agent_ready event is ever emitted on stdout/stderr.
//
// The script re-exports the test process's PATH (ExportedWorkLoopDeps sets
// handlerEnv=nil so the child inherits nothing; git must be findable).
func hkf6g7FixtureCommitScript(t *testing.T, beadID core.BeadID) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-commit-exit.sh")
	// Redirect stdout to stderr so the watcher sees clean EOF on stdout
	// (git commit writes progress to stdout; the NDJSON watcher would mis-parse it).
	// Use printf for the commit message body so the Refs: trailer lands on its own
	// line (single-quoted sh strings do not expand \n escape sequences).
	content := "#!/bin/sh\n" +
		"set -e\n" +
		"export PATH=" + os.Getenv("PATH") + "\n" +
		"git config user.email test@harmonik.local\n" +
		"git config user.name 'Harmonik Test'\n" +
		"echo hkf6g7 > .codex-change\n" +
		"git add .codex-change\n" +
		"MSG=$(printf 'hkf6g7 ProcessExit test\\n\\nRefs: " + string(beadID) + "')\n" +
		"git commit -m \"$MSG\" >/dev/null 2>&1\n"
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("hkf6g7FixtureCommitScript: WriteFile: %v", err)
	}
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// TestProcessExitHarness_SingleMode_SkipsAgentReadyGate (hk-f6g7 regression)
// ─────────────────────────────────────────────────────────────────────────────

// TestProcessExitHarness_SingleMode_SkipsAgentReadyGate verifies that a bead
// dispatched with a ProcessExit harness (codex) completes without hitting the
// HC-056 waitAgentReady timeout in single-mode.
//
// The script never emits agent_ready.  AgentReadyTimeout is set to 2 s so the
// test would fail within 2 s if the fix is absent (prior to hk-f6g7 the run
// would hit ErrAgentReadyTimeout and reopen the bead).
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
func TestProcessExitHarness_SingleMode_SkipsAgentReadyGate(t *testing.T) {
	// Build a project dir + git repo (productionWorktreeFactory needs `git worktree add`).
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust away from ~/.claude.json so the test does not
	// contend with a running daemon (same pattern as SC-1 test hk-x3s1p).
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	const beadID = core.BeadID("hkf6g7-process-exit-codex")
	ledger := &stubBeadLedger{ready: []core.BeadID{beadID}}
	collector := &stubEventCollector{}

	// Commit-and-exit script — NO agent_ready emitted.
	scriptPath := hkf6g7FixtureCommitScript(t, beadID)

	// HarnessRegistry: claude + codex registered (same as production).
	harnessReg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          collector,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// AdapterRegistry2 with CodexAdapter: ForAgent(codex) succeeds →
		// code enters the else branch and hits the hk-f6g7 completion-mode check.
		AdapterRegistry2: NewCodexSealedAdapterRegistryForTest(t),
		// HarnessRegistry with CodexHarness: Completion() == CompletionProcessExit
		// → waitAgentReady is skipped (the fix under test).
		HarnessRegistry: harnessReg,
		// LaunchSpecBuilder that stamps resolvedAgentType=codex so the adapter and
		// harness lookups use the codex path.
		LaunchSpecBuilder: daemon.ExportedCodexProcessExitLaunchSpecBuilder(scriptPath),
		// Short timeout: if waitAgentReady IS called (bug regression), the test
		// fails within 2 s instead of the 90 s production default.
		AgentReadyTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until the bead reaches a terminal state (CloseBead or ReopenBead).
	terminalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("hkf6g7: work loop did not exit within 5 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()
	t.Logf("hkf6g7: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: CloseBead called (bead reached success) ─────────────────
	if len(closedIDs) == 0 {
		t.Errorf("hkf6g7 FAIL: CloseBead not called; bead %q never completed (reopenedIDs=%v)\n"+
			"If reopenedIDs contains 'agent_ready_timeout', the HC-056 fix is absent.",
			beadID, reopenedIDs)
	} else if closedIDs[0] != beadID {
		t.Errorf("hkf6g7 FAIL: closed bead = %q; want %q", closedIDs[0], beadID)
	}

	// ── Assertion 2: ReopenBead NOT called ───────────────────────────────────
	if len(reopenedIDs) > 0 {
		t.Errorf("hkf6g7 FAIL: ReopenBead called unexpectedly: %v\n"+
			"This indicates the bead was reopened — likely due to an HC-056 agent_ready_timeout\n"+
			"(ProcessExit harness must skip waitAgentReady per hk-f6g7).",
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
		t.Errorf("hkf6g7 FAIL: run_completed event not emitted (events=%v)", emittedTypes)
	}

	// ── Assertion 4: agent_ready NOT expected ────────────────────────────────
	// The script never emits agent_ready; if it appears something synthesised it
	// unexpectedly. This is a soft warning, not a hard failure.
	for _, et := range emittedTypes {
		if et == string(core.EventTypeAgentReady) {
			t.Logf("hkf6g7 NOTE: agent_ready observed even though script never emitted it — " +
				"may be a synthetic event from the tap infrastructure (not a failure)")
			break
		}
	}
}
