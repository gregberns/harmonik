// Package core — EM-041 + EM-041a requirement-traceable sensors.
//
// This file covers the deterministic edge-selection cascade defined in
// execution-model.md §4.10.EM-041 and the context-update ordering rule defined
// in §4.10.EM-041a.
//
// Test naming pattern:
//
//	TestEdgeCascadeEM041_<Case>
//	TestEdgeCascadeEM041a_<Case>
//
// Run all sensors for EM-041 with:
//
//	go test -run TestEdgeCascadeEM041 ./internal/core/...
package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// edgeCascadeFixtureRun returns a minimal valid Run ready for cascade tests.
// run.Context is pre-allocated so ApplyContextUpdates may write into it.
func edgeCascadeFixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-1"),
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// edgeCascadeFixtureOutcome returns a minimal valid Outcome (SUCCESS, default kind).
func edgeCascadeFixtureOutcome(t *testing.T) Outcome {
	t.Helper()
	return Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
	}
}

// edgeCascadeFixtureEdge returns a valid Edge with the given ordering_key and
// weight, from "node-a" to the supplied toNode. Condition is nil (unconditional).
func edgeCascadeFixtureEdge(t *testing.T, toNode NodeID, weight int, orderingKey string) Edge {
	t.Helper()
	return Edge{
		FromNode:    "node-a",
		ToNode:      toNode,
		Weight:      weight,
		OrderingKey: orderingKey,
	}
}

// edgeCascadeFixtureEvalAlwaysTrue is a ConditionEvaluator that always returns true.
func edgeCascadeFixtureEvalAlwaysTrue(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
	return true
}

// edgeCascadeFixtureEvalAlwaysFalse is a ConditionEvaluator that always returns false.
func edgeCascadeFixtureEvalAlwaysFalse(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
	return false
}

// ── EM-041a: context-update ordering ────────────────────────────────────────

