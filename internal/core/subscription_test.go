package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// subscriptionValidV7 returns a UUIDv7 EventID for use in subscription test fixtures.
func subscriptionValidV7(t *testing.T) EventID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("subscriptionValidV7: uuid.NewV7(): %v", err)
	}
	return EventID(u)
}

// subscriptionMinimal returns a fully-valid Subscription with the smallest
// possible populated set of required fields. Tests mutate individual fields
// to probe Valid().
func subscriptionMinimal(t *testing.T) Subscription {
	t.Helper()
	return Subscription{
		ConsumerID:              "consumer-a",
		ConsumerClass:           ConsumerClassAsynchronous,
		EventPattern:            EventPattern{Wildcard: true, Types: map[EventType]struct{}{}},
		Since:                   nil,
		OffsetCheckpointEventID: nil,
		OnPanic:                 OnPanicRecoverAndLog,
		Handler:                 func(_ context.Context, _ Event) error { return nil },
	}
}

// --- Valid() tests — required fields ---

func TestSubscriptionValid_Minimal(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	if !s.Valid() {
		t.Error("Valid() = false for minimal Subscription, want true")
	}
}

func TestSubscriptionValid_AllConsumerClasses(t *testing.T) {
	t.Parallel()

	for _, cls := range []ConsumerClass{ConsumerClassSynchronous, ConsumerClassAsynchronous, ConsumerClassObserver} {
		cls := cls
		t.Run(string(cls), func(t *testing.T) {
			t.Parallel()
			s := subscriptionMinimal(t)
			s.ConsumerClass = cls
			if !s.Valid() {
				t.Errorf("Valid() = false for ConsumerClass=%q, want true", cls)
			}
		})
	}
}

func TestSubscriptionValid_AllOnPanicPolicies(t *testing.T) {
	t.Parallel()

	for _, policy := range []OnPanic{OnPanicRecoverAndLog, OnPanicQuarantineConsumer, OnPanicFailDaemon} {
		policy := policy
		t.Run(string(policy), func(t *testing.T) {
			t.Parallel()
			s := subscriptionMinimal(t)
			s.OnPanic = policy
			if !s.Valid() {
				t.Errorf("Valid() = false for OnPanic=%q, want true", policy)
			}
		})
	}
}

func TestSubscriptionValid_WithOptionalSince(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	id := subscriptionValidV7(t)
	s.Since = &id
	if !s.Valid() {
		t.Error("Valid() = false with non-nil Since, want true")
	}
}

func TestSubscriptionValid_WithOptionalOffsetCheckpoint(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	id := subscriptionValidV7(t)
	s.OffsetCheckpointEventID = &id
	if !s.Valid() {
		t.Error("Valid() = false with non-nil OffsetCheckpointEventID, want true")
	}
}

func TestSubscriptionValid_WithBothOptionalUUIDs(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	since := subscriptionValidV7(t)
	checkpoint := subscriptionValidV7(t)
	s.Since = &since
	s.OffsetCheckpointEventID = &checkpoint
	if !s.Valid() {
		t.Error("Valid() = false with both Since and OffsetCheckpointEventID set, want true")
	}
}

func TestSubscriptionValid_ExplicitEventPattern(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.EventPattern = EventPattern{
		Wildcard: false,
		Types:    map[EventType]struct{}{EventTypeRunStarted: {}, EventTypeRunCompleted: {}},
	}
	if !s.Valid() {
		t.Error("Valid() = false with explicit EventPattern, want true")
	}
}

// --- Valid() tests — required field violations ---

func TestSubscriptionValid_EmptyConsumerID(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.ConsumerID = ""
	if s.Valid() {
		t.Error("Valid() = true with empty ConsumerID, want false")
	}
}

func TestSubscriptionValid_EmptyConsumerClass(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.ConsumerClass = ConsumerClass("")
	if s.Valid() {
		t.Error("Valid() = true with empty ConsumerClass, want false")
	}
}

