package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// validEvent returns a fully-populated Event with all required fields set to
// non-zero values and all optional fields set to non-nil valid values.
func validEvent(t *testing.T) Event {
	t.Helper()

	monoNs := int64(1_000_000)
	runID := RunID(uuid.Must(uuid.NewV7()))
	stateID := StateID(uuid.Must(uuid.NewV7()))
	tc := validTraceContext(t)

	return Event{
		EventID:           uuid.Must(uuid.NewV7()),
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

func TestEventValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	if !e.Valid() {
		t.Error("Valid() = false for fully-populated Event, want true")
	}
}

func TestEventValid_AllOptionalsNil(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.TimestampMonoNsec = nil
	e.RunID = nil
	e.StateID = nil
	e.TraceContext = nil
	if !e.Valid() {
		t.Error("Valid() = false when all optional fields are nil, want true")
	}
}

func TestEventValid_EmptyPayloadIsValid(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.Payload = json.RawMessage{}
	if !e.Valid() {
		t.Error("Valid() = false with non-nil but empty Payload, want true (zero-length payload allowed for no-body event types)")
	}
}

func TestEventValid_ZeroEventID(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.EventID = uuid.Nil
	if e.Valid() {
		t.Error("Valid() = true with zero EventID (uuid.Nil), want false")
	}
}

func TestEventValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.SchemaVersion = 0
	if e.Valid() {
		t.Error("Valid() = true with zero SchemaVersion, want false")
	}
}

func TestEventValid_EmptyType(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.Type = ""
	if e.Valid() {
		t.Error("Valid() = true with empty Type, want false")
	}
}

func TestEventValid_ZeroTimestampWall(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.TimestampWall = time.Time{}
	if e.Valid() {
		t.Error("Valid() = true with zero TimestampWall, want false")
	}
}

func TestEventValid_ZeroTimestampMonoNsec(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	zero := int64(0)
	e.TimestampMonoNsec = &zero
	if e.Valid() {
		t.Error("Valid() = true with TimestampMonoNsec = 0 (non-nil), want false")
	}
}

func TestEventValid_NegativeTimestampMonoNsec(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	neg := int64(-1)
	e.TimestampMonoNsec = &neg
	if e.Valid() {
		t.Error("Valid() = true with TimestampMonoNsec = -1 (negative), want false")
	}
}

func TestEventValid_NilRunIDIsValid(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.RunID = nil
	if !e.Valid() {
		t.Error("Valid() = false with nil RunID, want true (RunID is optional)")
	}
}

func TestEventValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	zero := RunID(uuid.Nil)
	e.RunID = &zero
	if e.Valid() {
		t.Error("Valid() = true with non-nil RunID pointing to uuid.Nil, want false")
	}
}

func TestEventValid_NilStateIDIsValid(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.StateID = nil
	if !e.Valid() {
		t.Error("Valid() = false with nil StateID, want true (StateID is optional)")
	}
}

func TestEventValid_ZeroStateID(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	zero := StateID(uuid.Nil)
	e.StateID = &zero
	if e.Valid() {
		t.Error("Valid() = true with non-nil StateID pointing to uuid.Nil, want false")
	}
}

func TestEventValid_EmptySourceSubsystem(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.SourceSubsystem = ""
	if e.Valid() {
		t.Error("Valid() = true with empty SourceSubsystem, want false")
	}
}

func TestEventValid_NilTraceContextIsValid(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.TraceContext = nil
	if !e.Valid() {
		t.Error("Valid() = false with nil TraceContext, want true (TraceContext is optional)")
	}
}

func TestEventValid_InvalidTraceContext(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	nilUUID := uuid.Nil
	invalidTC := TraceContext{
		ParentEventID: &nilUUID, // non-nil but uuid.Nil — invalid per TraceContext.Valid()
	}
	e.TraceContext = &invalidTC
	if e.Valid() {
		t.Error("Valid() = true with non-nil but invalid TraceContext, want false")
	}
}

func TestEventValid_NilPayload(t *testing.T) {
	t.Parallel()

	e := validEvent(t)
	e.Payload = nil
	if e.Valid() {
		t.Error("Valid() = true with nil Payload, want false")
	}
}