// TestEdgeCascadeEM041a_ContextUpdatesAppliedBeforeConditionEval verifies that
// outcome.ContextUpdates are merged into run.Context before any condition is
// evaluated, so cascade conditions observe post-update state (§4.10.EM-041a).
func TestEdgeCascadeEM041a_ContextUpdatesAppliedBeforeConditionEval(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	outcome.ContextUpdates = map[string]any{"flag": true}

	cond := PolicyExpression("flag == true")
	e := Edge{
		FromNode:    "node-a",
		ToNode:      "node-b",
		Condition:   &cond,
		Weight:      0,
		OrderingKey: "a",
	}

	// The evaluator observes run.Context at evaluation time; we verify that
	// "flag" is already present (context-update applied before eval).
	var seenCtx map[string]any
	eval := func(expr PolicyExpression, ctx map[string]any, _ Outcome) bool {
		seenCtx = ctx
		return true // let the edge through
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, eval, cycles)

	if !result.Matched {
		t.Fatalf("EM-041a: cascade did not match, want Matched=true; failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	// The context must carry the update at evaluation time.
	if v, ok := seenCtx["flag"]; !ok || v != true {
		t.Errorf("EM-041a: condition evaluator saw ctx[\"flag\"]=%v (present=%v), want true (EM-041a: updates precede condition eval)", v, ok)
	}
	// run.Context itself must reflect the update after SelectNextEdge returns.
	if v, ok := run.Context["flag"]; !ok || v != true {
		t.Errorf("EM-041a: run.Context[\"flag\"]=%v (present=%v) after SelectNextEdge, want true", v, ok)
	}
}

// TestEdgeCascadeEM041a_ExistingContextKeyOverwritten verifies that an update
// whose key already exists in run.Context overwrites the prior value.
func TestEdgeCascadeEM041a_ExistingContextKeyOverwritten(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	run.Context["counter"] = 1

	outcome := edgeCascadeFixtureOutcome(t)
	outcome.ContextUpdates = map[string]any{"counter": 2}

	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a")
	cycles := NewCycleCounter()
	_ = SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if run.Context["counter"] != 2 {
		t.Errorf("EM-041a: run.Context[\"counter\"]=%v, want 2 (overwrite)", run.Context["counter"])
	}
}

// TestEdgeCascadeEM041a_NilContextUpdatesIsNoop verifies that nil ContextUpdates
// leave run.Context unchanged.
func TestEdgeCascadeEM041a_NilContextUpdatesIsNoop(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	run.Context["existing"] = "value"

	outcome := edgeCascadeFixtureOutcome(t)
	outcome.ContextUpdates = nil

	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a")
	cycles := NewCycleCounter()
	_ = SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if run.Context["existing"] != "value" {
		t.Errorf("EM-041a: nil ContextUpdates should be a no-op; got run.Context[\"existing\"]=%v", run.Context["existing"])
	}
}

// ── EM-041 (a): condition filtering ─────────────────────────────────────────

// TestEdgeCascadeEM041_NilConditionIsUnconditional verifies that an edge with a
// nil Condition is always included in the matched set without calling the
// evaluator.
func TestEdgeCascadeEM041_NilConditionIsUnconditional(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	// Edge has nil Condition — evaluator must NOT be called.
	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a")
	if e.Condition != nil {
		t.Fatal("fixture error: Condition should be nil")
	}

	evalCalled := false
	eval := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
		evalCalled = true
		return false
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, eval, cycles)

	if evalCalled {
		t.Error("EM-041 (a): evaluator was called for nil-condition edge, want not called")
	}
	if !result.Matched {
		t.Errorf("EM-041 (a): nil-condition edge not matched, want Matched=true")
	}
}

// TestEdgeCascadeEM041_FalseConditionExcludesEdge verifies that an edge whose
// condition evaluates false is excluded from the matched set.
func TestEdgeCascadeEM041_FalseConditionExcludesEdge(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	cond := PolicyExpression("false")
	e := Edge{
		FromNode:    "node-a",
		ToNode:      "node-b",
		Condition:   &cond,
		Weight:      0,
		OrderingKey: "a",
	}
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysFalse, cycles)

	if !result.Failed {
		t.Error("EM-041 (a): false-condition edge should yield no-match failure, got Matched=true")
	}
	if result.FailureClass != FailureClassStructural {
		t.Errorf("EM-041 (a): expected FailureClassStructural, got %q", result.FailureClass)
	}
	if result.FailureReason != "no_outgoing_edge_matches" {
		t.Errorf("EM-041 (a): expected reason %q, got %q", "no_outgoing_edge_matches", result.FailureReason)
	}
}

// TestEdgeCascadeEM041_TrueConditionIncludesEdge verifies that an edge whose
// condition evaluates true is included in the matched set.
func TestEdgeCascadeEM041_TrueConditionIncludesEdge(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	cond := PolicyExpression("true")
	e := Edge{
		FromNode:    "node-a",
		ToNode:      "node-b",
		Condition:   &cond,
		Weight:      0,
		OrderingKey: "a",
	}
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Errorf("EM-041 (a): true-condition edge not matched; failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-b" {
		t.Errorf("EM-041 (a): selected edge ToNode=%q, want %q", result.Edge.ToNode, "node-b")
	}
}

// TestEdgeCascadeEM041_OnlyConditionTrueEdgesAreMatched verifies that a mixed
// set of true/false-condition edges produces a matched set containing only the
// true-condition edges.
func TestEdgeCascadeEM041_OnlyConditionTrueEdgesAreMatched(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	condTrue := PolicyExpression("true")
	condFalse := PolicyExpression("false")

	eTrue := Edge{
		FromNode:    "node-a",
		ToNode:      "node-match",
		Condition:   &condTrue,
		Weight:      0,
		OrderingKey: "b",
	}
	eFalse := Edge{
		FromNode:    "node-a",
		ToNode:      "node-no-match",
		Condition:   &condFalse,
		Weight:      10, // higher weight — must NOT win because condition is false
		OrderingKey: "a",
	}

	eval := func(expr PolicyExpression, _ map[string]any, _ Outcome) bool {
		return string(expr) == "true"
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eFalse, eTrue}, outcome, eval, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (a): expected match, got failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-match" {
		t.Errorf("EM-041 (a): selected %q, want %q (false-condition edge must be excluded)",
			result.Edge.ToNode, "node-match")
	}
}

