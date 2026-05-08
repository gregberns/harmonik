package core

import "fmt"

// GateRef is the typed string alias for the gate_ref field of a workflow Node
// (execution-model.md §6.1 RECORD Node field gate_ref; authoritative definition
// at control-points.md §6.2).
//
// A GateRef value names a gate element declared in a policy YAML document and
// registered in the control-point registry per [control-points.md §4.9]. DOT
// node attributes MUST NOT embed gate bodies inline per
// [control-points.md §4.3.CP-044]; they carry only the registered name.
//
// The spec declares gate_ref as a non-empty String with no closed enum and no
// mandatory regex shape at MVH; validation requires only non-empty.
type GateRef string

// Valid reports whether g is a non-empty GateRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (g GateRef) Valid() bool {
	return g != ""
}

// MarshalText implements encoding.TextMarshaler so GateRef serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (g GateRef) MarshalText() ([]byte, error) {
	if !g.Valid() {
		return nil, fmt.Errorf("gateref: value must not be empty")
	}
	return []byte(g), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (g *GateRef) UnmarshalText(text []byte) error {
	v := GateRef(text)
	if !v.Valid() {
		return fmt.Errorf("gateref: value must not be empty")
	}
	*g = v
	return nil
}
