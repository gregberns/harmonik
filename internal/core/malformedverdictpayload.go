package core

import "github.com/google/uuid"

// MalformedVerdictPayload is the payload of the reconciliation_verdict_malformed
// event (RC-023) (specs/reconciliation/schemas.md §6.1 RECORD MalformedVerdictPayload).
//
// It is produced when the reconciliation investigator emits a verdict that
// cannot be parsed or validated. The payload captures enough context for the
// operator to diagnose the root cause.
//
// # Valid() rules
//
// All three identifier/enum fields are required:
//   - InvestigatorRunID must not be uuid.Nil.
//   - TargetRunID must not be uuid.Nil.
//   - MalformationReason must satisfy MalformationReason.Valid().
//
// RawVerdictExcerpt may be empty (e.g., when the raw payload is itself absent
// or entirely unparseable); it is opaque diagnostic text and is not validated.
type MalformedVerdictPayload struct {
	// InvestigatorRunID is the run_id of the reconciliation workflow that
	// produced the malformed verdict. Required (non-nil UUID).
	InvestigatorRunID uuid.UUID

	// TargetRunID is the run_id of the outer run being reconciled.
	// Required (non-nil UUID).
	TargetRunID uuid.UUID

	// MalformationReason classifies why the verdict was rejected.
	// Required; must satisfy MalformationReason.Valid().
	MalformationReason MalformationReason

	// RawVerdictExcerpt is a truncated excerpt of the raw verdict payload,
	// included for diagnosis. May be empty when the raw payload is absent or
	// entirely unparseable.
	RawVerdictExcerpt string
}

// Valid reports whether all required fields carry non-zero/valid values.
//
// Rules per specs/reconciliation/schemas.md §6.1 RECORD MalformedVerdictPayload:
//   - InvestigatorRunID must not be uuid.Nil.
//   - TargetRunID must not be uuid.Nil.
//   - MalformationReason must satisfy MalformationReason.Valid().
//
// RawVerdictExcerpt is not validated; nil/empty is acceptable.
func (m MalformedVerdictPayload) Valid() bool {
	if m.InvestigatorRunID == uuid.Nil {
		return false
	}
	if m.TargetRunID == uuid.Nil {
		return false
	}
	return m.MalformationReason.Valid()
}
