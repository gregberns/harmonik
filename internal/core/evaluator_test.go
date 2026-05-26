package core

import "testing"

// TestEvaluatorValid_UnknownMode verifies that an Evaluator with an
// unrecognised ModeTag is invalid (specs/control-points.md §6.1 RECORD
// Evaluator; architecture.md §4.2 AR-005 — unknown values MUST be rejected).
func TestEvaluatorValid_UnknownMode(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	e := Evaluator{Mode: ModeTag("unknown"), Expression: &expr}
	if e.Valid() {
		t.Error("Evaluator with unknown ModeTag.Valid() = true, want false")
	}
}

// TestEvaluatorValid_EmptyMode verifies that an Evaluator with an empty
// ModeTag is invalid (architecture.md §4.2 AR-005).
func TestEvaluatorValid_EmptyMode(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	e := Evaluator{Mode: ModeTag(""), Expression: &expr}
	if e.Valid() {
		t.Error("Evaluator with empty ModeTag.Valid() = true, want false")
	}
}

// TestEvaluatorValid_CognitionWithExpression verifies that a cognition-tagged
// Evaluator that also carries an Expression is invalid — the two fields are
// mutually exclusive (specs/control-points.md §6.1 RECORD Evaluator).
func TestEvaluatorValid_CognitionWithExpression(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	e := Evaluator{
		Mode: ModeTagCognition,
		DelegationPath: &DelegationPath{
			Role:              "reviewer",
			ModelClass:        "reviewer-tier-1",
			InputSchemaRef:    "gate-input-v1",
			ResponseSchemaRef: "gate-response-v1",
			PromptTemplateRef: "gate-prompt-v1",
		},
		Expression: &expr,
	}
	if e.Valid() {
		t.Error("cognition Evaluator with Expression.Valid() = true, want false")
	}
}

// TestEvaluatorValid_MechanismEmptyExpression verifies that a mechanism-tagged
// Evaluator whose Expression is an empty string is invalid
// (specs/control-points.md §6.1; PolicyExpression.Valid() rejects empty
// strings per policyexpression.go).
func TestEvaluatorValid_MechanismEmptyExpression(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("")
	e := Evaluator{Mode: ModeTagMechanism, Expression: &expr}
	if e.Valid() {
		t.Error("mechanism Evaluator with empty Expression.Valid() = true, want false")
	}
}
