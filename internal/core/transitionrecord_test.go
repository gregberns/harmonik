// Package core — requirement-traceable sensors for transition-record sibling file
// serialization per execution-model.md §4.4.EM-018.
//
// EM-018 requires every checkpoint commit tree to contain typed JSON at
// .harmonik/transitions/<run_id>/<transition_id>.json with the full Transition
// record. The file MUST carry a schema_version field matching the commit's
// Harmonik-Schema-Version trailer.
package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// b3f22ValidTransition builds a fully-populated forward Transition suitable for
// transitionrecord tests. The schema version defaults to 1.
func b3f22ValidTransition(t *testing.T) Transition {
	t.Helper()
	return b3f77ValidTransition(t)
}

// b3f22UnmarshalWire decodes raw JSON bytes into a map[string]any for field
// inspection in tests.
func b3f22UnmarshalWire(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("b3f22UnmarshalWire: json.Unmarshal: %v", err)
	}
	return m
}

// --- MarshalTransitionRecord: happy path ---

// TestMarshalTransitionRecord_HappyPath verifies that a valid Transition
// marshals to JSON without error and that the output is a non-empty JSON object.
func TestMarshalTransitionRecord_HappyPath(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("MarshalTransitionRecord: returned empty bytes")
	}
	if !json.Valid(data) {
		t.Fatalf("MarshalTransitionRecord: output is not valid JSON: %s", data)
	}
}

// TestMarshalTransitionRecord_PathShape verifies that the path returned by
// TransitionRecordPath embeds the run_id and transition_id from the transition.
// This is an EM-018 requirement: the path MUST encode the run and transition IDs.
func TestMarshalTransitionRecord_PathShape(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	p := TransitionRecordPath(tr.RunID, tr.TransitionID)

	if !strings.Contains(p, tr.RunID.String()) {
		t.Errorf("TransitionRecordPath %q does not contain run_id %q", p, tr.RunID.String())
	}
	if !strings.Contains(p, tr.TransitionID.String()) {
		t.Errorf("TransitionRecordPath %q does not contain transition_id %q", p, tr.TransitionID.String())
	}
	if !strings.HasPrefix(p, ".harmonik/transitions/") {
		t.Errorf("TransitionRecordPath %q does not start with .harmonik/transitions/", p)
	}
	if !strings.HasSuffix(p, ".json") {
		t.Errorf("TransitionRecordPath %q does not end with .json", p)
	}
}

// TestMarshalTransitionRecord_FieldNamesSnakeCase verifies that the marshaled
// JSON uses snake_case field names as declared in execution-model.md §6.1
// RECORD Transition. A reader uses `git show` to retrieve the file; the field
// names are part of the wire contract.
func TestMarshalTransitionRecord_FieldNamesSnakeCase(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	m := b3f22UnmarshalWire(t, data)

	requiredKeys := []string{
		"transition_id",
		"run_id",
		"from_state",
		"to_state",
		"actor_role",
		"candidate_actions",
		"chosen_action",
		"policy_version",
		"evidence",
		"verifier_metrics",
		"outcome_status",
		"transition_kind",
		"schema_version",
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("MarshalTransitionRecord: output missing key %q", k)
		}
	}
}

// TestMarshalTransitionRecord_SchemaVersionPresent verifies that schema_version
// is present in the marshaled output with the correct value (EM-018).
func TestMarshalTransitionRecord_SchemaVersionPresent(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	m := b3f22UnmarshalWire(t, data)

	sv, ok := m["schema_version"]
	if !ok {
		t.Fatal("MarshalTransitionRecord: schema_version field missing from output")
	}
	// json.Unmarshal decodes numbers as float64 by default.
	svFloat, ok := sv.(float64)
	if !ok {
		t.Fatalf("MarshalTransitionRecord: schema_version type unexpected: %T", sv)
	}
	if int(svFloat) != tr.SchemaVersion {
		t.Errorf("MarshalTransitionRecord: schema_version = %v, want %d", svFloat, tr.SchemaVersion)
	}
}

