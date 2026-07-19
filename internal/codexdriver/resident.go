package codexdriver

// hk-160yb G1: the per-crew resident-session owner.
//
// A codexSession is single-child: it winds down terminally when its child exits
// or the wire closes (session.go finalize). A crew orchestrator, however, needs
// ONE logical Codex session that survives child deaths across many wakes,
// preserving the server-side thread context. ResidentSession is that supervised
// owner:
//
//   - It holds the CURRENT live codexSession and presents a STABLE
//     handler.InputPort (SubmitInput) to callers, so the child underneath can be
//     replaced without the caller — the G3 BoundedInputQueue drainer — ever
//     seeing a new port.
//   - On child death it revives lazily on the next submit: it respawns and, when
//     a prior thread id is known, re-attaches to it via the G1a thread/resume
//     handshake (spawn(..., resumeThreadID)), so server-side context carries
//     across the death. A submit that races the death is retried exactly once on
//     the fresh session.
//   - It owns a BoundedInputQueue (G3) whose drainer is exactly this
//     SubmitInput — that is the production caller the residual-gap audit called
//     for (it clears codexdriver's x-missing-wire-up on the queue).
//
// Proactive output-or-stale liveness (a watchdog that revives BEFORE the next
// submit) is deliberately out of scope here — that is G4. This owner's revival
// is on-demand, which for a queue-fed sidecar reconnects on the next unit of
// work without burning an idle child.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ErrResidentClosed is returned by SubmitInput/Enqueue after Close.
var ErrResidentClosed = errors.New("codexdriver: resident session closed")

// ResidentSession is a supervised, reconnecting owner of one logical Codex
// session. It satisfies handler.InputPort: the stable port the input queue
// drains into.
type ResidentSession struct {
	sub   *codexSubstrate
	spawn handler.SubstrateSpawn
	queue *BoundedInputQueue

	mu          sync.Mutex
	cur         *codexSession // current live child session (nil until first submit)
	threadID    string        // last-known server thread id, for resume-on-respawn
	closed      bool
	supervising bool          // a Supervise watchdog goroutine is running
	closeCh     chan struct{} // closed by Close to stop the watchdog
}

var _ handler.InputPort = (*ResidentSession)(nil)

// NewResidentSession builds a resident owner over a fresh codex substrate
// (Options) that respawns from the given spawn params. queueCap bounds the G3
// input-queue backlog (submissions buffered while a turn is in flight); it is
// clamped to >=1 by the queue. The owner spawns no child until the first
// submission is drained.
func NewResidentSession(opts Options, spawn handler.SubstrateSpawn, queueCap int) *ResidentSession {
	sub, ok := NewCodexSubstrate(opts).(*codexSubstrate)
	if !ok {
		// Construction-time invariant: NewCodexSubstrate always returns
		// *codexSubstrate. Fail loud here rather than nil-deref later in
		// spawnLocked if that factory's return type ever changes.
		panic("codexdriver: NewCodexSubstrate did not return *codexSubstrate")
	}
	r := &ResidentSession{sub: sub, spawn: spawn, closeCh: make(chan struct{})}
	// The queue's single drainer calls r.SubmitInput — the production caller that
	// gives G3 its live consumer.
	r.queue = NewBoundedInputQueue(r, queueCap)
	return r
}

// Enqueue offers one submission to the bounded backlog (G3). The returned
// channel resolves once the drainer delivers it through SubmitInput (respawn +
// resume handled transparently). Returns ErrQueueFull at capacity, or
// ErrResidentClosed after Close.
func (r *ResidentSession) Enqueue(ctx context.Context, req handler.InputRequest) (<-chan QueuedResult, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrResidentClosed
	}
	r.mu.Unlock()
	ch, err := r.queue.Enqueue(ctx, req)
	if errors.Is(err, ErrQueueClosed) {
		return nil, ErrResidentClosed
	}
	return ch, err
}

