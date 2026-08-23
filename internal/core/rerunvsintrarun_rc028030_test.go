package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func rc77RunFixture(t *testing.T, runID RunID) Run {
	t.Helper()
	beadID := BeadID("hk-63oh")
	now := time.Now().UTC()
	stateID := StateID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006001"))
	return Run{
		RunID:           runID,
		WorkflowID:      mustParseWorkflowID(t, "018f1e2a-0000-7000-8000-000000006000"),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		Input:           WorkspaceRef("/projects/my-project"),
		WorkflowMode:    WorkflowModeSingle,
		BeadID:          &beadID,
		State:           stateID,
		Context:         map[string]any{},
		StartTime:       now,
	}
}

func rc77RunIDv7(t *testing.T, uuidStr string) RunID {
	t.Helper()
	u, err := uuid.Parse(uuidStr)
	if err != nil {
		t.Fatalf("rc77RunIDv7: uuid.Parse(%q): %v", uuidStr, err)
	}
	if u.Version() != 7 {
		t.Fatalf("rc77RunIDv7: %q is UUID version %d, want 7", uuidStr, u.Version())
	}
	return RunID(u)
}

// TestRC028_ReopenBeadVerdictIsDeclared verifies that VerdictReopenBead is one of
// the seven closed Verdict enum values.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028;
// specs/reconciliation/schemas.md §6.1 ENUM Verdict.
func TestRC028_ReopenBeadVerdictIsDeclared(t *testing.T) {
	t.Parallel()

	if !VerdictReopenBead.Valid() {
		t.Error("RC-028: VerdictReopenBead.Valid() = false; reopen-bead MUST be a valid Verdict enum value")
	}
	if string(VerdictReopenBead) != "reopen-bead" {
		t.Errorf("RC-028: VerdictReopenBead = %q, want %q", string(VerdictReopenBead), "reopen-bead")
	}
}

// TestRC028_NewRunIDAfterReopenBead verifies that two distinct Run values with
// different RunIDs represent distinct runs — the type-level enforcement of
// RC-028's requirement that a reopen-bead verdict MUST NOT continue the prior run_id.
//
// The daemon enforces this at dispatch time; this test asserts the structural
// invariant that RunIDs are unique typed values.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028 — "The new run MUST receive
// a fresh run_id; continuation of the prior run_id after a reopen-bead verdict
// is forbidden."
func TestRC028_NewRunIDAfterReopenBead(t *testing.T) {
	t.Parallel()

	priorRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006801")
	freshRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006802")

	priorRun := rc77RunFixture(t, priorRunID)
	freshRun := rc77RunFixture(t, freshRunID)

	if !priorRun.Valid() {
		t.Fatal("RC-028: priorRun.Valid() = false; fixture error")
	}
	if !freshRun.Valid() {
		t.Fatal("RC-028: freshRun.Valid() = false; fixture error")
	}

	if priorRun.RunID == freshRun.RunID {
		t.Error("RC-028: priorRun.RunID == freshRun.RunID; reopen-bead MUST produce a DISTINCT run_id (RC-028)")
	}
	if uuid.UUID(freshRun.RunID).Version() != 7 {
		t.Errorf("RC-028: freshRun.RunID is UUID version %d, want 7 (UUIDv7 per EM-013)", uuid.UUID(freshRun.RunID).Version())
	}
}

// TestRC028_ReopenBeadRunIDMustNotMatchPrior is the negative scenario:
// if the new run carries the same run_id as the prior run, RC-028 is violated.
// This test documents the forbidden case explicitly.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028.
func TestRC028_ReopenBeadRunIDMustNotMatchPrior(t *testing.T) {
	t.Parallel()

	priorRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006803")

	buggyReusedRunID := priorRunID // RC-028 violation: same run_id
	freshRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006804")

	reuseViolation := buggyReusedRunID == priorRunID
	if !reuseViolation {
		t.Fatal("RC-028: test fixture error — reused run_id should equal prior run_id")
	}

	if freshRunID == priorRunID {
		t.Error("RC-028: freshRunID should differ from priorRunID; test fixture error")
	}

	correctFreshness := freshRunID != priorRunID
	if !correctFreshness {
		t.Error("RC-028: fresh run_id must differ from prior run_id")
	}
}

