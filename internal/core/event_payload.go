package core

// PayloadHasBeadID reports whether payload satisfies the BI-019 presence rule:
// the payload is non-nil and contains the key "bead_id" mapped to a non-empty
// string value.
//
// Per beads-integration.md §4 BI-019, every event emitted for a bead-bound run
// MUST carry "bead_id" on its payload. Events that are not scoped to a specific
// run (e.g., daemon lifecycle) MUST omit the field. This helper is the structural
// primitive that downstream emitters can use to assert the presence-or-absence
// rule at the data-shape level.
//
// Returns false when:
//   - payload is nil
//   - "bead_id" key is absent
//   - "bead_id" value is not a string
//   - "bead_id" value is an empty string
func PayloadHasBeadID(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	v, ok := payload["bead_id"]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return s != ""
}
