package core

// PolicyExpression is a typed alias for a policy-expression string evaluated
// by the mechanism-tagged evaluator (specs/control-points.md §4.7.CP-034,
// §6.1, §6.4).
//
// # Adopted grammar
//
// PolicyExpression strings conform to the expr-lang/expr grammar
// (https://github.com/expr-lang/expr). The evaluator type-checks expressions
// at workflow-ingest time against the §6.4 environment (run, outcome, event,
// context, policy_meta). Expressions MUST be side-effect-free per CP-034.
//
// # Return-shape conventions
//
// The expected return type depends on the ControlPoint Kind in which the
// expression is embedded (specs/control-points.md §6.4.2):
//
//   - Hook (subscription_filter): Bool — predicate on the event payload.
//   - Hook (evaluator expression): SideEffect struct or Null.
//   - Gate (evaluator expression): Bool.
//   - Guard: Bool.
//
// # Valid() semantics
//
// Valid() only checks that the expression is non-empty. Full syntactic
// validation (AST parse + type-check against the §6.4 environment) requires
// the expr-lang/expr runtime and is performed at workflow-ingest time per
// §4.7.CP-035. Promotion of Valid() to a full parse-level check is deferred
// pending the workflow-ingest subsystem's availability.
type PolicyExpression string

// Valid reports whether pe is structurally non-empty.
//
// An empty PolicyExpression is not a valid expression in any ControlPoint
// Kind. Full syntactic validation (expr-lang/expr parse + type-check against
// the §6.4 environment) is deferred to workflow-ingest per CP-035 and is
// outside the scope of this type.
func (pe PolicyExpression) Valid() bool {
	return pe != ""
}
