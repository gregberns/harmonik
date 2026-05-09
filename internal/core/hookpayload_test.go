package core

import (
	"encoding/json"
	"testing"
)

// hookPayloadFixture returns a fully-populated HookPayload with all fields set
// to valid non-zero values, suitable for structural and round-trip tests
// (hk-a8bg.63).
func hookPayloadFixture(t *testing.T) HookPayload {
	t.Helper()

	filter := "event.payload.status == \"ready\""
	return HookPayload{
		TriggerEvent:       "node.completed",
		SubscriptionFilter: &filter,
		SideEffectKind:     SideEffectKindEmitEvent,
		HaltOnFailure:      false,
		SubsystemPriority:  10,
	}
}

// TestNewHookPayload_HaltOnFailureDefault verifies that NewHookPayload sets
// HaltOnFailure to false per specs/control-points.md §4.3.CP-015.
func TestNewHookPayload_HaltOnFailureDefault(t *testing.T) {
	t.Parallel()

	hp := NewHookPayload()
	if hp.HaltOnFailure {
		t.Error("NewHookPayload().HaltOnFailure = true, want false (default per CP-015)")
	}
}

// TestNewHookPayload_ZeroValueOtherFields verifies that NewHookPayload leaves
// TriggerEvent, SideEffectKind, SubsystemPriority, and SubscriptionFilter at
// their zero values so callers know they must set them.
func TestNewHookPayload_ZeroValueOtherFields(t *testing.T) {
	t.Parallel()

	hp := NewHookPayload()
	if hp.TriggerEvent != "" {
		t.Errorf("NewHookPayload().TriggerEvent = %q, want empty", hp.TriggerEvent)
	}
	if hp.SideEffectKind != "" {
		t.Errorf("NewHookPayload().SideEffectKind = %q, want empty", hp.SideEffectKind)
	}
	if hp.SubscriptionFilter != nil {
		t.Errorf("NewHookPayload().SubscriptionFilter = %v, want nil", hp.SubscriptionFilter)
	}
	if hp.SubsystemPriority != 0 {
		t.Errorf("NewHookPayload().SubsystemPriority = %d, want 0", hp.SubsystemPriority)
	}
}

// TestHookPayloadValid_FullyPopulated verifies that a fully-populated fixture
// is valid.
func TestHookPayloadValid_FullyPopulated(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	if !hp.Valid() {
		t.Error("hookPayloadFixture should be valid, but Valid() returned false")
	}
}

// TestHookPayloadValid_EmptyTriggerEvent verifies that an empty TriggerEvent
// makes the payload invalid (specs/control-points.md §6.1.2).
func TestHookPayloadValid_EmptyTriggerEvent(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.TriggerEvent = ""
	if hp.Valid() {
		t.Error("HookPayload with empty TriggerEvent should be invalid")
	}
}

// TestHookPayloadValid_InvalidSideEffectKind verifies that an unknown
// SideEffectKind makes the payload invalid.
func TestHookPayloadValid_InvalidSideEffectKind(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SideEffectKind = SideEffectKind("unknown-kind")
	if hp.Valid() {
		t.Error("HookPayload with invalid SideEffectKind should be invalid")
	}
}

// TestHookPayloadValid_EmptySideEffectKind verifies that an empty SideEffectKind
// makes the payload invalid.
func TestHookPayloadValid_EmptySideEffectKind(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SideEffectKind = SideEffectKind("")
	if hp.Valid() {
		t.Error("HookPayload with empty SideEffectKind should be invalid")
	}
}

// TestHookPayloadValid_AllSideEffectKinds verifies that each declared
// SideEffectKind value produces a valid payload.
func TestHookPayloadValid_AllSideEffectKinds(t *testing.T) {
	t.Parallel()

	kinds := []SideEffectKind{
		SideEffectKindEmitEvent,
		SideEffectKindStateMutation,
		SideEffectKindExternalAction,
	}
	for _, k := range kinds {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			hp := hookPayloadFixture(t)
			hp.SideEffectKind = k
			if !hp.Valid() {
				t.Errorf("HookPayload with SideEffectKind=%q should be valid", k)
			}
		})
	}
}

// TestHookPayloadValid_NilSubscriptionFilter verifies that a nil
// SubscriptionFilter is valid (filter is optional, None = fire on all events).
func TestHookPayloadValid_NilSubscriptionFilter(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SubscriptionFilter = nil
	if !hp.Valid() {
		t.Error("HookPayload with nil SubscriptionFilter should be valid")
	}
}

// TestHookPayloadValid_NonNilSubscriptionFilter verifies that a non-nil
// SubscriptionFilter is accepted by Valid() without evaluating the expression.
func TestHookPayloadValid_NonNilSubscriptionFilter(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	filter := "some.filter.expression"
	hp.SubscriptionFilter = &filter
	if !hp.Valid() {
		t.Error("HookPayload with non-nil SubscriptionFilter should be valid")
	}
}