// ── EM-041 (b): preferred_label narrowing ───────────────────────────────────

// TestEdgeCascadeEM041_PreferredLabelNarrowsMatchedSet verifies that when
// outcome.PreferredLabel is set the cascade narrows to edges whose Label equals
// it, discarding unmatched edges.
func TestEdgeCascadeEM041_PreferredLabelNarrowsMatchedSet(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	label := "success-path"
	outcome.PreferredLabel = &label

	labelA := "success-path"
	labelB := "failure-path"
	eLabeled := Edge{
		FromNode:    "node-a",
		ToNode:      "node-success",
		Label:       &labelA,
		Weight:      0,
		OrderingKey: "b",
	}
	eUnlabeled := Edge{
		FromNode:    "node-a",
		ToNode:      "node-other",
		Label:       &labelB,
		Weight:      10, // higher weight — must NOT win because label doesn't match
		OrderingKey: "a",
	}

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eUnlabeled, eLabeled}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (b): expected match, got failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-success" {
		t.Errorf("EM-041 (b): selected %q, want %q (preferred_label must narrow set)",
			result.Edge.ToNode, "node-success")
	}
}

// TestEdgeCascadeEM041_PreferredLabelNoMatchFallsBackToFullSet verifies that
// when no edge matches PreferredLabel the hint is discarded and the full matched
// set is used (hint is non-binding per spec).
func TestEdgeCascadeEM041_PreferredLabelNoMatchFallsBackToFullSet(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	noMatch := "label-that-matches-nothing"
	outcome.PreferredLabel = &noMatch

	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a") // no Label set
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Errorf("EM-041 (b): unmatched preferred_label should fall back to full set, got failure: %s",
			result.FailureClass)
	}
	if result.Edge.ToNode != "node-b" {
		t.Errorf("EM-041 (b): selected %q, want %q (fallback to full set)", result.Edge.ToNode, "node-b")
	}
}

// ── EM-041 (c): suggested_next_ids narrowing ────────────────────────────────

// TestEdgeCascadeEM041_SuggestedNextIDsNarrowsMatchedSet verifies that when
// PreferredLabel is absent and SuggestedNextIDs is non-empty, the cascade
// narrows to edges whose ToNode is in SuggestedNextIDs.
func TestEdgeCascadeEM041_SuggestedNextIDsNarrowsMatchedSet(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	outcome.SuggestedNextIDs = []NodeID{"node-preferred"}

	eSuggested := edgeCascadeFixtureEdge(t, "node-preferred", 0, "b")
	eOther := edgeCascadeFixtureEdge(t, "node-other", 10, "a") // higher weight, should not win

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eOther, eSuggested}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (c): expected match, got failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-preferred" {
		t.Errorf("EM-041 (c): selected %q, want %q (SuggestedNextIDs must narrow set)",
			result.Edge.ToNode, "node-preferred")
	}
}

// TestEdgeCascadeEM041_SuggestedNextIDsNoMatchFallsBackToFullSet verifies that
// when no edge's ToNode appears in SuggestedNextIDs the hint is discarded.
func TestEdgeCascadeEM041_SuggestedNextIDsNoMatchFallsBackToFullSet(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	outcome.SuggestedNextIDs = []NodeID{"node-nonexistent"}

	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a")
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Errorf("EM-041 (c): unmatched SuggestedNextIDs should fall back to full set, got failure: %s",
			result.FailureClass)
	}
}

