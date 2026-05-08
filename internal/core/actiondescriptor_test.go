package core

import (
	"encoding/json"
	"testing"
)

// TestActionDescriptorValid verifies that any non-empty string is valid and
// that an empty string is rejected.
func TestActionDescriptorValid(t *testing.T) {
	t.Parallel()

	valid := []ActionDescriptor{
		"action-a",
		"write-file",
		"run-tests",
		"lint",
		"a",
	}
	for _, a := range valid {
		if !a.Valid() {
			t.Errorf("expected %q to be valid", a)
		}
	}

	if ActionDescriptor("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestActionDescriptorMarshalText verifies MarshalText accepts non-empty values
// and rejects the empty string.
func TestActionDescriptorMarshalText(t *testing.T) {
	t.Parallel()

	got, err := ActionDescriptor("write-file").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "write-file" {
		t.Errorf("MarshalText = %q, want %q", string(got), "write-file")
	}

	if _, err := ActionDescriptor("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestActionDescriptorUnmarshalText verifies JSON round-trip behaviour.
func TestActionDescriptorUnmarshalText(t *testing.T) {
	t.Parallel()

	type actionDescFixtureWrapper struct {
		Action ActionDescriptor `json:"chosen_action"`
	}

	tests := []struct {
		name    string
		input   string
		want    ActionDescriptor
		wantErr bool
	}{
		{name: "write-file", input: `{"chosen_action":"write-file"}`, want: "write-file"},
		{name: "run-tests", input: `{"chosen_action":"run-tests"}`, want: "run-tests"},
		{name: "arbitrary non-empty", input: `{"chosen_action":"action-x"}`, want: "action-x"},
		{name: "empty rejected", input: `{"chosen_action":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w actionDescFixtureWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.Action != tc.want {
				t.Errorf("got %q, want %q", string(w.Action), string(tc.want))
			}
		})
	}
}

// TestActionDescriptorRoundTrip verifies a non-empty ActionDescriptor survives
// a json.Marshal / json.Unmarshal round-trip.
func TestActionDescriptorRoundTrip(t *testing.T) {
	t.Parallel()

	actionDescFixtureValue := ActionDescriptor("write-output-file")

	data, err := json.Marshal(actionDescFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded ActionDescriptor
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != actionDescFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, actionDescFixtureValue)
	}
}

// TestActionDescriptorSliceRoundTrip verifies a []ActionDescriptor (candidate_actions)
// survives a json.Marshal / json.Unmarshal round-trip.
func TestActionDescriptorSliceRoundTrip(t *testing.T) {
	t.Parallel()

	actionDescFixtureSlice := []ActionDescriptor{"action-a", "action-b", "action-c"}

	data, err := json.Marshal(actionDescFixtureSlice)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded []ActionDescriptor
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(decoded) != len(actionDescFixtureSlice) {
		t.Fatalf("length: got %d, want %d", len(decoded), len(actionDescFixtureSlice))
	}
	for i, want := range actionDescFixtureSlice {
		if decoded[i] != want {
			t.Errorf("[%d]: got %q, want %q", i, decoded[i], want)
		}
	}
}

// TestTraceValid_EmptyChosenActionRejected verifies that a Trace with an empty
// ChosenAction fails Valid().
func TestTraceValid_EmptyChosenActionRejected(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ChosenAction = ActionDescriptor("")
	if tr.Valid() {
		t.Error("Valid() = true with empty ChosenAction, want false")
	}
}

// TestTraceValid_NilCandidateActionsStillInvalid verifies that nil CandidateActions
// still fails Valid() with ActionDescriptor element type.
func TestTraceValid_NilCandidateActionsStillInvalid(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.CandidateActions = nil
	if tr.Valid() {
		t.Error("Valid() = true with nil CandidateActions, want false")
	}
}
