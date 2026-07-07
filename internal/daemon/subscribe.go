package daemon

// subscribe.go — "subscribe" socket op (hk-6ynv4).
//
// Long-running socket op: a client connects, sends a single JSON request
// describing the event-type filter and heartbeat cadence, and then receives
// an NDJSON stream of envelopes on the same connection until the client
// closes or the daemon stops.
//
// Replaces the brittle "tail .harmonik/events/events.jsonl" pattern with a
// first-class subscriber interface.
//
// # Request shape
//
//	{
//	  "op": "subscribe",
//	  "types": ["run_completed","run_failed", ...],
//	  "since_event_id": "",
//	  "heartbeat_seconds": 60
//	}
//
// An empty or missing "types" array subscribes to ALL event types (wildcard).
// "heartbeat_seconds" is clamped to [10, 600] inclusive; default 60.
//
// # Output
//
// Each matched event becomes one NDJSON line carrying the full core.Event
// envelope (event_id, type, payload, run_id, ...) as defined in
// internal/core/event.go. Two additional connection-only event kinds are
// emitted directly to the subscriber and are NOT bus-published:
//
//	{"type":"heartbeat","ts":"...","active_runs":[...],"last_event_id":"..."}
//	{"type":"subscription_gap","dropped":N}
//
// # Back-pressure (EV-012 observer-class invariant)
//
// The bus dispatches to this consumer via the observer class. The handler
// performs a non-blocking send into a 256-slot buffered channel. If the
// channel is full (slow client), the OLDEST queued event is discarded and a
// drop counter is incremented. On the next successful send to the socket a
// subscription_gap line is emitted carrying the accumulated drop count, then
// the counter resets. The bus's emission goroutine is NEVER blocked by a slow
// subscriber.
//
// # Heartbeat
//
// A timer fires every heartbeat_seconds. On fire, an active-runs snapshot is
// taken from RunRegistry and a heartbeat line is written to the connection.
// Heartbeats are subscription-only — they do NOT pollute events.jsonl.
//
// # Lifecycle
//
// - subscribe() returns when the client disconnects (write error), the daemon
//   context is cancelled, or the connection's read side closes.
// - The bus subscription is best-effort transient: it must be registered
//   BEFORE bus.Seal at daemon startup (EV-009). Because subscribers connect
//   AFTER seal, we register a single long-lived hub-style observer at startup
//   that fans matched events to per-connection channels (registered/removed
//   on the fly).
//
// Bead ref: hk-6ynv4.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// SubscribeHandler is the interface the socket dispatch invokes for a
// "subscribe" op. A nil SubscribeHandler causes the socket to reply with
// an error response.
type SubscribeHandler interface {
	// HandleSubscribe runs the subscribe protocol on conn until the client
	// disconnects or ctx is cancelled. The handler MUST NOT close conn —
	// the socket dispatcher will close it after HandleSubscribe returns.
	HandleSubscribe(ctx context.Context, conn net.Conn, req SubscribeRequest)
}

// SubscribeRequest is the decoded body of a "subscribe" op request.
type SubscribeRequest struct {
	// Types restricts delivery to the listed event types. Empty/nil = all types.
	Types []string `json:"types,omitempty"`

	// SinceEventID enables replay-from-cursor. When non-empty, the daemon
	// replays all events from events.jsonl with event_id strictly after this
	// value (subject to the Types filter), then attaches to the live stream.
	// The value must be a valid UUID string; the socket layer validates the
	// format and returns an error response if malformed. Implemented: hk-a5sil.
	SinceEventID string `json:"since_event_id,omitempty"`

	// HeartbeatSeconds is the idle heartbeat cadence. Clamped to [10, 600];
	// zero or negative defaults to 60.
	HeartbeatSeconds int `json:"heartbeat_seconds,omitempty"`

	// Agent-message addressing filters (agent-comms spec §3 / N1). Applied via
	// MatchAgentMessage on BOTH the replay path and the live offer path. Empty
	// values are wildcards — non-agent_message events always bypass these.
	To    string `json:"to,omitempty"`
	From  string `json:"from,omitempty"`
	Topic string `json:"topic,omitempty"`
}