// TestEdgeCascadeEM041_PreferredLabelTakesPrecedenceOverSuggestedNextIDs
// verifies that when PreferredLabel is set, the SuggestedNextIDs branch is
// skipped entirely (spec: "ELSE IF outcome.suggested_next_ids").
func TestEdgeCascadeEM041_PreferredLabelTakesPrecedenceOverSuggestedNextIDs(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	lbl := "label-path"
	outcome.PreferredLabel = &lbl
	outcome.SuggestedNextIDs = []NodeID{"node-suggested"}

	labelA := "label-path"
	eLabeled := Edge{
		FromNode:    "node-a",
		ToNode:      "node-labeled",
		Label:       &labelA,
		Weight:      0,
		OrderingKey: "b",
	}
	eSuggested := edgeCascadeFixtureEdge(t, "node-suggested", 5, "a") // higher weight, no label

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eSuggested, eLabeled}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (b>c): expected match, got failure: %s", result.FailureClass)
	}
	if result.Edge.ToNode != "node-labeled" {
		t.Errorf("EM-041 (b>c): selected %q, want %q (preferred_label takes precedence over suggested_next_ids)",
			result.Edge.ToNode, "node-labeled")
	}
}

// ── EM-041 (d)+(e): weight + ordering_key tie-break ─────────────────────────

// TestEdgeCascadeEM041_HigherWeightSelectedFirst verifies that the edge with the
// higher Weight is selected when OrderingKey would otherwise distinguish them.
func TestEdgeCascadeEM041_HigherWeightSelectedFirst(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	eLow := edgeCascadeFixtureEdge(t, "node-low", 1, "a")
	eHigh := edgeCascadeFixtureEdge(t, "node-high", 10, "b")

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eLow, eHigh}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (d): expected match, got failure: %s", result.FailureClass)
	}
	if result.Edge.ToNode != "node-high" {
		t.Errorf("EM-041 (d): selected %q, want %q (higher weight wins)", result.Edge.ToNode, "node-high")
	}
}

// TestEdgeCascadeEM041_LexicalOrderingKeyTieBreak verifies that when weights are
// equal the edge with the lexically smaller OrderingKey is selected.
func TestEdgeCascadeEM041_LexicalOrderingKeyTieBreak(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	eB := edgeCascadeFixtureEdge(t, "node-b-key", 5, "b")
	eA := edgeCascadeFixtureEdge(t, "node-a-key", 5, "a")

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eB, eA}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (e): expected match, got failure: %s", result.FailureClass)
	}
	if result.Edge.ToNode != "node-a-key" {
		t.Errorf("EM-041 (e): selected %q, want %q (lexical ordering_key wins on equal weight)",
			result.Edge.ToNode, "node-a-key")
	}
}

// TestEdgeCascadeEM041_WeightBeatsOrderingKey verifies that a higher weight
// always beats a lexically-smaller ordering_key.
func TestEdgeCascadeEM041_WeightBeatsOrderingKey(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	// "a" < "b" lexically, but weight 1 < 10, so weight wins.
	eLexFirst := edgeCascadeFixtureEdge(t, "node-lex-first", 1, "a")
	eWeightFirst := edgeCascadeFixtureEdge(t, "node-weight-first", 10, "b")

	cycles := NewCycleCounter()
	result := SelectNextEdge(run, []Edge{eLexFirst, eWeightFirst}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Fatalf("EM-041 (d>e): expected match, got failure: %s", result.FailureClass)
	}
	if result.Edge.ToNode != "node-weight-first" {
		t.Errorf("EM-041 (d>e): selected %q, want %q (weight beats lexical ordering_key)",
			result.Edge.ToNode, "node-weight-first")
	}
}

// ── EM-046a: no-matching-edge failure ────────────────────────────────────────

