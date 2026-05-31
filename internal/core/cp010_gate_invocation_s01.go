package core

// cp010_gate_invocation_s01.go — S01 ownership of Gate invocation.
//
// Implements specs/control-points.md §4.2.CP-010:
//
//	CP-010 — Gate invocation is owned by S01
//	Gate invocation during the transition cascade MUST be performed by S01
//	(Orchestrator Core) consulting the §4.9 registry. This spec defines the
//	Gate record and its outcome semantics; it does NOT define the invocation
//	mechanics — those belong to the S01 subsystem spec and are constrained by
//	[execution-model.md §4.10].
//
// S01BuildGateEvaluator is the entry point. It queries the registry for Gate
// ControlPoints registered at a given AttachPoint, preserves their declaration
// order, and composes them into the single GateEvaluator passed to DispatchEdge.
//
// Tags: mechanism
// Refs: hk-a8bg.9

// S01BuildGateEvaluator builds a composite GateEvaluator by consulting the
// §4.9 registry for all Gate ControlPoints registered at attachPoint, then
// composing their wired evaluator functions in declaration order with
// short-circuit semantics on the first non-allow verdict.
//
// This function is the CP-010 obligation of S01 (Orchestrator Core).
// DispatchEdge callers MUST obtain the GateEvaluator argument by calling this
// function rather than constructing a GateEvaluator directly, so that all
// Gates registered at the attach point fire in the normative declaration order.
//
// # Gate selection
//
// The Registry's LookupByAttachPoint(attachPoint) returns all Gate-kind
// ControlPoints registered at attachPoint, already sorted by declaration order
// per §4.9.CP-007. Non-Gate ControlPoints are excluded by LookupByAttachPoint.
//
// # Declaration order and short-circuit
//
// Per CP-007: "When multiple Gates are registered at the same attach point,
// the S01 invocation layer MUST honor declaration order and MUST short-circuit
// on the first non-allow verdict." The composite evaluator applies wired Gates
// in declaration order and returns the first non-GateActionAllow verdict
// immediately, without evaluating subsequent Gates.
//
// # Gate evaluator functions
//
// Gate policy expressions (mechanism-tagged PolicyExpression strings on the
// ControlPoint) are declarations, not runtime Go functions. The fns parameter
// maps registered gate names to their compiled GateEvaluator functions, which
// are wired by the daemon's composition root at startup.
//
// A Gate whose name is absent from fns is skipped (no evaluator wired). If no
// applicable Gate has a wired evaluator, the returned evaluator is PermitGate.
//
// Spec ref: specs/control-points.md §4.2.CP-010, §4.2.CP-007, §4.9.CP-043,
// §4.9.CP-046.
func S01BuildGateEvaluator(reg Registry, attachPoint AttachPoint, fns map[string]GateEvaluator) GateEvaluator {
	// LookupByAttachPoint already returns only KindGate entries at this attach
	// point, sorted by declaration order (registration order) per CP-007.
	applicable := reg.LookupByAttachPoint(attachPoint)
	if len(applicable) == 0 {
		return PermitGate
	}

	// Collect wired evaluators in declaration order; skip gates with no fn.
	var chain []GateEvaluator
	for _, cp := range applicable {
		fn, ok := fns[cp.Name]
		if !ok {
			continue
		}
		chain = append(chain, fn)
	}
	if len(chain) == 0 {
		return PermitGate
	}

	// Composite: apply gates in declaration order, short-circuiting on the first
	// non-allow verdict per CP-007.
	return func(run *Run, chosen Edge, outcome Outcome) GateAction {
		for _, fn := range chain {
			action := fn(run, chosen, outcome)
			if action != GateActionAllow {
				return action
			}
		}
		return GateActionAllow
	}
}
