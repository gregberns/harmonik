package daemon_test

// dot_cascade_tool_hkcucz6_test.go — end-to-end scenario test for tool-node
// exit-code→Outcome→cascade routing (hk-cucz6 / T6a).
//
// Behavior validated (per HC-063 exit-state map):
//
//   - exit 0               → Outcome=SUCCESS → cascade routes to "close" terminal.
//   - exit 3               → Outcome=FAIL+deterministic → cascade routes to
//     "close-needs-attention" terminal.
//   - sleep past timeout   → Outcome=FAIL+transient → cascade routes to
//     "close-needs-attention" terminal.
//   - ctx-cancel           → Outcome=FAIL+canceled → cascade short-circuits
//     (needsAttention=false, no terminal reached).
//
// Observable terminal: DotWorkflowResultExported (Success, TerminalNodeID,
// NeedsAttention) and the node_dispatch_requested / node_dispatch_decided events
// captured by stubEventCollector.
//
// Substrate: non-agentic tool nodes run in-process via /bin/sh; no claude or
// twin binary is dispatched.
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-039 (tool_command / shell handler)
//   - specs/execution-model.md §II.7 EM-057 item 7 (exit-code → outcome)
//   - specs/execution-model.md §II.8 EM-058 (non-agentic row + sub-note)
//   - specs/handler-contract.md §III.1 HC-063 (in-process handler contract)
//   - specs/examples/tool-node.dot (canonical fixture)
//
// Bead refs: hk-cucz6 (T6a attractor-parity scenario), hk-l8rpd (T1 unit tests).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
	wfdot "github.com/gregberns/harmonik/internal/workflow/dot"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// toolNodeDOT returns the DOT source for a minimal tool-node workflow:
//
//	start(noop) → run-tool(shell, cmd[, timeout]) → close(terminal) [SUCCESS]
//	                                               → close-needs-attention(terminal) [FAIL]
//
// cmd is written verbatim as the tool_command value; timeoutSec is the timeout
// attribute string (empty = omit, using the default 300 s). The unconditional
// fallback edge routes FAIL to close-needs-attention per the
// D-edge-cascade-invariant.
func toolNodeDOT(workflowID, cmd, timeoutSec string) string {
	timeoutAttr := ""
	if timeoutSec != "" {
		timeoutAttr = `, timeout="` + timeoutSec + `"`
	}
	return `digraph "` + workflowID + `" {
    schema_version="1"; version="1.0"; workflow_id="` + workflowID + `";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "run-tool" [type="non-agentic", handler_ref="shell", idempotency_class="non-idempotent", tool_command="` + cmd + `"` + timeoutAttr + `];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> "run-tool";
    "run-tool" -> close [condition="outcome.status == 'SUCCESS'"];
    "run-tool" -> "close-needs-attention" [condition="outcome.status == 'FAIL'"];
    "run-tool" -> "close-needs-attention";
}
`
}

// toolNodeFixtureGraph writes the DOT source to a temp file, parses it via
// workflow.LoadDotWorkflow, and returns the validated *wfdot.Graph.
func toolNodeFixtureGraph(t *testing.T, dotSrc string) *wfdot.Graph {
	t.Helper()
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "workflow.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(dotPath, []byte(dotSrc), 0o644); err != nil {
		t.Fatalf("toolNodeFixtureGraph: write DOT: %v", err)
	}
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("toolNodeFixtureGraph: LoadDotWorkflow: %v\nDOT:\n%s", err, dotSrc)
	}
	return graph
}

// toolNodeFixtureDeps builds WorkLoopDepsParams for non-agentic tool-node
// scenarios. HandlerBinary=/bin/sh is listed but never invoked (all nodes are
// non-agentic).
func toolNodeFixtureDeps(t *testing.T, bus *stubEventCollector) daemon.WorkLoopDepsParams {
	t.Helper()
	projectDir := t.TempDir()
	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		//nolint:gosec // G301: test-only temp dir
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			t.Fatalf("toolNodeFixtureDeps: mkdir %s: %v", sub, err)
		}
	}
	return daemon.WorkLoopDepsParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
}

