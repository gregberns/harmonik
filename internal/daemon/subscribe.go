package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/runregistry"
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

const (
	subscribeHeartbeatMin     = 10
	subscribeHeartbeatMax     = 600
	subscribeHeartbeatDefault = 60

	subscribeChannelCapacity = 256

	subscribeWriteTimeout = 30 * time.Second
)

const commsCursorFlushInterval = 2 * time.Second

// ActiveRunsSource is the minimal runregistry.RunRegistry surface that subscribeHub
// needs to render the heartbeat active_runs snapshot. *runregistry.RunRegistry satisfies
// this interface via Snapshot().
type ActiveRunsSource interface {
	Snapshot() []*runregistry.RunHandle
}

const subscribeMaxConnectionsDefault = 32

// NewTimerFn is a factory that creates a timer for a given duration.
// It returns a receive-only channel that fires when the timer expires,
// a stop function (mirrors time.Timer.Stop), and a reset function
// (mirrors time.Timer.Reset). Tests inject a fake via SubscribeHubConfig.NewTimer.
type NewTimerFn func(d time.Duration) (c <-chan time.Time, stop func() bool, reset func(time.Duration))

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

	// WriteTimeout bounds how long a single write to a subscriber connection
	// may take before it is treated as a dead/stuck peer and the connection is
	// torn down. When zero, subscribeWriteTimeout (30s) is used. Tests override
	// this with a short duration to exercise the reap-on-stalled-write path
	// without a real 30s wait.
	WriteTimeout time.Duration

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

	// cursorStore, when non-nil, lets a `comms recv --follow`/`--wait` (or a
	// bare `harmonik subscribe --to`) session advance the calling agent's
	// durable LIVE comms cursor as agent_message events are delivered
	// (hk-tafd4). Without this, a watcher that restarts replays everything
	// since its initial drain because the cursor only moves on a one-shot
	// `comms recv`. Set post-construction via SetCommsCursorStore.
	//
	// This is the LIVE cursor, independent of the POLL cursor a plain one-shot
	// `comms recv --agent` (without --follow/--wait) advances (hk-8xspi, B1).
	// The two are deliberately decoupled: a poller and a follow/wait watcher no
	// longer race over one shared position — draining one never advances the
	// other. See commsSendHandlerImpl.SetRecvDeps (commsrecvhandler_nnwaa.go)
	// for the poll-store side and daemon.go for the wiring. nil = cursor
	// advancement disabled (subscribe behaves as before).
	cursorStore *CursorStore

	closed atomic.Bool
}

