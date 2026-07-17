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
	"net"
	"os"
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
	if err := removeStaleSocket(sockPath); err != nil {
		return fmt.Errorf("daemon: RunSocketListenerWithDashboard: stale-socket check: %w", err)
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon: RunSocketListenerWithDashboard: listen unix %q: %w", sockPath, err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck

	if err := os.Chmod(sockPath, 0o600); err != nil {
		return fmt.Errorf("daemon: RunSocketListenerWithDashboard: chmod 0600 %q: %w", sockPath, err)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck
	}()

	var queueHandler QueueHandler
	if len(qh) > 0 {
		queueHandler = qh[0]
	}

	// Build the router ONCE per listener body, before the Accept loop.
	router := buildSocketRouter(&socketDispatch{
		h: h, qh: queueHandler, oh: oh, ch: ch, crewh: crewh, sleepWakeh: sleepWakeh, stateh: stateh, dashh: dashh,
	})

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("daemon: RunSocketListenerWithDashboard: accept: %w", err)
		}
		go handleSocketConn(ctx, conn, hr, sub, router)
	}
}
