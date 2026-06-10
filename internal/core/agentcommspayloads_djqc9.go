package core

// agentcommspayloads_djqc9.go — event-bus payload types for agent-comms
// typed events (agent-comms spec §1, bead hk-djqc9):
//
//   - agent_message  (§1.1) — directed/broadcast message between agents
//   - agent_presence (§1.2) — join/refresh/leave presence beat
//
// Both types ride the standard EV-001 envelope (event.go:27).
// agent_message is F-class (fsync-boundary per spec §1.1 "no silent drops" G2).
// agent_presence is O-class (ordinary — losing a refresh beat on crash is harmless).
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §1.
// Bead ref: hk-djqc9.

// AgentPresenceStatus is the typed discriminator for the status field of an
// agent_presence event (agent-comms spec §1.2).
type AgentPresenceStatus string

const (
	// AgentPresenceStatusOnline indicates the agent is online (join or refresh beat).
	AgentPresenceStatusOnline AgentPresenceStatus = "online"

	// AgentPresenceStatusOffline indicates the agent has left (leave beat).
	AgentPresenceStatusOffline AgentPresenceStatus = "offline"
)

// Valid reports whether s is one of the two declared AgentPresenceStatus constants.
func (s AgentPresenceStatus) Valid() bool {
	switch s {
	case AgentPresenceStatusOnline, AgentPresenceStatusOffline:
		return true
	default:
		return false
	}
}

// AgentPresenceReason is the optional provenance label for an agent_presence beat.
type AgentPresenceReason string

const (
	AgentPresenceReasonJoin    AgentPresenceReason = "join"
	AgentPresenceReasonRefresh AgentPresenceReason = "refresh"
	AgentPresenceReasonLeave   AgentPresenceReason = "leave"
)

// Valid reports whether r is one of the three declared AgentPresenceReason constants.
func (r AgentPresenceReason) Valid() bool {
	switch r {
	case AgentPresenceReasonJoin, AgentPresenceReasonRefresh, AgentPresenceReasonLeave:
		return true
	default:
		return false
	}
}

// AgentMessagePayload is the typed event payload for the agent_message event
// (agent-comms spec §1.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — "no silent drops" G2; the event is
// fsync'd to events.jsonl before comms-send returns OK, ensuring durable delivery
// even across an immediate daemon crash per spec §1.1 / busimpl.go fsyncBoundaryEventTypes).
//
// Emitted by the daemon on behalf of an agent via the comms-send socket op
// (agent-comms spec §2.1). The from field is supplied by the CLI caller and is
// NOT authenticated by the daemon (inter-agent auth is a non-goal per spec §non-goals).
//
// # Payload fields (agent-comms spec §1.1)
//
//   - from        — sender's registered presence id (REQUIRED)
//   - to          — directed recipient name OR "*" broadcast (REQUIRED)
//   - topic       — free-text filter key (OPTIONAL)
//   - body        — compact UTF-8 message body (REQUIRED)
//   - in_reply_to — threading hint: event_id of the message being replied to (OPTIONAL)
type AgentMessagePayload struct {
	// From is the sender's registered presence id. Required (non-empty).
	// Supplied by the CLI caller; not authenticated by the daemon.
	From string `json:"from"`

	// To is the directed recipient name or the broadcast sentinel "*". Required (non-empty).
	// A literal agent named "*" is disallowed; "*" means broadcast only.
	To string `json:"to"`

	// Topic is an optional free-text filter key for subscriber-side filtering.
	// Empty string means no topic.
	Topic string `json:"topic,omitempty"`

	// Body is the compact UTF-8 message body. Required (non-empty).
	// The comms-send op enforces a soft size cap (spec §1.1); payloads over
	// the cap are rejected before the event is emitted.
	Body string `json:"body"`

	// InReplyTo is the optional event_id of the message being replied to.
	// Provides a threading hint for consumers; there is no thread-engine in v1.
	// Nil means this is not a reply.
	InReplyTo *string `json:"in_reply_to,omitempty"`
}

// Valid reports whether p is a well-formed AgentMessagePayload.
//
// Rules per agent-comms spec §1.1:
//   - From must be non-empty.
//   - To must be non-empty.
//   - Body must be non-empty.
//   - InReplyTo, when non-nil, must be non-empty.
func (p AgentMessagePayload) Valid() bool {
	if p.From == "" {
		return false
	}
	if p.To == "" {
		return false
	}
	if p.Body == "" {
		return false
	}
	if p.InReplyTo != nil && *p.InReplyTo == "" {
		return false
	}
	return true
}

// AgentPresencePayload is the typed event payload for the agent_presence event
// (agent-comms spec §1.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — losing the last refresh beat on a crash is harmless;
// the TTL projection reconciles it per spec §4 / Q2).
//
// Emitted by the daemon on behalf of an agent via comms join/leave ops, or
// automatically as a refresh beat during a long-lived recv --follow / subscribe
// session (agent-comms spec §2.5). The presence registry (spec §4) is a pure
// projection over these events.
//
// # Payload fields (agent-comms spec §1.2)
//
//   - agent      — the agent's registered presence id (REQUIRED)
//   - status     — "online" or "offline" (REQUIRED)
//   - last_seen  — RFC 3339 wall-clock timestamp of this beat (REQUIRED)
//   - reason     — provenance: "join", "refresh", or "leave" (OPTIONAL)
//   - session_id — opaque per-session token; enables two-captains conflict
//     detection (OPTIONAL; hk-z0f02)
type AgentPresencePayload struct {
	// Agent is the agent's registered presence id. Required (non-empty).
	Agent string `json:"agent"`

	// Status is the presence phase at this beat. Required; must be a valid
	// AgentPresenceStatus constant ("online" or "offline").
	Status AgentPresenceStatus `json:"status"`

	// LastSeen is the RFC 3339 wall-clock timestamp of this beat. Required (non-empty).
	LastSeen string `json:"last_seen"`

	// Reason is the optional provenance of the beat: "join", "refresh", or "leave".
	// Empty when the reason is not specified. When non-empty, must be a valid
	// AgentPresenceReason constant.
	Reason AgentPresenceReason `json:"reason,omitempty"`

	// SessionID is an optional opaque token that identifies the specific process
	// or Claude session that emitted this beat. Sourced from $HARMONIK_SESSION_ID
	// when available. The presence projection tracks this field so the CLI can
	// warn when two distinct sessions simultaneously claim the same agent name
	// (two-captains conflict detection, hk-z0f02).
	SessionID string `json:"session_id,omitempty"`
}

// Valid reports whether p is a well-formed AgentPresencePayload.
//
// Rules per agent-comms spec §1.2:
//   - Agent must be non-empty.
//   - Status must be a valid AgentPresenceStatus constant.
//   - LastSeen must be non-empty.
//   - Reason, when non-empty, must be a valid AgentPresenceReason constant.
func (p AgentPresencePayload) Valid() bool {
	if p.Agent == "" {
		return false
	}
	if !p.Status.Valid() {
		return false
	}
	if p.LastSeen == "" {
		return false
	}
	if p.Reason != "" && !p.Reason.Valid() {
		return false
	}
	return true
}
