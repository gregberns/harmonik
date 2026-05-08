package core

import (
	"encoding/json"
	"testing"
)

func TestKindValid(t *testing.T) {
	t.Parallel()

	valid := []Kind{
		KindGate,
		KindHook,
		KindGuard,
		KindBudget,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("expected %q to be valid", k)
		}
	}

	invalid := []Kind{
		"",
		"gate",
		"GATE",
		"hook",
		"HOOK",
		"guard",
		"GUARD",
		"budget",
		"BUDGET",
		"unknown",
		"gate|hook",
	}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestKindMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind Kind
		want string
	}{
		{KindGate, "Gate"},
		{KindHook, "Hook"},
		{KindGuard, "Guard"},
		{KindBudget, "Budget"},
	}
	for _, tc := range tests {
		got, err := tc.kind.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.kind, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.kind, string(got), tc.want)
		}
	}

	if _, err := Kind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := Kind("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestKindUnmarshalText(t *testing.T) {
	t.Parallel()

	type kindFixtureWrapper struct {
		Kind Kind `json:"kind"`
	}

	tests := []struct {
		name    string
		input   string
		want    Kind
		wantErr bool
	}{
		{
			name:  "Gate",
			input: `{"kind":"Gate"}`,
			want:  KindGate,
		},
		{
			name:  "Hook",
			input: `{"kind":"Hook"}`,
			want:  KindHook,
		},
		{
			name:  "Guard",
			input: `{"kind":"Guard"}`,
			want:  KindGuard,
		},
		{
			name:  "Budget",
			input: `{"kind":"Budget"}`,
			want:  KindBudget,
		},
		{
			name:    "lowercase gate rejected",
			input:   `{"kind":"gate"}`,
			wantErr: true,
		},
		{
			name:    "uppercase GATE rejected",
			input:   `{"kind":"GATE"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"kind":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"kind":""}`,
			wantErr: true,
		},
		{
			name:    "partial match guard rejected",
			input:   `{"kind":"Guar"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w kindFixtureWrapper
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
			if w.Kind != tc.want {
				t.Errorf("got %q, want %q", string(w.Kind), string(tc.want))
			}
		})
	}
}