// SubmitInput is the stable InputPort the queue drains into. It ensures a live
// child, delivers the submission, and — if the child died (ErrSessionClosed) —
// respawns (resuming the prior thread when known) and retries exactly once. On a
// successful submit it refreshes the retained thread id so the NEXT respawn can
// resume.
func (r *ResidentSession) SubmitInput(ctx context.Context, req handler.InputRequest) (handler.Ack, error) {
	sess, err := r.ensure(ctx)
	if err != nil {
		return handler.Ack{}, err
	}

	ack, err := sess.SubmitInput(ctx, req)
	if errors.Is(err, ErrSessionClosed) {
		// The child died at or before this submission. Revive once and retry;
		// the retained thread id (if any) drives a thread/resume re-attach.
		sess, err = r.revive(ctx, sess)
		if err != nil {
			return handler.Ack{}, err
		}
		ack, err = sess.SubmitInput(ctx, req)
	}
	if err == nil {
		r.rememberThread(sess)
	}
	return ack, err
}

// Watchdog backoff bounds: after a child death the watchdog waits before
// respawning, growing the delay on rapid successive deaths so a crash-looping
// child does not spin-respawn hot. A healthy child (one that reached Ready)
// resets the delay to the floor.
const (
	watchdogBackoffMin = 100 * time.Millisecond
	watchdogBackoffMax = 5 * time.Second
)

// Supervise starts a proactive liveness watchdog owning the resident session: it
// brings up a warm child immediately and respawns it on death — resuming the
// retained thread — so a live, resumed session is ready BEFORE the next submit
// rather than only revived lazily on demand (G1b). It latches the thread id once
// each (re)spawned child reaches Ready, so continuity holds even across a death
// with no intervening submit. In-turn stale liveness stays owned by the
// codexinput reactor (AIS-INV-001); this watchdog owns only the cross-death /
// idle-child liveness the per-turn timers do not cover.
//
// Idempotent and non-blocking: it returns immediately and the goroutine runs
// until Close or ctx cancellation.
func (r *ResidentSession) Supervise(ctx context.Context) {
	r.mu.Lock()
	if r.closed || r.supervising {
		r.mu.Unlock()
		return
	}
	r.supervising = true
	r.mu.Unlock()
	go r.superviseLoop(ctx)
}

