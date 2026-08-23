package codexdriver

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
		//nolint:forbidigo // construction-time invariant: NewCodexSubstrate always returns *codexSubstrate; fail loud rather than nil-deref later (constructor returns no error to thread)
		panic("codexdriver: NewCodexSubstrate did not return *codexSubstrate")
	}
	r := &ResidentSession{sub: sub, spawn: spawn, closeCh: make(chan struct{})}
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
			if r.isClosed() || !r.backoffSleep(ctx, clock, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if err := sess.awaitReady(ctx); err == nil {
			r.rememberThread(sess)
			backoff = watchdogBackoffMin // healthy: reset the crash-loop backoff
		} else if ctx.Err() != nil {
			return
		}
		select {
		case <-sess.loopDone:
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

func (r *ResidentSession) spawnLocked(ctx context.Context) (*codexSession, error) {
	sess, err := r.sub.spawn(ctx, r.spawn, r.threadID)
	if err != nil {
		return nil, fmt.Errorf("codexdriver: resident respawn: %w", err)
	}
	cs, ok := sess.(*codexSession)
	if !ok {
		_ = sess.Kill(ctx) //nolint:errcheck // best-effort teardown of the mis-typed session on an error return; the type-assertion failure is the reported error
		return nil, fmt.Errorf("codexdriver: resident respawn: unexpected session type %T", sess)
	}
	r.cur = cs
	return cs, nil
}

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

func sessionDead(s *codexSession) bool {
	select {
	case <-s.loopDone:
		return true
	default:
		return false
	}
}
