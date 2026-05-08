package core

import "fmt"

// FreedomProfileRef is the typed reference for the Node.freedom_profile_ref
// field (execution-model.md §6.1 RECORD Node; control-points.md §4.6).
//
// The value names a FreedomProfile registered in the policy-layer registry
// per [control-points.md §4.6.CP-033]. Policy YAML documents reference
// FreedomProfiles by name; the DOT node attribute freedom_profile_ref
// resolves to a registered name per [control-points.md §4.9].
//
// The spec declares freedom_profile_ref as String | None at §6.1 of
// execution-model.md. None is represented in Go as *FreedomProfileRef at
// the call site; FreedomProfileRef itself must always be non-empty.
//
// control-points.md §4.6 does not declare a structured record shape for the
// reference value at MVH; MVH realises FreedomProfileRef as a typed non-empty
// string alias following the same pattern as PolicyVersion. A future
// control-points.md revision may promote this to a structured record via the
// amendment protocol per [architecture.md §4.6].
type FreedomProfileRef string

// Valid reports whether r is a non-empty FreedomProfileRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (r FreedomProfileRef) Valid() bool {
	return r != ""
}

// MarshalText implements encoding.TextMarshaler so FreedomProfileRef
// serialises correctly in JSON and YAML.
// It rejects empty values.
func (r FreedomProfileRef) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("freedomprofileref: value must not be empty")
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (r *FreedomProfileRef) UnmarshalText(text []byte) error {
	v := FreedomProfileRef(text)
	if !v.Valid() {
		return fmt.Errorf("freedomprofileref: value must not be empty")
	}
	*r = v
	return nil
}
