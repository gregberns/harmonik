package workflow_test

// context_keys_explore_hk6zvki_test.go — exploratory test for context_keys
// registered-list warn-and-drop behavior (HC-062).
//
// Exercises the scenario-level flow: parse a DOT graph with declared
// context_keys, simulate a handler returning context_updates with both
// registered and unregistered keys, and assert that:
//  1. Registered keys are propagated to run.Context.
//  2. Unregistered keys are dropped (not written to run.Context).
//  3. A context_update_unregistered_key warning event is emitted for each
//     dropped key.
//  4. A context_updated event is emitted containing only the registered keys.
//
// This differs from the unit-level tests in context_updates_hk_rhj3t_test.go
// by exercising the DOT parser → ContextKeys → ValidateAndApplyContextUpdates
// integration path, verifying the registered-key list flows correctly from
// the graph definition through to the enforcement point.
//
// Spec refs:
//   - specs/handler-contract.md §5.6 HC-062 — context-update discipline
//   - specs/workflow-graph.md §10 WG-031a — context_keys graph attribute
//   - specs/execution-model.md §4.10 EM-041a — pre-cascade context mutation
//
// Bead ref: hk-6zvki.
// Tags: exploration, mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

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
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// ctxKeysExplDotSrc is a minimal DOT graph declaring context_keys=["allowed_key"].
// Two nodes (start → done), no conditions — the graph exists solely to parse
// and extract ContextKeys for the warn-and-drop test.
const ctxKeysExplDotSrc = `digraph ctx_keys_explore {
	schema_version="1";
	version="1.0";
	start_node="start";
	terminal_node_ids="done";
	context_keys="allowed_key";

	start [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
	done  [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];

	start -> done;
}`

// ctxKeysExplMultiDotSrc declares multiple context_keys to verify the parser
// handles comma-separated lists and the enforcement uses the full set.
const ctxKeysExplMultiDotSrc = `digraph ctx_keys_multi {
	schema_version="1";
	version="1.0";
	start_node="start";
	terminal_node_ids="done";
	context_keys="key_alpha,key_beta";

	start [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
	done  [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];

	start -> done;
}`

func ctxKeysExplRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// ctxKeysExplCaptureBus creates an in-memory EventBus with a synchronous
// wildcard consumer that captures all emitted events.
func ctxKeysExplCaptureBus(t *testing.T) (eventbus.EventBus, *[]capturedEvent, *sync.Mutex) {
	t.Helper()
	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []capturedEvent

	sub := core.Subscription{
		ConsumerID:    "ctx-keys-expl-capture",
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
		t.Fatalf("ctxKeysExplCaptureBus Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("ctxKeysExplCaptureBus Seal: %v", err)
	}
	return bus, &captured, &mu
}

// ── tests ───────────────────────────────────────────────────────────────────

// TestContextKeysExplore_ParsedKeysFlowToWarnAndDrop exercises the full
// DOT-parse → ContextKeys → ValidateAndApplyContextUpdates path.
//
// Scenario: A handler returns context_updates with one registered key
// ("allowed_key") and one unregistered key ("unregistered_key"). The test
// verifies that the graph's parsed ContextKeys drive the enforcement:
// allowed_key lands in run.Context, unregistered_key is dropped with a
// warning event.
func TestContextKeysExplore_ParsedKeysFlowToWarnAndDrop(t *testing.T) {
	t.Parallel()

	// Step 1: Parse the DOT graph and extract context_keys.
	graph, err := dot.Parse(ctxKeysExplDotSrc, "ctx_keys_explore.dot")
	if err != nil {
		t.Fatalf("dot.Parse: %v", err)
	}

	if len(graph.ContextKeys) != 1 || graph.ContextKeys[0] != "allowed_key" {
		t.Fatalf("ContextKeys = %v, want [allowed_key]", graph.ContextKeys)
	}

	// Step 2: Simulate a handler outcome with mixed keys.
	run := ctxKeysExplRun(t)
	bus, captured, mu := ctxKeysExplCaptureBus(t)
	nodeID := core.NodeID("start")

	updates := map[string]any{
		"allowed_key":      "val1",
		"unregistered_key": "val2",
	}

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, nodeID, updates, graph.ContextKeys, bus,
	); err != nil {
		t.Fatalf("ValidateAndApplyContextUpdates: %v", err)
	}

	// Step 3: Assert registered key propagated.
	if v, ok := run.Context["allowed_key"]; !ok || v != "val1" {
		t.Errorf("run.Context[allowed_key] = %v (ok=%v), want val1", v, ok)
	}

	// Step 4: Assert unregistered key dropped.
	if _, ok := run.Context["unregistered_key"]; ok {
		t.Error("unregistered_key was written to run.Context; want it dropped")
	}

	// Step 5: Assert events.
	mu.Lock()
	evts := *captured
	mu.Unlock()

	// Expect 2 events: 1 unregistered-key warning + 1 context_updated.
	if len(evts) != 2 {
		t.Fatalf("emitted %d events, want 2 (1 warning + 1 updated); events: %+v", len(evts), evts)
	}

	var warnCount, updatedCount int
	for _, e := range evts {
		switch e.EventType {
		case workflow.EventTypeContextUpdateUnregisteredKey:
			warnCount++
			var payload workflow.ContextUpdateUnregisteredKeyPayload
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatalf("unmarshal unregistered-key payload: %v", err)
			}
			if payload.Key != "unregistered_key" {
				t.Errorf("warning payload.Key = %q, want unregistered_key", payload.Key)
			}
			if payload.ValueType != "string" {
				t.Errorf("warning payload.ValueType = %q, want string", payload.ValueType)
			}
		case workflow.EventTypeContextUpdated:
			updatedCount++
			var payload workflow.ContextUpdatedPayload
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatalf("unmarshal context_updated payload: %v", err)
			}
			if v, ok := payload.Diff["allowed_key"]; !ok || v != "val1" {
				t.Errorf("context_updated diff[allowed_key] = %v (ok=%v), want val1", v, ok)
			}
			if _, ok := payload.Diff["unregistered_key"]; ok {
				t.Error("context_updated diff contains unregistered_key; want it absent")
			}
		default:
			t.Errorf("unexpected event type %q", e.EventType)
		}
	}
	if warnCount != 1 {
		t.Errorf("warn events = %d, want 1", warnCount)
	}
	if updatedCount != 1 {
		t.Errorf("context_updated events = %d, want 1", updatedCount)
	}
}

