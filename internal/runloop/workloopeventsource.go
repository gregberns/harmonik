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

	// mu guards subs. Subscribe is called only at run-setup time (before the
	// producer is hot), but Emit may race with a late Subscribe, so the slice
	// is mutex-guarded for safety under -race.
	mu sync.Mutex
	// subs holds every subscriber channel. Each receives a copy of every
	// emitted synthetic envelope (non-blocking, drop-if-full per channel).
	subs []chan core.EventEnvelope
}

// perRunEventTapBufSize is the capacity of each per-run subscriber channel.
// Large enough to absorb a burst of rapid watcher events without blocking
// the watcher goroutine; consumers drain lazily.
const perRunEventTapBufSize = 64

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
	}
	events = tap.Subscribe()
	return tap, events
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
func (t *PerRunEventTap) Subscribe() <-chan core.EventEnvelope {
	ch := make(chan core.EventEnvelope, perRunEventTapBufSize)
	t.mu.Lock()
	t.subs = append(t.subs, ch)
	t.mu.Unlock()
	return ch
}

// fanOut delivers env to every registered subscriber channel. Each send is
// non-blocking: if a subscriber's buffer is full the event is dropped for that
// subscriber only, never blocking the producer or starving other subscribers.
func (t *PerRunEventTap) fanOut(env core.EventEnvelope) {
	t.mu.Lock()
	subs := t.subs
	t.mu.Unlock()
	for _, ch := range subs {
		// Non-blocking send: discard for this subscriber if its buffer is full.
		select {
		case ch <- env:
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
