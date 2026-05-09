package core

import "github.com/google/uuid"

// VerdictExecutedPayload is the payload of the `reconciliation_verdict_executed`
// event (RC-025; reconciliation/schemas.md §6.1 RECORD VerdictExecutedPayload).
//
// The record is emitted by the daemon's verdict-executor after every mechanical
// action, confirming which verdict was executed, on which run, and when.
// `executed_at_timestamp` is advisory display only — it MUST NOT be used for
// ordering or causal inference.
//
// # Structural invariants (enforced by Valid)
//
//   - InvestigatorRunID is not uuid.Nil.
//   - TargetRunID is not uuid.Nil.
//   - Verdict is non-empty and one of the seven declared constants.
//   - ExecutedAtTimestamp is non-empty (caller MUST format as RFC 3339 per
//     [event-model.md §4.3]).
//   - ActionSummary is non-empty.
type VerdictExecutedPayload struct {
	// InvestigatorRunID is the run_id of the reconciliation workflow that
	// produced and executed the verdict. Must not be uuid.Nil.
	InvestigatorRunID uuid.UUID

	// TargetRunID is the run_id of the outer run that was reconciled.
	// Must not be uuid.Nil.
	TargetRunID uuid.UUID

	// Verdict is the reconciliation verdict that was executed. Required; must
	// be one of the seven declared Verdict constants per
	// reconciliation/schemas.md §6.1.
	Verdict Verdict

	// ExecutedAtTimestamp is the RFC 3339 wall-clock time at which the verdict
	// was executed. Advisory display only per RC-025. Required (non-empty).
	// Caller MUST format as RFC 3339 per [event-model.md §4.3].
	ExecutedAtTimestamp string

	// ActionSummary is short prose describing the mechanical action taken by
	// the daemon's verdict-executor. Required (non-empty).
	ActionSummary string
}

// Valid reports whether all structural invariants of the VerdictExecutedPayload
// are satisfied.
//
// Rules per reconciliation/schemas.md §6.1:
//   - InvestigatorRunID is not uuid.Nil.
//   - TargetRunID is not uuid.Nil.
//   - Verdict is non-empty and a declared constant (Verdict.Valid() true).
//   - ExecutedAtTimestamp is non-empty.
//   - ActionSummary is non-empty.
func (p VerdictExecutedPayload) Valid() bool {
	if p.InvestigatorRunID == uuid.Nil {
		return false
	}
	if p.TargetRunID == uuid.Nil {
		return false
	}
	if !p.Verdict.Valid() {
		return false
	}
	if p.ExecutedAtTimestamp == "" {
		return false
	}
	if p.ActionSummary == "" {
		return false
	}
	return true
}
