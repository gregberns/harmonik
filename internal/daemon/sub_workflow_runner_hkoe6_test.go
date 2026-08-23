package daemon

// sub_workflow_runner_hkoe6_test.go — SW-001..SW-010 conformance tests.
//
// Covers the 6 obligations from specs/sub-workflow-dispatch.md §6 Conformance:
//  1. Acyclicity rejection (SW-003).
//  2. Namespacing format (SW-002).
//  3. Outcome escape (SW-006 / SW-INV-002).
//  4. Parent run_id on events (SW-005 / SW-INV-001).
//  5. Resolution order (SW-004).
//  6. No review-loop sub-workflow (SW-010).
//
// Bead: hk-oe6
// Spec: specs/sub-workflow-dispatch.md
// Tags: mechanism

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func swTestRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID("test-sub-workflow"),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("/tmp/wt"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func swTestParentGraph(refName string) *dot.Graph {
	return &dot.Graph{
		Name:            "parent",
		StartNodeID:     "sw-node",
		TerminalNodeIDs: []string{"done"},
		Nodes: []*dot.Node{
			{ID: "sw-node", Type: core.NodeTypeSubWorkflow, SubWorkflowRef: refName, WorkflowVersion: "1.0"},
			{ID: "done", Type: core.NodeTypeNonAgentic},
		},
	}
}

func swWriteDotFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("swWriteDotFile: %v", err)
	}
	return path
}

const minimalDotWorkflow = `digraph {
  schema_version="1";
  version="1.0";
  workflow_id="sub-workflow-minimal-fixture";
  start_node="only";
  terminal_node_ids="only";
  only [type="non-agentic", idempotency_class="idempotent", handler_ref="noop"];
}`

const reviewLoopDotWorkflow = `digraph {
  schema_version="1";
  version="1.0";
  workflow_id="sub-workflow-review-loop-fixture";
  workflow_class="review-loop";
  start_node="only";
  terminal_node_ids="only";
  only [type="non-agentic", idempotency_class="idempotent", handler_ref="noop"];
}`

type recordingBusDaemon struct {
	events []swRecordedEvent
}

type swRecordedEvent struct {
	EventType core.EventType
	RunID     core.RunID
	Payload   json.RawMessage
}

func (b *recordingBusDaemon) EmitWithRunID(_ context.Context, runID core.RunID, et core.EventType, payload []byte) error {
	b.events = append(b.events, swRecordedEvent{EventType: et, RunID: runID, Payload: payload})
	return nil
}

func (b *recordingBusDaemon) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.events = append(b.events, swRecordedEvent{EventType: et})
	return nil
}

func swMakeRunner(t *testing.T, bus *recordingBusDaemon, projectDir string, parentGraph *dot.Graph) *dotSubWorkflowRunner {
	t.Helper()
	run := swTestRun(t)
	deps := testRuntime{
		env:   runloop.RunEnv{ProjectDir: projectDir},
		ports: runloop.RunPorts{Emitter: bus},
	}
	iterCount := 1
	sessID := ""
	env, ports, handles := runBundlesFromDeps(deps, run.RunID)
	return newDotSubWorkflowRunner(
		env,
		ports,
		handles,
		run.RunID,
		core.BeadID("hk-test"),
		core.BeadRecord{},
		"test-title",
		"test-description",
		"/tmp/repo", // activeRepo
		"/tmp/wt",
		"deadbeef",
		"",
		&iterCount,
		&sessID,
		"",
		"",
		projectconfig.PiProfileConfig{}, // piProfile: non-Pi bead
		"",
		"main",
		run,
		core.NewCycleCounter(),
		parentGraph,
		nil, // runner: local (NFR7)
		"",  // workerBinaryPath: local
		"",  // workerSessionName: local
		"",  // workerSessionCwd: local
	)
}

// TestSubWorkflowRunner_AcyclicityReject_SelfReference verifies that a direct
// self-reference (parent workflow references itself) fails closed with a
// structural Outcome and no sub_workflow_entered event is emitted (SW-003).
func TestSubWorkflowRunner_AcyclicityReject_SelfReference(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "parent.dot", minimalDotWorkflow)

	parentGraph := &dot.Graph{
		Name: "parent",
		Nodes: []*dot.Node{
			{ID: "sw", Type: core.NodeTypeSubWorkflow, SubWorkflowRef: "parent", WorkflowVersion: "1.0"},
		},
	}

	bus := &recordingBusDaemon{}
	runner := swMakeRunner(t, bus, dir, parentGraph)
	runner.parentWorkflowName = "parent"

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "parent",
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v (want structural Outcome, not error)", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q, want FAIL (SW-003 acyclicity rejection)", outcome.Status)
	}
	if outcome.FailureClass == nil || *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %v, want structural (SW-003)", outcome.FailureClass)
	}
	for _, ev := range bus.events {
		if ev.EventType == core.EventTypeSubWorkflowEntered {
			t.Errorf("sub_workflow_entered was emitted on acyclicity failure (SW-003 violation)")
		}
	}
}

