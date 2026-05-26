package core

import "fmt"

// OutcomeKind is the discriminator for the Outcome.payload envelope
// (execution-model.md §4.1.EM-005a, §6.1 ENUM OutcomeKind, v0.3.3).
// The enum is closed at MVH; future variants extend via the amendment
// protocol per [architecture.md §4.6].
//
// Wire alias: the outcome_kind field on HC-008's outcome_emitted event
// (handler-contract.md §4.3); the daemon MUST set Outcome.kind from the
// handler-returned outcome_kind without rewriting.
//
// A reader observing an unknown OutcomeKind MUST route the outcome to
// reconciliation per [reconciliation/spec.md §8.11 Cat 6a]; no silent
// fallback to default is permitted.
type OutcomeKind string

// OutcomeKind values per execution-model.md §6.1 ENUM OutcomeKind (v0.3.4).
const (
	// OutcomeKindDefault is the ordinary handler outcome shape.
	// payload MUST be absent when kind = default.
	OutcomeKindDefault OutcomeKind = "default"

	// OutcomeKindReconciliationVerdict carries a reconciliation investigator's
	// verdict. payload MUST be a VerdictEvent per [reconciliation/schemas.md §6.1]
	// (RC-022a) when kind = reconciliation_verdict.
	OutcomeKindReconciliationVerdict OutcomeKind = "reconciliation_verdict"

	// OutcomeKindGateDecision carries a gate evaluator's structured decision
	// rationale. payload MUST be a GateDecisionPayload per
	// [control-points.md §6.1.8 CP-058] (EM-005b). Added at schema v0.3.4
	// per EM-005a amendment. v0.3.3 readers encountering this value MUST route
	// to reconciliation Cat 6a per [reconciliation/spec.md §8.11]; they MUST
	// NOT silently degrade to default.
	OutcomeKindGateDecision OutcomeKind = "gate_decision"
)

// Valid reports whether k is one of the three declared OutcomeKind constants at v0.3.4.
// Unknown values are NOT tolerated — per execution-model.md §4.1.EM-005a, a reader
// observing an unknown kind MUST route to reconciliation Cat 6a rather than silently
// falling back to default.
func (k OutcomeKind) Valid() bool {
	switch k {
	case OutcomeKindDefault, OutcomeKindReconciliationVerdict, OutcomeKindGateDecision:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so OutcomeKind serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the three declared constants at v0.3.4.
func (k OutcomeKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("outcomekind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants at v0.3.4.
// Per execution-model.md §4.1.EM-005a, unknown kind values route to
// reconciliation Cat 6a; callers MUST NOT silently degrade to default.
func (k *OutcomeKind) UnmarshalText(text []byte) error {
	v := OutcomeKind(text)
	if !v.Valid() {
		return fmt.Errorf(
			"outcomekind: unknown value %q; must be one of default, reconciliation_verdict, gate_decision",
			string(text),
		)
	}
	*k = v
	return nil
}
