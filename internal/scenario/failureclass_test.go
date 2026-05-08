package scenario

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFailureClassValid(t *testing.T) {
	t.Parallel()

	valid := []FailureClass{
		FailureClassScenarioLoadFailure,
		FailureClassTwinBinaryNotFound,
		FailureClassFixtureSetupFailed,
		FailureClassOrchestrationInternalError,
		FailureClassHarnessInternalError,
		FailureClassAssertionFailed,
		FailureClassScenarioTimeout,
		FailureClassCleanupFailed,
	}
	for _, f := range valid {
		if !f.Valid() {
			t.Errorf("expected %q to be valid", f)
		}
	}

	invalid := []FailureClass{
		"",
		"made_up",
		"assertion_failed", // underscore instead of hyphen
		"scenario_timeout", // underscore instead of hyphen
		"Assertion-Failed", // mixed case
		"CLEANUP-FAILED",   // upper case
		"cleanup_failed",   // underscore instead of hyphen
		"unknown",
	}
	for _, f := range invalid {
		if f.Valid() {
			t.Errorf("expected %q to be invalid", f)
		}
	}
}

func TestFailureClassUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Class FailureClass `json:"class"`
	}

	tests := []struct {
		name    string
		input   string
		want    FailureClass
		wantErr bool
	}{
		{
			name:  "valid scenario-load-failure",
			input: `{"class":"scenario-load-failure"}`,
			want:  FailureClassScenarioLoadFailure,
		},
		{
			name:  "valid twin-binary-not-found",
			input: `{"class":"twin-binary-not-found"}`,
			want:  FailureClassTwinBinaryNotFound,
		},
		{
			name:  "valid fixture-setup-failed",
			input: `{"class":"fixture-setup-failed"}`,
			want:  FailureClassFixtureSetupFailed,
		},
		{
			name:  "valid orchestration-internal-error",
			input: `{"class":"orchestration-internal-error"}`,
			want:  FailureClassOrchestrationInternalError,
		},
		{
			name:  "valid harness-internal-error",
			input: `{"class":"harness-internal-error"}`,
			want:  FailureClassHarnessInternalError,
		},
		{
			name:  "valid assertion-failed",
			input: `{"class":"assertion-failed"}`,
			want:  FailureClassAssertionFailed,
		},
		{
			name:  "valid scenario-timeout",
			input: `{"class":"scenario-timeout"}`,
			want:  FailureClassScenarioTimeout,
		},
		{
			name:  "valid cleanup-failed",
			input: `{"class":"cleanup-failed"}`,
			want:  FailureClassCleanupFailed,
		},
		{
			name:    "invalid made_up",
			input:   `{"class":"made_up"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"class":""}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase ASSERTION-FAILED",
			input:   `{"class":"ASSERTION-FAILED"}`,
			wantErr: true,
		},
		{
			name:    "invalid underscore variant assertion_failed",
			input:   `{"class":"assertion_failed"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w wrapper
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
			if w.Class != tc.want {
				t.Errorf("got %q, want %q", w.Class, tc.want)
			}
		})
	}
}

func TestFailureClassMarshalText(t *testing.T) {
	t.Parallel()

	got, err := FailureClassAssertionFailed.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "assertion-failed" {
		t.Errorf("MarshalText = %q, want %q", string(got), "assertion-failed")
	}

	if _, err := FailureClass("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestFailureClassRoundTrip(t *testing.T) {
	t.Parallel()

	// JSON round-trip for all eight values.
	type wrapper struct {
		Class FailureClass `json:"class"`
	}

	values := []FailureClass{
		FailureClassScenarioLoadFailure,
		FailureClassTwinBinaryNotFound,
		FailureClassFixtureSetupFailed,
		FailureClassOrchestrationInternalError,
		FailureClassHarnessInternalError,
		FailureClassAssertionFailed,
		FailureClassScenarioTimeout,
		FailureClassCleanupFailed,
	}

	for _, f := range values {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			in := wrapper{Class: f}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", f, err)
			}
			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.Class != f {
				t.Errorf("round-trip: got %q, want %q", out.Class, f)
			}
		})
	}
}

func TestFailureClassUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all eight declared values.
	var f FailureClass
	err := f.UnmarshalText([]byte("made_up"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"scenario-load-failure", "twin-binary-not-found", "fixture-setup-failed",
		"orchestration-internal-error", "harness-internal-error", "assertion-failed",
		"scenario-timeout", "cleanup-failed",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
