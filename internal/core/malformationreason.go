package core

import "fmt"

// MalformationReason classifies why a reconciliation verdict is malformed
// (specs/reconciliation/schemas.md §6.1 ENUM MalformationReason).
//
// It is carried in MalformedVerdictPayload and emitted on the
// reconciliation_verdict_malformed event per RC-023.
//
// A reader observing an unknown MalformationReason MUST reject the enclosing
// record; no silent fallback is permitted.
type MalformationReason string

// MalformationReason values per specs/reconciliation/schemas.md §6.1 ENUM MalformationReason.
const (
	// MalformationReasonUnknownVerdictValue indicates the verdict field is not
	// one of the declared Verdict enum values.
	MalformationReasonUnknownVerdictValue MalformationReason = "unknown-verdict-value"

	// MalformationReasonMissingRequiredField indicates a required field is
	// absent (e.g., context when verdict=resume-with-context).
	MalformationReasonMissingRequiredField MalformationReason = "missing-required-field"

	// MalformationReasonExtraFields indicates top-level fields not present in
	// the VerdictEvent schema were found.
	MalformationReasonExtraFields MalformationReason = "extra-fields"

	// MalformationReasonWrongType indicates a field is present but carries the
	// wrong JSON type.
	MalformationReasonWrongType MalformationReason = "wrong-type"

	// MalformationReasonMultipleVerdicts indicates the reconciliation workflow
	// emitted more than one verdict event.
	MalformationReasonMultipleVerdicts MalformationReason = "multiple-verdicts"

	// MalformationReasonVerdictAfterTerminal indicates a verdict event was
	// emitted after the workflow's terminal event.
	MalformationReasonVerdictAfterTerminal MalformationReason = "verdict-after-terminal"
)

// Valid reports whether r is one of the six declared MalformationReason constants.
// Unknown values are NOT tolerated — a reader observing an unknown MalformationReason
// MUST reject the enclosing record per specs/reconciliation/schemas.md §6.1.
func (r MalformationReason) Valid() bool {
	switch r {
	case MalformationReasonUnknownVerdictValue,
		MalformationReasonMissingRequiredField,
		MalformationReasonExtraFields,
		MalformationReasonWrongType,
		MalformationReasonMultipleVerdicts,
		MalformationReasonVerdictAfterTerminal:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so MalformationReason
// serialises correctly in JSON and YAML.
// It rejects any value that is not one of the six declared constants.
func (r MalformationReason) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("malformationreason: unknown value %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the six declared constants.
// Per specs/reconciliation/schemas.md §6.1, unknown MalformationReason values
// MUST be rejected; callers MUST NOT silently degrade to a default reason.
func (r *MalformationReason) UnmarshalText(text []byte) error {
	v := MalformationReason(text)
	if !v.Valid() {
		return fmt.Errorf(
			"malformationreason: unknown value %q; must be one of"+
				" unknown-verdict-value, missing-required-field, extra-fields,"+
				" wrong-type, multiple-verdicts, verdict-after-terminal",
			string(text),
		)
	}
	*r = v
	return nil
}
