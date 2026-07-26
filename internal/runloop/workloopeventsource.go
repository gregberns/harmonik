package runloop

// workloopeventsource.go — the per-run event tap every dispatch segment's
// ready pump consumes (hk-gql20.14).
//
// The daemon's event bus is sealed at Start time (EV-009) before the work loop
// runs, so post-seal Subscribe is not available. This file provides a thin
// "tapping emitter" wrapper that intercepts Emit calls from the watcher
// goroutine and forwards a synthetic envelope to a per-run channel that the
// segment's ready pump reads.
//
// One type lives here:
//
//   - perRunEventTap: a handlercontract.EventEmitter adapter that wraps the
//     real bus emitter, forwarding every Emit call to a buffered channel AND
//     to the underlying bus.  One tap is created per beadRunOne call.
//
// RT14 removed the second type, chanAgentEventSource. It existed only to
// satisfy waitAgentReady's agentEventSource interface, and both of its
// constructors were the two open-coded ready waits RT14 converted onto
// dispatchSegment (dispatchsegment.go), whose ready pump consumes the tap
// channel directly.
//
// Bead: hk-gql20.14. Retirement: P2 E5 RT14.

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// PerRunEventTap is a handlercontract.EventEmitter wrapper that forwards every
// Emit call to one or more buffered subscriber channels as a synthetic
// core.EventEnvelope, in addition to delegating to the underlying bus emitter.
//
// It is created once per beadRunOne call and passed as the publisher to the
// per-run handler.Launch. Because each bead goroutine owns its own tap, there
// is no cross-run event leakage.
//
// # Fan-out (hk-37giq)
//
// The tap is a FAN-OUT, not a single shared channel: each call to Subscribe
// returns an independent buffered channel, and every Emit/EmitWithRunID writes
// a COPY of the synthetic envelope to EVERY registered subscriber. This is the
// fix for the concurrent-dispatch wedge: previously a single channel was shared
// by two competing consumers — the segment's ready pump and
// pasteInjectQuitOnCommit (the launch/heartbeat watchdog). A Go channel receive
// is EXCLUSIVE, so under 2+ concurrent runs the ready-side drain goroutine
// stayed hot and consumed every heartbeat; pasteInjectQuitOnCommit never observed
// firstHeartbeatSeen, its launch-verification branch reset launchDeadline forever,
// and the implementer appeared stalled at launch (launch_stall_detected →
// run_stale), never advancing. Giving each consumer its OWN subscription delivers
// every event to BOTH, eliminating the competing-consumer race. The 12-min
// launchSuppressionCeiling (hk-jgxqc) remains as a defence-in-depth backstop.
//
// Each subscriber channel is buffered at perRunEventTapBufSize so that the
// watcher goroutine (the producer) does not block if a consumer has not yet
// drained a previous event. Per-channel buffer overflow means that subscriber's
// event is silently discarded (worst-case for the ready pump: the segment times out
// instead of detecting ready — safe; worst-case for the watchdog: it falls back
// to its wall-clock backstops — also safe). Crucially, a slow/full consumer can
// NO LONGER starve the other consumer of events, because each owns its own
// buffer.
type PerRunEventTap struct {
	// underlying is the real bus emitter; all Emit calls are forwarded here.
	underlying handlercontract.EventEmitter

	// runID is the run identifier stamped onto synthetic envelopes.
	runID core.RunID

	// mu guards subs and serializes fan-out with unsubscription. Fan-out holds
	// the lock only across non-blocking channel sends, so Unsubscribe can wait
	// for any in-flight send and guarantee that none occurs after it returns.
	mu sync.Mutex
	// subs holds every live owned subscription. Each receives a copy of every
	// emitted synthetic envelope (non-blocking, drop-if-full per handle).
	subs map[*EventSubscription]struct{}
}

// perRunEventTapBufSize is the capacity of each per-run subscriber channel.
// Large enough to absorb a burst of rapid watcher events without blocking
// the watcher goroutine; consumers drain lazily.
const perRunEventTapBufSize = 64

// EventSubscription owns one PerRunEventTap subscription.
//
// Callers receive events through Events and MUST call Unsubscribe when their
// consumer lifetime ends. Unsubscribe is synchronous and idempotent: after it
// returns, the tap cannot send another event to this subscription. Done closes
// when unsubscription has completed. Events is also closed; events buffered
// before unsubscription remain available to receive before channel closure is
// observed.
//
// The concrete handle intentionally exposes no channel-send capability.
type EventSubscription struct {
	tap    *PerRunEventTap
	events chan core.EventEnvelope
	done   chan struct{}
	once   sync.Once
}

// Events returns the subscription's receive-only event channel.
func (s *EventSubscription) Events() <-chan core.EventEnvelope {
	return s.events
}

// Done returns a channel that closes after the subscription has been removed
// from its tap and no further event can be sent to it.
func (s *EventSubscription) Done() <-chan struct{} {
	return s.done
}

