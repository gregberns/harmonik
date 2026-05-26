package workflow_test

// context_updates_hk_rhj3t_test.go — sensors for hk-rhj3t (T-IMPL-009):
// context-update cascade plumbing.
//
// Covers HC-062 warn-and-drop discipline and context_updated diff emission per
// the bead acceptance criteria:
//  1. Registered keys are applied to run.Context.
//  2. Unregistered keys are dropped with a context_update_unregistered_key event.
//  3. context_updated is emitted with the applied diff.
//  4. Empty updates map produces no events.
//  5. Updates with no registered keys emit only unregistered-key warnings (no context_updated).
//  6. value_type in the warning reflects the JSON type of the dropped value.
//
// Spec refs:
//   - specs/handler-contract.md §5.6 HC-062
//   - specs/execution-model.md §4.10 EM-041a
//
// Bead ref: hk-rhj3t.
// Tags: mechanism

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ── fixtures ────────────────────────────────────────────────────────────────

func contextUpdatesFixtureRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("0.1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// capturedEvent holds a single event received by the test consumer.
type capturedEvent struct {
	EventType core.EventType
	Payload   []byte
}

// captureBus sets up a synchronous consumer on an in-memory bus that appends
// all received events to the returned slice. The slice is safe to read after
// the test function returns; bus.Seal() is called before returning.
func captureBus(t *testing.T) (eventbus.EventBus, *[]capturedEvent, *sync.Mutex) {
	t.Helper()
	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []capturedEvent

	sub := core.Subscription{
		ConsumerID:    "test-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			defer mu.Unlock()
			captured = append(captured, capturedEvent{
				EventType: core.EventType(evt.Type),
				Payload:   evt.Payload,
			})
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("captureBus Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("captureBus Seal: %v", err)
	}
	return bus, &captured, &mu
}

// ── sensors ─────────────────────────────────────────────────────────────────

// TestValidateAndApplyContextUpdates_RegisteredKeyApplied verifies that a key
// present in registeredKeys is written into run.Context and a context_updated
// event is emitted containing that key in the diff.
func TestValidateAndApplyContextUpdates_RegisteredKeyApplied(t *testing.T) {
	t.Parallel()

	run := contextUpdatesFixtureRun(t)
	bus, captured, mu := captureBus(t)

	updates := map[string]any{"pr_url": "https://github.com/example/pr/1"}
	registeredKeys := []string{"pr_url"}
	nodeID := core.NodeID("node-impl")

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, nodeID, updates, registeredKeys, bus,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// run.Context must contain the registered key.
	if run.Context["pr_url"] != "https://github.com/example/pr/1" {
		t.Errorf("run.Context[pr_url] = %v, want https://github.com/example/pr/1", run.Context["pr_url"])
	}

	// Exactly one event: context_updated.
	mu.Lock()
	evts := *captured
	mu.Unlock()
	if len(evts) != 1 {
		t.Fatalf("emitted %d events, want 1; events: %+v", len(evts), evts)
	}
	if evts[0].EventType != workflow.EventTypeContextUpdated {
		t.Errorf("event type = %q, want %q", evts[0].EventType, workflow.EventTypeContextUpdated)
	}

	var payload workflow.ContextUpdatedPayload
	if err := json.Unmarshal(evts[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal context_updated payload: %v", err)
	}
	if payload.RunID != run.RunID {
		t.Errorf("payload.RunID mismatch")
	}
	if payload.NodeID != nodeID {
		t.Errorf("payload.NodeID = %q, want %q", payload.NodeID, nodeID)
	}
	if payload.WorkflowID != run.WorkflowID {
		t.Errorf("payload.WorkflowID mismatch")
	}
	if v, ok := payload.Diff["pr_url"]; !ok || v != "https://github.com/example/pr/1" {
		t.Errorf("payload.Diff[pr_url] = %v, want https://github.com/example/pr/1", v)
	}
}

// TestValidateAndApplyContextUpdates_UnregisteredKeyDropped verifies that a key
// absent from registeredKeys is NOT written into run.Context and that a
// context_update_unregistered_key event is emitted with the correct fields.
func TestValidateAndApplyContextUpdates_UnregisteredKeyDropped(t *testing.T) {
	t.Parallel()

	run := contextUpdatesFixtureRun(t)
	bus, captured, mu := captureBus(t)

	updates := map[string]any{"secret_token": "s3cret"}
	registeredKeys := []string{"pr_url"} // "secret_token" is NOT registered
	nodeID := core.NodeID("node-impl")

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, nodeID, updates, registeredKeys, bus,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// run.Context must NOT contain the unregistered key.
	if _, ok := run.Context["secret_token"]; ok {
		t.Error("unregistered key was written to run.Context; want it dropped")
	}

	// Exactly one event: context_update_unregistered_key.
	mu.Lock()
	evts := *captured
	mu.Unlock()
	if len(evts) != 1 {
		t.Fatalf("emitted %d events, want 1", len(evts))
	}
	if evts[0].EventType != workflow.EventTypeContextUpdateUnregisteredKey {
		t.Errorf("event type = %q, want %q", evts[0].EventType, workflow.EventTypeContextUpdateUnregisteredKey)
	}

	var payload workflow.ContextUpdateUnregisteredKeyPayload
	if err := json.Unmarshal(evts[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal unregistered-key payload: %v", err)
	}
	if payload.Key != "secret_token" {
		t.Errorf("payload.Key = %q, want secret_token", payload.Key)
	}
	if payload.ValueType != "string" {
		t.Errorf("payload.ValueType = %q, want string", payload.ValueType)
	}
	if payload.RunID != run.RunID {
		t.Errorf("payload.RunID mismatch")
	}
	if payload.NodeID != nodeID {
		t.Errorf("payload.NodeID = %q, want %q", payload.NodeID, nodeID)
	}
	if payload.WorkflowID != run.WorkflowID {
		t.Errorf("payload.WorkflowID mismatch")
	}
}

// TestValidateAndApplyContextUpdates_EmptyUpdatesNoEvents verifies that a nil
// or empty updates map produces no events and does not modify run.Context.
func TestValidateAndApplyContextUpdates_EmptyUpdatesNoEvents(t *testing.T) {
	t.Parallel()

	for _, updates := range []map[string]any{nil, {}} {
		run := contextUpdatesFixtureRun(t)
		bus, captured, mu := captureBus(t)

		if err := workflow.ValidateAndApplyContextUpdates(
			context.Background(), run, "node-a", updates, []string{"pr_url"}, bus,
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(run.Context) != 0 {
			t.Errorf("run.Context modified for empty updates: %v", run.Context)
		}

		mu.Lock()
		evts := *captured
		mu.Unlock()
		if len(evts) != 0 {
			t.Errorf("emitted %d events for empty updates, want 0", len(evts))
		}
	}
}

// TestValidateAndApplyContextUpdates_MixedKeys verifies that registered keys
// are applied and unregistered keys are dropped when both are present.
func TestValidateAndApplyContextUpdates_MixedKeys(t *testing.T) {
	t.Parallel()

	run := contextUpdatesFixtureRun(t)
	bus, captured, mu := captureBus(t)

	updates := map[string]any{
		"pr_url":       "https://github.com/example/pr/2",
		"unknown_key":  42.0,
		"another_bad":  true,
	}
	registeredKeys := []string{"pr_url"}
	nodeID := core.NodeID("node-reviewer")

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, nodeID, updates, registeredKeys, bus,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if run.Context["pr_url"] != "https://github.com/example/pr/2" {
		t.Errorf("registered key not applied: run.Context[pr_url] = %v", run.Context["pr_url"])
	}
	if _, ok := run.Context["unknown_key"]; ok {
		t.Error("unregistered key 'unknown_key' was written to run.Context")
	}
	if _, ok := run.Context["another_bad"]; ok {
		t.Error("unregistered key 'another_bad' was written to run.Context")
	}

	mu.Lock()
	evts := *captured
	mu.Unlock()

	// 2 unregistered-key warnings + 1 context_updated.
	if len(evts) != 3 {
		t.Fatalf("emitted %d events, want 3 (2 unregistered + 1 updated)", len(evts))
	}
	warnCount := 0
	updatedCount := 0
	for _, e := range evts {
		switch e.EventType {
		case workflow.EventTypeContextUpdateUnregisteredKey:
			warnCount++
		case workflow.EventTypeContextUpdated:
			updatedCount++
		default:
			t.Errorf("unexpected event type %q", e.EventType)
		}
	}
	if warnCount != 2 {
		t.Errorf("warn events = %d, want 2", warnCount)
	}
	if updatedCount != 1 {
		t.Errorf("context_updated events = %d, want 1", updatedCount)
	}
}

// TestValidateAndApplyContextUpdates_AllUnregistered verifies that when all
// keys are unregistered, no context_updated event is emitted.
func TestValidateAndApplyContextUpdates_AllUnregistered(t *testing.T) {
	t.Parallel()

	run := contextUpdatesFixtureRun(t)
	bus, captured, mu := captureBus(t)

	updates := map[string]any{"rogue_key": "value"}
	registeredKeys := []string{"pr_url"}

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, "node-x", updates, registeredKeys, bus,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	evts := *captured
	mu.Unlock()

	// Only the unregistered-key warning; no context_updated.
	if len(evts) != 1 {
		t.Fatalf("emitted %d events, want 1", len(evts))
	}
	if evts[0].EventType != workflow.EventTypeContextUpdateUnregisteredKey {
		t.Errorf("event type = %q, want context_update_unregistered_key", evts[0].EventType)
	}
}

// TestValidateAndApplyContextUpdates_ValueTypeCoverage verifies that
// jsonValueType returns the correct JSON type label for representative values.
// This is tested indirectly via the emitted event payload.
func TestValidateAndApplyContextUpdates_ValueTypeCoverage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		value     any
		wantType  string
	}{
		{"string", "hello", "string"},
		{"number_float", 3.14, "number"},
		{"number_int", 42, "number"},
		{"boolean_true", true, "boolean"},
		{"boolean_false", false, "boolean"},
		{"null", nil, "null"},
		{"object", map[string]any{"x": 1}, "object"},
		{"array", []any{1, 2}, "array"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := contextUpdatesFixtureRun(t)
			bus, captured, mu := captureBus(t)

			updates := map[string]any{"unregistered": tc.value}

			if err := workflow.ValidateAndApplyContextUpdates(
				context.Background(), run, "node-a", updates, []string{}, bus,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mu.Lock()
			evts := *captured
			mu.Unlock()

			if len(evts) != 1 {
				t.Fatalf("emitted %d events, want 1", len(evts))
			}
			var payload workflow.ContextUpdateUnregisteredKeyPayload
			if err := json.Unmarshal(evts[0].Payload, &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if payload.ValueType != tc.wantType {
				t.Errorf("ValueType = %q, want %q", payload.ValueType, tc.wantType)
			}
		})
	}
}
