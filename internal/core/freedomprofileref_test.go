package core

import (
	"encoding/json"
	"testing"
)

// TestFreedomProfileRefValid verifies that any non-empty string is valid and
// that an empty string is rejected.
func TestFreedomProfileRefValid(t *testing.T) {
	t.Parallel()

	valid := []FreedomProfileRef{
		"default-researcher",
		"builder-restricted",
		"planner-full",
		"a",
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	if FreedomProfileRef("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestFreedomProfileRefMarshalText verifies MarshalText accepts non-empty
// values and rejects the empty string.
func TestFreedomProfileRefMarshalText(t *testing.T) {
	t.Parallel()

	got, err := FreedomProfileRef("builder-restricted").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "builder-restricted" {
		t.Errorf("MarshalText = %q, want %q", string(got), "builder-restricted")
	}

	if _, err := FreedomProfileRef("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestFreedomProfileRefUnmarshalText verifies JSON round-trip behaviour.
func TestFreedomProfileRefUnmarshalText(t *testing.T) {
	t.Parallel()

	type freedomprofileFixtureWrapper struct {
		Ref FreedomProfileRef `json:"freedom_profile_ref"`
	}

	tests := []struct {
		name    string
		input   string
		want    FreedomProfileRef
		wantErr bool
	}{
		{name: "default-researcher", input: `{"freedom_profile_ref":"default-researcher"}`, want: "default-researcher"},
		{name: "builder-restricted", input: `{"freedom_profile_ref":"builder-restricted"}`, want: "builder-restricted"},
		{name: "arbitrary non-empty", input: `{"freedom_profile_ref":"profile-x"}`, want: "profile-x"},
		{name: "empty rejected", input: `{"freedom_profile_ref":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w freedomprofileFixtureWrapper
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
			if w.Ref != tc.want {
				t.Errorf("got %q, want %q", string(w.Ref), string(tc.want))
			}
		})
	}
}

// TestFreedomProfileRefRoundTrip verifies a non-empty FreedomProfileRef
// survives a json.Marshal / json.Unmarshal round-trip.
func TestFreedomProfileRefRoundTrip(t *testing.T) {
	t.Parallel()

	freedomprofileFixtureValue := FreedomProfileRef("planner-full")

	data, err := json.Marshal(freedomprofileFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded FreedomProfileRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != freedomprofileFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, freedomprofileFixtureValue)
	}
}