func TestSubscriptionValid_InvalidConsumerClass(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.ConsumerClass = ConsumerClass("unknown_class")
	if s.Valid() {
		t.Error("Valid() = true with unknown ConsumerClass, want false")
	}
}

func TestSubscriptionValid_InvalidEventPattern(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	// Wildcard=true with non-empty Types violates §6.1 invariant.
	s.EventPattern = EventPattern{
		Wildcard: true,
		Types:    map[EventType]struct{}{EventTypeRunStarted: {}},
	}
	if s.Valid() {
		t.Error("Valid() = true with invalid EventPattern (wildcard+types), want false")
	}
}

func TestSubscriptionValid_InvalidEventPatternNoTypes(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	// Wildcard=false with empty Types violates §6.1 invariant.
	s.EventPattern = EventPattern{Wildcard: false, Types: map[EventType]struct{}{}}
	if s.Valid() {
		t.Error("Valid() = true with invalid EventPattern (explicit+empty types), want false")
	}
}

func TestSubscriptionValid_SinceIsNilUUID(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	// Since non-nil but points to uuid.Nil is invalid.
	nilID := EventID(uuid.Nil)
	s.Since = &nilID
	if s.Valid() {
		t.Error("Valid() = true with Since=uuid.Nil, want false")
	}
}

func TestSubscriptionValid_OffsetCheckpointIsNilUUID(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	// OffsetCheckpointEventID non-nil but points to uuid.Nil is invalid.
	nilID := EventID(uuid.Nil)
	s.OffsetCheckpointEventID = &nilID
	if s.Valid() {
		t.Error("Valid() = true with OffsetCheckpointEventID=uuid.Nil, want false")
	}
}

func TestSubscriptionValid_EmptyOnPanic(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.OnPanic = OnPanic("")
	if s.Valid() {
		t.Error("Valid() = true with empty OnPanic, want false")
	}
}

func TestSubscriptionValid_InvalidOnPanic(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.OnPanic = OnPanic("unknown_policy")
	if s.Valid() {
		t.Error("Valid() = true with unknown OnPanic, want false")
	}
}

func TestSubscriptionValid_NilHandler(t *testing.T) {
	t.Parallel()

	s := subscriptionMinimal(t)
	s.Handler = nil
	if s.Valid() {
		t.Error("Valid() = true with nil Handler, want false")
	}
}

// --- JSON round-trip (serializable fields) ---
//
// Subscription.Handler is a function and encodes as JSON null (Go's encoding/json
// cannot serialise a function pointer; it emits null and sets the field to nil on
// decode). The round-trip test verifies all non-function fields survive
// marshal/unmarshal correctly. Handler being nil after unmarshal is expected and
// documented here explicitly so future readers understand the limitation.

// subscriptionJSONWire is a test-local wire struct carrying all fields except
// Handler, used to verify that the serializable subset of Subscription
// round-trips through JSON without loss.
type subscriptionJSONWire struct {
	ConsumerID              string       `json:"consumer_id"`
	ConsumerClass           string       `json:"consumer_class"`
	EventPattern            EventPattern `json:"event_pattern"`
	Since                   *EventID     `json:"since,omitempty"`
	OffsetCheckpointEventID *EventID     `json:"offset_checkpoint_event_id,omitempty"`
	OnPanic                 string       `json:"on_panic"`
}

