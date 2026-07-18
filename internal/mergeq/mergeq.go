// Package mergeq is the merge-queue exclusion domain for the daemon run path.
//
// It splits the historical daemon `mergeMu` mutex into an explicit, strictly
// FIFO single-executor queue (RSM-002 / RSM-012). All members of the merge
// exclusion domain (commit-phase merge, escape check, remote base-sync +
// worktree-add) run their critical section via Queue.Submit; the queue's one
// owner goroutine drains submissions in intake-sequence order, preserving the
// global single-writer invariant (hk-yyso7) while keeping build-class work
// OUTSIDE the domain.
//
// FIFO mechanism: each submission is stamped with a monotonically increasing
// intake sequence number under the queue mutex, and the owner goroutine serves
// pending submissions in seq order. Ordering is therefore deterministic and
// spec-guaranteed — it does NOT rely on the release order of blocked channel
// senders, which the Go spec leaves unspecified (RU-03).
//
// The package is a leaf: it imports the Go standard library only. It never
// imports internal/daemon (depguard-enforced) — the daemon threads its
// prepare/commit closures IN as `critical` funcs, so the dependency direction is
// daemon -> mergeq, never the reverse.
//
// Design: .kerf/works/2026-07-14-run-state-machine/04-design/merge-queue-design.md §1.
package mergeq

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
)

// ErrQueueStopped is returned by Submit when the queue's owner goroutine has
// stopped (its Start ctx was cancelled) before the submission could enter the
// exclusion domain. The critical section did not run.
var ErrQueueStopped = errors.New("mergeq: queue stopped")

// job is a single submission held on the pending list until the owner
// goroutine serves it.
type job struct {
	// ctx is the submitter's context. Carrying it on the job is deliberate: the
	// owner goroutine must run the critical section under the SUBMITTER's ctx
	// (not its own), and must consult it at the pre-execution gate to honour a
	// cancel-before-execution. This is the queue's message payload, not a stored
	// request-scoped ctx of the queue itself.
	ctx      context.Context //nolint:containedctx // ctx is the submission payload; see field doc.
	seq      uint64
	label    string
	critical func(context.Context) error
	result   chan error
	enqueued time.Time
}

// Queue serializes the merge critical section. Submissions execute strictly
// FIFO — in intake-sequence order — on a single owner goroutine; a critical
// section never overlaps another.
//
// The zero value is not usable; construct with New and drive with Start.
type Queue struct {
	// mu guards nextSeq, pending, and stopped. Seq assignment and the append to
	// pending happen atomically under mu, so pending is always sorted by seq and
	// serving pending head-first IS serving in intake order.
	mu      sync.Mutex
	nextSeq uint64
	pending []job
	stopped bool

	// wake (capacity 1) nudges the owner goroutine after an enqueue. A full
	// buffer means a wake is already pending, so the non-blocking send in Submit
	// can never lose a wakeup: the owner re-checks pending under mu after every
	// receive.
	wake chan struct{}

	// done is closed by the owner goroutine when it exits (Start ctx cancelled).
	// At that point stopped is already set and every still-pending submission has
	// been released with ErrQueueStopped, so no submitter leaks.
	done   chan struct{}
	logger *slog.Logger
}

// New builds a Queue. A nil logger is replaced with a no-op logger so callers
// need not guard every log site.
func New(logger *slog.Logger) *Queue {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Queue{wake: make(chan struct{}, 1), done: make(chan struct{}), logger: logger}
}

// Start launches the owner goroutine that drains the queue. It returns
// immediately; the goroutine runs until ctx is cancelled. Call Start exactly
// once per Queue.
func (q *Queue) Start(ctx context.Context) {
	go q.run(ctx)
}

func (q *Queue) run(ctx context.Context) {
	defer close(q.done)
	for {
		// Stop promptly on cancellation, even with work still pending — pending
		// submissions never entered the domain and are released with
		// ErrQueueStopped below.
		if ctx.Err() != nil {
			q.stop()
			return
		}

		if j, ok := q.pop(); ok {
			q.execute(j)
			continue
		}

		select {
		case <-ctx.Done():
			q.stop()
			return
		case <-q.wake:
		}
	}
}

