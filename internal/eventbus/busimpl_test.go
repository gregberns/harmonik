package eventbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

const busImplFixtureEventType core.EventType = "test.busimpl.v1"

func TestBusImplEmit_UnknownTypeReturnsTypedError(t *testing.T) {
	bus := eventbus.NewBusImpl()
	err := bus.Emit(context.Background(), core.EventType("test.unknown.event"), []byte(`{}`))
	if !errors.Is(err, core.ErrUnknownEventType) {
		t.Fatalf("Emit error = %v; want errors.Is(ErrUnknownEventType)", err)
	}
}

func busImplFixtureWildcardPattern() core.EventPattern {
	return core.EventPattern{Wildcard: true}
}

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

	registry := core.NewRedactionRegistry()
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

	if got, ok := receivedPayload["run_id"]; !ok {
		t.Error("consumer payload missing 'run_id' key")
	} else if got != "run-999" {
		t.Errorf("consumer payload[\"run_id\"] = %v, want %q; safe value MUST NOT be redacted", got, "run-999")
	}
}

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

	var syncDone atomic.Int32

	asyncGate := make(chan struct{})
	var asyncRan atomic.Int32

	syncSub := core.Subscription{
		ConsumerID:    "dispatch-order-sync",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
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

	if err := bus.Emit(context.Background(), busImplFixtureEventType, payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if syncDone.Load() != 1 {
		t.Error("EV-014a violated: Emit returned before synchronous consumer handler finished; " +
			"sync consumer MUST run on caller goroutine and complete before Emit returns")
	}

	if asyncRan.Load() != 0 {
		t.Error("EV-014a violated: async consumer handler completed before Emit returned; " +
			"async/observer dispatch MUST NOT extend Emit latency")
	}

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

const busImplFixtureFsyncEventType core.EventType = "daemon_started"

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
	defer eventbusFixtureClose(t, writer)

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
	defer eventbusFixtureClose(t, writer)

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

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

func TestBusImplEmitAgentPresence_RefreshPersistsForWhoProjection(t *testing.T) {
	t.Parallel()

	logPath := busImplFixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer eventbusFixtureClose(t, writer)

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	emitter, ok := bus.(eventbus.CommsPresenceEmitter)
	if !ok {
		t.Fatal("bus does not implement CommsPresenceEmitter")
	}

	_, err = emitter.EmitAgentPresence(context.Background(), core.AgentPresencePayload{
		Agent:     "captain",
		Status:    core.AgentPresenceStatusOnline,
		Reason:    core.AgentPresenceReasonRefresh,
		SessionID: "session-refresh-proof",
	})
	if err != nil {
		t.Fatalf("EmitAgentPresence(refresh): %v", err)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file contains %d lines after refresh, want 1; lines: %v", len(lines), lines)
	}
	var evt core.Event
	if err := json.Unmarshal([]byte(lines[0]), &evt); err != nil {
		t.Fatalf("decode persisted refresh: %v", err)
	}
	const presenceType core.EventType = "agent_presence"
	if evt.Type != presenceType {
		t.Fatalf("persisted event type = %q, want %q", evt.Type, presenceType)
	}
}

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
	defer eventbusFixtureClose(t, writer)

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

	if emitErr := bus.EmitWithRunID(context.Background(), runID, core.EventTypeRunStarted, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID: %v", emitErr)
	}

	lines := busImplFixtureReadJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL contains %d lines after EmitWithRunID, want 1", len(lines))
	}

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
	defer eventbusFixtureClose(t, writer)

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

	if emitErr := bus.Emit(context.Background(), core.EventTypeDaemonStarted, payload); emitErr != nil {
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
// delay shutdown of another run (POST_OPERATIONAL_PARALLELISM_ROADMAP.md §1, blocker A).
//
// Bead: hk-fx6zl.
func TestBusImplDrainRun_IsolatesRunFromSlowPeer(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()

	runAGate := make(chan struct{})
	var runBRan atomic.Int32

	runAID := busImplDrainFixtureNewRunID(t)
	runBID := busImplDrainFixtureNewRunID(t)

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
				runBRan.Store(1)
			case runAID:
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

	if emitErr := bus.EmitWithRunID(context.Background(), runAID, busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID (run A): %v", emitErr)
	}
	if emitErr := bus.EmitWithRunID(context.Background(), runBID, busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID (run B): %v", emitErr)
	}

	rd, ok := bus.(eventbus.RunDrainer)
	if !ok {
		t.Fatal("bus does not implement eventbus.RunDrainer; hk-fx6zl requires per-run drain support")
	}

	drainBCtx, cancelB := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelB()

	start := time.Now()
	if drainErr := rd.DrainRun(drainBCtx, runBID); drainErr != nil {
		t.Fatalf("DrainRun(runB): %v (elapsed %v); run A's hanging consumer blocked run B's drain, "+
			"which is the isolation hk-fx6zl requires", drainErr, time.Since(start))
	}

	if runBRan.Load() != 1 {
		t.Error("run B's async consumer never ran before DrainRun returned")
	}

	close(runAGate)
	globalDrainCtx, cancelGlobal := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancelGlobal()
	if globalErr := bus.Drain(globalDrainCtx); globalErr != nil {
		t.Fatalf("global Drain after releasing run A gate: %v", globalErr)
	}
}

func busImplDrainFixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("busImplDrainFixtureNewRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

type deadLetterSinkFixtureRecord struct {
	reason string
	evtID  string
}

type deadLetterSinkFixtureStub struct {
	mu      sync.Mutex
	records []deadLetterSinkFixtureRecord
}

func (s *deadLetterSinkFixtureStub) Record(_ context.Context, env core.EventEnvelope, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, deadLetterSinkFixtureRecord{
		reason: reason,
		evtID:  env.EventID.String(),
	})
	return nil
}

func (s *deadLetterSinkFixtureStub) Close() error { return nil }

func (s *deadLetterSinkFixtureStub) deadLetterSinkFixtureSnap() []deadLetterSinkFixtureRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]deadLetterSinkFixtureRecord, len(s.records))
	copy(out, s.records)
	return out
}

