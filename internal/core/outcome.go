package core

// Outcome is the handler-produced result of a workflow node's execution
// (execution-model.md §6.1 RECORD Outcome, v0.3.3).
//
// The record is the 7-field discriminated envelope returned by every node
// handler and consumed by the cascade (§4.10.EM-041) and durability decision
// (§4.5.EM-023a). The Kind / Payload extension envelope is the v0.3.3 additive
// addition per EM-005a.
//
// # Kind / Payload discriminated union
//
// Kind is the discriminator (OutcomeKind). Payload carries kind-specific data:
//   - Kind == OutcomeKindDefault: Payload MUST be nil. The cascade and durability
//     decision operate on the remaining fields unchanged.
//   - Kind == OutcomeKindReconciliationVerdict: Payload MUST be a *VerdictEvent
//     per [reconciliation/schemas.md §6.1]. At MVH VerdictEvent is not yet
//     declared in internal/core/; the Payload field uses *string as a
//     typed-alias-deferral placeholder (see TODO below). Follow-up bead:
//     hk-b3f.93 — "Define VerdictEvent record in internal/core".
//
// Unknown Kind values route to reconciliation Cat 6a per
// [reconciliation/spec.md §8.11]; callers MUST NOT silently fall back to
// OutcomeKindDefault.
//
// # OutcomeStatus vs OutcomeKind
//
// These are DISTINCT types:
//   - OutcomeStatus is the 4-value result enum (SUCCESS / FAIL / RETRY /
//     PARTIAL_SUCCESS). It drives the durability decision of §4.5.EM-023a and
//     determines whether a terminal state is reached per §4.9.
//   - OutcomeKind is the envelope discriminator (default /
//     reconciliation_verdict). It determines whether Payload is present and
//     how the daemon routes the outcome through the verdict-executor path.
//
// Do NOT conflate them (HANDOFF.md scar #5).
type Outcome struct {
	// Status is the 4-value result of the node's execution per §6.1 ENUM
	// OutcomeStatus. Drives the durability decision (§4.5.EM-023a) and
	// terminal-state detection (§4.9). Required (must satisfy Status.Valid()).
	Status OutcomeStatus

	// PreferredLabel is an optional routing hint. The cascade resolver matches
	// this value against Edge.Label during edge selection per §4.10.EM-041.
	// Nil means no hint; the cascade falls back to weight-ordered edges.
	PreferredLabel *string

	// SuggestedNextIDs is an ordered list of node IDs the handler recommends as
	// the next routing target (routing hint per §4.10.EM-041). NOT an override:
	// the cascade MUST still evaluate edge conditions and labels; the hint only
	// influences the ordering of candidates. Nil or empty means no hint.
	SuggestedNextIDs []NodeID

	// ContextUpdates is a freeform key/value map applied to run.context before
	// the cascade evaluates edge conditions, per §4.10.EM-041a (pre-cascade
	// application). Nil and empty map are equivalent (no updates).
	ContextUpdates map[string]any

	// Notes is a freeform human-readable string captured for observability.
	// No structural constraint; empty string is valid.
	Notes string

	// Kind is the OutcomeKind discriminator per §4.1.EM-005a (v0.3.3).
	// Defaults to OutcomeKindDefault. MUST satisfy Kind.Valid(); unknown values
	// route to reconciliation Cat 6a per [reconciliation/spec.md §8.11].
	Kind OutcomeKind

	// Payload is the kind-discriminated extension envelope per §4.1.EM-005a.
	//
	// When Kind == OutcomeKindDefault: MUST be nil.
	// When Kind == OutcomeKindReconciliationVerdict: MUST be a *VerdictEvent
	// per [reconciliation/schemas.md §6.1] (RC-022a). The payload is opaque to
	// the cascade (§4.10) and the durability decision (§4.5.EM-023a); it is
	// consumed exclusively by the RC-025a verdict-executor on the reconciliation
	// outcome-spine path.
	//
	// TODO(hk-b3f.93): replace *string placeholder with *VerdictEvent once
	// internal/core/verdictevent.go lands (reconciliation/schemas.md §6.1
	// VerdictEvent, 8-field RECORD). Until then this field holds a raw JSON
	// blob or nil; callers that need to consume the verdict MUST assert
	// Kind == OutcomeKindReconciliationVerdict and decode the blob themselves.
	Payload *string
}

// Valid reports whether the Outcome satisfies its structural invariants.
//
// Rules:
//   - Status must satisfy Status.Valid() (one of the four declared constants).
//   - Kind must satisfy Kind.Valid() (one of the two declared constants).
//   - When Kind == OutcomeKindDefault, Payload MUST be nil.
//   - When Kind == OutcomeKindReconciliationVerdict, Payload MUST be non-nil.
//
// Note: ContextUpdates, SuggestedNextIDs, PreferredLabel, and Notes carry no
// structural constraints beyond their Go types. An empty or nil value for any
// of those fields is valid.
func (o Outcome) Valid() bool {
	if !o.Status.Valid() {
		return false
	}
	if !o.Kind.Valid() {
		return false
	}
	switch o.Kind {
	case OutcomeKindDefault:
		if o.Payload != nil {
			return false
		}
	case OutcomeKindReconciliationVerdict:
		if o.Payload == nil {
			return false
		}
	}
	return true
}
