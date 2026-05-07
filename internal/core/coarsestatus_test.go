package core

import (
	"encoding/json"
	"testing"
)

func TestCoarseStatusValid(t *testing.T) {
	t.Parallel()

	valid := []CoarseStatus{
		CoarseStatusOpen,
		CoarseStatusInProgress,
		CoarseStatusBlocked,
		CoarseStatusDeferred,
		CoarseStatusDraft,
		CoarseStatusClosed,
		CoarseStatusTombstone,
		CoarseStatusPinned,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []CoarseStatus{
		"",
		"OPEN",
		"Open",
		"IN_PROGRESS",
		"In_Progress",
		"cancelled",
		"parked",
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestCoarseStatusUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Status CoarseStatus `json:"status"`
	}

	tests := []struct {
		name    string
		input   string
		want    CoarseStatus
		wantErr bool
	}{
		{
			name:  "valid open",
			input: `{"status":"open"}`,
			want:  CoarseStatusOpen,
		},
		{
			name:  "valid in_progress",
			input: `{"status":"in_progress"}`,
			want:  CoarseStatusInProgress,
		},
		{
			name:  "valid blocked",
			input: `{"status":"blocked"}`,
			want:  CoarseStatusBlocked,
		},
		{
			name:  "valid deferred",
			input: `{"status":"deferred"}`,
			want:  CoarseStatusDeferred,
		},
		{
			name:  "valid draft",
			input: `{"status":"draft"}`,
			want:  CoarseStatusDraft,
		},
		{
			name:  "valid closed",
			input: `{"status":"closed"}`,
			want:  CoarseStatusClosed,
		},
		{
			name:  "valid tombstone",
			input: `{"status":"tombstone"}`,
			want:  CoarseStatusTombstone,
		},
		{
			name:  "valid pinned",
			input: `{"status":"pinned"}`,
			want:  CoarseStatusPinned,
		},
		{
			name:    "invalid unknown value",
			input:   `{"status":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"status":""}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase OPEN",
			input:   `{"status":"OPEN"}`,
			wantErr: true,
		},
		{
			name:    "invalid cancelled (not in spec)",
			input:   `{"status":"cancelled"}`,
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

func TestCoarseStatusMarshalText(t *testing.T) {
	t.Parallel()

	got, err := CoarseStatusOpen.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "open" {
		t.Errorf("MarshalText = %q, want %q", string(got), "open")
	}

	if _, err := CoarseStatus("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