func (r *ResidentSession) superviseLoop(ctx context.Context) {
	clock := r.sub.opts.Clock
	backoff := watchdogBackoffMin
	for {
		if r.isClosed() || ctx.Err() != nil {
			return
		}
		sess, err := r.ensure(ctx)
		if err != nil {
			// Closed, or the (re)spawn failed (e.g. a fail-closed PreSpawn guard).
			// Back off and retry unless shutting down.
			if r.isClosed() || !r.backoffSleep(ctx, clock, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		// Wait until the child is Ready so we can latch its thread id (a fresh
		// thread/start id, or the confirmed resumed id). A non-nil error means it
		// died/failed before Ready — fall through to the death wait, which will see
		// loopDone and respawn.
		if err := sess.awaitReady(ctx); err == nil {
			r.rememberThread(sess)
			backoff = watchdogBackoffMin // healthy: reset the crash-loop backoff
		} else if ctx.Err() != nil {
			return
		}
		select {
		case <-sess.loopDone:
			// Child died. Back off (crash-loop guard) then loop to respawn+resume.
			if r.isClosed() || !r.backoffSleep(ctx, clock, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
		case <-r.closeCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// backoffSleep waits for d, returning false (stop) if the watchdog should exit —
// on ctx cancel OR Close (closeCh). Unlike a bare ClockPort.Sleep it also wakes
// on closeCh, so Close stops the watchdog promptly instead of lingering up to a
// full backoff interval.
func (r *ResidentSession) backoffSleep(ctx context.Context, clock substrate.ClockPort, d time.Duration) bool {
	tk := clock.NewTicker(d)
	defer tk.Stop()
	select {
	case <-tk.C():
		return true
	case <-r.closeCh:
		return false
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	n := cur * 2
	if n > watchdogBackoffMax {
		return watchdogBackoffMax
	}
	return n
}

func (r *ResidentSession) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// CloseInput signals end-of-input to the current child (InputPort contract). It
// forwards to the live child's CloseInput and does NOT respawn — a resident
// owner that is done submitting for now closes the current turn stream; full
// teardown is Close. A no-op when no child is live.
func (r *ResidentSession) CloseInput(ctx context.Context) error {
	r.mu.Lock()
	cur := r.cur
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return ErrResidentClosed
	}
	if cur == nil {
		return nil
	}
	return cur.CloseInput(ctx)
}

// ensure returns a live child, spawning one (fresh, or resuming the retained
// thread) when none exists or the current one has wound down.
func (r *ResidentSession) ensure(ctx context.Context) (*codexSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrResidentClosed
	}
	if r.cur != nil && !sessionDead(r.cur) {
		return r.cur, nil
	}
	return r.spawnLocked(ctx)
}

// revive replaces a dead child. It is a no-op re-fetch if another concurrent
// caller already revived past `dead` (the queue serializes callers, so this is
// belt-and-suspenders for the InputPort contract).
func (r *ResidentSession) revive(ctx context.Context, dead *codexSession) (*codexSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrResidentClosed
	}
	if r.cur != dead && r.cur != nil && !sessionDead(r.cur) {
		return r.cur, nil
	}
	return r.spawnLocked(ctx)
}

// spawnLocked spawns a new child under r.mu and adopts it as current. When a
// prior thread id is known it re-attaches via the G1a thread/resume handshake;
// otherwise it opens a fresh thread. The prior (dead) session is left for its
// own goroutines to finalize — its child is already gone.
func (r *ResidentSession) spawnLocked(ctx context.Context) (*codexSession, error) {
	sess, err := r.sub.spawn(ctx, r.spawn, r.threadID)
	if err != nil {
		return nil, fmt.Errorf("codexdriver: resident respawn: %w", err)
	}
	cs, ok := sess.(*codexSession)
	if !ok {
		// spawn always returns *codexSession; guard the type assertion so a future
		// refactor fails loud rather than nil-panicking.
		_ = sess.Kill(ctx)
		return nil, fmt.Errorf("codexdriver: resident respawn: unexpected session type %T", sess)
	}
	r.cur = cs
	return cs, nil
}

// rememberThread latches the child's current thread id so a later respawn can
// resume it. Called after a successful submit, by which point the handshake has
// stamped the id.
func (r *ResidentSession) rememberThread(sess *codexSession) {
	if id := sess.currentThreadID(); id != "" {
		r.mu.Lock()
		r.threadID = id
		r.mu.Unlock()
	}
}

// ThreadID returns the last-known server thread id (empty before the first
// handshake). Observability / tests.
func (r *ResidentSession) ThreadID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threadID
}

// Close stops accepting work, drains the queue's buffered submissions through
// the current child, then kills that child. Idempotent.
func (r *ResidentSession) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.closeCh) // stop the watchdog (first-time only; closed guards idempotency)
	r.mu.Unlock()

	// Drain buffered submissions (graceful FIFO) before tearing the child down.
	r.queue.Close()

	r.mu.Lock()
	cur := r.cur
	r.cur = nil
	r.mu.Unlock()
	if cur == nil {
		return nil
	}
	if err := cur.Kill(ctx); err != nil {
		return err
	}
	return cur.Wait(ctx)
}

// sessionDead reports whether the child's reactor loop has exited (child gone or
// wire closed). loopDone is closed by finalize on wind-down.
func sessionDead(s *codexSession) bool {
	select {
	case <-s.loopDone:
		return true
	default:
		return false
	}
}
