package dot

// edges.go — Edge-condition evaluator per specs/workflow-graph.md §6.
//
// EvalCondition evaluates a *Condition against a core.Outcome and a run
// context map, returning (matched, err).  The evaluator is the runtime
// complement of the parse-time LHS whitelist check (WG-014): it enforces the
// same whitelist at evaluation time as defense-in-depth (WG-014: "A loader
// MUST reject a graph that declares an edge condition with an LHS outside this
// whitelist … the rejection is an ingest-time error"), but returns
// ErrDeterministic rather than panicking when it encounters an out-of-whitelist
// LHS that somehow slipped past the validator.
//
// Spec refs:
//   - specs/workflow-graph.md §6 WG-013  — restricted equality dialect; &&-conjunction.
//   - specs/workflow-graph.md §6 WG-014  — LHS whitelist.
//   - specs/workflow-graph.md §6 WG-015  — RHS literal types.
//   - specs/workflow-graph.md §6 WG-016  — dialect is distinct from guard predicates.
//
// Tags: mechanism, normative

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrDeterministic is returned by EvalCondition when the condition contains an
// out-of-whitelist LHS.  The sentinel wraps the same handler-contract
// deterministic class so callers can use errors.Is to route it.
//
// Defense-in-depth per WG-014: the validator MUST have caught this at load
// time; if we reach here the graph was not properly validated.
var ErrDeterministic = errors.New("dot: deterministic condition evaluation error")

// lhsWhitelist is the closed set of valid LHS prefixes per WG-014.
// "outcome.<field>" and "context.<key>" are the two namespaces.
const (
	lhsOutcomeStatus         = "outcome.status"
	lhsOutcomePreferredLabel = "outcome.preferred_label"
	lhsOutcomeFailureClass   = "outcome.failure_class"
	lhsOutcomeKind           = "outcome.kind"
	lhsContextPrefix         = "context."
)

// EvalCondition evaluates cond against outcome and ctx.
//
//   - A nil condition (unconditional edge) always returns (true, nil).
//   - All clauses in the conjunction must match for the condition to be true.
//   - Returns (false, ErrDeterministic) when any clause has an out-of-whitelist LHS.
//   - Returns (false, nil) when the condition is false with no error.
//   - Returns (true, nil) when the condition is true.
//
// The function is deterministic: equal (cond, outcome, ctx) inputs always
// produce the same output.
func EvalCondition(cond *Condition, outcome core.Outcome, ctx map[string]string) (bool, error) {
	if cond == nil {
		return true, nil
	}
	for _, eq := range cond.Clauses {
		matched, err := evalEquality(eq, outcome, ctx)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// evalEquality evaluates one Equality clause.
func evalEquality(eq Equality, outcome core.Outcome, ctx map[string]string) (bool, error) {
	lhsVal, err := resolveLHS(eq.LHS, outcome, ctx)
	if err != nil {
		return false, err
	}
	switch eq.Op {
	case "==":
		return lhsVal == eq.RHS, nil
	case "!=":
		return lhsVal != eq.RHS, nil
	default:
		return false, fmt.Errorf("%w: unsupported operator %q on LHS %q", ErrDeterministic, eq.Op, eq.LHS)
	}
}

// resolveLHS extracts the runtime value for the given LHS expression.
// Returns ErrDeterministic for any LHS outside the WG-014 whitelist.
func resolveLHS(lhs string, outcome core.Outcome, ctx map[string]string) (string, error) {
	switch lhs {
	case lhsOutcomeStatus:
		return string(outcome.Status), nil

	case lhsOutcomePreferredLabel:
		if outcome.PreferredLabel == nil {
			return "", nil
		}
		return *outcome.PreferredLabel, nil

	case lhsOutcomeFailureClass:
		if outcome.FailureClass == nil {
			return "", nil
		}
		return string(*outcome.FailureClass), nil

	case lhsOutcomeKind:
		return string(outcome.Kind), nil

	default:
		if strings.HasPrefix(lhs, lhsContextPrefix) {
			key := lhs[len(lhsContextPrefix):]
			if key == "" {
				return "", fmt.Errorf("%w: context key must be non-empty in LHS %q (WG-014)", ErrDeterministic, lhs)
			}
			return ctx[key], nil
		}
		return "", fmt.Errorf("%w: LHS %q is not in the WG-014 whitelist (outcome.status, outcome.preferred_label, outcome.failure_class, outcome.kind, context.<key>)", ErrDeterministic, lhs)
	}
}
