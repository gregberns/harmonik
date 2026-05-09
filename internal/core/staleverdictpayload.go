package core

// StaleVerdictPayload is the payload of the `reconciliation_verdict_stale`
// event (RC-024; reconciliation/schemas.md §6.1 RECORD StaleVerdictPayload).
//
// The record is emitted by the daemon's staleness check when the system state
// has advanced beyond the snapshot captured at investigator-dispatch time.
// Both current_git_head_hash and current_beads_audit_id are re-captured at
// verdict-execution time and compared against the values in SnapshotToken;
// divergence_reason classifies which store advanced.
//
// # Structural invariants (enforced by Valid)
//
//   - SnapshotToken passes SnapshotToken.Valid().
//   - CurrentGitHeadHash is non-empty.
//   - CurrentBeadsAuditID is non-empty.
//   - DivergenceReason is a declared StaleDivergenceReason constant.
type StaleVerdictPayload struct {
	// SnapshotToken is the token captured at investigator-dispatch time.
	// It bounds the investigator's view and is consumed by the staleness
	// check per RC-024. Must pass SnapshotToken.Valid().
	SnapshotToken SnapshotToken

	// CurrentGitHeadHash is the project HEAD SHA re-captured at
	// verdict-execution time. Non-empty required.
	CurrentGitHeadHash string

	// CurrentBeadsAuditID is the Beads audit-log entry ID re-captured at
	// verdict-execution time. Non-empty required.
	CurrentBeadsAuditID string

	// DivergenceReason classifies which store advanced since the snapshot,
	// causing the staleness check to fail per RC-024.
	// Must be a declared StaleDivergenceReason constant.
	DivergenceReason StaleDivergenceReason
}

// Valid reports whether all structural invariants of the StaleVerdictPayload
// are satisfied.
//
// Rules per reconciliation/schemas.md §6.1:
//   - SnapshotToken passes SnapshotToken.Valid().
//   - CurrentGitHeadHash is non-empty.
//   - CurrentBeadsAuditID is non-empty.
//   - DivergenceReason is a declared constant (StaleDivergenceReason.Valid() true).
func (p StaleVerdictPayload) Valid() bool {
	if !p.SnapshotToken.Valid() {
		return false
	}
	if p.CurrentGitHeadHash == "" {
		return false
	}
	if p.CurrentBeadsAuditID == "" {
		return false
	}
	if !p.DivergenceReason.Valid() {
		return false
	}
	return true
}
