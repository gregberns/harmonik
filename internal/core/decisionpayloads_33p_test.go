package core

import (
	"encoding/json"
	"testing"
)

// TestDecisionNeededPayloadValid covers the Valid() contract for
// DecisionNeededPayload (hitl-decisions SPEC §1.1): question required, options
// ≥1; context_link / blocked_agent / value_requested optional.
func TestDecisionNeededPayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     DecisionNeededPayload
		valid bool
	}{
		{
			name:  "minimal valid",
			p:     DecisionNeededPayload{Question: "ship it?", Options: []string{"yes"}},
			valid: true,
		},
		{
			name: "valid with all optional fields",
			p: DecisionNeededPayload{
				Question:       "which approach?",
				Options:        []string{"A", "B", "C"},
				ContextLink:    "hk-33p",
				BlockedAgent:   "agent-a",
				ValueRequested: true,
			},
			valid: true,
		},
		{
			name:  "missing question",
			p:     DecisionNeededPayload{Question: "", Options: []string{"yes"}},
			valid: false,
		},
		{
			name:  "nil options",
			p:     DecisionNeededPayload{Question: "ship it?", Options: nil},
			valid: false,
		},
		{
			name:  "empty options",
			p:     DecisionNeededPayload{Question: "ship it?", Options: []string{}},
			valid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestDecisionResolvedPayloadValid covers the Valid() contract for
// DecisionResolvedPayload (hitl-decisions SPEC §1.2): decision_id and
// chosen_option required; value / resolver optional.
func TestDecisionResolvedPayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     DecisionResolvedPayload
		valid bool
	}{
		{
			name:  "minimal valid",
			p:     DecisionResolvedPayload{DecisionID: "01900000-0000-7000-8000-000000000001", ChosenOption: "yes"},
			valid: true,
		},
		{
			name: "valid with optional fields",
			p: DecisionResolvedPayload{
				DecisionID:   "01900000-0000-7000-8000-000000000001",
				ChosenOption: "A",
				Value:        "with caveats",
				Resolver:     "operator",
			},
			valid: true,
		},
		{
			name:  "missing decision_id",
			p:     DecisionResolvedPayload{DecisionID: "", ChosenOption: "yes"},
			valid: false,
		},
		{
			name:  "missing chosen_option",
			p:     DecisionResolvedPayload{DecisionID: "01900000-0000-7000-8000-000000000001", ChosenOption: ""},
			valid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestDecisionWithdrawnPayloadValid covers the Valid() contract for
// DecisionWithdrawnPayload (hitl-decisions SPEC §1.3): decision_id required,
// reason required and ∈ {self_obsoleted, orphaned}; by optional.
func TestDecisionWithdrawnPayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     DecisionWithdrawnPayload
		valid bool
	}{
		{
			name:  "valid self_obsoleted",
			p:     DecisionWithdrawnPayload{DecisionID: "01900000-0000-7000-8000-000000000001", Reason: DecisionWithdrawnReasonSelfObsoleted, By: "agent-a"},
			valid: true,
		},
		{
			name:  "valid orphaned",
			p:     DecisionWithdrawnPayload{DecisionID: "01900000-0000-7000-8000-000000000001", Reason: DecisionWithdrawnReasonOrphaned, By: "keeper"},
			valid: true,
		},
		{
			name:  "valid without by",
			p:     DecisionWithdrawnPayload{DecisionID: "01900000-0000-7000-8000-000000000001", Reason: DecisionWithdrawnReasonSelfObsoleted},
			valid: true,
		},
		{
			name:  "missing decision_id",
			p:     DecisionWithdrawnPayload{DecisionID: "", Reason: DecisionWithdrawnReasonOrphaned},
			valid: false,
		},
		{
			name:  "missing reason",
			p:     DecisionWithdrawnPayload{DecisionID: "01900000-0000-7000-8000-000000000001", Reason: ""},
			valid: false,
		},
		{
			name:  "bad reason",
			p:     DecisionWithdrawnPayload{DecisionID: "01900000-0000-7000-8000-000000000001", Reason: "bogus"},
			valid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestDecisionWithdrawnReason_Valid covers the reason discriminator.
func TestDecisionWithdrawnReason_Valid(t *testing.T) {
	t.Parallel()

	if !DecisionWithdrawnReasonSelfObsoleted.Valid() {
		t.Error("self_obsoleted should be valid")
	}
	if !DecisionWithdrawnReasonOrphaned.Valid() {
		t.Error("orphaned should be valid")
	}
	if DecisionWithdrawnReason("bogus").Valid() {
		t.Error("bogus should not be valid")
	}
	if DecisionWithdrawnReason("").Valid() {
		t.Error("empty should not be valid")
	}
}

// TestDecisionPayloads_RegistryRoundTrip verifies that decision_needed,
// decision_resolved, and decision_withdrawn are registered in the global event
// registry and that a marshal → DecodePayload round-trip restores the original
// payload (hitl-decisions SPEC §1; component K1 acceptance criterion).
func TestDecisionPayloads_RegistryRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("decision_needed", func(t *testing.T) {
		t.Parallel()

		want := DecisionNeededPayload{
			Question:       "ship the release?",
			Options:        []string{"yes", "no", "wait"},
			ContextLink:    "hk-33p",
			BlockedAgent:   "agent-a",
			ValueRequested: true,
		}

		payloadJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		evt := Event{Type: "decision_needed", Payload: payloadJSON}
		decoded, err := evt.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}

		got, ok := decoded.(*DecisionNeededPayload)
		if !ok {
			t.Fatalf("DecodePayload returned %T; want *DecisionNeededPayload", decoded)
		}
		if got.Question != want.Question || got.ContextLink != want.ContextLink ||
			got.BlockedAgent != want.BlockedAgent || got.ValueRequested != want.ValueRequested {
			t.Errorf("round-trip mismatch: got %+v; want %+v", got, want)
		}
		if len(got.Options) != len(want.Options) {
			t.Fatalf("options length: got %d; want %d", len(got.Options), len(want.Options))
		}
		for i := range want.Options {
			if got.Options[i] != want.Options[i] {
				t.Errorf("options[%d]: got %q; want %q", i, got.Options[i], want.Options[i])
			}
		}
	})

	t.Run("decision_resolved", func(t *testing.T) {
		t.Parallel()

		want := DecisionResolvedPayload{
			DecisionID:   "01900000-0000-7000-8000-000000000001",
			ChosenOption: "yes",
			Value:        "ship it",
			Resolver:     "operator",
		}

		payloadJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		evt := Event{Type: "decision_resolved", Payload: payloadJSON}
		decoded, err := evt.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}

		got, ok := decoded.(*DecisionResolvedPayload)
		if !ok {
			t.Fatalf("DecodePayload returned %T; want *DecisionResolvedPayload", decoded)
		}
		if got.DecisionID != want.DecisionID || got.ChosenOption != want.ChosenOption ||
			got.Value != want.Value || got.Resolver != want.Resolver {
			t.Errorf("round-trip mismatch: got %+v; want %+v", got, want)
		}
	})

	t.Run("decision_withdrawn", func(t *testing.T) {
		t.Parallel()

		want := DecisionWithdrawnPayload{
			DecisionID: "01900000-0000-7000-8000-000000000001",
			Reason:     DecisionWithdrawnReasonOrphaned,
			By:         "keeper",
		}

		payloadJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		evt := Event{Type: "decision_withdrawn", Payload: payloadJSON}
		decoded, err := evt.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}

		got, ok := decoded.(*DecisionWithdrawnPayload)
		if !ok {
			t.Fatalf("DecodePayload returned %T; want *DecisionWithdrawnPayload", decoded)
		}
		if got.DecisionID != want.DecisionID || got.Reason != want.Reason || got.By != want.By {
			t.Errorf("round-trip mismatch: got %+v; want %+v", got, want)
		}
	})
}
