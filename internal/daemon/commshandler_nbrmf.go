package daemon

// commshandler_nbrmf.go — CommsSendHandler interface and implementation for the
// comms-send socket op (agent-comms spec §2.1 C2, bead hk-nbrmf T4).
//
// The handler validates a comms-send request, emits an agent_message event via
// the event bus, and returns the minted event_id in the SocketResponse. The bus
// guarantees fsync-before-return for F-class events (agent_message is F-class per
// busimpl.go:fsyncBoundaryEventTypes), satisfying the "no silent drops" goal (G2).
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.1 C2.
// Bead ref: hk-nbrmf.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// commsSendBodySizeCap is the soft body size limit for comms-send (agent-comms
// spec §1.1). Requests exceeding this limit are rejected; no event is emitted.
const commsSendBodySizeCap = 8 * 1024 // 8 KiB

// CommsSendHandler is the interface the daemon registers to process comms-send
// socket ops. It mirrors QueueHandler / SubscribeHandler in purpose.
//
// A nil CommsSendHandler causes comms-send ops to return an error response.
// Spec ref: agent-comms spec §2.1 C2.
// Bead ref: hk-nbrmf.
type CommsSendHandler interface {
	// HandleCommsSend processes one comms-send payload (the "payload" field of
	// the SocketRequest). Returns the JSON-encoded CommsSendResult on success.
	HandleCommsSend(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// CommsSendRequest is the wire payload for a "comms-send" socket op.
// It maps to the agent_message event fields (agent-comms spec §1.1).
type CommsSendRequest struct {
	// From is the sender's presence id. REQUIRED.
	From string `json:"from"`

	// To is the directed recipient name OR the broadcast sentinel "*". REQUIRED.
	To string `json:"to"`

	// Topic is an optional free-text filter key.
	Topic string `json:"topic,omitempty"`

	// Body is the compact UTF-8 message body. REQUIRED. Enforced ≤ 8 KiB.
	Body string `json:"body"`

	// InReplyTo is the optional event_id of the message being replied to.
	InReplyTo *string `json:"in_reply_to,omitempty"`

	// SessionID is an optional opaque per-session token (from $HARMONIK_SESSION_ID).
	// Forwarded to the implicit refresh beat so the presence projection can detect
	// two-captains conflicts (hk-z0f02).
	SessionID string `json:"session_id,omitempty"`
}

// CommsSendResult is the SocketResponse.Result payload for a successful comms-send op.
type CommsSendResult struct {
	// EventID is the UUIDv7 event_id of the minted agent_message event.
	EventID string `json:"event_id"`
}

// commsSendHandlerImpl is the concrete CommsSendHandler (and CommsPresenceHandler,
// CommsRecvHandler) backed by a CommsMessageEmitter and an optional
// CommsPresenceEmitter. All three interfaces are implemented on this single struct
// so the daemon can pass one handler value to RunSocketListenerFull and the socket
// op switch can type-assert to the appropriate sub-interface per op.
//
// cursorStore and eventsJSONLPath are set post-construction via SetRecvDeps
// (commsrecvhandler_nnwaa.go) and enable the comms-recv op (hk-nnwaa T8).
type commsSendHandlerImpl struct {
	emitter     eventbus.CommsMessageEmitter
	presEmitter eventbus.CommsPresenceEmitter // nil when bus does not support presence

	// recv deps — set by SetRecvDeps; nil/empty disables comms-recv.
	cursorStore     *CursorStore
	eventsJSONLPath string
}

// NewCommsSendHandler constructs a CommsSendHandler that emits agent_message
// events via bus. Returns nil if bus does not implement CommsMessageEmitter
// (e.g. a test stub that only implements the base EventBus interface).
//
// When bus also implements CommsPresenceEmitter (as busImpl does), the returned
// handler additionally satisfies CommsPresenceHandler for comms-presence ops (T10).
//
// In production, the daemon passes the real *busImpl which satisfies both
// CommsMessageEmitter (via EmitAgentMessage) and CommsPresenceEmitter (via EmitAgentPresence).
func NewCommsSendHandler(bus eventbus.EventBus) CommsSendHandler {
	ce, ok := bus.(eventbus.CommsMessageEmitter)
	if !ok {
		return nil
	}
	h := &commsSendHandlerImpl{emitter: ce}
	if pe, ok := bus.(eventbus.CommsPresenceEmitter); ok {
		h.presEmitter = pe
	}
	return h
}

// HandleCommsSend validates the request, emits an agent_message event, and
// returns the minted event_id.
//
// Validation rules (agent-comms spec §1.1):
//   - from must be non-empty.
//   - to must be non-empty.
//   - body must be non-empty and ≤ 8 KiB.
//   - in_reply_to, when present, must be non-empty.
//
// On validation failure: returns an error; no event is emitted.
// On success: the event is fsync'd to events.jsonl before this method returns (F-class).
func (h *commsSendHandlerImpl) HandleCommsSend(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req CommsSendRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("comms-send: decode request payload: %w", err)
	}

	// Validate required fields (agent-comms spec §1.1).
	if req.From == "" {
		return nil, fmt.Errorf("comms-send: from is required")
	}
	if req.To == "" {
		return nil, fmt.Errorf("comms-send: to is required")
	}
	if req.Body == "" {
		return nil, fmt.Errorf("comms-send: body is required")
	}
	if len(req.Body) > commsSendBodySizeCap {
		return nil, fmt.Errorf("comms-send: body exceeds 8 KiB cap (%d bytes)", len(req.Body))
	}
	if req.InReplyTo != nil && *req.InReplyTo == "" {
		return nil, fmt.Errorf("comms-send: in_reply_to must be non-empty when present")
	}

	msgPayload := core.AgentMessagePayload{
		From:      req.From,
		To:        req.To,
		Topic:     req.Topic,
		Body:      req.Body,
		InReplyTo: req.InReplyTo,
	}

	eventID, err := h.emitter.EmitAgentMessage(ctx, msgPayload)
	if err != nil {
		return nil, fmt.Errorf("comms-send: emit agent_message: %w", err)
	}

	// Refresh presence for the sender so active agents stay visible in "comms who"
	// without requiring explicit join/refresh beats (hk-6vwi3 fix #2).
	// Thread session_id so the projection can detect two-captains conflicts (hk-z0f02).
	h.emitRefreshBeat(ctx, req.From, req.SessionID)

	result := CommsSendResult{EventID: eventID.String()}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("comms-send: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}
