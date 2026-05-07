package core

import (
	"encoding/json"
	"testing"
)

func TestIdempotencyClassValid(t *testing.T) {
	t.Parallel()

	valid := []IdempotencyClass{
		IdempotencyClassIdempotent,
		IdempotencyClassNonIdempotent,
		IdempotencyClassRecoverableNonIdempotent,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("expected %q to be valid", c)
		}
	}

	invalid := []IdempotencyClass{
		"",
		"non_idempotent",
		"recoverable_non_idempotent",
		"IDEMPOTENT",
		"NonIdempotent",
		"RETRY",
		"unknown",
		"Idempotent",
	}
	for _, c := range invalid {
		if c.Valid() {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestIdempotencyClassUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Class IdempotencyClass `json:"class"`
	}

	tests := []struct {
		name    string
		input   string
		want    IdempotencyClass
		wantErr bool
	}{
		{
			name:    "valid idempotent",
			input:   `{"class":"idempotent"}`,
			want:    IdempotencyClassIdempotent,
			wantErr: false,
		},
		{
			name:    "valid non-idempotent",
			input:   `{"class":"non-idempotent"}`,
			want:    IdempotencyClassNonIdempotent,
			wantErr: false,
		},
		{
			name:    "valid recoverable-non-idempotent",
			input:   `{"class":"recoverable-non-idempotent"}`,
			want:    IdempotencyClassRecoverableNonIdempotent,
			wantErr: false,
		},
		{
			name:    "invalid underscore variant non_idempotent",
			input:   `{"class":"non_idempotent"}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase",
			input:   `{"class":"IDEMPOTENT"}`,
			wantErr: true,
		},
		{
			name:    "invalid RETRY",
			input:   `{"class":"RETRY"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"class":""}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
			input:   `{"class":"unknown"}`,
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
			if w.Class != tc.want {
				t.Errorf("got %q, want %q", w.Class, tc.want)
			}
		})
	}
}

func TestIdempotencyClassMarshalText(t *testing.T) {
	t.Parallel()

	got, err := IdempotencyClassIdempotent.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "idempotent" {
		t.Errorf("MarshalText = %q, want %q", string(got), "idempotent")
	}

	got, err = IdempotencyClassNonIdempotent.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "non-idempotent" {
		t.Errorf("MarshalText = %q, want %q", string(got), "non-idempotent")
	}

	got, err = IdempotencyClassRecoverableNonIdempotent.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "recoverable-non-idempotent" {
		t.Errorf("MarshalText = %q, want %q", string(got), "recoverable-non-idempotent")
	}

	if _, err := IdempotencyClass("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
