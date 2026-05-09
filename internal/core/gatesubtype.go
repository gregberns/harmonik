package core

import "fmt"

// GateSubtype is the sub-discriminator for Gate ControlPoints that declares
// the operator intent of the gate (control-points.md §6.1.1 ENUM GateSubtype,
// §4.1.CP-008, v0.3.2).
//
// Subtype is informative for operators; enforcement uses the evaluator and
// outcome-action fields only (§4.1.CP-008). Operators may use the subtype
// to understand the intent of a gate without consulting the evaluator
// expression.
//
// GateSubtype is a sub-discriminator under [KindGate]: it only appears inside
// a GatePayload record (§6.1.1) and MUST NOT be interpreted outside that
// context.
type GateSubtype string

// GateSubtype values per control-points.md §6.1.1 ENUM GateSubtype (v0.3.2).
const (
	// GateSubtypeGoal is a gate that expresses a policy-level goal assertion
	// that cannot be bypassed by the run. The evaluator must return allow
	// for the transition to proceed (§4.1.CP-008).
	GateSubtypeGoal GateSubtype = "goal-gate"

	// GateSubtypeApproval is a gate that requires a named approver (human role
	// or agent role per architecture.md §4.8). The named_approver field of the
	// GatePayload record MUST be set when subtype = approval-gate (§6.1.1).
	GateSubtypeApproval GateSubtype = "approval-gate"

	// GateSubtypeQuality is a gate that requires a prior verification node's
	// outcome to satisfy a policy expression. The verification_ref field of the
	// GatePayload record MUST be set when subtype = quality-gate (§6.1.1).
	GateSubtypeQuality GateSubtype = "quality-gate"
)

// Valid reports whether s is one of the three declared GateSubtype constants.
// Unknown values are not tolerated; callers must reject any GatePayload
// carrying an invalid subtype at registration per [control-points.md §4.9].
func (s GateSubtype) Valid() bool {
	switch s {
	case GateSubtypeGoal, GateSubtypeApproval, GateSubtypeQuality:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so GateSubtype serialises
// correctly in JSON and YAML policy documents (control-points.md §6.3).
// It rejects any value that is not one of the three declared constants.
func (s GateSubtype) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("gatesubtype: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Per control-points.md §4.9, unknown values must be rejected at registration;
// callers MUST NOT silently degrade to a default subtype.
func (s *GateSubtype) UnmarshalText(text []byte) error {
	v := GateSubtype(text)
	if !v.Valid() {
		return fmt.Errorf(
			"gatesubtype: unknown value %q; must be one of goal-gate, approval-gate, quality-gate",
			string(text),
		)
	}
	*s = v
	return nil
}