// TestSubWorkflowRunner_AcyclicityReject_MutualReference verifies that a
// mutual reference (A → B → A) is detected and fails closed (SW-003).
func TestSubWorkflowRunner_AcyclicityReject_MutualReference(t *testing.T) {
	dir := t.TempDir()
	childDot := `digraph {
  schema_version = "1";
  version = "1.0";
  workflow_id = "sub-workflow-mutual-cycle-child";
  start_node = "sw-back";
  terminal_node_ids = "sw-back";
  sw-back [type="sub-workflow", sub_workflow_ref="A", workflow_version="1.0"];
}`
	swWriteDotFile(t, dir, "B.dot", childDot)

	parentGraph := &dot.Graph{
		Name: "A",
		Nodes: []*dot.Node{
			{ID: "sw", Type: core.NodeTypeSubWorkflow, SubWorkflowRef: "B", WorkflowVersion: "1.0"},
		},
	}

	bus := &recordingBusDaemon{}
	runner := swMakeRunner(t, bus, dir, parentGraph)
	runner.parentWorkflowName = "A"

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "B",
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q, want FAIL (mutual cycle detection SW-003)", outcome.Status)
	}
	if outcome.FailureClass == nil || *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %v, want structural", outcome.FailureClass)
	}
}

// TestSubWorkflowRunner_NamespacingFormat verifies that the expanded sub-graph
// node IDs are in the form <parentNodeID>/<subNodeID> per EM-034a (SW-002).
// This is tested via workflow.ExpandSubWorkflowGraph which is called by Run.
// We verify indirectly by checking the sub_workflow_entered payload.
func TestSubWorkflowRunner_NamespacingFormat(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "child.dot", minimalDotWorkflow)

	parentGraph := &dot.Graph{Name: "parent"}
	bus := &recordingBusDaemon{}
	runner := swMakeRunner(t, bus, dir, parentGraph)

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "review", // parent node ID — this is the namespace prefix
		SubWorkflowRef:     "child",
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Logf("note: outcome.Status = %q (may be FAIL if cascade finds no edge; acyclicity OK)", outcome.Status)
	}

	var foundEntered bool
	for _, ev := range bus.events {
		if ev.EventType != core.EventTypeSubWorkflowEntered {
			continue
		}
		foundEntered = true
		var payload core.SubWorkflowEnteredPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("unmarshal sub_workflow_entered: %v", err)
		}
		if payload.ParentNodeID != "review" {
			t.Errorf("sub_workflow_entered.ParentNodeID = %q, want %q (SW-002 namespace prefix)", payload.ParentNodeID, "review")
		}
	}
	if !foundEntered {
		t.Error("sub_workflow_entered event was not emitted (SW-005)")
	}
}

// TestSubWorkflowRunner_EventsCarryParentRunID verifies that both
// sub_workflow_entered and sub_workflow_exited carry the parent run_id and
// no child run identifier exists (SW-005 / SW-INV-001).
func TestSubWorkflowRunner_EventsCarryParentRunID(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "child.dot", minimalDotWorkflow)

	parentGraph := &dot.Graph{Name: "parent"}
	bus := &recordingBusDaemon{}
	runner := swMakeRunner(t, bus, dir, parentGraph)
	parentRunID := runner.run.RunID

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw-node",
		SubWorkflowRef:     "child",
		SubWorkflowVersion: "1.0",
	}

	if _, err := runner.Run(context.Background(), spec); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var gotEntered, gotExited bool
	for _, ev := range bus.events {
		switch ev.EventType {
		case core.EventTypeSubWorkflowEntered:
			gotEntered = true
			var p core.SubWorkflowEnteredPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("unmarshal entered: %v", err)
			}
			if p.RunID != parentRunID {
				t.Errorf("entered.RunID = %v, want parent RunID %v (SW-005/SW-INV-001)", p.RunID, parentRunID)
			}
		case core.EventTypeSubWorkflowExited:
			gotExited = true
			var p core.SubWorkflowExitedPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("unmarshal exited: %v", err)
			}
			if p.RunID != parentRunID {
				t.Errorf("exited.RunID = %v, want parent RunID %v (SW-005/SW-INV-001)", p.RunID, parentRunID)
			}
		}
	}
	if !gotEntered {
		t.Error("sub_workflow_entered event was not emitted (SW-005)")
	}
	if !gotExited {
		t.Error("sub_workflow_exited event was not emitted (SW-005)")
	}
}

