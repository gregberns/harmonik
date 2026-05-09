package core

import (
	"encoding/json"
	"testing"
)

// gatePayloadFixture returns a fully-populated GatePayload for the
// approval-gate subtype with all fields set to valid non-zero values,
// suitable for structural and round-trip tests (hk-a8bg.60).
func gatePayloadFixture(t *testing.T) GatePayload {
	t.Helper()

	approver := "ops-lead"
	return GatePayload{
		Subtype:       GateSubtypeApproval,
		AttachPoint:   AttachPointNodePreEntry,
		NamedApprover: &approver,
	}
}

// TestGatePayloadValid_GoalGate verifies that a goal-gate with neither
// optional field set is valid (specs/control-points.md §6.1.1).
func TestGatePayloadValid_GoalGate(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtypeGoal,
		AttachPoint: AttachPointNodePreEntry,
	}
	if !g.Valid() {
		t.Error("Valid() = false for valid goal-gate GatePayload, want true")
	}
}

// TestGatePayloadValid_GoalGateAllAttachPoints verifies that goal-gate is
// valid at every declared attach point.
func TestGatePayloadValid_GoalGateAllAttachPoints(t *testing.T) {
	t.Parallel()

	points := []AttachPoint{
		AttachPointNodePreEntry,
		AttachPointNodePostExit,
		AttachPointEdgeBeforeSelection,
		AttachPointEdgeAfterSelection,
	}
	for _, ap := range points {
		ap := ap
		t.Run(string(ap), func(t *testing.T) {
			t.Parallel()

			g := GatePayload{
				Subtype:     GateSubtypeGoal,
				AttachPoint: ap,
			}
			if !g.Valid() {
				t.Errorf("Valid() = false for goal-gate at attach_point=%q, want true", ap)
			}
		})
	}
}

// TestGatePayloadValid_ApprovalGate verifies that an approval-gate with a
// non-empty NamedApprover and no VerificationRef is valid
// (specs/control-points.md §6.1.1).
func TestGatePayloadValid_ApprovalGate(t *testing.T) {
	t.Parallel()

	g := gatePayloadFixture(t)
	if !g.Valid() {
		t.Error("Valid() = false for valid approval-gate GatePayload, want true")
	}
}

// TestGatePayloadValid_QualityGate verifies that a quality-gate with a
// non-empty VerificationRef and no NamedApprover is valid
// (specs/control-points.md §6.1.1).
func TestGatePayloadValid_QualityGate(t *testing.T) {
	t.Parallel()

	ref := "verify-security-scan"
	g := GatePayload{
		Subtype:         GateSubtypeQuality,
		AttachPoint:     AttachPointNodePostExit,
		VerificationRef: &ref,
	}
	if !g.Valid() {
		t.Error("Valid() = false for valid quality-gate GatePayload, want true")
	}
}

// TestGatePayloadValid_ZeroValue verifies that a zero-value GatePayload is
// not valid (Subtype and AttachPoint are both empty strings).
func TestGatePayloadValid_ZeroValue(t *testing.T) {
	t.Parallel()

	var g GatePayload
	if g.Valid() {
		t.Error("Valid() = true for zero-value GatePayload, want false")
	}
}

// TestGatePayloadValid_InvalidSubtype verifies that Valid() returns false when
// Subtype is not a declared GateSubtype constant.
func TestGatePayloadValid_InvalidSubtype(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtype("bogus-gate"),
		AttachPoint: AttachPointNodePreEntry,
	}
	if g.Valid() {
		t.Error("Valid() = true for invalid subtype, want false")
	}
}

// TestGatePayloadValid_InvalidAttachPoint verifies that Valid() returns false
// when AttachPoint is not a declared AttachPoint constant.
func TestGatePayloadValid_InvalidAttachPoint(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtypeGoal,
		AttachPoint: AttachPoint("bogus-attach"),
	}
	if g.Valid() {
		t.Error("Valid() = true for invalid attach_point, want false")
	}
}

