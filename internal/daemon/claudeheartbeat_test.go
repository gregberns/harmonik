package daemon_test

// claudeheartbeat_test.go — unit tests for newDaemonHeartbeatEmitter (HC-057).
//
// Helper-prefix discipline (implementer-protocol.md §Helper-prefix discipline):
// per-bead camelCase prefix "heartbeatEmitter" for all test helpers in this file.
//
// Bead: hk-gql20.17.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// heartbeatEmitter test helpers
// ─────────────────────────────────────────────────────────────────────────────

// heartbeatEmitterRecordedEvent captures a single bus.EmitWithRunID call.
type heartbeatEmitterRecordedEvent struct {
	RunID     core.RunID
	EventType core.EventType
	Payload   json.RawMessage
}

// heartbeatEmitterBus is a minimal handlercontract.EventEmitter stub that
// records EmitWithRunID calls.
type heartbeatEmitterBus struct {
	mu     sync.Mutex
	events []heartbeatEmitterRecordedEvent
}

func (b *heartbeatEmitterBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	raw := make(json.RawMessage, len(payload))
	copy(raw, payload)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, heartbeatEmitterRecordedEvent{
		EventType: eventType,
		Payload:   raw,
	})
	return nil
}

func (b *heartbeatEmitterBus) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	raw := make(json.RawMessage, len(payload))
	copy(raw, payload)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, heartbeatEmitterRecordedEvent{
		RunID:     runID,
		EventType: eventType,
		Payload:   raw,
	})
	return nil
}

func (b *heartbeatEmitterBus) recorded() []heartbeatEmitterRecordedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]heartbeatEmitterRecordedEvent, len(b.events))
	copy(out, b.events)
	return out
}

// heartbeatEmitterNewRunID returns a fresh RunID for tests.
func heartbeatEmitterNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("heartbeatEmitter: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHeartbeatEmitterEmitsAgentHeartbeatEvent verifies that calling the
// emitter once puts an agent_heartbeat event on the bus with:
//   - event type == core.EventTypeAgentHeartbeat ("agent_heartbeat")
//   - envelope run_id matching the runID passed to newDaemonHeartbeatEmitter
//   - payload.session_id matching the sessionID argument
//   - payload.phase matching the phase argument
func TestHeartbeatEmitterEmitsAgentHeartbeatEvent(t *testing.T) {
	bus := &heartbeatEmitterBus{}
	runID := heartbeatEmitterNewRunID(t)
	const sessionID = "test-session-abc"
	const phase = "reasoning"

	emitter := daemon.ExportedNewDaemonHeartbeatEmitter(bus, runID)
	if err := emitter(context.Background(), sessionID, phase); err != nil {
		t.Fatalf("emitter returned unexpected error: %v", err)
	}

	events := bus.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 event on bus, got %d", len(events))
	}

	ev := events[0]

	// Verify event type.
	if ev.EventType != core.EventTypeAgentHeartbeat {
		t.Errorf("event type: got %q, want %q", ev.EventType, core.EventTypeAgentHeartbeat)
	}

	// Verify envelope run_id.
	if ev.RunID != runID {
		t.Errorf("run_id: got %v, want %v", ev.RunID, runID)
	}

	// Decode and verify payload fields.
	var pl struct {
		SessionID string `json:"session_id"`
		Phase     string `json:"phase"`
	}
	if err := json.Unmarshal(ev.Payload, &pl); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if pl.SessionID != sessionID {
		t.Errorf("payload.session_id: got %q, want %q", pl.SessionID, sessionID)
	}
	if pl.Phase != phase {
		t.Errorf("payload.phase: got %q, want %q", pl.Phase, phase)
	}
}

// TestHeartbeatEmitterRunIDBinding verifies that two emitters constructed with
// different runIDs emit events with their respective run_ids (no cross-
// contamination from closure binding).
func TestHeartbeatEmitterRunIDBinding(t *testing.T) {
	bus := &heartbeatEmitterBus{}
	runIDA := heartbeatEmitterNewRunID(t)
	runIDB := heartbeatEmitterNewRunID(t)

	emitterA := daemon.ExportedNewDaemonHeartbeatEmitter(bus, runIDA)
	emitterB := daemon.ExportedNewDaemonHeartbeatEmitter(bus, runIDB)

	if err := emitterA(context.Background(), "sess-a", "reasoning"); err != nil {
		t.Fatalf("emitterA: %v", err)
	}
	if err := emitterB(context.Background(), "sess-b", "reasoning"); err != nil {
		t.Fatalf("emitterB: %v", err)
	}

	events := bus.recorded()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].RunID != runIDA {
		t.Errorf("event[0].RunID: got %v, want %v (runIDA)", events[0].RunID, runIDA)
	}
	if events[1].RunID != runIDB {
		t.Errorf("event[1].RunID: got %v, want %v (runIDB)", events[1].RunID, runIDB)
	}
}
