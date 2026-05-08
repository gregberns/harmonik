package core

import "fmt"

// PolicyVersion is the typed string alias for the policy_version field of a
// Trace/Transition record (architecture.md §4.3.AR-012;
// execution-model.md §6.1 RECORD Transition, field policy_version).
//
// The value identifies the policy snapshot under which an actor's decision
// was made. The spec declares policy_version as a String with no closed enum
// and no mandatory regex shape; MVH validation requires only non-empty.
//
// Future revisions may introduce a regex constraint via the amendment protocol
// per [architecture.md §4.6].
type PolicyVersion string

// Valid reports whether v is a non-empty PolicyVersion string.
// Empty values are rejected; all non-empty strings are accepted.
func (v PolicyVersion) Valid() bool {
	return v != ""
}

// MarshalText implements encoding.TextMarshaler so PolicyVersion serialises
// correctly in JSON and YAML.
// It rejects any empty value.
func (v PolicyVersion) MarshalText() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("policyversion: value must not be empty")
	}
	return []byte(v), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (v *PolicyVersion) UnmarshalText(text []byte) error {
	val := PolicyVersion(text)
	if !val.Valid() {
		return fmt.Errorf("policyversion: value must not be empty")
	}
	*v = val
	return nil
}
