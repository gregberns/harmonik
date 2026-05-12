package eventbus_test

// busimpl_test.go — sensors for the concrete EventBus implementation (EV-035, EV-014a).
//
// Spec refs: specs/event-model.md §4.4 EV-035, §4.2 EV-014a;
// specs/handler-contract.md §4.7.HC-031.
// Bead refs: hk-8mup.62, hk-hqwn.19.
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
//
//  5. TestBusImplEmit_DispatchOrder_SyncBlocksAsyncDoesNot — Emit blocks until
//     the synchronous consumer returns AND does NOT block on async/observer
//     consumers (EV-014a dispatch-order contract: redact → JSONL-stub →
//     sync-dispatch → Emit-returns, async/observer off critical path).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

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

// ─────────────────────────────────────────────────────────────────────────────
// EV-014a: dispatch-order contract (hk-hqwn.19)
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImplEmit_DispatchOrder_SyncBlocksAsyncDoesNot is the EV-014a
// dispatch-order sensor for hk-hqwn.19.
//
// Contract under test (EV-014a):
//
//  1. Emit MUST block until the synchronous consumer's handler returns.
//  2. Emit MUST NOT block on asynchronous or observer consumer handlers.
//  3. Async/observer handlers MUST eventually execute (observable via Drain).
//
// Method: the synchronous handler sleeps briefly and records when it ran; Emit
// return is timestamped. If Emit returns BEFORE the sync handler records, the
// contract is violated. The async handler is gated by a channel released AFTER
// Emit returns; Drain then waits for it. If Drain times out, the async goroutine
// was never launched.
//
// Spec ref: specs/event-model.md §4.2 EV-014a.
// Bead ref: hk-hqwn.19.
func TestBusImplEmit_DispatchOrder_SyncBlocksAsyncDoesNot(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	// syncDone is set to 1 by the synchronous handler just before it returns.
	var syncDone atomic.Int32

	// asyncGate is closed by the test after Emit returns, allowing the async
	// handler to proceed and confirm it runs off the critical path.
	asyncGate := make(chan struct{})
	// asyncRan is set to 1 by the async handler once it has been unblocked.
	var asyncRan atomic.Int32

	syncSub := core.Subscription{
		ConsumerID:    "dispatch-order-sync",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			// A small sleep makes the ordering difference visible: if Emit
			// returns before this function records syncDone, the invariant is
			// violated.
			time.Sleep(5 * time.Millisecond)
			syncDone.Store(1)
			return nil
		},
	}

	asyncSub := core.Subscription{
		ConsumerID:    "dispatch-order-async",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			// Block until the test confirms Emit has already returned,
			// verifying the async handler runs off the critical path.
			<-asyncGate
			asyncRan.Store(1)
			return nil
		},
	}

	observerSub := core.Subscription{
		ConsumerID:    "dispatch-order-observer",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			// Observer shares the asyncGate to confirm observer dispatch is
			// also off the critical path.
			<-asyncGate
			return nil
		},
	}

	for _, sub := range []core.Subscription{syncSub, asyncSub, observerSub} {
		if _, err := bus.Subscribe(sub); err != nil {
			t.Fatalf("Subscribe %q: %v", sub.ConsumerID, err)
		}
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-dispatch-order"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// --- Contract assertion 1: Emit blocks on sync consumer ----------------
	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Emit has returned. The synchronous handler MUST have completed already.
	if syncDone.Load() != 1 {
		t.Error("EV-014a violated: Emit returned before synchronous consumer handler finished; " +
			"sync consumer MUST run on caller goroutine and complete before Emit returns")
	}

	// --- Contract assertion 2: async/observer did NOT block Emit -----------
	// asyncRan is still 0 here because asyncGate is not yet closed.
	if asyncRan.Load() != 0 {
		t.Error("EV-014a violated: async consumer handler completed before Emit returned; " +
			"async/observer dispatch MUST NOT extend Emit latency")
	}

	// --- Contract assertion 3: async/observer handlers eventually execute --
	// Unblock the async and observer handlers, then wait via Drain.
	close(asyncGate)

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v (async/observer goroutines did not complete in time)", err)
	}

	if asyncRan.Load() != 1 {
		t.Error("EV-014a violated: async consumer handler never ran after Drain; " +
			"async dispatch MUST eventually deliver the event off the critical path")
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

// ─────────────────────────────────────────────────────────────────────────────
// EV-016 / EV-016a: JSONL append + fsync-boundary wiring (hk-8mup.63)
// ─────────────────────────────────────────────────────────────────────────────

// busImplFixtureFsyncEventType is an F-class (fsync-boundary) event type
// (daemon_started, §8.7.1) used to exercise the sync=true path in Emit.
const busImplFixtureFsyncEventType core.EventType = "daemon_started"

// busImplFixtureOrdinaryEventType is an O-class (ordinary) event type
// (daemon_orphan_sweep_completed, §8.7.14) used to exercise the sync=false path.
const busImplFixtureOrdinaryEventType core.EventType = "daemon_orphan_sweep_completed"

// TestBusImplEmit_FsyncBoundaryEventWritesToJSONL asserts that when the bus
// is constructed via NewBusImplWithWriter, an F-class (fsync-boundary) event
// is appended to the JSONL tempfile and Emit returns without error.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a.
// Bead ref: hk-8mup.63.
func TestBusImplEmit_FsyncBoundaryEventWritesToJSONL(t *testing.T) {
	t.Parallel()

	logPath := busImplFixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"started_at":         "2026-05-12T00:00:00Z",
		"pid":                12345,
		"binary_commit_hash": "abc123",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if emitErr := bus.Emit(context.Background(), busImplFixtureFsyncEventType, payload); emitErr != nil {
		t.Fatalf("Emit (F-class): %v; want nil", emitErr)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file contains %d lines after one F-class Emit, want 1", len(lines))
	}
	if lines[0] == "" {
		t.Error("JSONL line is empty; want a non-empty JSON object")
	}
}

// TestBusImplEmit_OrdinaryEventWritesToJSONLWithoutSync asserts that when the
// bus is constructed via NewBusImplWithWriter, an O-class (ordinary) event is
// appended to the JSONL tempfile without error, and a second F-class event
// correctly appears as the second line.
//
// This confirms the sync=false (ordinary) path does not block or error,
// and that lines accumulate correctly.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a.
// Bead ref: hk-8mup.63.
func TestBusImplEmit_OrdinaryEventWritesToJSONLWithoutSync(t *testing.T) {
	t.Parallel()

	logPath := busImplFixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Emit one O-class event.
	ordinaryPayload, err := json.Marshal(map[string]any{
		"tmux_sessions_killed": 0,
		"swept_at":             "2026-05-12T00:00:01Z",
	})
	if err != nil {
		t.Fatalf("json.Marshal (ordinary): %v", err)
	}
	if emitErr := bus.Emit(context.Background(), busImplFixtureOrdinaryEventType, ordinaryPayload); emitErr != nil {
		t.Fatalf("Emit (O-class): %v; want nil", emitErr)
	}

	// Emit one F-class event after the ordinary one.
	fsyncPayload, err := json.Marshal(map[string]any{
		"started_at":         "2026-05-12T00:00:02Z",
		"pid":                99,
		"binary_commit_hash": "def456",
	})
	if err != nil {
		t.Fatalf("json.Marshal (fsync): %v", err)
	}
	if emitErr := bus.Emit(context.Background(), busImplFixtureFsyncEventType, fsyncPayload); emitErr != nil {
		t.Fatalf("Emit (F-class): %v; want nil", emitErr)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("JSONL file contains %d lines after O-class + F-class Emit, want 2; lines: %v", len(lines), lines)
	}
	for i, line := range lines {
		if line == "" {
			t.Errorf("JSONL line[%d] is empty; want a non-empty JSON object", i)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PARA-1 / hk-n9f51: EmitWithRunID stamps run_id on the envelope
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImplEmitWithRunID_RunIDAppearsInJSONL asserts that EmitWithRunID
// appends a JSONL line whose "run_id" field matches the supplied RunID.
//
// Spec ref: specs/event-model.md §6.1 EV-001; specs/execution-model.md §4.3 EM-013.
// Bead: hk-n9f51.
func TestBusImplEmitWithRunID_RunIDAppearsInJSONL(t *testing.T) {
	t.Parallel()

	logPath := busImplFixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	runUUID, uuidErr := uuid.NewV7()
	if uuidErr != nil {
		t.Fatalf("uuid.NewV7: %v", uuidErr)
	}
	runID := core.RunID(runUUID)

	payload, marshalErr := json.Marshal(map[string]any{
		"bead_id":        "hk-test001",
		"workspace_path": "/tmp/wt",
		"started_at":     "2026-05-12T00:00:00Z",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal payload: %v", marshalErr)
	}

	if emitErr := bus.EmitWithRunID(context.Background(), runID, core.EventType("run_started"), payload); emitErr != nil {
		t.Fatalf("EmitWithRunID: %v", emitErr)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL contains %d lines after EmitWithRunID, want 1", len(lines))
	}

	// Parse the envelope and assert run_id is present and matches.
	var envelope map[string]any
	if parseErr := json.Unmarshal([]byte(lines[0]), &envelope); parseErr != nil {
		t.Fatalf("parse JSONL envelope: %v", parseErr)
	}

	gotRunID, ok := envelope["run_id"]
	if !ok {
		t.Fatal("JSONL envelope missing 'run_id' field; EmitWithRunID MUST stamp run_id (EV-001 / EM-013 / hk-n9f51)")
	}
	if gotRunID != runID.String() {
		t.Errorf("envelope run_id = %q, want %q", gotRunID, runID.String())
	}
}

// TestBusImplEmit_PlainEmit_RunIDAbsentFromJSONL asserts that plain Emit does
// NOT set run_id on the envelope (omitempty suppresses the field when zero).
//
// Spec ref: specs/event-model.md §6.1 EV-001.
// Bead: hk-n9f51.
func TestBusImplEmit_PlainEmit_RunIDAbsentFromJSONL(t *testing.T) {
	t.Parallel()

	logPath := busImplFixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload, marshalErr := json.Marshal(map[string]any{
		"started_at":         "2026-05-12T00:00:00Z",
		"pid":                1,
		"binary_commit_hash": "abc",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal payload: %v", marshalErr)
	}

	// daemon_started is F-class (daemon-level, no run in flight).
	if emitErr := bus.Emit(context.Background(), core.EventType("daemon_started"), payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL contains %d lines after Emit, want 1", len(lines))
	}

	var envelope map[string]any
	if parseErr := json.Unmarshal([]byte(lines[0]), &envelope); parseErr != nil {
		t.Fatalf("parse JSONL envelope: %v", parseErr)
	}

	if runID, exists := envelope["run_id"]; exists {
		t.Errorf("plain Emit MUST NOT set run_id; got %q (omitempty should suppress)", runID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-fx6zl: per-run Drain coordination — DrainRun isolates run B from run A
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImplDrainRun_IsolatesRunFromSlowPeer is the acceptance sensor for
// hk-fx6zl.
//
// Contract under test:
//
//	Two concurrent runs (A and B) each dispatch an asynchronous consumer.
//	Run A's consumer blocks for 5 seconds (simulating a slow handler).
//	DrainRun(ctx, runB) MUST return in <100ms even while run A's consumer is
//	still in flight.
//
// This verifies per-run fair termination: slow consumers from one run cannot
// delay shutdown of another run (POST_MVH_PARALLELISM_ROADMAP.md §1, blocker A).
//
// Bead: hk-fx6zl.
func TestBusImplDrainRun_IsolatesRunFromSlowPeer(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	// Gate used to let run A's consumer block indefinitely until the test is done.
	runAGate := make(chan struct{})
	// runBRan records whether run B's consumer executed.
	var runBRan atomic.Int32

	// runAID and runBID are set before EmitWithRunID; the handler closure
	// captures them by pointer so the handler can distinguish the two runs.
	runAID := busImplDrainFixtureNewRunID(t)
	runBID := busImplDrainFixtureNewRunID(t)

	// Register a single wildcard async consumer. The handler distinguishes
	// runs by comparing evt.RunID against the captured run IDs.
	sub := core.Subscription{
		ConsumerID:    "drain-run-isolation-async",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.RunID == nil {
				return nil
			}
			switch *evt.RunID {
			case runBID:
				// Run B: complete quickly.
				runBRan.Store(1)
			case runAID:
				// Run A: block until the test releases the gate.
				<-runAGate
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

	payload, err := json.Marshal(map[string]any{"node_id": "n1"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Emit run A's event — async consumer blocks on runAGate.
	if emitErr := bus.EmitWithRunID(context.Background(), runAID, busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID (run A): %v", emitErr)
	}
	// Emit run B's event — async consumer runs quickly.
	if emitErr := bus.EmitWithRunID(context.Background(), runBID, busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID (run B): %v", emitErr)
	}

	// Type-assert to RunDrainer to access per-run quiescence.
	rd, ok := bus.(eventbus.RunDrainer)
	if !ok {
		t.Fatal("bus does not implement eventbus.RunDrainer; hk-fx6zl requires per-run drain support")
	}

	// DrainRun for run B must return in <100ms even though run A is still hanging.
	drainBCtx, cancelB := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelB()

	start := time.Now()
	if drainErr := rd.DrainRun(drainBCtx, runBID); drainErr != nil {
		t.Fatalf("DrainRun(runB): %v (elapsed %v); run B's consumer should have completed quickly, "+
			"independent of the blocked run A consumer (hk-fx6zl)", drainErr, time.Since(start))
	}
	elapsed := time.Since(start)
	if elapsed >= 100*time.Millisecond {
		t.Errorf("DrainRun(runB) took %v, want <100ms; "+
			"run A's hanging consumer MUST NOT delay run B's drain (hk-fx6zl)", elapsed)
	}

	if runBRan.Load() != 1 {
		t.Error("run B's async consumer never ran before DrainRun returned")
	}

	// Clean up: release run A's consumer so the global Drain can complete.
	close(runAGate)
	globalDrainCtx, cancelGlobal := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancelGlobal()
	if globalErr := bus.Drain(globalDrainCtx); globalErr != nil {
		t.Fatalf("global Drain after releasing run A gate: %v", globalErr)
	}
}

// busImplDrainFixtureNewRunID generates a fresh UUIDv7-based RunID for use in
// per-run drain tests. Distinct from the busImplFixture prefix intentionally:
// this prefix is reserved for hk-fx6zl helpers.
func busImplDrainFixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("busImplDrainFixtureNewRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// busImplFixtureJSONLPath returns a temporary file path for use as a JSONL log.
func busImplFixtureJSONLPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "events.jsonl")
}

// busImplFixtureReadJSONLLines reads all non-empty lines from the JSONL file
// at path and returns them without trailing newlines.
func busImplFixtureReadJSONLLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("busImplFixtureReadJSONLLines: ReadFile %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
