package core

import "testing"

// evidenceFixture returns a populated Evidence map for use in tests.
func evidenceFixture() Evidence {
	return Evidence{
		"key":  "value",
		"num":  42,
		"flag": true,
	}
}

func TestEvidenceValid_Nil(t *testing.T) {
	t.Parallel()

	var e Evidence
	if !e.Valid() {
		t.Error("Valid() = false for nil Evidence, want true (spec: nil map is permitted)")
	}
}

func TestEvidenceValid_Empty(t *testing.T) {
	t.Parallel()

	e := Evidence{}
	if !e.Valid() {
		t.Error("Valid() = false for empty Evidence, want true")
	}
}

func TestEvidenceValid_ArbitraryKeys(t *testing.T) {
	t.Parallel()

	e := evidenceFixture()
	if !e.Valid() {
		t.Error("Valid() = false for Evidence with arbitrary keys, want true")
	}
}

func TestEvidenceValid_ReservedKeySubWorkflowPin(t *testing.T) {
	t.Parallel()

	e := Evidence{EvidenceKeySubWorkflowPin: "sha256:abc123"}
	if !e.Valid() {
		t.Error("Valid() = false for Evidence with sub_workflow_pin key, want true")
	}
}

func TestEvidenceValid_ReservedKeySynthesizedOutcome(t *testing.T) {
	t.Parallel()

	e := Evidence{EvidenceKeySynthesizedOutcome: true}
	if !e.Valid() {
		t.Error("Valid() = false for Evidence with synthesized_outcome key, want true")
	}
}

func TestEvidenceKeyConstants(t *testing.T) {
	t.Parallel()

	if EvidenceKeySubWorkflowPin != "sub_workflow_pin" {
		t.Errorf("EvidenceKeySubWorkflowPin = %q, want %q", EvidenceKeySubWorkflowPin, "sub_workflow_pin")
	}
	if EvidenceKeySynthesizedOutcome != "synthesized_outcome" {
		t.Errorf("EvidenceKeySynthesizedOutcome = %q, want %q", EvidenceKeySynthesizedOutcome, "synthesized_outcome")
	}
}
