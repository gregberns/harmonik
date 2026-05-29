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

// --- CP-040 Gate verdict persistence ---

// gateVerdictForEvidenceTest returns a minimal valid GateVerdictRecord for
// use in Evidence.SetGateVerdict tests.
func gateVerdictForEvidenceTest() GateVerdictRecord {
	h := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return GateVerdictRecord{
		GateName:          "deploy-gate",
		Action:            GateActionAllow,
		InputEnvelopeHash: h,
		ProducedAt:        "2026-05-29T00:00:00Z",
	}
}

// TestEvidence_SetGateVerdict_WritesKeyedByGateName verifies that
// SetGateVerdict inserts the GateVerdictRecord under the verdict's GateName
// key per specs/control-points.md §4.8.CP-040.
func TestEvidence_SetGateVerdict_WritesKeyedByGateName(t *testing.T) {
	t.Parallel()

	verdict := gateVerdictForEvidenceTest()
	e := Evidence{}
	updated := e.SetGateVerdict(verdict)

	stored, ok := updated[verdict.GateName]
	if !ok {
		t.Fatalf("key %q absent from Evidence after SetGateVerdict", verdict.GateName)
	}
	storedRecord, ok := stored.(GateVerdictRecord)
	if !ok {
		t.Fatalf("evidence[%q] has type %T, want GateVerdictRecord", verdict.GateName, stored)
	}
	if storedRecord.InputEnvelopeHash != verdict.InputEnvelopeHash {
		t.Errorf("InputEnvelopeHash: stored %q, want %q",
			storedRecord.InputEnvelopeHash, verdict.InputEnvelopeHash)
	}
}

// TestEvidence_SetGateVerdict_NilMap verifies that SetGateVerdict allocates
// a new map when called on a nil Evidence and still writes the verdict.
func TestEvidence_SetGateVerdict_NilMap(t *testing.T) {
	t.Parallel()

	verdict := gateVerdictForEvidenceTest()
	var e Evidence // nil
	updated := e.SetGateVerdict(verdict)

	if updated == nil {
		t.Fatal("SetGateVerdict returned nil Evidence, want allocated map")
	}
	if _, ok := updated[verdict.GateName]; !ok {
		t.Errorf("key %q absent from newly allocated Evidence", verdict.GateName)
	}
}

// TestEvidence_SetGateVerdict_PreservesExistingKeys verifies that
// SetGateVerdict does not remove pre-existing keys from the Evidence map.
func TestEvidence_SetGateVerdict_PreservesExistingKeys(t *testing.T) {
	t.Parallel()

	verdict := gateVerdictForEvidenceTest()
	e := Evidence{"prior_key": "prior_value", EvidenceKeySynthesizedOutcome: false}
	updated := e.SetGateVerdict(verdict)

	if _, ok := updated["prior_key"]; !ok {
		t.Error("pre-existing key prior_key removed by SetGateVerdict")
	}
	if _, ok := updated[verdict.GateName]; !ok {
		t.Errorf("gate verdict key %q absent after SetGateVerdict", verdict.GateName)
	}
}

// TestEvidence_SetGateVerdict_ResultIsValid verifies that the Evidence map
// returned by SetGateVerdict satisfies Valid().
func TestEvidence_SetGateVerdict_ResultIsValid(t *testing.T) {
	t.Parallel()

	verdict := gateVerdictForEvidenceTest()
	e := Evidence{}
	updated := e.SetGateVerdict(verdict)

	if !updated.Valid() {
		t.Error("Evidence.Valid() = false after SetGateVerdict, want true")
	}
}
