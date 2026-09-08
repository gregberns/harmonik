package herdrwire

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// SubscriptionType names one events.subscribe filter. Only the two the P0
// keeper needs are modeled: PaneCreated (to learn a new agent's pane id)
// and PaneAgentStatusChanged (to cache agent_status without polling
// pane.process_info every tick).
//
// PaneCreated is global: no pane_id filter, and it fires for every pane on
// the server. PaneAgentStatusChanged is scoped: verified live against the
// protocol-22 server, a subscription of this type with no pane_id is
// refused with {"code":"invalid_request","message":"missing field
// `pane_id`"} — matching the design of record
// (plans/2026-09-07-keeper-herdr-substrate/README.md §5.3, "subscribe per
// pane, after learning it from a global pane.created"). Use
// PaneAgentStatusChangedSpec to subscribe to one pane's status changes.
type SubscriptionType string

// The two SubscriptionType values this package supports.
const (
	SubscribePaneCreated            SubscriptionType = "pane.created"
	SubscribePaneAgentStatusChanged SubscriptionType = "pane.agent_status_changed"
)

// SubscriptionSpec is one events.subscribe filter to request. PaneID is
// required for SubscribePaneAgentStatusChanged (the server rejects the
// subscribe call otherwise) and must be left empty for SubscribePaneCreated
// (a global subscription; herdr has no pane_id filter for it).
type SubscriptionSpec struct {
	Type   SubscriptionType
	PaneID string
}

// PaneCreatedSpec is the global "learn every new pane" subscription.
func PaneCreatedSpec() SubscriptionSpec {
	return SubscriptionSpec{Type: SubscribePaneCreated}
}

// PaneAgentStatusChangedSpec subscribes to agent_status changes for one
// specific pane. paneID must be non-empty — herdr fails closed on this
// subscription type without one.
func PaneAgentStatusChangedSpec(paneID string) SubscriptionSpec {
	return SubscriptionSpec{Type: SubscribePaneAgentStatusChanged, PaneID: paneID}
}

// PaneCreatedEvent is the "pane_created" subscription-event payload.
type PaneCreatedEvent struct {
	Pane PaneInfo `json:"pane"`
}

// PaneAgentStatusChangedEvent is the "pane_agent_status_changed"
// subscription-event payload.
type PaneAgentStatusChangedEvent struct {
	PaneID      string      `json:"pane_id"`
	WorkspaceID string      `json:"workspace_id"`
	Agent       string      `json:"agent"`
	AgentStatus AgentStatus `json:"agent_status"`
}

// Event is one decoded subscription-event line. Exactly one of the typed
// fields is non-nil, selected by Kind; an event type this package does not
// model decodes with Kind set and Raw carrying the "data" bytes verbatim,
// rather than being dropped or erroring the stream.
type Event struct {
	Kind                   string
	PaneCreated            *PaneCreatedEvent
	PaneAgentStatusChanged *PaneAgentStatusChangedEvent
	Raw                    json.RawMessage
}

type wireSubscriptionSpec struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id,omitempty"`
}

type wireEventsSubscribeParams struct {
	Subscriptions []wireSubscriptionSpec `json:"subscriptions"`
}

type wireSubscriptionEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// Subscription is a live events.subscribe stream. Unlike every other
// Client method it holds one connection open for its whole lifetime — the
// documented exception to dial-per-call (package doc). Call Next in a loop
// until it returns an error, then Close.
type Subscription struct {
	conn   net.Conn
	reader *bufio.Reader
	closed bool
}

