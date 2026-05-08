package core

import (
	"encoding/json"
	"testing"
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

// outcomeValidPayload returns the smallest valid *string sentinel used to
// represent a non-nil payload (the typed-alias-deferral placeholder for
// VerdictEvent per hk-b3f.93).
func outcomeValidPayload(t *testing.T) *string {
	t.Helper()
	s := `{"placeholder":true}`
	return &s
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

func TestOutcomeJSONRoundTrip_Default(t *testing.T) {
	t.Parallel()

	label := "my-label"
	original := Outcome{
		Status:           OutcomeStatusFail,
		PreferredLabel:   &label,
		SuggestedNextIDs: []NodeID{"node-1", "node-2"},
		ContextUpdates:   map[string]any{"attempt": float64(3)},
		Notes:            "some notes",
		Kind:             OutcomeKindDefault,
		Payload:          nil,
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got Outcome
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.Status != original.Status {
		t.Errorf("Status = %q, want %q", got.Status, original.Status)
	}
	if got.PreferredLabel == nil || *got.PreferredLabel != label {
		t.Errorf("PreferredLabel = %v, want %q", got.PreferredLabel, label)
	}
	if len(got.SuggestedNextIDs) != 2 || got.SuggestedNextIDs[0] != "node-1" || got.SuggestedNextIDs[1] != "node-2" {
		t.Errorf("SuggestedNextIDs = %v, want [node-1 node-2]", got.SuggestedNextIDs)
	}
	if got.Notes != original.Notes {
		t.Errorf("Notes = %q, want %q", got.Notes, original.Notes)
	}
	if got.Kind != original.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, original.Kind)
	}
	if got.Payload != nil {
		t.Errorf("Payload = %v, want nil", got.Payload)
	}
	if !got.Valid() {
		t.Error("Valid() = false after JSON round-trip, want true")
	}
}

func TestOutcomeJSONRoundTrip_ReconciliationVerdict(t *testing.T) {
	t.Parallel()

	payload := `{"verdict":"resume-here","schema_version":1}`
	original := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: &payload,
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got Outcome
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.Kind != original.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, original.Kind)
	}
	if got.Payload == nil {
		t.Fatal("Payload = nil after JSON round-trip, want non-nil")
	}
	if *got.Payload != payload {
		t.Errorf("Payload = %q, want %q", *got.Payload, payload)
	}
	if !got.Valid() {
		t.Error("Valid() = false after JSON round-trip, want true")
	}
}

// TestOutcomeJSONRoundTrip_UnknownKindRejected verifies that OutcomeKind's
// UnmarshalText rejects unknown values at deserialization time per
// execution-model.md §4.1.EM-005a: a reader observing an unknown kind MUST
// route to reconciliation Cat 6a rather than silently degrade. The strict
// rejection at unmarshal time enforces this — the caller cannot accidentally
// observe an invalid Outcome.
func TestOutcomeJSONRoundTrip_UnknownKindRejected(t *testing.T) {
	t.Parallel()

	raw := `{"Status":"SUCCESS","Kind":"unknown_kind","Payload":null}`
	var got Outcome
	if err := json.Unmarshal([]byte(raw), &got); err == nil {
		t.Error("json.Unmarshal succeeded for unknown Kind, want error (per EM-005a: unknown kinds MUST be rejected)")
	}
}
