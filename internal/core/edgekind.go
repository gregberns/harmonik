package core

import "fmt"

// EdgeKind is the kind of a dependency edge between beads (beads-integration.md §6.1).
// One of: parent-child, blocks, conditional-blocks, waits-for.
// Validators reject any other value.
type EdgeKind string

// EdgeKind values per beads-integration.md §6.1 ENUM declaration.
const (
	EdgeKindParentChild       EdgeKind = "parent-child"
	EdgeKindBlocks            EdgeKind = "blocks"
	EdgeKindConditionalBlocks EdgeKind = "conditional-blocks"
	EdgeKindWaitsFor          EdgeKind = "waits-for"
)

// Valid reports whether e is one of the four declared EdgeKind constants.
func (e EdgeKind) Valid() bool {
	switch e {
	case EdgeKindParentChild, EdgeKindBlocks, EdgeKindConditionalBlocks, EdgeKindWaitsFor:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so EdgeKind serialises
// correctly in JSON and YAML workflow definitions.
func (e EdgeKind) MarshalText() ([]byte, error) {
	if !e.Valid() {
		return nil, fmt.Errorf("edgekind: unknown value %q", string(e))
	}
	return []byte(e), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants.
func (e *EdgeKind) UnmarshalText(text []byte) error {
	v := EdgeKind(text)
	if !v.Valid() {
		return fmt.Errorf("edgekind: unknown value %q; must be one of parent-child, blocks, conditional-blocks, waits-for", string(text))
	}
	*e = v
	return nil
}
