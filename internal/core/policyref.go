package core

import "fmt"

// PolicyRef is the typed string alias for the policy_ref field of a workflow
// Node (execution-model.md §6.1 RECORD Node field policy_ref; authoritative
// definition at control-points.md §6.4).
//
// A PolicyRef value names a policy element registered in the control-point
// registry per [control-points.md §4.9]. DOT node attributes MUST NOT embed
// policy bodies inline per [control-points.md §4.3.CP-044]; they carry only
// the registered name.
//
// The spec declares policy_ref as a non-empty String with no closed enum and
// no mandatory regex shape at MVH; validation requires only non-empty.
//
// Note: the bead title (hk-b3f.99) cites §6.3, but execution-model.md §6.1
// authoritatively sources PolicyRef from [control-points.md §6.4]. §6.4 wins
// per implementer-protocol.md §Path-discrepancy resolution (spec wins for
// spec content; bead body citation deferred to follow-up if needed).
type PolicyRef string

// Valid reports whether p is a non-empty PolicyRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (p PolicyRef) Valid() bool {
	return p != ""
}

// MarshalText implements encoding.TextMarshaler so PolicyRef serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (p PolicyRef) MarshalText() ([]byte, error) {
	if !p.Valid() {
		return nil, fmt.Errorf("policyref: value must not be empty")
	}
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (p *PolicyRef) UnmarshalText(text []byte) error {
	v := PolicyRef(text)
	if !v.Valid() {
		return fmt.Errorf("policyref: value must not be empty")
	}
	*p = v
	return nil
}
