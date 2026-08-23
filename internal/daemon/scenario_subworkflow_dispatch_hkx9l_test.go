//go:build scenario

package daemon_test

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

const swDispatchChildNoop = `digraph "sw-dispatch-child" {
    schema_version="1"; version="1.0"; workflow_id="sw-dispatch-child";
    start_node="inner-only"; terminal_node_ids="inner-only";

    "inner-only" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
}
`

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

func swDispatchChildShellExit(exitCode int) string {
	return fmt.Sprintf(`digraph "sw-dispatch-child-shell" {
    schema_version="1"; version="1.0"; workflow_id="sw-dispatch-child-shell";
    start_node="inner-only"; terminal_node_ids="inner-only";

    "inner-only" [type="non-agentic", handler_ref="shell", idempotency_class="idempotent",
                  tool_command="exit %d", timeout="10"];
}
`, exitCode)
}

func swDispatchWriteDOT(t *testing.T, projectDir, name, content string) string {
	t.Helper()
	path := filepath.Join(projectDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("swDispatchWriteDOT: WriteFile %s: %v", name, err)
	}
	return path
}

func swDispatchRunID(t *testing.T) core.RunID {
	t.Helper()
	return rlFixtureRunID(t)
}

func swDispatchDeps(t *testing.T, collector *stubEventCollector, projectDir string) daemon.TestRuntimeParams {
	t.Helper()
	ledger := &stubBeadLedger{}
	return daemon.TestRuntimeParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	}
}

// TestScenario_SubWorkflowDispatch_InPlaceNoRunID verifies that when the DOT
// cascade processes a sub-workflow node, the expanded child nodes execute
// within the parent run (SW-001): both sub_workflow_entered and
// sub_workflow_exited carry the parent run_id, and no extra run_id appears in
// any event (SW-INV-001). The cascade completes with success=true.
func TestScenario_SubWorkflowDispatch_InPlaceNoRunID(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	swDispatchWriteDOT(t, projectDir, "child.dot", swDispatchChildNoop)

	parentDOTPath := swDispatchWriteDOT(t, projectDir, "parent.dot", swDispatchParentDOT("child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(parent.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(swDispatchDeps(t, collector, projectDir))

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

	if !result.Success {
		t.Errorf("SW-001: expected success=true; summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("SW-001: expected terminalNodeID=%q, got %q", "close", result.TerminalNodeID)
	}

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

// TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Success verifies that
// when the child sub-workflow's terminal node produces a SUCCESS Outcome, the
// parent cascade routes to the SUCCESS-conditioned edge (to "close"), not the
// unconditional fallback (to "close-needs-attention"). This proves verbatim
// outcome propagation per SW-006 / SW-INV-002.
func TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Success(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	swDispatchWriteDOT(t, projectDir, "success-child.dot", swDispatchChildShellExit(0))

	parentDOTPath := swDispatchWriteDOT(t, projectDir, "outcome-parent.dot", swDispatchOutcomeParentDOT("success-child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(outcome-parent.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(swDispatchDeps(t, collector, projectDir))

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

	if !result.Success {
		t.Errorf("SW-006/success: expected success=true (child exit 0 → SUCCESS Outcome → parent 'close' edge); summary=%q", result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("SW-006/success: terminalNodeID=%q, want %q (SUCCESS Outcome must route to 'close')", result.TerminalNodeID, "close")
	}

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

// TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Fail verifies that
// when the child sub-workflow's terminal node produces a FAIL Outcome, the
// parent cascade routes to the unconditional fallback edge (to
// "close-needs-attention") rather than the SUCCESS-conditioned edge (to "close").
// This proves the FAIL Outcome propagates verbatim per SW-006 / SW-INV-002.
func TestScenario_SubWorkflowDispatch_TerminalOutcomeEscapes_Fail(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)

	swDispatchWriteDOT(t, projectDir, "fail-child.dot", swDispatchChildShellExit(1))

	parentDOTPath := swDispatchWriteDOT(t, projectDir, "outcome-parent-fail.dot", swDispatchOutcomeParentDOT("fail-child.dot"))
	graph, loadErr := workflow.LoadDotWorkflow(parentDOTPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(outcome-parent-fail.dot): %v", loadErr)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(swDispatchDeps(t, collector, projectDir))

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

	if result.TerminalNodeID != "close-needs-attention" {
		t.Errorf("SW-006/fail: terminalNodeID=%q, want %q (FAIL Outcome must escape to unconditional fallback)",
			result.TerminalNodeID, "close-needs-attention")
	}
	if result.Success {
		t.Errorf("SW-006/fail: expected success=false (routed to close-needs-attention); summary=%q", result.Summary)
	}

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
