package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// outcomeSpineFixtureHookVerdictRecord returns a fully-populated
// HookVerdictRecord with all fields set to valid non-zero values.
// Used as the base for structural tests (hk-b3f.35).
func outcomeSpineFixtureHookVerdictRecord(t *testing.T) HookVerdictRecord {
	t.Helper()

	reason := "evaluator returned typed failure"
	hash := strings.Repeat("a", 64)
	return HookVerdictRecord{
		HookName:          "pre-commit-hook",
		InvocationID:      uuid.MustParse("01942b3c-0000-7000-8000-000000000001"),
		SideEffect:        outcomeSpineFixtureSideEffect(t),
		Failed:            true,
		Reason:            &reason,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T00:00:00Z",
	}
}

// outcomeSpineFixtureHookVerdictRecordSuccess returns a HookVerdictRecord for
// a successful (non-failed) hook dispatch where Reason is nil.
func outcomeSpineFixtureHookVerdictRecordSuccess(t *testing.T) HookVerdictRecord {
	t.Helper()

	hash := strings.Repeat("b", 64)
	return HookVerdictRecord{
		HookName:          "pre-commit-hook",
		InvocationID:      uuid.MustParse("01942b3c-0000-7000-8000-000000000002"),
		SideEffect:        outcomeSpineFixtureSideEffect(t),
		Failed:            false,
		Reason:            nil,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-08T00:00:00Z",
	}
}

// outcomeSpineFixtureSideEffect returns a valid SideEffect for use in
// HookVerdictRecord fixture construction.
func outcomeSpineFixtureSideEffect(t *testing.T) SideEffect {
	t.Helper()
	return SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "hook.fired",
		Payload:          map[string]any{"key": "value"},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
}

// TestHookVerdictRecord_Valid_SuccessNilReason verifies that a successful
// (non-failed) record with Reason == nil passes Valid().
func TestHookVerdictRecord_Valid_SuccessNilReason(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	if !r.Valid() {
		t.Error("expected Valid() == true for successful record with nil Reason, got false")
	}
}

// TestHookVerdictRecord_Valid_SuccessWithReason verifies that a successful
// record with an optional reason set also passes Valid().
func TestHookVerdictRecord_Valid_SuccessWithReason(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	note := "explicitly noted"
	r.Reason = &note
	if !r.Valid() {
		t.Error("expected Valid() == true for successful record with optional Reason, got false")
	}
}

// TestHookVerdictRecord_Valid_FailedWithReason verifies that a failed record
// with Reason set passes Valid().
func TestHookVerdictRecord_Valid_FailedWithReason(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecord(t)
	if !r.Valid() {
		t.Error("expected Valid() == true for failed record with Reason, got false")
	}
}

// TestHookVerdictRecord_Valid_FailedReasonNil verifies that Valid() rejects a
// failed record with Reason == nil (cross-field invariant per §6.1.6).
func TestHookVerdictRecord_Valid_FailedReasonNil(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecord(t)
	r.Reason = nil
	if r.Valid() {
		t.Error("expected Valid() == false for failed record with nil Reason, got true")
	}
}

// TestHookVerdictRecord_Valid_FailedReasonEmpty verifies that Valid() rejects a
// failed record with Reason set to an empty string.
func TestHookVerdictRecord_Valid_FailedReasonEmpty(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecord(t)
	empty := ""
	r.Reason = &empty
	if r.Valid() {
		t.Error("expected Valid() == false for failed record with empty Reason, got true")
	}
}

// TestHookVerdictRecord_Valid_EmptyHookName verifies that Valid() rejects a
// record with an empty HookName.
func TestHookVerdictRecord_Valid_EmptyHookName(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	r.HookName = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty HookName, got true")
	}
}

// TestHookVerdictRecord_Valid_NilInvocationID verifies that Valid() rejects a
// record with a nil (zero) InvocationID.
func TestHookVerdictRecord_Valid_NilInvocationID(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	r.InvocationID = uuid.Nil
	if r.Valid() {
		t.Error("expected Valid() == false for uuid.Nil InvocationID, got true")
	}
}

// TestHookVerdictRecord_Valid_InvalidSideEffect verifies that Valid() rejects
// a record with an invalid SideEffect (empty Target).
func TestHookVerdictRecord_Valid_InvalidSideEffect(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	r.SideEffect.Target = ""
	if r.Valid() {
		t.Error("expected Valid() == false for invalid SideEffect, got true")
	}
}