// TestGatePayloadValid_ApprovalMissingNamedApprover verifies that an
// approval-gate without NamedApprover is invalid
// (specs/control-points.md §6.1.1: named_approver required when subtype = approval-gate).
func TestGatePayloadValid_ApprovalMissingNamedApprover(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtypeApproval,
		AttachPoint: AttachPointNodePreEntry,
	}
	if g.Valid() {
		t.Error("Valid() = true for approval-gate missing named_approver, want false")
	}
}

// TestGatePayloadValid_ApprovalEmptyNamedApprover verifies that an
// approval-gate with an empty NamedApprover string is invalid.
func TestGatePayloadValid_ApprovalEmptyNamedApprover(t *testing.T) {
	t.Parallel()

	empty := ""
	g := GatePayload{
		Subtype:       GateSubtypeApproval,
		AttachPoint:   AttachPointNodePreEntry,
		NamedApprover: &empty,
	}
	if g.Valid() {
		t.Error("Valid() = true for approval-gate with empty named_approver, want false")
	}
}

// TestGatePayloadValid_ApprovalWithVerificationRef verifies that an
// approval-gate carrying a VerificationRef is invalid — the field is only
// valid on quality-gate subtypes (specs/control-points.md §6.1.1).
func TestGatePayloadValid_ApprovalWithVerificationRef(t *testing.T) {
	t.Parallel()

	approver := "ops-lead"
	ref := "some-verifier"
	g := GatePayload{
		Subtype:         GateSubtypeApproval,
		AttachPoint:     AttachPointNodePreEntry,
		NamedApprover:   &approver,
		VerificationRef: &ref,
	}
	if g.Valid() {
		t.Error("Valid() = true for approval-gate with unexpected verification_ref, want false")
	}
}

// TestGatePayloadValid_QualityMissingVerificationRef verifies that a
// quality-gate without VerificationRef is invalid
// (specs/control-points.md §6.1.1: verification_ref required when subtype = quality-gate).
func TestGatePayloadValid_QualityMissingVerificationRef(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtypeQuality,
		AttachPoint: AttachPointNodePostExit,
	}
	if g.Valid() {
		t.Error("Valid() = true for quality-gate missing verification_ref, want false")
	}
}

// TestGatePayloadValid_QualityEmptyVerificationRef verifies that a
// quality-gate with an empty VerificationRef string is invalid.
func TestGatePayloadValid_QualityEmptyVerificationRef(t *testing.T) {
	t.Parallel()

	empty := ""
	g := GatePayload{
		Subtype:         GateSubtypeQuality,
		AttachPoint:     AttachPointNodePostExit,
		VerificationRef: &empty,
	}
	if g.Valid() {
		t.Error("Valid() = true for quality-gate with empty verification_ref, want false")
	}
}

// TestGatePayloadValid_QualityWithNamedApprover verifies that a quality-gate
// carrying a NamedApprover is invalid — the field is only valid on
// approval-gate subtypes (specs/control-points.md §6.1.1).
func TestGatePayloadValid_QualityWithNamedApprover(t *testing.T) {
	t.Parallel()

	approver := "ops-lead"
	ref := "verify-scan"
	g := GatePayload{
		Subtype:         GateSubtypeQuality,
		AttachPoint:     AttachPointNodePostExit,
		VerificationRef: &ref,
		NamedApprover:   &approver,
	}
	if g.Valid() {
		t.Error("Valid() = true for quality-gate with unexpected named_approver, want false")
	}
}

// TestGatePayloadValid_GoalGateWithNamedApprover verifies that a goal-gate
// carrying a NamedApprover is invalid.
func TestGatePayloadValid_GoalGateWithNamedApprover(t *testing.T) {
	t.Parallel()

	approver := "ops-lead"
	g := GatePayload{
		Subtype:       GateSubtypeGoal,
		AttachPoint:   AttachPointNodePreEntry,
		NamedApprover: &approver,
	}
	if g.Valid() {
		t.Error("Valid() = true for goal-gate with unexpected named_approver, want false")
	}
}

