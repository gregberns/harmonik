package core

import "fmt"

// ModeTag is the closed mechanism/cognition discriminator carried on every
// normative requirement and on every workflow Node
// (architecture.md §4.2 AR-005; execution-model.md §6.1 RECORD Node field
// mode_tag).
//
// The two values are mutually exclusive per [architecture.md §4.2]: a surface
// that is both mechanism- and cognition-tagged MUST be split into two
// requirements. The enum is closed at MVH; future variants require the
// amendment protocol per [architecture.md §4.6].
//
// A reader observing an unknown ModeTag MUST NOT silently default to either
// value; callers MUST reject unknown values or route to an error surface.
type ModeTag string

// ModeTag values per architecture.md §4.2 AR-005 / execution-model.md §6.1
// RECORD Node.mode_tag.
const (
	// ModeTagMechanism marks an evaluation point whose behavior is fully
	// specified by deterministic rules (I/O, schema/type checks, policy
	// enforcement by deterministic evaluator, state transitions, typed error
	// handling). See [architecture.md §4.2 AR-006].
	ModeTagMechanism ModeTag = "mechanism"

	// ModeTagCognition marks an evaluation point that requires semantic
	// judgment and delegates to an LLM under a named prompt and input shape
	// (ranking, scoring, plan composition, semantic analysis, quality judgment).
	// See [architecture.md §4.2 AR-007].
	ModeTagCognition ModeTag = "cognition"
)

// Valid reports whether m is one of the two declared ModeTag constants at MVH.
// Unknown values are NOT tolerated — a reader observing an unknown ModeTag
// MUST NOT silently default to either value per [architecture.md §4.2 AR-005].
func (m ModeTag) Valid() bool {
	switch m {
	case ModeTagMechanism, ModeTagCognition:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ModeTag serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the two declared constants at MVH.
func (m ModeTag) MarshalText() ([]byte, error) {
	if !m.Valid() {
		return nil, fmt.Errorf("modetag: unknown value %q", string(m))
	}
	return []byte(m), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the two declared constants at MVH.
// Callers MUST NOT silently degrade to a default on error.
func (m *ModeTag) UnmarshalText(text []byte) error {
	v := ModeTag(text)
	if !v.Valid() {
		return fmt.Errorf(
			"modetag: unknown value %q; must be one of mechanism, cognition",
			string(text),
		)
	}
	*m = v
	return nil
}
