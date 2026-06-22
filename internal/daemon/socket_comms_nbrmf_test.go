package daemon_test

// socket_comms_nbrmf_test.go — socket-level tests for the "comms-send" op
// (agent-comms spec §2.1 C2, bead hk-nbrmf T4).
//
// Acceptance criteria verified here:
//   - comms-send op routes to CommsSendHandler when registered.
//   - A well-formed request emits an agent_message event to the bus.
//   - The response carries the minted event_id (non-empty UUIDv7 string).
//   - Validation errors (missing from/to/body, oversized body) return Ok=false.
//   - nil CommsSendHandler → Ok=false, error response (not registered).
//
// Spec ref: agent-comms spec §2.1 C2.
// Bead ref: hk-nbrmf.

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

// commsFixtureBuildBus constructs a sealed in-memory EventBus with a
// synchronous consumer that captures all agent_message events into *captured.
func commsFixtureBuildBus(t *testing.T) (eventbus.EventBus, *[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-comms-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Wildcard: false,
			Types:    map[core.EventType]struct{}{"agent_message": {}},
		},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			captured = append(captured, evt)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("commsFixtureBuildBus: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("commsFixtureBuildBus: Seal: %v", err)
	}
	return bus, &captured, &mu
}

// commsFixtureStartListener starts RunSocketListenerFull with a CommsSendHandler
// wired from the given bus. Returns the socket path; cleanup is registered.
func commsFixtureStartListener(t *testing.T, bus eventbus.EventBus) string {
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

// commsFixtureSendRequest dials sockPath, sends a comms-send SocketRequest with
// the given JSON payload, and returns the SocketResponse.
func commsFixtureSendRequest(t *testing.T, sockPath string, payload json.RawMessage) daemon.SocketResponse {
	t.Helper()

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable
	return socketFixtureSendRecv(t, conn, daemon.SocketRequest{
		Op:      "comms-send",
		Payload: payload,
	})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCommsSend_HappyPath verifies that a valid comms-send request emits an
// agent_message event and returns Ok=true with a non-empty event_id.
func TestCommsSend_HappyPath(t *testing.T) {
	t.Parallel()

	bus, captured, mu := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"from":"alice","to":"bob","body":"hello world"}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-send happy path: Ok=false, error=%q", resp.Error)
	}

	// Result must contain a non-empty event_id.
	var result daemon.CommsSendResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("comms-send happy path: unmarshal result: %v", err)
	}
	if result.EventID == "" {
		t.Fatal("comms-send happy path: result.event_id is empty")
	}
	// UUIDv7 is 36 characters in canonical form.
	if len(result.EventID) != 36 {
		t.Errorf("comms-send happy path: event_id length=%d, want 36 (UUID form); got %q",
			len(result.EventID), result.EventID)
	}

	// Drain the bus so synchronous consumer has finished (it runs inline, but
	// let's verify the captured event after the response).
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("comms-send happy path: bus.Drain: %v", err)
	}

	mu.Lock()
	n := len(*captured)
	var evt core.Event
	if n > 0 {
		evt = (*captured)[0]
	}
	mu.Unlock()

	if n != 1 {
		t.Fatalf("comms-send happy path: expected 1 agent_message event; got %d", n)
	}
	if evt.Type != "agent_message" {
		t.Errorf("comms-send happy path: event.Type=%q, want %q", evt.Type, "agent_message")
	}

	// Verify payload fields.
	var msgPayload core.AgentMessagePayload
	if err := json.Unmarshal(evt.Payload, &msgPayload); err != nil {
		t.Fatalf("comms-send happy path: unmarshal event payload: %v", err)
	}
	if msgPayload.From != "alice" {
		t.Errorf("comms-send happy path: payload.from=%q, want %q", msgPayload.From, "alice")
	}
	if msgPayload.To != "bob" {
		t.Errorf("comms-send happy path: payload.to=%q, want %q", msgPayload.To, "bob")
	}
	if msgPayload.Body != "hello world" {
		t.Errorf("comms-send happy path: payload.body=%q, want %q", msgPayload.Body, "hello world")
	}

	// event_id in the response MUST match the one stamped on the emitted event.
	if evt.EventID.String() != result.EventID {
		t.Errorf("comms-send happy path: response event_id=%q does not match emitted event_id=%q",
			result.EventID, evt.EventID.String())
	}
}

// TestCommsSend_BroadcastTo verifies that to="*" is accepted (broadcast).
func TestCommsSend_BroadcastTo(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"from":"alice","to":"*","body":"broadcast message"}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-send broadcast: Ok=false, error=%q", resp.Error)
	}
}

// TestCommsSend_WithTopicAndReplyTo verifies optional fields are accepted.
func TestCommsSend_WithTopicAndReplyTo(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{
		"from": "agent-a",
		"to": "agent-b",
		"topic": "status",
		"body": "all good",
		"in_reply_to": "01900000-0000-7000-8000-000000000001"
	}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if !resp.Ok {
		t.Fatalf("comms-send with topic+reply: Ok=false, error=%q", resp.Error)
	}
}

// TestCommsSend_MissingFrom verifies that omitting "from" returns an error.
func TestCommsSend_MissingFrom(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"to":"bob","body":"hello"}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-send missing from: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "from") {
		t.Errorf("comms-send missing from: error %q does not mention 'from'", resp.Error)
	}
}

// TestCommsSend_MissingTo verifies that omitting "to" returns an error.
func TestCommsSend_MissingTo(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"from":"alice","body":"hello"}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-send missing to: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "to") {
		t.Errorf("comms-send missing to: error %q does not mention 'to'", resp.Error)
	}
}

// TestCommsSend_MissingBody verifies that omitting "body" returns an error.
func TestCommsSend_MissingBody(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	payload := json.RawMessage(`{"from":"alice","to":"bob"}`)
	resp := commsFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-send missing body: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "body") {
		t.Errorf("comms-send missing body: error %q does not mention 'body'", resp.Error)
	}
}

// TestCommsSend_BodyExceedsCap verifies that a body over 8 KiB is rejected.
func TestCommsSend_BodyExceedsCap(t *testing.T) {
	t.Parallel()

	bus, _, _ := commsFixtureBuildBus(t)
	sockPath := commsFixtureStartListener(t, bus)

	bigBody := strings.Repeat("x", 8*1024+1) // 8193 bytes > 8 KiB
	req := map[string]string{"from": "alice", "to": "bob", "body": bigBody}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	resp := commsFixtureSendRequest(t, sockPath, payload)

	if resp.Ok {
		t.Fatal("comms-send oversized body: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "8 KiB") {
		t.Errorf("comms-send oversized body: error %q does not mention '8 KiB'", resp.Error)
	}
}

// TestCommsSend_NilHandler verifies that a nil CommsSendHandler returns Ok=false.
func TestCommsSend_NilHandler(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		// ch=nil: CommsSendHandler not registered.
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

	payload := json.RawMessage(`{"from":"alice","to":"bob","body":"hello"}`)
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable
	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: "comms-send", Payload: payload})

	if resp.Ok {
		t.Fatal("comms-send nil handler: Ok=true, want false")
	}
	if !strings.Contains(resp.Error, "CommsSendHandler not registered") {
		t.Errorf("comms-send nil handler: error %q does not mention 'CommsSendHandler not registered'", resp.Error)
	}
}
