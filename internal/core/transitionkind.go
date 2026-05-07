package core

import "fmt"

// TransitionKind is the kind of a workflow transition (execution-model.md §6.1, EM-044).
// One of: forward, local-patchback, architectural-rollback, policy-rollback, context-restore.
// Durable values per EM-023a; rollback_to_state_id constraints per EM-044.
type TransitionKind string

// TransitionKind values per execution-model.md §6.1 ENUM declaration.
const (
	TransitionKindForward               TransitionKind = "forward"
	TransitionKindLocalPatchback        TransitionKind = "local-patchback"
	TransitionKindArchitecturalRollback TransitionKind = "architectural-rollback"
	TransitionKindPolicyRollback        TransitionKind = "policy-rollback"
	TransitionKindContextRestore        TransitionKind = "context-restore"
)

// Valid reports whether k is one of the five declared TransitionKind constants.
// This is the predicate hook for EM-044: validators call Valid on every
// transition's kind field and reject records that contain unknown values.
func (k TransitionKind) Valid() bool {
	switch k {
	case TransitionKindForward, TransitionKindLocalPatchback, TransitionKindArchitecturalRollback,
		TransitionKindPolicyRollback, TransitionKindContextRestore:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so TransitionKind serialises
// correctly in JSON and YAML workflow definitions.
func (k TransitionKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("transitionkind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the five declared constants,
// satisfying the EM-044 requirement that unknown kinds are rejected.
func (k *TransitionKind) UnmarshalText(text []byte) error {
	v := TransitionKind(text)
	if !v.Valid() {
		return fmt.Errorf("transitionkind: unknown value %q; must be one of forward, local-patchback, architectural-rollback, policy-rollback, context-restore", string(text))
	}
	*k = v
	return nil
}
