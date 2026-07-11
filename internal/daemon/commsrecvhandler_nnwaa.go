package daemon

// commsrecvhandler_nnwaa.go — CommsRecvHandler interface and implementation for
// the comms-recv socket op (agent-comms spec §2.2 C2/C5, bead hk-nnwaa T8).
//
// The handler reads agent_message events from the calling agent's durable cursor
// (CursorStore, T7) forward, filters via the SHARED MatchAgentMessage predicate
// (N1, R1 — must not duplicate the filter), advances the cursor after delivery,
// and returns the matched messages in the SocketResponse.
//
// # At-least-once delivery (N3)
//
// The cursor advances AFTER the scan returns all matched messages. If the daemon
// crashes between scan and advance, the same events are re-delivered on the next
// call. Recipients deduplicate on event_id at the application layer.
//
// # Durability on daemon restart
//
// The cursor is written by CursorStore.Advance with temp+rename+fsync discipline
// (T7 contract). A daemon restart reads the cursor from disk and resumes from
// the stored position — no messages are lost.
//
// # Decoupled poll/live cursors (hk-8xspi, B1)
//
// A plain one-shot `comms recv --agent` poll and a `--follow`/`--wait` live
// session each own an INDEPENDENT durable cursor (CommsRecvRequest.Live selects
// which one this call reads/advances). Before this change both paths shared one
// cursor, so a poller and a follow/wait watcher raced over the same position and
// one would starve the other (recv-drains-0-under-follow). N3 at-least-once +
// mandatory dedupe-on-event_id makes the resulting duplicate delivery across the
// two cursors harmless — see agent-comms spec §5 Q1/Q3.
//
// # Shared predicate (R1 / N1)
//
// comms-recv uses the same MatchAgentMessage predicate as the live subscribe
// path (subscriptionStream.offer in subscribe.go) and the JSONL replay path
// (HandleSubscribe ScanAfter loop in subscribe.go). There is exactly one copy of
// the addressing logic: agent_message.go:MatchAgentMessage.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.2 C2/C5.
// Bead ref: hk-nnwaa (T8), hk-8xspi (B1 decoupled cursor).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// CommsRecvHandler is the interface the daemon registers to process comms-recv
// socket ops. A nil CommsRecvHandler causes comms-recv ops to return an error
// response.
//
// Spec ref: agent-comms spec §2.2 C2/C5.
// Bead ref: hk-nnwaa.
type CommsRecvHandler interface {
	// HandleCommsRecv processes one comms-recv payload (the "payload" field of
	// the SocketRequest). Returns the JSON-encoded CommsRecvResult on success.
	HandleCommsRecv(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// CommsRecvRequest is the wire payload for a "comms-recv" socket op.
// Spec ref: agent-comms spec §2.2 C2/C5.
type CommsRecvRequest struct {
	// Agent is the calling agent's name — used as the cursor key and as the
	// "to" filter (deliver messages directed to Agent or broadcast). REQUIRED.
	Agent string `json:"agent"`

	// From is an optional sender filter. Empty = match any sender.
	From string `json:"from,omitempty"`

	// Topic is an optional topic filter. Empty = match any topic.
	Topic string `json:"topic,omitempty"`

	// Live selects which of the agent's two independent durable cursors this
	// call reads/advances (hk-8xspi, B1). false (default) = the plain poll
	// cursor used by a bare `comms recv --agent`. true = the live cursor shared
	// with SubscribeHub, used for the catch-up drain that precedes a
	// `--follow`/`--wait` subscribe session. The two cursors never interact:
	// draining one never advances the other.
	Live bool `json:"live,omitempty"`
}

// CommsRecvMessage is one agent_message event returned by comms-recv.
// Fields mirror AgentMessagePayload (agent_message.go) plus the envelope
// metadata needed for at-least-once dedup (event_id) and ordering (ts).
type CommsRecvMessage struct {
	// EventID is the UUIDv7 event_id of the agent_message event.
	// Recipients use this for deduplication (N3).
	EventID string `json:"event_id"`

	// From is the sender's presence id.
	From string `json:"from"`

	// To is the directed recipient name or "*" (broadcast).
	To string `json:"to"`

	// Topic is the optional filter key (omitted when empty).
	Topic string `json:"topic,omitempty"`

	// Body is the message body.
	Body string `json:"body"`

	// InReplyTo is the event_id of the message being replied to (omitted when absent).
	InReplyTo string `json:"in_reply_to,omitempty"`

	// Ts is the RFC 3339 wall-clock timestamp of the event.
	Ts string `json:"ts"`
}

// CommsRecvResult is the SocketResponse.Result payload for a successful comms-recv op.
type CommsRecvResult struct {
	// Messages contains the unread agent_message events since the caller's cursor.
	// Empty slice (not null) when there are no new messages.
	Messages []CommsRecvMessage `json:"messages"`

	// CursorAfter is the agent's cursor position after this op: the last delivered
	// event_id (if any messages were returned), otherwise the previously-stored cursor.
	// Empty string when no cursor has ever been stored and the backlog was empty.
	// Clients use this as the since-event-id anchor for a follow-mode subscribe.
	CursorAfter string `json:"cursor_after,omitempty"`

	// ScanAnchor is the event_id of the last event scanned during this drain,
	// regardless of whether it matched the agent filter. Empty only when the
	// events.jsonl was completely empty (no events at all).
	//
	// Purpose (GH #8 / hk-7xvf): when CursorAfter is "" (no matching messages
	// and no stored cursor), the CLI --follow path passes ScanAnchor as
	// since_event_id so HandleSubscribe runs the replay path from ScanAnchor
	// forward. This covers any messages that arrived in the gap between the
	// drain completing and the subscribe subscriber registering on the hub.
	ScanAnchor string `json:"scan_anchor,omitempty"`
}

// SetRecvDeps wires the two independent cursor stores and the events JSONL
// path into the handler so comms-recv ops can scan durable history and advance
// the agent cursor (hk-8xspi, B1: poll and live are separate stores — see
// commsSendHandlerImpl doc). liveStore is also the store passed to
// SubscribeHub.SetCommsCursorStore so a --follow/--wait session's catch-up
// drain and its live tail share one continuous cursor.
//
// Called by the daemon after NewCommsSendHandler when ProjectDir is known.
// A nil pollStore/liveStore or empty eventsPath causes comms-recv to return an
// error response.
func (h *commsSendHandlerImpl) SetRecvDeps(pollStore, liveStore *CursorStore, eventsJSONLPath string) {
	h.pollCursorStore = pollStore
	h.liveCursorStore = liveStore
	h.eventsJSONLPath = eventsJSONLPath
}

// HandleCommsRecv reads unread agent_message events for the calling agent,
// advances its cursor, and returns the results.
//
// Algorithm:
//  1. Validate request.
//  2. Read cursor for agent (empty = beginning of log).
//  3. ScanAfter(eventsJSONLPath, cursor) → filter agent_message events via
//     MatchAgentMessage(payload, agent, from, topic) — to==agent means
//     "directed to me or broadcast".
//  4. Collect matched messages.
//  5. If any messages found, Advance cursor to last event_id (N3: after read).
//  6. Return CommsRecvResult.
func (h *commsSendHandlerImpl) HandleCommsRecv(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if h.pollCursorStore == nil || h.liveCursorStore == nil {
		return nil, fmt.Errorf("comms-recv: CursorStore not configured")
	}
	if h.eventsJSONLPath == "" {
		return nil, fmt.Errorf("comms-recv: events JSONL path not configured")
	}

	var req CommsRecvRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("comms-recv: decode request payload: %w", err)
	}
	if req.Agent == "" {
		return nil, fmt.Errorf("comms-recv: agent is required")
	}

	// hk-8xspi (B1): route to the poll or live cursor store per req.Live. The two
	// stores are independent — draining one never advances the other.
	cursorStore := h.pollCursorStore
	if req.Live {
		cursorStore = h.liveCursorStore
	}

	// Serialize the Get→scan→Advance critical section per agent (hk-fww4e).
	// Two concurrent "comms recv --agent X" calls on separate connections would
	// otherwise both Get the same cursor, scan the same backlog, and both Advance —
	// causing bounded duplicate delivery. The per-agent mutex in CursorStore
	// prevents this without blocking concurrent ops for different agents.
	agentMu := cursorStore.AgentMu(req.Agent)
	agentMu.Lock()
	defer agentMu.Unlock()

	// Read the durable cursor; "" means start of log (deliver all matching events).
	cursorStr, err := cursorStore.Get(req.Agent)
	if err != nil {
		return nil, fmt.Errorf("comms-recv: read cursor for %q: %w", req.Agent, err)
	}

	// Convert cursor string to EventID for ScanAfter.
	var sinceID core.EventID
	if cursorStr != "" {
		parsed, parseErr := uuid.Parse(cursorStr)
		if parseErr != nil {
			return nil, fmt.Errorf("comms-recv: malformed cursor %q for agent %q: %w", cursorStr, req.Agent, parseErr)
		}
		sinceID = core.EventID(parsed)
	}

	// Scan events.jsonl forward from the cursor, filter, collect.
	var messages []CommsRecvMessage
	var lastEventID string
	var lastScannedID string // last event_id seen regardless of match (for ScanAnchor)

	for evt := range eventbus.ScanAfter(h.eventsJSONLPath, sinceID) {
		lastScannedID = evt.EventID.String()
		if evt.Type != "agent_message" {
			continue
		}
		var p AgentMessagePayload
		if unmarshalErr := json.Unmarshal(evt.Payload, &p); unmarshalErr != nil {
			continue
		}
		// R1: use the SHARED MatchAgentMessage predicate (agent_message.go).
		// to=req.Agent means "directed to me OR broadcast *".
		if !MatchAgentMessage(p, req.Agent, req.From, req.Topic) {
			continue
		}
		messages = append(messages, CommsRecvMessage{
			EventID:   evt.EventID.String(),
			From:      p.From,
			To:        p.To,
			Topic:     p.Topic,
			Body:      p.Body,
			InReplyTo: p.InReplyTo,
			Ts:        evt.TimestampWall.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
		lastEventID = evt.EventID.String()
	}

	// N3: advance cursor AFTER read so a crash between scan and advance
	// causes re-delivery rather than loss.
	if lastEventID != "" {
		if advErr := cursorStore.Advance(req.Agent, lastEventID); advErr != nil {
			return nil, fmt.Errorf("comms-recv: advance cursor for %q: %w", req.Agent, advErr)
		}
	}

	// Refresh presence for the receiving agent so receive-only agents stay
	// visible in "comms who" (hk-6vwi3 fix #2). No session_id for recv beats —
	// comms-recv requests do not carry a session token.
	h.emitRefreshBeat(ctx, req.Agent, "")

	if messages == nil {
		messages = []CommsRecvMessage{}
	}
	// cursor_after: new cursor position (for --follow anchor).
	// If we advanced the cursor, use lastEventID; otherwise use the stored cursor.
	cursorAfter := lastEventID
	if cursorAfter == "" {
		cursorAfter = cursorStr
	}
	result := CommsRecvResult{Messages: messages, CursorAfter: cursorAfter, ScanAnchor: lastScannedID}
	resultBytes, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("comms-recv: marshal result: %w", marshalErr)
	}
	return resultBytes, nil
}
