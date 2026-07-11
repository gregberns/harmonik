package daemon_test

// commsrefresh_hk6vwi3_test.go — tests that refresh beats are emitted on
// comms-send and comms-recv (hk-6vwi3 fix #2, test case (e)).
//
// Verifies:
//   (e-send) comms-send emits agent_presence{online, refresh} for the sender after
//            the agent_message is accepted.
//   (e-recv) comms-recv emits agent_presence{online, refresh} for the receiving
//            agent after the cursor is advanced.
//
// Bead ref: hk-6vwi3.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// refreshFixtureBuildBus builds a sealed bus that captures BOTH agent_message
// and agent_presence events. Returns (bus, capturedPtr, mu).
func refreshFixtureBuildBus(t *testing.T) (eventbus.EventBus, *[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-refresh-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			captured = append(captured, evt)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("refreshFixtureBuildBus: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("refreshFixtureBuildBus: Seal: %v", err)
	}
	return bus, &captured, &mu
}

// refreshFixtureStartListener starts RunSocketListenerFull with a CommsSendHandler
// that has both send and recv deps wired. Returns (sockPath, eventsPath).
func refreshFixtureStartListener(t *testing.T, bus eventbus.EventBus) (sockPath, eventsPath string) {
	t.Helper()

	dir := t.TempDir()
	eventsPath = filepath.Join(dir, "events.jsonl")
	cursorsDir := filepath.Join(dir, "cursors")

	sockPath = socketFixtureTempSockPath(t)
	ch := daemon.NewCommsSendHandler(bus)

	// Wire recv deps so comms-recv works.
	type recvDepsSetter interface {
		SetRecvDeps(pollStore, liveStore *daemon.CursorStore, eventsJSONLPath string)
	}
	if rds, ok := ch.(recvDepsSetter); ok {
		cs := daemon.NewCursorStore(cursorsDir)
		rds.SetRecvDeps(cs, cs, eventsPath)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, nil, nil, ch)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		default:
		}
	})
	socketFixtureWaitReady(t, sockPath)
	return sockPath, eventsPath
}

// capturedPresenceBeats returns the agent_presence events in captured whose
// reason field equals "refresh", for the given agent name.
func capturedPresenceBeats(captured []core.Event, agent string) []core.AgentPresencePayload {
	var out []core.AgentPresencePayload
	for _, ev := range captured {
		if ev.Type != "agent_presence" {
			continue
		}
		var p core.AgentPresencePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			continue
		}
		if p.Agent != agent {
			continue
		}
		if p.Reason != core.AgentPresenceReasonRefresh {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---------------------------------------------------------------------------
// (e-send) refresh beat emitted on comms-send
// ---------------------------------------------------------------------------

// TestCommsRefresh_SendEmitsPresenceBeat verifies that a successful comms-send
// op results in an agent_presence{online, reason:"refresh"} event being emitted
// for the message sender (hk-6vwi3 fix #2 — send path).
func TestCommsRefresh_SendEmitsPresenceBeat(t *testing.T) {
	t.Parallel()

	bus, captured, mu := refreshFixtureBuildBus(t)
	sockPath, _ := refreshFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"from":"orchestrator","to":"worker","body":"do work"}`)
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()
	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{
		Op:      "comms-send",
		Payload: payload,
	})
	if !resp.Ok {
		t.Fatalf("comms-send: Ok=false: %s", resp.Error)
	}

	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("bus.Drain: %v", err)
	}

	mu.Lock()
	beats := capturedPresenceBeats(*captured, "orchestrator")
	mu.Unlock()

	if len(beats) == 0 {
		t.Fatal("comms-send: expected a refresh presence beat for 'orchestrator'; got none")
	}
	if beats[0].Status != core.AgentPresenceStatusOnline {
		t.Errorf("comms-send: refresh beat status=%q, want %q", beats[0].Status, core.AgentPresenceStatusOnline)
	}
	if beats[0].LastSeen == "" {
		t.Error("comms-send: refresh beat last_seen is empty")
	}
}

// ---------------------------------------------------------------------------
// (e-recv) refresh beat emitted on comms-recv
// ---------------------------------------------------------------------------

// TestCommsRefresh_RecvEmitsPresenceBeat verifies that a comms-recv op emits an
// agent_presence{online, reason:"refresh"} event for the receiving agent
// (hk-6vwi3 fix #2 — recv path).
func TestCommsRefresh_RecvEmitsPresenceBeat(t *testing.T) {
	t.Parallel()

	bus, captured, mu := refreshFixtureBuildBus(t)
	sockPath, eventsPath := refreshFixtureStartListener(t, bus)

	// Seed one message directed to "worker" so the recv op has something to drain.
	writeTestEventFile(t, eventsPath, "agent_message", map[string]any{
		"from": "orchestrator",
		"to":   "worker",
		"body": "hello worker",
	})

	payload := json.RawMessage(`{"agent":"worker"}`)
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()
	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{
		Op:      "comms-recv",
		Payload: payload,
	})
	if !resp.Ok {
		t.Fatalf("comms-recv: Ok=false: %s", resp.Error)
	}

	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("bus.Drain: %v", err)
	}

	mu.Lock()
	beats := capturedPresenceBeats(*captured, "worker")
	mu.Unlock()

	if len(beats) == 0 {
		t.Fatal("comms-recv: expected a refresh presence beat for 'worker'; got none")
	}
	if beats[0].Status != core.AgentPresenceStatusOnline {
		t.Errorf("comms-recv: refresh beat status=%q, want %q", beats[0].Status, core.AgentPresenceStatusOnline)
	}
}

// TestCommsRefresh_RecvEmitsBeatEvenWhenEmpty verifies that comms-recv emits a
// refresh beat for the agent even when the backlog is empty (no new messages).
func TestCommsRefresh_RecvEmitsBeatEvenWhenEmpty(t *testing.T) {
	t.Parallel()

	bus, captured, mu := refreshFixtureBuildBus(t)
	sockPath, _ := refreshFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"idleagent"}`)
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()
	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{
		Op:      "comms-recv",
		Payload: payload,
	})
	if !resp.Ok {
		t.Fatalf("comms-recv empty: Ok=false: %s", resp.Error)
	}

	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("bus.Drain: %v", err)
	}

	mu.Lock()
	beats := capturedPresenceBeats(*captured, "idleagent")
	mu.Unlock()

	if len(beats) == 0 {
		t.Fatal("comms-recv empty backlog: expected a refresh presence beat; got none")
	}
}

// ---------------------------------------------------------------------------
// helper: write event directly to a JSONL file without going through the bus
// ---------------------------------------------------------------------------

func writeTestEventFile(t *testing.T, path string, evType string, payload any) {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("writeTestEventFile: marshal payload: %v", err)
	}
	line := map[string]any{
		"event_id":         "01965b00-0000-7000-8000-abcdef000001",
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   time.Now().UTC().Format(time.RFC3339),
		"source_subsystem": "daemon.comms",
		"payload":          json.RawMessage(payloadBytes),
	}
	lineBytes, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("writeTestEventFile: marshal line: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("writeTestEventFile: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	lineBytes = append(lineBytes, '\n')
	if _, err := f.Write(lineBytes); err != nil {
		t.Fatalf("writeTestEventFile: write: %v", err)
	}
}