// TestContextKeysExplore_MultipleRegisteredKeys verifies that a graph with
// multiple context_keys (comma-separated) allows all declared keys through
// while still dropping undeclared ones.
func TestContextKeysExplore_MultipleRegisteredKeys(t *testing.T) {
	t.Parallel()

	graph, err := dot.Parse(ctxKeysExplMultiDotSrc, "ctx_keys_multi.dot")
	if err != nil {
		t.Fatalf("dot.Parse: %v", err)
	}

	if len(graph.ContextKeys) != 2 {
		t.Fatalf("ContextKeys = %v, want [key_alpha, key_beta]", graph.ContextKeys)
	}

	run := ctxKeysExplRun(t)
	bus, captured, mu := ctxKeysExplCaptureBus(t)

	updates := map[string]any{
		"key_alpha": "alpha_val",
		"key_beta":  "beta_val",
		"key_rogue": "should_be_dropped",
	}

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, "start", updates, graph.ContextKeys, bus,
	); err != nil {
		t.Fatalf("ValidateAndApplyContextUpdates: %v", err)
	}

	// Both registered keys propagated.
	if v := run.Context["key_alpha"]; v != "alpha_val" {
		t.Errorf("run.Context[key_alpha] = %v, want alpha_val", v)
	}
	if v := run.Context["key_beta"]; v != "beta_val" {
		t.Errorf("run.Context[key_beta] = %v, want beta_val", v)
	}

	// Rogue key dropped.
	if _, ok := run.Context["key_rogue"]; ok {
		t.Error("key_rogue was written to run.Context; want it dropped")
	}

	mu.Lock()
	evts := *captured
	mu.Unlock()

	// 1 unregistered-key warning + 1 context_updated.
	if len(evts) != 2 {
		t.Fatalf("emitted %d events, want 2", len(evts))
	}
}

// TestContextKeysExplore_EmptyContextKeys_AllDropped verifies that when a DOT
// graph declares no context_keys (empty list), ALL handler-emitted
// context_updates are dropped with warnings. This is the maximally
// restrictive posture.
func TestContextKeysExplore_EmptyContextKeys_AllDropped(t *testing.T) {
	t.Parallel()

	run := ctxKeysExplRun(t)
	bus, captured, mu := ctxKeysExplCaptureBus(t)

	// Empty registeredKeys — simulates a graph with no context_keys attribute.
	updates := map[string]any{
		"any_key": "any_value",
	}

	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, "start", updates, []string{}, bus,
	); err != nil {
		t.Fatalf("ValidateAndApplyContextUpdates: %v", err)
	}

	// Nothing propagated.
	if len(run.Context) != 0 {
		t.Errorf("run.Context = %v, want empty", run.Context)
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

// TestContextKeysExplore_ContextPreservedAcrossUpdates verifies that
// previously-set context values survive a second ValidateAndApplyContextUpdates
// call — the function mutates run.Context additively, never clears it.
func TestContextKeysExplore_ContextPreservedAcrossUpdates(t *testing.T) {
	t.Parallel()

	graph, err := dot.Parse(ctxKeysExplMultiDotSrc, "ctx_keys_multi.dot")
	if err != nil {
		t.Fatalf("dot.Parse: %v", err)
	}

	run := ctxKeysExplRun(t)
	bus, _, _ := ctxKeysExplCaptureBus(t)

	// First update: set key_alpha.
	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, "start", map[string]any{"key_alpha": "first"}, graph.ContextKeys, bus,
	); err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Second update: set key_beta only.
	if err := workflow.ValidateAndApplyContextUpdates(
		context.Background(), run, "done", map[string]any{"key_beta": "second"}, graph.ContextKeys, bus,
	); err != nil {
		t.Fatalf("second update: %v", err)
	}

	// Both keys present.
	if v := run.Context["key_alpha"]; v != "first" {
		t.Errorf("key_alpha = %v after second update, want first (preserved)", v)
	}
	if v := run.Context["key_beta"]; v != "second" {
		t.Errorf("key_beta = %v, want second", v)
	}
}
