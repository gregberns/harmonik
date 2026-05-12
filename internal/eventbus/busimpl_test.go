package eventbus_test

// busimpl_test.go — sensors for the concrete EventBus implementation (EV-035).
//
// Spec refs: specs/event-model.md §4.4 EV-035; specs/handler-contract.md §4.7.HC-031.
// Bead ref: hk-8mup.62.
//
// Helper prefix: busImplFixture (per implementer-protocol.md §Helper-prefix
// discipline; distinct from jsonlWriter helpers).
//
// What this file provides:
//
//  1. TestBusImplEmit_RedactsSecretNamedFieldBeforeDispatch — Emit applies
//     HC-031 field-name redaction before delivering the event to any consumer
//     (EV-035). A field named "secret" in the input payload MUST arrive at the
//     consumer as "<redacted>".
//
//  2. TestBusImplEmit_SafeFieldsReachConsumerUnchanged — Emit does NOT redact
//     fields whose names do not match the HC-031 regex (no over-redaction).
//
//  3. TestBusImplEmit_NoConsumersReturnsNil — Emit with zero subscribers
//     returns nil (no dispatch, no error).
//
//  4. TestBusImplSubscribe_AfterSealReturnsError — Subscribe called after Seal
//     returns a non-nil error (EV-009).

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// busImplFixtureEventType is a synthetic event type string used in busimpl
// tests. The real EventType enum (hk-hqwn.59) is not yet landed; string
// satisfies the core.EventType alias.
const busImplFixtureEventType core.EventType = "test.busimpl.v1"

// busImplFixtureWildcardPattern returns an EventPattern that matches every
// event type. Used to wire consumer subscriptions in tests.
func busImplFixtureWildcardPattern() core.EventPattern {
	return core.EventPattern{Wildcard: true}
}

// ─────────────────────────────────────────────────────────────────────────────
// EV-035: redaction before dispatch
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImplEmit_RedactsSecretNamedFieldBeforeDispatch asserts that Emit
// applies HC-031 common-prefix redaction to the payload before invoking any
// consumer handler (EV-035).
//
// Input payload contains a field named "secret" with a non-empty value. The
// consumer MUST receive the payload with that field replaced by "<redacted>".
//
// Spec ref: specs/event-model.md §4.4 EV-035; specs/handler-contract.md §4.7.HC-031.
func TestBusImplEmit_RedactsSecretNamedFieldBeforeDispatch(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	var receivedPayload map[string]any
	sub := core.Subscription{
		ConsumerID:    "test-consumer-redact",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if err := json.Unmarshal(evt.Payload, &receivedPayload); err != nil {
				t.Errorf("consumer: json.Unmarshal: %v", err)
			}
			return nil
		},
	}

	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"secret":  "super-secret-value",
		"node_id": "node-abc-123",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("consumer was not called; receivedPayload is nil")
	}

	const sentinel = "<redacted>"
	if got, ok := receivedPayload["secret"]; !ok {
		t.Error("consumer payload missing 'secret' key")
	} else if got != sentinel {
		t.Errorf("consumer received payload[\"secret\"] = %q, want %q (EV-035 / HC-031)", got, sentinel)
	}

	// Safe field must pass through unchanged.
	if got, ok := receivedPayload["node_id"]; !ok {
		t.Error("consumer payload missing 'node_id' key")
	} else if got != "node-abc-123" {
		t.Errorf("consumer received payload[\"node_id\"] = %q, want %q", got, "node-abc-123")
	}
}

// TestBusImplEmit_SafeFieldsReachConsumerUnchanged asserts that Emit does NOT
// over-redact: a payload with only safe field names is delivered unmodified.
//
// Spec ref: specs/event-model.md §4.4 EV-035; specs/handler-contract.md §4.7.HC-031.
func TestBusImplEmit_SafeFieldsReachConsumerUnchanged(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	var receivedPayload map[string]any
	sub := core.Subscription{
		ConsumerID:    "test-consumer-safe",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if err := json.Unmarshal(evt.Payload, &receivedPayload); err != nil {
				t.Errorf("consumer: json.Unmarshal: %v", err)
			}
			return nil
		},
	}

	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	want := map[string]any{
		"node_id":    "node-abc-123",
		"run_id":     "run-xyz-456",
		"status":     "SUCCESS",
		"agent_type": "claude",
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("consumer was not called; receivedPayload is nil")
	}

	for k, wantVal := range want {
		k, wantVal := k, wantVal
		gotVal, ok := receivedPayload[k]
		if !ok {
			t.Errorf("consumer payload missing safe key %q", k)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("consumer payload[%q] = %v, want %v; safe field MUST NOT be redacted (HC-031)", k, gotVal, wantVal)
		}
	}
}

// TestBusImplEmit_NoConsumersReturnsNil asserts that Emit with no registered
// consumers returns nil without error.
//
// Spec ref: specs/event-model.md §6.1.
func TestBusImplEmit_NoConsumersReturnsNil(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n1"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Errorf("Emit with no consumers returned error: %v; want nil", err)
	}
}

