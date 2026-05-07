package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInterruptStateValid(t *testing.T) {
	t.Parallel()

	valid := []InterruptState{
		InterruptStateNone,
		InterruptStateOperatorPaused,
		InterruptStateOperatorStoppedGraceful,
		InterruptStateOperatorStoppedImmediate,
		InterruptStateDaemonCrashSuspected,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []InterruptState{
		"",
		"made_up",
		"None",
		"NONE",
		"operator_paused",          // underscore instead of hyphen
		"operator-stopped-unknown", // not a declared value
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestInterruptStateUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		State InterruptState `json:"state"`
	}

	tests := []struct {
		name    string
		input   string
		want    InterruptState
		wantErr bool
	}{
		{
			name:  "valid none",
			input: `{"state":"none"}`,
			want:  InterruptStateNone,
		},
		{
			name:  "valid operator-paused",
			input: `{"state":"operator-paused"}`,
			want:  InterruptStateOperatorPaused,
		},
		{
			name:  "valid operator-stopped-graceful",
			input: `{"state":"operator-stopped-graceful"}`,
			want:  InterruptStateOperatorStoppedGraceful,
		},
		{
			name:  "valid operator-stopped-immediate",
			input: `{"state":"operator-stopped-immediate"}`,
			want:  InterruptStateOperatorStoppedImmediate,
		},
		{
			name:  "valid daemon-crash-suspected",
			input: `{"state":"daemon-crash-suspected"}`,
			want:  InterruptStateDaemonCrashSuspected,
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
			name:    "invalid uppercase NONE",
			input:   `{"state":"NONE"}`,
			wantErr: true,
		},
		{
			name:    "invalid underscore variant",
			input:   `{"state":"operator_paused"}`,
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

func TestInterruptStateMarshalText(t *testing.T) {
	t.Parallel()

	got, err := InterruptStateNone.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "none" {
		t.Errorf("MarshalText = %q, want %q", string(got), "none")
	}

	if _, err := InterruptState("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestInterruptStateRoundTrip(t *testing.T) {
	t.Parallel()

	// JSON round-trip for all five values.
	type wrapper struct {
		State InterruptState `json:"state"`
	}

	values := []InterruptState{
		InterruptStateNone,
		InterruptStateOperatorPaused,
		InterruptStateOperatorStoppedGraceful,
		InterruptStateOperatorStoppedImmediate,
		InterruptStateDaemonCrashSuspected,
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

func TestInterruptStateUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all five declared values.
	var s InterruptState
	err := s.UnmarshalText([]byte("made_up"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"none", "operator-paused", "operator-stopped-graceful",
		"operator-stopped-immediate", "daemon-crash-suspected",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
