package daemon_test

// socket_commspresence_7t27s_test.go — socket-level tests for the "comms-presence" op
// (agent-comms spec §2.5 C6, bead hk-7t27s T10).
//
// Acceptance criteria verified here:
//   - comms-presence op routes to CommsPresenceHandler when registered (via *commsSendHandlerImpl).
//   - A join request (status=online, reason=join) emits an agent_presence event.
//   - A leave request (status=offline, reason=leave) emits an agent_presence event.
//   - The response carries the minted event_id (non-empty UUIDv7 string).
//   - Validation errors (missing agent, invalid status) return Ok=false.
//   - nil CommsSendHandler → CommsPresenceHandler type-assert fails → Ok=false, "not registered".
//
// Spec ref: agent-comms spec §2.5, §4.
// Bead ref: hk-7t27s.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// commsPresenceFixtureBuildBus constructs a sealed in-memory EventBus with a
// synchronous consumer that captures all agent_presence events into *captured.
func commsPresenceFixtureBuildBus(t *testing.T) (eventbus.EventBus, *[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-comms-presence-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Wildcard: false,
			Types:    map[core.EventType]struct{}{"agent_presence": {}},
		},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			captured = append(captured, evt)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("commsPresenceFixtureBuildBus: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("commsPresenceFixtureBuildBus: Seal: %v", err)
	}
	return bus, &captured, &mu
}

// commsPresenceFixtureStartListener starts RunSocketListenerFull with a CommsSendHandler
// (which also satisfies CommsPresenceHandler). Returns the socket path; cleanup is registered.
func commsPresenceFixtureStartListener(t *testing.T, bus eventbus.EventBus) string {
	t.Helper()

	sockPath := socketFixtureTempSockPath(t)
	ch := daemon.NewCommsSendHandler(bus)
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
	return sockPath
}

// commsPresenceFixtureSendRequest dials sockPath, sends a comms-presence SocketRequest with
// the given JSON payload, and returns the SocketResponse.
func commsPresenceFixtureSendRequest(t *testing.T, sockPath string, payload json.RawMessage) daemon.SocketResponse {
	t.Helper()

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable
	return socketFixtureSendRecv(t, conn, daemon.SocketRequest{
		Op:      "comms-presence",
		Payload: payload,
	})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCommsPresence_JoinHappyPath verifies that a join request emits an online
// agent_presence event and returns Ok=true with a non-empty event_id.
func TestCommsPresence_JoinHappyPath(t *testing.T) {
	t.Parallel()

	bus, captured, mu := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"alice","status":"online","reason":"join"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-presence join: Ok=false, error=%q", resp.Error)
	}

	// Result must contain a non-empty event_id (UUIDv7, 36-char canonical form).
	var result daemon.CommsPresenceResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("comms-presence join: unmarshal result: %v", err)
	}
	if result.EventID == "" {
		t.Fatal("comms-presence join: result.event_id is empty")
	}
	if len(result.EventID) != 36 {
		t.Errorf("comms-presence join: event_id length=%d, want 36 (UUID form); got %q",
			len(result.EventID), result.EventID)
	}

	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("comms-presence join: bus.Drain: %v", err)
	}

	mu.Lock()
	n := len(*captured)
	var evt core.Event
	if n > 0 {
		evt = (*captured)[0]
	}
	mu.Unlock()

	if n != 1 {
		t.Fatalf("comms-presence join: expected 1 agent_presence event; got %d", n)
	}
	if evt.Type != "agent_presence" {
		t.Errorf("comms-presence join: event.Type=%q, want %q", evt.Type, "agent_presence")
	}

	var presPayload core.AgentPresencePayload
	if err := json.Unmarshal(evt.Payload, &presPayload); err != nil {
		t.Fatalf("comms-presence join: unmarshal event payload: %v", err)
	}
	if presPayload.Agent != "alice" {
		t.Errorf("comms-presence join: payload.agent=%q, want %q", presPayload.Agent, "alice")
	}
	if presPayload.Status != core.AgentPresenceStatusOnline {
		t.Errorf("comms-presence join: payload.status=%q, want %q", presPayload.Status, core.AgentPresenceStatusOnline)
	}
	if presPayload.Reason != core.AgentPresenceReasonJoin {
		t.Errorf("comms-presence join: payload.reason=%q, want %q", presPayload.Reason, core.AgentPresenceReasonJoin)
	}
	if presPayload.LastSeen == "" {
		t.Error("comms-presence join: payload.last_seen is empty (handler must stamp wall time)")
	}

	// event_id in the response MUST match the one stamped on the emitted event.
	if evt.EventID.String() != result.EventID {
		t.Errorf("comms-presence join: response event_id=%q does not match emitted event_id=%q",
			result.EventID, evt.EventID.String())
	}
}