// TestHookPayloadValid_HaltOnFailureTrue verifies that HaltOnFailure=true is
// valid (it is a bool flag, any value is structurally legal).
func TestHookPayloadValid_HaltOnFailureTrue(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.HaltOnFailure = true
	if !hp.Valid() {
		t.Error("HookPayload with HaltOnFailure=true should be valid")
	}
}

// TestHookPayloadValid_NegativeSubsystemPriority verifies that a negative
// SubsystemPriority is structurally accepted (no domain constraint at the type
// level per §6.1.2).
func TestHookPayloadValid_NegativeSubsystemPriority(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SubsystemPriority = -1
	if !hp.Valid() {
		t.Error("HookPayload with SubsystemPriority=-1 should be structurally valid")
	}
}

// TestHookPayloadValid_ZeroSubsystemPriority verifies that SubsystemPriority=0
// is accepted.
func TestHookPayloadValid_ZeroSubsystemPriority(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SubsystemPriority = 0
	if !hp.Valid() {
		t.Error("HookPayload with SubsystemPriority=0 should be valid")
	}
}

// TestHookPayloadValid_ZeroValue verifies that a zero-value HookPayload is
// invalid (TriggerEvent is empty, SideEffectKind is empty).
func TestHookPayloadValid_ZeroValue(t *testing.T) {
	t.Parallel()

	var hp HookPayload
	if hp.Valid() {
		t.Error("zero-value HookPayload should be invalid")
	}
}

// TestHookPayloadJSONRoundTrip verifies that a fully-populated HookPayload
// survives a JSON marshal/unmarshal round-trip with all fields intact
// (specs/control-points.md §6.1.2 wire shape).
func TestHookPayloadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := hookPayloadFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got HookPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.TriggerEvent != orig.TriggerEvent {
		t.Errorf("TriggerEvent: got %q, want %q", got.TriggerEvent, orig.TriggerEvent)
	}
	if got.SideEffectKind != orig.SideEffectKind {
		t.Errorf("SideEffectKind: got %q, want %q", got.SideEffectKind, orig.SideEffectKind)
	}
	if got.HaltOnFailure != orig.HaltOnFailure {
		t.Errorf("HaltOnFailure: got %v, want %v", got.HaltOnFailure, orig.HaltOnFailure)
	}
	if got.SubsystemPriority != orig.SubsystemPriority {
		t.Errorf("SubsystemPriority: got %d, want %d", got.SubsystemPriority, orig.SubsystemPriority)
	}
	switch {
	case orig.SubscriptionFilter == nil && got.SubscriptionFilter != nil:
		t.Errorf("SubscriptionFilter: want nil, got %q", *got.SubscriptionFilter)
	case orig.SubscriptionFilter != nil && got.SubscriptionFilter == nil:
		t.Errorf("SubscriptionFilter: want %q, got nil", *orig.SubscriptionFilter)
	case orig.SubscriptionFilter != nil && got.SubscriptionFilter != nil:
		if *got.SubscriptionFilter != *orig.SubscriptionFilter {
			t.Errorf("SubscriptionFilter: got %q, want %q", *got.SubscriptionFilter, *orig.SubscriptionFilter)
		}
	}
	if !got.Valid() {
		t.Error("round-tripped HookPayload.Valid() = false, want true")
	}
}

// TestHookPayloadJSONFieldNames verifies that HookPayload serialises with the
// snake_case field names declared in the spec
// (specs/control-points.md §6.1.2).
func TestHookPayloadJSONFieldNames(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	data, err := json.Marshal(hp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"trigger_event", "side_effect_kind", "halt_on_failure", "subsystem_priority"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON output missing key %q", key)
		}
	}
}

// TestHookPayloadJSONSubscriptionFilterOmitEmpty verifies that a nil
// SubscriptionFilter is omitted from the JSON output (omitempty).
func TestHookPayloadJSONSubscriptionFilterOmitEmpty(t *testing.T) {
	t.Parallel()

	hp := hookPayloadFixture(t)
	hp.SubscriptionFilter = nil
	data, err := json.Marshal(hp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	if _, ok := m["subscription_filter"]; ok {
		t.Error("JSON output should omit subscription_filter when nil")
	}
}

// TestHookPayloadJSONSubscriptionFilterPresent verifies that a non-nil
// SubscriptionFilter appears in the JSON output.
func TestHookPayloadJSONSubscriptionFilterPresent(t *testing.T) {
	t.Parallel()

	filter := "event.status == \"done\""
	hp := hookPayloadFixture(t)
	hp.SubscriptionFilter = &filter
	data, err := json.Marshal(hp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	if _, ok := m["subscription_filter"]; !ok {
		t.Error("JSON output should include subscription_filter when non-nil")
	}
}
