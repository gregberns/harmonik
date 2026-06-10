package daemon_test

// reviewloop_agent_ready_timeout_hknfhqd_test.go — scenario test: reviewer
// agent_ready timeout re-queues bead (no leak).
//
// # What this tests
//
// When the reviewer phase exceeds agentReadyTimeout without emitting agent_ready,
// runReviewLoop must:
//  1. Kill the reviewer process.
//  2. Reap within agentReadyKillReapTimeout.
//  3. Return an error result (success=false, completionReason=error, needsAttention=true).
//
// The work loop (runWorkLoop / beadRunOne) converts a needsAttention=true result
// into a ReopenBead call, returning the bead to "open" state rather than leaving
// it stranded as "in_progress". This test exercises the runReviewLoop contract
// (result shape) directly via ExportedRunReviewLoop rather than driving the full
// work loop, which is the same approach used by nilwatcher_scenario_hk3aqtb_test.go
// and reviewloop_hkgql2015_test.go.
//
// # Placement note
//
// The bead body (hk-nfhqd) targets test/scenario/; however ExportedRunReviewLoop
// and WorkLoopDepsParams.AdapterRegistry2 are test-only exports (export_test.go,
// package daemon) and are only compiled into the daemon test binary. An external
// package (test/scenario/) cannot import them. Per implementer-protocol
// §Path-discrepancy resolution ("bead body wins"), this file is placed in
// internal/daemon/ where the seam is available; the deviation is documented
// in the commit body.
//
// # Helper prefix
//
// reviewHangFixture (bead hk-nfhqd, per implementer-protocol §Helper-prefix
// discipline).
//
// # Spec refs
//
//   - specs/execution-model.md §4.3 (re-queue contract)
//   - specs/handler-contract.md §4.8, §4.9 HC-056
//
// Bead: hk-nfhqd.

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

