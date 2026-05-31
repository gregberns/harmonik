package core

import "sort"

// cp021_guard_invocation_s01.go — S01 ownership of Guard invocation.
//
// Implements specs/control-points.md §4.4.CP-021:
//
//	CP-021 — Guard invocation is owned by S01
//	Guard invocation during the cascade MUST be performed by S01 (Orchestrator
//	Core) consulting the §4.9 registry. This spec defines the Guard record and
//	its outcome semantics; invocation is S01's obligation under
//	[execution-model.md §4.10].
//
// S01BuildGuardEvaluator is the entry point. It queries the registry for Guard
// ControlPoints that apply to a given node, sorts them by declaration order,
// and composes them into the single GuardEvaluator passed to DispatchEdge.
//
// Tags: mechanism
// Refs: hk-a8bg.20

// S01BuildGuardEvaluator builds a composite GuardEvaluator by consulting the
// §4.9 registry for all Guard ControlPoints that apply to nodeID, then
// composing their wired evaluator functions in declaration order.
//
// This function is the CP-021 obligation of S01 (Orchestrator Core).
// DispatchEdge callers MUST obtain the GuardEvaluator argument by calling this
// function rather than constructing a GuardEvaluator directly, so that all
// registered Guards scoped to the node are applied in the normative order.
//
// # Guard selection
//
// A Guard ControlPoint in the registry applies to nodeID when:
//   - Payload.Guard.AppliesToNode is nil — the Guard applies to all nodes, OR
//   - Payload.Guard.AppliesToNode is non-nil and equals nodeID.
//
// Guards whose Payload.Guard is nil (malformed entry) are skipped.
// Non-Guard ControlPoints (Kind ≠ KindGuard) are ignored.
//
// # Declaration order
//
// Applicable Guards are sorted by DeclarationIndex ascending before chaining.
// DeclarationIndex is assigned by the MapRegistry at registration time in
// insertion order (§4.9.CP-046), so this preserves the declaration order of
// the policy YAML that populated the registry.
//
// # Guard evaluator functions
//
// Guard policy expressions (mechanism-tagged PolicyExpression strings on the
// ControlPoint) are declarations, not runtime Go functions. The fns parameter
// maps registered guard names to their compiled GuardEvaluator functions, which
// are wired by the daemon's composition root at startup.
//
// A Guard whose name is absent from fns is skipped (no evaluator wired). If no
// applicable Guard has a wired evaluator, the returned evaluator is
// IdentityGuard.
//
// # Composite behaviour
//
// The returned evaluator chains the wired Guards in declaration order,
// threading the edge slice through each in turn: the first Guard receives the
// original candidate slice; each subsequent Guard receives the slice returned
// by the previous one.
//
// Spec ref: specs/control-points.md §4.4.CP-021, §4.9.CP-043, §4.9.CP-046.
func S01BuildGuardEvaluator(reg Registry, nodeID NodeID, fns map[string]GuardEvaluator) GuardEvaluator {
	var applicable []ControlPoint
	for _, cp := range reg.All() {
		if cp.Kind != KindGuard {
			continue
		}
		gp := cp.Payload.Guard
		if gp == nil {
			continue
		}
		// nil AppliesToNode means applies to all nodes.
		if gp.AppliesToNode == nil || *gp.AppliesToNode == nodeID {
			applicable = append(applicable, cp)
		}
	}
	if len(applicable) == 0 {
		return IdentityGuard
	}

	// Sort by DeclarationIndex ascending to honour declaration order (CP-046).
	sort.Slice(applicable, func(i, j int) bool {
		return applicable[i].DeclarationIndex < applicable[j].DeclarationIndex
	})

	// Collect wired evaluators in declaration order; skip guards with no fn.
	var chain []GuardEvaluator
	for _, cp := range applicable {
		fn, ok := fns[cp.Name]
		if !ok {
			continue
		}
		chain = append(chain, fn)
	}
	if len(chain) == 0 {
		return IdentityGuard
	}

	// Composite: thread the edge slice through the chain in declaration order.
	return func(run *Run, candidates []Edge, outcome Outcome) []Edge {
		edges := candidates
		for _, fn := range chain {
			edges = fn(run, edges, outcome)
		}
		return edges
	}
}
