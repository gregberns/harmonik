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

func TestFailureClassPrecedenceNonZero(t *testing.T) {
	t.Parallel()

	// Every valid FailureClass must return a non-zero precedence rank.
	valid := []FailureClass{
		FailureClassHarnessInternalError,
		FailureClassOrchestrationInternalError,
		FailureClassScenarioLoadFailure,
		FailureClassTwinBinaryNotFound,
		FailureClassFixtureSetupFailed,
		FailureClassScenarioTimeout,
		FailureClassAssertionFailed,
		FailureClassCleanupFailed,
	}
	for _, f := range valid {
		if got := f.Precedence(); got == 0 {
			t.Errorf("Precedence(%q) = 0; want non-zero for a valid FailureClass", f)
		}
	}
}

func TestFailureClassPrecedenceInvalidReturnsZero(t *testing.T) {
	t.Parallel()

	// An unknown FailureClass must return 0 from Precedence.
	if got := FailureClass("").Precedence(); got != 0 {
		t.Errorf("Precedence(%q) = %d; want 0 for invalid value", "", got)
	}
	if got := FailureClass("made_up").Precedence(); got != 0 {
		t.Errorf("Precedence(%q) = %d; want 0 for invalid value", "made_up", got)
	}
}

func TestFailureClassPrecedenceOrder(t *testing.T) {
	t.Parallel()

	// §8.0 precedence table (highest first).  Rank 1 = highest.
	// Verify the ordinal assignment matches the spec order.
	table := []struct {
		class FailureClass
		rank  int
	}{
		{FailureClassHarnessInternalError, 1},
		{FailureClassOrchestrationInternalError, 2},
		{FailureClassScenarioLoadFailure, 3},
		{FailureClassTwinBinaryNotFound, 4},
		{FailureClassFixtureSetupFailed, 5},
		{FailureClassScenarioTimeout, 6},
		{FailureClassAssertionFailed, 7},
		{FailureClassCleanupFailed, 8},
	}
	for _, tc := range table {
		if got := tc.class.Precedence(); got != tc.rank {
			t.Errorf("Precedence(%q) = %d; want %d", tc.class, got, tc.rank)
		}
	}
}

func TestFailureClassPrecedenceStrictOrdering(t *testing.T) {
	t.Parallel()

	// Verify that each class has strictly lower rank (higher precedence) than
	// the next class in the table — i.e., the ranks form a strict total order.
	ordered := []FailureClass{
		FailureClassHarnessInternalError,
		FailureClassOrchestrationInternalError,
		FailureClassScenarioLoadFailure,
		FailureClassTwinBinaryNotFound,
		FailureClassFixtureSetupFailed,
		FailureClassScenarioTimeout,
		FailureClassAssertionFailed,
		FailureClassCleanupFailed,
	}
	for i := 0; i < len(ordered)-1; i++ {
		hi := ordered[i]
		lo := ordered[i+1]
		if hi.Precedence() >= lo.Precedence() {
			t.Errorf("Precedence(%q)=%d must be < Precedence(%q)=%d",
				hi, hi.Precedence(), lo, lo.Precedence())
		}
	}
}

func TestFailureClassHigherPrecedenceThan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		a     FailureClass
		b     FailureClass
		wantA bool // a.HigherPrecedenceThan(b)
		wantB bool // b.HigherPrecedenceThan(a)
	}{
		{
			name:  "harness-internal-error beats cleanup-failed",
			a:     FailureClassHarnessInternalError,
			b:     FailureClassCleanupFailed,
			wantA: true,
			wantB: false,
		},
		{
			name:  "orchestration-internal-error beats assertion-failed",
			a:     FailureClassOrchestrationInternalError,
			b:     FailureClassAssertionFailed,
			wantA: true,
			wantB: false,
		},
		{
			name:  "scenario-timeout beats assertion-failed",
			a:     FailureClassScenarioTimeout,
			b:     FailureClassAssertionFailed,
			wantA: true,
			wantB: false,
		},
		{
			name:  "harness-internal-error beats orchestration-internal-error",
			a:     FailureClassHarnessInternalError,
			b:     FailureClassOrchestrationInternalError,
			wantA: true,
			wantB: false,
		},
		{
			name:  "equal classes return false for both directions",
			a:     FailureClassAssertionFailed,
			b:     FailureClassAssertionFailed,
			wantA: false,
			wantB: false,
		},
		{
			name:  "invalid vs valid returns false",
			a:     FailureClass(""),
			b:     FailureClassAssertionFailed,
			wantA: false,
			wantB: false,
		},
		{
			name:  "valid vs invalid returns false",
			a:     FailureClassAssertionFailed,
			b:     FailureClass("unknown"),
			wantA: false,
			wantB: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.a.HigherPrecedenceThan(tc.b); got != tc.wantA {
				t.Errorf("%q.HigherPrecedenceThan(%q) = %v; want %v",
					tc.a, tc.b, got, tc.wantA)
			}
			if got := tc.b.HigherPrecedenceThan(tc.a); got != tc.wantB {
				t.Errorf("%q.HigherPrecedenceThan(%q) = %v; want %v",
					tc.b, tc.a, got, tc.wantB)
			}
		})
	}
}
