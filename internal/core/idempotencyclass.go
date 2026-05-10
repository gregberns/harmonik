package core

import "fmt"

// IdempotencyClass is the per-node tag driving reconciliation behavior
// (execution-model.md §6.1, EM-010) and the per-side-effect delivery contract
// driving at-least-once vs at-most-once dispatch (control-points.md §6.1.6, CP-016).
//
// execution-model.md §6.1 is the canonical owner and declares three values:
// idempotent, non-idempotent, recoverable-non-idempotent. Absent a policy
// override the per-node-type defaults declared in EM-010 apply; a YAML policy
// MAY override any default.
//
// control-points.md §6.1.6 cites this type on SideEffect.idempotency_class.
// CP-016 delivery semantics apply to the {idempotent, non-idempotent} subset:
// idempotent allows at-least-once delivery; non-idempotent requires at-most-once
// via persisted receipt. SideEffect descriptors carrying recoverable-non-idempotent
// are treated as non-idempotent by the S05 dispatcher — that value is a
// node-level concept, not a distinct S05 dispatch mode. CP §6.1.6 is a filter
// over the full enum, not a redeclaration; EM §6.1 owns the type.
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

// DefaultIdempotencyClassForNodeRole returns the MVH-baseline idempotency-class
// default for a given node role, absent a YAML policy override, per
// execution-model.md §4.2.EM-010.
//
// Mapping (MVH baseline):
//
//	reviewer, researcher, lint, test, typecheck, analysis → idempotent
//	builder, merge                                        → non-idempotent
//
// A YAML policy MAY override any default at runtime; this function only
// encodes the static baseline. Post-MVH node types that register
// recoverable-non-idempotent defaults are NOT represented here — that
// extension is deferred per EM-010 and §9.3.
//
// The second return value is false when role is not one of the eight declared
// NodeRole constants at MVH; callers MUST check ok before using the class.
func DefaultIdempotencyClassForNodeRole(role NodeRole) (class IdempotencyClass, ok bool) {
	switch role {
	case NodeRoleReviewer,
		NodeRoleResearcher,
		NodeRoleLint,
		NodeRoleTest,
		NodeRoleTypecheck,
		NodeRoleAnalysis:
		return IdempotencyClassIdempotent, true
	case NodeRoleBuilder, NodeRoleMerge:
		return IdempotencyClassNonIdempotent, true
	default:
		return "", false
	}
}
