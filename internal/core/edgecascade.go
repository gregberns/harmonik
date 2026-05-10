package core

import "sort"

// ConditionEvaluator is a caller-supplied function that evaluates a
// PolicyExpression against the current run context and outcome.
//
// The expression is a policy expression per [control-points.md §6.4]. Full
// syntactic evaluation (expr-lang/expr) is performed at the subsystem layer;
// this core type is intentionally thin. When expr is nil the edge is
// unconditional and ConditionEvaluator is never called for that edge.
//
// Returning true means the condition is satisfied and the edge is a candidate
// for selection. Returning false means the edge is excluded from the matched
// set.
type ConditionEvaluator func(expr PolicyExpression, ctx map[string]any, outcome Outcome) bool

// CascadeResult is the outcome of [SelectNextEdge].
//
// Exactly one of Matched or Failed is true after a successful call; the zero
// value is invalid.
type CascadeResult struct {
	// Matched is true when the cascade selected an edge.
	Matched bool

	// Edge is the selected edge when Matched is true. The zero value when
	// Matched is false.
	Edge Edge

	// Failed is true when the cascade produced no satisfiable match
	// (FailureClassStructural, reason "no_outgoing_edge_matches") or when the
	// traversal cap for the selected edge has been reached
	// (FailureClassCompilationLoop).
	Failed bool

	// FailureClass carries the failure class when Failed is true. Zero value
	// when Matched is true.
	FailureClass FailureClass

	// FailureReason is a human-readable string identifying the cause of
	// failure. Populated whenever Failed is true; empty when Matched is true.
	FailureReason string
}

// SelectNextEdge implements the deterministic edge-selection cascade defined in
// execution-model.md §4.10.EM-041 and §4.10.EM-041a.
//
// # Ordering
//
// The cascade runs in this order:
//
//  1. [§4.10.EM-041a] Apply outcome.ContextUpdates to run.Context BEFORE
//     evaluating any edge condition. Cascade conditions therefore observe
//     post-update state.
//  2. [§4.10.EM-041 (a)] Filter candidates to edges whose Condition evaluates
//     true against the post-update context and outcome. A nil Condition is
//     unconditionally true.
//  3. [§4.10.EM-041 (b)] If outcome.PreferredLabel is set, narrow the matched
//     set to edges whose Label equals PreferredLabel. If the narrowed set is
//     non-empty it replaces the full matched set; otherwise the hint is
//     discarded and the full matched set is retained.
//  4. [§4.10.EM-041 (c)] Else if outcome.SuggestedNextIDs is non-empty, narrow
//     the matched set to edges whose ToNode is in SuggestedNextIDs. Same
//     non-empty-replaces semantics as (b).
//  5. [§4.10.EM-041 (d)+(e)] Sort the matched set by -Weight (descending)
//     then by OrderingKey (lexical ascending) as the final tie-break.
//  6. If the matched set is empty, return
//     FailureClassStructural / "no_outgoing_edge_matches".
//  7. Select matched[0]. If its TraversalCap is set and cycles has already
//     reached the cap, return FailureClassCompilationLoop.
//
// # Guard and gate integration
//
// Guards (EM-042) reorder the candidate list before this cascade runs; gates
// (EM-042) evaluate the chosen edge after this cascade returns. Both are
// upstream/downstream concerns owned by bead hk-b3f.55; SelectNextEdge is
// guard- and gate-agnostic.
//
// # Parameters
//
//   - run: the current run. run.Context is mutated in-place by the
//     EM-041a context-update step. Must not be nil.
//   - candidates: outgoing edges from the current node, already guard-reordered
//     if guards are active. May be empty (yields the no-match failure).
//   - outcome: the handler-produced outcome driving selection.
//   - eval: caller-supplied condition evaluator. Must not be nil. It is called
//     for each edge with a non-nil Condition.
//   - cycles: the run's in-memory CycleCounter. Must not be nil. It is NOT
//     incremented here; the caller increments after committing the durable
//     transition.
func SelectNextEdge(
	run *Run,
	candidates []Edge,
	outcome Outcome,
	eval ConditionEvaluator,
	cycles *CycleCounter,
) CascadeResult {
	// §4.10.EM-041a — apply context updates BEFORE evaluating conditions.
	ApplyContextUpdates(run, outcome.ContextUpdates)

	// §4.10.EM-041 (a) — filter to condition-true edges.
	matched := make([]Edge, 0, len(candidates))
	for _, e := range candidates {
		if e.Condition == nil || eval(*e.Condition, run.Context, outcome) {
			matched = append(matched, e)
		}
	}

	// §4.10.EM-041 (b) — prefer label matching outcome.PreferredLabel.
	if outcome.PreferredLabel != nil {
		label := *outcome.PreferredLabel
		preferred := make([]Edge, 0, len(matched))
		for _, e := range matched {
			if e.Label != nil && *e.Label == label {
				preferred = append(preferred, e)
			}
		}
		if len(preferred) > 0 {
			matched = preferred
		}
	} else if len(outcome.SuggestedNextIDs) > 0 {
		// §4.10.EM-041 (c) — prefer edges matching outcome.SuggestedNextIDs.
		// Build a set for O(1) lookup.
		hintSet := make(map[NodeID]struct{}, len(outcome.SuggestedNextIDs))
		for _, id := range outcome.SuggestedNextIDs {
			hintSet[id] = struct{}{}
		}
		suggested := make([]Edge, 0, len(matched))
		for _, e := range matched {
			if _, ok := hintSet[e.ToNode]; ok {
				suggested = append(suggested, e)
			}
		}
		if len(suggested) > 0 {
			matched = suggested
		}
	}

	// §4.10.EM-041 (d)+(e) — sort by -Weight then by OrderingKey (lexical).
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Weight != matched[j].Weight {
			return matched[i].Weight > matched[j].Weight // higher weight first
		}
		return matched[i].OrderingKey < matched[j].OrderingKey // lexical ascending
	})

	// §4.10.EM-046a — no matching edge: structural failure.
	if len(matched) == 0 {
		return CascadeResult{
			Failed:        true,
			FailureClass:  FailureClassStructural,
			FailureReason: "no_outgoing_edge_matches",
		}
	}

	chosen := matched[0]

	// §4.10.EM-043 — traversal-cap check (compilation_loop).
	if chosen.TraversalCap != nil {
		count := cycles.Get(run.RunID, chosen.FromNode, chosen.ToNode)
		if count >= uint64(*chosen.TraversalCap) {
			return CascadeResult{
				Failed:        true,
				FailureClass:  FailureClassCompilationLoop,
				FailureReason: "traversal cap reached",
			}
		}
	}

	return CascadeResult{
		Matched: true,
		Edge:    chosen,
	}
}

// ApplyContextUpdates merges updates into run.Context per §4.10.EM-041a.
//
// Each key/value pair in updates is written into run.Context; existing keys are
// overwritten. A nil or empty updates map is a no-op. run.Context must be
// non-nil (enforced by [Run.Valid]).
func ApplyContextUpdates(run *Run, updates map[string]any) {
	for k, v := range updates {
		run.Context[k] = v
	}
}
