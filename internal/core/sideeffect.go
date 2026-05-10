package core

// SideEffect is the side-effect descriptor produced by a Hook evaluator
// (specs/control-points.md §6.1.6 RECORD SideEffect).
//
// The four fields map directly to the RECORD declaration:
//
//	RECORD SideEffect:
//	    kind                 : SideEffectKind
//	    target               : String
//	    payload              : Map<String, Any>
//	    idempotency_class    : IdempotencyClass
//
// # Payload opacity
//
// Payload is opaque to this layer. The Map<String, Any> spec type is
// represented as map[string]any. The S05 dispatcher interprets the payload
// per its own semantics following CP-016 (specs/control-points.md §4.3.CP-016);
// no validation of payload contents is performed here.
//
// # IdempotencyClass note
//
// IdempotencyClass carries three values per execution-model.md §6.1 (canonical
// owner): idempotent, non-idempotent, recoverable-non-idempotent. CP-016
// delivery rules apply to the {idempotent, non-idempotent} subset; descriptors
// carrying recoverable-non-idempotent are treated as non-idempotent by the S05
// dispatcher. Valid() accepts all three values; the CP-016 subset is enforced
// at the S05 dispatch layer, not here. See control-points.md §6.1.6 for the
// clarifying note on the filter-vs-redeclaration distinction.
type SideEffect struct {
	// Kind discriminates how the S05 dispatcher interprets Target.
	// Must be one of the three declared SideEffectKind constants.
	Kind SideEffectKind `json:"kind"`

	// Target is the event name, state key, or effector id depending on Kind:
	//   - SideEffectKindEmitEvent:       event name to emit
	//   - SideEffectKindStateMutation:   typed state key written by S05 dispatcher
	//   - SideEffectKindExternalAction:  effector id registered per handler-contract.md §4.11
	//
	// Must be non-empty.
	Target string `json:"target"`

	// Payload is opaque to this spec and interpreted by the S05 dispatcher per
	// specs/control-points.md §4.3.CP-016. An absent or nil payload is valid;
	// field presence is controlled by the evaluator expression.
	Payload map[string]any `json:"payload"`

	// IdempotencyClass determines S05 delivery semantics per CP-016:
	//   - IdempotencyClassIdempotent:    S05 MAY deliver duplicates; handlers accept retry safely
	//   - IdempotencyClassNonIdempotent: S05 MUST bound delivery to at-most-once via persisted receipt
	IdempotencyClass IdempotencyClass `json:"idempotency_class"`
}

// Valid reports whether s is a well-formed SideEffect.
//
// The following invariants are checked:
//   - Kind must be a declared SideEffectKind value (via SideEffectKind.Valid)
//   - Target must be non-empty
//   - IdempotencyClass must be a declared IdempotencyClass value (via IdempotencyClass.Valid)
//
// Payload contents are not validated; they are opaque to this layer
// (specs/control-points.md §6.1.6).
func (s SideEffect) Valid() bool {
	return s.Kind.Valid() && s.Target != "" && s.IdempotencyClass.Valid()
}
