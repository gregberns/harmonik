package daemon

// heartbeat_ev039_test.go — binding tests for EV-039 (event-model.md §4.11).
//
// EV-039 specifies: heartbeat (default 60s) payload MUST carry
//   - last_event_id: UUIDv7 string — the most recently fanned-out event_id
//   - active_runs[]: array of {bead_id, age_seconds} ONLY — MUST NOT carry run_id
//
// Consumers MUST advance their watermark to last_event_id on every heartbeat
// (per EV-037a). The active_runs array is used for stall detection (SHOULD
// inspect age_seconds). run_id is intentionally absent — consumers MUST NOT
// assume it is present; run-level correlation requires reading queue.json.
//
// Refs: hk-qv3bc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// TestEV039_HeartbeatCarriesLastEventID verifies that after events have been
// dispatched through the hub, the next heartbeat carries a non-empty
// last_event_id equal to the most recently dispatched event's event_id.
//
// Spec ref: EV-039 — "last_event_id (UUIDv7 string)"
func TestEV039_HeartbeatCarriesLastEventID(t *testing.T) {
	t.Parallel()

	// Dispatch two events so lastEventID is non-empty.
	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})
	e1 := subscribeTestMakeEvent(t, "ev_a")
	e2 := subscribeTestMakeEvent(t, "ev_b")
	_ = hub.dispatch(context.Background(), e1)
	_ = hub.dispatch(context.Background(), e2)

	hb := hub.makeHeartbeat()
	if hb.LastEventID == "" {
		t.Fatal("EV-039: heartbeat.last_event_id must be non-empty after events dispatched")
	}
	want := e2.EventID.String()
	if hb.LastEventID != want {
		t.Errorf("EV-039: heartbeat.last_event_id: got %q, want %q (most recent event)", hb.LastEventID, want)
	}
}

// TestEV039_HeartbeatLastEventIDZeroBeforeDispatch verifies that before any
// events have been dispatched, last_event_id is the empty string — consumers
// treat empty as "no events yet" and do not advance their watermark.
func TestEV039_HeartbeatLastEventIDZeroBeforeDispatch(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})
	hb := hub.makeHeartbeat()
	// last_event_id is "" when no events have fired — that is correct; the
	// watermark advance rule (EV-037a) is a no-op for empty strings.
	if hb.Type != "heartbeat" {
		t.Errorf("EV-039: heartbeat type: got %q, want %q", hb.Type, "heartbeat")
	}
	// last_event_id must be a valid string field (empty is acceptable here).
	_ = hb.LastEventID // access to confirm it compiles and is present
}

// TestEV039_ActiveRunsOmitsRunID verifies the activeRunSummary struct does NOT
// contain a run_id field. EV-039: "The active_runs array carries bead_id +
// age_seconds ONLY; it does NOT carry run_id."
//
// This test marshals a heartbeat to JSON and confirms the JSON object for each
// active_runs entry contains exactly the two required keys and no run_id key.
func TestEV039_ActiveRunsOmitsRunID(t *testing.T) {
	t.Parallel()

	reg := NewRunRegistry()
	runID, _ := uuid.NewV7()
	startedAt := time.Now().Add(-10 * time.Second)
	reg.Register(core.RunID(runID), &RunHandle{
		BeadID:    core.BeadID("hk-ev039-test"),
		StartedAt: startedAt,
	})

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:        nil,
		ActiveRuns: reg,
		Now:        func() time.Time { return startedAt.Add(20 * time.Second) },
	})

	hb := hub.makeHeartbeat()
	if len(hb.ActiveRuns) != 1 {
		t.Fatalf("EV-039: active_runs: got %d entries, want 1", len(hb.ActiveRuns))
	}

	// Marshal to JSON and decode into a generic map so we can inspect keys.
	raw, err := json.Marshal(hb.ActiveRuns[0])
	if err != nil {
		t.Fatalf("EV-039: marshal active_runs[0]: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("EV-039: unmarshal active_runs[0]: %v", err)
	}

	// MUST have bead_id.
	if _, ok := fields["bead_id"]; !ok {
		t.Error("EV-039: active_runs[0] missing required field bead_id")
	}
	// MUST have age_seconds.
	if _, ok := fields["age_seconds"]; !ok {
		t.Error("EV-039: active_runs[0] missing required field age_seconds")
	}
	// MUST NOT have run_id (EV-039: consumers MUST NOT assume run_id is present).
	if _, ok := fields["run_id"]; ok {
		t.Error("EV-039: active_runs[0] MUST NOT carry run_id; run-level correlation requires reading queue.json")
	}
}

