package workflow

// context_updates.go — context-update discipline for workflow_mode=dot.
//
// Implements HC-062 (context-update registered-key validation) and C3 §5.6:
// - Validates each key in Outcome.context_updates against the workflow's
//   context_keys declaration.
// - Drops unregistered keys with a warning event per the D8 warn-and-drop rule.
// - Applies only registered keys to run.Context (pre-cascade mutation per EM-041a).
// - Emits context_updated with the applied diff for observability.
//
// Spec refs:
//   - specs/handler-contract.md §5.6 HC-062 — warn-and-drop discipline
//   - specs/execution-model.md §4.10 EM-041a — pre-cascade context mutation
//   - specs/workflow-graph.md §10 WG-031a — context_keys DOT attribute
//
// Bead ref: hk-rhj3t (T-IMPL-009).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// EventTypeContextUpdateUnregisteredKey is the event type emitted when a
// handler emits a context_updates key that is not in the workflow's registered
// context_keys list (HC-062 warn-and-drop).
const EventTypeContextUpdateUnregisteredKey = core.EventType("context_update_unregistered_key")

// EventTypeContextUpdated is the event type emitted after registered
// context_updates have been applied to run.Context (EM-041a).
const EventTypeContextUpdated = core.EventType("context_updated")

// ContextUpdateUnregisteredKeyPayload is the event payload for
// context_update_unregistered_key (HC-062).
//
// The value itself is NOT included — it may contain PII.
type ContextUpdateUnregisteredKeyPayload struct {
	RunID      core.RunID      `json:"run_id"`
	NodeID     core.NodeID     `json:"node_id"`
	WorkflowID core.WorkflowID `json:"workflow_id"`
	Key        string          `json:"key"`
	ValueType  string          `json:"value_type"`
}

// ContextUpdatedPayload is the event payload for context_updated.
//
// Diff contains only the keys whose values changed (new value after update).
// Keys in run.Context that were not touched by this update are absent.
type ContextUpdatedPayload struct {
	RunID      core.RunID      `json:"run_id"`
	NodeID     core.NodeID     `json:"node_id"`
	WorkflowID core.WorkflowID `json:"workflow_id"`
	Diff       map[string]any  `json:"diff"`
}

// ValidateAndApplyContextUpdates implements the HC-062 context-update discipline.
//
// For each key in updates:
//  1. If not in registeredKeys: emit context_update_unregistered_key and skip.
//  2. If registered: write into run.Context, record in diff.
//
// After processing all keys, emits context_updated with the diff (skipped when
// the diff is empty — no registered keys were present in updates).
//
// run.Context must be non-nil (enforced by [core.Run.Valid]).
// A nil or empty updates map is a no-op (no events emitted).
func ValidateAndApplyContextUpdates(
	ctx context.Context,
	run *core.Run,
	nodeID core.NodeID,
	updates map[string]any,
	registeredKeys []string,
	bus eventbus.EventBus,
) error {
	if len(updates) == 0 {
		return nil
	}

	// Build O(1) lookup set from the registered key list.
	registered := make(map[string]struct{}, len(registeredKeys))
	for _, k := range registeredKeys {
		registered[k] = struct{}{}
	}

	diff := make(map[string]any, len(updates))

	for k, v := range updates {
		if _, ok := registered[k]; !ok {
			// Warn-and-drop: emit per HC-062, do not write to run.Context.
			payload := ContextUpdateUnregisteredKeyPayload{
				RunID:      run.RunID,
				NodeID:     nodeID,
				WorkflowID: run.WorkflowID,
				Key:        k,
				ValueType:  jsonValueType(v),
			}
			if err := emitJSON(ctx, bus, run.RunID, EventTypeContextUpdateUnregisteredKey, payload); err != nil {
				return fmt.Errorf("context_updates: emit unregistered-key event for %q: %w", k, err)
			}
			continue
		}
		run.Context[k] = v
		diff[k] = v
	}

	if len(diff) == 0 {
		return nil
	}

	updated := ContextUpdatedPayload{
		RunID:      run.RunID,
		NodeID:     nodeID,
		WorkflowID: run.WorkflowID,
		Diff:       diff,
	}
	if err := emitJSON(ctx, bus, run.RunID, EventTypeContextUpdated, updated); err != nil {
		return fmt.Errorf("context_updates: emit context_updated event: %w", err)
	}
	return nil
}

// jsonValueType returns the JSON type name of v: "string", "number", "boolean",
// "object", "array", or "null". Used in the unregistered-key warning payload
// so the value itself is never logged.
func jsonValueType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		// Fallback: marshal to JSON and inspect the first byte.
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 {
			return "unknown"
		}
		switch b[0] {
		case '"':
			return "string"
		case '[':
			return "array"
		case '{':
			return "object"
		case 't', 'f':
			return "boolean"
		case 'n':
			return "null"
		default:
			return "number"
		}
	}
}

// emitJSON marshals payload to JSON and calls bus.EmitWithRunID.
func emitJSON(ctx context.Context, bus eventbus.EventBus, runID core.RunID, eventType core.EventType, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return bus.EmitWithRunID(ctx, runID, eventType, b)
}