// Subscribe opens one events.subscribe stream for the given subscription
// specs and blocks until the server acks it ("subscription_started") or
// the ack fails. The returned *Subscription owns the connection; the
// caller must Close it.
//
// Fails closed before dialing when a SubscribePaneAgentStatusChanged spec
// carries an empty PaneID — herdr itself refuses that combination
// (invalid_request, "missing field `pane_id`"), so this check only turns
// the failure into a local, typed one instead of a round trip.
func (c *Client) Subscribe(ctx context.Context, specs ...SubscriptionSpec) (*Subscription, error) {
	wireSpecs := make([]wireSubscriptionSpec, len(specs))
	for i, s := range specs {
		if s.Type == SubscribePaneAgentStatusChanged && s.PaneID == "" {
			return nil, fmt.Errorf("herdrwire: Subscribe: %s requires a non-empty PaneID", s.Type)
		}
		wireSpecs[i] = wireSubscriptionSpec{Type: string(s.Type), PaneID: s.PaneID}
	}

	if err := c.checkProtocolOnce(ctx); err != nil {
		return nil, err
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	paramsRaw, err := json.Marshal(wireEventsSubscribeParams{Subscriptions: wireSpecs})
	if err != nil {
		abortSubscribeDial(ctx, conn)
		return nil, fmt.Errorf("herdrwire: marshal events.subscribe params: %w", err)
	}
	req := wireRequest{ID: c.nextRequestID(), Method: "events.subscribe", Params: paramsRaw}
	line, err := json.Marshal(req)
	if err != nil {
		abortSubscribeDial(ctx, conn)
		return nil, fmt.Errorf("herdrwire: marshal events.subscribe request: %w", err)
	}
	line = append(line, '\n')
	if _, err := conn.Write(line); err != nil {
		abortSubscribeDial(ctx, conn)
		return nil, &DialError{SockPath: c.sockPath, Err: fmt.Errorf("write events.subscribe: %w", err)}
	}

	reader := bufio.NewReader(conn)
	ackLine, err := reader.ReadBytes('\n')
	if err != nil && len(ackLine) == 0 {
		abortSubscribeDial(ctx, conn)
		return nil, &DialError{SockPath: c.sockPath, Err: fmt.Errorf("read events.subscribe ack: %w", err)}
	}
	var ack wireResponse
	if jsonErr := json.Unmarshal(ackLine, &ack); jsonErr != nil {
		abortSubscribeDial(ctx, conn)
		return nil, &FrameError{Line: ackLine, Err: jsonErr}
	}
	if ack.Error != nil {
		abortSubscribeDial(ctx, conn)
		return nil, ack.Error
	}

	return &Subscription{conn: conn, reader: reader}, nil
}

// abortSubscribeDial closes a Subscribe connection that failed before
// handoff to a *Subscription. The close error is immaterial here (the
// connection is being abandoned regardless) but observable, so it is
// logged rather than silently dropped.
func abortSubscribeDial(ctx context.Context, conn net.Conn) {
	if err := conn.Close(); err != nil {
		slog.WarnContext(ctx, "herdrwire: close conn after failed Subscribe setup", "err", err)
	}
}

// Next blocks for the next event on the stream. It fails closed: a
// mid-stream close, a read past the connection's deadline, or a malformed
// event line all return a non-nil error rather than a zero-value Event —
// callers must not treat a decode failure as "no event yet".
func (s *Subscription) Next(ctx context.Context) (Event, error) {
	if s.closed {
		return Event{}, fmt.Errorf("herdrwire: Next called on closed Subscription")
	}
	if dl, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(dl); err != nil {
			return Event{}, fmt.Errorf("herdrwire: set subscription read deadline: %w", err)
		}
	} else if err := s.conn.SetReadDeadline(time.Time{}); err != nil {
		return Event{}, fmt.Errorf("herdrwire: clear subscription read deadline: %w", err)
	}

	line, err := s.reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Event{}, fmt.Errorf("herdrwire: subscription stream closed: %w", err)
	}

	var env wireSubscriptionEnvelope
	if jsonErr := json.Unmarshal(line, &env); jsonErr != nil {
		return Event{}, &FrameError{Line: line, Err: jsonErr}
	}

	out := Event{Kind: env.Event, Raw: env.Data}
	switch env.Event {
	case "pane_created":
		var d PaneCreatedEvent
		if jsonErr := json.Unmarshal(env.Data, &d); jsonErr != nil {
			return Event{}, &FrameError{Line: line, Err: jsonErr}
		}
		out.PaneCreated = &d
	case "pane_agent_status_changed":
		var d PaneAgentStatusChangedEvent
		if jsonErr := json.Unmarshal(env.Data, &d); jsonErr != nil {
			return Event{}, &FrameError{Line: line, Err: jsonErr}
		}
		out.PaneAgentStatusChanged = &d
	}
	return out, nil
}

// Close ends the subscription stream. Idempotent.
func (s *Subscription) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
}