func TestSubscriptionJSONRoundTrip_WildcardNoCheckpoints(t *testing.T) {
	t.Parallel()

	orig := subscriptionJSONWire{
		ConsumerID:    "consumer-b",
		ConsumerClass: "observer",
		EventPattern:  EventPattern{Wildcard: true, Types: map[EventType]struct{}{}},
		OnPanic:       "recover_and_log",
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got subscriptionJSONWire
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ConsumerID != orig.ConsumerID {
		t.Errorf("ConsumerID = %q, want %q", got.ConsumerID, orig.ConsumerID)
	}
	if got.ConsumerClass != orig.ConsumerClass {
		t.Errorf("ConsumerClass = %q, want %q", got.ConsumerClass, orig.ConsumerClass)
	}
	if got.EventPattern.Wildcard != orig.EventPattern.Wildcard {
		t.Errorf("EventPattern.Wildcard = %v, want %v", got.EventPattern.Wildcard, orig.EventPattern.Wildcard)
	}
	if got.OnPanic != orig.OnPanic {
		t.Errorf("OnPanic = %q, want %q", got.OnPanic, orig.OnPanic)
	}
	if got.Since != nil {
		t.Errorf("Since = %v, want nil", got.Since)
	}
	if got.OffsetCheckpointEventID != nil {
		t.Errorf("OffsetCheckpointEventID = %v, want nil", got.OffsetCheckpointEventID)
	}
}

func TestSubscriptionJSONRoundTrip_WithCheckpoints(t *testing.T) {
	t.Parallel()

	sinceID := subscriptionValidV7(t)
	checkpointID := subscriptionValidV7(t)

	orig := subscriptionJSONWire{
		ConsumerID:    "consumer-c",
		ConsumerClass: "synchronous",
		EventPattern: EventPattern{
			Wildcard: false,
			Types:    map[EventType]struct{}{EventTypeRunStarted: {}},
		},
		Since:                   &sinceID,
		OffsetCheckpointEventID: &checkpointID,
		OnPanic:                 "quarantine_consumer",
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got subscriptionJSONWire
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ConsumerID != orig.ConsumerID {
		t.Errorf("ConsumerID = %q, want %q", got.ConsumerID, orig.ConsumerID)
	}
	if got.ConsumerClass != orig.ConsumerClass {
		t.Errorf("ConsumerClass = %q, want %q", got.ConsumerClass, orig.ConsumerClass)
	}
	if got.Since == nil {
		t.Fatal("Since = nil after round-trip, want non-nil")
	}
	if *got.Since != sinceID {
		t.Errorf("Since = %v, want %v", *got.Since, sinceID)
	}
	if got.OffsetCheckpointEventID == nil {
		t.Fatal("OffsetCheckpointEventID = nil after round-trip, want non-nil")
	}
	if *got.OffsetCheckpointEventID != checkpointID {
		t.Errorf("OffsetCheckpointEventID = %v, want %v", *got.OffsetCheckpointEventID, checkpointID)
	}
	if got.OnPanic != orig.OnPanic {
		t.Errorf("OnPanic = %q, want %q", got.OnPanic, orig.OnPanic)
	}
	if len(got.EventPattern.Types) != 1 {
		t.Errorf("EventPattern.Types len = %d, want 1", len(got.EventPattern.Types))
	}
	if _, ok := got.EventPattern.Types[EventTypeRunStarted]; !ok {
		t.Error("EventPattern.Types missing \"run_started\" after round-trip")
	}
}

func TestSubscriptionJSONRoundTrip_MalformedInput(t *testing.T) {
	t.Parallel()

	var got subscriptionJSONWire
	if err := json.Unmarshal([]byte(`not-json`), &got); err == nil {
		t.Error("json.Unmarshal: expected error for malformed input, got nil")
	}
}

func TestSubscriptionJSONRoundTrip_MalformedEventID(t *testing.T) {
	t.Parallel()

	// since field receives an invalid UUID string — UnmarshalText on EventID must reject it.
	raw := `{"consumer_id":"c","consumer_class":"observer","event_pattern":{"wildcard":true,"types":[]},"since":"not-a-uuid","on_panic":"recover_and_log"}`
	var got subscriptionJSONWire
	if err := json.Unmarshal([]byte(raw), &got); err == nil {
		t.Error("json.Unmarshal: expected error for invalid since UUID, got nil")
	}
}
