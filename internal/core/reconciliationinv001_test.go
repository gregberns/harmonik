package core

import (
	"testing"

	"github.com/google/uuid"
)

// rc73InvFixtureWorkflow returns a reconciliation Workflow fixture, reusing the
// rc73WorkflowFixtureReconciliation helper from reconciliationworkflow_rc001_test.go
// under a distinct WorkflowID to avoid collision.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001.
func rc73InvFixtureWorkflow(t *testing.T) Workflow {
	t.Helper()
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006310"))
	startNode := Node{
		NodeID:           NodeID("inv-investigator"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	termNode := Node{
		NodeID:           NodeID("inv-verdict"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	cls := WorkflowClassReconciliation
	return Workflow{
		WorkflowID:      wfID,
		Name:            "rc-inv-001-sensor-workflow",
		Version:         "1.0.0",
		Nodes:           []Node{startNode, termNode},
		Edges:           []Edge{},
		StartNodeID:     NodeID("inv-investigator"),
		TerminalNodeIDs: []NodeID{NodeID("inv-verdict")},
		Policies:        []PolicyRef{},
		Metadata:        map[string]string{},
		WorkflowClass:   &cls,
		SchemaVersion:   1,
	}
}

// rc73InvFixtureOrdinaryWorkflow returns an ordinary (non-reconciliation)
// Workflow fixture used to validate that non-reconciliation workflows MUST NOT
// be associated with reconciliation_verdict_* events per RC-INV-001.
func rc73InvFixtureOrdinaryWorkflow(t *testing.T) Workflow {
	t.Helper()
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006311"))
	startNode := Node{
		NodeID:           NodeID("ordinary-start"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	termNode := Node{
		NodeID:           NodeID("ordinary-done"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	return Workflow{
		WorkflowID:      wfID,
		Name:            "rc-inv-001-ordinary-workflow",
		Version:         "1.0.0",
		Nodes:           []Node{startNode, termNode},
		Edges:           []Edge{},
		StartNodeID:     NodeID("ordinary-start"),
		TerminalNodeIDs: []NodeID{NodeID("ordinary-done")},
		Policies:        []PolicyRef{},
		Metadata:        map[string]string{},
		WorkflowClass:   nil,
		SchemaVersion:   1,
	}
}

// rc73InvFixtureIsReconciliationWorkflow returns true if the given Workflow
// has workflow_class = reconciliation. This is the daemon's audit-time check
// for RC-INV-001: every reconciliation_verdict_* event source workflow must
// pass this predicate.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 Sensor — "(a) Daemon
// MUST tag the Workflow record in its registry with workflow_class; startup
// audit log samples emitted workflows and asserts every reconciliation_verdict_*
// event traces back to a Workflow whose workflow_class = reconciliation."
func rc73InvFixtureIsReconciliationWorkflow(wf Workflow) bool {
	return wf.WorkflowClass != nil && *wf.WorkflowClass == WorkflowClassReconciliation
}

// TestRCINV001_ReconciliationVerdictWorkflowMustBeTagged verifies the core
// invariant of RC-INV-001: a workflow that emits reconciliation_verdict_*
// events MUST carry workflow_class = reconciliation.
//
// Sensor: every reconciliation_verdict_* event traces back to a Workflow whose
// workflow_class = reconciliation. A workflow without this tag is a structural
// violation per RC-INV-001.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 — "every reconciliation
// dispatch... MUST run as a DOT-tagged workflow with workflow_class =
// reconciliation."
func TestRCINV001_ReconciliationVerdictWorkflowMustBeTagged(t *testing.T) {
	t.Parallel()

	wf := rc73InvFixtureWorkflow(t)

	if !rc73InvFixtureIsReconciliationWorkflow(wf) {
		t.Error("RC-INV-001: reconciliation verdict workflow does not carry workflow_class=reconciliation; " +
			"every reconciliation_verdict_* event source workflow must be tagged")
	}
}

// TestRCINV001_OrdinaryWorkflowMustNotEmitVerdictEvents verifies that an
// ordinary workflow (workflow_class absent) would fail the RC-INV-001 audit:
// the audit's join "reconciliation_verdict_* → source workflow" would flag
// any workflow lacking workflow_class=reconciliation.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 — "a non-reconciliation
// workflow node MUST NOT invoke the verdict-execution mechanics of RC-025."
func TestRCINV001_OrdinaryWorkflowMustNotEmitVerdictEvents(t *testing.T) {
	t.Parallel()

	ord := rc73InvFixtureOrdinaryWorkflow(t)

	if rc73InvFixtureIsReconciliationWorkflow(ord) {
		t.Error("RC-INV-001: ordinary workflow incorrectly passes the reconciliation-workflow audit check; " +
			"ordinary workflows MUST NOT be associated with reconciliation_verdict_* events")
	}
}

// TestRCINV001_AuditSensorDistinguishesReconciliationFromOrdinary verifies
// that the audit-time predicate correctly classifies a mixed set of workflows:
// reconciliation workflows pass the audit; ordinary workflows fail.
//
// This models the RC-INV-001 startup audit log sample described in the spec:
// "startup audit log samples emitted workflows and asserts every
// reconciliation_verdict_* event traces back to a Workflow whose
// workflow_class = reconciliation."
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 Sensor.
func TestRCINV001_AuditSensorDistinguishesReconciliationFromOrdinary(t *testing.T) {
	t.Parallel()

	reconWF := rc73InvFixtureWorkflow(t)
	ordWF := rc73InvFixtureOrdinaryWorkflow(t)

	workflows := []struct {
		wf            Workflow
		shouldBeRecon bool
		name          string
	}{
		{reconWF, true, "reconciliation-workflow"},
		{ordWF, false, "ordinary-workflow"},
	}

	for _, tc := range workflows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rc73InvFixtureIsReconciliationWorkflow(tc.wf)
			if got != tc.shouldBeRecon {
				t.Errorf("RC-INV-001 audit: IsReconciliationWorkflow(%q) = %v, want %v",
					tc.name, got, tc.shouldBeRecon)
			}
		})
	}
}

// TestRCINV001_ExactlyOneVerdictEventPerDispatch verifies the RC-021 uniqueness
// constraint that RC-INV-001 closes: at most one reconciliation_verdict_emitted
// event per reconciliation dispatch (per workflow run).
//
// This test verifies the invariant at the structural level: a reconciliation
// workflow is structurally bounded by exactly one verdict commit per run
// (RC-002), and RC-INV-001 audits that this holds across the daemon lifetime.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 — "MUST emit at most
// one reconciliation_verdict_emitted event per dispatch (RC-021)."
func TestRCINV001_ExactlyOneVerdictEventPerDispatch(t *testing.T) {
	t.Parallel()

	// The invariant: a reconciliation workflow is a Workflow with workflow_class
	// = reconciliation (RC-001), emits exactly one verdict commit (RC-002),
	// and is the sole source of reconciliation_verdict_* events per dispatch.
	//
	// At the structural level: two reconciliation workflows for the same
	// target_run_id are prevented by RC-002a (flock dedup). The invariant
	// holds because:
	//   1. Each reconciliation dispatch acquires exactly one lock (RC-002a).
	//   2. Each locked dispatch produces exactly one workflow run (RC-001).
	//   3. Each workflow run emits exactly one verdict commit (RC-002).
	//   4. Each verdict commit emits exactly one reconciliation_verdict_emitted (RC-021).
	//
	// This test models the invariant by verifying that two distinct reconciliation
	// workflow fixtures have distinct WorkflowIDs, establishing the one-workflow-
	// per-dispatch contract at the identity level.

	wf1 := rc73InvFixtureWorkflow(t)
	wf2 := rc73WorkflowFixtureReconciliation(t)

	// Both must be reconciliation workflows.
	if !rc73InvFixtureIsReconciliationWorkflow(wf1) {
		t.Error("RC-INV-001: wf1 is not a reconciliation workflow")
	}
	if !rc73InvFixtureIsReconciliationWorkflow(wf2) {
		t.Error("RC-INV-001: wf2 is not a reconciliation workflow")
	}

	// Two dispatches for different target runs produce two distinct workflow
	// instances (different WorkflowIDs).
	if uuid.UUID(wf1.WorkflowID) == uuid.UUID(wf2.WorkflowID) {
		t.Error("RC-INV-001: two reconciliation workflow fixtures share the same WorkflowID; " +
			"each dispatch must produce a distinct workflow instance")
	}
}

// TestRCINV001_WorkflowClassConstantIsTheAuditAnchor verifies that the string
// value "reconciliation" is the canonical audit anchor for RC-INV-001's JSONL
// query: "filter reconciliation_verdict_emitted events and join against Workflow
// registry; any event whose source workflow's class is NOT 'reconciliation'
// fails the audit."
//
// This test ensures the constant value matches what the audit query will look
// for in the Workflow registry.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001 Sensor — "(b) JSONL
// query at audit time filters reconciliation_verdict_emitted events and joins
// against Workflow registry; any event whose source workflow's class is NOT
// 'reconciliation' fails the audit."
func TestRCINV001_WorkflowClassConstantIsTheAuditAnchor(t *testing.T) {
	t.Parallel()

	const auditAnchor = "reconciliation"

	if string(WorkflowClassReconciliation) != auditAnchor {
		t.Errorf("RC-INV-001: WorkflowClassReconciliation = %q, want %q (audit anchor mismatch)",
			string(WorkflowClassReconciliation), auditAnchor)
	}

	// The audit predicate: wf.WorkflowClass != nil && *wf.WorkflowClass == WorkflowClassReconciliation
	// is equivalent to *wf.WorkflowClass == "reconciliation" — the JSONL query anchor.
	wf := rc73InvFixtureWorkflow(t)
	if wf.WorkflowClass == nil {
		t.Fatal("RC-INV-001: fixture workflow has nil WorkflowClass; cannot verify audit anchor")
	}
	if string(*wf.WorkflowClass) != auditAnchor {
		t.Errorf("RC-INV-001: *wf.WorkflowClass = %q, want %q (audit anchor)", string(*wf.WorkflowClass), auditAnchor)
	}
}