// TestBusImplWithSink_NilSinkDoesNotPanicOnAsyncError verifies that when
// NewBusImplWithSink is called with a nil sink, async consumer errors do NOT
// cause a panic in the bus goroutine.
//
// Bead ref: hk-xvpwb.
func TestBusImplWithSink_NilSinkDoesNotPanicOnAsyncError(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImplWithSink(nil, nil, nil)

	sub := core.Subscription{
		ConsumerID:    "xvpwb-nil-sink-async",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			return fmt.Errorf("deliberate async error")
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-nil-sink"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if emitErr := bus.Emit(context.Background(), busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v; want nil (nil sink MUST NOT propagate async errors)", emitErr)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

// TestBusImplWithSink_AsyncConsumerErrorRecordedToSink verifies that an async
// consumer that returns a non-nil error causes the dead-letter sink to receive
// a Record call with reason "consumer_error".
//
// Bead ref: hk-xvpwb.
func TestBusImplWithSink_AsyncConsumerErrorRecordedToSink(t *testing.T) {
	t.Parallel()

	sink := &deadLetterSinkFixtureStub{}
	bus := eventbus.NewBusImplWithSink(nil, nil, sink)

	sub := core.Subscription{
		ConsumerID:    "xvpwb-async-error",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			return fmt.Errorf("deliberate consumer error")
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-async-error"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if emitErr := bus.Emit(context.Background(), busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v; want nil", emitErr)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	records := sink.deadLetterSinkFixtureSnap()
	if len(records) != 1 {
		t.Fatalf("sink.Record called %d times, want 1", len(records))
	}
	if records[0].reason != "consumer_error" {
		t.Errorf("sink.Record reason = %q, want %q (hk-xvpwb)", records[0].reason, "consumer_error")
	}
}

// TestBusImplWithSink_ObserverPanicRecordedToSink verifies that an observer
// consumer that panics causes the dead-letter sink to receive a Record call
// with reason "observer_panic", and that the panic does NOT propagate out of
// the bus goroutine.
//
// Bead ref: hk-xvpwb.
func TestBusImplWithSink_ObserverPanicRecordedToSink(t *testing.T) {
	t.Parallel()

	sink := &deadLetterSinkFixtureStub{}
	bus := eventbus.NewBusImplWithSink(nil, nil, sink)

	sub := core.Subscription{
		ConsumerID:    "xvpwb-observer-panic",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			panic("deliberate observer panic")
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-panic"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if emitErr := bus.Emit(context.Background(), busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v; want nil", emitErr)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	records := sink.deadLetterSinkFixtureSnap()
	if len(records) != 1 {
		t.Fatalf("sink.Record called %d times, want 1", len(records))
	}
	if records[0].reason != "observer_panic" {
		t.Errorf("sink.Record reason = %q, want %q (hk-xvpwb)", records[0].reason, "observer_panic")
	}
}

// TestBusImplWithSink_NilSinkDoesNotPanicOnObserverPanic verifies that when
// NewBusImplWithSink is called with a nil sink, an observer panic does NOT
// propagate out of the bus goroutine.
//
// Bead ref: hk-xvpwb.
func TestBusImplWithSink_NilSinkDoesNotPanicOnObserverPanic(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImplWithSink(nil, nil, nil)

	sub := core.Subscription{
		ConsumerID:    "xvpwb-nil-sink-panic",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  busImplFixtureWildcardPattern(),
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			panic("deliberate observer panic with nil sink")
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"node_id": "n-nil-sink-panic"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if emitErr := bus.Emit(context.Background(), busImplFixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v; want nil", emitErr)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

// TestBusImplSubscribe_DuplicateSynchronousConsumerReturnsError is the
// cardinality sensor for hk-hqwn.49.
//
// Contract under test (EV-014 / EV-INV-003):
//
// At most ONE synchronous consumer per event type is permitted. If a second
// synchronous consumer registers for an event type already claimed by an
// existing synchronous consumer, Subscribe MUST return a typed
// [*eventbus.ErrDuplicateSynchronousConsumer] configuration error before Seal.
//
// Method: register a first synchronous consumer on a named event type; then
// register a second synchronous consumer whose pattern overlaps the first.
// Confirm Subscribe returns *ErrDuplicateSynchronousConsumer. The bus MUST NOT
// be sealed for the second registration (the error fires at registration time).
//
// Spec ref: specs/event-model.md §4.2 EV-014; §5.3 EV-INV-003.
// Bead ref: hk-hqwn.49.
func TestBusImplSubscribe_DuplicateSynchronousConsumerReturnsError(t *testing.T) {
	t.Parallel()

	const syncEventType core.EventType = "test.hqwn49.sync.v1"

	bus := eventbus.NewBusImpl()

	firstSub := core.Subscription{
		ConsumerID:    "hqwn49-sync-first",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{syncEventType: {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error { return nil },
	}

	if _, err := bus.Subscribe(firstSub); err != nil {
		t.Fatalf("Subscribe (first sync consumer): %v; want nil", err)
	}

	secondSub := core.Subscription{
		ConsumerID:    "hqwn49-sync-second",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{syncEventType: {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error { return nil },
	}

	_, err := bus.Subscribe(secondSub)
	if err == nil {
		t.Fatal("Subscribe (second sync consumer for same event type) returned nil error; " +
			"want *eventbus.ErrDuplicateSynchronousConsumer (EV-014 / EV-INV-003)")
	}

	var dupErr *eventbus.ErrDuplicateSynchronousConsumer
	if !errors.As(err, &dupErr) {
		t.Errorf("Subscribe returned %T (%v); want *eventbus.ErrDuplicateSynchronousConsumer "+
			"(EV-014 typed configuration error / EV-INV-003)", err, err)
		return
	}

	if dupErr.IncomingConsumerID != secondSub.ConsumerID {
		t.Errorf("ErrDuplicateSynchronousConsumer.IncomingConsumerID = %q, want %q",
			dupErr.IncomingConsumerID, secondSub.ConsumerID)
	}
	if dupErr.ConflictingConsumerID != firstSub.ConsumerID {
		t.Errorf("ErrDuplicateSynchronousConsumer.ConflictingConsumerID = %q, want %q",
			dupErr.ConflictingConsumerID, firstSub.ConsumerID)
	}
}

// TestBusImplSubscribe_ReentrantSynchronousConsumerReturnsAcyclicityError is
// the acyclicity sensor for hk-hqwn.49.
//
// Contract under test (EV-010 / EV-INV-003):
//
// A synchronous consumer MUST NOT emit events that would re-dispatch to itself
// (directly or transitively). The registration path MUST verify acyclicity
// across declared emission surfaces and fail-closed on cycles.
//
// Method: register a synchronous consumer A subscribed to event type X that
// declares it emits event type Y; then register a synchronous consumer B
// subscribed to Y that declares it emits X (completing the cycle A→B→A).
// The second Subscribe call MUST return [*eventbus.ErrSynchronousConsumerCycle].
//
// Spec ref: specs/event-model.md §4.2 EV-010; §5.3 EV-INV-003.
// Bead ref: hk-hqwn.49.
func TestBusImplSubscribe_ReentrantSynchronousConsumerReturnsAcyclicityError(t *testing.T) {
	t.Parallel()

	const (
		eventTypeX core.EventType = "test.hqwn49.acyclic.x.v1"
		eventTypeY core.EventType = "test.hqwn49.acyclic.y.v1"
	)

	bus := eventbus.NewBusImpl()

	consumerA := core.Subscription{
		ConsumerID:    "hqwn49-acyclic-A",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{eventTypeX: {}},
		},
		DeclaredEmitTypes: []core.EventType{eventTypeY},
		OnPanic:           core.OnPanicRecoverAndLog,
		Handler:           func(_ context.Context, _ core.Event) error { return nil },
	}

	if _, err := bus.Subscribe(consumerA); err != nil {
		t.Fatalf("Subscribe (consumer A, emits Y from X): %v; want nil", err)
	}

	consumerB := core.Subscription{
		ConsumerID:    "hqwn49-acyclic-B",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{eventTypeY: {}},
		},
		DeclaredEmitTypes: []core.EventType{eventTypeX},
		OnPanic:           core.OnPanicRecoverAndLog,
		Handler:           func(_ context.Context, _ core.Event) error { return nil },
	}

	_, err := bus.Subscribe(consumerB)
	if err == nil {
		t.Fatal("Subscribe (consumer B completing X→Y→X cycle) returned nil error; " +
			"want *eventbus.ErrSynchronousConsumerCycle (EV-010 fail-closed / EV-INV-003)")
	}

	var cycleErr *eventbus.ErrSynchronousConsumerCycle
	if !errors.As(err, &cycleErr) {
		t.Errorf("Subscribe returned %T (%v); want *eventbus.ErrSynchronousConsumerCycle "+
			"(EV-010 acyclicity typed error / EV-INV-003)", err, err)
		return
	}

	if cycleErr.IncomingConsumerID != consumerB.ConsumerID {
		t.Errorf("ErrSynchronousConsumerCycle.IncomingConsumerID = %q, want %q",
			cycleErr.IncomingConsumerID, consumerB.ConsumerID)
	}
	if len(cycleErr.CyclePath) == 0 {
		t.Error("ErrSynchronousConsumerCycle.CyclePath is empty; want non-empty path showing the cycle")
	}
}

func busImplFixtureJSONLPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "events.jsonl")
}

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
