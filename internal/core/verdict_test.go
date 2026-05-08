package core

import (
	"encoding/json"
	"testing"
)

func TestVerdictValid(t *testing.T) {
	t.Parallel()

	valid := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}
	for _, v := range valid {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.Valid() {
				t.Errorf("Valid() = false for %q, want true", v)
			}
		})
	}

	invalid := []Verdict{
		"",
		"RESUME-HERE",
		"Resume-Here",
		"resume_here",
		"unknown",
		"resume",
		"reset",
	}
	for _, v := range invalid {
		v := v
		t.Run("invalid/"+string(v), func(t *testing.T) {
			t.Parallel()
			if v.Valid() {
				t.Errorf("Valid() = true for %q, want false", v)
			}
		})
	}
}

func TestVerdictMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verdict Verdict
		want    string
	}{
		{VerdictResumeHere, "resume-here"},
		{VerdictResumeWithContext, "resume-with-context"},
		{VerdictResetToCheckpoint, "reset-to-checkpoint"},
		{VerdictReopenBead, "reopen-bead"},
		{VerdictAcceptCloseWithNote, "accept-close-with-note"},
		{VerdictNoOpAccept, "no-op-accept"},
		{VerdictEscalateToHuman, "escalate-to-human"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got, err := tc.verdict.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("MarshalText = %q, want %q", string(got), tc.want)
			}
		})
	}

	if _, err := Verdict("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid verdict value")
	}
	if _, err := Verdict("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestVerdictUnmarshalText(t *testing.T) {
	t.Parallel()

	type verdictWrapper struct {
		V Verdict `json:"verdict"`
	}

	tests := []struct {
		name    string
		input   string
		want    Verdict
		wantErr bool
	}{
		{
			name:  "resume-here",
			input: `{"verdict":"resume-here"}`,
			want:  VerdictResumeHere,
		},
		{
			name:  "resume-with-context",
			input: `{"verdict":"resume-with-context"}`,
			want:  VerdictResumeWithContext,
		},
		{
			name:  "reset-to-checkpoint",
			input: `{"verdict":"reset-to-checkpoint"}`,
			want:  VerdictResetToCheckpoint,
		},
		{
			name:  "reopen-bead",
			input: `{"verdict":"reopen-bead"}`,
			want:  VerdictReopenBead,
		},
		{
			name:  "accept-close-with-note",
			input: `{"verdict":"accept-close-with-note"}`,
			want:  VerdictAcceptCloseWithNote,
		},
		{
			name:  "no-op-accept",
			input: `{"verdict":"no-op-accept"}`,
			want:  VerdictNoOpAccept,
		},
		{
			name:  "escalate-to-human",
			input: `{"verdict":"escalate-to-human"}`,
			want:  VerdictEscalateToHuman,
		},
		{
			name:    "unknown value rejected",
			input:   `{"verdict":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"verdict":""}`,
			wantErr: true,
		},
		{
			name:    "uppercase rejected",
			input:   `{"verdict":"RESUME-HERE"}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"verdict":"resume"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var w verdictWrapper
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
			if w.V != tc.want {
				t.Errorf("got %q, want %q", string(w.V), string(tc.want))
			}
		})
	}
}
