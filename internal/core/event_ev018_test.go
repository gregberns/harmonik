// Package core — EV-018 idempotent-emission sensors.
//
// This file provides named, requirement-traceable sensors for the
// idempotent-emission discipline defined in event-model.md §4.4 EV-018.
//
// EV-018 states: every producer MUST emit each event in idempotent form.
// Re-emitting the same event (same event_id, same payload) during recovery
// MUST be safe for downstream observational consumers. Producers MUST NOT
// encode one-shot side-effect semantics into event payloads.
//
// The tests below exercise Event.EquivalentTo, which is the data-shape sensor
// for this discipline. They are intentionally named with "EV018" so that a
// grep or test filter can locate all requirement-traceable sensors for EV-018.
package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// baseEventEV018 returns a minimal valid Event suitable for EV-018 tests.
// Optional fields (RunID, StateID, TraceContext, TimestampMonoNsec) are
// deliberately nil to keep tests focused on the idempotency-identity fields.
func baseEventEV018(t *testing.T) Event {
	t.Helper()
	return Event{
		EventID:         EventID(uuid.Must(uuid.NewV7())),
		SchemaVersion:   1,
		Type:            "checkpoint_written",
		TimestampWall:   time.Now(),
		SourceSubsystem: "github.com/harmonik/internal/orchestrator",
		Payload:         json.RawMessage(`{"step":1,"status":"ok"}`),
	}
}

// TestEventEV018_SameIDSamePayloadEquivalent asserts that two Event values built
// with the same EventID, same Type, and same payload are idempotency-equivalent
// per event-model.md §4.4 EV-018.
//
// This is the core idempotency assertion: a producer that re-emits an event
// with the same event_id and same payload during recovery produces a value
// that is equivalent to the original.
func TestEventEV018_SameIDSamePayloadEquivalent(t *testing.T) {
	t.Parallel()

	// EV-018: same EventID + same payload + same Type → EquivalentTo must be true.
	original := baseEventEV018(t)

	// Simulate a recovery re-emit: copy the idempotency-identity fields but allow
	// envelope fields like TimestampWall and SourceSubsystem to differ (as they
	// legitimately would on a re-emit after a process restart).
	reEmit := Event{
		EventID:         original.EventID, // same — required by EV-018
		SchemaVersion:   original.SchemaVersion,
		Type:            original.Type,             // same — required by EV-018
		TimestampWall:   time.Now().Add(time.Hour), // wall clock differs — acceptable
		SourceSubsystem: original.SourceSubsystem,
		Payload:         original.Payload, // same — required by EV-018
	}

	if !original.EquivalentTo(reEmit) {
		t.Error("EquivalentTo = false for same EventID + same Type + same Payload, want true (EV-018)")
	}
}

// TestEventEV018_SameIDDifferentPayloadNotEquivalent asserts that two Event
// values with the same EventID but different payloads are NOT equivalent per
// event-model.md §4.4 EV-018.
//
// Idempotency requires PAYLOAD identity, not merely event_id identity. A
// producer that silently changes the payload between the original emit and a
// recovery re-emit violates EV-018; this test is the data-shape defense against
// that payload-drift failure mode.
func TestEventEV018_SameIDDifferentPayloadNotEquivalent(t *testing.T) {
	t.Parallel()

	original := baseEventEV018(t)

	// Same EventID, same Type, but payload has drifted — EV-018 violation.
	drifted := Event{
		EventID:         original.EventID,
		SchemaVersion:   original.SchemaVersion,
		Type:            original.Type,
		TimestampWall:   original.TimestampWall,
		SourceSubsystem: original.SourceSubsystem,
		Payload:         json.RawMessage(`{"step":2,"status":"ok"}`), // different payload
	}

	if original.EquivalentTo(drifted) {
		t.Error("EquivalentTo = true for same EventID but different Payload, want false (EV-018 requires payload identity)")
	}
}

// TestEventEV018_DifferentIDNotEquivalent asserts that two Event values with
// different EventIDs are NOT equivalent, even when all other fields are
// identical, per event-model.md §4.4 EV-018.
//
// The EventID is the primary idempotency key. Two events with distinct IDs are
// distinct events regardless of payload similarity.
func TestEventEV018_DifferentIDNotEquivalent(t *testing.T) {
	t.Parallel()

	original := baseEventEV018(t)

	// Different EventID, same payload — these are distinct events.
	differentID := Event{
		EventID:         EventID(uuid.Must(uuid.NewV7())), // different ID
		SchemaVersion:   original.SchemaVersion,
		Type:            original.Type,
		TimestampWall:   original.TimestampWall,
		SourceSubsystem: original.SourceSubsystem,
		Payload:         original.Payload, // same payload
	}

	if original.EquivalentTo(differentID) {
		t.Error("EquivalentTo = true for different EventID, want false (EV-018)")
	}
}

// TestEventEV018_PayloadOrderIndependent asserts that JSON object payloads that
// differ only in key ordering are considered equivalent per EV-018.
//
// Producers may re-serialize a map payload during recovery; Go's map iteration
// order is randomized, so key ordering is not stable. EquivalentTo MUST treat
// {"a":1,"b":2} and {"b":2,"a":1} as the same payload.
func TestEventEV018_PayloadOrderIndependent(t *testing.T) {
	t.Parallel()

	base := baseEventEV018(t)
	base.Payload = json.RawMessage(`{"a":1,"b":2}`)

	reordered := base
	reordered.Payload = json.RawMessage(`{"b":2,"a":1}`)

	if !base.EquivalentTo(reordered) {
		t.Error("EquivalentTo = false for payloads that differ only in JSON key order, want true (EV-018 order-independent comparison)")
	}
}
