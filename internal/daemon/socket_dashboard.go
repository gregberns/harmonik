package daemon

// socket_dashboard.go — DashboardHandler socket interface for `harmonik dashboard`.
//
// Defines the DashboardHandler interface, its live implementation backed by
// DashboardBuilder, and RunSocketListenerWithDashboard which adds the
// "dashboard" socket op on top of RunSocketListenerWithState.
//
// The "dashboard" op mirrors the "state" op (socket.go:742): same
// request/response envelope, same read-only invariant (no mutation).
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §2.
// Bead ref: hk-2exz9.

import (
	"context"
	"encoding/json"
	"fmt"
)

// DashboardHandler is the interface the daemon registers to handle "dashboard"
// socket requests. The snapshot MUST NOT mutate daemon state or affect any
// in-flight run (mirrors SS-INV-007 for the state op).
type DashboardHandler interface {
	HandleDashboard(ctx context.Context) (json.RawMessage, error)
}

// liveDashboardHandlerImpl wraps a DashboardBuilder for the socket RPC.
type liveDashboardHandlerImpl struct {
	builder *DashboardBuilder
}

// NewLiveDashboardSocketHandler returns a DashboardHandler backed by b.
func NewLiveDashboardSocketHandler(b *DashboardBuilder) DashboardHandler {
	return &liveDashboardHandlerImpl{builder: b}
}

func (h *liveDashboardHandlerImpl) HandleDashboard(ctx context.Context) (json.RawMessage, error) {
	snap := h.builder.Build(ctx)
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("daemon: dashboard: marshal snapshot: %w", err)
	}
	return json.RawMessage(data), nil
}

// RunSocketListenerWithDashboard is RunSocketListenerWithState with an
// additional DashboardHandler parameter. When dashh is nil, "dashboard" ops
// return an error response.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §2.
// Bead ref: hk-2exz9.
func RunSocketListenerWithDashboard(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler, sleepWakeh QuiesceOverrideHandler, stateh StateHandler, dashh DashboardHandler, qh ...QueueHandler) error {
	return Serve(ctx, sockPath, SocketHandlers{
		Request: h, HookRelay: hr, Queue: firstQueueHandler(qh), Subscribe: sub,
		Operator: oh, Comms: ch, Crew: crewh, SleepWake: sleepWakeh, State: stateh, Dashboard: dashh,
	})
}
