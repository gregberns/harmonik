//go:build scenario

package daemon_test

// scenario_subworkflow_dispatch_hkx9l_test.go — scenario coverage for
// sub-workflow dispatch: in-place expansion (no new RunID) + terminal outcome
// escape (SW-001 / SW-006).
//
// # What this guards
//
// Two orthogonal claims about the dotSubWorkflowRunner wired into driveDotWorkflow:
//
//  1. SW-001 / SW-INV-001 — In-place expansion, single run identity.
//     When the DOT cascade encounters a sub-workflow node, the expanded child
//     nodes execute WITHIN the parent run — no new RunID is allocated. Both
//     sub_workflow_entered and sub_workflow_exited MUST carry the parent run_id.
//
//  2. SW-006 / SW-INV-002 — Terminal outcome escape.
//     The Outcome produced by the last expanded child node propagates verbatim
//     to the parent cascade. The parent's edge-selection uses that Outcome to
//     pick the next node, so a SUCCESS in the child routes the parent to the
//     SUCCESS-conditioned edge, and a FAIL routes to the FAIL-conditioned edge
//     (or the unconditional fallback).
//
// # Topology
//
// Test 1 (InPlaceNoRunID):
//
//	Parent graph:
//	  start (noop) → sw (sub-workflow ref=child.dot) → close (terminal, noop)
//	Child graph:
//	  inner-start (noop) → inner-close (terminal, noop)
//
//	Assertions:
//	  - cascade result: success=true, terminalNodeID="close"
//	  - sub_workflow_entered + sub_workflow_exited emitted
//	  - both lifecycle events carry the parent run_id (SW-INV-001)
//	  - no second run_id appears in any event
//
// Test 2 (TerminalOutcomeEscapes — success path):
//
//	Parent graph:
//	  start (noop) → sw (sub-workflow ref=success-child.dot)
//	             → close [condition=outcome.status=='SUCCESS'] (terminal, noop)
//	             → close-needs-attention [unconditional fallback] (terminal, noop)
//	Child graph (success-child.dot):
//	  inner-only (non-agentic/shell exit 0, terminal)
//
//	Assertions:
//	  - cascade result: success=true, terminalNodeID="close" (not close-needs-attention)
//
// Test 3 (TerminalOutcomeEscapes — fail path):
//
//	Same parent graph, different sub-workflow ref (fail-child.dot):
//	Child graph (fail-child.dot):
//	  inner-only (non-agentic/shell exit 1, terminal)
//
//	Assertions:
//	  - cascade result: success=false, terminalNodeID="close-needs-attention"
//
// # Driver
//
// All three tests call ExportedDriveDotWorkflow — the same function the
// daemon runs in workflow-mode dot — with a real shell handler env (no
// agentic nodes, so no twin binary is required). Sub-workflow child graphs
// contain only non-agentic nodes (noop or shell) so the cascade runs entirely
// in-process without spawning Claude.
//
// # Spec refs
//   - specs/sub-workflow-dispatch.md SW-001, SW-006, SW-INV-001, SW-INV-002
//   - specs/execution-model.md §4.8 EM-034, EM-036, EM-036a
//   - specs/event-model.md §8.1.9–10 (sub_workflow_entered / sub_workflow_exited)
//
// Bead: hk-x9l.
// Run: go test -tags=scenario -run TestScenario_SubWorkflowDispatch ./internal/daemon/...

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ── DOT fixture sources ───────────────────────────────────────────────────────

// swDispatchParentDOT returns the DOT source for the parent graph used in
// TestScenario_SubWorkflowDispatch_InPlaceNoRunID. The sub-workflow node
// references childRef (e.g. "child.dot").
//
//	start (noop) → sw (sub-workflow ref=<childRef>) → close (terminal, noop)
func swDispatchParentDOT(childRef string) string {
	return fmt.Sprintf(`digraph "sw-dispatch-parent" {
    schema_version="1"; version="1.0"; workflow_id="sw-dispatch-parent";
    start_node="start"; terminal_node_ids="close";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    sw    [type="sub-workflow", sub_workflow_ref=%q, workflow_version="1.0"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> sw;
    sw -> close;
}
`, childRef)
}

// swDispatchChildNoop returns the DOT source for a trivial child workflow
// containing only a single non-agentic terminal node.
//
//	inner-only (noop, terminal)
const swDispatchChildNoop = `digraph "sw-dispatch-child" {
    schema_version="1"; version="1.0"; workflow_id="sw-dispatch-child";
    start_node="inner-only"; terminal_node_ids="inner-only";

    "inner-only" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
}
`

