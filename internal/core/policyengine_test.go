package core

import "testing"

// noopPolicyFixture returns a NoOpPolicyEngine for use in tests that need a
// production-interface PolicyEngine value (hk-b3f.89).
func noopPolicyFixture(t *testing.T) PolicyEngine {
	t.Helper()
	return NoOpPolicyEngine{}
}

// TestNoOpPolicyEngine_ImplementsInterface verifies that NoOpPolicyEngine
// satisfies the PolicyEngine interface at compile time.
func TestNoOpPolicyEngine_ImplementsInterface(t *testing.T) {
	t.Parallel()

	// Compile-time assertion via interface conversion.
	var _ PolicyEngine = NoOpPolicyEngine{}
}

// TestNoOpPolicyEngine_EvaluatePermitted verifies that NoOpPolicyEngine.Evaluate
// always returns Permitted=true for any PolicyEvalContext.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5;
// bootstrap-subset.md §1 (CP fully deferred; no-op is the production binding).
func TestNoOpPolicyEngine_EvaluatePermitted(t *testing.T) {
	t.Parallel()

	eng := noopPolicyFixture(t)
	verdict := eng.Evaluate(PolicyEvalContext{})
	if !verdict.Permitted {
		t.Error("NoOpPolicyEngine.Evaluate().Permitted = false, want true")
	}
}

// TestNoOpPolicyEngine_EvaluateNoConstraints verifies that NoOpPolicyEngine.Evaluate
// always returns nil Constraints.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5.
func TestNoOpPolicyEngine_EvaluateNoConstraints(t *testing.T) {
	t.Parallel()

	eng := noopPolicyFixture(t)
	verdict := eng.Evaluate(PolicyEvalContext{})
	if verdict.Constraints != nil {
		t.Errorf("NoOpPolicyEngine.Evaluate().Constraints = %v, want nil", verdict.Constraints)
	}
}

// TestNoOpPolicyEngine_EvaluateViaInterface verifies that Evaluate is callable
// through the PolicyEngine interface value — confirming the composition root can
// hold a PolicyEngine and call through it without knowing the concrete type.
//
// This is the key invariant of SH-018: the dispatcher holds a PolicyEngine
// interface; it never branches on whether the engine is a no-op or real.
//
// Spec ref: specs/scenario-harness.md §4.3.SH-018; bootstrap-subset.md §1.
func TestNoOpPolicyEngine_EvaluateViaInterface(t *testing.T) {
	t.Parallel()

	// Simulate the composition root wiring.
	var eng PolicyEngine = NoOpPolicyEngine{}

	verdict := eng.Evaluate(PolicyEvalContext{})
	if !verdict.Permitted {
		t.Error("PolicyEngine(NoOpPolicyEngine).Evaluate().Permitted = false, want true")
	}
	if verdict.Constraints != nil {
		t.Errorf("PolicyEngine(NoOpPolicyEngine).Evaluate().Constraints = %v, want nil", verdict.Constraints)
	}
}