// SetCommsCursorStore wires the per-agent LIVE comms cursor store into the hub
// so a `comms recv --follow`/`--wait` (or bare `harmonik subscribe --to`)
// session advances the agent's durable cursor as agent_message events are
// delivered (hk-tafd4). Pass the SAME *CursorStore used as the liveStore
// argument to commsSendHandlerImpl.SetRecvDeps (see daemon wiring) so a
// follow/wait session's catch-up drain and its live tail share one continuous
// cursor — decoupled from the poll cursor a plain `comms recv --agent` uses
// (hk-8xspi, B1). A nil store leaves cursor advancement disabled.
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
	maxConn := int64(h.cfg.MaxConnections)
	if maxConn <= 0 {
		maxConn = subscribeMaxConnectionsDefault
	}
	writeTimeout := h.cfg.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = subscribeWriteTimeout
	}
	for {
		cur := h.connCount.Load()
		if cur >= maxConn {
			if dlErr := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); dlErr != nil {
				slog.WarnContext(ctx, "daemon: subscribe: set write deadline on refused connection", "err", dlErr)
			}
			writeSubscribeError(conn, "subscribe_capacity_exceeded")
			return
		}
		if h.connCount.CompareAndSwap(cur, cur+1) {
			break
		}
	}
	defer h.connCount.Add(-1)

	typeFilter := make(map[string]struct{}, len(req.Types))
	for _, t := range req.Types {
		if t != "" {
			typeFilter[t] = struct{}{}
		}
	}
	wildcard := len(typeFilter) == 0

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

	h.mu.Lock()
	h.subscribers[s] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, s)
		h.mu.Unlock()
	}()

	if h.cfg.PresenceEmitter != nil && req.To != "" {
		if _, emitErr := h.cfg.PresenceEmitter.EmitAgentPresence(ctx, core.AgentPresencePayload{
			Agent:    req.To,
			Status:   core.AgentPresenceStatusOnline,
			LastSeen: h.cfg.Now().UTC().Format(time.RFC3339),
			Reason:   core.AgentPresenceReasonRefresh,
		}); emitErr != nil {
			slog.WarnContext(ctx, "daemon: subscribe: emit agent_presence refresh beat", "err", emitErr, "agent", req.To)
		}
	}

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

	cursorAdvanceEnabled := h.cursorStore != nil && req.To != ""
	var pendingCursorID string // last agent_message event_id delivered but not yet flushed
	flushCursor := func() {
		if !cursorAdvanceEnabled || pendingCursorID == "" {
			return
		}
		agentMu := h.cursorStore.AgentMu(req.To)
		agentMu.Lock()
		if advErr := h.cursorStore.Advance(req.To, pendingCursorID); advErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: subscribe: advance comms cursor for %s: %v\n", req.To, advErr)
		}
		agentMu.Unlock()
		pendingCursorID = ""
	}
	defer flushCursor()
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
					if _, ok := typeFilter[string(evt.Type)]; !ok {
						continue
					}
				}
				if evt.Type == "agent_message" && (req.To != "" || req.From != "" || req.Topic != "") {
					var p AgentMessagePayload
					if unmarshalErr := json.Unmarshal(evt.Payload, &p); unmarshalErr != nil || !MatchAgentMessage(p, req.To, req.From, req.Topic) {
						continue
					}
				}
				if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
					return
				}
				if encErr := enc.Encode(evt); encErr != nil {
					return
				}
				lastReplayedUID = [16]byte(evt.EventID)
				if cursorAdvanceEnabled && evt.Type == "agent_message" {
					pendingCursorID = evt.EventID.String()
				}
			}
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
			if lastReplayedUID != ([16]byte{}) {
				evtUID := [16]byte(evt.EventID)
				if bytes.Compare(evtUID[:], lastReplayedUID[:]) <= 0 {
					continue
				}
			}

			if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				return
			}

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
			if cursorAdvanceEnabled && evt.Type == "agent_message" {
				pendingCursorID = evt.EventID.String()
			}
			if !hbStop() {
				select {
				case <-hbC:
				default:
				}
			}
			hbReset(heartbeatInterval)

		case <-cursorFlushC:
			flushCursor()
			cursorFlushReset(commsCursorFlushInterval)

		case <-hbC:
			if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				return
			}
			payload := h.makeHeartbeat()
			if err := enc.Encode(payload); err != nil {
				return
			}
			hbReset(heartbeatInterval)
		}
	}
}

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
	v, ok := h.lastEventID.Load().(string)
	if !ok {
		return "" // never stored yet
	}
	return v
}

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

func (s *subscriptionStream) offer(evt core.Event) {
	if !s.wildcard {
		if _, ok := s.typeFilter[string(evt.Type)]; !ok {
			return
		}
	}
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
			select {
			case <-s.ch:
				s.dropped.Add(1)
			default:
			}
		}
	}
}

func (s *subscriptionStream) swapDropped() int64 {
	return s.dropped.Swap(0)
}

type activeRunSummary struct {
	BeadID     string `json:"bead_id"`
	AgeSeconds int    `json:"age_seconds"`
}

type heartbeatLine struct {
	Type        string             `json:"type"`
	Timestamp   string             `json:"ts"`
	ActiveRuns  []activeRunSummary `json:"active_runs"`
	LastEventID string             `json:"last_event_id"`
}

type subscriptionGapLine struct {
	Type    string `json:"type"`
	Dropped int64  `json:"dropped"`
}

func writeSubscribeError(w io.Writer, msg string) {
	data, marshalErr := json.Marshal(SocketResponse{Ok: false, Error: msg})
	if marshalErr != nil {
		slog.WarnContext(context.Background(), "daemon: subscribe: marshal error reply", "err", marshalErr, "msg", msg)
		return
	}
	if _, writeErr := w.Write(data); writeErr != nil {
		slog.WarnContext(context.Background(), "daemon: subscribe: write error reply", "err", writeErr, "msg", msg)
	}
}