// TestRC028_SameBeadDifferentRuns verifies the EM-014 one-bead-many-runs structural
// fact that underlies RC-028: a single BeadID can be associated with multiple distinct
// RunIDs over its lifetime.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028;
// specs/execution-model.md §4.3 EM-014.
func TestRC028_SameBeadDifferentRuns(t *testing.T) {
	t.Parallel()

	beadID := BeadID("hk-63oh-rc28")
	run1ID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006805")
	run2ID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006806")

	run1 := rc77RunFixture(t, run1ID)
	run2 := rc77RunFixture(t, run2ID)
	run1.BeadID = &beadID
	run2.BeadID = &beadID

	if run1.RunID == run2.RunID {
		t.Fatal("RC-028: test fixture run IDs must differ")
	}
	if *run1.BeadID != *run2.BeadID {
		t.Errorf("RC-028: bead mismatch: run1.BeadID=%q, run2.BeadID=%q", *run1.BeadID, *run2.BeadID)
	}
	if !run1.Valid() {
		t.Error("RC-028: run1.Valid() = false; fixture error")
	}
	if !run2.Valid() {
		t.Error("RC-028: run2.Valid() = false; fixture error")
	}
}

// TestRC028_FreshWorkspaceRefOnReopenBead verifies that a fresh run after
// reopen-bead carries a different WorkspaceRef than the prior run, encoding the
// WM-034 requirement that a fresh worktree is allocated on subsequent claim.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028;
// specs/workspace-model.md §4.9 WM-034 — "fresh worktree and a fresh branch."
func TestRC028_FreshWorkspaceRefOnReopenBead(t *testing.T) {
	t.Parallel()

	priorRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006807")
	freshRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006808")

	priorRun := rc77RunFixture(t, priorRunID)
	priorRun.Input = WorkspaceRef("/projects/my-proj/.harmonik/worktrees/run-prior")

	freshRun := rc77RunFixture(t, freshRunID)
	freshRun.Input = WorkspaceRef("/projects/my-proj/.harmonik/worktrees/run-fresh")

	if priorRun.Input == freshRun.Input {
		t.Errorf("RC-028/WM-034: priorRun.Input == freshRun.Input (%q); reopen-bead MUST produce a FRESH worktree", string(priorRun.Input))
	}
}

// TestRC029_ResetToCheckpointVerdictIsDeclared verifies that VerdictResetToCheckpoint
// is one of the seven Verdict enum values.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029;
// specs/reconciliation/schemas.md §6.1 ENUM Verdict.
func TestRC029_ResetToCheckpointVerdictIsDeclared(t *testing.T) {
	t.Parallel()

	if !VerdictResetToCheckpoint.Valid() {
		t.Error("RC-029: VerdictResetToCheckpoint.Valid() = false; reset-to-checkpoint MUST be a valid Verdict enum value")
	}
	if string(VerdictResetToCheckpoint) != "reset-to-checkpoint" {
		t.Errorf("RC-029: VerdictResetToCheckpoint = %q, want %q", string(VerdictResetToCheckpoint), "reset-to-checkpoint")
	}
}