// swDispatchOutcomeParentDOT returns the DOT source for the parent graph used
// in TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes. The cascade has
// two branches: SUCCESS routes to "close"; anything else (including FAIL)
// routes to "close-needs-attention" via the unconditional fallback.
func swDispatchOutcomeParentDOT(childRef string) string {
	return fmt.Sprintf(`digraph "sw-outcome-parent" {
    schema_version="1"; version="1.0"; workflow_id="sw-outcome-parent";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    sw    [type="sub-workflow", sub_workflow_ref=%q, workflow_version="1.0"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> sw;
    sw -> close [condition="outcome.status == 'SUCCESS'"];
    sw -> "close-needs-attention";
}
`, childRef)
}

// swDispatchChildShellExit returns the DOT source for a child workflow with a
// single non-agentic shell node that exits with the given code. The shell
// exit code drives the parent cascade via outcome escape (SW-006).
//
//	inner-only (shell exit <exitCode>, terminal)
func swDispatchChildShellExit(exitCode int) string {
	return fmt.Sprintf(`digraph "sw-dispatch-child-shell" {
    schema_version="1"; version="1.0"; workflow_id="sw-dispatch-child-shell";
    start_node="inner-only"; terminal_node_ids="inner-only";

    "inner-only" [type="non-agentic", handler_ref="shell", idempotency_class="idempotent",
                  tool_command="exit %d", timeout="10"];
}
`, exitCode)
}

// ── fixture helpers ───────────────────────────────────────────────────────────

// swDispatchWriteDOT writes content to projectDir/<name> and returns the path.
func swDispatchWriteDOT(t *testing.T, projectDir, name, content string) string {
	t.Helper()
	path := filepath.Join(projectDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("swDispatchWriteDOT: WriteFile %s: %v", name, err)
	}
	return path
}

// swDispatchRunID returns a stable RunID for sub-workflow scenario tests.
func swDispatchRunID(t *testing.T) core.RunID {
	t.Helper()
	return rlFixtureRunID(t)
}

// swDispatchDeps builds a minimal workLoopDeps wired to the given event
// collector, project directory (where child DOT files live), and /bin/sh
// for the shell handler.
func swDispatchDeps(t *testing.T, collector *stubEventCollector, projectDir string) daemon.WorkLoopDepsParams {
	t.Helper()
	ledger := &stubBeadLedger{}
	return daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	}
}

// ── Test 1: SW-001 / SW-INV-001 — In-place expansion, single run identity ──

// TestScenario_SubWorkflowDispatch_InPlaceNoRunID verifies that when the DOT
// cascade processes a sub-workflow node, the expanded child nodes execute
// within the parent run (SW-001): both sub_workflow_entered and
// sub_workflow_exited carry the parent run_id, and no extra run_id appears in
// any event (SW-INV-001). The cascade completes with success=true.
func TestScenario_SubWorkflowDispatch_InPlaceNoRunID(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	// Write child DOT file to the project dir (tier-1 resolution via explicit ref).
	swDispatchWriteDOT(t, projectDir, "child.dot", swDispatchChildNoop)

	// Write parent DOT file and load it.
	parentDOTPath := swDispatchWriteDOT(t, projectDir, "parent.dot", swDispatchParentDOT("child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(parent.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(swDispatchDeps(t, collector, projectDir))

	runID := swDispatchRunID(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(ctx, deps, runID, core.BeadID("sw-test-inplace"), wtPath, parentSHA, graph)
	}()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("SW-001: cascade did not complete within deadline; events=%v", collector.eventTypes())
	}

	t.Logf("SW-001: result=%+v events=%v", result, collector.eventTypes())

	// ── Assertion 1: cascade completes successfully ───────────────────────────
	if !result.Success {
		t.Errorf("SW-001: expected success=true; summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("SW-001: expected terminalNodeID=%q, got %q", "close", result.TerminalNodeID)
	}

	// ── Assertion 2: sub_workflow_entered and sub_workflow_exited emitted ─────
	var foundEntered, foundExited bool
	for _, ev := range collector.eventTypes() {
		switch ev {
		case string(core.EventTypeSubWorkflowEntered):
			foundEntered = true
		case string(core.EventTypeSubWorkflowExited):
			foundExited = true
		}
	}
	if !foundEntered {
		t.Errorf("SW-005: sub_workflow_entered was not emitted (events=%v)", collector.eventTypes())
	}
	if !foundExited {
		t.Errorf("SW-005: sub_workflow_exited was not emitted (events=%v)", collector.eventTypes())
	}

	// ── Assertion 3: lifecycle events carry the parent run_id (SW-INV-001) ────
	// Both entered and exited payloads embed "run_id"; verify they match the
	// parent run_id and no other run_id is present.
	expectedRunID := runID.String()
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeSubWorkflowEntered) &&
			ev.EventType != string(core.EventTypeSubWorkflowExited) {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(ev.Payload, &m); err != nil {
			t.Fatalf("SW-INV-001: unmarshal %s payload: %v", ev.EventType, err)
		}
		gotRunID, _ := m["run_id"].(string)
		if gotRunID != expectedRunID {
			t.Errorf("SW-INV-001: event %q run_id=%q, want parent run_id=%q (no child RunID allocated)",
				ev.EventType, gotRunID, expectedRunID)
		}
	}
}

// ── Test 2: SW-006 / SW-INV-002 — Terminal outcome escape (success path) ────

// TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Success verifies that
// when the child sub-workflow's terminal node produces a SUCCESS Outcome, the
// parent cascade routes to the SUCCESS-conditioned edge (to "close"), not the
// unconditional fallback (to "close-needs-attention"). This proves verbatim
// outcome propagation per SW-006 / SW-INV-002.
func TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Success(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	// Child workflow: single shell node that exits 0 → SUCCESS Outcome.
	swDispatchWriteDOT(t, projectDir, "success-child.dot", swDispatchChildShellExit(0))

	parentDOTPath := swDispatchWriteDOT(t, projectDir, "outcome-parent.dot", swDispatchOutcomeParentDOT("success-child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(outcome-parent.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(swDispatchDeps(t, collector, projectDir))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps, swDispatchRunID(t),
			core.BeadID("sw-outcome-success"), wtPath, parentSHA, graph,
		)
	}()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("SW-006/success: cascade did not complete within deadline; events=%v", collector.eventTypes())
	}

	t.Logf("SW-006/success: result=%+v events=%v", result, collector.eventTypes())

	// ── SUCCESS path: parent routes to "close" ────────────────────────────────
	if !result.Success {
		t.Errorf("SW-006/success: expected success=true (child exit 0 → SUCCESS Outcome → parent 'close' edge); summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("SW-006/success: terminalNodeID=%q, want %q (SUCCESS Outcome must route to 'close')", result.TerminalNodeID, "close")
	}

	// ── sub_workflow_exited terminal_outcome_status must be SUCCESS (SW-005) ──
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeSubWorkflowExited) {
			continue
		}
		var payload core.SubWorkflowExitedPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("SW-005: unmarshal sub_workflow_exited: %v", err)
		}
		if payload.TerminalOutcomeStatus != core.OutcomeStatusSuccess {
			t.Errorf("SW-005: sub_workflow_exited.terminal_outcome_status=%q, want %q",
				payload.TerminalOutcomeStatus, core.OutcomeStatusSuccess)
		}
	}
}

