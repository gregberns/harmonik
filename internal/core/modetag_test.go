package core

import (
	"encoding/json"
	"testing"
)

// TestModeTagValid verifies that only the two declared constants are valid and
// that all other strings (including empty, uppercase, and partial matches) are
// rejected per architecture.md §4.2 AR-005.
func TestModeTagValid(t *testing.T) {
	t.Parallel()

	valid := []ModeTag{
		ModeTagMechanism,
		ModeTagCognition,
	}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("expected %q to be valid", m)
		}
	}

	invalid := []ModeTag{
		"",
		"MECHANISM",
		"Mechanism",
		"COGNITION",
		"Cognition",
		"unknown",
		"mech",
		"cog",
	}
	for _, m := range invalid {
		if m.Valid() {
			t.Errorf("expected %q to be invalid", m)
		}
	}
}

// TestModeTagMarshalText verifies MarshalText accepts the two declared values
// and rejects all others.
func TestModeTagMarshalText(t *testing.T) {
	t.Parallel()

	got, err := ModeTagMechanism.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "mechanism" {
		t.Errorf("MarshalText = %q, want %q", string(got), "mechanism")
	}

	got, err = ModeTagCognition.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "cognition" {
		t.Errorf("MarshalText = %q, want %q", string(got), "cognition")
	}

	if _, err := ModeTag("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := ModeTag("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestModeTagUnmarshalText verifies JSON round-trip behaviour and that unknown
// values produce errors (no silent default).
func TestModeTagUnmarshalText(t *testing.T) {
	t.Parallel()

	type modeFixtureWrapper struct {
		Mode ModeTag `json:"mode_tag"`
	}

	tests := []struct {
		name    string
		input   string
		want    ModeTag
		wantErr bool
	}{
		{name: "mechanism", input: `{"mode_tag":"mechanism"}`, want: ModeTagMechanism},
		{name: "cognition", input: `{"mode_tag":"cognition"}`, want: ModeTagCognition},
		{name: "unknown value rejected", input: `{"mode_tag":"unknown"}`, wantErr: true},
		{name: "empty string rejected", input: `{"mode_tag":""}`, wantErr: true},
		{name: "uppercase MECHANISM rejected", input: `{"mode_tag":"MECHANISM"}`, wantErr: true},
		{name: "uppercase COGNITION rejected", input: `{"mode_tag":"COGNITION"}`, wantErr: true},
		{name: "partial match mech rejected", input: `{"mode_tag":"mech"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w modeFixtureWrapper
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

// TestModeTagRoundTrip verifies that a ModeTag value survives a
// json.Marshal / json.Unmarshal round-trip without loss.
func TestModeTagRoundTrip(t *testing.T) {
	t.Parallel()

	for _, modeFixtureValue := range []ModeTag{ModeTagMechanism, ModeTagCognition} {
		data, err := json.Marshal(modeFixtureValue)
		if err != nil {
			t.Fatalf("json.Marshal(%q): %v", modeFixtureValue, err)
		}

		var decoded ModeTag
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(%q): %v", string(data), err)
		}

		if decoded != modeFixtureValue {
			t.Errorf("round-trip: got %q, want %q", decoded, modeFixtureValue)
		}
	}
}
