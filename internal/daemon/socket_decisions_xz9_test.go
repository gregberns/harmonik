package daemon_test

// socket_decisions_xz9_test.go — socket-level tests for the hitl-decisions
// agent-side emit ops (hitl-decisions SPEC §2, component K2, bead hk-xz9):
//   - decisions-raise    → emits decision_needed; returns the minted decision_id
//                          (= the decision_needed event's own event_id, SPEC §1).
//   - decisions-withdraw → emits decision_withdrawn(self_obsoleted); returns event_id.
//
// Acceptance criteria verified here:
//   - the ops route to the DecisionsHandler (rode on the CommsSendHandler value)
//     when registered.
//   - a well-formed raise emits a decision_needed event AND the returned
//     decision_id equals that event's own event_id (the K3 key contract, SPEC §1).
//   - a well-formed withdraw emits a decision_withdrawn with payload.decision_id
//     set to the supplied id and reason=self_obsoleted.
//   - validation errors (missing question / no options for raise; missing id or
//     bad reason for withdraw) return Ok=false with no event emitted.
//   - a nil handler returns Ok=false ("DecisionsHandler not registered").
//
// Reuses the socketFixture* helpers (socket_test.go) and the same in-memory bus
// pattern as socket_comms_nbrmf_test.go.
//
// Bead ref: hk-xz9 (K2).

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

// dx9FixtureBuildBus builds a sealed in-memory bus capturing the three decision_*
// event types into *captured.
func dx9FixtureBuildBus(t *testing.T) (eventbus.EventBus, *[]core.Event, *sync.Mutex) {
	t.Helper()

	bus := eventbus.NewBusImpl()
	var mu sync.Mutex
	var captured []core.Event

	sub := core.Subscription{
		ConsumerID:    "test-decisions-capture",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Wildcard: false,
			Types: map[core.EventType]struct{}{
				core.EventTypeDecisionNeeded:    {},
				core.EventTypeDecisionResolved:  {},
				core.EventTypeDecisionWithdrawn: {},
			},
		},
		Handler: func(_ context.Context, evt core.Event) error {
			mu.Lock()
			captured = append(captured, evt)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("dx9FixtureBuildBus: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("dx9FixtureBuildBus: Seal: %v", err)
	}
	return bus, &captured, &mu
}

func dx9FixtureStartListener(t *testing.T, bus eventbus.EventBus) string {
	t.Helper()
	sockPath := socketFixtureTempSockPath(t)
	// NewCommsSendHandler also wires the TypedEmitter for the decisions-* ops
	// (the same handler value rides as both CommsSendHandler and DecisionsHandler).
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

func dx9FixtureOp(t *testing.T, sockPath, op string, payload json.RawMessage) daemon.SocketResponse {
	t.Helper()
	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()
	return socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: op, Payload: payload})
}

// TestDecisionsRaise_HappyPath: a well-formed raise emits decision_needed and the
// returned decision_id equals that event's own event_id (SPEC §1 key contract).
func TestDecisionsRaise_HappyPath(t *testing.T) {
	t.Parallel()
	bus, captured, mu := dx9FixtureBuildBus(t)
	sockPath := dx9FixtureStartListener(t, bus)

	payload := json.RawMessage(`{"question":"Ship v2?","options":["ship","hold"],"blocked_agent":"alice","context_link":"hk-aaa"}`)
	resp := dx9FixtureOp(t, sockPath, "decisions-raise", payload)
	if !resp.Ok {
		t.Fatalf("decisions-raise happy path: Ok=false, error=%q", resp.Error)
	}

	var result daemon.DecisionsRaiseResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode raise result: %v", err)
	}
	if result.DecisionID == "" {
		t.Fatal("decisions-raise returned empty decision_id")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) != 1 {
		t.Fatalf("expected 1 decision_needed event, got %d", len(*captured))
	}
	ev := (*captured)[0]
	if ev.Type != string(core.EventTypeDecisionNeeded) {
		t.Errorf("captured event type = %q, want decision_needed", ev.Type)
	}
	// The decision_id MUST be the decision_needed event's OWN event_id (SPEC §1).
	if ev.EventID.String() != result.DecisionID {
		t.Errorf("decision_id %q != decision_needed event_id %q (SPEC §1 key contract)", result.DecisionID, ev.EventID.String())
	}
	var p core.DecisionNeededPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("decode decision_needed payload: %v", err)
	}
	if p.Question != "Ship v2?" || len(p.Options) != 2 || p.BlockedAgent != "alice" {
		t.Errorf("decision_needed payload wrong: %+v", p)
	}
}

