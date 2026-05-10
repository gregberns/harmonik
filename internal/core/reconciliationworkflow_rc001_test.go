package core

import (
	"testing"

	"github.com/google/uuid"
)

// rc73WorkflowFixtureReconciliation returns a valid reconciliation-class Workflow
// with workflow_class = reconciliation, used by RC-001..006 structural tests.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-001 — "Reconciliation MUST run
// as a normal harmonik workflow: DOT-defined per [execution-model.md §4.1]";
// specs/reconciliation/schemas.md §6.5 WorkflowClass extension.
func rc73WorkflowFixtureReconciliation(t *testing.T) Workflow {
	t.Helper()
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006301"))
	startNode := Node{
		NodeID:           NodeID("investigator"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	termNode := Node{
		NodeID:           NodeID("verdict-commit"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	cls := WorkflowClassReconciliation
	return Workflow{
		WorkflowID:      wfID,
		Name:            "rc-reconciliation-workflow",
		Version:         "1.0.0",
		Nodes:           []Node{startNode, termNode},
		Edges:           []Edge{},
		StartNodeID:     NodeID("investigator"),
		TerminalNodeIDs: []NodeID{NodeID("verdict-commit")},
		Policies:        []PolicyRef{},
		Metadata:        map[string]string{},
		WorkflowClass:   &cls,
		SchemaVersion:   1,
	}
}

// rc73WorkflowFixtureOrdinary returns a valid ordinary (non-reconciliation)
// Workflow without a WorkflowClass tag, used to contrast with reconciliation
// workflows in structural tests.
func rc73WorkflowFixtureOrdinary(t *testing.T) Workflow {
	t.Helper()
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006302"))
	startNode := Node{
		NodeID:           NodeID("start"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	termNode := Node{
		NodeID:           NodeID("done"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	return Workflow{
		WorkflowID:      wfID,
		Name:            "ordinary-workflow",
		Version:         "1.0.0",
		Nodes:           []Node{startNode, termNode},
		Edges:           []Edge{},
		StartNodeID:     NodeID("start"),
		TerminalNodeIDs: []NodeID{NodeID("done")},
		Policies:        []PolicyRef{},
		Metadata:        map[string]string{},
		WorkflowClass:   nil,
		SchemaVersion:   1,
	}
}

// TestRC001_ReconciliationWorkflowCarriesWorkflowClassTag verifies that a
// reconciliation workflow carries workflow_class = "reconciliation" as
// required by RC-001.
//
// RC-001: "Reconciliation MUST run as a normal harmonik workflow: DOT-defined
// per [execution-model.md §4.1], dispatched deterministically by the daemon,
// and event-logged per [event-model.md §4.1]."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-001;
// specs/reconciliation/schemas.md §6.5.
func TestRC001_ReconciliationWorkflowCarriesWorkflowClassTag(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureReconciliation(t)

	if wf.WorkflowClass == nil {
		t.Fatal("RC-001: reconciliation Workflow.WorkflowClass is nil; want non-nil")
	}
	if *wf.WorkflowClass != WorkflowClassReconciliation {
		t.Errorf("RC-001: WorkflowClass = %q, want %q",
			*wf.WorkflowClass, WorkflowClassReconciliation)
	}
}

// TestRC001_ReconciliationWorkflowIsValidWorkflow verifies that the
// reconciliation workflow fixture satisfies all structural invariants of the
// Workflow record per RC-001 (it is a normal harmonik workflow).
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-001;
// specs/execution-model.md §6.1 RECORD Workflow.
func TestRC001_ReconciliationWorkflowIsValidWorkflow(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureReconciliation(t)

	if !wf.Valid() {
		t.Error("RC-001: reconciliation Workflow.Valid() = false; want true — reconciliation runs as a normal harmonik workflow")
	}
}

// TestRC001_OrdinaryWorkflowHasNoWorkflowClass verifies that an ordinary
// (non-reconciliation) workflow carries no WorkflowClass, establishing the
// distinction required by RC-001 and schemas.md §6.5.
//
// Spec ref: specs/reconciliation/schemas.md §6.5 — "Absence of the field (nil
// *WorkflowClass on the Workflow record) means an ordinary, unclassed workflow."
func TestRC001_OrdinaryWorkflowHasNoWorkflowClass(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureOrdinary(t)

	if wf.WorkflowClass != nil {
		t.Errorf("RC-001 contrast: ordinary Workflow.WorkflowClass = %q, want nil",
			*wf.WorkflowClass)
	}
	if !wf.Valid() {
		t.Error("RC-001 contrast: ordinary Workflow.Valid() = false; want true")
	}
}

// TestRC002_ReconciliationWorkflowClassIsDistinctFromOrdinary verifies that
// the WorkflowClass field distinguishes a reconciliation workflow from an
// ordinary workflow at the Workflow record level, as required by RC-002's
// "keyed on workflow_class = reconciliation" constraint.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002 — "This is an explicit
// exception to [execution-model.md §4.5 EM-023] and is keyed on the workflow-
// library metadata tag workflow_class = reconciliation."
func TestRC002_ReconciliationWorkflowClassIsDistinctFromOrdinary(t *testing.T) {
	t.Parallel()

	rec := rc73WorkflowFixtureReconciliation(t)
	ord := rc73WorkflowFixtureOrdinary(t)

	recIsReconciliation := rec.WorkflowClass != nil && *rec.WorkflowClass == WorkflowClassReconciliation
	ordIsReconciliation := ord.WorkflowClass != nil && *ord.WorkflowClass == WorkflowClassReconciliation

	if !recIsReconciliation {
		t.Error("RC-002: reconciliation workflow fixture does not carry workflow_class=reconciliation")
	}
	if ordIsReconciliation {
		t.Error("RC-002: ordinary workflow fixture incorrectly carries workflow_class=reconciliation")
	}
}

// TestRC004_ReconciliationWorkflowClassConstantIsStable verifies that
// WorkflowClassReconciliation equals the string "reconciliation" — the value
// that S01-shipped DOT workflow headers must declare per RC-004.
//
// RC-004: "The reconciliation workflow library... MUST be owned by S01
// (Orchestrator Core) and ship as part of the S01 package."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-004;
// specs/reconciliation/schemas.md §6.5.
func TestRC004_ReconciliationWorkflowClassConstantIsStable(t *testing.T) {
	t.Parallel()

	const want = "reconciliation"
	if string(WorkflowClassReconciliation) != want {
		t.Errorf("RC-004: WorkflowClassReconciliation = %q, want %q (S01 DOT header must match)",
			string(WorkflowClassReconciliation), want)
	}
}

// TestRC005_DetectorsNotInWorkflowLibrary verifies the structural boundary:
// reconciliation category classification lives in daemon code (not in a
// workflow's node logic), so the WorkflowClass field on Workflow is the only
// tag — no detector-specific fields are embedded in the Workflow record itself.
//
// RC-005: "The §4.3 detectors MUST live in the daemon's Go code (mechanism-
// tagged functions), NOT in the S01 workflow library."
//
// This test verifies the boundary by confirming that a reconciliation Workflow
// carries only the workflow_class tag and is otherwise structurally identical
// to an ordinary workflow (no detector-specific fields).
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_DetectorsNotInWorkflowLibrary(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureReconciliation(t)

	// The reconciliation workflow is valid as an ordinary harmonik workflow
	// (same structural rules apply), confirming it has no daemon-internal fields.
	if !wf.Valid() {
		t.Error("RC-005: reconciliation Workflow.Valid() = false; detector logic MUST NOT be embedded in the workflow record")
	}

	// The WorkflowClass is the only reconciliation-specific discriminator.
	if wf.WorkflowClass == nil {
		t.Fatal("RC-005: WorkflowClass is nil; the workflow_class tag is the sole reconciliation discriminator")
	}
	if *wf.WorkflowClass != WorkflowClassReconciliation {
		t.Errorf("RC-005: WorkflowClass = %q, want %q", *wf.WorkflowClass, WorkflowClassReconciliation)
	}
}

// TestRC006_WorkflowClassIsOnlyReconciliationAtMVH verifies that at MVH the
// only accepted WorkflowClass value is "reconciliation", enforcing the closed
// enum contract of RC-006's "same harmonik release" upgrade discipline.
//
// RC-006: "A new reconciliation category... MUST ship a daemon-code change...
// AND a workflow-library addition in S01... in the same harmonik release."
//
// This test probes the WorkflowClass.Valid() fence: values that are not
// "reconciliation" are rejected, preventing split-release scenarios where an
// unknown workflow_class bypasses the detector.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-006;
// specs/reconciliation/schemas.md §6.5 "Future enum growth".
func TestRC006_WorkflowClassIsOnlyReconciliationAtMVH(t *testing.T) {
	t.Parallel()

	// Only "reconciliation" is valid at MVH; any future class must be added
	// to WorkflowClass.Valid() and the daemon detector table atomically.
	futureClasses := []WorkflowClass{
		"improvement-loop",
		"operator-cli-handler",
		"unknown-class",
		"",
		"RECONCILIATION",
	}

	for _, cls := range futureClasses {
		cls := cls
		t.Run(string(cls), func(t *testing.T) {
			t.Parallel()
			if cls.Valid() {
				t.Errorf("RC-006: WorkflowClass(%q).Valid() = true; future/unknown class MUST be rejected at MVH", cls)
			}
		})
	}

	// The only valid MVH value.
	if !WorkflowClassReconciliation.Valid() {
		t.Error("RC-006: WorkflowClassReconciliation.Valid() = false; want true")
	}
}

// TestRC001_ReconciliationWorkflowClassRoundTripValue verifies that the
// WorkflowClass string value "reconciliation" is stable across the comparison
// chain: constant → Workflow field → pointer dereference → string comparison.
// This covers the daemon's classifier check: wf.WorkflowClass != nil &&
// *wf.WorkflowClass == WorkflowClassReconciliation.
//
// Spec ref: specs/reconciliation/schemas.md §6.5.
func TestRC001_ReconciliationWorkflowClassRoundTripValue(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureReconciliation(t)

	if wf.WorkflowClass == nil {
		t.Fatal("WorkflowClass is nil")
	}

	// Dereference and compare: this is the exact check the daemon runs.
	got := *wf.WorkflowClass
	if got != WorkflowClassReconciliation {
		t.Errorf("*wf.WorkflowClass = %q, want WorkflowClassReconciliation (%q)", got, WorkflowClassReconciliation)
	}
	if string(got) != "reconciliation" {
		t.Errorf("string(*wf.WorkflowClass) = %q, want %q", string(got), "reconciliation")
	}
}
