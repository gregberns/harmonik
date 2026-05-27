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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

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

	// SinceEventID is reserved for replay-from-cursor; not implemented in v1
	// (would require coordination with bus.ReplayFrom and a per-consumer
	// stable ID). The daemon REJECTS subscribe requests where this field is
	// non-empty with an explicit error (filed as hk-a5sil). The field is
	// retained on the wire so the protocol shape is stable when replay lands.
	SinceEventID string `json:"since_event_id,omitempty"`

	// HeartbeatSeconds is the idle heartbeat cadence. Clamped to [10, 600];
	// zero or negative defaults to 60.
	HeartbeatSeconds int `json:"heartbeat_seconds,omitempty"`
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

	// Now is the wall-clock function. Defaults to time.Now when nil.
	// Tests override this; production wiring leaves it nil.
	Now func() time.Time
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

	closed atomic.Bool
}

// NewSubscribeHub returns a hub bound to cfg. Bus is required.
func NewSubscribeHub(cfg SubscribeHubConfig) *SubscribeHub {
	if cfg.Now == nil {
		cfg.Now = time.Now
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
	}

	// Register; deregister on return so the bus stops fanning out.
	h.mu.Lock()
	h.subscribers[s] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subscribers, s)
		h.mu.Unlock()
	}()

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

	heartbeat := time.NewTimer(heartbeatInterval)
	defer heartbeat.Stop()

	enc := json.NewEncoder(conn)

	for {
		select {
		case <-streamCtx.Done():
			return

		case evt := <-s.ch:
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
			// Reset heartbeat — we just wrote, so the line is fresh.
			if !heartbeat.Stop() {
				select {
				case <-heartbeat.C:
				default:
				}
			}
			heartbeat.Reset(heartbeatInterval)

		case <-heartbeat.C:
			payload := h.makeHeartbeat()
			if err := enc.Encode(payload); err != nil {
				return
			}
			heartbeat.Reset(heartbeatInterval)
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

	dropped atomic.Int64
}

// offer attempts a non-blocking send. On full channel: drops OLDEST, counts.
// Filters by type before queuing to avoid burning channel slots on
// uninteresting events.
func (s *subscriptionStream) offer(evt core.Event) {
	if !s.wildcard {
		if _, ok := s.typeFilter[evt.Type]; !ok {
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
