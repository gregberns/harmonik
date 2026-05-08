package core

import "fmt"

// SubWorkflowRef is the typed reference for the Node.sub_workflow_ref field
// (execution-model.md §6.1 RECORD Node, line 641).
//
// The value names a registered sub-workflow and is required when
// Node.type = sub-workflow per [execution-model.md §4.8.EM-034]. At runtime
// the daemon expands the sub-workflow in-place within the parent run;
// see [execution-model.md §4.8] for expansion semantics.
//
// The spec declares sub_workflow_ref as String | None at §6.1 of
// execution-model.md. None is represented in Go as *SubWorkflowRef at the
// call site; SubWorkflowRef itself must always be non-empty. An absent ref
// (Node.type ≠ sub-workflow) is encoded as a nil pointer, NOT as an empty
// SubWorkflowRef.
//
// execution-model.md §6.1 does not declare a structured record shape for the
// reference value at MVH; MVH realises SubWorkflowRef as a typed non-empty
// string alias following the same pattern as PolicyVersion. A future
// execution-model.md revision may promote this to a structured record via the
// amendment protocol per [architecture.md §4.6].
type SubWorkflowRef string

// Valid reports whether r is a non-empty SubWorkflowRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (r SubWorkflowRef) Valid() bool {
	return r != ""
}

// MarshalText implements encoding.TextMarshaler so SubWorkflowRef serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (r SubWorkflowRef) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("subworkflowref: value must not be empty")
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (r *SubWorkflowRef) UnmarshalText(text []byte) error {
	v := SubWorkflowRef(text)
	if !v.Valid() {
		return fmt.Errorf("subworkflowref: value must not be empty")
	}
	*r = v
	return nil
}
