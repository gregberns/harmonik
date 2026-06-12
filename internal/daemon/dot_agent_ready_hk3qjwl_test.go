package daemon_test

// dot_agent_ready_hk3qjwl_test.go — regression test: the DOT cascade agentic-node
// dispatch must gate paste-inject on agent_ready (hk-3qjwl).
//
// # The bug
//
// `harmonik run --workflow-mode dot` walked the graph and dispatched the
// implementer agentic node into the tmux substrate, but — unlike the single-mode
// (workloop.go) and review-loop (reviewloop.go) dispatch paths — it never waited
// for agent_ready before delivering the kick-off paste. The paste landed before
// the pane's REPL was input-ready; Claude Code's welcome splash consumed the
// trailing Enter; the prompt sat typed-but-unsubmitted; the agent idled and no
// commit was ever produced (run_stale ~10.5 min later). This blocked ALL DOT-mode
// live execution.
//
// # What this test proves
//
// driveDotWorkflow → dispatchDotAgenticNode now calls waitAgentReady BEFORE
// pasteInjectOnLaunch, exactly like the other two dispatch paths. The proof is
// structural, mirroring the review-loop analog (hk-kunm4,
// reviewloop_impl_agent_ready_hkkunm4_test.go):
//
//   - The handler hangs indefinitely without emitting agent_ready.
//   - agentReadyTimeout = 100ms fires promptly.
//   - With the gate wired: the implementer node returns an agent_ready_timeout
//     error, driveDotWorkflow returns needsAttention=true, and an
//     agent_ready_timeout event is emitted. The result arrives within the timeout
//     window (100ms + kill/reap), well inside the test's 8s context.
//   - WITHOUT the gate (the bug): there is no timeout — dispatchDotAgenticNode
//     would block in sess.Wait until the outer 8s context cancelled, so the
//     agent_ready_timeout event would NOT appear and the summary would not mention
//     it. This is precisely the divergence the fix repairs.
//
// Bead: hk-3qjwl.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// TestScenario_DotMode_ImplementerAgentReadyTimeout verifies that the DOT
// cascade agentic-node dispatch gates paste-inject on agent_ready (hk-3qjwl).
//
// The graph is the canonical review-loop topology: start (non-agentic) →
// implementer (agentic) → reviewer → close/close-needs-attention. The walk
// synthesises SUCCESS at `start`, advances to `implementer`, and dispatches it.
// The implementer handler hangs without signalling readiness, so the
// waitAgentReady gate must fire agent_ready_timeout.
func TestScenario_DotMode_ImplementerAgentReadyTimeout(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	// Reuse the review-loop implementer-ready fixtures (same package): a git
	// project, a detached worktree, a hanging /bin/sh handler, and an
	// AdapterRegistry with the ClaudeCodeAdapter registered (required for
	// waitAgentReady to obtain an adapter and DetectReady).
	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	scriptPath := implReadyFixtureHandlerScript(t)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	// Load the canonical review-loop graph through the real loader/validator so
	// the walk exercises the production parse → validate → cascade path.
	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		// 100ms timeout: the handler hangs for 3600s, so this fires first — but
		// ONLY if the dispatch path actually waits on agent_ready (the fix).
		AgentReadyTimeout: 100 * time.Millisecond,
		HookStore:         daemon.ExportedNewHookSessionStore(),
	})

	// 8s outer bound: the agent_ready timeout fires at 100ms; the remaining time
	// is the kill + reap of the hung /bin/sh handler. This is generous headroom
	// over the timeout path while keeping the test from running long. WITHOUT the
	// fix the dispatch hangs in sess.Wait until this context cancels (the failure
	// signature in the pre-fix run), so the bound also caps the negative case.
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-agent-ready-timeout-001"),
		wtPath, parentSHA,
		graph,
	)

	t.Logf("TestScenario_DotMode_ImplementerAgentReadyTimeout: result=%+v events=%v",
		result, collector.eventTypes())

	// ── Core assertions ─────────────────────────────────────────────────────

	// The agentic-node dispatch failed via the agent_ready_timeout path, so the
	// walk did not reach the success terminal.
	if result.Success {
		t.Errorf("DotMode agent_ready_timeout FAIL: expected success=false; got true")
	}
	if !result.NeedsAttention {
		t.Errorf("DotMode agent_ready_timeout FAIL: expected needsAttention=true; got false (summary=%q)", result.Summary)
	}

	// The summary must mention agent_ready_timeout — proving the timeout path
	// (not some other failure) fired in the agentic-node dispatch.
	if !strings.Contains(result.Summary, "agent_ready_timeout") {
		t.Errorf("DotMode agent_ready_timeout FAIL: summary=%q; expected to contain 'agent_ready_timeout'",
			result.Summary)
	}

	// ── Event assertions ────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()

	// agent_ready_timeout event must be emitted (matches the single-mode and
	// review-loop observability behaviour, hk-5cox8).
	agentReadyTimeoutFound := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReadyTimeout) {
			agentReadyTimeoutFound = true
			break
		}
	}
	if !agentReadyTimeoutFound {
		t.Errorf("DotMode agent_ready_timeout FAIL: agent_ready_timeout event not emitted; got: %v", eventTypes)
	}

	// agent_ready must NOT appear: the handler hangs without emitting it.
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReady) {
			t.Errorf("DotMode agent_ready_timeout FAIL: agent_ready event was emitted — handler did not hang as expected; events: %v", eventTypes)
			break
		}
	}

	t.Logf("DotMode agent_ready_timeout PASS: DOT agentic-node dispatch gates paste-inject on agent_ready (hk-3qjwl)")
}
