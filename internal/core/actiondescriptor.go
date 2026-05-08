package core

import "fmt"

// ActionDescriptor is the typed descriptor of a handler-considered or
// handler-chosen action, carried in the Trace/Transition record
// (architecture.md §4.3.AR-012; execution-model.md §6.1 RECORD Transition
// fields candidate_actions and chosen_action).
//
// The spec cites [handler-contract.md §4.1] as the defining location for
// ActionDescriptor; that spec does not yet declare a structured record shape
// at MVH (see execution-model.md OQ-EM-005). MVH realises ActionDescriptor as
// a typed non-empty string alias following the same pattern as PolicyVersion.
// A future handler-contract revision may promote this to a structured record
// via the amendment protocol per [architecture.md §4.6].
type ActionDescriptor string

// Valid reports whether a is a non-empty ActionDescriptor string.
// Empty values are rejected; all non-empty strings are accepted.
func (a ActionDescriptor) Valid() bool {
	return a != ""
}

// MarshalText implements encoding.TextMarshaler so ActionDescriptor serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (a ActionDescriptor) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("actiondescriptor: value must not be empty")
	}
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (a *ActionDescriptor) UnmarshalText(text []byte) error {
	v := ActionDescriptor(text)
	if !v.Valid() {
		return fmt.Errorf("actiondescriptor: value must not be empty")
	}
	*a = v
	return nil
}
