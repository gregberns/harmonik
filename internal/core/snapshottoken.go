package core

// SnapshotToken bounds the investigator's view of the system state at dispatch
// time (reconciliation/schemas.md §6.1 RECORD SnapshotToken).
//
// All three fields are required (non-empty / non-zero). The token is consumed
// by the staleness check at verdict-execution time per RC-024.
//
// # CapturedAtTimestamp type decision
//
// CapturedAtTimestamp is kept as string rather than time.Time. The token is
// serialized verbatim into event payloads and LaunchSpec (RC-015), so a plain
// string avoids silent timezone normalization and JSON round-trip drift. The
// caller MUST format the value as RFC 3339 wall-clock per [event-model.md §4.3].
// Promotion to time.Time with custom marshal/unmarshal is a future option if a
// parsing use-case emerges.
type SnapshotToken struct {
	// GitHeadHash is the SHA of the project HEAD (or the reference the
	// investigator reads from) at snapshot time. Required (non-empty).
	GitHeadHash string

	// BeadsAuditEntryID is the ID of the most recent Beads audit-log entry at
	// capture time. Required (non-empty).
	BeadsAuditEntryID string

	// CapturedAtTimestamp is the RFC 3339 wall-clock time at which the token
	// was captured. Advisory display only per [event-model.md §4.3].
	// Required (non-empty). Caller MUST format as RFC 3339.
	CapturedAtTimestamp string
}

// Valid reports whether all three required fields are present and non-empty.
//
// Rules per reconciliation/schemas.md §6.1:
//   - GitHeadHash must be non-empty.
//   - BeadsAuditEntryID must be non-empty.
//   - CapturedAtTimestamp must be non-empty.
func (s SnapshotToken) Valid() bool {
	return s.GitHeadHash != "" && s.BeadsAuditEntryID != "" && s.CapturedAtTimestamp != ""
}