// subscribeHeartbeatMin / Max / Default define the heartbeat-clamp range.
const (
	subscribeHeartbeatMin     = 10
	subscribeHeartbeatMax     = 600
	subscribeHeartbeatDefault = 60

	// subscribeChannelCapacity is the per-subscriber buffered-channel depth.
	// On overflow the OLDEST event is dropped (see drop-oldest discipline).
	subscribeChannelCapacity = 256
)

// commsCursorFlushInterval bounds how often a `comms recv --follow` subscribe
// session fsyncs the agent's durable comms cursor (hk-tafd4). Advancing per
// delivered event would fsync on every message; batching every few seconds
// bounds the IO while keeping the replay-on-restart window small. The cursor is
// also flushed once on session return so the final delivered event is durable.
const commsCursorFlushInterval = 2 * time.Second

// ActiveRunsSource is the minimal RunRegistry surface that subscribeHub
// needs to render the heartbeat active_runs snapshot. *RunRegistry satisfies
// this interface via Snapshot().
type ActiveRunsSource interface {
	Snapshot() []*RunHandle
}

// subscribeMaxConnectionsDefault is the default per-process connection cap
// applied when SubscribeHubConfig.MaxConnections is zero. A daemon is
// single-user (0600 socket), so 32 concurrent subscribers is a generous
// ceiling that still bounds memory consumption (32 × 256 × core.Event).
const subscribeMaxConnectionsDefault = 32

// NewTimerFn is a factory that creates a timer for a given duration.
// It returns a receive-only channel that fires when the timer expires,
// a stop function (mirrors time.Timer.Stop), and a reset function
// (mirrors time.Timer.Reset). Tests inject a fake via SubscribeHubConfig.NewTimer.
type NewTimerFn func(d time.Duration) (c <-chan time.Time, stop func() bool, reset func(time.Duration))

// realNewTimer wraps time.NewTimer to satisfy NewTimerFn.
func realNewTimer(d time.Duration) (<-chan time.Time, func() bool, func(time.Duration)) {
	t := time.NewTimer(d)
	return t.C, t.Stop, func(d time.Duration) { t.Reset(d) }
}

// SubscribeHubConfig wires bus + active-runs source into a SubscribeHub.
type SubscribeHubConfig struct {
	// Bus is the event bus. Required.
	Bus eventbus.EventBus

	// ActiveRuns is the source of active-run metadata for heartbeats.
	// May be nil; the heartbeat payload's active_runs field will then be empty.
	ActiveRuns ActiveRunsSource

	// MaxConnections caps the number of concurrent subscribe connections. When
	// zero, subscribeMaxConnectionsDefault (32) is used. A new subscribe
	// request that would exceed the cap is rejected immediately with a
	// "subscribe_capacity_exceeded" error written to the connection.
	MaxConnections int

	// EventsJSONLPath is the absolute path to the events.jsonl log file used
	// for since_event_id replay (hk-a5sil). When empty, replay is skipped and
	// a subscribe request with since_event_id proceeds as a live-only stream.
	// Production wiring supplies cfg.JSONLLogPath from DaemonConfig.
	EventsJSONLPath string

	// PresenceEmitter, when non-nil, is used to emit refresh beats when an agent
	// starts a subscribe session with a non-empty To field (hk-6vwi3 fix #2).
	// Optional; without it subscribe connections do not refresh presence.
	PresenceEmitter eventbus.CommsPresenceEmitter

	// Now is the wall-clock function. Defaults to time.Now when nil.
	// Tests override this; production wiring leaves it nil.
	Now func() time.Time

	// NewTimer is the timer factory used for heartbeat ticks.
	// Defaults to realNewTimer (wrapping time.NewTimer) when nil.
	// Tests inject a fake to avoid real-time waits and CI flakiness.
	NewTimer NewTimerFn
}