// TestEdgeCascadeEM041_EmptyCandidatesYieldsStructuralFailure verifies that an
// empty candidate slice yields FailureClassStructural / no_outgoing_edge_matches
// per §4.10.EM-046a.
func TestEdgeCascadeEM041_EmptyCandidatesYieldsStructuralFailure(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, nil, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Failed {
		t.Error("EM-046a: empty candidate list should yield Failed=true")
	}
	if result.FailureClass != FailureClassStructural {
		t.Errorf("EM-046a: expected FailureClassStructural, got %q", result.FailureClass)
	}
	if result.FailureReason != "no_outgoing_edge_matches" {
		t.Errorf("EM-046a: expected reason %q, got %q", "no_outgoing_edge_matches", result.FailureReason)
	}
}

// TestEdgeCascadeEM041_AllConditionsFalseYieldsStructuralFailure verifies that
// when all edges fail their condition the result is FailureClassStructural.
func TestEdgeCascadeEM041_AllConditionsFalseYieldsStructuralFailure(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)

	cond := PolicyExpression("always false")
	edges := []Edge{
		{FromNode: "node-a", ToNode: "node-b", Condition: &cond, Weight: 0, OrderingKey: "a"},
		{FromNode: "node-a", ToNode: "node-c", Condition: &cond, Weight: 1, OrderingKey: "b"},
	}
	cycles := NewCycleCounter()
	result := SelectNextEdge(run, edges, outcome, edgeCascadeFixtureEvalAlwaysFalse, cycles)

	if !result.Failed {
		t.Error("EM-046a: all-false conditions should yield Failed=true")
	}
	if result.FailureClass != FailureClassStructural {
		t.Errorf("EM-046a: expected FailureClassStructural, got %q", result.FailureClass)
	}
}

// ── EM-043: traversal cap → compilation_loop ─────────────────────────────────

// TestEdgeCascadeEM041_TraversalCapReachedYieldsCompilationLoop verifies that
// when the selected edge's traversal count has reached its TraversalCap the
// cascade returns FailureClassCompilationLoop (§4.10.EM-043).
func TestEdgeCascadeEM041_TraversalCapReachedYieldsCompilationLoop(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	cycles := NewCycleCounter()

	cap1 := 1
	e := Edge{
		FromNode:     "node-a",
		ToNode:       "node-b",
		Weight:       0,
		OrderingKey:  "a",
		TraversalCap: &cap1,
	}

	// Simulate one prior traversal by directly incrementing the counter.
	_, err := cycles.Increment(run.RunID, "node-a", "node-b", nil)
	if err != nil {
		t.Fatalf("fixture error: Increment failed: %v", err)
	}

	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Failed {
		t.Error("EM-043: traversal cap reached should yield Failed=true")
	}
	if result.FailureClass != FailureClassCompilationLoop {
		t.Errorf("EM-043: expected FailureClassCompilationLoop, got %q", result.FailureClass)
	}
}

