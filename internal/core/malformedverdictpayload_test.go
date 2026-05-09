package core

import (
	"testing"

	"github.com/google/uuid"
)

// malformedVerdictPayloadFixture returns a fully-populated MalformedVerdictPayload
// with all required fields set to valid values. Tests mutate individual fields
// to probe Valid().
func malformedVerdictPayloadFixture(t *testing.T) MalformedVerdictPayload {
	t.Helper()
	return MalformedVerdictPayload{
		InvestigatorRunID:  uuid.Must(uuid.NewV7()),
		TargetRunID:        uuid.Must(uuid.NewV7()),
		MalformationReason: MalformationReasonUnknownVerdictValue,
		RawVerdictExcerpt:  `{"verdict":"not-a-verdict"}`,
	}
}

func TestMalformedVerdictPayloadValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	if !m.Valid() {
		t.Error("Valid() = false for fully-populated MalformedVerdictPayload, want true")
	}
}

func TestMalformedVerdictPayloadValid_AllMalformationReasons(t *testing.T) {
	t.Parallel()

	reasons := []MalformationReason{
		MalformationReasonUnknownVerdictValue,
		MalformationReasonMissingRequiredField,
		MalformationReasonExtraFields,
		MalformationReasonWrongType,
		MalformationReasonMultipleVerdicts,
		MalformationReasonVerdictAfterTerminal,
	}
	for _, r := range reasons {
		r := r
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			m := malformedVerdictPayloadFixture(t)
			m.MalformationReason = r
			if !m.Valid() {
				t.Errorf("Valid() = false with MalformationReason=%q, want true", r)
			}
		})
	}
}

func TestMalformedVerdictPayloadValid_ZeroInvestigatorRunID(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	m.InvestigatorRunID = uuid.Nil
	if m.Valid() {
		t.Error("Valid() = true with zero InvestigatorRunID, want false")
	}
}

func TestMalformedVerdictPayloadValid_ZeroTargetRunID(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	m.TargetRunID = uuid.Nil
	if m.Valid() {
		t.Error("Valid() = true with zero TargetRunID, want false")
	}
}

func TestMalformedVerdictPayloadValid_InvalidMalformationReason(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	m.MalformationReason = MalformationReason("bogus")
	if m.Valid() {
		t.Error("Valid() = true with unknown MalformationReason, want false")
	}
}

func TestMalformedVerdictPayloadValid_EmptyMalformationReason(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	m.MalformationReason = MalformationReason("")
	if m.Valid() {
		t.Error("Valid() = true with empty MalformationReason, want false")
	}
}

func TestMalformedVerdictPayloadValid_EmptyRawVerdictExcerpt(t *testing.T) {
	t.Parallel()

	m := malformedVerdictPayloadFixture(t)
	m.RawVerdictExcerpt = ""
	if !m.Valid() {
		t.Error("Valid() = false with empty RawVerdictExcerpt, want true (field is optional)")
	}
}

func TestMalformedVerdictPayloadValid_ZeroValue(t *testing.T) {
	t.Parallel()

	var m MalformedVerdictPayload
	if m.Valid() {
		t.Error("Valid() = true for zero-value MalformedVerdictPayload, want false")
	}
}
