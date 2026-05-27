package core

import "fmt"

// Kind is the discriminator for the ControlPoint payload envelope
// (control-points.md §6.1 ENUM Kind, §4.1.CP-001, v0.3.2).
// The enum is closed at MVH; future variants extend via the amendment
// protocol per [architecture.md §4.6].
//
// Kind parameterizes a ControlPoint into one of four surfaces: Gate, Hook,
// Guard, and Budget. Each Kind has its own trigger, evaluator input/output,
// outcome-action enum, and boundary-classification rule per §4.1.CP-005.
//
// A reader observing an unknown Kind MUST reject the ControlPoint at registration
// per [control-points.md §4.9]; no silent fallback is permitted.
type Kind string

// Kind values per control-points.md §6.1 ENUM Kind (v0.3.2).
const (
	// KindGate is a ControlPoint that fires on a transition attempt and returns
	// allow, deny, or escalate-to-human. Evaluator may be mechanism or cognition
	// per §4.1.CP-005.
	KindGate Kind = "Gate"

	// KindHook is a ControlPoint that fires on event match and produces
	// side-effects; a Hook never halts a run. Evaluator may be mechanism or
	// cognition per §4.1.CP-005.
	KindHook Kind = "Hook"

	// KindGuard is a ControlPoint that fires during edge evaluation and reorders
	// the candidate edge list. Guards may only reorder — not add, remove, or
	// block edges. Evaluator MUST be mechanism-tagged per §4.1.CP-005.
	KindGuard Kind = "Guard"

	// KindBudget is a ControlPoint that declares a consumable allowance (tokens,
	// wall-clock, iterations) and fires on accrual, warning-threshold, and
	// exhaustion. Evaluator MUST be mechanism-tagged per §4.1.CP-005.
	KindBudget Kind = "Budget"
)

// Valid reports whether k is one of the four declared Kind constants at MVH.
// Unknown values are NOT tolerated — a reader observing an unknown Kind MUST
// reject the ControlPoint at registration per [control-points.md §4.9].
func (k Kind) Valid() bool {
	switch k {
	case KindGate, KindHook, KindGuard, KindBudget:
		return true
	default:
		return false
	}
}

// AllowsCognition reports whether a ControlPoint of this Kind may carry a
// cognition-tagged evaluator per the §4.1.CP-005 boundary-classification table.
//
// Gate and Hook may be mechanism OR cognition per CP-011 and CP-017.
// Guard and Budget MUST be mechanism-tagged (CP-020; Budget boundary rule from
// the §4.1 table). A cognition-tagged evaluator on a Guard or Budget MUST fail
// registration at the §7.1 registration sequence.
//
// AllowsCognition returns false for unknown Kind values; callers should check
// Kind.Valid() first to distinguish "mechanism-only" from "invalid Kind".
func (k Kind) AllowsCognition() bool {
	switch k {
	case KindGate, KindHook:
		return true
	case KindGuard, KindBudget:
		return false
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so Kind serialises correctly
// in JSON and YAML policy documents (control-points.md §6.3).
// It rejects any value that is not one of the four declared constants at MVH.
func (k Kind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("kind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants at MVH.
// Per control-points.md §4.9, unknown Kind values must be rejected at registration;
// callers MUST NOT silently degrade to a default kind.
func (k *Kind) UnmarshalText(text []byte) error {
	v := Kind(text)
	if !v.Valid() {
		return fmt.Errorf(
			"kind: unknown value %q; must be one of Gate, Hook, Guard, Budget",
			string(text),
		)
	}
	*k = v
	return nil
}
