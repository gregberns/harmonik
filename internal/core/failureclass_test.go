package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFailureClassValid(t *testing.T) {
	t.Parallel()

	valid := []FailureClass{
		FailureClassTransient,
		FailureClassStructural,
		FailureClassDeterministic,
		FailureClassCanceled,
		FailureClassBudgetExhausted,
		FailureClassCompilationLoop,
	}
	for _, f := range valid {
		if !f.Valid() {
			t.Errorf("expected %q to be valid", f)
		}
	}

	invalid := []FailureClass{
		"",
		"made_up",
		"Transient",
		"TRANSIENT",
		"cancelled",        // common misspelling; not the declared value
		"budget-exhausted", // hyphen instead of underscore
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
			name:  "valid transient",
			input: `{"class":"transient"}`,
			want:  FailureClassTransient,
		},
		{
			name:  "valid structural",
			input: `{"class":"structural"}`,
			want:  FailureClassStructural,
		},
		{
			name:  "valid deterministic",
			input: `{"class":"deterministic"}`,
			want:  FailureClassDeterministic,
		},
		{
			name:  "valid canceled",
			input: `{"class":"canceled"}`,
			want:  FailureClassCanceled,
		},
		{
			name:  "valid budget_exhausted",
			input: `{"class":"budget_exhausted"}`,
			want:  FailureClassBudgetExhausted,
		},
		{
			name:  "valid compilation_loop",
			input: `{"class":"compilation_loop"}`,
			want:  FailureClassCompilationLoop,
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
			name:    "invalid uppercase TRANSIENT",
			input:   `{"class":"TRANSIENT"}`,
			wantErr: true,
		},
		{
			name:    "invalid cancelled (misspelling)",
			input:   `{"class":"cancelled"}`,
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

	got, err := FailureClassTransient.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "transient" {
		t.Errorf("MarshalText = %q, want %q", string(got), "transient")
	}

	if _, err := FailureClass("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestFailureClassRoundTrip(t *testing.T) {
	t.Parallel()

	// JSON round-trip for all six values.
	type wrapper struct {
		Class FailureClass `json:"class"`
	}

	values := []FailureClass{
		FailureClassTransient,
		FailureClassStructural,
		FailureClassDeterministic,
		FailureClassCanceled,
		FailureClassBudgetExhausted,
		FailureClassCompilationLoop,
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

	// Error message for an unknown value must list all six declared values.
	var f FailureClass
	err := f.UnmarshalText([]byte("made_up"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"transient", "structural", "deterministic",
		"canceled", "budget_exhausted", "compilation_loop",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
