package core

// CostBasis is a typed identifier for a cost model used in budget accrual events
// (event-model.md §8.4.2).
//
// A CostBasis value names the dimension on which cost is metered, for example
// "input_tokens" or "output_tokens". It is carried in BudgetAccrualPayload.CostBasis
// and is used by the improvement-loop subsystem for per-dimension cost attribution
// (control-points.md §4.5).
//
// CostBasis is a string-backed opaque identifier: the daemon and observability
// consumers treat it as an opaque label — no parsing or enumeration is imposed
// at MVH. The non-empty invariant is enforced by BudgetAccrualPayload.Valid().
//
// Bead: hk-hqwn.73.
type CostBasis string
