package handlercontract_test

// cp024_budget_accrual_per_chunk_test.go — CP-024 conformance sensor.
//
// Invariant (specs/control-points.md §4.5.CP-024):
//
//	Every agent-output chunk MUST emit a budget_accrual event within the same
//	handler tick that produces the chunk (bounded by the handler's
//	chunk-emission cadence per [handler-contract.md §4.2]).
//
// This sensor verifies:
//
//  1. For each agent_output_chunk in the progress stream, exactly one
//     budget_accrual event is emitted to the bus.
//  2. Each budget_accrual event is emitted immediately after its corresponding
//     agent_output_chunk (adjacent in the bus receive order).
//  3. The budget_accrual payload carries the chunk_index from the chunk,
//     cost_basis = "output_bytes", and cost_units = float64(bytes_emitted).
//  4. No budget_accrual events are emitted for non-chunk message types.
//
// Helper prefix: cp024Fixture (implementer-protocol.md §Helper-prefix
// discipline; bead hk-a8bg.23).
//
// Spec refs:
//   - specs/control-points.md §4.5.CP-024 (per-chunk accrual MUST)
//   - specs/event-model.md §8.4.2 (budget_accrual payload)
//   - specs/handler-contract.md §4.2.HC-007 (progress-stream message types)
//
// Bead: hk-a8bg.23.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// cp024FixtureRunID is the stable RunID used in chunk progress messages.
const cp024FixtureRunID = "01960084-0000-7000-8000-000000000024"

// cp024FixtureSessionID is the stable handler-assigned session ID in chunks.
const cp024FixtureSessionID = "cp024-handler-session-01"

// cp024FixtureOrderedEmitter records events in emission order, capturing both
// type and payload bytes for budget_accrual inspection.
type cp024FixtureOrderedEmitter struct {
	events []cp024FixtureEvent
}

type cp024FixtureEvent struct {
	EventType string
	Payload   []byte
}

func (r *cp024FixtureOrderedEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	r.events = append(r.events, cp024FixtureEvent{EventType: string(eventType), Payload: payload})
	return nil
}

func (r *cp024FixtureOrderedEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return r.Emit(context.Background(), eventType, payload)
}

// cp024FixtureChunkLine encodes one agent_output_chunk NDJSON line with the
// given chunk_index and bytes_emitted values.
func cp024FixtureChunkLine(t *testing.T, chunkIndex, bytesEmitted int) string {
	t.Helper()
	m := map[string]interface{}{
		"type":          handlercontract.ProgressMsgTypeAgentOutputChunk,
		"run_id":        cp024FixtureRunID,
		"session_id":    cp024FixtureSessionID,
		"chunk_index":   chunkIndex,
		"bytes_emitted": bytesEmitted,
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("cp024FixtureChunkLine: marshal: %v", err)
	}
	return string(b) + "\n"
}

// cp024FixtureWaitDone waits for the watcher to finish with a short deadline.
func cp024FixtureWaitDone(t *testing.T, w *handlercontract.Watcher) {
	t.Helper()
	select {
	case <-w.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("cp024FixtureWaitDone: watcher did not finish within 3s")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CP-024 sensor tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCP024_BudgetAccrualEmittedPerChunk is the primary CP-024 acceptance sensor.
//
// It feeds N agent_output_chunk messages through SpawnWatcher and verifies that
// exactly N budget_accrual events appear on the bus, each immediately following
// its corresponding chunk event.
//
// Spec: control-points.md §4.5.CP-024.
func TestCP024_BudgetAccrualEmittedPerChunk(t *testing.T) {
	t.Parallel()

	const nChunks = 3
	var sb strings.Builder
	for i := 0; i < nChunks; i++ {
		sb.WriteString(cp024FixtureChunkLine(t, i, (i+1)*100))
	}

	bus := &cp024FixtureOrderedEmitter{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("cp024-sensor-01"),
		ProgressStream: strings.NewReader(sb.String()),
		Publisher:      bus,
		DeadLetter:     dl,
	})
	cp024FixtureWaitDone(t, w)

	if len(dl.Events()) != 0 {
		t.Errorf("CP-024: dead-letter has %d event(s), want 0: %v", len(dl.Events()), dl.Events())
	}

	// Expect 2 events per chunk: agent_output_chunk then budget_accrual.
	wantTotal := nChunks * 2
	if len(bus.events) != wantTotal {
		t.Fatalf("CP-024: event count = %d, want %d (2 per chunk); types = %v",
			len(bus.events), wantTotal, cp024FixtureEventTypes(bus.events))
	}

	for i := 0; i < nChunks; i++ {
		chunkIdx := i * 2
		accrualIdx := chunkIdx + 1

		if bus.events[chunkIdx].EventType != handlercontract.ProgressMsgTypeAgentOutputChunk {
			t.Errorf("CP-024: event[%d] type = %q, want %q",
				chunkIdx, bus.events[chunkIdx].EventType, handlercontract.ProgressMsgTypeAgentOutputChunk)
		}
		if bus.events[accrualIdx].EventType != string(core.EventTypeBudgetAccrual) {
			t.Errorf("CP-024: event[%d] type = %q, want %q",
				accrualIdx, bus.events[accrualIdx].EventType, core.EventTypeBudgetAccrual)
		}
	}
}

// TestCP024_BudgetAccrualPayloadFields verifies the budget_accrual payload
// carries correct chunk_index, cost_units, and cost_basis derived from the
// agent_output_chunk message.
//
// Spec: control-points.md §4.5.CP-024; event-model.md §8.4.2.
func TestCP024_BudgetAccrualPayloadFields(t *testing.T) {
	t.Parallel()

	const chunkIndex = 7
	const bytesEmitted = 512

	stream := cp024FixtureChunkLine(t, chunkIndex, bytesEmitted)

	bus := &cp024FixtureOrderedEmitter{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("cp024-payload-01"),
		ProgressStream: strings.NewReader(stream),
		Publisher:      bus,
		DeadLetter:     dl,
	})
	cp024FixtureWaitDone(t, w)

	if len(dl.Events()) != 0 {
		t.Errorf("CP-024 payload: dead-letter has events: %v", dl.Events())
	}

	// Two events: chunk then budget_accrual.
	if len(bus.events) < 2 {
		t.Fatalf("CP-024 payload: event count = %d, want >= 2", len(bus.events))
	}
	accrualEv := bus.events[1]
	if accrualEv.EventType != string(core.EventTypeBudgetAccrual) {
		t.Fatalf("CP-024 payload: event[1] type = %q, want budget_accrual", accrualEv.EventType)
	}

	var p core.BudgetAccrualPayload
	if err := json.Unmarshal(accrualEv.Payload, &p); err != nil {
		t.Fatalf("CP-024 payload: unmarshal budget_accrual: %v", err)
	}

	if p.ChunkIndex == nil {
		t.Error("CP-024 payload: ChunkIndex is nil, want non-nil")
	} else if *p.ChunkIndex != chunkIndex {
		t.Errorf("CP-024 payload: ChunkIndex = %d, want %d", *p.ChunkIndex, chunkIndex)
	}

	wantCostUnits := float64(bytesEmitted)
	if p.CostUnits != wantCostUnits {
		t.Errorf("CP-024 payload: CostUnits = %v, want %v", p.CostUnits, wantCostUnits)
	}

	if p.CostBasis != core.CostBasisOutputBytes {
		t.Errorf("CP-024 payload: CostBasis = %q, want %q", p.CostBasis, core.CostBasisOutputBytes)
	}

	if string(p.SessionID) != cp024FixtureSessionID {
		t.Errorf("CP-024 payload: SessionID = %q, want %q", p.SessionID, cp024FixtureSessionID)
	}
}

