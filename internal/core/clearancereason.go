package core

// ClearanceReason is the typed discriminator for the `clearance_reason` field
// of operator_escalation_cleared (event-model.md §8.7.17).
//
// Spec ref: event-model.md §8.7.17.
// Bead ref: hk-hqwn.71.
type ClearanceReason string

const (
	// ClearanceReasonVerdictExecuted indicates the escalation was cleared because
	// the pending verdict was successfully executed.
	ClearanceReasonVerdictExecuted ClearanceReason = "verdict_executed"

	// ClearanceReasonManualClear indicates the operator manually cleared the
	// escalation.
	ClearanceReasonManualClear ClearanceReason = "manual_clear"

	// ClearanceReasonSuperseded indicates the escalation was cleared because a
	// newer escalation superseded it.
	ClearanceReasonSuperseded ClearanceReason = "superseded"
)

// Valid reports whether r is one of the three declared ClearanceReason constants.
func (r ClearanceReason) Valid() bool {
	switch r {
	case ClearanceReasonVerdictExecuted, ClearanceReasonManualClear, ClearanceReasonSuperseded:
		return true
	default:
		return false
	}
}
