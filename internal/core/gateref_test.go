package core

import (
	"encoding/json"
	"testing"
)

// TestGateRefValid verifies that any non-empty string is valid and that an
// empty string is rejected per control-points.md §6.2 / execution-model.md §6.1.
func TestGateRefValid(t *testing.T) {
	t.Parallel()

	valid := []GateRef{
		"gates/g001",
		"approval-gate",
		"quality-gate-1",
		"g",
		"GATE",
	}
	for _, g := range valid {
		if !g.Valid() {
			t.Errorf("expected %q to be valid", g)
		}
	}

	if GateRef("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestGateRefMarshalText verifies MarshalText accepts non-empty values and
// rejects the empty string.
func TestGateRefMarshalText(t *testing.T) {
	t.Parallel()

	got, err := GateRef("gates/g001").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "gates/g001" {
		t.Errorf("MarshalText = %q, want %q", string(got), "gates/g001")
	}

	if _, err := GateRef("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestGateRefUnmarshalText verifies JSON round-trip behaviour and that an
// empty value is rejected.
func TestGateRefUnmarshalText(t *testing.T) {
	t.Parallel()

	type gaterefFixtureWrapper struct {
		Ref GateRef `json:"gate_ref"`
	}

	tests := []struct {
		name    string
		input   string
		want    GateRef
		wantErr bool
	}{
		{name: "named gate", input: `{"gate_ref":"gates/g001"}`, want: "gates/g001"},
		{name: "simple name", input: `{"gate_ref":"approval-gate"}`, want: "approval-gate"},
		{name: "arbitrary non-empty", input: `{"gate_ref":"x"}`, want: "x"},
		{name: "empty rejected", input: `{"gate_ref":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w gaterefFixtureWrapper
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
			if w.Ref != tc.want {
				t.Errorf("got %q, want %q", string(w.Ref), string(tc.want))
			}
		})
	}
}

// TestGateRefRoundTrip verifies a non-empty GateRef survives a
// json.Marshal / json.Unmarshal round-trip.
func TestGateRefRoundTrip(t *testing.T) {
	t.Parallel()

	gaterefFixtureValue := GateRef("gates/approval-gate")

	data, err := json.Marshal(gaterefFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded GateRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != gaterefFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, gaterefFixtureValue)
	}
}
