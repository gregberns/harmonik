package daemon

// commspresencehandler_7t27s.go — CommsPresenceHandler interface and implementation
// for the comms-presence socket op (agent-comms spec §2.5 C6, bead hk-7t27s T10).
//
// The handler validates a comms-presence request, stamps last_seen with wall time,
// emits an agent_presence event via the event bus (O-class: ordinary durability,
// not fsync-boundary), and returns the minted event_id in the SocketResponse.
//
// CommsPresenceHandler is a separate interface from CommsSendHandler but is
// implemented on the same *commsSendHandlerImpl so the daemon passes one handler
// value to RunSocketListenerFull; socket.go type-asserts ch.(CommsPresenceHandler)
// when processing comms-presence ops.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.5, §4.
// Bead ref: hk-7t27s.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// emitRefreshBeat emits an agent_presence{online, reason:"refresh"} beat for agent
// via h.presEmitter. Used by HandleCommsSend and HandleCommsRecv to wire the
// dead AgentPresenceReasonRefresh path (hk-6vwi3 fix #2): an agent stays visible
// in "comms who" as long as it is actively sending or receiving messages even when
// it does not emit explicit join/refresh beats.
//
// sessionID propagates the caller's per-session token (from $HARMONIK_SESSION_ID)
// so the presence projection can detect two-captains conflicts (hk-z0f02). Pass ""
// when no session token is available (e.g. comms-recv refresh beats).
//
// Errors are suppressed — a dropped refresh beat is harmless (O-class durability).
func (h *commsSendHandlerImpl) emitRefreshBeat(ctx context.Context, agent, sessionID string) {
	if h.presEmitter == nil || agent == "" {
		return
	}
	_, _ = h.presEmitter.EmitAgentPresence(ctx, core.AgentPresencePayload{
		Agent:     agent,
		Status:    core.AgentPresenceStatusOnline,
		LastSeen:  time.Now().UTC().Format(time.RFC3339),
		Reason:    core.AgentPresenceReasonRefresh,
		SessionID: sessionID,
	})
}

// CommsPresenceHandler is the interface for processing comms-presence socket ops.
// Implemented by *commsSendHandlerImpl (commshandler_nbrmf.go) which also
// satisfies CommsSendHandler — both ride the same handler value in the daemon.
//
// Spec ref: agent-comms spec §2.5 C6.
// Bead ref: hk-7t27s.
type CommsPresenceHandler interface {
	// HandleCommsPresence processes one comms-presence payload (the "payload" field
	// of the SocketRequest). Returns the JSON-encoded CommsPresenceResult on success.
	HandleCommsPresence(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// CommsPresenceRequest is the wire payload for a "comms-presence" socket op.
// It maps to the agent_presence event fields (agent-comms spec §1.2).
// last_seen is NOT included in the request — the handler stamps it with wall time.
type CommsPresenceRequest struct {
	// Agent is the agent's registered presence id. REQUIRED.
	Agent string `json:"agent"`

	// Status is "online" or "offline". REQUIRED.
	Status core.AgentPresenceStatus `json:"status"`

	// Reason is "join", "refresh", or "leave". OPTIONAL.
	Reason core.AgentPresenceReason `json:"reason,omitempty"`

	// SessionID is an optional opaque per-session token (from $HARMONIK_SESSION_ID).
	// Forwarded into the agent_presence event for two-captains conflict detection (hk-z0f02).
	SessionID string `json:"session_id,omitempty"`
}

// CommsPresenceResult is the SocketResponse.Result payload for a successful comms-presence op.
type CommsPresenceResult struct {
	// EventID is the UUIDv7 event_id of the minted agent_presence event.
	EventID string `json:"event_id"`
}

// HandleCommsPresence validates the request, stamps last_seen, emits an
// agent_presence event, and returns the minted event_id.
//
// Validation rules (agent-comms spec §1.2):
//   - agent must be non-empty.
//   - status must be "online" or "offline".
//   - reason, when non-empty, must be "join", "refresh", or "leave".
//
// On validation failure: returns an error; no event is emitted.
// agent_presence is O-class (ordinary durability); the JSONL append is not fsynced.
func (h *commsSendHandlerImpl) HandleCommsPresence(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.presEmitter == nil {
		return nil, fmt.Errorf("comms-presence: CommsPresenceEmitter not available")
	}

	var req CommsPresenceRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("comms-presence: decode request payload: %w", err)
	}

	if req.Agent == "" {
		return nil, fmt.Errorf("comms-presence: agent is required")
	}
	if !req.Status.Valid() {
		return nil, fmt.Errorf("comms-presence: status %q must be \"online\" or \"offline\"", req.Status)
	}
	if req.Reason != "" && !req.Reason.Valid() {
		return nil, fmt.Errorf("comms-presence: reason %q must be \"join\", \"refresh\", or \"leave\"", req.Reason)
	}

	presPayload := core.AgentPresencePayload{
		Agent:     req.Agent,
		Status:    req.Status,
		LastSeen:  time.Now().UTC().Format(time.RFC3339),
		Reason:    req.Reason,
		SessionID: req.SessionID,
	}

	eventID, err := h.presEmitter.EmitAgentPresence(ctx, presPayload)
	if err != nil {
		return nil, fmt.Errorf("comms-presence: emit agent_presence: %w", err)
	}

	result := CommsPresenceResult{EventID: eventID.String()}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("comms-presence: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}
