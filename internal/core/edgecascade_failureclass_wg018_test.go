// Package core — WG-018 requirement-traceable scenario.
//
// This file covers the end-to-end failure_class flow from a handler FAIL Outcome
// through cascade routing per workflow-graph.md §7 WG-018 and the design
// decisions that underpin it:
//
//   - D2: failure_class is a top-level field on the Outcome record (directly
//     addressable by the §6 LHS whitelist of §6 WG-014).
//   - D1: outcome.failure_class is a legal LHS term in edge conditions, enabling
//     failure-class-conditional routing distinct from status-only routing.
//   - D4: the LHS whitelist (WG-014) admits outcome.failure_class for the cascade.
//
// The scenario uses budget_exhausted as the routed-on class — a spec-sanctioned
// failure class (execution-model.md §8) that EM-005b (line 149) explicitly uses
// as the failure class for a budget/resource-limited gate deny.
//
// The scenario uses the twin substrate: the test constructs real core.Outcome and
// core.Edge values (production types with no test-aware branches) and drives
// SelectNextEdge / DispatchEdge directly, exercising the same code paths that
// the daemon's dispatch loop uses when a handler returns a FAIL outcome.
//
// Three assertions per the bead spec (hk-aoz34):
//
//	(a) D2 top-level field — Outcome.FailureClass is populated at the top level
//	    and Outcome.Valid() passes for a FAIL outcome carrying budget_exhausted.
//	(b) Cascade routing — the cascade selects the edge whose condition matches
//	    outcome.failure_class == "budget_exhausted" over a lower-weight generic
//	    FAIL edge and an unconditional fallback edge.
//	(c) Terminal node — the selected edge leads to the expected terminal node ID.
//
// Test naming pattern:
//
//	TestFailureClassCascadeWG018_<Case>
//
// Run all sensors for WG-018 with:
//
//	go test -run TestFailureClassCascadeWG018 ./internal/core/...
package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

