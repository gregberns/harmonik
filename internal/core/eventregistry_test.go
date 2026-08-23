package core

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/testhelpers/hermetic"
)

var initialRegistrySnapshot map[EventType]typeEntry

// hermetic.Setup, not hermetic.Main, because this TestMain already has work of
// its own. It makes this package's tests give the same answer on any machine —
// see internal/testhelpers/hermetic for the seams it redirects and why.
func TestMain(m *testing.M) {
	cleanup := hermetic.Setup()

	globalEventRegistry.mu.Lock()
	initialRegistrySnapshot = make(map[EventType]typeEntry, len(globalEventRegistry.entries))
	for k, v := range globalEventRegistry.entries {
		initialRegistrySnapshot[k] = v
	}
	globalEventRegistry.mu.Unlock()

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func eventRegistryReset() {
	globalEventRegistry.mu.Lock()
	defer globalEventRegistry.mu.Unlock()
	globalEventRegistry.entries = make(map[EventType]typeEntry, len(initialRegistrySnapshot))
	for k, v := range initialRegistrySnapshot {
		globalEventRegistry.entries[k] = v
	}
	globalEventRegistry.sealed = false
}

// TestSealEventRegistry_RejectsPostSealRegistration verifies EV-034: after the
// registry is sealed, registration returns ErrRegistrySealed instead of silently
// mutating the dispatch table mid-dispatch. Read paths remain functional.
func TestSealEventRegistry_RejectsPostSealRegistration(t *testing.T) {
	t.Cleanup(eventRegistryReset)

	if EventRegistrySealed() {
		t.Fatal("registry unexpectedly sealed at test start")
	}

	if err := RegisterEventType("test.ev034.presail", func() EventPayload { return &struct{}{} }); err != nil {
		t.Fatalf("pre-seal RegisterEventType err = %v; want nil", err)
	}

	SealEventRegistry()
	if !EventRegistrySealed() {
		t.Fatal("EventRegistrySealed() = false after SealEventRegistry()")
	}

	err := RegisterEventType("test.ev034.postseal", func() EventPayload { return &struct{}{} })
	if !errors.Is(err, ErrRegistrySealed) {
		t.Errorf("post-seal RegisterEventType err = %v; want wrapping ErrRegistrySealed", err)
	}
	if _, ok := LookupTypeSchemaVersion("test.ev034.postseal"); ok {
		t.Error("post-seal type leaked into the registry despite the rejected registration")
	}

	if err := RegisterEventTypeAtVersion("test.ev034.postseal2", func() EventPayload { return &struct{}{} }, 2); !errors.Is(err, ErrRegistrySealed) {
		t.Errorf("post-seal RegisterEventTypeAtVersion err = %v; want wrapping ErrRegistrySealed", err)
	}

	if _, ok := LookupTypeSchemaVersion("test.ev034.presail"); !ok {
		t.Error("pre-seal type missing from registry after seal; read path must remain functional")
	}
}

// TestSealEventRegistry_Idempotent verifies sealing twice is a harmless no-op.
func TestSealEventRegistry_Idempotent(t *testing.T) {
	t.Cleanup(eventRegistryReset)
	SealEventRegistry()
	SealEventRegistry()
	if !EventRegistrySealed() {
		t.Fatal("registry not sealed after double SealEventRegistry()")
	}
}

type testPayloadAlpha struct {
	Foo string `json:"foo"`
	Bar int    `json:"bar"`
}

type testPayloadBeta struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

func minimalEvent(t *testing.T, typeName EventType, payloadJSON []byte) Event {
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

		want := &testPayloadAlpha{Foo: "hello", Bar: 42}
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
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
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
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

		rawA, err := json.Marshal(&testPayloadAlpha{Foo: "concurrent", Bar: 1})
		if err != nil {
			t.Fatalf("marshal typeA payload: %v", err)
		}
		evA := minimalEvent(t, typeA, rawA)
		if _, err := evA.DecodePayload(); err != nil {
			t.Errorf("DecodePayload for typeA after concurrent registration: %v", err)
		}

		rawB, err := json.Marshal(&testPayloadBeta{Name: "concurrent", Score: 2.0})
		if err != nil {
			t.Fatalf("marshal typeB payload: %v", err)
		}
		evB := minimalEvent(t, typeB, rawB)
		if _, err := evB.DecodePayload(); err != nil {
			t.Errorf("DecodePayload for typeB after concurrent registration: %v", err)
		}
	})
}