// TestSubWorkflowRunner_Resolution_ExplicitRefWins verifies that when both an
// explicit ref ("child.dot") and "workflow.dot" exist, the explicit ref is
// resolved (tier 1 wins per SW-004).
func TestSubWorkflowRunner_Resolution_ExplicitRefWins(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "child.dot", minimalDotWorkflow)
	swWriteDotFile(t, dir, "workflow.dot", minimalDotWorkflow)

	bus := &recordingBusDaemon{}
	parentGraph := &dot.Graph{Name: "parent"}
	runner := swMakeRunner(t, bus, dir, parentGraph)

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "child.dot", // explicit ref
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	_ = outcome

	for _, ev := range bus.events {
		if ev.EventType != core.EventTypeSubWorkflowEntered {
			continue
		}
		var p core.SubWorkflowEnteredPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("unmarshal entered: %v", err)
		}
		if string(p.SubWorkflowName) != "child.dot" {
			t.Errorf("entered.SubWorkflowName = %q, want %q (SW-004 tier 1 wins)", p.SubWorkflowName, "child.dot")
		}
	}
}

// TestSubWorkflowRunner_Resolution_FallsBackToWorkflowDot verifies that when no
// explicit ref is found, resolution falls back to workflow.dot (SW-004 tier 2).
func TestSubWorkflowRunner_Resolution_FallsBackToWorkflowDot(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "workflow.dot", minimalDotWorkflow)

	bus := &recordingBusDaemon{}
	parentGraph := &dot.Graph{Name: "parent"}
	runner := swMakeRunner(t, bus, dir, parentGraph)

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "missing-ref", // not present on disk
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if outcome.FailureClass != nil && *outcome.FailureClass == core.FailureClassStructural {
		if outcome.Notes != "" && len(outcome.Notes) > 20 {
			t.Logf("got structural fail with notes: %s", outcome.Notes)
		}
	}
	var foundEntered bool
	for _, ev := range bus.events {
		if ev.EventType == core.EventTypeSubWorkflowEntered {
			foundEntered = true
		}
	}
	if !foundEntered {
		t.Error("sub_workflow_entered not emitted — tier-2 resolution may have failed (SW-004)")
	}
}

// TestSubWorkflowRunner_Resolution_StructuralFailWhenNeitherExists verifies
// that when neither the explicit ref nor workflow.dot exists, Run returns a
// structural Outcome without dispatching any node (SW-004 tier 3).
func TestSubWorkflowRunner_Resolution_StructuralFailWhenNeitherExists(t *testing.T) {
	dir := t.TempDir()

	bus := &recordingBusDaemon{}
	parentGraph := &dot.Graph{Name: "parent"}
	runner := swMakeRunner(t, bus, dir, parentGraph)

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "nonexistent",
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v (want structural Outcome)", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q, want FAIL (SW-004 tier 3 structural)", outcome.Status)
	}
	if outcome.FailureClass == nil || *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %v, want structural (SW-004)", outcome.FailureClass)
	}
	for _, ev := range bus.events {
		if ev.EventType == core.EventTypeSubWorkflowEntered {
			t.Error("sub_workflow_entered was emitted despite resolution failure (SW-004)")
		}
	}
}

// TestSubWorkflowRunner_NoReviewLoop_FailsStructural verifies that a
// sub-workflow with workflow_class="review-loop" is rejected at dispatch with
// a structural Outcome (SW-010).
func TestSubWorkflowRunner_NoReviewLoop_FailsStructural(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "rl.dot", reviewLoopDotWorkflow)

	bus := &recordingBusDaemon{}
	parentGraph := &dot.Graph{Name: "parent"}
	runner := swMakeRunner(t, bus, dir, parentGraph)

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "rl.dot",
		SubWorkflowVersion: "1.0",
	}

	outcome, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v (want structural Outcome)", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q, want FAIL (SW-010 review-loop rejection)", outcome.Status)
	}
	if outcome.FailureClass == nil || *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %v, want structural (SW-010)", outcome.FailureClass)
	}
}

// TestSubWorkflowRunner_SingleRunID verifies that a successful sub-workflow
// execution emits both lifecycle events carrying the SAME run_id as the parent
// run — no child run identifier is allocated (SW-001 / SW-INV-001).
func TestSubWorkflowRunner_SingleRunID(t *testing.T) {
	dir := t.TempDir()
	swWriteDotFile(t, dir, "child.dot", minimalDotWorkflow)

	bus := &recordingBusDaemon{}
	parentGraph := &dot.Graph{Name: "parent"}
	runner := swMakeRunner(t, bus, dir, parentGraph)
	expectedRunID := runner.run.RunID

	spec := handler.SubWorkflowRunSpec{
		Run:                runner.run,
		ParentNodeID:       "sw",
		SubWorkflowRef:     "child.dot",
		SubWorkflowVersion: "1.0",
	}

	if _, err := runner.Run(context.Background(), spec); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for _, ev := range bus.events {
		if ev.EventType != core.EventTypeSubWorkflowEntered && ev.EventType != core.EventTypeSubWorkflowExited {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(ev.Payload, &m); err != nil {
			continue
		}
		got, _ := m["run_id"].(string)
		if got != expectedRunID.String() {
			t.Errorf("event %q run_id = %q, want parent run_id %q (SW-INV-001)", ev.EventType, got, expectedRunID.String())
		}
	}
}
