package core

import (
	"encoding/json"
	"testing"
)

func TestHarmonikWriteStatusValid(t *testing.T) {
	t.Parallel()

	valid := []HarmonikWriteStatus{
		HarmonikWriteStatusOpen,
		HarmonikWriteStatusInProgress,
		HarmonikWriteStatusClosed,
		HarmonikWriteStatusDeferred,
		HarmonikWriteStatusTombstone,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []HarmonikWriteStatus{
		"",
		"OPEN",
		"IN_PROGRESS",
		"Open",
		"Closed",
		"blocked", // in CoarseStatus read surface but NOT the write subset
		"draft",
		"pinned",
		"unknown",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestHarmonikWriteStatusUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Status HarmonikWriteStatus `json:"status"`
	}

	tests := []struct {
		name    string
		input   string
		want    HarmonikWriteStatus
		wantErr bool
	}{
		{
			name:    "valid open",
			input:   `{"status":"open"}`,
			want:    HarmonikWriteStatusOpen,
			wantErr: false,
		},
		{
			name:    "valid in_progress",
			input:   `{"status":"in_progress"}`,
			want:    HarmonikWriteStatusInProgress,
			wantErr: false,
		},
		{
			name:    "valid closed",
			input:   `{"status":"closed"}`,
			want:    HarmonikWriteStatusClosed,
			wantErr: false,
		},
		{
			name:    "valid deferred",
			input:   `{"status":"deferred"}`,
			want:    HarmonikWriteStatusDeferred,
			wantErr: false,
		},
		{
			name:    "valid tombstone",
			input:   `{"status":"tombstone"}`,
			want:    HarmonikWriteStatusTombstone,
			wantErr: false,
		},
		{
			name:    "invalid blocked (read-surface only)",
			input:   `{"status":"blocked"}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase OPEN",
			input:   `{"status":"OPEN"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"status":""}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
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

func TestHarmonikWriteStatusMarshalText(t *testing.T) {
	t.Parallel()

	got, err := HarmonikWriteStatusOpen.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "open" {
		t.Errorf("MarshalText = %q, want %q", string(got), "open")
	}

	// valid values round-trip correctly
	validCases := []struct {
		status HarmonikWriteStatus
		want   string
	}{
		{HarmonikWriteStatusOpen, "open"},
		{HarmonikWriteStatusInProgress, "in_progress"},
		{HarmonikWriteStatusClosed, "closed"},
		{HarmonikWriteStatusDeferred, "deferred"},
		{HarmonikWriteStatusTombstone, "tombstone"},
	}
	for _, tc := range validCases {
		b, err := tc.status.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%q) error: %v", tc.status, err)
			continue
		}
		if string(b) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.status, string(b), tc.want)
		}
	}

	// bogus path: invalid value must error
	if _, err := HarmonikWriteStatus("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

// TestHarmonikWriteStatusSubsetInvariant asserts that every HarmonikWriteStatus
// value is a valid CoarseStatus (BI-007: write surface is a strict subset of the
// read surface).  If either enum is mutated without updating the other, this test
// will fail at compile / test time, surfacing the drift.
func TestHarmonikWriteStatusSubsetInvariant(t *testing.T) {
	t.Parallel()

	writeValues := []HarmonikWriteStatus{
		HarmonikWriteStatusOpen,
		HarmonikWriteStatusInProgress,
		HarmonikWriteStatusClosed,
		HarmonikWriteStatusDeferred,
		HarmonikWriteStatusTombstone,
	}
	for _, s := range writeValues {
		cs := s.CoarseStatus()
		if !cs.Valid() {
			t.Errorf("HarmonikWriteStatus %q converted to CoarseStatus %q which is not valid; subset invariant broken", s, cs)
		}
	}
}

// TestHarmonikWriteStatusRoundTrip asserts that every HarmonikWriteStatus value
// survives a round-trip through CoarseStatus and back via AsHarmonikWriteStatus.
func TestHarmonikWriteStatusRoundTrip(t *testing.T) {
	t.Parallel()

	writeValues := []HarmonikWriteStatus{
		HarmonikWriteStatusOpen,
		HarmonikWriteStatusInProgress,
		HarmonikWriteStatusClosed,
		HarmonikWriteStatusDeferred,
		HarmonikWriteStatusTombstone,
	}
	for _, s := range writeValues {
		got, ok := s.CoarseStatus().AsHarmonikWriteStatus()
		if !ok {
			t.Errorf("AsHarmonikWriteStatus(%q) returned false; expected true", s)
			continue
		}
		if got != s {
			t.Errorf("AsHarmonikWriteStatus(%q) = %q, want %q", s, got, s)
		}
	}
}

// TestCoarseStatusAsHarmonikWriteStatusRejectNonSubset asserts that read-only
// CoarseStatus values (blocked, draft, pinned) are rejected by AsHarmonikWriteStatus.
func TestCoarseStatusAsHarmonikWriteStatusRejectNonSubset(t *testing.T) {
	t.Parallel()

	readOnly := []CoarseStatus{
		CoarseStatusBlocked,
		CoarseStatusDraft,
		CoarseStatusPinned,
	}
	for _, c := range readOnly {
		got, ok := c.AsHarmonikWriteStatus()
		if ok {
			t.Errorf("AsHarmonikWriteStatus(%q) returned (%q, true); expected (\"\", false)", c, got)
		}
		if got != "" {
			t.Errorf("AsHarmonikWriteStatus(%q) returned non-empty zero value %q", c, got)
		}
	}
}

// TestCoarseStatusAsHarmonikWriteStatusRejectUnknown asserts that an unknown
// CoarseStatus literal is rejected by AsHarmonikWriteStatus.
func TestCoarseStatusAsHarmonikWriteStatusRejectUnknown(t *testing.T) {
	t.Parallel()

	unknown := CoarseStatus("something_future_beads_adds")
	got, ok := unknown.AsHarmonikWriteStatus()
	if ok {
		t.Errorf("AsHarmonikWriteStatus(%q) returned (%q, true); expected (\"\", false)", unknown, got)
	}
	if got != "" {
		t.Errorf("AsHarmonikWriteStatus(%q) returned non-empty zero value %q", unknown, got)
	}
}
