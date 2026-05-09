package core

import (
	"encoding/json"
	"testing"
)

// sideEffectFixture returns a fully-populated SideEffect with all fields set
// to valid non-zero values, suitable for structural and round-trip tests
// (hk-a8bg.74).
func sideEffectFixture(t *testing.T) SideEffect {
	t.Helper()

	return SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "node.completed",
		Payload:          map[string]any{"run_id": "r-001", "node": "build"},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
}

// TestSideEffectValid_CrossProduct exercises Valid() over every combination of
// the three SideEffectKind values and the three IdempotencyClass values, all
// with a non-empty target (specs/control-points.md §6.1.6).
func TestSideEffectValid_CrossProduct(t *testing.T) {
	t.Parallel()

	kinds := []SideEffectKind{
		SideEffectKindEmitEvent,
		SideEffectKindStateMutation,
		SideEffectKindExternalAction,
	}
	classes := []IdempotencyClass{
		IdempotencyClassIdempotent,
		IdempotencyClassNonIdempotent,
		IdempotencyClassRecoverableNonIdempotent,
	}

	for _, k := range kinds {
		for _, c := range classes {
			k, c := k, c
			t.Run(string(k)+"/"+string(c), func(t *testing.T) {
				t.Parallel()

				s := SideEffect{
					Kind:             k,
					Target:           "some-target",
					Payload:          nil,
					IdempotencyClass: c,
				}
				if !s.Valid() {
					t.Errorf("Valid() = false for kind=%q idempotency_class=%q, want true", k, c)
				}
			})
		}
	}
}

// TestSideEffectValid_InvalidKind verifies that Valid() returns false when
// Kind is not a declared SideEffectKind constant.
func TestSideEffectValid_InvalidKind(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKind("bogus-kind"),
		Target:           "event-name",
		IdempotencyClass: IdempotencyClassIdempotent,
	}
	if s.Valid() {
		t.Error("Valid() = true for invalid kind, want false")
	}
}

// TestSideEffectValid_EmptyKind verifies that Valid() returns false when Kind
// is the empty string.
func TestSideEffectValid_EmptyKind(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKind(""),
		Target:           "event-name",
		IdempotencyClass: IdempotencyClassIdempotent,
	}
	if s.Valid() {
		t.Error("Valid() = true for empty kind, want false")
	}
}

// TestSideEffectValid_EmptyTarget verifies that Valid() returns false when
// Target is the empty string (specs/control-points.md §6.1.6: target is
// required and identifies the event name | state key | effector id).
func TestSideEffectValid_EmptyTarget(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "",
		IdempotencyClass: IdempotencyClassIdempotent,
	}
	if s.Valid() {
		t.Error("Valid() = true for empty target, want false")
	}
}

// TestSideEffectValid_InvalidIdempotencyClass verifies that Valid() returns
// false when IdempotencyClass is not a declared constant.
func TestSideEffectValid_InvalidIdempotencyClass(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "event-name",
		IdempotencyClass: IdempotencyClass("unknown-class"),
	}
	if s.Valid() {
		t.Error("Valid() = true for invalid idempotency_class, want false")
	}
}

// TestSideEffectValid_EmptyIdempotencyClass verifies that Valid() returns
// false when IdempotencyClass is the empty string.
func TestSideEffectValid_EmptyIdempotencyClass(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "event-name",
		IdempotencyClass: IdempotencyClass(""),
	}
	if s.Valid() {
		t.Error("Valid() = true for empty idempotency_class, want false")
	}
}

// TestSideEffectValid_ZeroValue verifies that a zero-value SideEffect is not
// valid (kind and idempotency_class are both empty strings, target is empty).
func TestSideEffectValid_ZeroValue(t *testing.T) {
	t.Parallel()

	var s SideEffect
	if s.Valid() {
		t.Error("Valid() = true for zero-value SideEffect, want false")
	}
}

// TestSideEffectValid_NilPayloadIsAccepted verifies that Valid() returns true
// when Payload is nil — the spec allows the payload field to be absent and it
// is opaque to this layer.
func TestSideEffectValid_NilPayloadIsAccepted(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "node.started",
		Payload:          nil,
		IdempotencyClass: IdempotencyClassNonIdempotent,
	}
	if !s.Valid() {
		t.Error("Valid() = false for nil payload, want true (payload is opaque)")
	}
}

// TestSideEffectValid_EmptyPayloadIsAccepted verifies that an empty (non-nil)
// Payload map is accepted by Valid().
func TestSideEffectValid_EmptyPayloadIsAccepted(t *testing.T) {
	t.Parallel()

	s := SideEffect{
		Kind:             SideEffectKindExternalAction,
		Target:           "effector-id",
		Payload:          map[string]any{},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
	if !s.Valid() {
		t.Error("Valid() = false for empty payload map, want true")
	}
}

// TestSideEffectJSONRoundTrip verifies that a fully-populated SideEffect
// survives a JSON marshal/unmarshal round-trip with all fields intact
// (specs/control-points.md §6.1.6 wire shape).
func TestSideEffectJSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := sideEffectFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got SideEffect
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if got.Target != orig.Target {
		t.Errorf("Target: got %q, want %q", got.Target, orig.Target)
	}
	if got.IdempotencyClass != orig.IdempotencyClass {
		t.Errorf("IdempotencyClass: got %q, want %q", got.IdempotencyClass, orig.IdempotencyClass)
	}
	if len(got.Payload) != len(orig.Payload) {
		t.Errorf("Payload length: got %d, want %d", len(got.Payload), len(orig.Payload))
	}
	if !got.Valid() {
		t.Error("round-tripped SideEffect.Valid() = false, want true")
	}
}

// TestSideEffectJSONFieldNames verifies that SideEffect serialises with the
// snake_case field names declared in the spec
// (specs/control-points.md §6.1.6).
func TestSideEffectJSONFieldNames(t *testing.T) {
	t.Parallel()

	s := sideEffectFixture(t)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"kind", "target", "payload", "idempotency_class"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON output missing key %q", key)
		}
	}
}
