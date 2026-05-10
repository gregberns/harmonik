package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// subwfEnteredFixture returns a fully-populated SubWorkflowEnteredPayload with
// all required fields set to valid non-zero values.
// Used as the base for structural tests (hk-b3f.48).
func subwfEnteredFixture(t *testing.T) SubWorkflowEnteredPayload {
	t.Helper()
	return SubWorkflowEnteredPayload{
		RunID:              RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000048")),
		ParentNodeID:       NodeID("review-step"),
		SubWorkflowName:    SubWorkflowRef("code-review-workflow"),
		SubWorkflowVersion: WorkflowVersion("1.2.0"),
	}
}

// TestSubWorkflowEnteredPayload_Valid verifies that a fully-populated
// SubWorkflowEnteredPayload passes Valid().
func TestSubWorkflowEnteredPayload_Valid(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	if !p.Valid() {
		t.Error("expected Valid() == true for well-formed payload, got false")
	}
}

// TestSubWorkflowEnteredPayload_Valid_ZeroRunID verifies that Valid() rejects
// a payload with a zero RunID.
func TestSubWorkflowEnteredPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("expected Valid() == false for zero RunID, got true")
	}
}

// TestSubWorkflowEnteredPayload_Valid_EmptyParentNodeID verifies that Valid()
// rejects a payload with an empty ParentNodeID.
func TestSubWorkflowEnteredPayload_Valid_EmptyParentNodeID(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	p.ParentNodeID = NodeID("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty ParentNodeID, got true")
	}
}

// TestSubWorkflowEnteredPayload_Valid_EmptySubWorkflowName verifies that
// Valid() rejects a payload with an empty SubWorkflowName.
func TestSubWorkflowEnteredPayload_Valid_EmptySubWorkflowName(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	p.SubWorkflowName = SubWorkflowRef("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty SubWorkflowName, got true")
	}
}

// TestSubWorkflowEnteredPayload_Valid_EmptySubWorkflowVersion verifies that
// Valid() rejects a payload with an empty SubWorkflowVersion.
func TestSubWorkflowEnteredPayload_Valid_EmptySubWorkflowVersion(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	p.SubWorkflowVersion = WorkflowVersion("")
	if p.Valid() {
		t.Error("expected Valid() == false for empty SubWorkflowVersion, got true")
	}
}

// TestSubWorkflowEnteredPayload_JSONRoundTrip verifies that a
// SubWorkflowEnteredPayload survives a JSON marshal/unmarshal round-trip with
// all fields intact (event-model.md §8.1.9 wire shape).
func TestSubWorkflowEnteredPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := subwfEnteredFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got SubWorkflowEnteredPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.RunID != orig.RunID {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if got.ParentNodeID != orig.ParentNodeID {
		t.Errorf("ParentNodeID: got %q, want %q", got.ParentNodeID, orig.ParentNodeID)
	}
	if got.SubWorkflowName != orig.SubWorkflowName {
		t.Errorf("SubWorkflowName: got %q, want %q", got.SubWorkflowName, orig.SubWorkflowName)
	}
	if got.SubWorkflowVersion != orig.SubWorkflowVersion {
		t.Errorf("SubWorkflowVersion: got %q, want %q", got.SubWorkflowVersion, orig.SubWorkflowVersion)
	}
}

// TestSubWorkflowEnteredPayload_JSONKeys verifies that the JSON field names
// match the snake_case wire shape declared in event-model.md §8.1.9.
func TestSubWorkflowEnteredPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"run_id", "parent_node_id", "sub_workflow_name", "sub_workflow_version"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present, got absent", key)
		}
	}
}

// TestSubWorkflowEnteredPayload_CorrelationFields verifies that the payload
// carries the correlation fields (run_id + parent_node_id) required by
// execution-model.md §4.8.EM-036 for correlating sub-workflow lifecycle events.
func TestSubWorkflowEnteredPayload_CorrelationFields(t *testing.T) {
	t.Parallel()

	p := subwfEnteredFixture(t)

	// EM-036: "Both events correlate via run_id and the parent namespaced node_id"
	if uuid.UUID(p.RunID) == uuid.Nil {
		t.Error("RunID is zero: run_id correlation field cannot be the zero UUID (EM-036)")
	}
	if p.ParentNodeID == "" {
		t.Error("ParentNodeID is empty: parent_node_id correlation field must be non-empty (EM-036)")
	}
}
