package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// subworkflowpinFixture returns a valid SubWorkflowExpansionPin for tests.
func subworkflowpinFixture() SubWorkflowExpansionPin {
	return SubWorkflowExpansionPin{
		SubWorkflowRef:     "reconciliation-v1",
		SubWorkflowVersion: "1.2.3",
		ResolvedWorkflowID: WorkflowID(uuid.MustParse("01960000-0000-7000-8000-000000000002")),
	}
}

func TestSubWorkflowExpansionPinValid_AllFields(t *testing.T) {
	t.Parallel()

	p := subworkflowpinFixture()
	if !p.Valid() {
		t.Errorf("Valid() = false for fully-populated SubWorkflowExpansionPin, want true")
	}
}

func TestSubWorkflowExpansionPinValid_EmptyRef(t *testing.T) {
	t.Parallel()

	p := subworkflowpinFixture()
	p.SubWorkflowRef = ""
	if p.Valid() {
		t.Error("Valid() = true for empty SubWorkflowRef, want false")
	}
}

func TestSubWorkflowExpansionPinValid_EmptyVersion(t *testing.T) {
	t.Parallel()

	p := subworkflowpinFixture()
	p.SubWorkflowVersion = ""
	if p.Valid() {
		t.Error("Valid() = true for empty SubWorkflowVersion, want false")
	}
}

func TestSubWorkflowExpansionPinValid_NilUUID(t *testing.T) {
	t.Parallel()

	p := subworkflowpinFixture()
	p.ResolvedWorkflowID = WorkflowID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true for nil ResolvedWorkflowID, want false")
	}
}

// TestSubWorkflowExpansionPinJSONRoundTrip verifies the pin survives a
// json.Marshal / json.Unmarshal round-trip with the spec's snake_case keys.
func TestSubWorkflowExpansionPinJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := subworkflowpinFixture()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded SubWorkflowExpansionPin
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.SubWorkflowRef != original.SubWorkflowRef {
		t.Errorf("SubWorkflowRef: got %q, want %q", decoded.SubWorkflowRef, original.SubWorkflowRef)
	}
	if decoded.SubWorkflowVersion != original.SubWorkflowVersion {
		t.Errorf("SubWorkflowVersion: got %q, want %q", decoded.SubWorkflowVersion, original.SubWorkflowVersion)
	}
	if uuid.UUID(decoded.ResolvedWorkflowID) != uuid.UUID(original.ResolvedWorkflowID) {
		t.Errorf("ResolvedWorkflowID: got %v, want %v", decoded.ResolvedWorkflowID, original.ResolvedWorkflowID)
	}
	if !decoded.Valid() {
		t.Error("Valid() = false after round-trip, want true")
	}
}

// TestSubWorkflowExpansionPinJSONKeys verifies the JSON key names match the
// spec's snake_case field names from EM-034c.
func TestSubWorkflowExpansionPinJSONKeys(t *testing.T) {
	t.Parallel()

	p := subworkflowpinFixture()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"sub_workflow_ref", "sub_workflow_version", "resolved_workflow_id"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON key %q missing from marshaled SubWorkflowExpansionPin; got keys: %v", key, keysOf(raw))
		}
	}
}

// TestSubWorkflowExpansionPinEvidenceMap asserts the pin can be stored in an
// Evidence map under EvidenceKeySubWorkflowPin and retrieved with the correct
// struct type, satisfying the entry-checkpoint durability contract of EM-034c.
func TestSubWorkflowExpansionPinEvidenceMap(t *testing.T) {
	t.Parallel()

	pin := subworkflowpinFixture()
	e := Evidence{EvidenceKeySubWorkflowPin: pin}

	got, ok := e[EvidenceKeySubWorkflowPin].(SubWorkflowExpansionPin)
	if !ok {
		t.Fatalf("Evidence[EvidenceKeySubWorkflowPin] type assertion to SubWorkflowExpansionPin failed")
	}
	if !got.Valid() {
		t.Error("pin retrieved from Evidence is not Valid()")
	}
	if got.SubWorkflowRef != pin.SubWorkflowRef {
		t.Errorf("SubWorkflowRef: got %q, want %q", got.SubWorkflowRef, pin.SubWorkflowRef)
	}
}

// keysOf returns the keys of a map as a slice, for diagnostic output only.
func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
