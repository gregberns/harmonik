package core

import (
	"testing"

	"github.com/google/uuid"
)

// outcomeValid returns a fully-populated Outcome with all required fields set
// to valid values. Tests mutate individual fields to probe Valid().
func outcomeValid(t *testing.T) Outcome {
	t.Helper()
	return Outcome{
		Status:           OutcomeStatusSuccess,
		PreferredLabel:   nil,
		SuggestedNextIDs: nil,
		ContextUpdates:   nil,
		Notes:            "",
		Kind:             OutcomeKindDefault,
		Payload:          nil,
	}
}

// outcomeValidPayload returns the smallest valid *VerdictEvent for use in
// Outcome tests that require a non-nil reconciliation-verdict payload.
func outcomeValidPayload(t *testing.T) *VerdictEvent {
	t.Helper()
	ctx := "investigator context"
	return &VerdictEvent{
		Verdict:           VerdictResumeWithContext,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		Context:           &ctx,
		SchemaVersion:     1,
	}
}

// --- Valid() tests ---

func TestOutcomeValid_MinimalDefault(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	if !o.Valid() {
		t.Error("Valid() = false for minimal default Outcome, want true")
	}
}

func TestOutcomeValid_FullDefaultOptionals(t *testing.T) {
	t.Parallel()

	label := "success-path"
	o := Outcome{
		Status:           OutcomeStatusPartialSuccess,
		PreferredLabel:   &label,
		SuggestedNextIDs: []NodeID{"node-a", "node-b"},
		ContextUpdates:   map[string]any{"key": "value", "count": 42},
		Notes:            "freeform notes",
		Kind:             OutcomeKindDefault,
		Payload:          nil,
	}
	if !o.Valid() {
		t.Error("Valid() = false for fully-populated default Outcome, want true")
	}
}

func TestOutcomeValid_ReconciliationVerdictWithPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: outcomeValidPayload(t),
	}
	if !o.Valid() {
		t.Error("Valid() = false for reconciliation_verdict Outcome with payload, want true")
	}
}

// --- Status field ---

func TestOutcomeValid_InvalidStatus(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Status = OutcomeStatus("UNKNOWN")
	if o.Valid() {
		t.Error("Valid() = true with invalid Status, want false")
	}
}

func TestOutcomeValid_EmptyStatus(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Status = OutcomeStatus("")
	if o.Valid() {
		t.Error("Valid() = true with empty Status, want false")
	}
}

func TestOutcomeValid_AllStatusValues(t *testing.T) {
	t.Parallel()

	statuses := []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
		OutcomeStatusPartialSuccess,
	}
	for _, s := range statuses {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			o := outcomeValid(t)
			o.Status = s
			if !o.Valid() {
				t.Errorf("Valid() = false for status=%q, want true", s)
			}
		})
	}
}

// --- Kind field ---

func TestOutcomeValid_InvalidKind(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKind("unknown_kind")
	if o.Valid() {
		t.Error("Valid() = true with invalid Kind, want false")
	}
}

func TestOutcomeValid_EmptyKind(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKind("")
	if o.Valid() {
		t.Error("Valid() = true with empty Kind, want false")
	}
}

// --- Kind/Payload discriminated-union enforcement ---

func TestOutcomeValid_DefaultKindWithPayload_Rejected(t *testing.T) {
	t.Parallel()

	// Spec: when Kind=default, Payload MUST be absent (nil).
	o := outcomeValid(t)
	o.Kind = OutcomeKindDefault
	o.Payload = outcomeValidPayload(t)
	if o.Valid() {
		t.Error("Valid() = true with Kind=default and non-nil Payload, want false (spec §6.1: payload absent when kind=default)")
	}
}

func TestOutcomeValid_ReconciliationVerdictKindWithoutPayload_Rejected(t *testing.T) {
	t.Parallel()

	// Spec: when Kind=reconciliation_verdict, Payload MUST be non-nil.
	o := outcomeValid(t)
	o.Kind = OutcomeKindReconciliationVerdict
	o.Payload = nil
	if o.Valid() {
		t.Error("Valid() = true with Kind=reconciliation_verdict and nil Payload, want false (spec §6.1 RC-022a)")
	}
}

// --- Optional fields carry no structural constraint ---

func TestOutcomeValid_EmptySuggestedNextIDs(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.SuggestedNextIDs = []NodeID{}
	if !o.Valid() {
		t.Error("Valid() = false for empty SuggestedNextIDs slice, want true")
	}
}

func TestOutcomeValid_NilContextUpdates(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.ContextUpdates = nil
	if !o.Valid() {
		t.Error("Valid() = false for nil ContextUpdates, want true")
	}
}

func TestOutcomeValid_EmptyContextUpdates(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.ContextUpdates = map[string]any{}
	if !o.Valid() {
		t.Error("Valid() = false for empty ContextUpdates map, want true")
	}
}

func TestOutcomeValid_EmptyNotes(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Notes = ""
	if !o.Valid() {
		t.Error("Valid() = false for empty Notes, want true")
	}
}

func TestOutcomeValid_NilPreferredLabel(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.PreferredLabel = nil
	if !o.Valid() {
		t.Error("Valid() = false for nil PreferredLabel, want true")
	}
}

// --- JSON round-trip ---

func TestOutcomeValid_DefaultKindNilPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status: OutcomeStatusFail,
		Kind:   OutcomeKindDefault,
	}
	if !o.Valid() {
		t.Error("Valid() = false for default kind with nil Payload, want true")
	}
}
