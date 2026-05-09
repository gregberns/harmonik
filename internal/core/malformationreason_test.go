package core

import (
	"encoding/json"
	"testing"
)

func TestMalformationReasonValid(t *testing.T) {
	t.Parallel()

	valid := []MalformationReason{
		MalformationReasonUnknownVerdictValue,
		MalformationReasonMissingRequiredField,
		MalformationReasonExtraFields,
		MalformationReasonWrongType,
		MalformationReasonMultipleVerdicts,
		MalformationReasonVerdictAfterTerminal,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	invalid := []MalformationReason{
		"",
		"Unknown-Verdict-Value",
		"UNKNOWN-VERDICT-VALUE",
		"unknownVerdictValue",
		"unknown_verdict_value",
		"missing_required_field",
		"MissingRequiredField",
		"extraFields",
		"extra_fields",
		"wrongType",
		"wrong_type",
		"multipleVerdicts",
		"multiple_verdicts",
		"verdictAfterTerminal",
		"verdict_after_terminal",
		"bogus",
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}

func TestMalformationReasonMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason MalformationReason
		want   string
	}{
		{MalformationReasonUnknownVerdictValue, "unknown-verdict-value"},
		{MalformationReasonMissingRequiredField, "missing-required-field"},
		{MalformationReasonExtraFields, "extra-fields"},
		{MalformationReasonWrongType, "wrong-type"},
		{MalformationReasonMultipleVerdicts, "multiple-verdicts"},
		{MalformationReasonVerdictAfterTerminal, "verdict-after-terminal"},
	}
	for _, tc := range tests {
		got, err := tc.reason.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.reason, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.reason, string(got), tc.want)
		}
	}

	if _, err := MalformationReason("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := MalformationReason("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestMalformationReasonUnmarshalText(t *testing.T) {
	t.Parallel()

	type malformationReasonFixtureWrapper struct {
		Reason MalformationReason `json:"reason"`
	}

	tests := []struct {
		name    string
		input   string
		want    MalformationReason
		wantErr bool
	}{
		{
			name:  "unknown-verdict-value",
			input: `{"reason":"unknown-verdict-value"}`,
			want:  MalformationReasonUnknownVerdictValue,
		},
		{
			name:  "missing-required-field",
			input: `{"reason":"missing-required-field"}`,
			want:  MalformationReasonMissingRequiredField,
		},
		{
			name:  "extra-fields",
			input: `{"reason":"extra-fields"}`,
			want:  MalformationReasonExtraFields,
		},
		{
			name:  "wrong-type",
			input: `{"reason":"wrong-type"}`,
			want:  MalformationReasonWrongType,
		},
		{
			name:  "multiple-verdicts",
			input: `{"reason":"multiple-verdicts"}`,
			want:  MalformationReasonMultipleVerdicts,
		},
		{
			name:  "verdict-after-terminal",
			input: `{"reason":"verdict-after-terminal"}`,
			want:  MalformationReasonVerdictAfterTerminal,
		},
		{
			name:    "mixed-case UnknownVerdictValue rejected",
			input:   `{"reason":"Unknown-Verdict-Value"}`,
			wantErr: true,
		},
		{
			name:    "camelCase unknownVerdictValue rejected",
			input:   `{"reason":"unknownVerdictValue"}`,
			wantErr: true,
		},
		{
			name:    "underscore form unknown_verdict_value rejected",
			input:   `{"reason":"unknown_verdict_value"}`,
			wantErr: true,
		},
		{
			name:    "camelCase missingRequiredField rejected",
			input:   `{"reason":"missingRequiredField"}`,
			wantErr: true,
		},
		{
			name:    "camelCase extraFields rejected",
			input:   `{"reason":"extraFields"}`,
			wantErr: true,
		},
		{
			name:    "camelCase wrongType rejected",
			input:   `{"reason":"wrongType"}`,
			wantErr: true,
		},
		{
			name:    "camelCase multipleVerdicts rejected",
			input:   `{"reason":"multipleVerdicts"}`,
			wantErr: true,
		},
		{
			name:    "camelCase verdictAfterTerminal rejected",
			input:   `{"reason":"verdictAfterTerminal"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"reason":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"reason":""}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"reason":"extra"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w malformationReasonFixtureWrapper
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
			if w.Reason != tc.want {
				t.Errorf("got %q, want %q", string(w.Reason), string(tc.want))
			}
		})
	}
}