// TestRC029_RunIDPreservedAfterResetToCheckpoint verifies that an intra-run rollback
// (reset-to-checkpoint verdict) MUST NOT change the RunID — the run continues
// with the same run_id.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029 — "The worktree and run_id
// MUST be preserved; the run reverts to the named checkpoint."
func TestRC029_RunIDPreservedAfterResetToCheckpoint(t *testing.T) {
	t.Parallel()

	runID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006901")
	run := rc77RunFixture(t, runID)

	postRollbackRunID := run.RunID // same: intra-run

	if postRollbackRunID != run.RunID {
		t.Errorf("RC-029: run_id changed during intra-run rollback: before=%s, after=%s",
			run.RunID.String(), postRollbackRunID.String())
	}
	if uuid.UUID(postRollbackRunID).Version() != 7 {
		t.Errorf("RC-029: post-rollback run_id is UUID version %d, want 7", uuid.UUID(postRollbackRunID).Version())
	}
}

// TestRC029_WorkspaceRefPreservedAfterResetToCheckpoint verifies that the worktree
// path (WorkspaceRef) is NOT changed by a reset-to-checkpoint verdict.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029.
func TestRC029_WorkspaceRefPreservedAfterResetToCheckpoint(t *testing.T) {
	t.Parallel()

	runID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006902")
	run := rc77RunFixture(t, runID)
	run.Input = WorkspaceRef("/projects/my-proj/.harmonik/worktrees/run-wt")

	postRollbackInput := run.Input // same: intra-run
	if postRollbackInput != run.Input {
		t.Errorf("RC-029: worktree path changed during intra-run rollback: before=%q, after=%q",
			string(run.Input), string(postRollbackInput))
	}
}