// TestHookVerdictRecord_Valid_EmptyInputEnvelopeHash verifies that Valid()
// rejects a record with an empty InputEnvelopeHash.
func TestHookVerdictRecord_Valid_EmptyInputEnvelopeHash(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	r.InputEnvelopeHash = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty InputEnvelopeHash, got true")
	}
}

// TestHookVerdictRecord_Valid_EmptyProducedAt verifies that Valid() rejects a
// record with an empty ProducedAt timestamp.
func TestHookVerdictRecord_Valid_EmptyProducedAt(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
	r.ProducedAt = ""
	if r.Valid() {
		t.Error("expected Valid() == false for empty ProducedAt, got true")
	}
}

// TestHookVerdictRecord_JSONRoundTrip verifies that a fully-populated
// HookVerdictRecord survives a JSON marshal/unmarshal round-trip with all
// fields intact (specs/control-points.md §6.1.6 wire shape).
func TestHookVerdictRecord_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := outcomeSpineFixtureHookVerdictRecord(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got HookVerdictRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.HookName != orig.HookName {
		t.Errorf("HookName: got %q, want %q", got.HookName, orig.HookName)
	}
	if got.InvocationID != orig.InvocationID {
		t.Errorf("InvocationID: got %v, want %v", got.InvocationID, orig.InvocationID)
	}
	if got.Failed != orig.Failed {
		t.Errorf("Failed: got %v, want %v", got.Failed, orig.Failed)
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

// TestHookVerdictRecord_JSONOmitsReasonWhenNil verifies that when Reason is
// nil the JSON output omits the reason key (omitempty).
func TestHookVerdictRecord_JSONOmitsReasonWhenNil(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
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

// TestHookVerdictRecord_JSONOmitsCognitionMetaWhenNil verifies that when
// CognitionMeta is nil the JSON output omits the cognition_meta key (omitempty).
func TestHookVerdictRecord_JSONOmitsCognitionMetaWhenNil(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecordSuccess(t)
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

// TestHookVerdictRecord_JSONKeys verifies that the JSON field names match the
// snake_case wire shape declared in specs/control-points.md §6.1.6.
func TestHookVerdictRecord_JSONKeys(t *testing.T) {
	t.Parallel()

	r := outcomeSpineFixtureHookVerdictRecord(t)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"hook_name", "invocation_id", "side_effect", "failed", "reason", "input_envelope_hash", "produced_at"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

// TestHookVerdictRecord_JSONRoundTrip_WithCognitionMeta verifies that a
// HookVerdictRecord with a non-nil CognitionMeta survives a JSON round-trip
// with the CognitionMeta field intact.
func TestHookVerdictRecord_JSONRoundTrip_WithCognitionMeta(t *testing.T) {
	t.Parallel()

	cm := cognitionMetaFixture(t)
	orig := outcomeSpineFixtureHookVerdictRecord(t)
	orig.CognitionMeta = &cm

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got HookVerdictRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.CognitionMeta == nil {
		t.Fatal("CognitionMeta: got nil after round-trip, want non-nil")
	}
	if got.CognitionMeta.ModelResponseDigest != cm.ModelResponseDigest {
		t.Errorf("CognitionMeta.ModelResponseDigest: got %q, want %q",
			got.CognitionMeta.ModelResponseDigest, cm.ModelResponseDigest)
	}
	if !got.Valid() {
		t.Error("Valid() returned false after round-trip with CognitionMeta set")
	}
}

// TestHookVerdictRecord_SpineTyping verifies that HookVerdictRecord is the
// typed output of the hook-dispatch segment in the outcome spine per
// execution-model.md §4.6.EM-027. This test documents the segment boundary:
// a function accepting Outcome and returning HookVerdictRecord is the
// well-typed spine segment 2 signature.
func TestHookVerdictRecord_SpineTyping(t *testing.T) {
	t.Parallel()

	// Demonstrate the spine-segment typing: the hook-dispatch segment takes an
	// Outcome (spine segment 1 output) and produces a HookVerdictRecord (spine
	// segment 2 output). The type signature is the contract.
	stubHookDispatch := func(_ Outcome) HookVerdictRecord {
		return outcomeSpineFixtureHookVerdictRecordSuccess(t)
	}

	outcome := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
	}
	result := stubHookDispatch(outcome)
	if !result.Valid() {
		t.Error("stub hook-dispatch segment produced invalid HookVerdictRecord")
	}
}
