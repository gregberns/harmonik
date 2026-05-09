package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// gateVerdictRecordFixture returns a fully-populated GateVerdictRecord with all
// fields set to valid non-zero values, suitable for structural tests (hk-a8bg.71).
func gateVerdictRecordFixture(t *testing.T) GateVerdictRecord {
	t.Helper()

	reason := "policy expression returned deny"
	hash := strings.Repeat("a", 64)
	return GateVerdictRecord{
		GateName:          "pre-deploy-gate",
		Action:            GateActionDeny,
		Reason:            &reason,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T00:00:00Z",
	}
}

// gateVerdictRecordAllowFixture returns a GateVerdictRecord for the allow action
// (Reason is nil, which is valid for allow).
func gateVerdictRecordAllowFixture(t *testing.T) GateVerdictRecord {
	t.Helper()

	hash := strings.Repeat("b", 64)
	return GateVerdictRecord{
		GateName:          "pre-deploy-gate",
		Action:            GateActionAllow,
		Reason:            nil,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T00:00:00Z",
	}
}

// TestGateVerdictRecord_Valid_Allow verifies that a valid allow record passes
// Valid() with Reason == nil.
func TestGateVerdictRecord_Valid_Allow(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	if !r.Valid() {
		t.Error("expected Valid() == true for allow record, got false")
	}
}

// TestGateVerdictRecord_Valid_AllowWithReason verifies that an allow record
// with an optional reason set also passes Valid().
func TestGateVerdictRecord_Valid_AllowWithReason(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	reason := "explicit allow note"
	r.Reason = &reason
	if !r.Valid() {
		t.Error("expected Valid() == true for allow record with reason, got false")
	}
}

// TestGateVerdictRecord_Valid_Deny verifies that a deny record with Reason set
// passes Valid().
func TestGateVerdictRecord_Valid_Deny(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordFixture(t)
	if !r.Valid() {
		t.Error("expected Valid() == true for deny record with reason, got false")
	}
}

// TestGateVerdictRecord_Valid_EscalateToHuman verifies that an escalate-to-human
// record with Reason set passes Valid().
func TestGateVerdictRecord_Valid_EscalateToHuman(t *testing.T) {
	t.Parallel()

	reason := "requires human review"
	hash := strings.Repeat("c", 64)
	r := GateVerdictRecord{
		GateName:          "sensitive-gate",
		Action:            GateActionEscalateToHuman,
		Reason:            &reason,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T01:00:00Z",
	}
	if !r.Valid() {
		t.Error("expected Valid() == true for escalate-to-human record, got false")
	}
}

// TestGateVerdictRecord_Valid_DenyReasonNil verifies that Valid() rejects a
// deny record with Reason == nil (cross-field invariant per §6.1.6).
func TestGateVerdictRecord_Valid_DenyReasonNil(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordFixture(t)
	r.Reason = nil
	if r.Valid() {
		t.Error("expected Valid() == false for deny record with nil Reason, got true")
	}
}

// TestGateVerdictRecord_Valid_DenyReasonEmpty verifies that Valid() rejects a
// deny record with Reason set to an empty string (cross-field invariant per §6.1.6).
func TestGateVerdictRecord_Valid_DenyReasonEmpty(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordFixture(t)
	empty := ""
	r.Reason = &empty
	if r.Valid() {
		t.Error("expected Valid() == false for deny record with empty Reason, got true")
	}
}

// TestGateVerdictRecord_Valid_EscalateReasonNil verifies that Valid() rejects
// an escalate-to-human record with Reason == nil.
func TestGateVerdictRecord_Valid_EscalateReasonNil(t *testing.T) {
	t.Parallel()

	hash := strings.Repeat("d", 64)
	r := GateVerdictRecord{
		GateName:          "sensitive-gate",
		Action:            GateActionEscalateToHuman,
		Reason:            nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T01:00:00Z",
	}
	if r.Valid() {
		t.Error("expected Valid() == false for escalate-to-human record with nil Reason, got true")
	}
}

// TestGateVerdictRecord_Valid_EmptyGateName verifies that Valid() rejects a
// record with an empty GateName.
func TestGateVerdictRecord_Valid_EmptyGateName(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	r.GateName = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty GateName, got true")
	}
}

// TestGateVerdictRecord_Valid_UnknownAction verifies that Valid() rejects a
// record with an unknown GateAction value.
func TestGateVerdictRecord_Valid_UnknownAction(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	r.Action = GateAction("unknown")
	if r.Valid() {
		t.Error("expected Valid() == false for unknown action, got true")
	}
}

// TestGateVerdictRecord_Valid_EmptyInputEnvelopeHash verifies that Valid()
// rejects a record with an empty InputEnvelopeHash.
func TestGateVerdictRecord_Valid_EmptyInputEnvelopeHash(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	r.InputEnvelopeHash = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty InputEnvelopeHash, got true")
	}
}

// TestGateVerdictRecord_Valid_EmptyProducedAt verifies that Valid() rejects a
// record with an empty ProducedAt timestamp.
func TestGateVerdictRecord_Valid_EmptyProducedAt(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	r.ProducedAt = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty ProducedAt, got true")
	}
}

// TestGateVerdictRecord_JSONRoundTrip verifies that a fully-populated
// GateVerdictRecord survives a JSON marshal/unmarshal round-trip with all fields
// intact (specs/control-points.md §6.3 wire shape).
func TestGateVerdictRecord_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := gateVerdictRecordFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got GateVerdictRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.GateName != orig.GateName {
		t.Errorf("GateName: got %q, want %q", got.GateName, orig.GateName)
	}
	if got.Action != orig.Action {
		t.Errorf("Action: got %q, want %q", got.Action, orig.Action)
	}
	if got.Reason == nil || *got.Reason != *orig.Reason {
		t.Errorf("Reason: got %v, want %v", got.Reason, orig.Reason)
	}
	if got.InputEnvelopeHash != orig.InputEnvelopeHash {
		t.Errorf("InputEnvelopeHash: got %q, want %q", got.InputEnvelopeHash, orig.InputEnvelopeHash)
	}
	if got.ProducedAt != orig.ProducedAt {
		t.Errorf("ProducedAt: got %q, want %q", got.ProducedAt, orig.ProducedAt)
	}
}

// TestGateVerdictRecord_JSONOmitsReasonWhenNil verifies that when Reason is nil
// the JSON output omits the reason key entirely (omitempty).
func TestGateVerdictRecord_JSONOmitsReasonWhenNil(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["reason"]; ok {
		t.Error("reason key present in JSON when Reason is nil, want omitted")
	}
}

// TestGateVerdictRecord_JSONOmitsCognitionMetaWhenNil verifies that when
// CognitionMeta is nil the JSON output omits the cognition_meta key (omitempty).
func TestGateVerdictRecord_JSONOmitsCognitionMetaWhenNil(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordAllowFixture(t)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["cognition_meta"]; ok {
		t.Error("cognition_meta key present in JSON when CognitionMeta is nil, want omitted")
	}
}

// TestGateVerdictRecord_JSONKeys verifies that the JSON field names match the
// snake_case wire shape declared in specs/control-points.md §6.1.6.
func TestGateVerdictRecord_JSONKeys(t *testing.T) {
	t.Parallel()

	r := gateVerdictRecordFixture(t)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"gate_name", "action", "reason", "input_envelope_hash", "produced_at"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}