// ── Test 3: SW-006 / SW-INV-002 — Terminal outcome escape (fail path) ───────

// TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Fail verifies that
// when the child sub-workflow's terminal node produces a FAIL Outcome, the
// parent cascade routes to the unconditional fallback edge (to
// "close-needs-attention") rather than the SUCCESS-conditioned edge (to "close").
// This proves the FAIL Outcome propagates verbatim per SW-006 / SW-INV-002.
func TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Fail(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	// Child workflow: single shell node that exits 1 → FAIL Outcome (deterministic).
	swDispatchWriteDOT(t, projectDir, "fail-child.dot", swDispatchChildShellExit(1))

	parentDOTPath := swDispatchWriteDOT(t, projectDir, "outcome-parent-fail.dot", swDispatchOutcomeParentDOT("fail-child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(outcome-parent-fail.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(swDispatchDeps(t, collector, projectDir))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps, swDispatchRunID(t),
			core.BeadID("sw-outcome-fail"), wtPath, parentSHA, graph,
		)
	}()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("SW-006/fail: cascade did not complete within deadline; events=%v", collector.eventTypes())
	}

	t.Logf("SW-006/fail: result=%+v events=%v", result, collector.eventTypes())

	// ── FAIL path: parent routes to "close-needs-attention" ──────────────────
	// The conditional edge (outcome.status=='SUCCESS') must NOT be taken.
	// The unconditional fallback (to close-needs-attention) must fire.
	// driveDotWorkflow maps terminal node "close-needs-attention" to
	// needsAttention=true / success=false.
	if result.TerminalNodeID != "close-needs-attention" {
		t.Errorf("SW-006/fail: terminalNodeID=%q, want %q (FAIL Outcome must escape to unconditional fallback)",
			result.TerminalNodeID, "close-needs-attention")
	}
	if result.Success {
		t.Errorf("SW-006/fail: expected success=false (routed to close-needs-attention); summary=%q", result.Summary)
	}

	// ── sub_workflow_exited terminal_outcome_status must be FAIL (SW-005) ─────
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeSubWorkflowExited) {
			continue
		}
		var payload core.SubWorkflowExitedPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("SW-005: unmarshal sub_workflow_exited: %v", err)
		}
		if payload.TerminalOutcomeStatus != core.OutcomeStatusFail {
			t.Errorf("SW-005: sub_workflow_exited.terminal_outcome_status=%q, want %q",
				payload.TerminalOutcomeStatus, core.OutcomeStatusFail)
		}
	}
}