// Unsubscribe synchronously removes this subscription from its tap.
//
// Removal is deliberately not canceled by ctx: retaining a dead subscriber
// because its owner is already canceling would leak the handle for the rest of
// the tap's lifetime. The context parameter keeps the lifecycle API compatible
// with PhaseScope-style teardown and error aggregation. This synchronous
// in-memory implementation cannot currently fail and always returns nil.
func (s *EventSubscription) Unsubscribe(_ context.Context) error {
	s.once.Do(func() {
		tap := s.tap
		tap.mu.Lock()
		defer tap.mu.Unlock()
		delete(tap.subs, s)
		s.tap = nil
		close(s.events)
		close(s.done)
	})
	return nil
}

// NewPerRunEventTap constructs a PerRunEventTap that wraps underlying and
// registers an initial subscriber. Returns the tap and that subscriber's
// channel (consumed by the dispatch segment's ready pump).
//
// Additional independent subscribers (e.g. for pasteInjectQuitOnCommit) are
// obtained via Subscribe — each receives its own copy of every event (hk-37giq).
func NewPerRunEventTap(underlying handlercontract.EventEmitter, runID core.RunID) (tap *PerRunEventTap, events <-chan core.EventEnvelope) {
	tap = &PerRunEventTap{
		underlying: underlying,
		runID:      runID,
		subs:       make(map[*EventSubscription]struct{}),
	}
	events = tap.Subscribe()
	return tap, events
}

// SubscribeOwned registers and returns a new independently owned subscription.
// Every subsequent Emit/EmitWithRunID delivers a copy of the synthetic envelope
// to Events (non-blocking, drop-if-full), independently of every other
// subscription. The owner must call Unsubscribe when its consumer exits.
func (t *PerRunEventTap) SubscribeOwned() *EventSubscription {
	sub := &EventSubscription{
		tap:    t,
		events: make(chan core.EventEnvelope, perRunEventTapBufSize),
		done:   make(chan struct{}),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.subs == nil {
		t.subs = make(map[*EventSubscription]struct{})
	}
	t.subs[sub] = struct{}{}
	return sub
}

// Subscribe registers and returns a new independent subscriber channel. Every
// subsequent Emit/EmitWithRunID delivers a copy of the synthetic envelope to
// this channel (non-blocking, drop-if-full), independently of any other
// subscriber. This lets two consumers (the segment's ready pump and the
// pasteInjectQuitOnCommit watchdog) each receive every event rather than
// competing for receives on a single shared channel (hk-37giq).
//
// Subscribe is intended to be called at run-setup time, before the producing
// watcher goroutine becomes hot. It is safe to call concurrently with Emit.
//
// Deprecated: use SubscribeOwned and call Unsubscribe when the consumer exits.
// This compatibility wrapper retains its subscription until the tap itself is
// released because the returned channel has no ownership handle.
func (t *PerRunEventTap) Subscribe() <-chan core.EventEnvelope {
	return t.SubscribeOwned().Events()
}

// fanOut delivers env to every registered subscriber channel. Each send is
// non-blocking: if a subscriber's buffer is full the event is dropped for that
// subscriber only, never blocking the producer or starving other subscribers.
func (t *PerRunEventTap) fanOut(env core.EventEnvelope) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for sub := range t.subs {
		// Non-blocking send: discard for this subscriber if its buffer is full.
		select {
		case sub.events <- env:
		default:
		}
	}
}

// Emit delegates to the underlying bus emitter and fans out a synthetic
// envelope to every subscriber channel.
//
// The synthetic envelope carries the event type and a fresh UUIDv7 event_id;
// it does NOT include the payload (the ready pump's adapter.DetectReady only
// inspects the Type field, per HC-041).
//
// If a subscriber channel is full (producer faster than that consumer), the
// event is discarded for that subscriber rather than blocking. This is
// intentional: the watcher goroutine MUST NOT be blocked by a slow consumer.
func (t *PerRunEventTap) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	// Delegate to underlying first — bus delivery takes priority.
	err := t.underlying.Emit(ctx, eventType, payload)

	// Build a synthetic envelope and fan it out to every subscriber.
	var env core.EventEnvelope
	if id, uuidErr := uuid.NewV7(); uuidErr == nil {
		env.EventID = core.EventID(id)
	}
	env.Type = string(eventType)
	runIDCopy := t.runID
	env.RunID = &runIDCopy

	t.fanOut(env)

	return err
}

// EmitWithRunID delegates to the underlying bus emitter and also fans out a
// synthetic envelope (using the provided runID) to every subscriber channel.
//
// The watcher uses plain Emit (not EmitWithRunID), but this method is required
// to satisfy handlercontract.EventEmitter. It is also called by the daemon
// heartbeat emitter (newDaemonHeartbeatEmitter) which uses EmitWithRunID.
func (t *PerRunEventTap) EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	err := t.underlying.EmitWithRunID(ctx, runID, eventType, payload)

	var env core.EventEnvelope
	if id, uuidErr := uuid.NewV7(); uuidErr == nil {
		env.EventID = core.EventID(id)
	}
	env.Type = string(eventType)
	runIDCopy := runID
	env.RunID = &runIDCopy

	t.fanOut(env)

	return err
}