// TestMarshalTransitionRecord_RunIDPresent verifies that the run_id in the
// marshaled output matches the transition's RunID string representation.
func TestMarshalTransitionRecord_RunIDPresent(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	m := b3f22UnmarshalWire(t, data)

	runID, ok := m["run_id"]
	if !ok {
		t.Fatal("MarshalTransitionRecord: run_id field missing from output")
	}
	if runID != tr.RunID.String() {
		t.Errorf("MarshalTransitionRecord: run_id = %v, want %s", runID, tr.RunID.String())
	}
}

// TestMarshalTransitionRecord_TransitionIDPresent verifies that the
// transition_id in the marshaled output matches the transition's TransitionID.
func TestMarshalTransitionRecord_TransitionIDPresent(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	m := b3f22UnmarshalWire(t, data)

	tid, ok := m["transition_id"]
	if !ok {
		t.Fatal("MarshalTransitionRecord: transition_id field missing from output")
	}
	if tid != tr.TransitionID.String() {
		t.Errorf("MarshalTransitionRecord: transition_id = %v, want %s", tid, tr.TransitionID.String())
	}
}

// TestMarshalTransitionRecord_RollbackStateIDOmittedForForward verifies that
// rollback_to_state_id is absent (null) for forward transitions per EM-044.
func TestMarshalTransitionRecord_RollbackStateIDOmittedForForward(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	// b3f22ValidTransition returns a forward transition with nil RollbackToStateID.
	if tr.RollbackToStateID != nil {
		t.Skip("fixture unexpectedly has non-nil RollbackToStateID")
	}
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	m := b3f22UnmarshalWire(t, data)

	v, ok := m["rollback_to_state_id"]
	if !ok {
		t.Fatal("MarshalTransitionRecord: rollback_to_state_id key missing from output (should be present as null)")
	}
	if v != nil {
		t.Errorf("MarshalTransitionRecord: rollback_to_state_id = %v, want null for forward transition", v)
	}
}

// --- ValidateTransitionSchemaVersion: happy path ---

// TestValidateTransitionSchemaVersion_Match verifies that matching versions
// produce a nil error.
func TestValidateTransitionSchemaVersion_Match(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	// tr.SchemaVersion == 1 from the fixture.
	if err := ValidateTransitionSchemaVersion(tr, tr.SchemaVersion); err != nil {
		t.Errorf("ValidateTransitionSchemaVersion: unexpected error: %v", err)
	}
}

// TestValidateTransitionSchemaVersion_Mismatch verifies that a mismatched
// commit schema version returns an error.
func TestValidateTransitionSchemaVersion_Mismatch(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	// tr.SchemaVersion == 1; supply a different commit version.
	err := ValidateTransitionSchemaVersion(tr, tr.SchemaVersion+1)
	if err == nil {
		t.Fatal("ValidateTransitionSchemaVersion: expected error for mismatched versions, got nil")
	}
}

// TestValidateTransitionSchemaVersion_MismatchMessageContainsValues verifies
// that the error message includes both the transition's schema_version and the
// commit schema version to aid debugging.
func TestValidateTransitionSchemaVersion_MismatchMessageContainsValues(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	commitVersion := tr.SchemaVersion + 5
	err := ValidateTransitionSchemaVersion(tr, commitVersion)
	if err == nil {
		t.Fatal("ValidateTransitionSchemaVersion: expected error for mismatched versions, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1") {
		t.Errorf("ValidateTransitionSchemaVersion error %q should contain transition schema_version 1", msg)
	}
	if !strings.Contains(msg, "6") {
		t.Errorf("ValidateTransitionSchemaVersion error %q should contain commit schema_version 6", msg)
	}
}

// TestValidateTransitionSchemaVersion_ZeroCommitVersion verifies that a zero
// commit schema version triggers a mismatch (schema versions must be > 0 per
// Transition.Valid()).
func TestValidateTransitionSchemaVersion_ZeroCommitVersion(t *testing.T) {
	t.Parallel()

	tr := b3f22ValidTransition(t)
	// tr.SchemaVersion == 1, commit claims 0 — mismatch.
	err := ValidateTransitionSchemaVersion(tr, 0)
	if err == nil {
		t.Fatal("ValidateTransitionSchemaVersion: expected error for zero commitSchemaVersion, got nil")
	}
}
