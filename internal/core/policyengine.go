package core

// PolicyVerdict is the result of a PolicyEngine evaluation.
//
// At MVH the verdict is always Permitted with no constraints
// (NoOpPolicyEngine). When the control-points subsystem lands post-MVH, the
// real evaluator replaces NoOpPolicyEngine at the composition root; the
// orchestrator dispatcher remains unchanged.
//
// Spec ref: specs/control-points.md (fully deferred per bootstrap-subset.md §1;
// 0 of 85 CP beads are in the bootstrap subset). See also
// docs/foundation/phase-1-readiness-gap-analysis.md §A5.
type PolicyVerdict struct {
	// Permitted is true when the policy evaluation grants the requested action.
	// NoOpPolicyEngine always returns true.
	Permitted bool

	// Constraints carries any policy-imposed constraints on the permitted action.
	// NoOpPolicyEngine always returns nil (no constraints).
	//
	// TODO(hk-a8bg): replace with a typed ConstraintSet per
	// specs/control-points.md §4.2 when the CP subsystem lands post-MVH.
	Constraints map[string]any
}

// PolicyEngine is the interface the EM dispatcher calls for every gate and
// guard evaluation along the outcome spine
// (specs/execution-model.md §4.6, §4.10.EM-042).
//
// The dispatcher always calls Evaluate; it MUST NOT branch on whether the
// engine is a no-op or a real evaluator. This invariant prevents
// test-mode branches in production code per specs/scenario-harness.md
// §4.3.SH-018.
//
// At MVH the composition root wires NoOpPolicyEngine, which always returns
// {Permitted: true, Constraints: nil}. When the control-points subsystem
// (hk-a8bg) lands post-MVH, the composition root is the only change site.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5;
// specs/control-points.md §4.2 (Gate), §4.4 (Guard); bootstrap-subset.md §1
// (CP fully deferred).
type PolicyEngine interface {
	// Evaluate assesses the transition identified by ctx against the
	// registered policies and returns a verdict.
	//
	// ctx carries the run context needed for expression evaluation per
	// specs/control-points.md §6.4 (PolicyExpression environment).
	//
	// TODO(hk-a8bg): widen PolicyEvalContext to the full CP §6.4 environment
	// record once the CP subsystem lands.
	Evaluate(ctx PolicyEvalContext) PolicyVerdict
}

// PolicyEvalContext carries the run-scoped inputs the PolicyEngine evaluates
// against.
//
// At MVH this is an empty record because NoOpPolicyEngine ignores all inputs.
// When the CP subsystem (hk-a8bg) lands, this expands to the full
// specs/control-points.md §6.4 environment (run, outcome, event, context,
// policy_meta).
//
// TODO(hk-a8bg): expand to the full CP §6.4 environment record post-MVH.
//
// Spec ref: specs/control-points.md §6.4 (PolicyExpression evaluation
// environment); docs/foundation/phase-1-readiness-gap-analysis.md §A5.
type PolicyEvalContext struct{}

// NoOpPolicyEngine is the production PolicyEngine binding for MVH.
//
// It always returns {Permitted: true, Constraints: nil} — "permitted, no
// constraints." It is a first-class production value, NOT a test double or a
// nil sentinel. The orchestrator dispatcher calls Evaluate on every gate and
// guard without branching on the engine's concrete type, satisfying SH-018.
//
// Wiring: the composition root (cmd/harmonik/main.go) constructs a
// NoOpPolicyEngine and supplies it to the EM dispatcher as a PolicyEngine
// interface value. When the CP subsystem lands post-MVH, the composition root
// substitutes the real evaluator; no dispatcher changes are required.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5;
// specs/scenario-harness.md §4.3.SH-018; bootstrap-subset.md §1.
type NoOpPolicyEngine struct{}

// Evaluate implements PolicyEngine. It unconditionally returns
// {Permitted: true, Constraints: nil}.
func (NoOpPolicyEngine) Evaluate(_ PolicyEvalContext) PolicyVerdict {
	return PolicyVerdict{Permitted: true, Constraints: nil}
}
