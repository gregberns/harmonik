package scenario

// EventExpectationKind is the assertion-direction discriminator for an
// EventExpectation — whether the declared event type MUST appear (event_present)
// or MUST NOT appear (event_absent) in the captured event log.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD EventExpectation.
type EventExpectationKind string

const (
	// EventExpectationKindPresent asserts that an event of the declared type
	// (and optional payload predicates) was emitted during the scenario window.
	EventExpectationKindPresent EventExpectationKind = "event_present"

	// EventExpectationKindAbsent asserts that no event of the declared type
	// (matching optional payload predicates) was emitted during the scenario
	// wall-clock window (fixture-setup-completion to terminal-emission or
	// timeout-cancellation-completion per SH-021).
	EventExpectationKindAbsent EventExpectationKind = "event_absent"
)

// EventExpectation is a declared expectation about whether an event of a given
// type appears (or does not appear) in the captured event log, with optional
// shallow field-match predicates over the payload.
//
// PayloadMatch keys use dotted-path grammar per SH-021 (e.g.,
// "error.category" addresses the category field of a top-level error object;
// array indices use bracket form "items[0].id"). Equality is value-equal under
// JSON canonical types with shallow-merge semantics: declared keys MUST appear
// in the actual payload with equal values, but the actual payload MAY contain
// additional unmatched keys. Key validation and path-walking are
// evaluator-side responsibilities; Valid does not check PayloadMatch structure.
//
// Spec ref: specs/scenario-harness.md §6.1.
type EventExpectation struct {
	// Kind is the assertion direction: event_present or event_absent.
	// Required (must be one of the declared EventExpectationKind constants).
	Kind EventExpectationKind `json:"kind" yaml:"kind"`

	// Type is the event type per specs/event-model.md §8 taxonomy. This field
	// is currently `string` pending the typed `EventType` enum (TODO: hk-szv5
	// — see follow-up bead). When that lands, this field should hoist to
	// `core.EventType` (non-breaking: string-constant assignment is assignable
	// to typed-string).
	Type string `json:"type" yaml:"type"`

	// PayloadMatch holds optional dotted-path-keyed field-equals predicates
	// applied against the actual event payload per SH-021 shallow-merge
	// semantics. nil means no payload predicates (|None in the spec); both nil
	// and non-nil are valid.
	PayloadMatch map[string]any `json:"payload_match,omitempty" yaml:"payload_match,omitempty"`

	// Description is an operator-facing label for the assertion.
	// Required (non-empty).
	Description string `json:"description" yaml:"description"`
}

// Valid reports whether the EventExpectation is structurally well-formed:
//   - Kind is one of the declared EventExpectationKind constants.
//   - Type is non-empty.
//   - Description is non-empty.
//   - PayloadMatch may be nil or non-nil; both are valid per the Map|None spec.
//
// Dotted-path key validation within PayloadMatch is an evaluator-side
// responsibility (SH-021) and is NOT checked here.
func (e EventExpectation) Valid() bool {
	switch e.Kind {
	case EventExpectationKindPresent, EventExpectationKindAbsent:
		// valid kind — continue
	default:
		return false
	}
	return e.Type != "" && e.Description != ""
}
