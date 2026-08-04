// Package readiness builds the retained readiness evidence record that the
// gate owner must hold before the first canary item is selected for a queue
// dogfood run.
//
// Spec ref: specs/beads-integration.md §4.5b BI-013e (readiness snapshot before
// first-canary selection); §4.7 BI-021 through BI-023 (Beads and git are
// authoritative, JSONL is observational).
// Operational record: docs/queue-readiness-ledger-events.md.
//
// # The record is read-only by construction, not by discipline
//
// The whole point of this package is that assembling the evidence cannot
// change fleet ledger state. Most of that guarantee is carried by the type
// system: [BeadReader] is the only ledger surface the package declares, and it
// has no method that can write, so no ordinary call can close, create, or
// reopen a bead.
//
// Be precise about the size of that promise, because it is not total. Go lets a
// caller widen a narrow interface at runtime with a type assertion, and such an
// assertion compiles. Two tests cover the two halves:
// TestBeadReaderExposesNoWriteMethod goes red if the port itself grows a method
// outside the read allow-list, and TestCapture_MakesNoLedgerCallThatCouldChangeABead
// goes red if a capture reaches a write by widening the port it was given.
//
// # Layout
//
// snapshot.go is pure. It takes values and returns values: no clock, no
// filesystem, no process. Its unexported newSnapshot is the
// parse-don't-validate boundary — it consumes an unchecked request and either
// refuses it or returns a [Snapshot] that every later reader can trust without
// re-checking.
//
// capture.go is the thin shell around it: the ledger port, the live read, and
// the intent-log directory scan. [Capture] is the only way into this package
// from outside, and that is deliberate. BI-013e says a caller must not supply a
// candidate's status, so there is no exported constructor that would accept
// one — every status in a snapshot was read live by [Capture] itself.
package readiness
