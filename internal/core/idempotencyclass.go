package core

import "fmt"

// IdempotencyClass is the per-node tag driving reconciliation behavior
// (execution-model.md §6.1, EM-010).
// One of: idempotent, non-idempotent, recoverable-non-idempotent.
// Absent a policy override the per-node-type defaults declared in EM-010 apply;
// a YAML policy MAY override any default.
type IdempotencyClass string

// IdempotencyClass values per execution-model.md §6.1 ENUM declaration.
const (
	IdempotencyClassIdempotent               IdempotencyClass = "idempotent"
	IdempotencyClassNonIdempotent            IdempotencyClass = "non-idempotent"
	IdempotencyClassRecoverableNonIdempotent IdempotencyClass = "recoverable-non-idempotent"
)

// Valid reports whether c is one of the three declared IdempotencyClass constants.
func (c IdempotencyClass) Valid() bool {
	switch c {
	case IdempotencyClassIdempotent, IdempotencyClassNonIdempotent, IdempotencyClassRecoverableNonIdempotent:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so IdempotencyClass serialises
// correctly in JSON and YAML workflow definitions.
func (c IdempotencyClass) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("idempotencyclass: unknown value %q", string(c))
	}
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
func (c *IdempotencyClass) UnmarshalText(text []byte) error {
	v := IdempotencyClass(text)
	if !v.Valid() {
		return fmt.Errorf("idempotencyclass: unknown value %q; must be one of idempotent, non-idempotent, recoverable-non-idempotent", string(text))
	}
	*c = v
	return nil
}
