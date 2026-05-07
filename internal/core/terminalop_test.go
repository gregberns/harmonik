package core

import (
	"encoding/json"
	"testing"
)

func TestTerminalOpValid(t *testing.T) {
	t.Parallel()

	valid := []TerminalOp{
		TerminalOpClaim,
		TerminalOpClose,
		TerminalOpReopen,
	}
	for _, op := range valid {
		if !op.Valid() {
			t.Errorf("expected %q to be valid", op)
		}
	}

	invalid := []TerminalOp{"", "CLAIM", "claimed", "open", "in_progress", "done", "Reopen"}
	for _, op := range invalid {
		if op.Valid() {
			t.Errorf("expected %q to be invalid", op)
		}
	}
}

func TestTerminalOpUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Op TerminalOp `json:"op"`
	}

	tests := []struct {
		name    string
		input   string
		want    TerminalOp
		wantErr bool
	}{
		{
			name:    "valid claim",
			input:   `{"op":"claim"}`,
			want:    TerminalOpClaim,
			wantErr: false,
		},
		{
			name:    "valid close",
			input:   `{"op":"close"}`,
			want:    TerminalOpClose,
			wantErr: false,
		},
		{
			name:    "valid reopen",
			input:   `{"op":"reopen"}`,
			want:    TerminalOpReopen,
			wantErr: false,
		},
		{
			name:    "invalid CLAIM uppercase",
			input:   `{"op":"CLAIM"}`,
			wantErr: true,
		},
		{
			name:    "invalid claimed",
			input:   `{"op":"claimed"}`,
			wantErr: true,
		},
		{
			name:    "invalid open (status not op)",
			input:   `{"op":"open"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"op":""}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
			input:   `{"op":"unknown"}`,
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
			if w.Op != tc.want {
				t.Errorf("got %q, want %q", w.Op, tc.want)
			}
		})
	}
}

func TestTerminalOpMarshalText(t *testing.T) {
	t.Parallel()

	got, err := TerminalOpClaim.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "claim" {
		t.Errorf("MarshalText = %q, want %q", string(got), "claim")
	}

	got, err = TerminalOpClose.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "close" {
		t.Errorf("MarshalText = %q, want %q", string(got), "close")
	}

	got, err = TerminalOpReopen.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "reopen" {
		t.Errorf("MarshalText = %q, want %q", string(got), "reopen")
	}

	if _, err := TerminalOp("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
