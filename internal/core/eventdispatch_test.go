package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// dispatchFixturePayloadGamma is a test-only payload type for dispatch tests.
type dispatchFixturePayloadGamma struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// dispatchFixtureBuildEvent returns a valid Event with the given type and raw
// payload JSON. Fatals the test on UUID generation failure.
func dispatchFixtureBuildEvent(t *testing.T, typeName string, payloadJSON []byte) Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("dispatchFixtureBuildEvent: uuid.NewV7: %v", err)
	}
	return Event{
		EventID:         EventID(id),
		SchemaVersion:   1,
		Type:            typeName,
		TimestampWall:   time.Now(),
		SourceSubsystem: "test.dispatch",
		Payload:         payloadJSON,
	}
}

// dispatchFixtureRegisterGamma registers the dispatchFixturePayloadGamma
// constructor under the given typeName. Fatals the test on error.
func dispatchFixtureRegisterGamma(t *testing.T, typeName string) {
	t.Helper()
	if err := RegisterEventType(typeName, func() EventPayload { return &dispatchFixturePayloadGamma{} }); err != nil {
		t.Fatalf("dispatchFixtureRegisterGamma: RegisterEventType(%q): %v", typeName, err)
	}
}

// TestDispatch groups all dispatch-layer tests. Subtests are sequential because
// they share the package-level registry; each subtest resets via t.Cleanup.
func TestDispatch(t *testing.T) {
	t.Run("Observational_HitReturnsPayload", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "dispatch.test.observational.hit"
		dispatchFixtureRegisterGamma(t, typeName)

		want := &dispatchFixturePayloadGamma{Value: "hello", Count: 7}
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		ev := dispatchFixtureBuildEvent(t, typeName, raw)

		got, err := DispatchObservational(ev)
		if err != nil {
			t.Fatalf("DispatchObservational: unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("DispatchObservational payload mismatch: got %+v, want %+v", got, want)
		}
	})

	t.Run("Observational_MissReturnsErrSkipUnknown", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		ev := dispatchFixtureBuildEvent(t, "dispatch.test.no-such-type", json.RawMessage(`{}`))

		got, err := DispatchObservational(ev)
		if got != nil {
			t.Errorf("DispatchObservational: expected nil payload, got %+v", got)
		}
		if err == nil {
			t.Fatal("DispatchObservational: expected ErrSkipUnknown, got nil")
		}
		if !errors.Is(err, ErrSkipUnknown) {
			t.Errorf("DispatchObservational: got %v, want errors.Is(ErrSkipUnknown)", err)
		}
		// ErrSkipUnknown MUST NOT wrap ErrUnknownEventType — observational callers
		// should not need to inspect further.
		if errors.Is(err, ErrUnknownEventType) {
			t.Errorf("DispatchObservational: ErrSkipUnknown should not wrap ErrUnknownEventType")
		}
	})

	t.Run("Observational_MalformedJSONPropagatesError", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "dispatch.test.observational.malformed"
		dispatchFixtureRegisterGamma(t, typeName)

		ev := dispatchFixtureBuildEvent(t, typeName, json.RawMessage(`not-json`))

		_, err := DispatchObservational(ev)
		if err == nil {
			t.Fatal("DispatchObservational: expected JSON error, got nil")
		}
		if errors.Is(err, ErrSkipUnknown) {
			t.Error("DispatchObservational: malformed JSON should not produce ErrSkipUnknown")
		}
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Errorf("DispatchObservational: got %T: %v, want *json.SyntaxError", err, err)
		}
	})

	t.Run("Synchronous_HitReturnsPayload", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "dispatch.test.synchronous.hit"
		dispatchFixtureRegisterGamma(t, typeName)

		want := &dispatchFixturePayloadGamma{Value: "sync", Count: 3}
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		ev := dispatchFixtureBuildEvent(t, typeName, raw)

		got, err := DispatchSynchronous(ev)
		if err != nil {
			t.Fatalf("DispatchSynchronous: unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("DispatchSynchronous payload mismatch: got %+v, want %+v", got, want)
		}
	})

	t.Run("Synchronous_MissReturnsStructuredError", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		ev := dispatchFixtureBuildEvent(t, "dispatch.test.no-such-type-sync", json.RawMessage(`{}`))

		got, err := DispatchSynchronous(ev)
		if got != nil {
			t.Errorf("DispatchSynchronous: expected nil payload, got %+v", got)
		}
		if err == nil {
			t.Fatal("DispatchSynchronous: expected structured error, got nil")
		}

		// Must be *DispatchUnknownEventError.
		var dispErr *DispatchUnknownEventError
		if !errors.As(err, &dispErr) {
			t.Fatalf("DispatchSynchronous: got %T: %v, want *DispatchUnknownEventError", err, err)
		}
		if dispErr.EventType != ev.Type {
			t.Errorf("DispatchUnknownEventError.EventType: got %q, want %q", dispErr.EventType, ev.Type)
		}
		if dispErr.EventID != ev.EventID {
			t.Errorf("DispatchUnknownEventError.EventID: got %v, want %v", dispErr.EventID, ev.EventID)
		}

		// Must also satisfy errors.Is(ErrUnknownEventType) via Unwrap.
		if !errors.Is(err, ErrUnknownEventType) {
			t.Errorf("DispatchSynchronous: error must wrap ErrUnknownEventType via Unwrap")
		}
	})

	t.Run("Synchronous_MalformedJSONPropagatesError", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "dispatch.test.synchronous.malformed"
		dispatchFixtureRegisterGamma(t, typeName)

		ev := dispatchFixtureBuildEvent(t, typeName, json.RawMessage(`not-json`))

		_, err := DispatchSynchronous(ev)
		if err == nil {
			t.Fatal("DispatchSynchronous: expected JSON error, got nil")
		}
		var dispErr *DispatchUnknownEventError
		if errors.As(err, &dispErr) {
			t.Error("DispatchSynchronous: malformed JSON should not produce *DispatchUnknownEventError")
		}
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Errorf("DispatchSynchronous: got %T: %v, want *json.SyntaxError", err, err)
		}
	})

	// DeterministicLookup asserts EV-033's "deterministic map lookup" guarantee:
	// repeated calls for the same registered type always produce a payload of the
	// same Go type.
	t.Run("DeterministicLookup_SameTypeAcrossIterations", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "dispatch.test.deterministic"
		dispatchFixtureRegisterGamma(t, typeName)

		raw, err := json.Marshal(&dispatchFixturePayloadGamma{Value: "x", Count: 1})
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		ev := dispatchFixtureBuildEvent(t, typeName, raw)

		const iterations = 20
		var firstType reflect.Type
		for i := range iterations {
			p, err := DispatchObservational(ev)
			if err != nil {
				t.Fatalf("iteration %d: DispatchObservational: %v", i, err)
			}
			rt := reflect.TypeOf(p)
			if i == 0 {
				firstType = rt
				continue
			}
			if rt != firstType {
				t.Errorf("iteration %d: type changed: got %v, want %v", i, rt, firstType)
			}
		}
	})
}
