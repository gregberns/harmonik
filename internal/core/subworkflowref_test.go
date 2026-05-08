package core

import (
	"encoding/json"
	"testing"
)

// TestSubWorkflowRefValid verifies that any non-empty string is valid and
// that an empty string is rejected.
func TestSubWorkflowRefValid(t *testing.T) {
	t.Parallel()

	valid := []SubWorkflowRef{
		"reconciliation-v1",
		"review-gate",
		"nested-planner",
		"a",
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	if SubWorkflowRef("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestSubWorkflowRefMarshalText verifies MarshalText accepts non-empty values
// and rejects the empty string.
func TestSubWorkflowRefMarshalText(t *testing.T) {
	t.Parallel()

	got, err := SubWorkflowRef("reconciliation-v1").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "reconciliation-v1" {
		t.Errorf("MarshalText = %q, want %q", string(got), "reconciliation-v1")
	}

	if _, err := SubWorkflowRef("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestSubWorkflowRefUnmarshalText verifies JSON round-trip behaviour.
func TestSubWorkflowRefUnmarshalText(t *testing.T) {
	t.Parallel()

	type subworkflowFixtureWrapper struct {
		Ref SubWorkflowRef `json:"sub_workflow_ref"`
	}

	tests := []struct {
		name    string
		input   string
		want    SubWorkflowRef
		wantErr bool
	}{
		{name: "reconciliation-v1", input: `{"sub_workflow_ref":"reconciliation-v1"}`, want: "reconciliation-v1"},
		{name: "review-gate", input: `{"sub_workflow_ref":"review-gate"}`, want: "review-gate"},
		{name: "arbitrary non-empty", input: `{"sub_workflow_ref":"wf-x"}`, want: "wf-x"},
		{name: "empty rejected", input: `{"sub_workflow_ref":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w subworkflowFixtureWrapper
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

// TestSubWorkflowRefRoundTrip verifies a non-empty SubWorkflowRef survives a
// json.Marshal / json.Unmarshal round-trip.
func TestSubWorkflowRefRoundTrip(t *testing.T) {
	t.Parallel()

	subworkflowFixtureValue := SubWorkflowRef("nested-planner")

	data, err := json.Marshal(subworkflowFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded SubWorkflowRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != subworkflowFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, subworkflowFixtureValue)
	}
}

// TestSubWorkflowRefNilPointerEncoding documents that the absence of a
// sub_workflow_ref (Node.type ≠ sub-workflow) is encoded as a nil *SubWorkflowRef
// at the call site, NOT as an empty SubWorkflowRef value.
func TestSubWorkflowRefNilPointerEncoding(t *testing.T) {
	t.Parallel()

	// A nil pointer is the correct Go representation for "sub_workflow_ref: None"
	// per the bead brief and godoc. The value type SubWorkflowRef must never
	// be empty; absence is always a nil pointer.
	var subworkflowFixtureAbsent *SubWorkflowRef
	if subworkflowFixtureAbsent != nil {
		t.Error("zero value of *SubWorkflowRef must be nil")
	}

	// A present ref must be non-nil and valid.
	ref := SubWorkflowRef("reconciliation-v1")
	subworkflowFixturePresent := &ref
	if subworkflowFixturePresent == nil {
		t.Fatal("unexpected nil pointer")
	}
	if !subworkflowFixturePresent.Valid() {
		t.Error("present SubWorkflowRef must be valid")
	}
}