// reviewHangFixtureProjectDir creates the minimal project directory tree for
// the reviewer-hang test: .harmonik/events/ and .harmonik/beads-intents/, then
// initialises a git repo with one initial commit.
func reviewHangFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("reviewHangFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("reviewHangFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("reviewHangFixtureProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("reviewer-hang scenario test repo\n"), 0o644); err != nil {
		t.Fatalf("reviewHangFixtureProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// reviewHangFixtureWorktree creates a detached git worktree under projectDir
// and returns the worktree path and parent commit SHA.
func reviewHangFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("reviewHangFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("reviewHangFixtureWorktree: git worktree add: %v\n%s", addErr, addOut)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("reviewHangFixtureWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// reviewHangFixtureRunID generates a fresh RunID using UUIDv7.
func reviewHangFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("reviewHangFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// reviewHangFixtureHandlerScript writes a shell handler to a temp dir.
//
// Odd invocations (1, 3, …) are the implementer phase: commits a sentinel file
// and exits 0. Even invocations (2, 4, …) are the reviewer phase: hangs
// indefinitely (sleeps for 1 hour) without ever emitting agent_ready.
//
// The 1-hour sleep is long enough to outlast the 100ms agentReadyTimeout used
// in the test; the reviewer process is killed by runReviewLoop when the timeout
// fires per HC-056.
func reviewHangFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := `#!/bin/sh
set -e
WTP='` + wtpEsc + `'
CNT_FILE="$WTP/.harmonik/rh_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 1 ]; then
  # Odd = implementer: commit a sentinel file and exit 0.
  printf '%d' "$CNT" > "$WTP/rh_impl_$CNT.txt"
  git -C "$WTP" add "rh_impl_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit -m "reviewer-hang impl $CNT" --no-gpg-sign >/dev/null 2>&1
  exit 0
else
  # Even = reviewer: hang indefinitely without emitting agent_ready.
  sleep 3600
fi
`
	scriptPath := filepath.Join(t.TempDir(), "rh_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("reviewHangFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// reviewHangFixtureAdapterRegistry builds an AdapterRegistry with
// ClaudeCodeAdapter registered. The registry is required for waitAgentReady
// to be invoked in runReviewLoop (skipped when adapterRegistry is nil).
func reviewHangFixtureAdapterRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("reviewHangFixtureAdapterRegistry: Register ClaudeCodeAdapter: %v", err)
	}
	return reg
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_AgentReadyTimeoutRequeues verifies the reviewer
// agent_ready timeout path (HC-056):
//
//   - Implementer completes normally (commits sentinel file, exits 0).
//   - Reviewer is launched but hangs indefinitely (never emits agent_ready).
//   - agentReadyTimeout of 100ms fires; runReviewLoop kills and reaps the
//     reviewer, then returns an error result.
//   - The error result (success=false, completionReason="error",
//     needsAttention=true) drives the work loop to call ReopenBead, returning
//     the bead to "open" state — no leak as "in_progress".
//
// Catches hk-yjduq broader form: the kill-and-reap path after reviewer
// ready-timeout is load-bearing for bead liveness but was not exercised
// prior to this bead; silent regression would strand beads in in_progress.
//
// Spec refs: specs/execution-model.md §4.3 (re-queue contract),
// specs/handler-contract.md §4.8, §4.9 HC-056.
// Source: docs/scenario-test-gap-audit-2026-05-18.md #5.
// Bead: hk-nfhqd.
func TestScenario_ReviewLoop_AgentReadyTimeoutRequeues(t *testing.T) {
	// NOT parallel (hk-1o0cc de-flake): isolates the process-global
	// ~/.claude.json trust config so EnsureWorktreeTrust does not contend on the
	// real config's lock under load. See rlIsolateClaudeConfig.
	rlIsolateClaudeConfig(t)

	projectDir := reviewHangFixtureProjectDir(t)
	wtPath, parentSHA := reviewHangFixtureWorktree(t, projectDir)
	scriptPath := reviewHangFixtureHandlerScript(t, wtPath)

	// Build an adapter registry with ClaudeCodeAdapter so that waitAgentReady
	// fires in the reviewer phase. Without an adapter, the guard is skipped and
	// the hang would block indefinitely (no agent_ready timeout path exercised).
	adapterReg := reviewHangFixtureAdapterRegistry(t)

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
		// AdapterRegistry2 must be non-nil for waitAgentReady to be invoked in
		// the reviewer phase. This is the adapterRegistry field on workLoopDeps
		// (distinct from AdapterRegistry, which is the handler.NewHandler registry).
		AdapterRegistry2: adapterReg,
		// hk-1o0cc de-flake: timeout raised 100ms → 5s. The IMPLEMENTER phase (odd
		// invocation) must commit a sentinel + exit 0 and have its watcher fire
		// implReadyCancel BEFORE this timeout, so the run advances to the reviewer
		// phase where the hang IS the thing under test. A real git commit + process
		// exit + watcher reap routinely exceeds 100ms under load, so the original
		// 100ms intermittently tripped the IMPLEMENTER's waitAgentReady (failing at
		// iteration 1, reviewer_launched never emitted) — the hk-1o0cc -short red.
		// 5s comfortably clears the implementer commit yet still fires well inside
		// the 30s outer deadline for the 3600s reviewer hang.
		AgentReadyTimeout: 5 * time.Second,
		// Explicit hookSessionStore so SetAgentReadyCallback wires correctly;
		// the reviewer script hangs and never emits agent_ready, so the
		// AgentReadyTimeout path fires (hk-d8u1y, hk-ngw3d).
		HookStore: daemon.ExportedNewHookSessionStore(),
	})

	// The test runs with a generous outer deadline. The 100ms agentReadyTimeout
	// fires internally after the reviewer is launched; the outer context provides
	// a safety net against infinite blocking in case the kill/reap path itself
	// stalls.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		reviewHangFixtureRunID(t),
		core.BeadID("rh-agent-ready-timeout-001"),
		wtPath, parentSHA,
	)

	t.Logf("TestScenario_ReviewLoop_AgentReadyTimeoutRequeues: result=%+v events=%v",
		result, collector.eventTypes())

	// ── Core assertions: the result must signal re-queue (no leak) ──────────
	//
	// success=false: the cycle did not complete successfully.
	if result.Success {
		t.Errorf("AgentReadyTimeoutRequeues FAIL: expected success=false after reviewer timeout; got success=true")
	}

	// completionReason=error: the timeout path produces rlErrorResult, which
	// uses ReviewLoopCompletionReasonError.
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("AgentReadyTimeoutRequeues FAIL: completion_reason=%q; want %q (HC-056 timeout path)",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}

	// needsAttention=true: rlErrorResult sets needsAttention=true so the work
	// loop calls ReopenBead (not CloseBead) — the bead returns to "open".
	if !result.NeedsAttention {
		t.Errorf("AgentReadyTimeoutRequeues FAIL: needs_attention=false; want true so the work loop re-queues bead to 'open'")
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()

	// reviewer_launched must be emitted before the timeout fires (we got far
	// enough to attempt the reviewer phase).
	reviewerLaunchedFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			reviewerLaunchedFound = true
			break
		}
	}
	if !reviewerLaunchedFound {
		t.Errorf("AgentReadyTimeoutRequeues FAIL: reviewer_launched event not emitted; got: %v", eventTypes)
	}

	// review_loop_cycle_complete must be emitted (lifecycle invariant: every
	// terminal path emits this event before returning).
	cycleCompleteFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			cycleCompleteFound = true
			break
		}
	}
	if !cycleCompleteFound {
		t.Errorf("AgentReadyTimeoutRequeues FAIL: review_loop_cycle_complete not emitted; got: %v", eventTypes)
	}

	// agent_ready must NOT appear: the reviewer script hangs without emitting
	// any agent_ready event. If it appears, the timeout path is not exercised.
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReady) {
			t.Errorf("AgentReadyTimeoutRequeues FAIL: agent_ready event was emitted — reviewer did not hang as expected; events: %v", eventTypes)
			break
		}
	}

	t.Logf("AgentReadyTimeoutRequeues PASS: reviewer agent_ready_timeout fired, bead result marks re-queue (hk-nfhqd)")
}
