package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestWorkflowModeValid verifies all declared constants pass Valid() and
// that non-declared values are rejected.
func TestWorkflowModeValid(t *testing.T) {
	t.Parallel()

	valid := []WorkflowMode{
		WorkflowModeSingle,
		WorkflowModeDot,
	}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("expected %q to be valid", m)
		}
	}

	invalid := []WorkflowMode{
		"",
		"Single",
		"SINGLE",
		// "review-loop" was a declared mode until it was retired
		// (execution-model.md §4.3.EM-015d). It must now be REJECTED: the
		// literal survives only as WorkflowModeRetiredReviewLoop so stale
		// values can be named, never as a valid mode.
		WorkflowMode(WorkflowModeRetiredReviewLoop),
		"Review-Loop",
		"REVIEW-LOOP",
		"reviewloop",
		"review_loop",
		"DOT",
		"Dot",
		"unknown",
		"single ",
		" single",
	}
	for _, m := range invalid {
		if m.Valid() {
			t.Errorf("expected %q to be invalid", m)
		}
	}
}

// TestWorkflowModeMarshalText verifies MarshalText accepts valid values and
// rejects invalid ones.
func TestWorkflowModeMarshalText(t *testing.T) {
	t.Parallel()

	got, err := WorkflowModeSingle.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "single" {
		t.Errorf("MarshalText = %q, want %q", string(got), "single")
	}

	// "review-loop" used to marshal successfully; since its retirement
	// (execution-model.md §4.3.EM-015d) MarshalText must reject it, and the
	// error must point the operator at dot.
	if _, err := WorkflowMode(WorkflowModeRetiredReviewLoop).MarshalText(); err == nil {
		t.Error("MarshalText accepted the retired review-loop value")
	} else if !strings.Contains(err.Error(), "dot") {
		t.Errorf("retired-mode error %q does not name the dot replacement", err.Error())
	}

	got, err = WorkflowModeDot.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "dot" {
		t.Errorf("MarshalText = %q, want %q", string(got), "dot")
	}

	if _, err := WorkflowMode("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := WorkflowMode("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestWorkflowModeUnmarshalText verifies JSON round-trip behaviour via UnmarshalText.
func TestWorkflowModeUnmarshalText(t *testing.T) {
	t.Parallel()

	type workflowModeFixtureWrapper struct {
		Mode WorkflowMode `json:"workflow_mode"`
	}

	tests := []struct {
		name    string
		input   string
		want    WorkflowMode
		wantErr bool
	}{
		{name: "single", input: `{"workflow_mode":"single"}`, want: WorkflowModeSingle},
		// Retired mode (execution-model.md §4.3.EM-015d) still DECODES, on
		// purpose: historical event logs and pre-retirement Run records are facts
		// about what already happened, and a reader that refuses to parse them
		// goes blind on exactly the runs an operator is investigating. Selection
		// is what must be strict, and it is — Valid() rejects the value (asserted
		// in TestWorkflowModeValid) so a decoded value cannot reach a dispatch
		// path, and the config / bootconfig / CLI surfaces reject the raw string
		// before this ever runs.
		{name: "retired review-loop still decodes for history", input: `{"workflow_mode":"review-loop"}`, want: WorkflowMode(WorkflowModeRetiredReviewLoop)},
		{name: "dot", input: `{"workflow_mode":"dot"}`, want: WorkflowModeDot},
		{name: "empty rejected", input: `{"workflow_mode":""}`, wantErr: true},
		{name: "uppercase SINGLE rejected", input: `{"workflow_mode":"SINGLE"}`, wantErr: true},
		{name: "mixed case Single rejected", input: `{"workflow_mode":"Single"}`, wantErr: true},
		{name: "reviewloop (no hyphen) rejected", input: `{"workflow_mode":"reviewloop"}`, wantErr: true},
		{name: "review_loop (underscore) rejected", input: `{"workflow_mode":"review_loop"}`, wantErr: true},
		{name: "unknown rejected", input: `{"workflow_mode":"wave"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w workflowModeFixtureWrapper
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
			if w.Mode != tc.want {
				t.Errorf("got %q, want %q", string(w.Mode), string(tc.want))
			}
		})
	}
}

// TestWorkflowModeAllConstantsRoundTrip verifies every declared constant
// survives a json.Marshal / json.Unmarshal round-trip.
func TestWorkflowModeAllConstantsRoundTrip(t *testing.T) {
	t.Parallel()

	workflowModeFixtureAllModes := []WorkflowMode{
		WorkflowModeSingle,
		WorkflowModeDot,
	}

	for _, m := range workflowModeFixtureAllModes {
		t.Run(string(m), func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", m, err)
			}

			var decoded WorkflowMode
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal(%q): %v", data, err)
			}

			if decoded != m {
				t.Errorf("round-trip: got %q, want %q", decoded, m)
			}
		})
	}
}

// TestErrUnknownWorkflowMode verifies the typed error is returned for
// unknown values and that errors.As can extract it.
func TestErrUnknownWorkflowMode(t *testing.T) {
	t.Parallel()

	var m WorkflowMode
	err := m.UnmarshalText([]byte("bogus"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var typed ErrUnknownWorkflowMode
	if !errors.As(err, &typed) {
		t.Errorf("expected ErrUnknownWorkflowMode, got %T: %v", err, err)
	}
	if typed.Value != "bogus" {
		t.Errorf("ErrUnknownWorkflowMode.Value = %q, want %q", typed.Value, "bogus")
	}
}
