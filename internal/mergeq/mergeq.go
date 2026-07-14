// Package mergeq is the merge-queue exclusion domain for the daemon run path.
//
// It splits the historical daemon `mergeMu` mutex into an explicit, strictly
// FIFO single-executor queue (RSM-002 / RSM-012). All members of the merge
// exclusion domain (commit-phase merge, escape check, remote base-sync +
// worktree-add) run their critical section via Queue.Submit; the queue's one
// owner goroutine drains submissions in arrival order, preserving the global
// single-writer invariant (hk-yyso7) while keeping build-class work OUTSIDE the
// domain.
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
	"time"
)

// ErrQueueStopped is returned by Submit when the queue's owner goroutine has
// stopped (its Start ctx was cancelled) before the submission could enter the
// exclusion domain. The critical section did not run.
var ErrQueueStopped = errors.New("mergeq: queue stopped")

// job is a single submission carried over the owner goroutine's intake channel.
type job struct {
	// ctx is the submitter's context. Carrying it on the job is deliberate: the
	// owner goroutine must run the critical section under the SUBMITTER's ctx
	// (not its own), and must consult it at the pre-execution gate to honour a
	// cancel-before-execution. This is the queue's message payload, not a stored
	// request-scoped ctx of the queue itself.
	ctx      context.Context //nolint:containedctx // ctx is the submission payload; see field doc.
	label    string
	critical func(context.Context) error
	result   chan error
	enqueued time.Time
}

// Queue serializes the merge critical section. Submissions execute strictly
// FIFO on a single owner goroutine; a critical section never overlaps another.
//
// The zero value is not usable; construct with New and drive with Start.
type Queue struct {
	submit chan job
	// done is closed by the owner goroutine when it exits (Start ctx cancelled),
	// so a submitter blocked on the unbuffered send is released instead of
	// leaking. Post-enqueue there is no leak: the owner always executes a job it
	// has received, so the buffered result channel is always written.
	done   chan struct{}
	logger *slog.Logger
}

// New builds a Queue. A nil logger is replaced with a no-op logger so callers
// need not guard every log site.
func New(logger *slog.Logger) *Queue {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	// Unbuffered intake: senders block until the owner is ready to receive,
	// which gives natural backpressure and a Go-runtime FIFO send queue so
	// arrival order is the execution order.
	return &Queue{submit: make(chan job), done: make(chan struct{}), logger: logger}
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
		select {
		case <-ctx.Done():
			return
		case j := <-q.submit:
			q.execute(j)
		}
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
			"label", j.label, "wait_ms", waitMS, "err", err)
		j.result <- err
		return
	}

	q.logger.InfoContext(j.ctx, "mergeq: executing", "label", j.label, "wait_ms", waitMS)
	err := j.critical(j.ctx)
	q.logger.InfoContext(j.ctx, "mergeq: executed", "label", j.label, "wait_ms", waitMS, "err", err)
	j.result <- err
}

// Submit runs critical inside the exclusion domain, strictly FIFO across all
// submitters. It blocks until the critical section has executed (returning its
// error) or until ctx is cancelled BEFORE execution starts (returning
// ctx.Err()). Once a critical section has begun, it runs to completion under
// its own ctx and its result is returned regardless of later cancellation.
// If the queue's owner goroutine has already stopped (its Start ctx was
// cancelled) when Submit is called or while it blocks on enqueue, Submit
// returns ErrQueueStopped and the critical section does not run.
func (q *Queue) Submit(ctx context.Context, label string, critical func(context.Context) error) error {
	j := job{
		ctx:      ctx,
		label:    label,
		critical: critical,
		result:   make(chan error, 1),
		enqueued: time.Now(),
	}

	// Enqueue phase: a ctx cancellation here means the submission never entered
	// the domain, so nothing ran. A closed q.done means the owner goroutine has
	// stopped, so the send would otherwise block forever — release with
	// ErrQueueStopped instead of leaking.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.done:
		return ErrQueueStopped
	case q.submit <- j:
	}

	// Enqueued: our FIFO position is fixed. Wait for the owner goroutine to
	// report the outcome (which may itself be ctx.Err() if we were cancelled
	// while queued — handled by execute's pre-execution gate).
	return <-j.result
}