// TestCommsPresence_LeaveHappyPath verifies that a leave request emits an offline
// agent_presence event and returns Ok=true with a non-empty event_id.
func TestCommsPresence_LeaveHappyPath(t *testing.T) {
	t.Parallel()

	bus, captured, mu := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"bob","status":"offline","reason":"leave"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-presence leave: Ok=false, error=%q", resp.Error)
	}

	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("comms-presence leave: bus.Drain: %v", err)
	}

	mu.Lock()
	n := len(*captured)
	var evt core.Event
	if n > 0 {
		evt = (*captured)[0]
	}
	mu.Unlock()

	if n != 1 {
		t.Fatalf("comms-presence leave: expected 1 agent_presence event; got %d", n)
	}

	var presPayload core.AgentPresencePayload
	if err := json.Unmarshal(evt.Payload, &presPayload); err != nil {
		t.Fatalf("comms-presence leave: unmarshal event payload: %v", err)
	}
	if presPayload.Agent != "bob" {
		t.Errorf("comms-presence leave: payload.agent=%q, want %q", presPayload.Agent, "bob")
	}
	if presPayload.Status != core.AgentPresenceStatusOffline {
		t.Errorf("comms-presence leave: payload.status=%q, want %q", presPayload.Status, core.AgentPresenceStatusOffline)
	}
	if presPayload.Reason != core.AgentPresenceReasonLeave {
		t.Errorf("comms-presence leave: payload.reason=%q, want %q", presPayload.Reason, core.AgentPresenceReasonLeave)
	}
}

// TestCommsPresence_RefreshReason verifies that reason=refresh is accepted.
func TestCommsPresence_RefreshReason(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"carol","status":"online","reason":"refresh"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-presence refresh: Ok=false, error=%q", resp.Error)
	}
}

// TestCommsPresence_NoReason verifies that omitting reason (optional) is accepted.
func TestCommsPresence_NoReason(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"dave","status":"online"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-presence no reason: Ok=false, error=%q", resp.Error)
	}
}

// TestCommsPresence_MissingAgent verifies that omitting "agent" returns an error.
func TestCommsPresence_MissingAgent(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"status":"online","reason":"join"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-presence missing agent: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "agent") {
		t.Errorf("comms-presence missing agent: error %q does not mention 'agent'", resp.Error)
	}
}

// TestCommsPresence_InvalidStatus verifies that an invalid status value returns an error.
func TestCommsPresence_InvalidStatus(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"eve","status":"unknown"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-presence invalid status: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "status") {
		t.Errorf("comms-presence invalid status: error %q does not mention 'status'", resp.Error)
	}
}

// TestCommsPresence_InvalidReason verifies that an invalid reason value returns an error.
func TestCommsPresence_InvalidReason(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsPresenceFixtureBuildBus(t)
	sockPath := commsPresenceFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"agent":"frank","status":"online","reason":"badvalue"}`)
	resp := commsPresenceFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-presence invalid reason: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "reason") {
		t.Errorf("comms-presence invalid reason: error %q does not mention 'reason'", resp.Error)
	}
}

// TestCommsPresence_NilHandler verifies that nil CommsSendHandler means comms-presence
// returns Ok=false "CommsPresenceHandler not registered".
func TestCommsPresence_NilHandler(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		// ch=nil: neither CommsSendHandler nor CommsPresenceHandler is registered.
		done <- daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, nil, nil, nil)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		default:
		}
	})
	socketFixtureWaitReady(t, sockPath)

	payload := json.RawMessage(`{"agent":"grace","status":"online","reason":"join"}`)
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable
	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: "comms-presence", Payload: payload})

	if resp.Ok {
		t.Fatal("comms-presence nil handler: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "not registered") {
		t.Errorf("comms-presence nil handler: error %q does not mention 'not registered'", resp.Error)
	}
}