// TestGatePayloadValid_GoalGateWithVerificationRef verifies that a goal-gate
// carrying a VerificationRef is invalid.
func TestGatePayloadValid_GoalGateWithVerificationRef(t *testing.T) {
	t.Parallel()

	ref := "verify-scan"
	g := GatePayload{
		Subtype:         GateSubtypeGoal,
		AttachPoint:     AttachPointNodePreEntry,
		VerificationRef: &ref,
	}
	if g.Valid() {
		t.Error("Valid() = true for goal-gate with unexpected verification_ref, want false")
	}
}

// TestGatePayloadJSONRoundTrip_ApprovalGate verifies that a fully-populated
// approval-gate GatePayload survives a JSON marshal/unmarshal round-trip with
// all fields intact (specs/control-points.md §6.1.1 wire shape).
func TestGatePayloadJSONRoundTrip_ApprovalGate(t *testing.T) {
	t.Parallel()

	orig := gatePayloadFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got GatePayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Subtype != orig.Subtype {
		t.Errorf("Subtype: got %q, want %q", got.Subtype, orig.Subtype)
	}
	if got.AttachPoint != orig.AttachPoint {
		t.Errorf("AttachPoint: got %q, want %q", got.AttachPoint, orig.AttachPoint)
	}
	if got.NamedApprover == nil || *got.NamedApprover != *orig.NamedApprover {
		t.Errorf("NamedApprover: got %v, want %v", got.NamedApprover, orig.NamedApprover)
	}
	if got.VerificationRef != nil {
		t.Errorf("VerificationRef: got %v, want nil", got.VerificationRef)
	}
	if !got.Valid() {
		t.Error("round-tripped GatePayload.Valid() = false, want true")
	}
}

// TestGatePayloadJSONRoundTrip_QualityGate verifies that a quality-gate
// GatePayload survives a JSON round-trip.
func TestGatePayloadJSONRoundTrip_QualityGate(t *testing.T) {
	t.Parallel()

	ref := "verify-security-scan"
	orig := GatePayload{
		Subtype:         GateSubtypeQuality,
		AttachPoint:     AttachPointEdgeAfterSelection,
		VerificationRef: &ref,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got GatePayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Subtype != orig.Subtype {
		t.Errorf("Subtype: got %q, want %q", got.Subtype, orig.Subtype)
	}
	if got.AttachPoint != orig.AttachPoint {
		t.Errorf("AttachPoint: got %q, want %q", got.AttachPoint, orig.AttachPoint)
	}
	if got.VerificationRef == nil || *got.VerificationRef != *orig.VerificationRef {
		t.Errorf("VerificationRef: got %v, want %v", got.VerificationRef, orig.VerificationRef)
	}
	if got.NamedApprover != nil {
		t.Errorf("NamedApprover: got %v, want nil", got.NamedApprover)
	}
	if !got.Valid() {
		t.Error("round-tripped GatePayload.Valid() = false, want true")
	}
}

// TestGatePayloadJSONFieldNames verifies that GatePayload serialises with the
// snake_case field names declared in the spec (specs/control-points.md §6.1.1).
func TestGatePayloadJSONFieldNames(t *testing.T) {
	t.Parallel()

	g := gatePayloadFixture(t)
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"subtype", "attach_point", "named_approver"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON output missing key %q", key)
		}
	}
	// verification_ref is omitempty and absent from approval-gate fixture.
	if _, ok := m["verification_ref"]; ok {
		t.Error("JSON output contains unexpected key \"verification_ref\" for approval-gate fixture")
	}
}

// TestGatePayloadJSONOmitempty verifies that nil optional fields are omitted
// from JSON output (omitempty), not serialised as null.
func TestGatePayloadJSONOmitempty(t *testing.T) {
	t.Parallel()

	g := GatePayload{
		Subtype:     GateSubtypeGoal,
		AttachPoint: AttachPointNodePreEntry,
	}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	if _, ok := m["named_approver"]; ok {
		t.Error("JSON output contains \"named_approver\" for goal-gate (should be omitted)")
	}
	if _, ok := m["verification_ref"]; ok {
		t.Error("JSON output contains \"verification_ref\" for goal-gate (should be omitted)")
	}
}