// pop removes and returns the lowest-seq pending job, if any. pending is
// seq-sorted by construction (seq assigned and appended under one mu hold), so
// the head is always the next intake-sequence job.
func (q *Queue) pop() (job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return job{}, false
	}
	j := q.pending[0]
	q.pending[0] = job{} // release references held by the popped slot
	q.pending = q.pending[1:]
	return j, true
}

// stop marks the queue stopped and releases every still-pending submission
// with ErrQueueStopped. After stopped is set (under mu), Submit rejects new
// intake, so no submission can be appended and then stranded.
func (q *Queue) stop() {
	q.mu.Lock()
	q.stopped = true
	pending := q.pending
	q.pending = nil
	q.mu.Unlock()
	for _, j := range pending {
		j.result <- ErrQueueStopped
	}
}

// execute runs one critical section under the owner goroutine. It is the only
// place a critical func is invoked, guaranteeing mutual exclusion.
func (q *Queue) execute(j job) {
	waitMS := time.Since(j.enqueued).Milliseconds()

	// ctx-cancel-BEFORE-execution: if the submitter's ctx already ended while
	// the job waited in the queue, skip the critical entirely and report the
	// cancellation. Once we pass this gate the critical runs to completion under
	// its own ctx (a Background-derived ctx for the shutdown-drain submission).
	if err := j.ctx.Err(); err != nil {
		q.logger.InfoContext(j.ctx, "mergeq: skipped cancelled submission",
			"label", j.label, "seq", j.seq, "wait_ms", waitMS, "err", err)
		j.result <- err
		return
	}

	q.logger.InfoContext(j.ctx, "mergeq: executing", "label", j.label, "seq", j.seq, "wait_ms", waitMS)
	err := j.critical(j.ctx)
	q.logger.InfoContext(j.ctx, "mergeq: executed", "label", j.label, "seq", j.seq, "wait_ms", waitMS, "err", err)
	j.result <- err
}

// Submit runs critical inside the exclusion domain, strictly FIFO across all
// submitters: every submission is assigned a monotonically increasing intake
// sequence number under the queue mutex, and the owner goroutine executes in
// seq order. Submit blocks until the critical section has executed (returning
// its error). A ctx already cancelled at intake returns ctx.Err() without
// enqueueing; a ctx cancelled while queued is caught by the pre-execution gate
// and returns ctx.Err() without the critical running. Once a critical section
// has begun, it runs to completion under its own ctx and its result is
// returned regardless of later cancellation. If the queue's owner goroutine
// has already stopped (its Start ctx was cancelled) at intake, or stops before
// this submission is served, Submit returns ErrQueueStopped and the critical
// section does not run.
func (q *Queue) Submit(ctx context.Context, label string, critical func(context.Context) error) error {
	// Intake gate: a ctx cancellation here means the submission never entered
	// the domain, so nothing ran.
	if err := ctx.Err(); err != nil {
		return err
	}

	j := job{
		ctx:      ctx,
		label:    label,
		critical: critical,
		result:   make(chan error, 1),
		enqueued: time.Now(),
	}

	// Intake: seq assignment and the append happen under one mu hold, so the
	// seq order IS the pending order — the FIFO position is fixed here,
	// deterministically, independent of goroutine scheduling.
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return ErrQueueStopped
	}
	j.seq = q.nextSeq
	q.nextSeq++
	q.pending = append(q.pending, j)
	q.mu.Unlock()

	// Nudge the owner. Non-blocking: a full buffer means a wake is already
	// pending and the owner will observe this job on its next pending re-check.
	select {
	case q.wake <- struct{}{}:
	default:
	}

	// Enqueued: our FIFO position is fixed. Wait for the owner goroutine to
	// report the outcome — the critical's error, ctx.Err() if we were cancelled
	// while queued (execute's pre-execution gate), or ErrQueueStopped if the
	// owner stopped before serving us.
	return <-j.result
}