// SubscribeHub is the long-lived bus consumer that fans matched events out
// to per-connection subscriptionStream channels.
//
// Must be constructed and Subscribed BEFORE bus.Seal (EV-009). New subscriber
// connections may register/unregister after seal — only the bus subscription
// itself is sealed.
type SubscribeHub struct {
	cfg SubscribeHubConfig

	mu          sync.RWMutex
	subscribers map[*subscriptionStream]struct{}
	lastEventID atomic.Value // string; last successfully fanned-out event_id

	// connCount tracks the number of active HandleSubscribe goroutines. It is
	// incremented after the capacity check passes and decremented when the
	// goroutine returns. Used to enforce cfg.MaxConnections.
	connCount atomic.Int64

	// cursorStore, when non-nil, lets a `comms recv --follow` subscribe session
	// advance the calling agent's durable comms cursor as agent_message events
	// are delivered (hk-tafd4). Without this, a --follow watcher that restarts
	// replays everything since its initial drain because the cursor only moves on
	// a one-shot `comms recv`. Set post-construction via SetCommsCursorStore so
	// the hub and the comms-recv handler share ONE cursor store (no parallel
	// cursor). nil = cursor advancement disabled (subscribe behaves as before).
	cursorStore *CursorStore

	closed atomic.Bool
}

// SetCommsCursorStore wires the per-agent comms cursor store into the hub so a
// `comms recv --follow` subscribe session advances the agent's durable cursor as
// agent_message events are delivered (hk-tafd4). Pass the SAME *CursorStore the
// comms-recv handler uses (see daemon wiring) so both paths share one cursor.
// A nil store leaves cursor advancement disabled.
func (h *SubscribeHub) SetCommsCursorStore(store *CursorStore) {
	h.cursorStore = store
}

// NewSubscribeHub returns a hub bound to cfg. Bus is required.
func NewSubscribeHub(cfg SubscribeHubConfig) *SubscribeHub {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewTimer == nil {
		cfg.NewTimer = realNewTimer
	}
	h := &SubscribeHub{
		cfg:         cfg,
		subscribers: make(map[*subscriptionStream]struct{}),
	}
	h.lastEventID.Store("")
	return h
}

// Subscribe registers the hub as a wildcard observer on the bus.
// MUST be called before bus.Seal.
func (h *SubscribeHub) Subscribe(bus eventbus.EventBus) error {
	sub := core.Subscription{
		ConsumerID:    "subscribe-hub",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       h.dispatch,
	}
	if _, err := bus.Subscribe(sub); err != nil {
		return fmt.Errorf("SubscribeHub.Subscribe: %w", err)
	}
	return nil
}

// dispatch is the observer-class handler invoked by the bus for every event.
// It MUST NOT block (EV-012); the per-subscriber send is non-blocking with
// drop-oldest discipline.
func (h *SubscribeHub) dispatch(_ context.Context, evt core.Event) error {
	h.lastEventID.Store(evt.EventID.String())
	h.mu.RLock()
	subs := make([]*subscriptionStream, 0, len(h.subscribers))
	for s := range h.subscribers {
		subs = append(subs, s)
	}
	h.mu.RUnlock()
	for _, s := range subs {
		s.offer(evt)
	}
	return nil
}

