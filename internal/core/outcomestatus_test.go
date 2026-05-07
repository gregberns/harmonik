package core

import (
	"encoding/json"
	"testing"
)

func TestOutcomeStatusValid(t *testing.T) {
	t.Parallel()

	valid := []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
		OutcomeStatusPartialSuccess,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []OutcomeStatus{
		"",
		"success",
		"fail",
		"retry",
		"partial_success",
		"PARTIAL-SUCCESS",
		"unknown",
		"DONE",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestOutcomeStatusUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Status OutcomeStatus `json:"status"`
	}

	tests := []struct {
		name    string
		input   string
		want    OutcomeStatus
		wantErr bool
	}{
		{
			name:    "valid SUCCESS",
			input:   `{"status":"SUCCESS"}`,
			want:    OutcomeStatusSuccess,
			wantErr: false,
		},
		{
			name:    "valid FAIL",
			input:   `{"status":"FAIL"}`,
			want:    OutcomeStatusFail,
			wantErr: false,
		},
		{
			name:    "valid RETRY",
			input:   `{"status":"RETRY"}`,
			want:    OutcomeStatusRetry,
			wantErr: false,
		},
		{
			name:    "valid PARTIAL_SUCCESS",
			input:   `{"status":"PARTIAL_SUCCESS"}`,
			want:    OutcomeStatusPartialSuccess,
			wantErr: false,
		},
		{
			name:    "invalid lowercase success",
			input:   `{"status":"success"}`,
			wantErr: true,
		},
		{
			name:    "invalid lowercase fail",
			input:   `{"status":"fail"}`,
			wantErr: true,
		},
		{
			name:    "invalid PARTIAL-SUCCESS hyphen",
			input:   `{"status":"PARTIAL-SUCCESS"}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
			input:   `{"status":"DONE"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"status":""}`,
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

func TestOutcomeStatusMarshalText(t *testing.T) {
	t.Parallel()

	got, err := OutcomeStatusSuccess.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "SUCCESS" {
		t.Errorf("MarshalText = %q, want %q", string(got), "SUCCESS")
	}

	got, err = OutcomeStatusPartialSuccess.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "PARTIAL_SUCCESS" {
		t.Errorf("MarshalText = %q, want %q", string(got), "PARTIAL_SUCCESS")
	}

	if _, err := OutcomeStatus("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
