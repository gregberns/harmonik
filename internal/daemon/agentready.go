package daemon

// agentready.go — waitAgentReady: observe event bus until agent_ready or timeout.
//
// Implements HC-056: the daemon MUST observe an agent_ready event from each
// launched session within agent_ready_timeout of process start (default 30s).
//
// # Design
//
// waitAgentReady takes an agentEventSource, which is a narrow interface that
// yields a channel of core.EventEnvelope values scoped to a specific runID.
// The observer goroutine reads from that channel, calling adapter.DetectReady
// on each event; on first true it closes the ready channel.
//
// The outer function resolves the effective timeout (Config.AgentReadyTimeout
// → defaultAgentReadyTimeout = 30s) and performs the three-way select:
//   - ready: return nil
//   - time.After(timeout): return ErrAgentReadyTimeout
//   - ctx.Done(): return ctx.Err()
//
// HC-056 §"last-second arrival": the ready channel is buffered (capacity 1) so
// that the observer goroutine can close it concurrently with the timeout firing.
// Select evaluates all ready cases before picking one; Go's random selection
// between equally-ready cases means ready MAY win on the same scheduler tick.
// The race test (TestWaitAgentReady_ReadyWinsAtBoundary) documents this posture.
//
// This bead does NOT wire waitAgentReady into the workloop completion path —
// that is hk-gql20.14/.15.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-gql20.18. Closes: hk-do7te.

import (
	"context"
	"errors"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// defaultAgentReadyTimeout is the HC-056 default: 30 seconds.
// Informed by claude cold-start latency (≤5s typical, 10–15s cold disk cache)
// plus margin for skill provisioning and one-time .claude/ filesystem warm-up.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
const defaultAgentReadyTimeout = 30 * time.Second

// ErrAgentReadyTimeout is the typed sentinel returned when no agent_ready event
// arrives within the configured timeout window.
//
// Callers (workloop integration hk-gql20.14/.15) MUST match this sentinel with
// errors.Is and respond by cancelling the session context, reaping the subprocess,
// emitting agent_failed{class=structural, sub_reason=agent_ready_timeout}, and
// reopening the bead per HC-056 steps 1–4.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
var ErrAgentReadyTimeout = errors.New("agent_ready timeout: no agent_ready event within deadline (HC-056)")

// agentEventSource is the narrow interface that delivers run-scoped bus events
// to waitAgentReady. The implementation yields one core.EventEnvelope per
// matching event; the channel MUST be closed when no further events will arrive
// (e.g. on ctx cancellation or session teardown).
//
// Production implementations wrap the sealed eventbus.EventBus subscriber
// registered at daemon startup. Test stubs send synthetic events directly.
//
// The interface is intentionally minimal — it does not expose Subscribe, Seal,
// or Drain so that waitAgentReady remains decoupled from bus lifecycle.
//
// Bead ref: hk-gql20.18.
type agentEventSource interface {
	// Events returns a channel that receives core.EventEnvelope values whose
	// run_id matches runID. The channel is closed when ctx is cancelled or
	// when the source determines no further events will arrive for this run.
	Events(ctx context.Context, runID core.RunID) <-chan core.EventEnvelope
}

// waitAgentReady blocks until one of three outcomes:
//
//  1. An event from source satisfies adapter.DetectReady — returns nil.
//  2. The effective timeout elapses — returns ErrAgentReadyTimeout.
//  3. ctx is cancelled — returns ctx.Err().
//
// The effective timeout is cfg.AgentReadyTimeout when non-zero, falling back
// to defaultAgentReadyTimeout (30s) per HC-056.
//
// HC-056 "last-second arrival" posture: if an agent_ready event arrives
// concurrently with timeout expiry, the ready case is preferred. Go's select
// semantics pick randomly among simultaneously ready cases; the ready channel
// is buffered (capacity 1) so the observer goroutine never blocks on send,
// ensuring the value is present for the select race.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-gql20.18.
func waitAgentReady(
	ctx context.Context,
	runID core.RunID,
	source agentEventSource,
	adapter handlercontract.Adapter,
	timeout time.Duration,
) error {
	if timeout <= 0 {
		timeout = defaultAgentReadyTimeout
	}

	// Buffered capacity 1: observer goroutine closes without blocking even if
	// outer select picks timeout first (avoids goroutine leak on timeout path).
	ready := make(chan struct{}, 1)

	// Observer goroutine: pull events from source, test DetectReady.
	// Exits when: ready channel is closed, ctx is done, or source channel closes.
	go func() {
		events := source.Events(ctx, runID)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					// Source closed — no further events; give up.
					return
				}
				if adapter.DetectReady(ev) {
					// Signal outer select; close is idempotent on buffered channel.
					close(ready)
					return
				}
			}
		}
	}()

	select {
	case <-ready:
		return nil
	case <-time.After(timeout):
		// Prefer ready if it arrived at the boundary (both cases simultaneously
		// ready → Go picks randomly; we cannot guarantee ready wins, but we MUST
		// NOT block after timeout). HC-056: "resolved in favour of agent_ready".
		select {
		case <-ready:
			return nil
		default:
			return ErrAgentReadyTimeout
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}
