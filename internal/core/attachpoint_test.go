package core

import (
	"encoding/json"
	"testing"
)

func TestAttachPointValid(t *testing.T) {
	t.Parallel()

	valid := []AttachPoint{
		AttachPointNodePreEntry,
		AttachPointNodePostExit,
		AttachPointEdgeBeforeSelection,
		AttachPointEdgeAfterSelection,
	}
	for _, ap := range valid {
		if !ap.Valid() {
			t.Errorf("expected %q to be valid", ap)
		}
	}

	invalid := []AttachPoint{
		"",
		"node-pre-Entry",
		"NODE-PRE-ENTRY",
		"node-post-exit-extra",
		"edge-before",
		"edge-after",
		"edge-before-Selection",
		"unknown",
		"node-pre-entry|node-post-exit",
	}
	for _, ap := range invalid {
		if ap.Valid() {
			t.Errorf("expected %q to be invalid", ap)
		}
	}
}

func TestAttachPointMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ap   AttachPoint
		want string
	}{
		{AttachPointNodePreEntry, "node-pre-entry"},
		{AttachPointNodePostExit, "node-post-exit"},
		{AttachPointEdgeBeforeSelection, "edge-before-selection"},
		{AttachPointEdgeAfterSelection, "edge-after-selection"},
	}
	for _, tc := range tests {
		got, err := tc.ap.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.ap, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.ap, string(got), tc.want)
		}
	}

	if _, err := AttachPoint("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := AttachPoint("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestAttachPointUnmarshalText(t *testing.T) {
	t.Parallel()

	type attachPointFixtureWrapper struct {
		AP AttachPoint `json:"attach_point"`
	}

	tests := []struct {
		name    string
		input   string
		want    AttachPoint
		wantErr bool
	}{
		{
			name:  "node-pre-entry",
			input: `{"attach_point":"node-pre-entry"}`,
			want:  AttachPointNodePreEntry,
		},
		{
			name:  "node-post-exit",
			input: `{"attach_point":"node-post-exit"}`,
			want:  AttachPointNodePostExit,
		},
		{
			name:  "edge-before-selection",
			input: `{"attach_point":"edge-before-selection"}`,
			want:  AttachPointEdgeBeforeSelection,
		},
		{
			name:  "edge-after-selection",
			input: `{"attach_point":"edge-after-selection"}`,
			want:  AttachPointEdgeAfterSelection,
		},
		{
			name:    "wrong case rejected",
			input:   `{"attach_point":"Node-Pre-Entry"}`,
			wantErr: true,
		},
		{
			name:    "uppercase rejected",
			input:   `{"attach_point":"NODE-PRE-ENTRY"}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"attach_point":"node-pre"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"attach_point":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"attach_point":""}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w attachPointFixtureWrapper
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
			if w.AP != tc.want {
				t.Errorf("got %q, want %q", string(w.AP), string(tc.want))
			}
		})
	}
}
