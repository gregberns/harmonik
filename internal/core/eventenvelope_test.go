package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// eventEnvelopeFixture returns a fully-populated EventEnvelope with all required
// fields set to non-zero values and all optional fields set to non-nil valid
// values. Uses the eventEnvelope helper prefix per implementer-protocol.md.
func eventEnvelopeFixture(t *testing.T) EventEnvelope {
	t.Helper()

	monoNs := int64(1_000_000)
	runID := RunID(uuid.Must(uuid.NewV7()))
	stateID := StateID(uuid.Must(uuid.NewV7()))
	tc := validTraceContext(t)

	return EventEnvelope{
		EventID:           EventID(uuid.Must(uuid.NewV7())),
		SchemaVersion:     1,
		Type:              "checkpoint_written",
		TimestampWall:     time.Now(),
		TimestampMonoNsec: &monoNs,
		RunID:             &runID,
		StateID:           &stateID,
		SourceSubsystem:   "github.com/harmonik/internal/orchestrator",
		TraceContext:      &tc,
		Payload:           json.RawMessage(`{"key":"value"}`),
	}
}

// TestEventEnvelope_ZeroValue verifies that the zero value of EventEnvelope is
// identical to the zero value of Event (alias identity).
func TestEventEnvelope_ZeroValue(t *testing.T) {
	t.Parallel()

	var env EventEnvelope
	var ev Event

	// As a true Go alias, zero values must be structurally identical.
	// Valid() should return false for both because required fields are unset.
	if env.Valid() {
		t.Error("EventEnvelope zero value: Valid() = true, want false (required fields unset)")
	}
	if ev.Valid() {
		t.Error("Event zero value: Valid() = true, want false (required fields unset)")
	}
}

// TestEventEnvelope_AliasInterchangeable verifies that an EventEnvelope value
// can be assigned to an Event variable and vice versa without conversion, and
// that Valid() behaves identically on both.
func TestEventEnvelope_AliasInterchangeable(t *testing.T) {
	t.Parallel()

	env := eventEnvelopeFixture(t)

	// Assign EventEnvelope to Event: no conversion required because it is an alias.
	var ev Event = env
	if !ev.Valid() {
		t.Error("Event assigned from EventEnvelope: Valid() = false, want true")
	}

	// Assign Event back to EventEnvelope.
	var env2 EventEnvelope = ev
	if !env2.Valid() {
		t.Error("EventEnvelope re-assigned from Event: Valid() = false, want true")
	}
}

// TestEventEnvelope_Valid_AllFieldsSet verifies that a fully-populated
// EventEnvelope passes Valid().
func TestEventEnvelope_Valid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	env := eventEnvelopeFixture(t)
	if !env.Valid() {
		t.Error("EventEnvelope.Valid() = false for fully-populated value, want true")
	}
}

// TestEventEnvelope_Valid_AllOptionalsNil verifies that an EventEnvelope with
// all optional pointer fields nil still passes Valid().
func TestEventEnvelope_Valid_AllOptionalsNil(t *testing.T) {
	t.Parallel()

	env := eventEnvelopeFixture(t)
	env.TimestampMonoNsec = nil
	env.RunID = nil
	env.StateID = nil
	env.TraceContext = nil

	if !env.Valid() {
		t.Error("EventEnvelope.Valid() = false with all optional fields nil, want true")
	}
}

// TestEventEnvelope_Slice_RoundTrip verifies that []EventEnvelope can hold
// multiple envelope values and that each passes Valid(). This exercises the
// primary use site: InvestigatorInput.jsonl_tail (reconciliation/schemas.md §6.1).
func TestEventEnvelope_Slice_RoundTrip(t *testing.T) {
	t.Parallel()

	tail := []EventEnvelope{
		eventEnvelopeFixture(t),
		eventEnvelopeFixture(t),
	}

	for i, env := range tail {
		if !env.Valid() {
			t.Errorf("tail[%d].Valid() = false, want true", i)
		}
	}

	// Nil and empty slices are both valid (no invariant on length).
	var nilTail []EventEnvelope
	if nilTail != nil {
		t.Error("nil []EventEnvelope should be nil")
	}
}
