package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// eventRegistryReset replaces the global registry with an empty one.
// MUST be called only from test cleanup (t.Cleanup) to restore state.
// Not exported — visible only to tests in the same package (package core).
func eventRegistryReset() {
	globalEventRegistry.mu.Lock()
	defer globalEventRegistry.mu.Unlock()
	globalEventRegistry.constructors = make(map[string]func() EventPayload)
}

// Test-only payload types — defined here so they never leak to production code.
type testPayloadAlpha struct {
	Foo string `json:"foo"`
	Bar int    `json:"bar"`
}

type testPayloadBeta struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// minimalEvent returns a valid Event with the given type and payload.
// Helper used by multiple subtests.
func minimalEvent(t *testing.T, typeName string, payloadJSON []byte) Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return Event{
		EventID:         EventID(id),
		SchemaVersion:   1,
		Type:            typeName,
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         payloadJSON,
	}
}

// TestRegistry groups all registry tests sequentially under one driver so that
// tests that mutate the shared package-level registry don't interfere with each
// other. Subtests are NOT run with t.Parallel() because they share global state.
// Only the concurrent-registration subtest spawns goroutines internally.
//
// Rationale: package-level registry is reset by eventRegistryReset() in
// t.Cleanup, but parallel subtests within the same TestRegistry run would race
// against each other's resets. Sequential subtests each get a clean registry.
func TestRegistry(t *testing.T) {
	t.Run("RegisterEventType_Succeeds", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.alpha"
		err := RegisterEventType(typeName, func() EventPayload { return &testPayloadAlpha{} })
		if err != nil {
			t.Fatalf("RegisterEventType returned unexpected error: %v", err)
		}

		// Confirm the type is reachable via DecodePayload round-trip.
		want := &testPayloadAlpha{Foo: "hello", Bar: 42}
		raw, _ := json.Marshal(want)
		ev := minimalEvent(t, typeName, raw)
		got, err := ev.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload returned unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("DecodePayload result mismatch: got %+v, want %+v", got, want)
		}
	})

	t.Run("RegisterEventType_DuplicateReturnsErrDuplicate", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.alpha.dup"
		ctor := func() EventPayload { return &testPayloadAlpha{} }

		if err := RegisterEventType(typeName, ctor); err != nil {
			t.Fatalf("first registration failed unexpectedly: %v", err)
		}
		err := RegisterEventType(typeName, ctor)
		if err == nil {
			t.Fatal("second registration: expected ErrDuplicateEventType, got nil")
		}
		if !errors.Is(err, ErrDuplicateEventType) {
			t.Errorf("second registration: got %v, want errors.Is(ErrDuplicateEventType)", err)
		}
	})

	t.Run("DecodePayload_RoundTrip", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.beta.roundtrip"
		if err := RegisterEventType(typeName, func() EventPayload { return &testPayloadBeta{} }); err != nil {
			t.Fatalf("RegisterEventType: %v", err)
		}

		want := &testPayloadBeta{Name: "widget", Score: 3.14}
		raw, _ := json.Marshal(want)
		ev := minimalEvent(t, typeName, raw)

		got, err := ev.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
		}
	})

	t.Run("DecodePayload_UnknownTypeReturnsErrUnknown", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		ev := minimalEvent(t, "no-such-type", json.RawMessage(`{}`))
		_, err := ev.DecodePayload()
		if err == nil {
			t.Fatal("expected ErrUnknownEventType, got nil")
		}
		if !errors.Is(err, ErrUnknownEventType) {
			t.Errorf("got %v, want errors.Is(ErrUnknownEventType)", err)
		}
	})

	t.Run("DecodePayload_MalformedJSON", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.alpha.malformed"
		if err := RegisterEventType(typeName, func() EventPayload { return &testPayloadAlpha{} }); err != nil {
			t.Fatalf("RegisterEventType: %v", err)
		}

		ev := minimalEvent(t, typeName, json.RawMessage(`not json`))
		_, err := ev.DecodePayload()
		if err == nil {
			t.Fatal("expected a JSON error, got nil")
		}
		var syntaxErr *json.SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Errorf("expected *json.SyntaxError, got %T: %v", err, err)
		}
	})

	t.Run("ConcurrentRegistration", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeA = "test.concurrent.alpha"
		const typeB = "test.concurrent.beta"

		var wg sync.WaitGroup
		errA := make(chan error, 1)
		errB := make(chan error, 1)

		wg.Add(2)
		go func() {
			defer wg.Done()
			errA <- RegisterEventType(typeA, func() EventPayload { return &testPayloadAlpha{} })
		}()
		go func() {
			defer wg.Done()
			errB <- RegisterEventType(typeB, func() EventPayload { return &testPayloadBeta{} })
		}()
		wg.Wait()

		if err := <-errA; err != nil {
			t.Errorf("typeA registration failed: %v", err)
		}
		if err := <-errB; err != nil {
			t.Errorf("typeB registration failed: %v", err)
		}

		// Both types must be reachable.
		rawA, _ := json.Marshal(&testPayloadAlpha{Foo: "concurrent", Bar: 1})
		evA := minimalEvent(t, typeA, rawA)
		if _, err := evA.DecodePayload(); err != nil {
			t.Errorf("DecodePayload for typeA after concurrent registration: %v", err)
		}

		rawB, _ := json.Marshal(&testPayloadBeta{Name: "concurrent", Score: 2.0})
		evB := minimalEvent(t, typeB, rawB)
		if _, err := evB.DecodePayload(); err != nil {
			t.Errorf("DecodePayload for typeB after concurrent registration: %v", err)
		}
	})
}
