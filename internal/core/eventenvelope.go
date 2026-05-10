package core

// EventEnvelope is a Go type alias for Event — the common envelope record that
// every event carries (event-model.md §4.1 EV-001, §6.1 RECORD Event).
//
// The spec declares the shape as "RECORD Event" in §6.1 and refers to it as
// "EventEnvelope" in cross-spec citations (reconciliation/schemas.md §6.1
// RECORD InvestigatorInput field jsonl_tail). This alias bridges the two names
// so that callers in the reconciliation subsystem can use the spec's
// cross-reference term without introducing a new type.
//
// Because EventEnvelope is a true Go alias (not a distinct named type), values
// of type EventEnvelope and Event are interchangeable in all contexts: no
// conversion is required and method sets are identical.
//
// # Pending substitution
//
// InvestigatorInput.jsonl_tail is currently typed []string pending the
// substitution bead (hk-63oh.47 merging to main) per typed-alias-deferral
// protocol. Once both beads land on main, the orchestrator will substitute
// []string with []EventEnvelope in InvestigatorInput (reconciliation/schemas.md
// §6.1).
type EventEnvelope = Event
