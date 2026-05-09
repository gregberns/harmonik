package core

import "testing"

// policyExpressionFixture returns a valid non-empty PolicyExpression for use
// in structural tests (hk-a8bg.92).
func policyExpressionFixture(t *testing.T) PolicyExpression {
	t.Helper()
	return PolicyExpression(`event.payload.status == "ready"`)
}

// TestPolicyExpressionValid_NonEmpty verifies that a non-empty PolicyExpression
// is valid (specs/control-points.md §6.4).
func TestPolicyExpressionValid_NonEmpty(t *testing.T) {
	t.Parallel()

	pe := policyExpressionFixture(t)
	if !pe.Valid() {
		t.Errorf("PolicyExpression(%q).Valid() = false, want true", pe)
	}
}

// TestPolicyExpressionValid_Empty verifies that an empty PolicyExpression is
// invalid. An empty string is not a valid expr-lang/expr expression in any
// ControlPoint Kind per specs/control-points.md §6.4.
func TestPolicyExpressionValid_Empty(t *testing.T) {
	t.Parallel()

	pe := PolicyExpression("")
	if pe.Valid() {
		t.Error("PolicyExpression(\"\").Valid() = true, want false")
	}
}

// TestPolicyExpressionValid_WhitespaceOnly verifies that a whitespace-only
// PolicyExpression is treated as valid at the type level. Full syntactic
// validation (which would reject whitespace-only inputs) is deferred to
// workflow-ingest per CP-035.
func TestPolicyExpressionValid_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	pe := PolicyExpression("   ")
	if !pe.Valid() {
		t.Error("PolicyExpression(whitespace).Valid() = false; type-level check is non-empty only; full parse is deferred to CP-035")
	}
}

// TestPolicyExpressionValid_SubscriptionFilterShape verifies a representative
// Hook subscription_filter expression form (specs/control-points.md §6.4.2).
func TestPolicyExpressionValid_SubscriptionFilterShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		expr PolicyExpression
		want bool
	}{
		{
			name: "event_field_predicate",
			expr: PolicyExpression(`event.payload.status == "ready"`),
			want: true,
		},
		{
			name: "run_context_lookup",
			expr: PolicyExpression(`run.context["phase"] == "review"`),
			want: true,
		},
		{
			name: "bool_literal_true",
			expr: PolicyExpression("true"),
			want: true,
		},
		{
			name: "empty",
			expr: PolicyExpression(""),
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.expr.Valid(); got != tc.want {
				t.Errorf("PolicyExpression(%q).Valid() = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}