// TestDecisionsRaise_Validation: missing question or empty options → Ok=false,
// no event emitted.
func TestDecisionsRaise_Validation(t *testing.T) {
	t.Parallel()
	bus, captured, mu := dx9FixtureBuildBus(t)
	sockPath := dx9FixtureStartListener(t, bus)

	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"no-question", `{"options":["a","b"]}`},
		{"no-options", `{"question":"Q?"}`},
		{"empty-options", `{"question":"Q?","options":[]}`},
	} {
		resp := dx9FixtureOp(t, sockPath, "decisions-raise", json.RawMessage(tc.payload))
		if resp.Ok {
			t.Errorf("%s: expected Ok=false, got Ok=true", tc.name)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) != 0 {
		t.Fatalf("invalid raises must emit no event, got %d", len(*captured))
	}
}

// TestDecisionsWithdraw_HappyPath: a well-formed withdraw emits
// decision_withdrawn with payload.decision_id and reason set.
func TestDecisionsWithdraw_HappyPath(t *testing.T) {
	t.Parallel()
	bus, captured, mu := dx9FixtureBuildBus(t)
	sockPath := dx9FixtureStartListener(t, bus)

	const did = "01965b00-0000-7000-8000-00000000d0d1"
	payload := json.RawMessage(`{"decision_id":"` + did + `","reason":"self_obsoleted","by":"alice"}`)
	resp := dx9FixtureOp(t, sockPath, "decisions-withdraw", payload)
	if !resp.Ok {
		t.Fatalf("decisions-withdraw happy path: Ok=false, error=%q", resp.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) != 1 {
		t.Fatalf("expected 1 decision_withdrawn event, got %d", len(*captured))
	}
	ev := (*captured)[0]
	if ev.Type != string(core.EventTypeDecisionWithdrawn) {
		t.Errorf("captured event type = %q, want decision_withdrawn", ev.Type)
	}
	var p core.DecisionWithdrawnPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("decode decision_withdrawn payload: %v", err)
	}
	if p.DecisionID != did {
		t.Errorf("withdrawn decision_id = %q, want %q", p.DecisionID, did)
	}
	if p.Reason != core.DecisionWithdrawnReasonSelfObsoleted {
		t.Errorf("withdrawn reason = %q, want self_obsoleted", p.Reason)
	}
}

// TestDecisionsWithdraw_Validation: missing id or bad reason → Ok=false.
func TestDecisionsWithdraw_Validation(t *testing.T) {
	t.Parallel()
	bus, captured, mu := dx9FixtureBuildBus(t)
	sockPath := dx9FixtureStartListener(t, bus)

	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"no-id", `{"reason":"self_obsoleted"}`},
		{"bad-reason", `{"decision_id":"01965b00-0000-7000-8000-00000000d0d2","reason":"bogus"}`},
	} {
		resp := dx9FixtureOp(t, sockPath, "decisions-withdraw", json.RawMessage(tc.payload))
		if resp.Ok {
			t.Errorf("%s: expected Ok=false, got Ok=true", tc.name)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) != 0 {
		t.Fatalf("invalid withdraws must emit no event, got %d", len(*captured))
	}
}

// TestDecisions_NilHandler: a nil CommsSendHandler (no DecisionsHandler) returns
// Ok=false for the decisions-* ops.
func TestDecisions_NilHandler(t *testing.T) {
	t.Parallel()
	sockPath := socketFixtureTempSockPath(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, nil, nil, nil) // ch=nil
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		default:
		}
	})
	socketFixtureWaitReady(t, sockPath)

	resp := dx9FixtureOp(t, sockPath, "decisions-raise", json.RawMessage(`{"question":"Q?","options":["a"]}`))
	if resp.Ok {
		t.Fatal("nil handler: expected Ok=false")
	}
	if !strings.Contains(resp.Error, "DecisionsHandler not registered") {
		t.Errorf("nil handler error %q does not mention 'DecisionsHandler not registered'", resp.Error)
	}
}