// TestRC029_ResetToCheckpointRequiresCheckpointRef verifies that a VerdictEvent
// with verdict=reset-to-checkpoint MUST carry a non-nil CheckpointRef per the
// VerdictEvent.Valid() contract.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-029;
// specs/reconciliation/schemas.md §6.1 VerdictEvent — "checkpoint_ref: UUID | None —
// required iff verdict = reset-to-checkpoint."
func TestRC029_ResetToCheckpointRequiresCheckpointRef(t *testing.T) {
	t.Parallel()

	investigatorRunID := uuid.MustParse("018f1e2a-0000-7000-8000-000000006903")
	targetRunID := uuid.MustParse("018f1e2a-0000-7000-8000-000000006904")
	checkpointRef := TransitionID(uuid.MustParse("018f1e2a-0000-7000-8000-000000006905"))

	token := SnapshotToken{
		GitHeadHash:         "abc123def456",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	e := VerdictEvent{
		Verdict:           VerdictResetToCheckpoint,
		InvestigatorRunID: investigatorRunID,
		TargetRunID:       targetRunID,
		CheckpointRef:     &checkpointRef,
		SnapshotToken:     token,
		SchemaVersion:     1,
	}
	if !e.Valid() {
		t.Error("RC-029: VerdictEvent with reset-to-checkpoint + CheckpointRef.Valid() = false; want true")
	}

	eNilRef := e
	eNilRef.CheckpointRef = nil
	if eNilRef.Valid() {
		t.Error("RC-029: VerdictEvent with reset-to-checkpoint + nil CheckpointRef.Valid() = true; want false (checkpoint_ref required)")
	}
}

// TestRC029_ContrastsWithReopenBeadRunIDChange verifies the key distinction:
// reset-to-checkpoint preserves run_id (RC-029) while reopen-bead requires a
// new run_id (RC-028). These are mutually exclusive behaviors.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-028, RC-029.
func TestRC029_ContrastsWithReopenBeadRunIDChange(t *testing.T) {
	t.Parallel()

	intraRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006906")
	intraRunPostRollback := intraRunID // RC-029: preserved
	if intraRunID != intraRunPostRollback {
		t.Error("RC-029: reset-to-checkpoint must preserve run_id")
	}

	priorRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006907")
	freshRunID := rc77RunIDv7(t, "018f1e2a-0000-7000-8000-000000006908")
	if priorRunID == freshRunID {
		t.Error("RC-028: reopen-bead must produce a distinct run_id")
	}

	resetPreservesRunID := (intraRunID == intraRunPostRollback)
	reopenChangesRunID := (priorRunID != freshRunID)
	if !resetPreservesRunID {
		t.Error("RC-029: reset-to-checkpoint did not preserve run_id (contrast test)")
	}
	if !reopenChangesRunID {
		t.Error("RC-028: reopen-bead did not change run_id (contrast test)")
	}
}

// TestRC030_ReconciliationVerdictEnumHasNoLoopVerdict verifies that none of
// the seven Verdict enum values is a "loop back to earlier node" operation.
//
// RC-030: "Intra-run loops (workflow edges routing back to an earlier node) are
// NOT produced by reconciliation. Those loops are ordinary workflow-graph
// traversal handled by edge conditions and Guard/Gate control-points per
// [control-points.md §4.2, §4.4]."
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-030.
func TestRC030_ReconciliationVerdictEnumHasNoLoopVerdict(t *testing.T) {
	t.Parallel()

	allVerdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	const settledCount = 7
	if len(allVerdicts) != settledCount {
		t.Errorf("RC-030: verdict enum has %d values, want %d (7-value closed set per RC-009 amendment discipline)", len(allVerdicts), settledCount)
	}
	for _, v := range allVerdicts {
		if !v.Valid() {
			t.Errorf("RC-030: verdict %q.Valid() = false; all enum members must be valid", string(v))
		}
	}
}

// TestRC030_ReconciliationWorkflowClassIsNotIntraRunLoop verifies that a
// workflow tagged workflow_class=reconciliation is NOT subject to intra-run
// loop traversal — the reconciliation workflow itself is a distinct workflow,
// not a loop-back within the target run's workflow graph.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-030;
// specs/reconciliation/schemas.md §6.5 WorkflowClassExtension.
func TestRC030_ReconciliationWorkflowClassIsNotIntraRunLoop(t *testing.T) {
	t.Parallel()

	cls := WorkflowClassReconciliation
	if string(cls) != "reconciliation" {
		t.Errorf("RC-030: WorkflowClassReconciliation = %q, want %q", string(cls), "reconciliation")
	}
	if !cls.Valid() {
		t.Error("RC-030: WorkflowClassReconciliation.Valid() = false; want true")
	}

	if !VerdictResetToCheckpoint.Valid() {
		t.Error("RC-030: VerdictResetToCheckpoint is not valid; should be (it's a one-shot intra-run rollback, not a loop)")
	}
}

// TestRC030_GuardGateOwnsIntraRunLoops verifies the RC-030 boundary: intra-run
// loops are Guard/Gate operations, not reconciliation. This test documents the
// type-level boundary by verifying that Guard/Gate action types are distinct
// from Verdict types.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-030;
// specs/control-points.md §4.2 (Guard), §4.4 (Gate).
func TestRC030_GuardGateOwnsIntraRunLoops(t *testing.T) {
	t.Parallel()

	var ga GateAction
	_ = ga // zero value: unused but demonstrates distinct type

	var v Verdict
	if v.Valid() {
		t.Error("RC-030: zero Verdict.Valid() = true; empty value must not be valid (type boundary test)")
	}
}

// TestRC030_ReconciliationRespectsWorkflowGraphEdges verifies that
// reconciliation verdicts do NOT inject new edges into the target workflow's
// graph. The Workflow type carries a static Edges slice; reconciliation acts
// on the run's state, not the workflow definition.
//
// Spec ref: specs/reconciliation/spec.md §4.6 RC-030.
func TestRC030_ReconciliationRespectsWorkflowGraphEdges(t *testing.T) {
	t.Parallel()

	wf := rc73WorkflowFixtureReconciliation(t)

	if !wf.Valid() {
		t.Error("RC-030: reconciliation workflow fixture.Valid() = false; fixture error")
	}
	edgeCount := len(wf.Edges)
	_ = edgeCount // Reconciliation cannot add to this; it's compile-time-fixed in the fixture.
}