// TestBusImplSubscribe_AfterSealReturnsError asserts that Subscribe returns a
// non-nil error when called after Seal (EV-009).
//
// Spec ref: specs/event-model.md §4.2 EV-009.
func TestBusImplSubscribe_AfterSealReturnsError(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	sub := core.Subscription{
		ConsumerID:    "late-consumer",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       func(_ context.Context, _ core.Event) error { return nil },
	}

	_, err := bus.Subscribe(sub)
	if err == nil {
		t.Error("Subscribe after Seal returned nil error; want non-nil error (EV-009)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-030 / HC-032: registry middleware in the producer path (hk-8i31.37)
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImplEmit_RegistryValuePatternRedactedBeforeDispatch is the runtime
// sensor for hk-8i31.37.
//
// It asserts that when a bus is constructed via NewBusImplWithRegistry, Emit
// invokes the registry's RedactionMiddleware on the payload before delivering
// the event to any consumer. Specifically, a string value matching a registered
// HC-032 pattern MUST arrive at the consumer as "<redacted>".
//
// This test distinguishes the HC-032 path (value-pattern redaction) from the
// HC-031 path (field-name redaction) that the existing busimpl tests exercise.
// It verifies that Emit does not short-circuit the registry middleware even
// when the payload field name is not a secret-prefix name.
//
// Spec refs: specs/handler-contract.md §4.7.HC-030, §4.7.HC-032;
// specs/event-model.md §4.4 EV-035.
// Bead ref: hk-8i31.37.
func TestBusImplEmit_RegistryValuePatternRedactedBeforeDispatch(t *testing.T) {
	t.Parallel()

	// Register a value pattern that matches the literal string "ghp_TOKENVALUE".
	// Using a fixed literal keeps the sensor deterministic; real tokens use
	// the HC-032 regex shapes declared by each handler subsystem.
	registry := handlercontract.NewRedactionRegistry()
	registry.RegisterPattern("busimpl_test_subsystem", []*regexp.Regexp{
		regexp.MustCompile(`^ghp_TOKENVALUE$`),
	})

	bus := eventbus.NewBusImplWithRegistry(registry)

	var receivedPayload map[string]any
	sub := core.Subscription{
		ConsumerID:    "test-consumer-hc032",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if err := json.Unmarshal(evt.Payload, &receivedPayload); err != nil {
				t.Errorf("consumer: json.Unmarshal: %v", err)
			}
			return nil
		},
	}

	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// "api_token" is not a secret-prefix name (HC-031 does not redact it).
	// Only the HC-032 value-pattern match should trigger redaction here.
	payload, err := json.Marshal(map[string]any{
		"api_token": "ghp_TOKENVALUE",
		"run_id":    "run-999",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("consumer was not called; receivedPayload is nil")
	}

	const sentinel = "<redacted>"

	// HC-032: the value matching the registered pattern MUST be redacted.
	if got, ok := receivedPayload["api_token"]; !ok {
		t.Error("consumer payload missing 'api_token' key")
	} else if got != sentinel {
		t.Errorf(
			"consumer received payload[\"api_token\"] = %q, want %q\n"+
				"  HC-032 value-pattern redaction MUST fire via registry middleware in Emit\n"+
				"  (NewBusImplWithRegistry path; bead hk-8i31.37; spec HC-030 / HC-032)",
			got, sentinel,
		)
	}

	// Safe field MUST pass through unchanged.
	if got, ok := receivedPayload["run_id"]; !ok {
		t.Error("consumer payload missing 'run_id' key")
	} else if got != "run-999" {
		t.Errorf("consumer payload[\"run_id\"] = %v, want %q; safe value MUST NOT be redacted", got, "run-999")
	}
}

// TestBusImplEmit_NilRegistryFallsBackToHC031Only asserts that
// NewBusImplWithRegistry(nil) is equivalent to NewBusImpl: HC-031 field-name
// redaction still fires, but no HC-032 value-pattern redaction is applied
// (because there are no patterns to match).
//
// This is a guard against regressions in the nil-registry guard in
// NewBusImplWithRegistry.
//
// Spec ref: specs/event-model.md §4.4 EV-035; specs/handler-contract.md §4.7.HC-031.
// Bead ref: hk-8i31.37.
func TestBusImplEmit_NilRegistryFallsBackToHC031Only(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImplWithRegistry(nil)

	var receivedPayload map[string]any
	sub := core.Subscription{
		ConsumerID:    "test-consumer-nil-registry",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if err := json.Unmarshal(evt.Payload, &receivedPayload); err != nil {
				t.Errorf("consumer: json.Unmarshal: %v", err)
			}
			return nil
		},
	}

	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"secret": "must-be-redacted-by-hc031",
		"run_id": "run-000",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("consumer was not called; receivedPayload is nil")
	}

	const sentinel = "<redacted>"

	// HC-031 MUST still fire even with nil registry.
	if got, ok := receivedPayload["secret"]; !ok {
		t.Error("consumer payload missing 'secret' key")
	} else if got != sentinel {
		t.Errorf(
			"consumer received payload[\"secret\"] = %q, want %q\n"+
				"  HC-031 field-name redaction MUST fire even when registry is nil\n"+
				"  (NewBusImplWithRegistry(nil) path; bead hk-8i31.37)",
			got, sentinel,
		)
	}

	if got, ok := receivedPayload["run_id"]; !ok {
		t.Error("consumer payload missing 'run_id' key")
	} else if got != "run-000" {
		t.Errorf("consumer payload[\"run_id\"] = %v, want %q; safe field MUST NOT be redacted", got, "run-000")
	}
}