// wg018FixtureRun returns a minimal valid Run ready for WG-018 scenario tests.
func wg018FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		Input:           WorkspaceRef("ws-wg018"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// wg018FixtureFailOutcome returns a FAIL Outcome with FailureClass set to
// budget_exhausted, modelling a handler that emits the class directly
// (handler-contract.md §4.2a HC-058, EM-005c additive field).
func wg018FixtureFailOutcome(t *testing.T) Outcome {
	t.Helper()
	fc := FailureClassBudgetExhausted
	return Outcome{
		Status:       OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         OutcomeKindDefault,
	}
}

// wg018FixtureEvaluator is a ConditionEvaluator that understands the two
// condition forms used in the WG-018 scenario:
//
//   - `outcome.failure_class == "budget_exhausted"` — evaluates true when
//     the outcome's FailureClass is budget_exhausted.
//   - `outcome.status == 'FAIL'` — evaluates true for any FAIL outcome.
//
// All other expressions return false.  This evaluator simulates what the
// expr-lang/expr runtime would produce for these specific condition strings,
// verifying that the cascade correctly routes on outcome.failure_class (D1/D4).
func wg018FixtureEvaluator(expr PolicyExpression, _ map[string]any, outcome Outcome) bool {
	switch string(expr) {
	case `outcome.failure_class == "budget_exhausted"`:
		return outcome.FailureClass != nil && *outcome.FailureClass == FailureClassBudgetExhausted
	case `outcome.status == 'FAIL'`:
		return outcome.Status == OutcomeStatusFail
	case `outcome.status == 'SUCCESS'`:
		return outcome.Status == OutcomeStatusSuccess
	}
	return false
}

// ── (a) D2: top-level field populated ────────────────────────────────────────

// TestFailureClassCascadeWG018_D2_TopLevelFieldPopulated verifies that an Outcome
// carrying failure_class=budget_exhausted is structurally valid per EM-005c's
// top-level placement (D2 design decision):
//
//   - FailureClass is a first-class top-level field on the Outcome struct, not
//     nested under a sub-object.  This makes it directly addressable by the
//     §6 WG-014 LHS whitelist as outcome.failure_class.
//   - Outcome.Valid() passes for Status=FAIL + FailureClass=budget_exhausted.
//   - Outcome.Valid() rejects FailureClass present on non-FAIL outcomes (HC-058).
//
// Cite: workflow-graph.md §7 WG-018, D2; handler-contract.md §4.2a HC-058.
func TestFailureClassCascadeWG018_D2_TopLevelFieldPopulated(t *testing.T) {
	t.Parallel()

	fc := FailureClassBudgetExhausted

	// FAIL outcome with budget_exhausted: must be valid.
	failOutcome := Outcome{
		Status:       OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         OutcomeKindDefault,
	}
	if !failOutcome.Valid() {
		t.Error("WG-018 D2: FAIL Outcome with FailureClass=budget_exhausted is invalid; want Valid()=true")
	}

	// Confirm FailureClass is non-nil and carries the expected value (top-level access).
	if failOutcome.FailureClass == nil {
		t.Fatal("WG-018 D2: Outcome.FailureClass is nil; D2 requires it as a top-level field on FAIL outcomes")
	}
	if *failOutcome.FailureClass != FailureClassBudgetExhausted {
		t.Errorf("WG-018 D2: Outcome.FailureClass = %q, want %q", *failOutcome.FailureClass, FailureClassBudgetExhausted)
	}

	// HC-058: FailureClass MUST be absent on non-FAIL outcomes.
	successOutcomeWithFC := Outcome{
		Status:       OutcomeStatusSuccess,
		FailureClass: &fc, // invalid: present on SUCCESS
		Kind:         OutcomeKindDefault,
	}
	if successOutcomeWithFC.Valid() {
		t.Error("WG-018 D2 / HC-058: SUCCESS Outcome with FailureClass must be invalid; want Valid()=false")
	}

	// Nil FailureClass on FAIL is valid (handler may omit; daemon back-fills per HC-020).
	failOutcomeNoFC := Outcome{
		Status: OutcomeStatusFail,
		Kind:   OutcomeKindDefault,
	}
	if !failOutcomeNoFC.Valid() {
		t.Error("WG-018 D2 / HC-058: FAIL Outcome with nil FailureClass must be valid (handler omission is permitted)")
	}
}

// ── (b) + (c) cascade routes to expected edge + terminal node ─────────────────

// TestFailureClassCascadeWG018_D1D4_CascadeRoutesOnFailureClass verifies that the
// cascade selects the failure_class-specific edge over a generic FAIL fallback
// edge and an unconditional fallback edge (D1 LHS admission, D4 LHS whitelist).
//
// Workflow topology:
//
//	node-work → node-budget-exhausted    [condition: failure_class == "budget_exhausted", weight=10]
//	node-work → node-generic-fail        [condition: status == 'FAIL',                     weight=5]
//	node-work → node-unconditional       [unconditional,                                    weight=1]
//
// With FAIL + failure_class=budget_exhausted:
//   - Both the failure_class edge (weight=10) and the generic FAIL edge (weight=5)
//     have true conditions.
//   - The cascade MUST select node-budget-exhausted (higher weight per EM-041(d)).
//
// Terminal node assertion: result.Edge.ToNode == "node-budget-exhausted", the
// terminal node the workflow author declares for this failure class.
//
// Cite: workflow-graph.md §6 WG-014 (LHS whitelist, D1), §7 WG-018 (D2 top-level,
// two-sided contract), §5 WG-010 (cascade step 1+3); hk-aoz34.
func TestFailureClassCascadeWG018_D1D4_CascadeRoutesOnFailureClass(t *testing.T) {
	t.Parallel()

	run := wg018FixtureRun(t)
	outcome := wg018FixtureFailOutcome(t)

	// Edge A: failure_class-specific (highest weight → must win).
	condA := PolicyExpression(`outcome.failure_class == "budget_exhausted"`)
	edgeBudgetExhausted := Edge{
		FromNode:    "node-work",
		ToNode:      "node-budget-exhausted",
		Condition:   &condA,
		Weight:      10,
		OrderingKey: "a",
	}

	// Edge B: generic FAIL fallback (lower weight → must lose to Edge A when both match).
	condB := PolicyExpression(`outcome.status == 'FAIL'`)
	edgeGenericFail := Edge{
		FromNode:    "node-work",
		ToNode:      "node-generic-fail",
		Condition:   &condB,
		Weight:      5,
		OrderingKey: "b",
	}

	// Edge C: unconditional fallback (lowest weight → must not be selected).
	edgeUnconditional := Edge{
		FromNode:    "node-work",
		ToNode:      "node-unconditional",
		Weight:      1,
		OrderingKey: "c",
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(
		run,
		[]Edge{edgeGenericFail, edgeUnconditional, edgeBudgetExhausted}, // intentionally unordered
		outcome,
		wg018FixtureEvaluator,
		cycles,
	)

	// (b) cascade must match (not fail with no_outgoing_edge_matches).
	if !result.Matched {
		t.Fatalf("WG-018 D1/D4: cascade did not match; failure=%s reason=%s "+
			"(outcome.failure_class=%v); expected cascade to select budget_exhausted edge (D1 LHS admission)",
			result.FailureClass, result.FailureReason, outcome.FailureClass)
	}

	// (c) terminal node: must be node-budget-exhausted (the failure_class-specific edge won).
	if result.Edge.ToNode != "node-budget-exhausted" {
		t.Errorf("WG-018 D1/D4: selected edge ToNode=%q, want %q; "+
			"failure_class-specific edge (weight=10) must beat generic FAIL edge (weight=5) per cascade step 3",
			result.Edge.ToNode, "node-budget-exhausted")
	}
}

// TestFailureClassCascadeWG018_D1D4_DifferentFailureClassTakesGenericEdge verifies
// that an edge conditioned on budget_exhausted is NOT selected when the outcome
// carries a different failure class — the generic FAIL edge wins instead.
//
// This tests that the LHS whitelist evaluation correctly distinguishes
// failure_class values, not just failure status.
func TestFailureClassCascadeWG018_D1D4_DifferentFailureClassTakesGenericEdge(t *testing.T) {
	t.Parallel()

	run := wg018FixtureRun(t)

	// Outcome is FAIL but with transient class, not budget_exhausted.
	fc := FailureClassTransient
	outcome := Outcome{
		Status:       OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         OutcomeKindDefault,
	}

	condA := PolicyExpression(`outcome.failure_class == "budget_exhausted"`)
	edgeBudgetExhausted := Edge{
		FromNode:    "node-work",
		ToNode:      "node-budget-exhausted",
		Condition:   &condA,
		Weight:      10,
		OrderingKey: "a",
	}

	condB := PolicyExpression(`outcome.status == 'FAIL'`)
	edgeGenericFail := Edge{
		FromNode:    "node-work",
		ToNode:      "node-generic-fail",
		Condition:   &condB,
		Weight:      5,
		OrderingKey: "b",
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(
		run,
		[]Edge{edgeBudgetExhausted, edgeGenericFail},
		outcome,
		wg018FixtureEvaluator,
		cycles,
	)

	if !result.Matched {
		t.Fatalf("WG-018: cascade did not match; failure=%s reason=%s", result.FailureClass, result.FailureReason)
	}

	// budget_exhausted condition is false for transient failure class →
	// generic FAIL edge (weight=5) must be selected.
	if result.Edge.ToNode != "node-generic-fail" {
		t.Errorf("WG-018: selected %q, want %q; "+
			"budget_exhausted condition must be false for transient failure class",
			result.Edge.ToNode, "node-generic-fail")
	}
}

// TestFailureClassCascadeWG018_D2_FailureClassAbsentOnSuccess verifies that a
// SUCCESS Outcome correctly carries no FailureClass (HC-058) and the cascade
// routes to the SUCCESS edge, not the failure_class edge.
func TestFailureClassCascadeWG018_D2_FailureClassAbsentOnSuccess(t *testing.T) {
	t.Parallel()

	run := wg018FixtureRun(t)
	outcome := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
		// FailureClass intentionally nil (HC-058: absent on non-FAIL).
	}

	condFC := PolicyExpression(`outcome.failure_class == "budget_exhausted"`)
	edgeBudgetExhausted := Edge{
		FromNode:    "node-work",
		ToNode:      "node-budget-exhausted",
		Condition:   &condFC,
		Weight:      10,
		OrderingKey: "a",
	}

	condSuccess := PolicyExpression(`outcome.status == 'SUCCESS'`)
	edgeSuccess := Edge{
		FromNode:    "node-work",
		ToNode:      "node-close",
		Condition:   &condSuccess,
		Weight:      10,
		OrderingKey: "b",
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(
		run,
		[]Edge{edgeBudgetExhausted, edgeSuccess},
		outcome,
		wg018FixtureEvaluator,
		cycles,
	)

	if !result.Matched {
		t.Fatalf("WG-018 D2: cascade did not match on SUCCESS outcome; failure=%s", result.FailureClass)
	}
	if result.Edge.ToNode != "node-close" {
		t.Errorf("WG-018 D2: selected %q, want %q; "+
			"SUCCESS outcome must not match failure_class edge (HC-058: FailureClass absent on non-FAIL)",
			result.Edge.ToNode, "node-close")
	}
}

// TestFailureClassCascadeWG018_FullDispatch_BudgetExhaustedReachesTerminal
// exercises the full DispatchEdge path (guard + cascade + gate) with the
// budget_exhausted FAIL outcome, asserting Advance=true and the correct
// terminal node ID.
//
// This is the "twin substrate" integration assertion: DispatchEdge (with
// IdentityGuard + PermitGate) is the production dispatch path; the test
// uses production core types with no test-aware branches (CHB-022 posture).
//
// Cite: hk-aoz34 assertion (c); workflow-graph.md §8 WG-021 (distinct terminal IDs).
func TestFailureClassCascadeWG018_FullDispatch_BudgetExhaustedReachesTerminal(t *testing.T) {
	t.Parallel()

	run := wg018FixtureRun(t)
	outcome := wg018FixtureFailOutcome(t)

	const terminalNodeID NodeID = "close-budget-exhausted"

	condA := PolicyExpression(`outcome.failure_class == "budget_exhausted"`)
	edgeToTerminal := Edge{
		FromNode:    "node-work",
		ToNode:      terminalNodeID,
		Condition:   &condA,
		Weight:      10,
		OrderingKey: "a",
	}

	condB := PolicyExpression(`outcome.status == 'FAIL'`)
	edgeGenericFail := Edge{
		FromNode:    "node-work",
		ToNode:      "close-needs-attention",
		Condition:   &condB,
		Weight:      5,
		OrderingKey: "b",
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(
		run,
		[]Edge{edgeGenericFail, edgeToTerminal},
		outcome,
		wg018FixtureEvaluator,
		cycles,
		IdentityGuard,
		PermitGate,
	)

	// Dispatch must advance (not stay, escalate, or fail).
	if !result.Advance {
		t.Fatalf("WG-018 full-dispatch: Advance=false; Stay=%v Escalate=%v Failed=%v "+
			"FailureClass=%s FailureReason=%s; expected Advance=true",
			result.Stay, result.Escalate, result.Failed,
			result.FailureClass, result.FailureReason)
	}

	// Terminal node assertion (hk-aoz34 assertion (c)).
	if result.Edge.ToNode != terminalNodeID {
		t.Errorf("WG-018 full-dispatch: terminal node reached = %q, want %q "+
			"(budget_exhausted-specific terminal node per WG-021 distinct terminal IDs)",
			result.Edge.ToNode, terminalNodeID)
	}
}