// HandleSubscribe implements SubscribeHandler. It blocks until the client
// disconnects or ctx is cancelled.
//
// When req.SinceEventID is non-empty, historical events are replayed from
// events.jsonl (events strictly after since_event_id, subject to the type
// filter) before the live stream begins. The subscriber channel is registered
// on the hub BEFORE replay starts so no live events are lost during the replay
// window. Events arriving on the live channel during replay that were already
// sent via JSONL replay are silently deduplicated using a high-water-mark
// comparison on event_id (UUIDv7 byte order = chronological order, EV-002).
//
// Bead ref: hk-a5sil.
func (h *SubscribeHub) HandleSubscribe(ctx context.Context, conn net.Conn, req SubscribeRequest) {
	// Enforce per-process connection cap. Use a compare-and-increment loop so
	// two concurrent callers can't both pass the check and both exceed the cap.
	maxConn := int64(h.cfg.MaxConnections)
	if maxConn <= 0 {
		maxConn = subscribeMaxConnectionsDefault
	}
	for {
		cur := h.connCount.Load()
		if cur >= maxConn {
			writeSubscribeError(conn, "subscribe_capacity_exceeded")
			return
		}
		if h.connCount.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	defer h.connCount.Add(-1)

	// Build the type filter.
	typeFilter := make(map[string]struct{}, len(req.Types))
	for _, t := range req.Types {
		if t != "" {
			typeFilter[t] = struct{}{}
		}
	}
	wildcard := len(typeFilter) == 0

	// Clamp heartbeat.
	hb := req.HeartbeatSeconds
	if hb <= 0 {
		hb = subscribeHeartbeatDefault
	}
	if hb < subscribeHeartbeatMin {
		hb = subscribeHeartbeatMin
	}
	if hb > subscribeHeartbeatMax {
		hb = subscribeHeartbeatMax
	}
	heartbeatInterval := time.Duration(hb) * time.Second

	s := &subscriptionStream{
		ch:         make(chan core.Event, subscribeChannelCapacity),
		typeFilter: typeFilter,
		wildcard:   wildcard,
		to:         req.To,
		from:       req.From,
		topic:      req.Topic,
	}

	// Register BEFORE replay so live events are buffered while we replay from
	// JSONL. Deregister on return so the bus stops fanning out.
	h.mu.Lock()
	h.subscribers[s] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, s)
		h.mu.Unlock()
	}()

	// Emit a refresh beat for the subscribing agent (hk-6vwi3 fix #2): a receive-only
	// agent that opens a subscribe session stays visible in "comms who" even if it
	// never calls comms-send. Best-effort: errors are silently dropped (O-class).
	if h.cfg.PresenceEmitter != nil && req.To != "" {
		_, _ = h.cfg.PresenceEmitter.EmitAgentPresence(ctx, core.AgentPresencePayload{
			Agent:    req.To,
			Status:   core.AgentPresenceStatusOnline,
			LastSeen: h.cfg.Now().UTC().Format(time.RFC3339),
			Reason:   core.AgentPresenceReasonRefresh,
		})
	}

	// Detect client-side close: a goroutine reads from the conn and signals
	// via cancellation. Any read error (EOF, RST, deadline) cancels.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				cancel()
				return
			}
		}
	}()

	enc := json.NewEncoder(conn)

	// Comms-cursor advancement for `comms recv --follow` (hk-tafd4). When the hub
	// has a cursor store and the request carries an agent name (req.To), we
	// advance that agent's durable cursor as agent_message events directed to it
	// are delivered, so a watcher restart does NOT replay already-delivered
	// messages. To bound fsync churn we track the last-delivered event_id and
	// flush it to the cursor at most every cursorFlushInterval (and once more on
	// return). Advancing AFTER delivery preserves at-least-once (N3): a crash
	// between deliver and flush re-delivers; clients dedup on event_id.
	//
	// B1 pin (hk-d65rb): a one-shot comms recv called AFTER a --follow session
	// has advanced the cursor correctly drains 0 messages — not a bug. The shared
	// cursor is the single source of truth for "what this agent has seen."
	// Operators can audit the full message history cursor-independently via
	// `comms log --since`, which scans events.jsonl without consulting the cursor.
	cursorAdvanceEnabled := h.cursorStore != nil && req.To != ""
	var pendingCursorID string // last agent_message event_id delivered but not yet flushed
	flushCursor := func() {
		if !cursorAdvanceEnabled || pendingCursorID == "" {
			return
		}
		// Best-effort: a failed flush is non-fatal (at-least-once tolerates it).
		// Serialize against concurrent one-shot comms-recv on the same agent.
		agentMu := h.cursorStore.AgentMu(req.To)
		agentMu.Lock()
		_ = h.cursorStore.Advance(req.To, pendingCursorID)
		agentMu.Unlock()
		pendingCursorID = ""
	}
	defer flushCursor()
	// Only arm the cursor-flush timer when advancement is enabled. When it is
	// not, leave cursorFlushC nil so the select branch below never fires and the
	// timer factory (which tests inspect for the FIRST call = heartbeat interval)
	// is not invoked for a flush timer at all.
	var cursorFlushC <-chan time.Time
	cursorFlushStop := func() bool { return true }
	cursorFlushReset := func(time.Duration) {}
	if cursorAdvanceEnabled {
		cursorFlushC, cursorFlushStop, cursorFlushReset = h.cfg.NewTimer(commsCursorFlushInterval)
	}
	defer cursorFlushStop()

	var lastReplayedUID [16]byte

	if req.SinceEventID != "" && h.cfg.EventsJSONLPath != "" {
		if sinceUUID, parseErr := uuid.Parse(req.SinceEventID); parseErr == nil {
			sinceID := core.EventID(sinceUUID)
			for evt := range eventbus.ScanAfter(h.cfg.EventsJSONLPath, sinceID) {
				select {
				case <-streamCtx.Done():
					return
				default:
				}
				if !wildcard {
					if _, ok := typeFilter[evt.Type]; !ok {
						continue
					}
				}
				// Agent-message addressing filter (N1). Applies on agent_message
				// events only; other event types bypass this block.
				if evt.Type == "agent_message" && (req.To != "" || req.From != "" || req.Topic != "") {
					var p AgentMessagePayload
					if unmarshalErr := json.Unmarshal(evt.Payload, &p); unmarshalErr != nil || !MatchAgentMessage(p, req.To, req.From, req.Topic) {
						continue
					}
				}
				if encErr := enc.Encode(evt); encErr != nil {
					return
				}
				lastReplayedUID = [16]byte(evt.EventID)
				// Advance the comms cursor over replayed agent_message events too:
				// the replay window covers messages this agent has now seen.
				if cursorAdvanceEnabled && evt.Type == "agent_message" {
					pendingCursorID = evt.EventID.String()
				}
			}
			// Persist progress made during replay before entering the live loop.
			flushCursor()
		}
	}

	hbC, hbStop, hbReset := h.cfg.NewTimer(heartbeatInterval)
	defer hbStop()

	for {
		select {
		case <-streamCtx.Done():
			return

		case evt := <-s.ch:
			// Deduplicate events already sent during JSONL replay.
			// UUIDv7 byte comparison: if this event's id ≤ the last replayed
			// id it was covered by the replay window (EV-002).
			if lastReplayedUID != ([16]byte{}) {
				evtUID := [16]byte(evt.EventID)
				if bytes.Compare(evtUID[:], lastReplayedUID[:]) <= 0 {
					continue
				}
			}

			// Drop-gap notice first if we accumulated drops.
			if dropped := s.swapDropped(); dropped > 0 {
				if err := enc.Encode(subscriptionGapLine{
					Type:    "subscription_gap",
					Dropped: dropped,
				}); err != nil {
					return
				}
			}
			if err := enc.Encode(evt); err != nil {
				return
			}
			// hk-tafd4: record this delivered agent_message so the cursor advances
			// past it. Flush is batched on the cursorFlushC tick (and on return).
			if cursorAdvanceEnabled && evt.Type == "agent_message" {
				pendingCursorID = evt.EventID.String()
			}
			// Reset heartbeat — we just wrote, so the line is fresh.
			if !hbStop() {
				select {
				case <-hbC:
				default:
				}
			}
			hbReset(heartbeatInterval)

		case <-cursorFlushC:
			// Batched durable cursor advance for --follow (hk-tafd4).
			flushCursor()
			cursorFlushReset(commsCursorFlushInterval)

		case <-hbC:
			payload := h.makeHeartbeat()
			if err := enc.Encode(payload); err != nil {
				return
			}
			hbReset(heartbeatInterval)
		}
	}
}

