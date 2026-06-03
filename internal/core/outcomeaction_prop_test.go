package core

// Property tests for OutcomeAction using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. MarshalText/UnmarshalText round-trip: every declared OutcomeAction
//     marshals to its string representation and unmarshals back identically.
//
//  2. Invalid values rejected: strings not in the declared set return errors
//     from both MarshalText and UnmarshalText.
//
// See outcomeaction.go and control-points.md §4.2–§4.5.

import (
	"testing"

	"pgregory.net/rapid"
)

var allOutcomeActions = []OutcomeAction{
	OutcomeActionAllow,
	OutcomeActionDeny,
	OutcomeActionEscalateToHuman,
	OutcomeActionSideEffect,
	OutcomeActionReorder,
	OutcomeActionAdmit,
	OutcomeActionWarn,
}

// TestProp_OutcomeAction_ValidMarshalRoundTrip checks that every declared
// OutcomeAction survives a MarshalText → UnmarshalText round-trip unchanged.
func TestProp_OutcomeAction_ValidMarshalRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a := rapid.SampledFrom(allOutcomeActions).Draw(rt, "action")

		text, err := a.MarshalText()
		if err != nil {
			rt.Fatalf("MarshalText(%q) failed: %v", a, err)
		}

		var recovered OutcomeAction
		if err := recovered.UnmarshalText(text); err != nil {
			rt.Fatalf("UnmarshalText(%q) failed: %v", string(text), err)
		}

		if recovered != a {
			rt.Errorf("round-trip mismatch: got %q, want %q", recovered, a)
		}
	})
}

// TestProp_OutcomeAction_InvalidRejected checks that strings outside the
// declared set are rejected by both MarshalText and UnmarshalText.
func TestProp_OutcomeAction_InvalidRejected(t *testing.T) {
	validSet := make(map[string]bool, len(allOutcomeActions))
	for _, a := range allOutcomeActions {
		validSet[string(a)] = true
	}

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 64, -1).Draw(rt, "raw")
		if validSet[raw] {
			rt.Skip("valid value, skipping rejection test")
		}

		a := OutcomeAction(raw)
		if _, err := a.MarshalText(); err == nil {
			rt.Errorf("MarshalText(%q): expected error for invalid value, got nil", raw)
		}

		var out OutcomeAction
		if err := out.UnmarshalText([]byte(raw)); err == nil {
			rt.Errorf("UnmarshalText(%q): expected error for invalid value, got nil", raw)
		}
	})
}
