package daemon

// workloopeventsource.go — per-run agentEventSource implementation for the
// workloop's waitAgentReady wiring (hk-gql20.14).
//
// The daemon's event bus is sealed at Start time (EV-009) before the work loop
// runs, so post-seal Subscribe is not available. This file provides a thin
// "tapping emitter" wrapper that intercepts Emit calls from the watcher
// goroutine and forwards a synthetic envelope to a per-run channel that
// waitAgentReady reads.
//
// The two types here are:
//
//   - perRunEventTap:    a handlercontract.EventEmitter adapter that wraps the
//     real bus emitter, forwarding every Emit call to a buffered channel AND
//     to the underlying bus.  One tap is created per beadRunOne call.
//
//   - chanAgentEventSource: satisfies agentEventSource (from agentready.go)
//     using the channel produced by perRunEventTap.  Events returns a read-only
//     view of that channel; once the context is cancelled, no further events
//     are delivered.
//
// Bead: hk-gql20.14.

import (
	"context"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// perRunEventTap is a handlercontract.EventEmitter wrapper that forwards every
// Emit call to a buffered channel as a synthetic core.EventEnvelope, in
// addition to delegating to the underlying bus emitter.
//
// It is created once per beadRunOne call and passed as the publisher to the
// per-run handler.Launch. Because each bead goroutine owns its own tap, there
// is no cross-run event leakage.
//
// The channel is buffered at perRunEventTapBufSize so that the watcher goroutine
// (the producer) does not block if waitAgentReady (the consumer) has not yet
// drained a previous event. Buffer overflow means events are silently discarded
// (worst-case: waitAgentReady times out instead of detects ready — safe).
type perRunEventTap struct {
	// underlying is the real bus emitter; all Emit calls are forwarded here.
	underlying handlercontract.EventEmitter

	// runID is the run identifier stamped onto synthetic envelopes.
	runID core.RunID

	// ch receives synthetic envelopes for every Emit call.
	// Capacity: perRunEventTapBufSize.
	ch chan core.EventEnvelope
}

// perRunEventTapBufSize is the capacity of the per-run event channel.
// Large enough to absorb a burst of rapid watcher events without blocking
// the watcher goroutine; waitAgentReady drains lazily.
const perRunEventTapBufSize = 64

// newPerRunEventTap constructs a perRunEventTap that wraps underlying.
// Returns the tap and its associated channel (used by chanAgentEventSource).
func newPerRunEventTap(underlying handlercontract.EventEmitter, runID core.RunID) (*perRunEventTap, <-chan core.EventEnvelope) {
	ch := make(chan core.EventEnvelope, perRunEventTapBufSize)
	return &perRunEventTap{
		underlying: underlying,
		runID:      runID,
		ch:         ch,
	}, ch
}

// Emit delegates to the underlying bus emitter and forwards a synthetic
// envelope to the per-run channel.
//
// The synthetic envelope carries the event type and a fresh UUIDv7 event_id;
// it does NOT include the payload (waitAgentReady / adapter.DetectReady only
// inspects the Type field, per HC-041).
//
// If the channel is full (producer faster than consumer), the event is
// discarded rather than blocking. This is intentional: the watcher goroutine
// MUST NOT be blocked by a slow waitAgentReady consumer.
func (t *perRunEventTap) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	// Delegate to underlying first — bus delivery takes priority.
	err := t.underlying.Emit(ctx, eventType, payload)

	// Build a synthetic envelope for the local channel.
	var env core.EventEnvelope
	if id, uuidErr := uuid.NewV7(); uuidErr == nil {
		env.EventID = core.EventID(id)
	}
	env.Type = string(eventType)
	runIDCopy := t.runID
	env.RunID = &runIDCopy

	// Non-blocking send: discard if the channel is full.
	select {
	case t.ch <- env:
	default:
	}

	return err
}

// EmitWithRunID delegates to the underlying bus emitter and also forwards a
// synthetic envelope (using the provided runID) to the per-run channel.
//
// The watcher uses plain Emit (not EmitWithRunID), but this method is required
// to satisfy handlercontract.EventEmitter. It is also called by the daemon
// heartbeat emitter (newDaemonHeartbeatEmitter) which uses EmitWithRunID.
func (t *perRunEventTap) EmitWithRunID(ctx context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	err := t.underlying.EmitWithRunID(ctx, runID, eventType, payload)

	var env core.EventEnvelope
	if id, uuidErr := uuid.NewV7(); uuidErr == nil {
		env.EventID = core.EventID(id)
	}
	env.Type = string(eventType)
	runIDCopy := runID
	env.RunID = &runIDCopy

	select {
	case t.ch <- env:
	default:
	}

	return err
}

// chanAgentEventSource satisfies agentEventSource by reading from a channel
// produced by perRunEventTap.
//
// Events returns a receive-only channel that delivers core.EventEnvelope values.
// It spawns a forwarding goroutine that exits when ctx is cancelled.
type chanAgentEventSource struct {
	ch <-chan core.EventEnvelope
}

// newChanAgentEventSource constructs a chanAgentEventSource backed by ch.
func newChanAgentEventSource(ch <-chan core.EventEnvelope) *chanAgentEventSource {
	return &chanAgentEventSource{ch: ch}
}

// Events implements agentEventSource.
//
// It returns a new channel that receives events from the underlying ch until
// ctx is cancelled. The returned channel is closed when either ctx is
// cancelled or the underlying channel is closed, so the waitAgentReady
// observer goroutine can detect both conditions cleanly.
//
// runID is accepted for interface compatibility (agentEventSource) but is not
// used to filter events — since each perRunEventTap is per-run, all events in
// ch are already scoped to this run.
func (s *chanAgentEventSource) Events(ctx context.Context, _ core.RunID) <-chan core.EventEnvelope {
	out := make(chan core.EventEnvelope, perRunEventTapBufSize)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-s.ch:
				if !ok {
					return
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
