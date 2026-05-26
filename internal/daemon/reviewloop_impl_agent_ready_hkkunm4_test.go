package daemon_test

// reviewloop_impl_agent_ready_hkkunm4_test.go — scenario test: implementer
// agent_ready_timeout fires correctly in the review-loop implementer path.
//
// # What this tests
//
// When the review-loop implementer phase exceeds agentReadyTimeout without
// emitting agent_ready, runReviewLoop must:
//  1. Kill the implementer process.
//  2. Reap within agentReadyKillReapTimeout.
//  3. Return an error result (success=false, completionReason=error).
//
// This exercises the fix for hk-kunm4: prior to the fix, the implementer
// path had no waitAgentReady gate — paste-inject fired immediately after
// Launch, racing Claude's REPL readiness. Now the timeout path fires when
// the implementer never signals agent_ready, proving the gate is wired.
//
// # Ordering contract
//
// The test also implicitly validates that pasteInjectOnLaunch is NOT called
// until after waitAgentReady returns: if paste-inject ran before the gate,
// the test handler (which hangs indefinitely) would never produce a commit,
// but the failure mode would be a hung test (no timeout), not the clean
// error result we assert. The fact that the error result arrives promptly
// (within the 100ms timeout + kill/reap overhead) proves the gate is active.
//
// Bead: hk-kunm4.

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
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// implReadyFixtureProjectDir creates the minimal project directory tree for
// the implementer-ready test: .harmonik/events/ and .harmonik/beads-intents/,
// then initialises a git repo with one initial commit.
func implReadyFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("implReadyFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("implReadyFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("implReadyFixtureProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("impl-ready scenario test repo\n"), 0o644); err != nil {
		t.Fatalf("implReadyFixtureProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// implReadyFixtureWorktree creates a detached git worktree under projectDir
// and returns the worktree path and parent commit SHA.
func implReadyFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("implReadyFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("implReadyFixtureWorktree: git worktree add: %v\n%s", addErr, addOut)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("implReadyFixtureWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// implReadyFixtureRunID generates a fresh RunID using UUIDv7.
func implReadyFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("implReadyFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// implReadyFixtureHandlerScript writes a shell handler that hangs indefinitely
// (sleeps for 1 hour) without emitting agent_ready. This simulates the
// implementer never becoming ready, which should trigger the agent_ready
// timeout in the implementer phase.
func implReadyFixtureHandlerScript(t *testing.T) string {
	t.Helper()
	script := `#!/bin/sh
# Hang indefinitely — never emit agent_ready.
# The 100ms agentReadyTimeout in the test fires long before the 1-hour sleep.
sleep 3600
`
	scriptPath := filepath.Join(t.TempDir(), "impl_hang_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("implReadyFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// implReadyFixtureAdapterRegistry builds an AdapterRegistry with
// ClaudeCodeAdapter registered. Required for waitAgentReady to fire.
func implReadyFixtureAdapterRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("implReadyFixtureAdapterRegistry: Register ClaudeCodeAdapter: %v", err)
	}
	return reg
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_ImplementerAgentReadyTimeout verifies that the
// implementer phase of runReviewLoop now gates on waitAgentReady (hk-kunm4).
//
// Setup:
//   - Handler script hangs indefinitely (never emits agent_ready).
//   - agentReadyTimeout = 100ms — fires promptly.
//
// Expectations:
//   - runReviewLoop returns success=false, completionReason="error".
//   - The error result proves waitAgentReady is wired in the implementer path:
//     without the gate, the implementer would hang indefinitely (no timeout),
//     and the test's outer 30s context would cancel instead.
//   - agent_ready_timeout event is emitted to the event bus.
//   - agent_ready event is NOT emitted (handler never signals readiness).
//
// Bead: hk-kunm4.
func TestScenario_ReviewLoop_ImplementerAgentReadyTimeout(t *testing.T) {
	t.Parallel()

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    adapterReg,
		// 100ms timeout: the handler hangs for 3600s, so this fires first.
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("impl-agent-ready-timeout-001"),
		wtPath, parentSHA,
	)

	t.Logf("TestScenario_ReviewLoop_ImplementerAgentReadyTimeout: result=%+v events=%v",
		result, collector.eventTypes())

	// ── Core assertions ─────────────────────────────────────────────────────

	// success=false: the implementer timed out before producing any work.
	if result.Success {
		t.Errorf("ImplementerAgentReadyTimeout FAIL: expected success=false; got true")
	}

	// completionReason=error: the timeout path produces rlErrorResult.
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("ImplementerAgentReadyTimeout FAIL: completion_reason=%q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}

	// summary should mention agent_ready_timeout.
	if !strings.Contains(result.Summary, "agent_ready_timeout") {
		t.Errorf("ImplementerAgentReadyTimeout FAIL: summary=%q; expected to contain 'agent_ready_timeout'",
			result.Summary)
	}

	// ── Event assertions ────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()

	// agent_ready_timeout event must be emitted (hk-5cox8 observability).
	agentReadyTimeoutFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReadyTimeout) {
			agentReadyTimeoutFound = true
			break
		}
	}
	if !agentReadyTimeoutFound {
		t.Errorf("ImplementerAgentReadyTimeout FAIL: agent_ready_timeout event not emitted; got: %v", eventTypes)
	}

	// agent_ready must NOT appear: the handler hangs without emitting it.
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReady) {
			t.Errorf("ImplementerAgentReadyTimeout FAIL: agent_ready event was emitted — handler did not hang as expected; events: %v", eventTypes)
			break
		}
	}

	// reviewer_launched must NOT appear: the implementer timed out before
	// producing a commit, so the reviewer phase should never be entered.
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			t.Errorf("ImplementerAgentReadyTimeout FAIL: reviewer_launched event was emitted — implementer should have timed out before reviewer phase; events: %v", eventTypes)
			break
		}
	}

	t.Logf("ImplementerAgentReadyTimeout PASS: implementer agent_ready_timeout fired, error result returned (hk-kunm4)")
}
