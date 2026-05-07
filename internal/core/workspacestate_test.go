package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkspaceStateValid(t *testing.T) {
	t.Parallel()

	valid := []WorkspaceState{
		WorkspaceStateCreated,
		WorkspaceStateReady,
		WorkspaceStateLeased,
		WorkspaceStateMergePending,
		WorkspaceStateConflictResolving,
		WorkspaceStateMerged,
		WorkspaceStateDiscarded,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []WorkspaceState{
		"",
		"made_up",
		"Created",
		"CREATED",
		"setup",            // retired value (v0.3.0); MUST NOT be reintroduced
		"merge_pending",    // underscore instead of hyphen
		"conflict-resolve", // truncated form; not the declared value
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestWorkspaceStateUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		State WorkspaceState `json:"state"`
	}

	tests := []struct {
		name    string
		input   string
		want    WorkspaceState
		wantErr bool
	}{
		{
			name:  "valid created",
			input: `{"state":"created"}`,
			want:  WorkspaceStateCreated,
		},
		{
			name:  "valid ready",
			input: `{"state":"ready"}`,
			want:  WorkspaceStateReady,
		},
		{
			name:  "valid leased",
			input: `{"state":"leased"}`,
			want:  WorkspaceStateLeased,
		},
		{
			name:  "valid merge-pending",
			input: `{"state":"merge-pending"}`,
			want:  WorkspaceStateMergePending,
		},
		{
			name:  "valid conflict-resolving",
			input: `{"state":"conflict-resolving"}`,
			want:  WorkspaceStateConflictResolving,
		},
		{
			name:  "valid merged",
			input: `{"state":"merged"}`,
			want:  WorkspaceStateMerged,
		},
		{
			name:  "valid discarded",
			input: `{"state":"discarded"}`,
			want:  WorkspaceStateDiscarded,
		},
		{
			name:    "invalid made_up",
			input:   `{"state":"made_up"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"state":""}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase CREATED",
			input:   `{"state":"CREATED"}`,
			wantErr: true,
		},
		{
			name:    "invalid retired setup",
			input:   `{"state":"setup"}`,
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
			if w.State != tc.want {
				t.Errorf("got %q, want %q", w.State, tc.want)
			}
		})
	}
}

func TestWorkspaceStateMarshalText(t *testing.T) {
	t.Parallel()

	got, err := WorkspaceStateCreated.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "created" {
		t.Errorf("MarshalText = %q, want %q", string(got), "created")
	}

	if _, err := WorkspaceState("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestWorkspaceStateRoundTrip(t *testing.T) {
	t.Parallel()

	// JSON round-trip for all seven values.
	type wrapper struct {
		State WorkspaceState `json:"state"`
	}

	values := []WorkspaceState{
		WorkspaceStateCreated,
		WorkspaceStateReady,
		WorkspaceStateLeased,
		WorkspaceStateMergePending,
		WorkspaceStateConflictResolving,
		WorkspaceStateMerged,
		WorkspaceStateDiscarded,
	}

	for _, s := range values {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			in := wrapper{State: s}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", s, err)
			}
			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.State != s {
				t.Errorf("round-trip: got %q, want %q", out.State, s)
			}
		})
	}
}

func TestWorkspaceStateUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all seven declared values.
	var s WorkspaceState
	err := s.UnmarshalText([]byte("made_up"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"created", "ready", "leased", "merge-pending",
		"conflict-resolving", "merged", "discarded",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
