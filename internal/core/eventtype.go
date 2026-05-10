package core

// EventType is the typed string identifier for an event type in the §8 taxonomy
// (event-model.md §8).
//
// The full set of enum values (~79 rows) is declared by bead hk-hqwn.59 and will
// be added in a separate commit. Until that commit lands, EventType is an open
// typed string: any non-empty string is structurally valid, and the per-type
// registry (eventregistry.go) enforces that only registered types are dispatched.
//
// Spec ref: specs/event-model.md §6.1 (Event.type), §8 (taxonomy table).
type EventType string

// Valid reports whether e is a non-empty EventType string.
// Registry-level validation (known vs unknown type) is enforced by EventRegistry.
func (e EventType) Valid() bool {
	return e != ""
}
