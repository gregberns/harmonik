package core

// Outcome is the handler-produced result of a workflow node's execution
// (execution-model.md §6.1 RECORD Outcome, v0.3.4).
//
// The record is the 8-field discriminated envelope returned by every node
// handler and consumed by the cascade (§4.10.EM-041) and durability decision
// (§4.5.EM-023a). The Kind / Payload extension envelope is the v0.3.3 additive
// addition per EM-005a; the FailureClass field and gate_decision kind are the
// v0.3.4 additive addition per EM-005b and EM-005c.
//
// # Kind / Payload discriminated union
//
// Kind is the discriminator (OutcomeKind). Payload carries kind-specific data:
//   - Kind == OutcomeKindDefault: Payload MUST be nil. The cascade and durability
//     decision operate on the remaining fields unchanged.
//   - Kind == OutcomeKindReconciliationVerdict: Payload MUST be a *VerdictEvent
//     per [reconciliation/schemas.md §6.1] (RC-022a).
//   - Kind == OutcomeKindGateDecision: Payload MUST be a *GateDecisionPayload
//     per [control-points.md §6.1.8 CP-058] (EM-005b). Added at v0.3.4.
//
// Unknown Kind values route to reconciliation Cat 6a per
// [reconciliation/spec.md §8.11]; callers MUST NOT silently fall back to
// OutcomeKindDefault.
//
// # FailureClass field
//
// FailureClass is an optional handler-emitted HINT present ONLY when
// Status == OutcomeStatusFail. The daemon back-fills from the §8 sentinel
// classification path (HC-020) when the handler omits the field; the daemon's
// classification is authoritative on disagreement per HC-059. The engine-side
// post-classifier Outcome MUST carry FailureClass when Status == FAIL (EM-005c).
//
// # OutcomeStatus vs OutcomeKind
//
// These are DISTINCT types:
//   - OutcomeStatus is the 4-value result enum (SUCCESS / FAIL / RETRY /
//     PARTIAL_SUCCESS). It drives the durability decision of §4.5.EM-023a and
//     determines whether a terminal state is reached per §4.9.
//   - OutcomeKind is the envelope discriminator (default /
//     reconciliation_verdict / gate_decision). It determines whether Payload is
//     present and how the daemon routes the outcome through the verdict-executor
//     or gate-decision path.
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

	// PreferredLabelFlags is an optional list of issue-tag flags associated with
	// the PreferredLabel. Set by reviewer nodes to carry the agent-reviewer schema
	// v1 flags field alongside the verdict label so driveDotWorkflow can surface
	// flags in review_fixup_stalled events without re-reading the verdict file.
	// Nil means no flags; semantically equivalent to an empty slice.
	// Bead ref: hk-m1wqp.
	PreferredLabelFlags []string

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

	// FailureClass is an optional handler-emitted failure classification hint
	// per §4.1.EM-005c (v0.3.4). MUST be present ONLY when Status == FAIL;
	// MUST be absent (nil) on non-FAIL outcomes per HC-058. The handler-emitted
	// value is a HINT; the daemon back-fills from the §8 sentinel classification
	// path (HC-020) when nil, and overrides the handler on disagreement (HC-059).
	// The post-classifier engine-side Outcome MUST carry this field on FAIL.
	FailureClass *FailureClass

	// Kind is the OutcomeKind discriminator per §4.1.EM-005a (v0.3.4).
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
	// When Kind == OutcomeKindGateDecision: MUST be a *GateDecisionPayload
	// per [control-points.md §6.1.8 CP-058] (EM-005b, v0.3.4). Opaque to the
	// cascade and durability decision; consumed by the gate-decision audit path.
	Payload any
}

// Valid reports whether the Outcome satisfies its structural invariants.
//
// Rules:
//   - Status must satisfy Status.Valid() (one of the four declared constants).
//   - Kind must satisfy Kind.Valid() (one of the three declared constants at v0.3.4).
//   - When Kind == OutcomeKindDefault, Payload MUST be nil.
//   - When Kind == OutcomeKindReconciliationVerdict, Payload MUST be a non-nil *VerdictEvent.
//   - When Kind == OutcomeKindGateDecision, Payload MUST be a non-nil *GateDecisionPayload
//     with a valid GateDecisionPayload (per GateDecisionPayload.Valid).
//   - FailureClass, when non-nil, MUST satisfy FailureClass.Valid() and Status
//     MUST be OutcomeStatusFail (per EM-005c / HC-058: present ONLY on FAIL).
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
	// FailureClass: must be absent on non-FAIL; when present must be a valid value.
	if o.FailureClass != nil {
		if o.Status != OutcomeStatusFail {
			return false
		}
		if !o.FailureClass.Valid() {
			return false
		}
	}
	switch o.Kind {
	case OutcomeKindDefault:
		if o.Payload != nil {
			return false
		}
	case OutcomeKindReconciliationVerdict:
		ve, ok := o.Payload.(*VerdictEvent)
		if !ok || ve == nil {
			return false
		}
	case OutcomeKindGateDecision:
		gdp, ok := o.Payload.(*GateDecisionPayload)
		if !ok || gdp == nil {
			return false
		}
		if !gdp.Valid() {
			return false
		}
	}
	return true
}
