package daemon

// socket_state.go — StateHandler socket interface for `harmonik state`.
//
// Defines the StateHandler interface, its live implementation, and the
// RunSocketListenerWithState wrapper that adds the "state" socket op.
//
// Spec ref: specs/system-state.md §4 (SS-001, SS-002fold, SS-INV-007).
// Bead ref: hk-gv04 (P2-a: harmonik state aggregator command).

import (
	"context"
	"encoding/json"
	"fmt"
)

// StateHandler is the interface the daemon registers to handle "state" socket
// requests.  The snapshot is read-only and MUST NOT mutate daemon state or
// affect any in-flight run (SS-INV-007).
type StateHandler interface {
	HandleState(ctx context.Context) (json.RawMessage, error)
}

// liveStateHandlerImpl wraps a LiveStateBuilder for the socket RPC.
type liveStateHandlerImpl struct {
	builder *LiveStateBuilder
}

// NewLiveStateSocketHandler returns a StateHandler backed by b.
func NewLiveStateSocketHandler(b *LiveStateBuilder) StateHandler {
	return &liveStateHandlerImpl{builder: b}
}

func (h *liveStateHandlerImpl) HandleState(ctx context.Context) (json.RawMessage, error) {
	snap := h.builder.Build(ctx)
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("daemon: state: marshal snapshot: %w", err)
	}
	return json.RawMessage(data), nil
}

// RunSocketListenerWithState is RunSocketListenerWithSleepWake with an
// additional StateHandler parameter.  When stateh is nil, "state" ops return
// an error response.
//
// Spec ref: specs/system-state.md SS-001 / SS-INV-007 (read-only observation).
// Bead ref: hk-gv04 (P2-a).
func RunSocketListenerWithState(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler, sleepWakeh QuiesceOverrideHandler, stateh StateHandler, qh ...QueueHandler) error {
	return Serve(ctx, sockPath, SocketHandlers{
		Request: h, HookRelay: hr, Queue: firstQueueHandler(qh), Subscribe: sub,
		Operator: oh, Comms: ch, Crew: crewh, SleepWake: sleepWakeh, State: stateh,
	})
}
