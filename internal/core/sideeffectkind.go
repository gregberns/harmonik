package core

import "fmt"

// SideEffectKind is the discriminator for SideEffect.target interpretation
// (specs/control-points.md §6.1.2 ENUM SideEffectKind).
//
// The three values determine how the S05 dispatcher interprets the opaque
// SideEffect.target string:
//   - SideEffectKindEmitEvent: target is an event name to emit
//   - SideEffectKindStateMutation: target is a typed state key to write
//   - SideEffectKindExternalAction: target is a handler-owned effector id
//     registered per handler-contract.md §4.11
//
// A reader observing an unknown SideEffectKind MUST reject the enclosing
// HookVerdictRecord; no silent fallback is permitted.
type SideEffectKind string

// SideEffectKind values per specs/control-points.md §6.1.2 ENUM SideEffectKind.
const (
	// SideEffectKindEmitEvent indicates the side effect emits an event;
	// SideEffect.target is the event name.
	SideEffectKindEmitEvent SideEffectKind = "emit-event"

	// SideEffectKindStateMutation indicates the side effect writes a state key;
	// SideEffect.target is interpreted as a typed state key by the S05
	// dispatcher per specs/control-points.md §4.3.CP-016.
	SideEffectKindStateMutation SideEffectKind = "state-mutation"

	// SideEffectKindExternalAction indicates the side effect invokes an
	// external effector; SideEffect.target is the effector id registered
	// per handler-contract.md §4.11.
	SideEffectKindExternalAction SideEffectKind = "external-action"
)

// Valid reports whether k is one of the three declared SideEffectKind constants.
// Unknown values are NOT tolerated — a reader observing an unknown SideEffectKind
// MUST reject the enclosing record per specs/control-points.md §6.1.2.
func (k SideEffectKind) Valid() bool {
	switch k {
	case SideEffectKindEmitEvent, SideEffectKindStateMutation, SideEffectKindExternalAction:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so SideEffectKind serialises
// correctly in JSON and YAML policy documents (specs/control-points.md §6.3).
// It rejects any value that is not one of the three declared constants.
func (k SideEffectKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("sideeffectkind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Per specs/control-points.md §6.1.2, unknown SideEffectKind values must be
// rejected; callers MUST NOT silently degrade to a default kind.
func (k *SideEffectKind) UnmarshalText(text []byte) error {
	v := SideEffectKind(text)
	if !v.Valid() {
		return fmt.Errorf(
			"sideeffectkind: unknown value %q; must be one of emit-event, state-mutation, external-action",
			string(text),
		)
	}
	*k = v
	return nil
}
