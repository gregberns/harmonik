package core

// GuardEvaluator is a caller-supplied function that reorders the candidate
// edge list before the EM-041 cascade runs.
//
// Per execution-model.md §4.10.EM-042 and control-points.md §6.4:
//
//   - A guard MAY reorder the candidate slice in any way it chooses.
//   - A guard MUST NOT add edges that were not in the input slice.
//   - A guard MUST NOT remove edges from the input slice.
//   - A guard MUST NOT block (suppress) edges.
//
// The returned slice MUST contain exactly the same set of Edge values as the
// input candidates slice (same length, same elements, possibly in a different
// order). The EM-041 cascade treats the returned slice as the authoritative
// ordering going into condition filtering. When no guard is configured, pass
// [IdentityGuard] or supply the candidates slice directly to [SelectNextEdge].
//
// Invariant enforcement (length check) is the caller's responsibility.
// [DispatchEdge] verifies the returned-length invariant and panics if violated.
type GuardEvaluator func(run *Run, candidates []Edge, outcome Outcome) []Edge

// IdentityGuard is a GuardEvaluator that returns the candidates slice unchanged.
// Use it when no guard is configured.
func IdentityGuard(_ *Run, candidates []Edge, _ Outcome) []Edge {
	return candidates
}

// GateEvaluator is a caller-supplied function that evaluates the chosen edge
// after the EM-041 cascade selects it and returns a GateAction verdict.
//
// Per execution-model.md §4.10.EM-042 and control-points.md §6.2:
//
//   - [GateActionAllow]: the transition proceeds normally.
//   - [GateActionDeny]: the run MUST remain in the source state; no checkpoint
//     is written; this does NOT constitute a durable transition per EM-023a.
//     The run enters a gate-pending sub-state per EM-042a.
//   - [GateActionEscalateToHuman]: the run enters quarantine awaiting
//     external resolution.
//
// When no gate is configured, pass [PermitGate].
type GateEvaluator func(run *Run, chosen Edge, outcome Outcome) GateAction

// PermitGate is a GateEvaluator that always returns [GateActionAllow].
// Use it when no gate is configured.
func PermitGate(_ *Run, _ Edge, _ Outcome) GateAction {
	return GateActionAllow
}

// DispatchOutcome is the result of [DispatchEdge].
//
// Exactly one of Advance, Stay, or Escalate is true after a successful call.
type DispatchOutcome struct {
	// Advance is true when the cascade selected an edge and the gate allowed it.
	// Edge carries the chosen transition target.
	Advance bool
	Edge    Edge

	// Stay is true when the gate denied the chosen transition. The run MUST
	// remain in the source state and enter a gate-pending sub-state per
	// execution-model.md §4.10.EM-042a. No checkpoint is written.
	Stay bool

	// Escalate is true when the gate escalated the chosen transition to a human.
	// The run enters quarantine per control-points.md §6.2.
	Escalate bool

	// Failed is true when the cascade produced a structural or compilation-loop
	// failure (forwarded from [CascadeResult]).
	Failed        bool
	FailureClass  FailureClass
	FailureReason string
}

// DispatchEdge runs the full EM-042 dispatch pipeline:
//
//  1. Apply outcome.ContextUpdates to run.Context (EM-041a) so that the guard
//     observes post-update run state, as required by CP-018 and the pseudocode
//     of execution-model.md §7.3 (apply_context_updates precedes apply_guards).
//  2. Apply guards to reorder candidates (EM-042, control-points.md §6.4).
//  3. Run the EM-041 deterministic cascade via [SelectNextEdge], which
//     re-applies context updates idempotently (same values, safe).
//  4. Apply the gate to the chosen edge (EM-042, control-points.md §6.2).
//
// Guards reorder the candidate edge list before the cascade; gates evaluate
// the cascade's chosen edge after it. Neither guards nor gates may add, remove,
// or block edges — only reorder (guards) or permit/deny/escalate (gates).
//
// # Parameters
//
//   - run: the current run. Mutated in-place by the EM-041a context-update step.
//     Must not be nil.
//   - candidates: outgoing edges from the current node. May be empty.
//   - outcome: the handler-produced outcome driving selection.
//   - eval: condition evaluator passed through to [SelectNextEdge]. Must not be nil.
//   - cycles: the run's in-memory CycleCounter. Must not be nil.
//   - guard: guard evaluator applied before the cascade. Pass [IdentityGuard]
//     when no guard is configured.
//   - gate: gate evaluator applied after the cascade. Pass [PermitGate] when
//     no gate is configured.
//
// # Invariant
//
// DispatchEdge panics if the guard returns a slice whose length differs from
// the input candidates slice. This enforces the EM-042 invariant that guards
// MUST NOT add or remove edges.
func DispatchEdge(
	run *Run,
	candidates []Edge,
	outcome Outcome,
	eval ConditionEvaluator,
	cycles *CycleCounter,
	guard GuardEvaluator,
	gate GateEvaluator,
) DispatchOutcome {
	// §4.10.EM-041a / CP-018 — apply context updates BEFORE guard fires so the
	// guard observes post-update run state per the execution-model.md §7.3
	// pseudocode ordering (apply_context_updates precedes apply_guards).
	// SelectNextEdge re-applies idempotently; same values, safe.
	ApplyContextUpdates(run, outcome.ContextUpdates)

	// §4.10.EM-042 — apply guard reordering before the cascade.
	reordered := guard(run, candidates, outcome)
	if len(reordered) != len(candidates) {
		panic("edgecascade: guard violated EM-042 invariant: returned slice length differs from input")
	}

	// §4.10.EM-041 — run the deterministic cascade on the (possibly reordered) candidates.
	cascadeResult := SelectNextEdge(run, reordered, outcome, eval, cycles)
	if cascadeResult.Failed {
		return DispatchOutcome{
			Failed:        true,
			FailureClass:  cascadeResult.FailureClass,
			FailureReason: cascadeResult.FailureReason,
		}
	}

	// §4.10.EM-042 — apply gate to the chosen edge.
	action := gate(run, cascadeResult.Edge, outcome)
	switch action {
	case GateActionAllow:
		return DispatchOutcome{Advance: true, Edge: cascadeResult.Edge}
	case GateActionDeny:
		// §4.10.EM-042a — gate denial; run stays in source state, gate-pending.
		return DispatchOutcome{Stay: true}
	case GateActionEscalateToHuman:
		return DispatchOutcome{Escalate: true}
	default:
		// Unknown GateAction: treat as structural failure per GateAction contract
		// ("A reader observing an unknown GateAction MUST reject the enclosing record").
		return DispatchOutcome{
			Failed:        true,
			FailureClass:  FailureClassStructural,
			FailureReason: "unknown gate action: " + string(action),
		}
	}
}