// TestEV028_LookupTypeSchemaVersion verifies per-type schema version tracking
// per EV-028 (event-model.md §4.7).
//
// Spec ref: event-model.md §4.7 EV-028 — "schema_version is an integer on the
// envelope and MUST match the schema version of the payload for that type."
func TestEV028_LookupTypeSchemaVersion(t *testing.T) {
	t.Run("DefaultVersion1_ViaRegisterEventType", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.ev028.default"
		if err := RegisterEventType(typeName, func() EventPayload { return &testPayloadAlpha{} }); err != nil {
			t.Fatalf("RegisterEventType: %v", err)
		}

		got, ok := LookupTypeSchemaVersion(typeName)
		if !ok {
			t.Fatal("LookupTypeSchemaVersion: type not found after registration")
		}
		if got != 1 {
			t.Errorf("LookupTypeSchemaVersion: got %d, want 1 (default for RegisterEventType)", got)
		}
	})

	t.Run("ExplicitVersion_ViaRegisterEventTypeAtVersion", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.ev028.explicit"
		const wantVersion = 3
		if err := RegisterEventTypeAtVersion(typeName, func() EventPayload { return &testPayloadAlpha{} }, wantVersion); err != nil {
			t.Fatalf("RegisterEventTypeAtVersion: %v", err)
		}

		got, ok := LookupTypeSchemaVersion(typeName)
		if !ok {
			t.Fatal("LookupTypeSchemaVersion: type not found after versioned registration")
		}
		if got != wantVersion {
			t.Errorf("LookupTypeSchemaVersion: got %d, want %d", got, wantVersion)
		}
	})

	t.Run("UnknownType_ReturnsFalse", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		_, ok := LookupTypeSchemaVersion("no-such-type")
		if ok {
			t.Error("LookupTypeSchemaVersion: expected (0, false) for unregistered type, got ok=true")
		}
	})

	t.Run("InvalidVersion_ReturnsError", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		err := RegisterEventTypeAtVersion("test.ev028.invalid", func() EventPayload { return &testPayloadAlpha{} }, 0)
		if err == nil {
			t.Fatal("RegisterEventTypeAtVersion with version=0: expected error, got nil")
		}
	})
}

// TestEV028_ValidateEnvelopeSchemaVersion verifies the EV-028 envelope
// schema_version validation.
//
// Spec ref: event-model.md §4.7 EV-028 — "schema_version ... MUST match the
// schema version of the payload for that type."
func TestEV028_ValidateEnvelopeSchemaVersion(t *testing.T) {
	t.Run("MatchingVersion_ReturnsNil", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.ev028.match"
		if err := RegisterEventType(typeName, func() EventPayload { return &testPayloadAlpha{} }); err != nil {
			t.Fatalf("RegisterEventType: %v", err)
		}

		ev := minimalEvent(t, typeName, json.RawMessage(`{}`))
		if err := ValidateEnvelopeSchemaVersion(ev); err != nil {
			t.Errorf("ValidateEnvelopeSchemaVersion: expected nil for matching versions, got %v", err)
		}
	})

	t.Run("MismatchedVersion_ReturnsErrSchemaVersionMismatch", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.ev028.mismatch"
		if err := RegisterEventTypeAtVersion(typeName, func() EventPayload { return &testPayloadAlpha{} }, 2); err != nil {
			t.Fatalf("RegisterEventTypeAtVersion: %v", err)
		}

		ev := minimalEvent(t, typeName, json.RawMessage(`{}`)) // minimalEvent sets SchemaVersion=1
		err := ValidateEnvelopeSchemaVersion(ev)
		if err == nil {
			t.Fatal("ValidateEnvelopeSchemaVersion: expected ErrSchemaVersionMismatch, got nil")
		}
		if !errors.Is(err, ErrSchemaVersionMismatch) {
			t.Errorf("ValidateEnvelopeSchemaVersion: got %v, want errors.Is(ErrSchemaVersionMismatch)", err)
		}
	})

	t.Run("UnknownType_ReturnsErrUnknownEventType", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		ev := minimalEvent(t, "no-such-type", json.RawMessage(`{}`))
		err := ValidateEnvelopeSchemaVersion(ev)
		if err == nil {
			t.Fatal("ValidateEnvelopeSchemaVersion: expected ErrUnknownEventType, got nil")
		}
		if !errors.Is(err, ErrUnknownEventType) {
			t.Errorf("ValidateEnvelopeSchemaVersion: got %v, want errors.Is(ErrUnknownEventType)", err)
		}
	})

	t.Run("ExplicitVersionMatch_ReturnsNil", func(t *testing.T) {
		t.Cleanup(eventRegistryReset)

		const typeName = "test.ev028.explicit.match"
		const version = 5
		if err := RegisterEventTypeAtVersion(typeName, func() EventPayload { return &testPayloadAlpha{} }, version); err != nil {
			t.Fatalf("RegisterEventTypeAtVersion: %v", err)
		}

		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("uuid.NewV7: %v", err)
		}
		ev := Event{
			EventID:         EventID(id),
			SchemaVersion:   version,
			Type:            EventType(typeName),
			TimestampWall:   time.Now(),
			SourceSubsystem: "test",
			Payload:         json.RawMessage(`{}`),
		}
		if err := ValidateEnvelopeSchemaVersion(ev); err != nil {
			t.Errorf("ValidateEnvelopeSchemaVersion: expected nil for matching version %d, got %v", version, err)
		}
	})
}
