package core

// DetectorClass is a typed identifier for a reconciliation detector class
// (event-model.md §8.6.12).
//
// A DetectorClass value names the category of detector whose per-detector
// panic-recovery barrier caught a panic, per RC-020b. It is carried in
// ReconciliationDetectorPanicPayload.DetectorClass and is used by the
// reconciliation-monitoring and audit consumers to identify which detector
// component requires attention.
//
// DetectorClass is a string-backed opaque identifier: the daemon and observability
// consumers treat it as an opaque label — no parsing or enumeration is imposed
// at MVH. The non-empty invariant is enforced by
// ReconciliationDetectorPanicPayload.Valid().
//
// Bead: hk-hqwn.75.
type DetectorClass string
