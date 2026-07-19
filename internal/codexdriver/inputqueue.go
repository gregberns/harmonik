package codexdriver

import (
	"context"
	"errors"
	"sync"

	"github.com/gregberns/harmonik/internal/handler"
)

// hk-160yb G3: bounded FIFO input queue in front of the resident app-server
// client.
//
// The underlying handler.InputPort already enforces ONE turn in flight —
// SubmitInput serializes on submitMu and blocks to the turn's terminal
// (session.go). What it does NOT bound is the BACKLOG: concurrent SubmitInput
// callers pile up as goroutines parked on submitMu, with no cap and no FIFO
// ordering guarantee. For a persistent supervised sidecar that a resident owner
// feeds from comms/queue wakes, that unbounded pile-up is the missing
// backpressure the residual-gap audit flagged.
//
// BoundedInputQueue closes that gap: a fixed-capacity buffered channel bounds the
// number of QUEUED submissions, a single drainer goroutine delivers them to the
// port in strict FIFO order (one at a time, so the port's one-in-flight
// invariant is preserved), and Enqueue past capacity returns ErrQueueFull rather
// than growing without bound. It wraps any handler.InputPort, so it is testable
// against a fake port with no live codex child.

// ErrQueueFull is returned by Enqueue when the bounded backlog is at capacity.
// The caller decides how to shed load (drop, retry-later, escalate) — the queue
// never grows past its cap.
var ErrQueueFull = errors.New("codexdriver: input queue full")

// ErrQueueClosed is returned by Enqueue after Close. Submissions already buffered
// at Close are still delivered to the port (graceful FIFO drain), not rejected.
var ErrQueueClosed = errors.New("codexdriver: input queue closed")

// QueuedResult is the terminal outcome of a queued submission, mirroring
// InputPort.SubmitInput's (Ack, error) return delivered asynchronously once the
// drainer processes the item.
type QueuedResult struct {
	Ack handler.Ack
	Err error
}

// queuedItem is one buffered submission: the request, the caller's context
// (threaded through to SubmitInput so a caller can still cancel its own park),
// and the single-slot channel the drainer resolves.
type queuedItem struct {
	ctx   context.Context
	req   handler.InputRequest
	resCh chan QueuedResult
}

// BoundedInputQueue serializes async submissions to a handler.InputPort with a
// bounded FIFO backlog. Safe for concurrent Enqueue; a single drainer goroutine
// owns all SubmitInput calls.
type BoundedInputQueue struct {
	port  handler.InputPort
	items chan queuedItem

	mu     sync.RWMutex
	closed bool

	drainDone chan struct{}
}

// NewBoundedInputQueue wraps port with a backlog of at most `capacity` QUEUED
// submissions (excluding the one in flight in the drainer). capacity < 1 is
// clamped to 1. It starts the drainer goroutine; call Close to stop it.
func NewBoundedInputQueue(port handler.InputPort, capacity int) *BoundedInputQueue {
	if capacity < 1 {
		capacity = 1
	}
	q := &BoundedInputQueue{
		port:      port,
		items:     make(chan queuedItem, capacity),
		drainDone: make(chan struct{}),
	}
	go q.drain()
	return q
}

// Enqueue offers one submission to the bounded backlog. It never blocks on the
// backlog: it returns (resultCh, nil) when accepted, ErrQueueFull when the
// backlog is at capacity, or ErrQueueClosed after Close. The returned channel
// receives exactly one QueuedResult once the drainer delivers the submission to
// the port (or resolves it as closed).
func (q *BoundedInputQueue) Enqueue(ctx context.Context, req handler.InputRequest) (<-chan QueuedResult, error) {
	resCh := make(chan QueuedResult, 1)
	item := queuedItem{ctx: ctx, req: req, resCh: resCh}

	// Hold the read lock across the send so Close (write lock) cannot close
	// q.items between the closed-check and the send — send-on-closed would panic.
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return nil, ErrQueueClosed
	}
	select {
	case q.items <- item:
		return resCh, nil
	default:
		return nil, ErrQueueFull
	}
}

// drain is the single serialized consumer: pull FIFO, deliver to the port one at
// a time, resolve the item's result channel. It exits when items is closed and
// fully drained; items still buffered at Close are delivered to the port
// normally (graceful FIFO drain), NOT rejected.
func (q *BoundedInputQueue) drain() {
	defer close(q.drainDone)
	for item := range q.items {
		// A caller that cancelled before its turn was dispatched short-circuits:
		// resolve with its ctx error rather than opening a turn it abandoned.
		if err := item.ctx.Err(); err != nil {
			item.resCh <- QueuedResult{Err: err}
			continue
		}
		ack, err := q.port.SubmitInput(item.ctx, item.req)
		item.resCh <- QueuedResult{Ack: ack, Err: err}
	}
}

// Close stops accepting new submissions and shuts the drainer down after it has
// processed whatever was already buffered (graceful FIFO drain). It is idempotent
// and blocks until the drainer has exited so no SubmitInput is in flight on
// return.
func (q *BoundedInputQueue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		<-q.drainDone
		return
	}
	q.closed = true
	close(q.items)
	q.mu.Unlock()
	<-q.drainDone
}