// toolNodeFixtureRunID generates a fresh RunID.
func toolNodeFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("toolNodeFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// toolNodeCountEvents counts the number of events of the given type in events.
func toolNodeCountEvents(events []string, eventType string) int {
	n := 0
	for _, et := range events {
		if et == eventType {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// T6a-1: exit 0 → SUCCESS → "close" terminal
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ToolNode_Exit0_RoutesToCloseTerminal verifies that a shell tool
// node with tool_command="exit 0" produces Outcome=SUCCESS and the cascade
// routes to the "close" success terminal.
//
// Walk: start(noop) → run-tool(exit 0) → close(terminal, SUCCESS).
//
// Assertions:
//   - result.Success = true
//   - result.TerminalNodeID = "close"
//   - result.NeedsAttention = false
//   - node_dispatch_requested fired for start, run-tool, close (≥3 events).
//   - node_dispatch_decided fired for each cascade decision (≥3 events).
func TestScenario_ToolNode_Exit0_RoutesToCloseTerminal(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(toolNodeFixtureDeps(t, bus))
	graph := toolNodeFixtureGraph(t, toolNodeDOT("t6a-exit0", "exit 0", "30"))

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		toolNodeFixtureRunID(t),
		core.BeadID("hk-cucz6-t6a-exit0"),
		t.TempDir(), "",
		graph,
	)

	events := bus.eventTypes()
	t.Logf("T6a-exit0: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("T6a-exit0: success=false, want true (summary=%q)", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("T6a-exit0: terminalNodeID=%q, want %q", result.TerminalNodeID, "close")
	}
	if result.NeedsAttention {
		t.Errorf("T6a-exit0: needsAttention=true, want false")
	}

	// 3 nodes visited: start → run-tool → close.
	ndReq := toolNodeCountEvents(events, string(core.EventTypeNodeDispatchRequested))
	if ndReq < 3 {
		t.Errorf("T6a-exit0: node_dispatch_requested count=%d, want ≥3; events=%v", ndReq, events)
	}
	ndDec := toolNodeCountEvents(events, string(core.EventTypeNodeDispatchDecided))
	if ndDec < 3 {
		t.Errorf("T6a-exit0: node_dispatch_decided count=%d, want ≥3; events=%v", ndDec, events)
	}

	t.Logf("T6a-exit0 PASS: exit 0 → SUCCESS → close terminal (hk-cucz6)")
}

// ─────────────────────────────────────────────────────────────────────────────
// T6a-2: exit 3 → FAIL+deterministic → "close-needs-attention" terminal
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ToolNode_Exit3_DeterministicRoutesToFailTerminal verifies that a
// shell tool node with tool_command="exit 3" produces Outcome=FAIL+deterministic
// and the cascade routes to the "close-needs-attention" failure terminal.
//
// Walk: start(noop) → run-tool(exit 3) → close-needs-attention(terminal, FAIL).
//
// Assertions:
//   - result.Success = false
//   - result.TerminalNodeID = "close-needs-attention"
//   - result.NeedsAttention = true
func TestScenario_ToolNode_Exit3_DeterministicRoutesToFailTerminal(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(toolNodeFixtureDeps(t, bus))
	graph := toolNodeFixtureGraph(t, toolNodeDOT("t6a-exit3", "exit 3", "30"))

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		toolNodeFixtureRunID(t),
		core.BeadID("hk-cucz6-t6a-exit3"),
		t.TempDir(), "",
		graph,
	)

	events := bus.eventTypes()
	t.Logf("T6a-exit3: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("T6a-exit3: success=true, want false (summary=%q)", result.Summary)
	}
	if result.TerminalNodeID != "close-needs-attention" {
		t.Errorf("T6a-exit3: terminalNodeID=%q, want %q",
			result.TerminalNodeID, "close-needs-attention")
	}
	if !result.NeedsAttention {
		t.Errorf("T6a-exit3: needsAttention=false, want true")
	}

	ndReq := toolNodeCountEvents(events, string(core.EventTypeNodeDispatchRequested))
	if ndReq < 3 {
		t.Errorf("T6a-exit3: node_dispatch_requested count=%d, want ≥3; events=%v", ndReq, events)
	}

	t.Logf("T6a-exit3 PASS: exit 3 → FAIL+deterministic → close-needs-attention (hk-cucz6)")
}

// ─────────────────────────────────────────────────────────────────────────────
// T6a-3: timeout → FAIL+transient → "close-needs-attention" terminal
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ToolNode_TimeoutKill_TransientRoutesToFailTerminal verifies that a
// shell tool node that runs past its timeout attribute produces Outcome=FAIL+transient
// and the cascade routes to "close-needs-attention".
//
// timeout=1 caps execution at 1 s; "sleep 60" is killed by the internal
// deadline (execCtx.DeadlineExceeded). The outer context is not cancelled, so
// the cascade sees FAIL+transient and routes normally to the fail terminal.
//
// Assertions:
//   - result.Success = false
//   - result.TerminalNodeID = "close-needs-attention"
//   - result.NeedsAttention = true
//   - Completes within ~5 s of the 1 s timeout.
func TestScenario_ToolNode_TimeoutKill_TransientRoutesToFailTerminal(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(toolNodeFixtureDeps(t, bus))
	// timeout=1: internal execCtx deadline fires after 1 s; "sleep 60" is killed.
	graph := toolNodeFixtureGraph(t, toolNodeDOT("t6a-timeout", "sleep 60", "1"))

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()
	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		toolNodeFixtureRunID(t),
		core.BeadID("hk-cucz6-t6a-timeout"),
		t.TempDir(), "",
		graph,
	)
	elapsed := time.Since(start)

	events := bus.eventTypes()
	t.Logf("T6a-timeout: result=%+v events=%v elapsed=%v", result, events, elapsed)

	if result.Success {
		t.Errorf("T6a-timeout: success=true, want false (summary=%q)", result.Summary)
	}
	if result.TerminalNodeID != "close-needs-attention" {
		t.Errorf("T6a-timeout: terminalNodeID=%q, want %q",
			result.TerminalNodeID, "close-needs-attention")
	}
	if !result.NeedsAttention {
		t.Errorf("T6a-timeout: needsAttention=false, want true")
	}

	// Should complete well within the outer 15 s budget.
	if elapsed > 8*time.Second {
		t.Errorf("T6a-timeout: elapsed %v > 8s; 1 s timeout should fire promptly", elapsed)
	}

	t.Logf("T6a-timeout PASS: timeout kill → FAIL+transient → close-needs-attention in %v (hk-cucz6)", elapsed)
}

// ─────────────────────────────────────────────────────────────────────────────
// T6a-4: ctx-cancel → FAIL+canceled → no terminal reached
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ToolNode_CtxCancel_CanceledNeedsAttentionFalse verifies that
// cancelling the outer context while the tool node is executing produces
// Outcome=FAIL+canceled, and driveDotWorkflow short-circuits the cascade
// (no terminal is reached) with needsAttention=false.
//
// After dispatchDotToolNode returns with FAIL+canceled, driveDotWorkflow detects
// ctx.Err() != nil and returns immediately — the cascade is NOT consulted —
// so TerminalNodeID is empty and Summary contains "context cancelled".
//
// Assertions:
//   - result.Success = false
//   - result.NeedsAttention = false (operator cancel, not structural failure)
//   - result.TerminalNodeID = ""
//   - result.Summary contains "context cancelled"
func TestScenario_ToolNode_CtxCancel_CanceledNeedsAttentionFalse(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(toolNodeFixtureDeps(t, bus))
	// Large internal timeout: the outer ctx-cancel must fire first.
	graph := toolNodeFixtureGraph(t, toolNodeDOT("t6a-ctxcancel", "sleep 60", "300"))

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps,
			toolNodeFixtureRunID(t),
			core.BeadID("hk-cucz6-t6a-ctxcancel"),
			t.TempDir(), "",
			graph,
		)
	}()

	// Wait for "sleep 60" to start executing, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("T6a-ctxcancel: ExportedDriveDotWorkflow did not return within 10 s after cancel")
	}

	events := bus.eventTypes()
	t.Logf("T6a-ctxcancel: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("T6a-ctxcancel: success=true, want false")
	}
	if result.NeedsAttention {
		t.Errorf("T6a-ctxcancel: needsAttention=true, want false (operator cancel, not structural failure)")
	}
	if result.TerminalNodeID != "" {
		t.Errorf("T6a-ctxcancel: terminalNodeID=%q, want \"\" (cascade short-circuited before terminal)",
			result.TerminalNodeID)
	}
	if !strings.Contains(result.Summary, "context cancelled") {
		t.Errorf("T6a-ctxcancel: summary=%q; want to contain \"context cancelled\"", result.Summary)
	}

	// node_dispatch_requested must appear for at least "start" (before cancel).
	ndReq := toolNodeCountEvents(events, string(core.EventTypeNodeDispatchRequested))
	if ndReq < 1 {
		t.Errorf("T6a-ctxcancel: node_dispatch_requested count=%d, want ≥1; events=%v", ndReq, events)
	}

	// node_dispatch_decided must appear for the start→run-tool cascade decision.
	ndDec := toolNodeCountEvents(events, string(core.EventTypeNodeDispatchDecided))
	if ndDec < 1 {
		t.Errorf("T6a-ctxcancel: node_dispatch_decided count=%d, want ≥1 (start→run-tool cascade); events=%v",
			ndDec, events)
	}

	t.Logf("T6a-ctxcancel PASS: ctx-cancel → FAIL+canceled → no terminal, needsAttention=false (hk-cucz6)")
}
