package runlaunch_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runlaunch"
)

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

func hksllRunID(t *testing.T) core.RunID {
	t.Helper()
	var id core.RunID
	if err := id.UnmarshalText([]byte("0198f0d0-0000-7000-8000-0000000000aa")); err != nil {
		t.Fatalf("parse run id: %v", err)
	}
	return id
}

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
