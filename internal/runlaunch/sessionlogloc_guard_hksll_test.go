package runlaunch_test

// sessionlogloc_guard_hksll_test.go — regression test for the guard half of
// hk-sll-empty-logpath-7dxdw.
//
// The daemon wrote a session_log_location event whose payload its own validator
// rejects: log_path was empty, and core.SessionLogLocationPayload.Valid()
// returns false for that (event-model.md §8.3.7). Observed live on 2026-08-09
// against a daemon built from 6920f9cf3, on the pi seed bead as-p4i:
//
//	{"type":"session_log_location","payload":{"agent_type":"claude-code",
//	 "log_format":"jsonl","log_path":"","node_id":"bead/as-p4i", ...}}
//
// The rule existed, the code that states it existed, and the emission path never
// asked. EmitPreExecMessage now asks, and refuses.
//
// Helper prefix: hksll (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runlaunch"
)

// hksllRecordingEmitter records the event type of every emit.
type hksllRecordingEmitter struct {
	types []string
}

func (e *hksllRecordingEmitter) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	e.types = append(e.types, string(eventType))
	return nil
}

func (e *hksllRecordingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, _ []byte) error {
	e.types = append(e.types, string(eventType))
	return nil
}

// hksllRunID returns a fixed non-nil run id. The value only has to be non-nil:
// core.SessionLogLocationPayload.Valid() rejects the zero uuid.
func hksllRunID(t *testing.T) core.RunID {
	t.Helper()
	var id core.RunID
	if err := id.UnmarshalText([]byte("0198f0d0-0000-7000-8000-0000000000aa")); err != nil {
		t.Fatalf("parse run id: %v", err)
	}
	return id
}

// hksllWithType re-encodes pl with a top-level "type" field, matching the
// on-wire pre-exec message shape EmitPreExecMessage dispatches on.
func hksllWithType(t *testing.T, pl core.SessionLogLocationPayload) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("re-decode payload: %v", err)
	}
	obj["type"] = json.RawMessage(`"` + string(core.EventTypeSessionLogLocation) + `"`)
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("re-encode message: %v", err)
	}
	return out
}

// TestEmitPreExecMessage_RefusesInvalidSessionLogLocation_hksll pins both halves
// of the guard: an invalid payload is refused, and a well-formed one still goes
// out. A guard that swallowed everything would pass the first check alone.
func TestEmitPreExecMessage_RefusesInvalidSessionLogLocation_hksll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runID := hksllRunID(t)

	valid := core.SessionLogLocationPayload{
		RunID:     runID,
		SessionID: core.SessionID("0198f0d0-0000-7000-8000-00000000abcd"),
		NodeID:    core.NodeID("bead/hksll-guard"),
		AgentType: core.AgentTypeCodex,
		LogPath:   "/tmp/hksll/sessions/abcd",
		LogFormat: "jsonl",
	}

	t.Run("empty log_path is refused", func(t *testing.T) {
		bad := valid
		bad.LogPath = ""
		bus := &hksllRecordingEmitter{}
		runlaunch.EmitPreExecMessage(ctx, bus, runID, hksllWithType(t, bad))
		if len(bus.types) != 0 {
			t.Errorf("emitted %v; want nothing — a payload Valid() rejects must not reach the bus (hk-sll-empty-logpath-7dxdw)", bus.types)
		}
	})

	t.Run("well-formed payload still emits", func(t *testing.T) {
		bus := &hksllRecordingEmitter{}
		runlaunch.EmitPreExecMessage(ctx, bus, runID, hksllWithType(t, valid))
		if len(bus.types) != 1 || bus.types[0] != string(core.EventTypeSessionLogLocation) {
			t.Errorf("emitted %v; want exactly one session_log_location — the guard must not swallow valid events", bus.types)
		}
	})
}