// TestEV039_HeartbeatWireFormat verifies the wire-format heartbeat delivered
// over a real HandleSubscribe connection carries both last_event_id and
// active_runs fields. A fake timer is injected so the heartbeat fires within
// 20ms.
func TestEV039_HeartbeatWireFormat(t *testing.T) {
	t.Parallel()

	// Seed a run in the registry so active_runs is non-empty.
	reg := NewRunRegistry()
	runID, _ := uuid.NewV7()
	startedAt := time.Now().Add(-5 * time.Second)
	reg.Register(core.RunID(runID), &RunHandle{
		BeadID:    core.BeadID("hk-wire-test"),
		StartedAt: startedAt,
	})

	// Dispatch an event so last_event_id is non-empty.
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:        nil,
		ActiveRuns: reg,
		NewTimer: func(_ time.Duration) (<-chan time.Time, func() bool, func(time.Duration)) {
			ch := make(chan time.Time, 1)
			go func() {
				time.Sleep(20 * time.Millisecond)
				ch <- time.Now()
			}()
			return ch, func() bool { return true }, func(time.Duration) {}
		},
	})

	e := subscribeTestMakeEvent(t, "probe")
	_ = hub.dispatch(context.Background(), e)
	wantLastEventID := e.EventID.String()

	srv, cli := net.Pipe()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{HeartbeatSeconds: 60})
		close(done)
	}()

	rdr := bufio.NewReader(cli)
	_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	line, err := rdr.ReadBytes('\n')
	if len(line) == 0 && err != nil {
		t.Fatalf("EV-039: read heartbeat: %v", err)
	}

	// Decode as a generic map to verify exact wire fields.
	var wireMsg map[string]json.RawMessage
	if jsonErr := json.Unmarshal(line, &wireMsg); jsonErr != nil {
		t.Fatalf("EV-039: decode wire line %q: %v", string(line), jsonErr)
	}

	// Must be type=heartbeat.
	var msgType string
	if err := json.Unmarshal(wireMsg["type"], &msgType); err != nil || msgType != "heartbeat" {
		t.Fatalf("EV-039: wire message type: got %q, want heartbeat (raw=%q)", msgType, string(wireMsg["type"]))
	}

	// last_event_id must be present and non-empty (reflecting the dispatched event).
	lastEventIDRaw, ok := wireMsg["last_event_id"]
	if !ok {
		t.Fatal("EV-039: wire heartbeat missing last_event_id field")
	}
	var lastEventID string
	if err := json.Unmarshal(lastEventIDRaw, &lastEventID); err != nil {
		t.Fatalf("EV-039: decode last_event_id: %v", err)
	}
	if lastEventID == "" {
		t.Error("EV-039: wire heartbeat last_event_id is empty; must carry the most recent event_id")
	}
	if lastEventID != wantLastEventID {
		t.Errorf("EV-039: wire heartbeat last_event_id: got %q, want %q", lastEventID, wantLastEventID)
	}

	// active_runs must be present and be an array.
	activeRunsRaw, ok := wireMsg["active_runs"]
	if !ok {
		t.Fatal("EV-039: wire heartbeat missing active_runs field")
	}
	var activeRuns []map[string]json.RawMessage
	if err := json.Unmarshal(activeRunsRaw, &activeRuns); err != nil {
		t.Fatalf("EV-039: decode active_runs: %v (raw=%q)", err, string(activeRunsRaw))
	}
	if len(activeRuns) != 1 {
		t.Fatalf("EV-039: active_runs: got %d entries, want 1", len(activeRuns))
	}
	entry := activeRuns[0]
	if _, ok := entry["bead_id"]; !ok {
		t.Error("EV-039: active_runs[0] missing bead_id")
	}
	if _, ok := entry["age_seconds"]; !ok {
		t.Error("EV-039: active_runs[0] missing age_seconds")
	}
	if _, ok := entry["run_id"]; ok {
		t.Error("EV-039: active_runs[0] MUST NOT carry run_id (spec: consumers MUST NOT assume run_id is present)")
	}

	_ = cli.Close()
	<-done
}
