package core

import (
	"testing"

	"github.com/google/uuid"
)

// evidenceFixture returns a populated Evidence map for use in tests.
func evidenceFixture() Evidence {
	return Evidence{
		"key":  "value",
		"num":  42,
		"flag": true,
	}
}

// evidenceSubWorkflowPinFixture returns a valid SubWorkflowExpansionPin for
// use in Evidence map tests. Uses the canonical value shape per EM-034c.
func evidenceSubWorkflowPinFixture() SubWorkflowExpansionPin {
	return SubWorkflowExpansionPin{
		SubWorkflowRef:     "reconciliation-v1",
		SubWorkflowVersion: "1.2.3",
		ResolvedWorkflowID: WorkflowID(uuid.MustParse("01960000-0000-7000-8000-000000000001")),
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

	// Value shape per EM-034c: SubWorkflowExpansionPin struct.
	pin := evidenceSubWorkflowPinFixture()
	e := Evidence{EvidenceKeySubWorkflowPin: pin}
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
	if EvidenceKeyPartialSuccess != "partial_success" {
		t.Errorf("EvidenceKeyPartialSuccess = %q, want %q", EvidenceKeyPartialSuccess, "partial_success")
	}
}

func TestEvidenceValid_ReservedKeyPartialSuccess(t *testing.T) {
	t.Parallel()

	e := Evidence{EvidenceKeyPartialSuccess: true}
	if !e.Valid() {
		t.Error("Valid() = false for Evidence with partial_success key, want true")
	}
}
