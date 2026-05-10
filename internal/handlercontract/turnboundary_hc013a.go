package handlercontract

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// turnBoundary — per-bead helper prefix for test helpers in
// turnboundary_hc013a_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.16).

// ─────────────────────────────────────────────────────────────────────────────
// HC-013a — RotateAccount fires only at turn boundaries
// ─────────────────────────────────────────────────────────────────────────────

// TurnBoundaryGuard tracks whether the handler session has an in-flight LLM
// turn and enforces the HC-013a rule: Adapter.RotateAccount MUST NOT be
// invoked while the subprocess has an in-flight turn.
//
// The watcher calls MarkTurnStarted when it observes the agent_started
// progress-stream message and MarkTurnCompleted when it observes
// agent_completed.  ScheduleRotateAccount blocks until the next clean turn
// boundary (MarkTurnCompleted) or until the configurable timeout expires.
//
// # Turn definition
//
// A turn is the interval between a work-dispatch that reaches the subprocess
// (observed as agent_started) and the subsequent agent_completed of that turn.
//
// # Timeout behaviour
//
// If a rotation is requested but no quiescent turn boundary is observed within
// timeout, ScheduleRotateAccount returns ErrTransient per HC-013a — "if no
// quiescent turn boundary is observed within a configurable window (default:
// the enclosing LaunchSpec.timeout), RotateAccount MUST return ErrTransient."
//
// # Goroutine safety
//
// All methods are safe to call from any goroutine.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
type TurnBoundaryGuard struct {
	mu        sync.Mutex
	inFlight  bool
	boundaryC chan struct{} // closed and replaced on each MarkTurnCompleted
}

// NewTurnBoundaryGuard creates a TurnBoundaryGuard in the idle (no turn
// in-flight) state.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
func NewTurnBoundaryGuard() *TurnBoundaryGuard {
	return &TurnBoundaryGuard{
		inFlight:  false,
		boundaryC: make(chan struct{}),
	}
}

// MarkTurnStarted records that the handler subprocess has begun processing a
// new work item (observed via the agent_started progress-stream message).
//
// After this call, ScheduleRotateAccount will defer rotation until the next
// MarkTurnCompleted call.  Calling MarkTurnStarted while a turn is already
// in-flight is a daemon defect; it panics to surface the inconsistency.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
func (g *TurnBoundaryGuard) MarkTurnStarted() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight {
		panic("handlercontract: TurnBoundaryGuard.MarkTurnStarted called while turn already in-flight — daemon defect")
	}
	g.inFlight = true
}

// MarkTurnCompleted records that the handler subprocess has completed its
// current turn (observed via the agent_completed progress-stream message).
//
// Any goroutines blocked in ScheduleRotateAccount are unblocked.  Calling
// MarkTurnCompleted while no turn is in-flight is a daemon defect; it panics
// to surface the inconsistency.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
func (g *TurnBoundaryGuard) MarkTurnCompleted() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.inFlight {
		panic("handlercontract: TurnBoundaryGuard.MarkTurnCompleted called while no turn is in-flight — daemon defect")
	}
	g.inFlight = false

	// Signal all waiters by closing the current boundary channel and replacing
	// it with a fresh one for the next turn.
	old := g.boundaryC
	g.boundaryC = make(chan struct{})
	close(old)
}

// IsTurnInFlight reports whether the session currently has an in-flight LLM
// turn.  Safe to call from any goroutine.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
func (g *TurnBoundaryGuard) IsTurnInFlight() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inFlight
}

// ScheduleRotateAccount enforces the HC-013a turn-boundary constraint and then
// calls adapter.RotateAccount(ctx).
//
// If the session has no in-flight turn, RotateAccount is called immediately.
//
// If the session has an in-flight turn, ScheduleRotateAccount blocks until:
//   - the next MarkTurnCompleted (clean boundary), then calls RotateAccount, or
//   - ctx is cancelled — returns ctx.Err() wrapped as ErrCanceled, or
//   - timeout elapses — returns ErrTransient per HC-013a.
//
// timeout MUST be positive.  The HC-013a default is the enclosing
// LaunchSpec.Timeout; callers are responsible for supplying the correct value.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
func (g *TurnBoundaryGuard) ScheduleRotateAccount(ctx context.Context, adapter Adapter, timeout time.Duration) error {
	// Snapshot the in-flight state and the current boundary channel under the
	// lock.  We wait on the channel without holding the lock.
	g.mu.Lock()
	inFlight := g.inFlight
	waitC := g.boundaryC
	g.mu.Unlock()

	if inFlight {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-waitC:
			// Turn boundary reached; fall through to call RotateAccount.
		case <-ctx.Done():
			return fmt.Errorf("handlercontract: HC-013a: rotation cancelled: %w", ErrCanceled)
		case <-timer.C:
			return fmt.Errorf("handlercontract: HC-013a: no clean turn boundary within %s: %w", timeout, ErrTransient)
		}
	}

	return adapter.RotateAccount(ctx)
}
