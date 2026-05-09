package core

import "fmt"

// GateAction is the discriminator for Gate verdict interpretation
// (specs/control-points.md §6.1.6 ENUM GateAction).
//
// The three values determine how the S01 invocation layer responds to a Gate
// evaluator's outcome per CP-006 + CP-009:
//   - GateActionAllow: transition proceeds normally
//   - GateActionDeny: run remains in source state; transition does NOT advance;
//     a gate_denied event is emitted per event-model.md §8.2
//   - GateActionEscalateToHuman: run enters quarantine awaiting external
//     resolution; a gate_escalated event is emitted per event-model.md §8.2
//
// A reader observing an unknown GateAction MUST reject the enclosing
// GateVerdictRecord; no silent fallback is permitted.
type GateAction string

// GateAction values per specs/control-points.md §6.1.6 ENUM GateAction.
const (
	// GateActionAllow indicates the Gate permits the transition to proceed.
	GateActionAllow GateAction = "allow"

	// GateActionDeny indicates the Gate blocks the transition; the run MUST
	// remain in the source state per specs/control-points.md §4.2.CP-009.
	GateActionDeny GateAction = "deny"

	// GateActionEscalateToHuman indicates the Gate escalates to external
	// human resolution; the run enters quarantine per
	// specs/control-points.md §4.2.CP-009.
	GateActionEscalateToHuman GateAction = "escalate-to-human"
)

// Valid reports whether a is one of the three declared GateAction constants.
// Unknown values are NOT tolerated — a reader observing an unknown GateAction
// MUST reject the enclosing record per specs/control-points.md §6.1.6.
func (a GateAction) Valid() bool {
	switch a {
	case GateActionAllow, GateActionDeny, GateActionEscalateToHuman:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so GateAction serialises
// correctly in JSON and YAML policy documents (specs/control-points.md §6.3).
// It rejects any value that is not one of the three declared constants.
func (a GateAction) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("gateaction: unknown value %q", string(a))
	}
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Per specs/control-points.md §6.1.6, unknown GateAction values must be
// rejected; callers MUST NOT silently degrade to a default action.
func (a *GateAction) UnmarshalText(text []byte) error {
	v := GateAction(text)
	if !v.Valid() {
		return fmt.Errorf(
			"gateaction: unknown value %q; must be one of allow, deny, escalate-to-human",
			string(text),
		)
	}
	*a = v
	return nil
}
