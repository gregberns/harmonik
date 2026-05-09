package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitInProgressOpValid(t *testing.T) {
	t.Parallel()

	valid := []GitInProgressOp{
		GitInProgressOpNone,
		GitInProgressOpRebase,
		GitInProgressOpMerge,
		GitInProgressOpCherryPick,
		GitInProgressOpBisect,
	}
	for _, op := range valid {
		if !op.Valid() {
			t.Errorf("expected %q to be valid", op)
		}
	}

	invalid := []GitInProgressOp{
		"",
		"None",
		"NONE",
		"Rebase",
		"REBASE",
		"Merge",
		"MERGE",
		"Cherry-pick",
		"cherry_pick", // underscore instead of hyphen
		"CHERRY-PICK",
		"cherrypick", // missing hyphen
		"Bisect",
		"BISECT",
		"unknown",
		"in-progress",
	}
	for _, op := range invalid {
		if op.Valid() {
			t.Errorf("expected %q to be invalid", op)
		}
	}
}

func TestGitInProgressOpMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   GitInProgressOp
		want string
	}{
		{GitInProgressOpNone, "none"},
		{GitInProgressOpRebase, "rebase"},
		{GitInProgressOpMerge, "merge"},
		{GitInProgressOpCherryPick, "cherry-pick"},
		{GitInProgressOpBisect, "bisect"},
	}
	for _, tc := range tests {
		got, err := tc.op.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.op, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.op, string(got), tc.want)
		}
	}

	if _, err := GitInProgressOp("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := GitInProgressOp("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestGitInProgressOpUnmarshalText(t *testing.T) {
	t.Parallel()

	type gitInProgressOpFixtureWrapper struct {
		Op GitInProgressOp `json:"op"`
	}

	tests := []struct {
		name    string
		input   string
		want    GitInProgressOp
		wantErr bool
	}{
		{
			name:  "none",
			input: `{"op":"none"}`,
			want:  GitInProgressOpNone,
		},
		{
			name:  "rebase",
			input: `{"op":"rebase"}`,
			want:  GitInProgressOpRebase,
		},
		{
			name:  "merge",
			input: `{"op":"merge"}`,
			want:  GitInProgressOpMerge,
		},
		{
			name:  "cherry-pick",
			input: `{"op":"cherry-pick"}`,
			want:  GitInProgressOpCherryPick,
		},
		{
			name:  "bisect",
			input: `{"op":"bisect"}`,
			want:  GitInProgressOpBisect,
		},
		{
			name:    "uppercase NONE rejected",
			input:   `{"op":"NONE"}`,
			wantErr: true,
		},
		{
			name:    "mixed-case Cherry-pick rejected",
			input:   `{"op":"Cherry-pick"}`,
			wantErr: true,
		},
		{
			name:    "underscore cherry_pick rejected",
			input:   `{"op":"cherry_pick"}`,
			wantErr: true,
		},
		{
			name:    "cherrypick without hyphen rejected",
			input:   `{"op":"cherrypick"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"op":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"op":""}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w gitInProgressOpFixtureWrapper
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
			if w.Op != tc.want {
				t.Errorf("got %q, want %q", string(w.Op), string(tc.want))
			}
		})
	}
}

func TestGitInProgressOpRoundTrip(t *testing.T) {
	t.Parallel()

	type gitInProgressOpFixtureWrapper struct {
		Op GitInProgressOp `json:"op"`
	}

	values := []GitInProgressOp{
		GitInProgressOpNone,
		GitInProgressOpRebase,
		GitInProgressOpMerge,
		GitInProgressOpCherryPick,
		GitInProgressOpBisect,
	}

	for _, op := range values {
		t.Run(string(op), func(t *testing.T) {
			t.Parallel()

			in := gitInProgressOpFixtureWrapper{Op: op}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", op, err)
			}
			var out gitInProgressOpFixtureWrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.Op != op {
				t.Errorf("round-trip: got %q, want %q", out.Op, op)
			}
		})
	}
}

func TestGitInProgressOpUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all five declared values.
	var op GitInProgressOp
	err := op.UnmarshalText([]byte("in-progress"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"none", "rebase", "merge", "cherry-pick", "bisect"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