// TestCP024_NoBudgetAccrualForNonChunkTypes verifies that budget_accrual is NOT
// emitted for non-chunk progress-stream message types.
//
// Spec: control-points.md §4.5.CP-024 (per-chunk trigger only).
func TestCP024_NoBudgetAccrualForNonChunkTypes(t *testing.T) {
	t.Parallel()

	stream := `{"type":"agent_ready"}` + "\n" +
		`{"type":"agent_heartbeat"}` + "\n" +
		`{"type":"agent_started"}` + "\n"

	bus := &cp024FixtureOrderedEmitter{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("cp024-no-accrual-01"),
		ProgressStream: strings.NewReader(stream),
		Publisher:      bus,
		DeadLetter:     dl,
	})
	cp024FixtureWaitDone(t, w)

	for _, ev := range bus.events {
		if ev.EventType == string(core.EventTypeBudgetAccrual) {
			t.Errorf("CP-024: budget_accrual emitted for non-chunk stream; all events: %v",
				cp024FixtureEventTypes(bus.events))
			break
		}
	}
}

// TestCP024_BudgetAccrualInterleavedWithOtherTypes verifies that budget_accrual
// events are emitted correctly when agent_output_chunk messages are interleaved
// with other message types.
func TestCP024_BudgetAccrualInterleavedWithOtherTypes(t *testing.T) {
	t.Parallel()

	stream := `{"type":"agent_ready"}` + "\n" +
		cp024FixtureChunkLine(t, 0, 200) +
		`{"type":"agent_heartbeat"}` + "\n" +
		cp024FixtureChunkLine(t, 1, 300) +
		`{"type":"agent_started"}` + "\n"

	bus := &cp024FixtureOrderedEmitter{}
	dl := &watcherFixtureDeadLetter{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("cp024-interleaved-01"),
		ProgressStream: strings.NewReader(stream),
		Publisher:      bus,
		DeadLetter:     dl,
	})
	cp024FixtureWaitDone(t, w)

	// Expected order: agent_ready, agent_output_chunk, budget_accrual,
	// agent_heartbeat, agent_output_chunk, budget_accrual, agent_started.
	wantTypes := []string{
		"agent_ready",
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		string(core.EventTypeBudgetAccrual),
		"agent_heartbeat",
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		string(core.EventTypeBudgetAccrual),
		"agent_started",
	}

	gotTypes := cp024FixtureEventTypes(bus.events)
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("CP-024 interleaved: event count = %d, want %d; got: %v",
			len(gotTypes), len(wantTypes), gotTypes)
	}
	for i, want := range wantTypes {
		if gotTypes[i] != want {
			t.Errorf("CP-024 interleaved: event[%d] = %q, want %q", i, gotTypes[i], want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// cp024FixtureEventTypes returns just the event type strings from a slice of events.
func cp024FixtureEventTypes(events []cp024FixtureEvent) []string {
	types := make([]string, len(events))
	for i, ev := range events {
		types[i] = ev.EventType
	}
	return types
}