// makeHeartbeat snapshots active-run metadata and packages a heartbeat line.
func (h *SubscribeHub) makeHeartbeat() heartbeatLine {
	out := heartbeatLine{
		Type:        "heartbeat",
		Timestamp:   h.cfg.Now().UTC().Format(time.RFC3339),
		LastEventID: h.loadLastEventID(),
	}
	if h.cfg.ActiveRuns != nil {
		handles := h.cfg.ActiveRuns.Snapshot()
		now := h.cfg.Now()
		for _, hd := range handles {
			if hd == nil {
				continue
			}
			ar := activeRunSummary{
				BeadID: string(hd.BeadID),
			}
			if !hd.StartedAt.IsZero() {
				ar.AgeSeconds = int(now.Sub(hd.StartedAt).Seconds())
			}
			out.ActiveRuns = append(out.ActiveRuns, ar)
		}
	}
	if out.ActiveRuns == nil {
		out.ActiveRuns = []activeRunSummary{}
	}
	return out
}

func (h *SubscribeHub) loadLastEventID() string {
	v, _ := h.lastEventID.Load().(string)
	return v
}

// subscriptionStream is the per-connection event channel + drop counter.
type subscriptionStream struct {
	ch         chan core.Event
	typeFilter map[string]struct{}
	wildcard   bool
	// Agent-message addressing filters, mirrored from SubscribeRequest (N1).
	to    string
	from  string
	topic string

	dropped atomic.Int64
}

