package core

import (
	"encoding/json"
	"testing"
)

// cp001_conformance_test.go — Conformance tests for CP-001
//
// specs/control-points.md §4.1.CP-001:
//
//	A ControlPoint MUST be a single typed record (one Go struct, one lifecycle,
//	one registration path) parameterized by kind ∈ {Gate, Hook, Guard, Budget}.
//	Common fields: name, kind, trigger, evaluator, outcome_action, plus a
//	Kind-specific typed payload per §4.2–§4.5. The unification is in the
//	primitive's shape; the semantics per Kind are explicit and NOT interchangeable.
//
// These tests verify:
//  1. All four Kinds produce a valid ControlPoint through the same struct type.
//  2. The ControlPoint JSON round-trips correctly for all four Kinds — confirming
//     that the unified type encodes/decodes per-Kind payloads correctly through
//     the KindPayload discriminated union.
//  3. After a JSON round-trip, Kind discrimination still works: only the
//     payload field matching the Kind is non-nil.

// TestCP001_JSONRoundTrip_AllKinds verifies that a ControlPoint for each Kind
// survives a JSON encode → decode cycle with fields intact, confirming that the
// single struct type encodes all four Kinds correctly.
//
// specs/control-points.md §4.1.CP-001 (single typed record, one lifecycle,
// one registration path); §6.1 (JSON field names and omitempty rules).
func TestCP001_JSONRoundTrip_AllKinds(t *testing.T) {
	t.Parallel()

	kinds := []Kind{KindGate, KindHook, KindGuard, KindBudget}
	for _, k := range kinds {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			original := registryFixtureControlPoint(t, k)
			if !original.Valid() {
				t.Fatalf("fixture ControlPoint{Kind=%q} is invalid before round-trip", k)
			}

			// Encode.
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("json.Marshal ControlPoint{Kind=%q}: %v", k, err)
			}

			// Decode into a fresh value of the same struct type.
			var decoded ControlPoint
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal ControlPoint{Kind=%q}: %v", k, err)
			}

			// The decoded value must still be valid.
			if !decoded.Valid() {
				t.Errorf("decoded ControlPoint{Kind=%q}.Valid() = false, want true", k)
			}

			// Core identity fields must survive the round-trip.
			if decoded.Name != original.Name {
				t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
			}
			if decoded.Kind != original.Kind {
				t.Errorf("Kind: got %q, want %q", decoded.Kind, original.Kind)
			}
			if decoded.OutcomeAction != original.OutcomeAction {
				t.Errorf("OutcomeAction: got %q, want %q", decoded.OutcomeAction, original.OutcomeAction)
			}
			if decoded.SchemaVersion != original.SchemaVersion {
				t.Errorf("SchemaVersion: got %d, want %d", decoded.SchemaVersion, original.SchemaVersion)
			}
			if decoded.ModeTag != original.ModeTag {
				t.Errorf("ModeTag: got %q, want %q", decoded.ModeTag, original.ModeTag)
			}
		})
	}
}

// TestCP001_KindPayloadDiscriminatorAfterRoundTrip verifies that after a JSON
// round-trip, exactly the payload field matching Kind is non-nil and the
// remaining three fields remain nil — confirming the KindPayload discriminated
// union survives serialisation correctly.
//
// specs/control-points.md §4.1.CP-001 (Kind-specific typed payload per §4.2–§4.5);
// §6.1 (KindPayload: exactly one field non-nil, matching Kind).
func TestCP001_KindPayloadDiscriminatorAfterRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind                                      Kind
		wantGate, wantHook, wantGuard, wantBudget bool
	}{
		{KindGate, true, false, false, false},
		{KindHook, false, true, false, false},
		{KindGuard, false, false, true, false},
		{KindBudget, false, false, false, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()

			original := registryFixtureControlPoint(t, tc.kind)

			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			var decoded ControlPoint
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			if (decoded.Payload.Gate != nil) != tc.wantGate {
				t.Errorf("Payload.Gate non-nil = %v, want %v", decoded.Payload.Gate != nil, tc.wantGate)
			}
			if (decoded.Payload.Hook != nil) != tc.wantHook {
				t.Errorf("Payload.Hook non-nil = %v, want %v", decoded.Payload.Hook != nil, tc.wantHook)
			}
			if (decoded.Payload.Guard != nil) != tc.wantGuard {
				t.Errorf("Payload.Guard non-nil = %v, want %v", decoded.Payload.Guard != nil, tc.wantGuard)
			}
			if (decoded.Payload.Budget != nil) != tc.wantBudget {
				t.Errorf("Payload.Budget non-nil = %v, want %v", decoded.Payload.Budget != nil, tc.wantBudget)
			}
		})
	}
}

// TestCP001_SingleTypeHomogeneousSlice verifies that all four Kinds can be held
// in a single []ControlPoint, demonstrating the "one Go struct" property of
// CP-001 at the language level.
//
// specs/control-points.md §4.1.CP-001: "one Go struct, one lifecycle, one
// registration path."
func TestCP001_SingleTypeHomogeneousSlice(t *testing.T) {
	t.Parallel()

	// All four Kinds stored in the same slice type — this would not compile if
	// ControlPoint were four separate types.
	all := []ControlPoint{
		registryFixtureControlPoint(t, KindGate),
		registryFixtureControlPoint(t, KindHook),
		registryFixtureControlPoint(t, KindGuard),
		registryFixtureControlPoint(t, KindBudget),
	}

	if len(all) != 4 {
		t.Fatalf("expected 4 ControlPoints, got %d", len(all))
	}

	// Every element must be valid.
	for _, cp := range all {
		if !cp.Valid() {
			t.Errorf("ControlPoint{Kind=%q} in homogeneous slice is invalid", cp.Kind)
		}
	}

	// Each element's Kind must match its payload field.
	for _, cp := range all {
		if !cp.Payload.ValidForKind(cp.Kind) {
			t.Errorf("ControlPoint{Kind=%q} payload does not match kind after slice storage", cp.Kind)
		}
	}
}