// TestEdgeCascadeEM041_TraversalCapNotReachedAllowsEdge verifies that the
// cascade selects the edge normally when the traversal count is below the cap.
func TestEdgeCascadeEM041_TraversalCapNotReachedAllowsEdge(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	cycles := NewCycleCounter()

	cap3 := 3
	e := Edge{
		FromNode:     "node-a",
		ToNode:       "node-b",
		Weight:       0,
		OrderingKey:  "a",
		TraversalCap: &cap3,
	}

	// One prior traversal — well below cap 3.
	_, err := cycles.Increment(run.RunID, "node-a", "node-b", nil)
	if err != nil {
		t.Fatalf("fixture error: Increment failed: %v", err)
	}

	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Errorf("EM-043: traversal below cap should allow edge; got failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
}

// TestEdgeCascadeEM041_NilTraversalCapIsUnbounded verifies that a nil
// TraversalCap never triggers the compilation_loop failure path.
func TestEdgeCascadeEM041_NilTraversalCapIsUnbounded(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	outcome := edgeCascadeFixtureOutcome(t)
	cycles := NewCycleCounter()

	e := edgeCascadeFixtureEdge(t, "node-b", 0, "a")
	// TraversalCap is nil — increment many times, should never trigger cap.
	for i := 0; i < 100; i++ {
		_, err := cycles.Increment(run.RunID, "node-a", "node-b", nil)
		if err != nil {
			t.Fatalf("fixture error: Increment[%d] failed: %v", i, err)
		}
	}

	result := SelectNextEdge(run, []Edge{e}, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles)

	if !result.Matched {
		t.Errorf("EM-043: nil TraversalCap should never trigger compilation_loop; got failure: %s",
			result.FailureClass)
	}
}

// ── Determinism assertion ────────────────────────────────────────────────────

// TestEdgeCascadeEM041_IdenticalInputsProduceIdenticalOutput verifies that the
// cascade is deterministic: identical inputs produce identical output regardless
// of call order (§4.10.EM-041: "The cascade MUST be deterministic").
func TestEdgeCascadeEM041_IdenticalInputsProduceIdenticalOutput(t *testing.T) {
	t.Parallel()

	buildRun := func() *Run {
		return &Run{
			RunID:           RunID(uuid.Must(uuid.NewV7())),
			WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
			WorkflowVersion: WorkflowVersion("0.1.0"),
			Input:           WorkspaceRef("ws-ref-1"),
			State:           StateID(uuid.Must(uuid.NewV7())),
			Context:         map[string]any{"env": "prod"},
			StartTime:       time.Now(),
		}
	}
	outcome := edgeCascadeFixtureOutcome(t)

	edges := []Edge{
		{FromNode: "node-a", ToNode: "node-c", Weight: 5, OrderingKey: "c"},
		{FromNode: "node-a", ToNode: "node-a2", Weight: 5, OrderingKey: "a"},
		{FromNode: "node-a", ToNode: "node-b2", Weight: 5, OrderingKey: "b"},
	}

	cycles1 := NewCycleCounter()
	cycles2 := NewCycleCounter()
	result1 := SelectNextEdge(buildRun(), edges, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles1)
	result2 := SelectNextEdge(buildRun(), edges, outcome, edgeCascadeFixtureEvalAlwaysTrue, cycles2)

	if result1.Matched != result2.Matched {
		t.Errorf("EM-041 determinism: Matched differs (%v vs %v)", result1.Matched, result2.Matched)
	}
	if result1.Matched && result2.Matched && result1.Edge.ToNode != result2.Edge.ToNode {
		t.Errorf("EM-041 determinism: selected different edges (%q vs %q) for identical inputs",
			result1.Edge.ToNode, result2.Edge.ToNode)
	}
}

// ── ApplyContextUpdates standalone ──────────────────────────────────────────

// TestApplyContextUpdates_MergesIntoContext is a direct unit test of the
// exported ApplyContextUpdates helper.
func TestApplyContextUpdates_MergesIntoContext(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	run.Context["existing"] = "old"

	ApplyContextUpdates(run, map[string]any{
		"existing": "new",
		"added":    42,
	})

	if run.Context["existing"] != "new" {
		t.Errorf("ApplyContextUpdates: existing key not overwritten; got %v", run.Context["existing"])
	}
	if run.Context["added"] != 42 {
		t.Errorf("ApplyContextUpdates: new key not added; got %v", run.Context["added"])
	}
}

// TestApplyContextUpdates_NilUpdatesIsNoop verifies nil update map leaves
// context unchanged.
func TestApplyContextUpdates_NilUpdatesIsNoop(t *testing.T) {
	t.Parallel()

	run := edgeCascadeFixtureRun(t)
	run.Context["k"] = "v"

	ApplyContextUpdates(run, nil)

	if run.Context["k"] != "v" {
		t.Errorf("ApplyContextUpdates: nil updates mutated context; got %v", run.Context["k"])
	}
	if len(run.Context) != 1 {
		t.Errorf("ApplyContextUpdates: nil updates changed context length; got %d", len(run.Context))
	}
}
