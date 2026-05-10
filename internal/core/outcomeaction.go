package core

import "fmt"

// OutcomeAction is the per-Kind declared action enum carried on a ControlPoint
// (specs/control-points.md §6.1 RECORD ControlPoint, field outcome_action).
//
// Each Kind defines its own outcome-action vocabulary:
//
//   - Gate:   OutcomeActionAllow, OutcomeActionDeny, OutcomeActionEscalateToHuman
//   - Hook:   OutcomeActionSideEffect (Hook never halts; outcome is always a side-effect descriptor)
//   - Guard:  OutcomeActionReorder (Guard may only reorder, never block)
//   - Budget: OutcomeActionAdmit, OutcomeActionWarn, OutcomeActionDeny
//
// OutcomeAction is a closed string enum at MVH. Unknown values are rejected at
// registration per [control-points.md §4.9].
//
// Per-Kind outcome-action semantics are declared in §4.2 (Gate), §4.3 (Hook),
// §4.4 (Guard), and §4.5 (Budget). The four vocabularies are disjoint; a valid
// OutcomeAction value must match the ControlPoint's Kind.
type OutcomeAction string

// OutcomeAction constants per control-points.md §4.2 (Gate), §4.3 (Hook),
// §4.4 (Guard), and §4.5 (Budget) (v0.3.2).
const (
	// OutcomeActionAllow is a Gate outcome: the evaluator permits the transition
	// (specs/control-points.md §4.2, §6.4.2 return conventions).
	OutcomeActionAllow OutcomeAction = "allow"

	// OutcomeActionDeny is a Gate or Budget outcome: the evaluator denies the
	// transition (Gate) or dispatch (Budget).
	// Gate: run stays in source state; gate_denied event emitted per §4.2.
	// Budget: handler is not launched; budget_exhausted event emitted per §4.5.
	OutcomeActionDeny OutcomeAction = "deny"

	// OutcomeActionEscalateToHuman is a Gate outcome: the deny is escalated to
	// a human approver (specs/control-points.md §4.2.CP-010). The run is paused
	// until the human resolves the escalation.
	OutcomeActionEscalateToHuman OutcomeAction = "escalate-to-human"

	// OutcomeActionSideEffect is the Hook outcome: the evaluator produced a
	// SideEffect descriptor (specs/control-points.md §4.3). Hooks never block;
	// this action is always produced on successful Hook evaluation.
	OutcomeActionSideEffect OutcomeAction = "side-effect"

	// OutcomeActionReorder is the Guard outcome: the evaluator reordered the
	// candidate edge list (specs/control-points.md §4.4). Guards may only
	// reorder — not add, remove, or block edges.
	OutcomeActionReorder OutcomeAction = "reorder"

	// OutcomeActionAdmit is a Budget outcome: the evaluator admits the pending
	// dispatch within the remaining allowance (specs/control-points.md §4.5).
	OutcomeActionAdmit OutcomeAction = "admit"

	// OutcomeActionWarn is a Budget outcome: the dispatch is admitted but the
	// accrual crossed the warning threshold (WarningThreshold × Limit);
	// a budget_warning event is emitted per §4.5.
	OutcomeActionWarn OutcomeAction = "warn"
)

// Valid reports whether a is one of the seven declared OutcomeAction constants.
// Unknown values are rejected at registration per [control-points.md §4.9].
func (a OutcomeAction) Valid() bool {
	switch a {
	case OutcomeActionAllow,
		OutcomeActionDeny,
		OutcomeActionEscalateToHuman,
		OutcomeActionSideEffect,
		OutcomeActionReorder,
		OutcomeActionAdmit,
		OutcomeActionWarn:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so OutcomeAction serialises
// correctly in JSON and YAML policy documents (control-points.md §6.3).
// It rejects any value that is not one of the declared constants.
func (a OutcomeAction) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("outcomeaction: unknown value %q", string(a))
	}
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the declared constants.
// Per control-points.md §4.9, unknown values must be rejected at registration.
func (a *OutcomeAction) UnmarshalText(text []byte) error {
	v := OutcomeAction(text)
	if !v.Valid() {
		return fmt.Errorf(
			"outcomeaction: unknown value %q; must be one of allow, deny, escalate-to-human, side-effect, reorder, admit, warn",
			string(text),
		)
	}
	*a = v
	return nil
}

// ValidForKind reports whether a is a legal OutcomeAction for the given Kind.
// Each Kind declares a disjoint outcome-action vocabulary per the §4.1.CP-005
// per-Kind table; mixing action values across Kinds is a registration error.
func (a OutcomeAction) ValidForKind(k Kind) bool {
	switch k {
	case KindGate:
		switch a {
		case OutcomeActionAllow, OutcomeActionDeny, OutcomeActionEscalateToHuman:
			return true
		}
	case KindHook:
		return a == OutcomeActionSideEffect
	case KindGuard:
		return a == OutcomeActionReorder
	case KindBudget:
		switch a {
		case OutcomeActionAdmit, OutcomeActionWarn, OutcomeActionDeny:
			return true
		}
	}
	return false
}
