package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDaemonStatusValid(t *testing.T) {
	t.Parallel()

	valid := []DaemonStatus{
		DaemonStatusStarting,
		DaemonStatusReconciling,
		DaemonStatusDegraded,
		DaemonStatusReady,
		DaemonStatusPaused,
		DaemonStatusDraining,
		DaemonStatusStopped,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []DaemonStatus{
		"",
		"made_up",
		"Starting",
		"READY",
		"stopped_",        // trailing underscore
		"drain",           // prefix of draining
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestDaemonStatusUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Status DaemonStatus `json:"status"`
	}

	tests := []struct {
		name    string
		input   string
		want    DaemonStatus
		wantErr bool
	}{
		{
			name:  "valid starting",
			input: `{"status":"starting"}`,
			want:  DaemonStatusStarting,
		},
		{
			name:  "valid reconciling",
			input: `{"status":"reconciling"}`,
			want:  DaemonStatusReconciling,
		},
		{
			name:  "valid degraded",
			input: `{"status":"degraded"}`,
			want:  DaemonStatusDegraded,
		},
		{
			name:  "valid ready",
			input: `{"status":"ready"}`,
			want:  DaemonStatusReady,
		},
		{
			name:  "valid paused",
			input: `{"status":"paused"}`,
			want:  DaemonStatusPaused,
		},
		{
			name:  "valid draining",
			input: `{"status":"draining"}`,
			want:  DaemonStatusDraining,
		},
		{
			name:  "valid stopped",
			input: `{"status":"stopped"}`,
			want:  DaemonStatusStopped,
		},
		{
			name:    "invalid made_up",
			input:   `{"status":"made_up"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"status":""}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase READY",
			input:   `{"status":"READY"}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown",
			input:   `{"status":"unknown"}`,
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
			if w.Status != tc.want {
				t.Errorf("got %q, want %q", w.Status, tc.want)
			}
		})
	}
}

func TestDaemonStatusMarshalText(t *testing.T) {
	t.Parallel()

	got, err := DaemonStatusReady.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "ready" {
		t.Errorf("MarshalText = %q, want %q", string(got), "ready")
	}

	if _, err := DaemonStatus("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestDaemonStatusRoundTrip(t *testing.T) {
	t.Parallel()

	// JSON round-trip for all seven values.
	type wrapper struct {
		Status DaemonStatus `json:"status"`
	}

	values := []DaemonStatus{
		DaemonStatusStarting,
		DaemonStatusReconciling,
		DaemonStatusDegraded,
		DaemonStatusReady,
		DaemonStatusPaused,
		DaemonStatusDraining,
		DaemonStatusStopped,
	}

	for _, s := range values {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			in := wrapper{Status: s}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", s, err)
			}
			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.Status != s {
				t.Errorf("round-trip: got %q, want %q", out.Status, s)
			}
		})
	}
}

func TestDaemonStatusUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all seven declared values.
	var s DaemonStatus
	err := s.UnmarshalText([]byte("made_up"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"starting", "reconciling", "degraded",
		"ready", "paused", "draining", "stopped",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