// offer attempts a non-blocking send. On full channel: drops OLDEST, counts.
// Filters by type before queuing to avoid burning channel slots on
// uninteresting events. For agent_message events, also applies the
// addressing filter (N1) via MatchAgentMessage.
func (s *subscriptionStream) offer(evt core.Event) {
	if !s.wildcard {
		if _, ok := s.typeFilter[evt.Type]; !ok {
			return
		}
	}
	// Agent-message addressing filter (N1). Applies on agent_message events
	// only; other event types bypass this block.
	if evt.Type == "agent_message" && (s.to != "" || s.from != "" || s.topic != "") {
		var p AgentMessagePayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil || !MatchAgentMessage(p, s.to, s.from, s.topic) {
			return
		}
	}
	for {
		select {
		case s.ch <- evt:
			return
		default:
			// Channel full → drop OLDEST and retry.
			select {
			case <-s.ch:
				s.dropped.Add(1)
			default:
				// Raced with a consumer drain; retry the send.
			}
		}
	}
}

// swapDropped atomically reads and zeros the drop counter.
func (s *subscriptionStream) swapDropped() int64 {
	return s.dropped.Swap(0)
}

// activeRunSummary is one entry in the heartbeat active_runs array.
type activeRunSummary struct {
	BeadID     string `json:"bead_id"`
	AgeSeconds int    `json:"age_seconds"`
}

// heartbeatLine is the per-heartbeat NDJSON payload.
type heartbeatLine struct {
	Type        string             `json:"type"`
	Timestamp   string             `json:"ts"`
	ActiveRuns  []activeRunSummary `json:"active_runs"`
	LastEventID string             `json:"last_event_id"`
}

// subscriptionGapLine is the connection-only payload announcing dropped events.
type subscriptionGapLine struct {
	Type    string `json:"type"`
	Dropped int64  `json:"dropped"`
}

// writeSubscribeError writes a SocketResponse error and is used when no
// SubscribeHandler is wired or the request is malformed.
func writeSubscribeError(w io.Writer, msg string) {
	data, _ := json.Marshal(SocketResponse{Ok: false, Error: msg})
	_, _ = w.Write(data)
}
