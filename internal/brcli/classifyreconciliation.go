package brcli

import "errors"

// ReconciliationCategory is the string code of a harmonik reconciliation
// category as defined in specs/reconciliation/spec.md §8.
//
// The declared constants below are the only values this package produces. The
// typed wrapper in internal/reconciliation/ (tracked as a follow-up bead; see
// below) will replace the raw string alias once that package exists.
//
// Valid values produced by [BrErrReconciliationCategory]:
//
//	"Cat 0"  — infrastructure unavailable (retry / daemon exit 8)
//	"Cat 3"  — store disagreement, generic (investigator dispatch)
//	"Cat 3a" — torn Beads write (idempotency recovery)
//	"Cat 6a" — integrity violation, LLM-triageable (non-BrError input)
//	""       — no reconciliation category (nil error or BrOK)
//
// TODO: replace with the typed alias from internal/reconciliation/ once that
// package lands (follow-up bead for the typed wrapper — see Typed-alias-deferral
// pattern in .claude/implementer-protocol.md).
type ReconciliationCategory = string

// Reconciliation category codes per specs/reconciliation/spec.md §8.
const (
	// RecCat0 — infrastructure unavailable; bounded retry, or daemon exit 8 on
	// persistent failure. Spec ref: reconciliation/spec.md §8.1.
	RecCat0 ReconciliationCategory = "Cat 0"

	// RecCat3 — store disagreement, generic; investigator dispatch.
	// Spec ref: reconciliation/spec.md §8.4.
	RecCat3 ReconciliationCategory = "Cat 3"

	// RecCat3a — torn Beads write; idempotency recovery via adapter re-issue.
	// Spec ref: reconciliation/spec.md §8.4a.
	RecCat3a ReconciliationCategory = "Cat 3a"

	// RecCat6a — integrity violation, LLM-triageable; investigator dispatch.
	// Returned when err is non-nil but does not wrap any BrError value.
	// Spec ref: reconciliation/spec.md §8.11.
	RecCat6a ReconciliationCategory = "Cat 6a"
)

// BrErrReconciliationCategory maps a BrError (or an error wrapping one via
// errors.Is) to the corresponding harmonik reconciliation category code.
//
// The six routing rules are declared in specs/beads-integration.md §8:
//
//	BrNotFound     → "Cat 3"  (store disagreement; investigator dispatch)
//	BrConflict     → "Cat 3a" (torn write; idempotency recovery per BI §4.10)
//	BrDbLocked     → "Cat 0"  (bounded retry; persistent → exit 8)
//	BrSchemaMismatch → "Cat 0" (daemon startup failure; exit 8)
//	BrUnavailable  → "Cat 0"  (bounded retry per PL-010 cadence)
//	BrOther        → "Cat 3"  (divergence detected; investigator dispatch)
//
// Special cases (not in the §8 table):
//
//   - nil error or BrOK: returns "" — no reconciliation category applies;
//     callers SHOULD NOT route successful invocations through this function.
//   - err is non-nil but wraps no recognized BrError value: returns "Cat 6a"
//     (integrity violation, LLM-triageable) as the safe escalation path.
//
// errors.Is unwrapping is applied so wrapped errors (e.g. fmt.Errorf("...: %w",
// BrUnavailable)) resolve correctly.
func BrErrReconciliationCategory(err error) ReconciliationCategory {
	if err == nil {
		return ""
	}

	// BrOK is the success sentinel: a successful br invocation carries no
	// reconciliation category.  It is not in the §8 routing table by design.
	if errors.Is(err, BrOK) {
		return ""
	}

	// Check each BrError sentinel via errors.Is so wrapped errors resolve.
	for _, e := range brErrRoutingTable {
		if errors.Is(err, e.brErr) {
			return e.cat
		}
	}

	// err is non-nil but does not wrap any recognized BrError value.
	// Escalate to Cat 6a (integrity violation, LLM-triageable) as the safest
	// default: an unrecognized error type at this call site indicates an
	// unexpected caller state rather than a Beads-side divergence.
	return RecCat6a
}

// brErrRoutingEntry pairs a BrError sentinel with its reconciliation category.
type brErrRoutingEntry struct {
	brErr BrError
	cat   ReconciliationCategory
}

// brErrRoutingTable is the machine-readable form of specs/beads-integration.md
// §8 Table, enumerating all six non-OK BrError values.  BrOK is excluded: a
// successful invocation carries no reconciliation category.
//
// Order follows the §8 table row order for auditability.
var brErrRoutingTable = [...]brErrRoutingEntry{
	{BrNotFound, RecCat3},
	{BrConflict, RecCat3a},
	{BrDbLocked, RecCat0},
	{BrSchemaMismatch, RecCat0},
	{BrUnavailable, RecCat0},
	{BrOther, RecCat3},
}
