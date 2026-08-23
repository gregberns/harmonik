// Package daemon — tmuxsubstrate: concrete Substrate implementation (hk-gql20.11).
//
// tmuxSubstrate bridges handler.Substrate → tmux.Adapter. It lives in the
// daemon package (composition root) so that internal/handler never imports
// internal/lifecycle/tmux — that cross-import is forbidden by the depguard
// component-matrix (subsystem-organization.md; lifecycle-tmux rule).
//
// The daemon composition root constructs a tmuxSubstrate via NewTmuxSubstrate
// and injects it into handler.LaunchSpec.Substrate for agent_type sessions that
// require tmux hosting. Twin sessions continue to use the exec.CommandContext
// path (nil Substrate).
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021b — "Substrate seam";
// design §4 component-2/design.md §4 "Substrate seam".
// Bead: hk-gql20.11.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const (
	exitCodeClean = 0

	exitCodeUnknown = -1
)

type pasteInjecter interface {
	// WriteLastPane delivers payload to the pane spawned by this run's
	// SpawnWindow call.  bufferName MUST follow the "harmonik-<session-id>-<purpose>"
	// format required by PL-021d.  Returns a non-nil error if no window has
	// been spawned yet or if the underlying WriteToPane call fails.
	WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
}

type tmuxSubstrate struct {
	adapter     tmux.Adapter
	sessionName string

	// newWindowMu serializes the underlying `tmux new-window` exec across the
	// whole daemon (hk-oihnf). All implementer/reviewer child windows are created
	// in the same shared tmux session (sessionName, immutable), so two concurrent
	// `tmux new-window` invocations contend on the tmux server's GLOBAL command
	// lock: one serializes behind the other and can crawl ~16 min behind under
	// MaxConcurrent>1. A single bead never collides. Holding this mutex around the
	// bounded new-window call (and ONLY that call — not the semaphore acquire or
	// spec-build) makes window creation strictly one-at-a-time daemon-wide,
	// eliminating the contention. The 60 s new-window bound caps how long a hung
	// new-window can hold the mutex: the bound fires, the call returns, and the
	// mutex is released, so a single wedge cannot block all other launches forever.
	newWindowMu sync.Mutex

	// spawnedMu guards spawnedWindows.
	spawnedMu sync.Mutex
	// spawnedWindows accumulates the WindowHandle (and the adapter it was
	// created through — the local adapter for local runs, the SSH-backed remote
	// adapter for worker-hosted runs) of every window created by SpawnWindow
	// during this daemon instance's lifetime. KillAllWindows iterates this slice
	// to clean up orphan windows on wave completion or daemon exit, killing each
	// window via the adapter that spawned it so remote windows are not leaked.
	// Entries are appended-only; no removal on individual Kill calls (KillWindow
	// is idempotent on a non-existent window so re-killing is harmless).
	spawnedWindows []spawnedWindow

	// spawnSem, when non-nil, is a resizable counting semaphore of capacity
	// cap+1 that bounds the total number of concurrently active sessions. The
	// +1 is the reserved slot for terminal/consolidate nodes (hk-x882o). Nil
	// when no cap is configured (WithSpawnCap was not passed).
	//
	// hk-omvan: capacity is live-resizable via SetSpawnCap — unlike a fixed
	// buffered channel, a *resizableSemaphore's capacity can grow (or shrink)
	// without recreating it, so the operator can raise real throughput with
	// `queue set-concurrency` and no daemon restart.
	//
	// Bead ref: hk-xb5yi (concurrent-spawn cap), hk-x882o (terminal reserve),
	// hk-omvan (live resize).
	spawnSem *resizableSemaphore

	// nonTerminalSem, when non-nil, gates ordinary (non-terminal) spawns to the
	// user-configured cap. It has capacity cap — one fewer slot than spawnSem —
	// so that non-terminal sessions can never occupy the reserved +1 slot in
	// spawnSem. Terminal spawns bypass nonTerminalSem entirely and draw from
	// the reserved slot. Nil when no cap is configured.
	//
	// Bead ref: hk-x882o, hk-omvan (live resize).
	nonTerminalSem *resizableSemaphore

	// capResizeMu serializes SetSpawnCap against itself, so two operators
	// resizing at once cannot interleave their two capacity moves and leave the
	// bounds crossed. It is taken on the resize path ONLY — never on the spawn
	// or teardown paths, which must not queue behind an operator command — and
	// it is never held while waiting on a semaphore, so it cannot take part in
	// a cycle.
	//
	// Bead ref: hk-pcjkp.
	capResizeMu sync.Mutex

	// capResizeMid, when non-nil, runs between the two capacity moves of a
	// resize. It exists so a test can hold that window open and observe the
	// state inside it: the window is microseconds wide in production and
	// hk-pcjkp cannot be reproduced from outside the substrate without it.
	// Nil everywhere except the tests that set it through the export file.
	//
	// Bead ref: hk-pcjkp.
	capResizeMid func()

	// spawnSemWaits counts the times a non-terminal spawn has missed the
	// fast-path TryAcquire in acquireSpawnSlot and fallen into
	// awaitSpawnSemHoldingNonTerminal — the bounded wait for a spawnSem slot
	// that the reserve was supposed to make unnecessary.
	//
	// It is a test seam. Production reads it nowhere, and it must not start
	// deciding anything: the increment is a plain counter on a path that is
	// already about to block, so it changes no behaviour and costs one atomic
	// add per slow acquire.
	//
	// What it buys is the only way to see this condition from outside the
	// substrate. Since hk-terminal-reserve-unbounded-wyy6y the wait normally
	// SUCCEEDS, so a spawn that took the slow path and one that took the fast
	// path return the same value, and only their latency differs. A test that
	// tells them apart by outcome cannot; a test that tells them apart by wall
	// clock is a load-flaky test waiting to happen. The entry count is exact
	// and load-independent. Without it the raise-order guard against hk-pcjkp
	// was vacuous — a woken spawn refused capacity it had just been granted
	// still started, so the test stayed green (hk-6zv97).
	//
	// Bead ref: hk-pcjkp (the ordering rule), hk-6zv97 (this counter).
	spawnSemWaits atomic.Uint64

	// spawnAcquireTimeout bounds how long SpawnWindow waits for a free spawn
	// slot before treating the launch as failed (hk-4l7zs). A run sitting at
	// launch_initiated forever (no tmux session, no implementer_phase_complete)
	// then failing no_commit at the 30-min timeout was traced to SpawnWindow
	// blocking indefinitely on a leaked semaphore. Bounding the wait converts an
	// indefinite hang into a prompt, observable launch failure.
	//
	// Zero or negative disables the timeout (blocks until ctx is cancelled, the
	// pre-hk-4l7zs behaviour). Set via WithSpawnAcquireTimeout.
	spawnAcquireTimeout time.Duration

	// spawnCapBlocked, when non-nil, is invoked once when SpawnWindow cannot
	// acquire a slot within spawnAcquireTimeout. It is a diagnostic hook the
	// daemon wires to emit a spawn_cap_blocked event (hk-4l7zs). waited is the
	// duration spent blocked; inUse / capSize describe the semaphore at the
	// moment of the timeout. Nil in tests that do not need the hook.
	spawnCapBlocked func(waited time.Duration, inUse, capSize int)

	// newWindowTimeout bounds how long a tmux CREATION call may run before the
	// caller gives up on it. It covers both shapes of creation this substrate
	// performs: `tmux new-window` (adapter.NewWindowIn, via callNewWindowBounded)
	// and `tmux new-session` (sessionCreator.NewSessionIn, via
	// callNewSessionBounded). A hung tmux invocation otherwise blocks its caller
	// indefinitely — SpawnWindow → handler.Launch for the window shape, and a
	// whole bead run or an operator's crew start for the session shape. One bound
	// serves both because the hazard is identical: the shell call has no inherent
	// timeout and the tmux server holds a global command lock.
	//
	// Zero or negative disables the bound (blocks until ctx is cancelled).
	// NewTmuxSubstrate applies defaultNewWindowTimeout when unset. Set via
	// WithNewWindowTimeout.
	newWindowTimeout time.Duration

	// newWindowTimedOut, when non-nil, is invoked once when a tmux creation call
	// does not return within newWindowTimeout — either `tmux new-window` or
	// `tmux new-session`. It is a diagnostic hook the daemon wires to emit a
	// tmux_new_window_timeout event (hk-r1rup). waited is the duration spent
	// blocked. Nil in tests that do not need the hook.
	newWindowTimedOut func(waited time.Duration)

	// spawnStagger, when positive, enforces a minimum interval between consecutive
	// window-creation calls (hk-hzj). Under a concurrent dispatch burst multiple
	// agent cold-starts compete for disk I/O and CPU simultaneously; spacing window
	// creation by spawnStagger reduces the peak contention window. Zero disables
	// staggering (the pre-hk-hzj behaviour). Set via WithSpawnStagger.
	//
	// lastWindowAt records when callNewWindowBounded last STOPPED work on a window
	// creation — either the create call returned, or the caller stopped waiting
	// for it. What it is never again is the moment a creation STARTED: charging
	// the creation's own duration to the next window's stagger is the hk-mirga
	// defect. On the two abandoned paths (the new-window bound fired, or the
	// caller's ctx was cancelled mid-create) the tmux client runs on past this
	// stamp, so there the stamp is EARLY and the interval it buys is a floor
	// rather than proof the tmux server has gone quiet. Both fields are accessed
	// only inside callNewWindowBounded while newWindowMu is held, so they need no
	// additional lock.
	//
	// Bead refs: hk-hzj, hk-mirga.
	spawnStagger time.Duration // set via WithSpawnStagger
	lastWindowAt time.Time     // guarded by newWindowMu; stamped when a creation attempt ends

	// projectHash project-qualifies crew session names per fleet-portability T2:
	// "harmonik-<projectHash>-crew-<name>". Set via WithCrewProjectHash. Required
	// for crew spawning: crewSessionName errors when it is empty (the legacy
	// no-hash "hk-crew-<name>" form was deleted in hk-rmy1, slice C). In
	// production both daemon construction sites always set it.
	projectHash core.ProjectHash

	// keepaliveEnabled, when true, signals that the daemon owns the spawn-target
	// session and must keep it alive for its entire lifetime. Set via
	// WithSessionKeepalive. The daemon calls RunSessionKeepalive as a background
	// goroutine when this flag is set (hk-9ptu).
	keepaliveEnabled bool

	// keepaliveInterval is the period between EnsureSession probes in
	// RunSessionKeepalive. Zero means use defaultSessionKeepaliveInterval.
	keepaliveInterval time.Duration
}

// TmuxSubstrateOption is a functional option for NewTmuxSubstrate.
//
// Bead ref: hk-xb5yi.
type TmuxSubstrateOption func(*tmuxSubstrate)

// WithSpawnCap sets a hard ceiling on the number of concurrently active tmux
// windows spawned by this substrate. When n > 0 each SpawnWindow call acquires
// a slot; the slot is released when the session's Kill is called. If all n
// slots are occupied, SpawnWindow blocks until a slot is freed or the context
// is cancelled.
//
// A value of 0 (or negative) disables the cap (no-op option).
//
// Typical production default: maxConcurrent*2 (one implementer + one reviewer
// per in-flight bead). Override via HARMONIK_MAX_CONCURRENT_SESSIONS env var,
// or raise it live post-startup via SetSpawnCap (hk-omvan).
//
// hk-x882o: internally the semaphore is sized at n+1 and a second semaphore of
// size n gates non-terminal spawns. The +1 slot is reserved exclusively for
// terminal/consolidate nodes so that a completed+reviewed run can always get
// its final merge node scheduled even when all ordinary slots are occupied.
//
// Bead ref: hk-xb5yi, hk-x882o.
func WithSpawnCap(n int) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		if n > 0 {
			s.spawnSem = newReservingSemaphore(n + 1) // +1 reserved for terminal spawns (hk-x882o)
			s.nonTerminalSem = newResizableSemaphore(n)
		}
	}
}

// SetSpawnCap live-resizes the substrate's spawn cap to n non-terminal slots
// (hk-omvan). It resizes nonTerminalSem's capacity to n and spawnSem's
// capacity to n+1, preserving the hk-x882o invariant that a terminal spawn
// always has a reserved slot even when all n non-terminal slots are occupied.
//
// A no-op when no cap was configured at construction (WithSpawnCap was not
// passed — spawnSem is nil) or when n <= 0: SetSpawnCap can only resize an
// existing cap, not install one where none exists. Growing the cap wakes any
// SpawnWindow call currently blocked in acquireSpawnSlot so it can proceed
// immediately against the new capacity, without waiting for an unrelated
// slot to free up.
//
// The daemon wires this through daemon.Config so `queue set-concurrency` can
// raise the cap to max(currentCap, N*2) instead of refusing an oversubscribing
// request (see HandleQueueSetConcurrency in internal/queue/rpc.go).
//
// # Order and reserve
//
// The two capacities are moved in the order that keeps
// spawnSem.Capacity() >= nonTerminalSem.Capacity()+1 true at every instant, not
// only at the ends: widen from the inside out, narrow from the outside in.
// SetCapacity broadcasts to waiters, so moving the non-terminal bound first on
// a raise wakes every blocked spawn against a spawnSem that still holds the old
// capacity. Asking for MORE capacity was the case that refused spawns
// (hk-pcjkp): those woken spawns missed the fast-path-only TryAcquire in
// acquireSpawnSlot and died on a structural error. That outcome is gone —
// acquireSpawnSlot now waits out the rest of its budget instead — so what the
// order costs a woken NON-TERMINAL spawn is latency rather than its life.
//
// The order still protects liveness, and that is the reason to keep it. Inside
// the wrong-order window the reserve does not exist, because
// spawnSem.Capacity() is not yet above nonTerminalSem.Capacity(). A TERMINAL
// spawn arriving there loses the slot it is guaranteed and falls into the
// unbounded terminal wait. That has not changed, and it is worse than the
// latency.
//
// spawnSem is resized through SetCapacityKeepingOneFree so a shrink can never
// take the reserved slot away from work already in flight (hk-6yrs9). The
// direction test compares n against the NON-TERMINAL capacity, not spawnSem's:
// a shrink held above n+1 by that reserve would otherwise read the next raise
// as a lowering.
//
// Asking for the cap it already holds does nothing. That case returns before
// either bound moves, because the shrink path would otherwise re-clamp
// spawnSem one slot higher every time it ran while the reserve was occupied.
//
// Bead ref: hk-omvan (follow-up to hk-vfeeo), hk-6yrs9, hk-pcjkp.
func (s *tmuxSubstrate) SetSpawnCap(n int) {
	if s.spawnSem == nil || s.nonTerminalSem == nil || n <= 0 {
		return
	}
	s.capResizeMu.Lock()
	defer s.capResizeMu.Unlock()

	if n == s.nonTerminalSem.Capacity() {
		return
	}
	if n > s.nonTerminalSem.Capacity() {
		s.spawnSem.SetCapacityKeepingOneFree(n + 1)
		s.capResizeGap()
		s.nonTerminalSem.SetCapacity(n)
		return
	}
	s.nonTerminalSem.SetCapacity(n)
	s.capResizeGap()
	s.spawnSem.SetCapacityKeepingOneFree(n + 1)
}

func (s *tmuxSubstrate) capResizeGap() {
	if s.capResizeMid != nil {
		s.capResizeMid()
	}
}

var errSemaphoreAcquireTimeout = errors.New("daemon: resizableSemaphore: acquire timed out")

type resizableSemaphore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	capacity int
	inUse    int

	// keepsOneFree marks a semaphore that SetCapacityKeepingOneFree will not
	// resize to or below its in-use count (hk-6yrs9). Read the bound as being
	// on that one method and not on the semaphore: ordinary acquires still take
	// inUse all the way up to capacity, and plain SetCapacity will drop it
	// anywhere at all. So this does not promise a privileged caller a free slot
	// at every instant. What it promises is that lowering the cap through the
	// method that respects the reserve will not take a slot away from work that
	// already holds one.
	// The spawn semaphore is one: the slot it keeps free is the terminal
	// reserve, and the terminal path waits for it with no timeout, so losing
	// the slot is an unbounded stall rather than a slow spawn. The non-terminal
	// semaphore is NOT one — its capacity is the operator's own number and
	// SpawnCapSize reports it verbatim.
	keepsOneFree bool

	// desired is the capacity asked for by the last SetCapacityKeepingOneFree.
	// It is what capacity returns to as slots are released, and it is read only
	// when keepsOneFree is set.
	desired int
}

func newResizableSemaphore(capacity int) *resizableSemaphore {
	sem := &resizableSemaphore{capacity: capacity}
	sem.cond = sync.NewCond(&sem.mu)
	return sem
}

func newReservingSemaphore(capacity int) *resizableSemaphore {
	sem := newResizableSemaphore(capacity)
	sem.keepsOneFree = true
	sem.desired = capacity
	return sem
}

// TryAcquire acquires a slot without blocking. Returns true and increments
// inUse if a slot was free; returns false (no state change) otherwise.
func (s *resizableSemaphore) TryAcquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inUse < s.capacity {
		s.inUse++
		return true
	}
	return false
}

// Acquire blocks until a slot frees up, ctx is cancelled, or timeout elapses,
// whichever comes first. timeout <= 0 disables the timeout bound (only ctx
// cancellation can interrupt the wait — the pre-hk-4l7zs terminal-spawn
// behaviour). Returns ctx.Err() on cancellation, errSemaphoreAcquireTimeout on
// timeout, or nil once a slot has been acquired (inUse is incremented before
// returning nil).
func (s *resizableSemaphore) Acquire(ctx context.Context, timeout time.Duration) error {
	if s.TryAcquire() {
		return nil
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-stop:
		}
	}()

	var timedOut atomic.Bool
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() {
			timedOut.Store(true)
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		})
		defer timer.Stop()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.inUse >= s.capacity {
		if err := ctx.Err(); err != nil {
			return err
		}
		if timedOut.Load() {
			return errSemaphoreAcquireTimeout
		}
		s.cond.Wait()
	}
	s.inUse++
	return nil
}

// Release returns one slot to the semaphore and wakes any blocked Acquire /
// waiter goroutine to re-check its exit condition. A no-op (no negative inUse)
// if called with no slot currently held.
func (s *resizableSemaphore) Release() {
	s.mu.Lock()
	if s.inUse > 0 {
		s.inUse--
	}
	if s.keepsOneFree {
		s.capacity = capacityKeepingOneFree(s.desired, s.inUse)
	}
	s.mu.Unlock()
	s.cond.Broadcast()
}

// SetCapacity live-resizes the semaphore (hk-omvan) and wakes every blocked
// Acquire so a raised capacity is picked up immediately rather than only once
// an unrelated slot happens to free up.
//
// Use it only on a plain semaphore. On a reserving one it sets the capacity but
// leaves desired holding the older number, and the next Release quietly resizes
// the semaphore back to that stale value. No caller does this today — spawnSem
// is resized through SetCapacityKeepingOneFree — and the two methods sitting on
// one type is what makes it reachable at all (hk-setcapacity-on-reserving-sem-shpak).
func (s *resizableSemaphore) SetCapacity(n int) {
	s.mu.Lock()
	s.capacity = n
	s.mu.Unlock()
	s.cond.Broadcast()
}

// SetCapacityKeepingOneFree live-resizes a reserving semaphore to n without
// ever dropping its capacity to or below what is in use, so the slot its
// privileged caller draws on survives the resize (hk-6yrs9).
//
// Plain SetCapacity sets the capacity to n whatever is in flight. Drop the
// spawn cap from 16 to 2 with 16 sessions running and the capacity (3) sits
// below the in-use count (16): the reserve is gone, and every merge node of a
// finished run waits in the terminal path — which has NO timeout — until
// fourteen sessions drain. That is the starvation hk-x882o removed, reached
// from the other direction, and it arrives exactly when the operator is
// throttling a box that is already overloaded.
//
// n is remembered, and Release returns the capacity to it as the excess slots
// come back. Calling this on a semaphore built by newResizableSemaphore sets
// the capacity but nothing restores it, so use it only on a reserving one.
func (s *resizableSemaphore) SetCapacityKeepingOneFree(n int) {
	s.mu.Lock()
	s.desired = n
	s.capacity = capacityKeepingOneFree(n, s.inUse)
	s.mu.Unlock()
	s.cond.Broadcast()
}

func capacityKeepingOneFree(n, inUse int) int {
	if inUse >= n {
		return inUse + 1
	}
	return n
}

// Capacity reports the current configured capacity.
func (s *resizableSemaphore) Capacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacity
}

// InUse reports the number of slots currently held.
func (s *resizableSemaphore) InUse() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inUse
}

// ErrSpawnCapTimeout is the sentinel wrapped by SpawnWindow when a non-terminal
// spawn cannot acquire a slot within spawnAcquireTimeout (hk-4l7zs, hk-x882o).
// Terminal spawns never return this error — they use a ctx-bounded wait that
// draws from the reserved +1 slot. The daemon launch paths detect it via
// errors.Is to emit a spawn_cap_blocked event with run context. It is also
// wrapped with handler.ErrStructural so existing structural-error handling
// continues to apply.
var ErrSpawnCapTimeout = errors.New("daemon: spawn cap acquire timed out")

const defaultSpawnAcquireTimeout = 2 * time.Minute

// ErrTmuxNewWindowTimeout is the sentinel wrapped by SpawnWindow when the
// underlying `tmux new-window` shell call (adapter.NewWindowIn) does not return
// within newWindowTimeout (hk-r1rup). The daemon launch paths detect it via
// errors.Is to emit a tmux_new_window_timeout event with run context. It is also
// wrapped with handler.ErrStructural so existing structural-error handling
// (reopen-the-bead) continues to apply.
//
// This is DISTINCT from ErrSpawnCapTimeout (hk-4l7zs), which fires when the
// spawn-semaphore acquire saturates (a slot leak), not when the new-window call
// itself hangs.
var ErrTmuxNewWindowTimeout = errors.New("daemon: tmux new-window timed out (possible hung tmux invocation)")

// ErrTmuxNewSessionTimeout is the sentinel wrapped by SpawnRunSession and
// SpawnCrewSession when the underlying `tmux new-session` shell call
// (sessionCreator.NewSessionIn) does not return within newWindowTimeout. Like
// ErrTmuxNewWindowTimeout it is also wrapped with handler.ErrStructural, so the
// existing structural-error handling (reopen-the-bead, fail the crew start)
// continues to apply.
//
// It is a DISTINCT sentinel from ErrTmuxNewWindowTimeout because the two name
// different failures with different blast radii. A hung `tmux new-window` wedges
// one launch inside the daemon's shared session. A hung `tmux new-session`
// wedges the creation of a whole independent session: for SpawnRunSession that
// is the bead run itself, and for SpawnCrewSession it is an operator waiting on
// a crew start. A caller that wants to tell the two apart can.
var ErrTmuxNewSessionTimeout = errors.New("daemon: tmux new-session timed out (possible hung tmux invocation)")

const defaultNewWindowTimeout = 60 * time.Second

// WithSpawnAcquireTimeout sets the bound on how long SpawnWindow blocks waiting
// for a free spawn slot before treating the launch as failed (hk-4l7zs).
//
// A value <= 0 disables the timeout (blocks until ctx is cancelled — the
// pre-hk-4l7zs behaviour). When unset, NewTmuxSubstrate applies
// defaultSpawnAcquireTimeout whenever a spawn cap is configured.
func WithSpawnAcquireTimeout(d time.Duration) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.spawnAcquireTimeout = d
	}
}

// WithSpawnCapBlockedHook installs a diagnostic callback invoked when
// SpawnWindow times out waiting for a spawn slot (hk-4l7zs). The daemon wires
// this to emit a spawn_cap_blocked event.
func WithSpawnCapBlockedHook(fn func(waited time.Duration, inUse, capSize int)) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.spawnCapBlocked = fn
	}
}

// WithNewWindowTimeout sets the bound on how long a tmux CREATION call may run
// before its caller gives up on it (hk-r1rup). It bounds both `tmux new-window`
// (adapter.NewWindowIn, used by SpawnWindow) and `tmux new-session`
// (sessionCreator.NewSessionIn, used by SpawnRunSession and SpawnCrewSession) —
// see the newWindowTimeout field for why one bound serves both.
//
// A value <= 0 disables the bound (blocks until ctx is cancelled — the
// pre-hk-r1rup behaviour). When unset, NewTmuxSubstrate applies
// defaultNewWindowTimeout.
func WithNewWindowTimeout(d time.Duration) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.newWindowTimeout = d
	}
}

// WithNewWindowTimedOutHook installs a diagnostic callback invoked when the
// `tmux new-window` call does not return within newWindowTimeout (hk-r1rup). The
// daemon wires this to emit a tmux_new_window_timeout event.
func WithNewWindowTimedOutHook(fn func(waited time.Duration)) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.newWindowTimedOut = fn
	}
}

// WithSpawnStagger sets the minimum interval between consecutive tmux window
// creations (hk-hzj). Under a concurrent dispatch burst multiple claude agents
// cold-start simultaneously and compete for disk I/O and CPU. Spreading window
// creation by d reduces the peak contention window and prevents agent_ready
// timeouts caused by resource starvation during cold-start.
//
// A value <= 0 disables staggering (the default — SpawnWindow creates windows as
// fast as the new-window mutex and semaphore allow). Production operators should
// tune this based on observed agent_ready_timeout events under concurrent load.
// A value of 2–5 seconds is a reasonable starting point for --max-concurrent ≥ 4
// on a disk-heavy box; 0 is correct for fast NVMe with low utilisation.
//
// The stagger is enforced inside callNewWindowBounded while newWindowMu is held,
// so at least d passes between one window creation RETURNING and the next one
// starting, regardless of how many goroutines are concurrently waiting to spawn
// and regardless of how long a creation itself takes. The wait uses the caller's
// context, so an operator SIGTERM cancels a pending stagger and returns
// ErrStructural.
//
// Bead refs: hk-hzj, hk-mirga.
func WithSpawnStagger(d time.Duration) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		if d > 0 {
			s.spawnStagger = d
		}
	}
}

// WithCrewProjectHash sets the project hash used to project-qualify crew session
// names: "harmonik-<projectHash>-crew-<name>" (fleet-portability T2).
//
// This option is REQUIRED for crew spawning: when unset, crewSessionName returns
// an error rather than minting a session under the deleted legacy "hk-crew-<name>"
// name (hk-rmy1, slice C — one prefix family for the whole fleet). Both daemon
// construction sites pass it.
func WithCrewProjectHash(h core.ProjectHash) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.projectHash = h
	}
}

const defaultSessionKeepaliveInterval = 30 * time.Second

// WithSessionKeepalive marks the substrate as owning its spawn-target session
// and enables the proactive keepalive mechanism (hk-9ptu).
//
// When interval > 0 it overrides the default 30 s probe period. Pass 0 to use
// the default.
//
// Call this option ONLY when the daemon owns the session (needEnsureSession=true
// in main.go — the supervisor-revive or display-message-failure boot path).
// For the normal "live ambient session" path the session is already managed by
// the operator's tmux-start/shell and no keepalive is needed.
//
// Bead ref: hk-9ptu.
func WithSessionKeepalive(interval time.Duration) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		s.keepaliveEnabled = true
		if interval > 0 {
			s.keepaliveInterval = interval
		}
	}
}

// RunSessionKeepalive is the background keepalive loop for the daemon-owned
// spawn-target session (hk-9ptu). It calls EnsureSession on the adapter at
// a fixed interval until ctx is cancelled.
//
// This is the proactive complement to the reactive hk-yaj ErrNoSession
// self-heal in SpawnWindow. hk-yaj recovers the session when a SpawnWindow
// call hits ErrNoSession; RunSessionKeepalive prevents the vulnerability window
// where the session is dead and no SpawnWindow is in-flight to trigger the
// self-heal — keeping the session alive between dispatches.
//
// It is a no-op when the adapter does not implement sessionEnsurer (no tmux
// available, test stubs, etc.).
//
// daemon.Start starts this as a goroutine when cfg.Substrate implements
// substrateWithKeepalive (detected via keepaliveEnabled=true, which is set by
// WithSessionKeepalive).
//
// Bead ref: hk-9ptu.
func (s *tmuxSubstrate) RunSessionKeepalive(ctx context.Context) {
	if !s.keepaliveEnabled {
		return // WithSessionKeepalive was not passed; no keepalive for this substrate
	}
	se, ok := s.adapter.(sessionEnsurer)
	if !ok {
		return // adapter lacks EnsureSession; keepalive is a no-op
	}

	interval := s.keepaliveInterval
	if interval <= 0 {
		interval = defaultSessionKeepaliveInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ensureErr := se.EnsureSession(ctx, s.sessionName, ""); ensureErr != nil {
				slog.WarnContext(ctx, "daemon: tmux substrate: ensure session on keepalive tick",
					"err", ensureErr, "session", s.sessionName)
			}
		}
	}
}

func (s *tmuxSubstrate) setDiagnosticHooks(spawnCapBlocked func(waited time.Duration, inUse, capSize int), newWindowTimedOut func(waited time.Duration)) {
	if spawnCapBlocked != nil {
		s.spawnCapBlocked = spawnCapBlocked
	}
	if newWindowTimedOut != nil {
		s.newWindowTimedOut = newWindowTimedOut
	}
}

type substrateDiagnosticHookSetter interface {
	setDiagnosticHooks(spawnCapBlocked func(waited time.Duration, inUse, capSize int), newWindowTimedOut func(waited time.Duration))
}

var _ handler.Substrate = (*tmuxSubstrate)(nil)

var _ windowCleaner = (*tmuxSubstrate)(nil)

// NewTmuxSubstrate constructs a tmuxSubstrate that delegates to adapter and
// creates new windows in sessionName.
//
// adapter MUST be non-nil. sessionName MUST be non-empty.
//
// Optional TmuxSubstrateOption values may be passed to configure additional
// behaviour (e.g. WithSpawnCap for a concurrent-session ceiling).
//
// The daemon composition root calls NewTmuxSubstrate after ProbeTmux and
// ResolveSession have succeeded per the PL-005 startup sequence.
func NewTmuxSubstrate(adapter tmux.Adapter, sessionName string, opts ...TmuxSubstrateOption) handler.Substrate {
	if adapter == nil {
		panic("daemon: NewTmuxSubstrate: adapter is nil — daemon defect")
	}
	if sessionName == "" {
		panic("daemon: NewTmuxSubstrate: sessionName is empty — daemon defect")
	}
	sub := &tmuxSubstrate{
		adapter:     adapter,
		sessionName: sessionName,
	}
	for _, opt := range opts {
		opt(sub)
	}
	if sub.spawnSem != nil && sub.spawnAcquireTimeout == 0 {
		sub.spawnAcquireTimeout = defaultSpawnAcquireTimeout
	}
	if sub.newWindowTimeout == 0 {
		sub.newWindowTimeout = defaultNewWindowTimeout
	}
	return sub
}

// SpawnSlotsInUse reports the number of spawn-semaphore slots currently held.
//
// Returns 0 when no cap is configured (spawnSem is nil). This is an
// observability/diagnostic accessor (hk-4l7zs): the daemon and tests use it to
// detect slot leaks — a slot acquired by SpawnWindow that is never returned by
// Kill.
func (s *tmuxSubstrate) SpawnSlotsInUse() int {
	if s.spawnSem == nil {
		return 0
	}
	return s.spawnSem.InUse()
}

// SpawnCapSize reports the user-configured spawn-cap ceiling for non-terminal
// sessions (nonTerminalSem.Capacity()). Returns 0 when no cap is configured.
// Diagnostic accessor (hk-4l7zs). hk-x882o: returns the non-terminal cap (n),
// not the total semaphore capacity (n+1), so diagnostics report the value the
// operator configured rather than the internal oversized semaphore. hk-omvan:
// reflects live resizes made via SetSpawnCap, not just the construction-time
// value.
func (s *tmuxSubstrate) SpawnCapSize() int {
	if s.nonTerminalSem == nil {
		return 0
	}
	return s.nonTerminalSem.Capacity()
}

func substrateSpawnStats(sub handler.Substrate) (slotsInUse, capSize int) {
	switch t := sub.(type) {
	case *tmuxSubstrate:
		return t.SpawnSlotsInUse(), t.SpawnCapSize()
	case *perRunSubstrate:
		if t != nil && t.inner != nil {
			return t.inner.SpawnSlotsInUse(), t.inner.SpawnCapSize()
		}
	}
	return 0, 0
}

func (s *tmuxSubstrate) releaseSpawnSlotFor(terminal bool) {
	if s.spawnSem == nil {
		return
	}
	s.spawnSem.Release()
	if !terminal && s.nonTerminalSem != nil {
		s.nonTerminalSem.Release()
	}
}

func (s *tmuxSubstrate) makeReleaseSlotFn(terminal bool) func() {
	if s.spawnSem == nil {
		return func() {}
	}
	return func() {
		s.releaseSpawnSlotFor(terminal)
	}
}

func (s *tmuxSubstrate) acquireSpawnSlot(ctx context.Context, terminal bool) error {
	if s.spawnSem == nil {
		return nil
	}

	if !terminal && s.nonTerminalSem != nil {
		start := time.Now()
		if err := s.nonTerminalSem.Acquire(ctx, s.spawnAcquireTimeout); err != nil {
			if errors.Is(err, errSemaphoreAcquireTimeout) {
				waited := time.Since(start)
				if s.spawnCapBlocked != nil {
					s.spawnCapBlocked(waited, s.SpawnSlotsInUse(), s.SpawnCapSize())
				}
				return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: no slot within %s (cap=%d in_use=%d): %w: %w",
					s.spawnAcquireTimeout, s.SpawnCapSize(), s.SpawnSlotsInUse(), ErrSpawnCapTimeout, handler.ErrStructural)
			}
			return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
				err, handler.ErrStructural)
		}
		if s.spawnSem.TryAcquire() {
			return nil
		}
		return s.awaitSpawnSemHoldingNonTerminal(ctx, start)
	}

	if err := s.spawnSem.Acquire(ctx, 0); err != nil {
		return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
			err, handler.ErrStructural)
	}
	return nil
}

func (s *tmuxSubstrate) awaitSpawnSemHoldingNonTerminal(ctx context.Context, start time.Time) error {
	s.spawnSemWaits.Add(1)

	unbounded := s.spawnAcquireTimeout <= 0
	remaining := time.Duration(0)
	if !unbounded {
		remaining = s.spawnAcquireTimeout - time.Since(start)
		if remaining <= 0 {
			s.nonTerminalSem.Release()
			return s.spawnSemSaturatedErr(time.Since(start))
		}
	}

	if err := s.spawnSem.Acquire(ctx, remaining); err != nil {
		s.nonTerminalSem.Release()
		if errors.Is(err, errSemaphoreAcquireTimeout) {
			return s.spawnSemSaturatedErr(time.Since(start))
		}
		return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
			err, handler.ErrStructural)
	}
	return nil
}

func (s *tmuxSubstrate) spawnSemSaturatedErr(waited time.Duration) error {
	if s.spawnCapBlocked != nil {
		s.spawnCapBlocked(waited, s.SpawnSlotsInUse(), s.SpawnCapSize())
	}
	return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: no spawn slot within %s: the reserve is held by terminal spawns (spawn_sem_cap=%d spawn_sem_in_use=%d non_terminal_in_use=%d): %w: %w",
		s.spawnAcquireTimeout, s.spawnSem.Capacity(), s.spawnSem.InUse(), s.nonTerminalSem.InUse(),
		ErrSpawnCapTimeout, handler.ErrStructural)
}

// SpawnWindow creates a new tmux window in the configured session, runs
// in.Argv inside it with in.Cwd and in.Env, and returns a tmuxSubstrateSession
// handle.
//
// WindowName is taken from in.WindowName; callers (work-loop, review-loop)
// MUST set it to the pre-computed deterministic window name from tmux.WindowName
// (hk-gql20.8).
//
// When a spawn cap was configured via WithSpawnCap, SpawnWindow blocks until a
// slot is available or ctx is cancelled. A context cancellation returns a
// handler.ErrStructural-wrapped error.
//
// Returns a non-nil error (wrapping handler.ErrStructural) when the tmux
// adapter reports a failure or the spawn cap blocks and ctx is cancelled.
//
// Spec ref: process-lifecycle.md §4.7 PL-021b obligation 1.
// Bead ref: hk-xb5yi (spawn cap).
func (s *tmuxSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return s.spawnWindowVia(ctx, in, s.adapter, s.sessionName, false /* local */, nil /* no SSH runner: local Kill uses syscall.Kill */)
}

// spawnWindowVia is the adapter/session-parameterised core of SpawnWindow. The
// local path passes s.adapter / s.sessionName (byte-identical to the original
// behaviour). The remote path (perRunSubstrate.SpawnWindow with a non-local
// runner) passes an SSH-backed adapter and a worker-scoped session so the
// `tmux new-window`, pane-PID resolution, and the spawned session's Wait/Kill
// all execute on the WORKER's tmux server rather than box A's.
//
// All shared machinery — spawn semaphore, new-window mutex, stagger, the
// new-window timeout, and spawnedWindows tracking — is preserved for both paths.
// The remote flag marks the spawned session as worker-hosted so runWait polls
// worker-side liveness instead of a local kill(s.pid,0) (hk-r1zq). Local callers
// pass false ⇒ unchanged behaviour (NFR7).
//
//nolint:gocognit,cyclop // spawnWindowVia is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
func (s *tmuxSubstrate) spawnWindowVia(ctx context.Context, in handler.SubstrateSpawn, adapter tmux.Adapter, sessionName string, remote bool, runner tmux.CommandRunner) (handler.SubstrateSession, error) {
	releaseSlotFn := s.makeReleaseSlotFn(in.Terminal)
	if remote {
		releaseSlotFn = func() {}
	} else if err := s.acquireSpawnSlot(ctx, in.Terminal); err != nil {
		return nil, err
	}
	windowName := in.WindowName
	if windowName == "" {
		windowName = "hk-unnamed"
		if len(in.Argv) > 0 {
			parts := strings.Split(in.Argv[0], "/")
			if len(parts) > 0 {
				windowName = "hk-" + parts[len(parts)-1]
			}
		}
	}

	command := shellJoinArgv(in.Argv)
	if in.StdinDevNull {
		if command == "" {
			command = "< /dev/null"
		} else {
			command += " < /dev/null"
		}
	}

	params := tmux.NewWindowIn{
		Session:    sessionName,
		WindowName: windowName,
		Env:        in.Env,
		WorkDir:    in.Cwd,
		Command:    command,
	}

	outcome, timeoutErr := s.callNewWindowBounded(ctx, adapter, params)
	if timeoutErr != nil {
		releaseSlotFn()
		return nil, timeoutErr
	}
	if outcome.Err != nil {
		recovered := false
		if errors.Is(outcome.Err, tmux.ErrNoSession) {
			if se, ok := adapter.(sessionEnsurer); ok {
				if ensErr := se.EnsureSession(ctx, sessionName, ""); ensErr == nil {
					retryOutcome, retryTimeoutErr := s.callNewWindowBounded(ctx, adapter, params)
					if retryTimeoutErr != nil {
						releaseSlotFn()
						return nil, retryTimeoutErr
					}
					if retryOutcome.Err == nil {
						outcome = retryOutcome
						recovered = true
					}
				}
			}
		}
		if !recovered {
			releaseSlotFn()
			return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: %w: %w", outcome.Err, handler.ErrStructural)
		}
	}

	s.spawnedMu.Lock()
	s.spawnedWindows = append(s.spawnedWindows, spawnedWindow{handle: outcome.Handle, adapter: adapter})
	s.spawnedMu.Unlock()

	paneID := outcome.PaneID
	if paneID == "" {
		if id, paneIDErr := adapter.WindowPaneID(ctx, outcome.Handle); paneIDErr == nil {
			paneID = id
		}
	}

	pidTarget := outcome.Handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}

	pid, pidErr := adapter.WindowPanePID(ctx, pidTarget)
	if pidErr != nil {
		pid = 0
	}

	sess := &tmuxSubstrateSession{
		adapter:     adapter,
		handle:      outcome.Handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		remote:      remote,
		runner:      runner,
		waitDone:    make(chan struct{}),
		releaseSlot: releaseSlotFn,
	}
	return sess, nil
}

func (s *tmuxSubstrate) callNewWindowBounded(ctx context.Context, adapter tmux.Adapter, params tmux.NewWindowIn) (tmux.Outcome, error) {
	s.newWindowMu.Lock()
	defer s.newWindowMu.Unlock()

	if s.spawnStagger > 0 && !s.lastWindowAt.IsZero() {
		elapsed := time.Since(s.lastWindowAt)
		if elapsed < s.spawnStagger {
			waitFor := s.spawnStagger - elapsed
			select {
			case <-time.After(waitFor):
			case <-ctx.Done():
				return tmux.Outcome{}, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn stagger: context cancelled: %w: %w",
					ctx.Err(), handler.ErrStructural)
			}
		}
	}
	if s.spawnStagger > 0 {
		defer func() { s.lastWindowAt = time.Now() }()
	}

	return s.callBoundedTmuxCreate(ctx, boundedCreate{
		op:       "tmuxSubstrate.SpawnWindow",
		verb:     "tmux new-window",
		timedOut: ErrTmuxNewWindowTimeout,
		invoke:   func(callCtx context.Context) tmux.Outcome { return adapter.NewWindowIn(callCtx, params) },
	})
}

type boundedCreate struct {
	// op names the calling surface and opens the error message, e.g.
	// "tmuxSubstrate.SpawnWindow" or "SpawnRunSession".
	op string
	// verb names the tmux command being bounded and MAY carry the target, e.g.
	// "tmux new-window" or `tmux new-session for "harmonik-ab12-run-0f0e"`. On the
	// abandoned path this string is the operator's only evidence about what the
	// tmux server may have gone on to create, so name the target where there is
	// one.
	verb string
	// timedOut is the sentinel wrapped when the bound fires — the caller's way to
	// tell a hung creation from any other failure.
	timedOut error
	// invoke performs the call. It receives the BOUNDED context, so a ctx-aware
	// adapter gets its tmux client SIGKILLed on timeout; the goroutine+select in
	// callBoundedTmuxCreate is the backstop for adapters that ignore it.
	invoke func(ctx context.Context) tmux.Outcome
}

func (s *tmuxSubstrate) callBoundedTmuxCreate(ctx context.Context, create boundedCreate) (tmux.Outcome, error) {
	callCtx := ctx
	var cancel context.CancelFunc
	if s.newWindowTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, s.newWindowTimeout)
		defer cancel()
	}

	resCh := make(chan tmux.Outcome, 1)
	start := time.Now()
	go func() {
		resCh <- create.invoke(callCtx)
	}()

	select {
	case outcome := <-resCh:
		return outcome, nil
	case <-callCtx.Done():
		waited := time.Since(start)
		if ctx.Err() != nil {
			return tmux.Outcome{}, fmt.Errorf("daemon: %s: %s: context cancelled: %w: %w",
				create.op, create.verb, ctx.Err(), handler.ErrStructural)
		}
		if s.newWindowTimedOut != nil {
			s.newWindowTimedOut(waited)
		}
		return tmux.Outcome{}, fmt.Errorf("daemon: %s: %s did not return within %s: %w: %w",
			create.op, create.verb, s.newWindowTimeout, create.timedOut, handler.ErrStructural)
	}
}

func (s *tmuxSubstrate) callNewSessionBounded(ctx context.Context, op string, sc sessionCreator, params tmux.NewWindowIn) (tmux.Outcome, error) {
	return s.callBoundedTmuxCreate(ctx, boundedCreate{
		op: op,
		// Name the session in the verb: on the abandoned path this is the only
		// evidence an operator has about what tmux may have gone on to create.
		verb:     fmt.Sprintf("tmux new-session for %q", params.Session),
		timedOut: ErrTmuxNewSessionTimeout,
		invoke:   func(callCtx context.Context) tmux.Outcome { return sc.NewSessionIn(callCtx, params) },
	})
}

// SrtSpawnConfig carries the per-run configuration for an srt argv-wrap.
//
// When perRunSubstrate.sandboxSpawn is non-nil, SpawnWindow:
//  1. Calls GenerateSandboxProfile(ProfileInput) to produce the settings JSON.
//  2. Writes the JSON to a temp file (harmonik-srt-<RunID>.json in os.TempDir()).
//  3. Prepends [SrtBinary, "--settings", <profilePath>] to in.Argv so the
//     agent runs under srt's filesystem + network policy.
//
// A nil sandboxSpawn is a strict no-op (today's behaviour for all harnesses).
//
// Set by the workloop (hk-6596l) for harnesses listed in sandbox.harnesses
// when sandbox.backend = "srt". All other runs leave sandboxSpawn nil.
//
// Bead: hk-rlxgx.
type SrtSpawnConfig struct {
	// SrtBinary is the path to the srt executable.
	// Empty → "srt" resolved via the process PATH at spawn time.
	SrtBinary string
	// ProfileInput carries the per-run filesystem coordinates for
	// GenerateSandboxProfile. WorktreePath, GitDir, RunID, and DaemonSockPath
	// are REQUIRED; omitting any causes SpawnWindow to return ErrStructural.
	ProfileInput SandboxProfileInput
}

type perRunSubstrate struct {
	// inner is the shared tmuxSubstrate. SpawnWindow is delegated here.
	inner *tmuxSubstrate

	// paneTargetMu guards paneTarget; set once by SpawnWindow.
	paneTargetMu sync.Mutex
	// paneTarget is the tmux pane target for the window spawned by this run's
	// SpawnWindow call (e.g. "%1964" or "session:window.0"). Captured from the
	// returned SubstrateSession via the paneTargeter interface.
	// Empty when SpawnWindow has not yet been called or when the session does
	// not implement paneTargeter.
	cachedPaneTarget string

	// agentCommandFragments holds command-name substrings used by
	// PaneHasActiveProcess to recognise the handler process when it is the pane
	// PID itself (exec'd shell, no children during thinking phase). Derived from
	// HandlerBinary via agentCommandFragmentsFor at construction time.
	//
	// Bead: hk-vhped.
	agentCommandFragments []string

	// runner is the CommandRunner used by PaneHasActiveProcess (pgrep/ps probes)
	// and by pasteInjectQuitOnCommit (git rev-parse HEAD / git status) via the
	// commandRunnerProvider interface.  A nil value falls back to
	// tmux.LocalRunner{} (unchanged local behaviour).
	//
	// A non-nil runner ALSO marks this as a REMOTE run: SpawnWindow then routes
	// `tmux new-window` (and the spawned session's pane-PID/Wait/Kill) through an
	// SSH-backed adapter targeting a worker-scoped tmux session, rather than the
	// shared local adapter + box-A session.
	//
	// Bead: hk-rs-b9-liveness-1m9n.
	runner tmux.CommandRunner

	// workerSessionName is the tmux session on the WORKER that remote runs spawn
	// their implementer/reviewer window into. Set by the workloop for remote runs
	// (nil runner ⇒ empty ⇒ local path, untouched). SpawnWindow ensures this
	// session exists on the worker (via the SSH-backed adapter) BEFORE the
	// `tmux new-window`, mirroring how box A ensures its "-default" session.
	//
	// Bead ref: remote-substrate worker-spawn gap.
	workerSessionName string

	// workerSessionCwd is the working directory used when ensuring
	// workerSessionName on the worker (the worker's repo_path). Empty ⇒ tmux
	// default cwd.
	workerSessionCwd string

	// runSessionID, when non-empty, routes SpawnWindow to SpawnRunSession on the
	// inner tmuxSubstrate instead of the shared daemon session. Set by beadRunOne
	// when the substrate supports runSessionSpawner and the run is local.
	// This produces an independent tmux session that survives daemon SIGKILL
	// (hk-o85ye). Empty means the shared-session path (unchanged behaviour).
	runSessionID string

	// remoteAdapter caches the SSH-backed adapter built once at SpawnWindow time
	// from the inner adapter via WithRunner(runner). Paste-inject calls
	// (WriteLastPane / SendEnter / SendQuit) and PaneHasActiveProcess's PID
	// resolution use it so all tmux I/O for a remote run reaches the worker's
	// tmux server. Nil for local runs (paste-inject uses inner.adapter, unchanged).
	// Guarded by paneTargetMu: written by spawnWindowRemote, read by
	// pasteAdapter which may run on a different goroutine.
	//
	// Bead ref: remote-substrate worker-spawn gap.
	remoteAdapter tmux.Adapter

	// onConnectionFailure is called when an SSH connection failure (exit-255) is
	// detected during PaneHasActiveProcess. Wired by the workloop for remote runs
	// to emit worker_offline and disable the worker in-memory. Nil for local runs.
	//
	// Bead: hk-rs-b11-offline-dh57.
	onConnectionFailure func(ctx context.Context, detail string)

	// sandboxSpawn, when non-nil, wraps in.Argv with srt before SpawnWindow
	// delegates to the inner substrate. SpawnWindow calls GenerateSandboxProfile,
	// writes the result to a temp file, then prepends
	// [SrtBinary, "--settings", <path>] to in.Argv.
	//
	// Nil for runs where sandbox.backend != "srt" or the harness is not in
	// sandbox.harnesses — preserves today's behaviour for all existing harnesses.
	//
	// Set by the workloop (hk-6596l) when the project config activates srt for
	// the current run's harness.
	//
	// Bead: hk-rlxgx.
	sandboxSpawn *SrtSpawnConfig
}

func (p *perRunSubstrate) commandRunner() tmux.CommandRunner {
	if p.runner != nil {
		return p.runner
	}
	return tmux.LocalRunner{}
}

var (
	_ handler.Substrate     = (*perRunSubstrate)(nil)
	_ handler.InputPort     = (*perRunSubstrate)(nil) // interim tmux/paste input port (AIS-001)
	_ pasteInjecter         = (*perRunSubstrate)(nil)
	_ enterSender           = (*perRunSubstrate)(nil)
	_ quitSender            = (*perRunSubstrate)(nil)
	_ paneLivenessChecker   = (*perRunSubstrate)(nil)
	_ paneOutputSizer       = (*perRunSubstrate)(nil)
	_ commandRunnerProvider = (*perRunSubstrate)(nil)
)

type paneTargeter interface {
	// PaneTarget returns the tmux pane target string for this session.
	// Returns an empty string when no pane target is available.
	PaneTarget() string
}

type substrateWithAdapter interface {
	tmuxAdapter() tmux.Adapter
}

func (s *tmuxSubstrate) tmuxAdapter() tmux.Adapter { return s.adapter }

type substrateWithSessionName interface {
	daemonSessionName() string
}

func (s *tmuxSubstrate) daemonSessionName() string { return s.sessionName }

type substrateWithKeepalive interface {
	RunSessionKeepalive(ctx context.Context)
}

var _ substrateWithKeepalive = (*tmuxSubstrate)(nil)

type substrateWithSpawnCap interface {
	SpawnCapSize() int
}

var _ substrateWithSpawnCap = (*tmuxSubstrate)(nil)

type substrateWithSpawnCapSetter interface {
	SetSpawnCap(n int)
}

var _ substrateWithSpawnCapSetter = (*tmuxSubstrate)(nil)

type substrateSpawnReadier interface {
	ProbeSpawnReady(ctx context.Context) error
}

// ProbeSpawnReady calls EnsureSession on the underlying adapter to verify the
// daemon's spawn-target session is ready to accept new tmux windows. Returns
// nil immediately when the adapter does not implement sessionEnsurer (test
// stubs, non-tmux adapters, etc.).
//
// Implements substrateSpawnReadier (hk-bk33).
func (s *tmuxSubstrate) ProbeSpawnReady(ctx context.Context) error {
	se, ok := s.adapter.(sessionEnsurer)
	if !ok {
		return nil
	}
	return se.EnsureSession(ctx, s.sessionName, "")
}

var _ substrateSpawnReadier = (*tmuxSubstrate)(nil)

// KillAllWindows kills every tmux window spawned by this daemon instance.
//
// It is called from exitClean() in runWorkLoop after wg.Wait() returns, so all
// in-flight goroutines have already exited before KillAllWindows runs.  Any
// windows that were already killed by a prior tmuxSubstrateSession.Kill call
// are simply no-ops (tmux kill-window on a missing window exits non-zero, which
// is silently swallowed here).
//
// Implements windowCleaner. Bead: hk-j6npz.
func (s *tmuxSubstrate) KillAllWindows(ctx context.Context) error {
	s.spawnedMu.Lock()
	windows := make([]spawnedWindow, len(s.spawnedWindows))
	copy(windows, s.spawnedWindows)
	s.spawnedMu.Unlock()

	for _, w := range windows {
		_ = w.adapter.KillWindow(ctx, w.handle) //nolint:errcheck // best-effort; window may already be killed by Kill or external tmux
	}
	return nil
}

type spawnedWindow struct {
	handle  tmux.WindowHandle
	adapter tmux.Adapter
}

// StopWindowByHandle sends /quit to the pane (best-effort), waits a grace
// period, then kills the window identified by handle. Used by crew-stop to tear
// down a persistent crew session whose handle was recorded in the crew registry.
//
// handle is the tmux window handle string (e.g. "session:window-name") stored
// in crew.Record.Handle. The pane target for /quit is derived as handle+".0".
//
// Implements crewPaneStopper (crewstart.go).
// Bead ref: hk-5tg5o (C2).
func (s *tmuxSubstrate) StopWindowByHandle(ctx context.Context, handle string) error {
	paneTarget := handle + ".0"
	_ = s.adapter.SendKeysQuit(ctx, paneTarget) //nolint:errcheck // best-effort; kill is authoritative

	select {
	case <-ctx.Done():
	case <-time.After(crewStopQuitGrace):
	}

	return s.adapter.KillWindow(ctx, tmux.WindowHandle(handle))
}

const crewStopQuitGrace = 30 * time.Second

type sessionCreator interface {
	NewSessionIn(ctx context.Context, params tmux.NewWindowIn) tmux.Outcome
}

type sessionEnsurer interface {
	EnsureSession(ctx context.Context, name, workDir string) error
}

type runnerSwapper interface {
	WithRunner(r tmux.CommandRunner) tmux.OSAdapter
}

var _ runnerSwapper = tmux.OSAdapter{}

type crewSessionSpawner interface {
	// SpawnCrewSession creates an independent tmux session for crewName and runs
	// spawn.Argv inside it. The session name is derived via crewSessionName.
	SpawnCrewSession(ctx context.Context, crewName string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error)
}

type crewSessionStopper interface {
	// StopCrewSession sends /quit to the crew pane (best-effort), waits a grace
	// period, then kills the crew's dedicated tmux session.
	StopCrewSession(ctx context.Context, crewName string, handle string) error
}

type runSessionSpawner interface {
	// SpawnRunSession creates an independent tmux session named
	// "harmonik-<hash>-run-<shortID>" and returns a session handle. The session
	// is outside the daemon's process group so it persists across daemon SIGKILL.
	SpawnRunSession(ctx context.Context, runID string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error)
}

func (s *tmuxSubstrate) crewSessionName(name string) (string, error) {
	if s.projectHash == "" {
		return "", fmt.Errorf("daemon: crewSessionName: project hash unavailable for crew %q "+
			"(NewTmuxSubstrate must be built with WithCrewProjectHash): %w", name, handler.ErrStructural)
	}
	return lifecycle.TmuxSessionName(s.projectHash, "crew-"+name), nil
}

func (s *tmuxSubstrate) workerSpawnSessionName(workerName string) string {
	if s.projectHash != "" && workerName != "" {
		return lifecycle.TmuxSessionName(s.projectHash, "worker-"+workerName)
	}
	return s.sessionName
}

// SpawnCrewSession creates an independent tmux session for the crew and runs
// and runs spawn.Argv inside it. The session is decoupled from the daemon's own
// session so that daemon restarts do not kill running crew windows (hk-mmlqt).
//
// Implements crewSessionSpawner. Called by HandleCrewStart when the substrate
// supports this interface (production path with OSAdapter).
//
// When the session already exists (ErrWindowCollision — the crew survived a
// prior daemon restart, or crew-stop removed the registry but failed to kill
// the session), SpawnCrewSession recovers gracefully: it calls
// ensureCrewKeeperWindow to re-arm the keeper if the "keeper" window is absent,
// then returns the existing session via existingCrewSession so that
// HandleCrewStart can update the registry handle and run the keeper liveness
// probe (hk-u5tgh).
func (s *tmuxSubstrate) SpawnCrewSession(ctx context.Context, crewName string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	sc, ok := s.adapter.(sessionCreator)
	if !ok {
		return nil, fmt.Errorf("daemon: SpawnCrewSession: adapter does not support session creation: %w", handler.ErrStructural)
	}

	sessName, nameErr := s.crewSessionName(crewName)
	if nameErr != nil {
		return nil, fmt.Errorf("daemon: SpawnCrewSession: %w", nameErr)
	}

	command := shellJoinArgv(spawn.Argv)

	params := tmux.NewWindowIn{
		Session:    sessName,
		WindowName: tmux.WindowAgent,
		Env:        spawn.Env,
		WorkDir:    spawn.Cwd,
		Command:    command,
	}

	outcome, boundErr := s.callNewSessionBounded(ctx, "SpawnCrewSession", sc, params)
	if boundErr != nil {
		return nil, boundErr
	}
	if outcome.Err != nil {
		if errors.Is(outcome.Err, tmux.ErrWindowCollision) {
			fmt.Fprintf(os.Stderr,
				"daemon: SpawnCrewSession: crew %q session already exists — re-arming keeper if absent\n",
				crewName)
			s.ensureCrewKeeperWindow(ctx, crewName, sessName, spawn)
			return s.existingCrewSession(ctx, sessName)
		}
		return nil, fmt.Errorf("daemon: SpawnCrewSession: new-session for crew %q: %w", crewName, outcome.Err)
	}

	s.spawnCrewKeeperWindow(ctx, crewName, sessName, spawn)

	paneID := outcome.PaneID
	pidTarget := outcome.Handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}
	pid, pidErr := s.adapter.WindowPanePID(ctx, pidTarget)
	if pidErr != nil {
		slog.WarnContext(ctx, "daemon: tmux substrate: resolve pane PID",
			"err", pidErr, "target", string(pidTarget))
	}

	sess := &tmuxSubstrateSession{
		adapter:     s.adapter,
		handle:      outcome.Handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		waitDone:    make(chan struct{}),
		releaseSlot: func() {}, // crew sessions are outside the daemon spawn-cap
	}
	return sess, nil
}

func (s *tmuxSubstrate) runSessionName(runID string) (string, error) {
	if s.projectHash == "" {
		return "", fmt.Errorf("daemon: runSessionName: project hash unavailable"+
			" (NewTmuxSubstrate must be built with WithCrewProjectHash): %w", handler.ErrStructural)
	}
	short := strings.ReplaceAll(runID, "-", "")
	if len(short) > 16 {
		short = short[:16]
	}
	return lifecycle.TmuxSessionName(s.projectHash, "run-"+short), nil
}

// SpawnRunSession creates an independent tmux session for a bead run so the
// run survives a daemon SIGKILL (hk-o85ye). The session is named via
// runSessionName and lives outside the daemon's shared session. Unlike
// SpawnCrewSession, no keeper window is added; the run's normal lifecycle
// (wtCleanup, forceTeardownSession, run registry removal) handles teardown.
//
// Implements runSessionSpawner. Called by perRunSubstrate.SpawnWindow when
// runSessionID is set (local runs only; remote runs keep their worker-session path).
func (s *tmuxSubstrate) SpawnRunSession(ctx context.Context, runID string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	sc, ok := s.adapter.(sessionCreator)
	if !ok {
		return nil, fmt.Errorf("daemon: SpawnRunSession: adapter does not support session creation: %w", handler.ErrStructural)
	}

	sessName, nameErr := s.runSessionName(runID)
	if nameErr != nil {
		return nil, fmt.Errorf("daemon: SpawnRunSession: %w", nameErr)
	}

	command := shellJoinArgv(spawn.Argv)

	params := tmux.NewWindowIn{
		Session:    sessName,
		WindowName: tmux.WindowAgent, // fixed name; avoids the hk-<hash6>- orphan-window sweep
		Env:        spawn.Env,
		WorkDir:    spawn.Cwd,
		Command:    command,
	}

	outcome, boundErr := s.callNewSessionBounded(ctx, "SpawnRunSession", sc, params)
	if boundErr != nil {
		return nil, boundErr
	}
	if outcome.Err != nil {
		if !errors.Is(outcome.Err, tmux.ErrWindowCollision) {
			return nil, fmt.Errorf("daemon: SpawnRunSession %q: %w", runID, outcome.Err)
		}
		fmt.Fprintf(os.Stderr,
			"daemon: SpawnRunSession: session %q still exists for run %s; finishing its teardown and retrying\n",
			sessName, runID)
		if killErr := s.adapter.KillSession(ctx, sessName); killErr != nil {
			return nil, fmt.Errorf("daemon: SpawnRunSession %q: clear the previous session: %w", runID, killErr)
		}
		outcome, boundErr = s.callNewSessionBounded(ctx, "SpawnRunSession", sc, params)
		if boundErr != nil {
			return nil, boundErr
		}
		if outcome.Err != nil {
			return nil, fmt.Errorf("daemon: SpawnRunSession %q after clearing the previous session: %w", runID, outcome.Err)
		}
	}

	paneID := outcome.PaneID
	pidTarget := outcome.Handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}
	pid, pidErr := s.adapter.WindowPanePID(ctx, pidTarget)
	if pidErr != nil {
		slog.WarnContext(ctx, "daemon: tmux substrate: resolve pane PID",
			"err", pidErr, "target", string(pidTarget))
	}

	sess := &tmuxSubstrateSession{
		adapter:     s.adapter,
		handle:      outcome.Handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		waitDone:    make(chan struct{}),
		releaseSlot: func() {}, // run sessions are outside the daemon spawn-cap
	}
	return sess, nil
}

// crewKeeperWindowArgv builds the argv the keeper window runs to watch the crew
// agent pane. The keeper targets the sibling "agent" window
// ("--tmux <session>:agent", slice K) so it never pastes into its own window.
//
//	<keeperBin> keeper --agent <crew> --tmux <session>:agent \
//	    --warn-abs-tokens <w> --act-abs-tokens <a> [--project <dir>]
//
// keeperBin is the path of the currently-running harmonik binary (the keeper is
// a harmonik subcommand, not the claude handler). projectDir, when non-empty,
// pins the keeper to the crew's project root.
//
// FORCE-CUT BY DEFAULT (ES5 / hk-lcga, D4): a crew "can't get too big". The crew
// keeper is armed in FULL band mode (NOT --warn-only), so when the crew fills its
// context the keeper's in-process restart-now cycle (handoff → /clear →
// /session-resume) fires and force-cuts it, instead of nagging forever under the
// old --warn-only default. The actual warn/act NUMBERS are OPERATOR CONFIG: this
// path passes NO product-default band (WarnAbsTokens/ActAbsTokens 0 = unset → the
// flags are omitted), so the spawned keeper reads the operator's keeper: block in
// .harmonik/config.yaml — matching the captain, and refusing to start if a
// required value is unset. (Operator-required-config change: no baked-in numbers.)
// The clear→resume cycle re-binds the crew on the SAME session_id (crews mint +
// reuse a uuid via resolveSessionID), so force-cut + restart works the same way
// it does for the captain.
//
// RespawnCmd is intentionally LEFT EMPTY here (NOT armed): the captain wires
// --respawn-cmd to its captain-specific `harmonik captain respawn` subcommand
// (dead-pane self-heal). No crew respawn entrypoint exists today, and building
// one is a separate surface — D4 ("force-cut on fill") is satisfied by the
// act-band restart-now cycle alone; dead-pane self-heal for crews is a distinct
// concern. TODO(hk-lcga follow-up): add a crew respawn entrypoint (e.g.
// `harmonik crew respawn`) and wire RespawnCmd here for crew dead-pane self-heal.
//
// Delegates to the SHARED agentlaunch.KeeperWindowArgv (review outcome A) so the
// crew keeper-window argv and the CLI captain keeper-window argv have a single
// source of truth.
func crewKeeperWindowArgv(keeperBin, crewName, sessName, projectDir string) []string {
	return agentlaunch.KeeperWindowArgv(agentlaunch.KeeperWindowOpts{
		KeeperBin:  keeperBin,
		AgentName:  crewName,
		Session:    sessName,
		ProjectDir: projectDir,
		WarnOnly:   false, // D4: crew is FORCE-CUT, full warn→act→restart band.
		// No product-default band: 0 (unset) → flags omitted → keeper reads the
		// operator's keeper: block in .harmonik/config.yaml. (No baked-in numbers.)
		WarnAbsTokens: 0,
		ActAbsTokens:  0,
	})
}

func (s *tmuxSubstrate) spawnCrewKeeperWindow(ctx context.Context, crewName, sessName string, spawn handler.SubstrateSpawn) {
	projectDir := spawn.Cwd
	if projectDir == "" {
		for _, kv := range spawn.Env {
			if v, ok := strings.CutPrefix(kv, "HARMONIK_PROJECT="); ok {
				projectDir = v
				break
			}
		}
	}

	keeperBin, exErr := os.Executable()
	if exErr != nil {
		keeperBin = "harmonik" // fallback: rely on PATH
	}

	argv := crewKeeperWindowArgv(keeperBin, crewName, sessName, projectDir)

	params := tmux.NewWindowIn{
		Session:    sessName,
		WindowName: tmux.WindowKeeper,
		WorkDir:    projectDir,
		Command:    shellJoinArgv(argv),
	}

	outcome, boundErr := s.callNewWindowBounded(ctx, s.adapter, params)
	if boundErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: SpawnCrewSession: launch keeper window for crew %q (%s:%s): %v (non-fatal)\n",
			crewName, sessName, tmux.WindowKeeper, boundErr)
		return
	}
	if outcome.Err != nil {
		fmt.Fprintf(os.Stderr, "daemon: SpawnCrewSession: launch keeper window for crew %q (%s:%s): %v (non-fatal)\n",
			crewName, sessName, tmux.WindowKeeper, outcome.Err)
	}
}

func (s *tmuxSubstrate) ensureCrewKeeperWindow(ctx context.Context, crewName, sessName string, spawn handler.SubstrateSpawn) {
	windows, listErr := s.adapter.ListWindows(ctx, sessName)
	if listErr != nil {
		s.spawnCrewKeeperWindow(ctx, crewName, sessName, spawn)
		return
	}
	for _, w := range windows {
		if w == tmux.WindowKeeper {
			fmt.Fprintf(os.Stderr,
				"daemon: SpawnCrewSession: crew %q session already has keeper window — no re-arm needed\n",
				crewName)
			return
		}
	}
	fmt.Fprintf(os.Stderr,
		"daemon: SpawnCrewSession: crew %q keeper window absent from existing session — re-arming\n",
		crewName)
	s.spawnCrewKeeperWindow(ctx, crewName, sessName, spawn)
}

func (s *tmuxSubstrate) existingCrewSession(ctx context.Context, sessName string) (handler.SubstrateSession, error) {
	handle := tmux.WindowHandle(sessName + ":" + tmux.WindowAgent)
	paneID, paneErr := s.adapter.WindowPaneID(ctx, handle)
	if paneErr != nil {
		slog.WarnContext(ctx, "daemon: tmux substrate: resolve pane ID for existing crew session",
			"err", paneErr, "handle", string(handle))
	}
	pidTarget := handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}
	pid, pidErr := s.adapter.WindowPanePID(ctx, pidTarget)
	if pidErr != nil {
		slog.WarnContext(ctx, "daemon: tmux substrate: resolve pane PID",
			"err", pidErr, "target", string(pidTarget))
	}
	return &tmuxSubstrateSession{
		adapter:     s.adapter,
		handle:      handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		waitDone:    make(chan struct{}),
		releaseSlot: func() {},
	}, nil
}

func shellJoinArgv(argv []string) string {
	return agentlaunch.ShellJoinArgv(argv)
}

// StopCrewSession sends /quit to the crew's pane (best-effort), waits a grace
// period, then kills the crew's independent tmux session (hk-mmlqt).
//
// handle is the window handle stored in the crew registry (e.g.
// "hk-crew-alpha:hk-crew-alpha"). The pane target for /quit is handle+".0".
//
// Implements crewSessionStopper (crewstart.go).
func (s *tmuxSubstrate) StopCrewSession(ctx context.Context, crewName string, handle string) error {
	if handle != "" {
		paneTarget := handle + ".0"
		_ = s.adapter.SendKeysQuit(ctx, paneTarget) //nolint:errcheck // best-effort; session kill is authoritative
	}

	select {
	case <-ctx.Done():
	case <-time.After(crewStopQuitGrace):
	}

	sessName, nameErr := s.crewSessionName(crewName)
	if nameErr != nil {
		return nameErr
	}
	return s.adapter.KillSession(ctx, sessName)
}

func newPerRunSubstrate(sub handler.Substrate, handlerBinary string, runner tmux.CommandRunner) *perRunSubstrate {
	notifySubstrateRunner(runner)
	if sub == nil {
		return nil
	}
	ts, ok := sub.(*tmuxSubstrate)
	if !ok {
		return nil
	}
	return &perRunSubstrate{
		inner:                 ts,
		agentCommandFragments: agentCommandFragmentsFor(handlerBinary),
		runner:                runner,
	}
}

// SpawnWindow delegates to the inner tmuxSubstrate.SpawnWindow and captures the
// spawned pane target into this per-run instance.
//
// The pane target is extracted via the paneTargeter interface (implemented by
// tmuxSubstrateSession and test doubles that need pane isolation). If the
// returned session does not implement paneTargeter, the pane target remains
// empty and paste-inject calls will fail gracefully.
func (p *perRunSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if p.sandboxSpawn != nil {
		wrapped, wrapErr := p.buildSrtArgv(in.Argv)
		if wrapErr != nil {
			return nil, fmt.Errorf("daemon: perRunSubstrate.SpawnWindow: srt argv-wrap: %w: %w", wrapErr, handler.ErrStructural)
		}
		in.Argv = wrapped.Argv
		in.Env = append(in.Env, wrapped.Env...)
	}

	if p.runSessionID != "" {
		sess, err := p.inner.SpawnRunSession(ctx, p.runSessionID, in)
		if err != nil {
			return nil, err
		}
		if pt, ok := sess.(paneTargeter); ok {
			if target := pt.PaneTarget(); target != "" {
				p.paneTargetMu.Lock()
				p.cachedPaneTarget = target
				p.paneTargetMu.Unlock()
			}
		}
		return sess, nil
	}

	if p.runner != nil {
		sess, err := p.spawnWindowRemote(ctx, in)
		if err != nil {
			return nil, err
		}
		if pt, ok := sess.(paneTargeter); ok {
			if target := pt.PaneTarget(); target != "" {
				p.paneTargetMu.Lock()
				p.cachedPaneTarget = target
				p.paneTargetMu.Unlock()
			}
		}
		return sess, nil
	}

	sess, err := p.inner.SpawnWindow(ctx, in)
	if err != nil {
		return nil, err
	}
	if pt, ok := sess.(paneTargeter); ok {
		if target := pt.PaneTarget(); target != "" {
			p.paneTargetMu.Lock()
			p.cachedPaneTarget = target
			p.paneTargetMu.Unlock()
		}
	}
	return sess, nil
}

func (p *perRunSubstrate) buildSrtArgv(agentArgv []string) (srtWrap, error) {
	return srtWrapArgv(p.sandboxSpawn, agentArgv)
}

func (p *perRunSubstrate) spawnWindowRemote(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	sw, ok := p.inner.adapter.(runnerSwapper)
	if !ok {
		return nil, fmt.Errorf("daemon: perRunSubstrate.spawnWindowRemote: inner adapter does not support WithRunner (cannot target worker tmux): %w", handler.ErrStructural)
	}
	remoteAdapter := tmux.Adapter(sw.WithRunner(p.runner))

	sessName := p.workerSessionName
	if sessName == "" {
		sessName = p.inner.sessionName
	}

	if se, ok := remoteAdapter.(sessionEnsurer); ok {
		if ensErr := se.EnsureSession(ctx, sessName, p.workerSessionCwd); ensErr != nil {
			return nil, fmt.Errorf("daemon: perRunSubstrate.spawnWindowRemote: ensure worker session %q: %w: %w", sessName, ensErr, handler.ErrStructural)
		}
	}

	p.paneTargetMu.Lock()
	p.remoteAdapter = remoteAdapter
	p.paneTargetMu.Unlock()

	return p.inner.spawnWindowVia(ctx, in, remoteAdapter, sessName, true /* remote: worker-hosted, runWait must poll worker liveness not local kill (hk-r1zq) */, p.runner /* SSH runner: Kill forcefully terminates the worker pane PID over SSH (hk-btl1n) */)
}

func (p *perRunSubstrate) pasteAdapter() tmux.Adapter {
	p.paneTargetMu.Lock()
	ra := p.remoteAdapter
	p.paneTargetMu.Unlock()
	if ra != nil {
		return ra
	}
	return p.inner.adapter
}

func (p *perRunSubstrate) paneTarget() string {
	p.paneTargetMu.Lock()
	defer p.paneTargetMu.Unlock()
	return p.cachedPaneTarget
}

// WriteLastPane delivers payload to this run's pane (not the shared
// "last pane" — the pane captured at SpawnWindow time for this run).
//
// Implements pasteInjecter.
func (p *perRunSubstrate) WriteLastPane(ctx context.Context, bufferName string, payload []byte) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.WriteLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.pasteAdapter().WriteToPane(ctx, bufferName, target, payload)
}

const inputBufferPurpose = "input"

func (p *perRunSubstrate) inputBufferName() string {
	id := sanitizeBufferSegment(p.runSessionID)
	if id == "" {
		id = sanitizeBufferSegment(p.paneTarget())
	}
	if id == "" {
		id = "run"
	}
	return bufferName(id, inputBufferPurpose)
}

func sanitizeBufferSegment(s string) string {
	return tmux.SanitizeBufferSegment(s)
}

// SubmitInput is the interim tmux/paste implementation of handler.InputPort
// (AIS-001 / AIS-003 / HC-069 / HC-070). It delivers the payload to this run's
// pane via the existing bracketed-paste path and returns an EXPLICIT
// Ack{Delivered} on a successful write — replacing the retired silent no-op.
//
// Its positive acceptance is confirmed ASYNCHRONOUSLY by the Claude-hook-bridge
// signal (outcome_emitted on Stop / agent_ready on SessionStart), or reaches the
// agent_input_stale terminal on the bounded-liveness timeout (HC-INV-008 /
// AIS-INV-001) — it MUST NOT synthesize a positive acceptance it did not observe,
// and MUST NOT scrape capture-pane for one. The tmux/paste path cannot produce a
// protocol-level Rejected, so this interim impl returns only Delivered or a write
// error. Seq/Token are codec-owned and remain zero for the interim paste path
// (no wire protocol supplies them).
func (p *perRunSubstrate) SubmitInput(ctx context.Context, req handler.InputRequest) (handler.Ack, error) {
	if err := p.WriteLastPane(ctx, p.inputBufferName(), req.Payload); err != nil {
		return handler.Ack{}, err
	}
	return handler.Ack{Outcome: handler.Delivered}, nil
}

// CloseInput signals end-of-input for the interim tmux/paste path (replaces the
// retired substrateSessionAdapter.CloseStdin no-op, AIS-001 / HC-069). The tmux
// pane owns the child's pty; there is no daemon-held write-end pipe to close, so
// this is a legitimate nil — distinct from the retired input-acceptance no-op.
func (p *perRunSubstrate) CloseInput(_ context.Context) error {
	return nil
}

// SendEnterToLastPane sends a bare Enter key to this run's pane.
//
// Implements enterSender.
func (p *perRunSubstrate) SendEnterToLastPane(ctx context.Context) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.SendEnterToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.pasteAdapter().SendKeysEnter(ctx, target)
}

// SendQuitToLastPane sends /quit followed by Enter to this run's pane.
//
// Implements quitSender.
func (p *perRunSubstrate) SendQuitToLastPane(ctx context.Context) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.SendQuitToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.pasteAdapter().SendKeysQuit(ctx, target)
}

type paneCaptureAdapter interface {
	CapturePane(ctx context.Context, paneTarget string, scrollback int) (string, error)
}

// CaptureLastPane returns the rendered text (plus scrollback lines of tail) of
// this run's pane — the pane captured at SpawnWindow time, NOT the shared "last
// pane".  For a remote run pasteAdapter() reads the pane on the WORKER's tmux
// server over the same SSH runner used for the paste, so the verification sees
// exactly what the seed paste targeted.
//
// Implements paneCapturer (consumed by the seed-paste verify-and-retry loop).
//
// Bead: hk-zexsj.
func (p *perRunSubstrate) CaptureLastPane(ctx context.Context, scrollback int) (string, error) {
	target := p.paneTarget()
	if target == "" {
		return "", fmt.Errorf("daemon: perRunSubstrate.CaptureLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	pa, ok := p.pasteAdapter().(paneCaptureAdapter)
	if !ok {
		return "", fmt.Errorf("daemon: perRunSubstrate.CaptureLastPane: adapter %T lacks CapturePane: %w", p.pasteAdapter(), errPaneCaptureUnsupported)
	}
	return pa.CapturePane(ctx, target, scrollback)
}

// PaneHasActiveProcess returns true when the tmux pane shell (identified by the
// pane target captured at SpawnWindow time) has at least one child process, or
// when the pane PID itself is the handler process (exec'd shell with no
// children during a thinking phase).
//
// The implementation retrieves the shell PID via WindowPanePID (using the
// stable per-run pane target), checks for direct children via hasAnyDirectChild,
// and — if none are found — checks whether the pane PID itself is a recognised
// handler command by matching against agentCommandFragments (derived from
// HandlerBinary at construction time via agentCommandFragmentsFor).
//
// Using the per-run fragments instead of the global livePaneCommandSubstrings
// means custom handler binaries (non-claude agents) are matched correctly.
//
// Returns false on any error (conservative).
//
// Implements paneLivenessChecker.
//
// Beads: hk-fbydv, hk-vhped.
func (p *perRunSubstrate) PaneHasActiveProcess(ctx context.Context) bool {
	target := p.paneTarget()
	if target == "" {
		return false
	}
	pid, err := p.pasteAdapter().WindowPanePID(ctx, tmux.WindowHandle(target))
	if err != nil || pid <= 0 {
		return false
	}
	r := p.commandRunner()
	alive, connFailed := probeLivenessOrSSHFail(ctx, r, pid, p.agentCommandFragments)
	if connFailed {
		p.notifyConnectionFailure(ctx, "liveness probe returned ssh exit-255")
	}
	return alive
}

func (p *perRunSubstrate) notifyConnectionFailure(ctx context.Context, detail string) {
	if p.onConnectionFailure != nil {
		p.onConnectionFailure(ctx, detail)
	}
}

// PaneOutputFingerprint returns a string encoding the current pane output
// volume: the tmux scrollback history size combined with the cursor row
// position.  The value changes as the pane produces visible output
// (streaming LLM responses, file reads, tool results), so an implementer
// that is actively reading/planning without yet editing the worktree
// advances this fingerprint every tick.
//
// The format is `"<history_size> <cursor_y>"` as reported by
// `tmux display-message -p "#{history_size} #{cursor_y}"`.
//   - history_size increases as content scrolls into the scrollback buffer.
//   - cursor_y increases as new output lines appear in the visible pane area
//     before the first scroll.
//
// Returns ("", false) on any error (conservative: treat unknown as no
// growth — the ceiling kill is allowed to proceed).
//
// Implements paneOutputSizer.
//
// Bead: hk-ue0u2.
func (p *perRunSubstrate) PaneOutputFingerprint(ctx context.Context) (string, bool) {
	target := p.paneTarget()
	if target == "" {
		return "", false
	}
	out, err := p.commandRunner().Command(ctx, "tmux", "display-message",
		"-t", target, "-p", "#{history_size} #{cursor_y}").Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", false
	}
	return s, true
}

type tmuxSubstrateSession struct {
	adapter tmux.Adapter
	handle  tmux.WindowHandle
	// paneID is the stable tmux pane identifier (e.g. "%1964") captured at
	// SpawnWindow time. Read by perRunSubstrate.SpawnWindow to initialise its
	// own isolated pane target (hk-012af).
	paneID string
	// pidTarget is the slash-free handle used by runWait's secondary
	// pane-presence check to resolve #{pane_pid} for THIS run's pane. It is the
	// slash-free pane ID ("%NNNN") when available, else the slash-bearing
	// "session:window-name" handle. Using a slash-free target prevents tmux from
	// misparsing the window-name handle and falling back to the session's
	// active pane, which under MaxConcurrent>1 aliases a sibling run's PID and
	// prematurely ends the implementer phase (hk-kuxxl).
	pidTarget tmux.WindowHandle
	pid       int

	// remote is true when this session was spawned on a REMOTE worker's tmux
	// server (perRunSubstrate.spawnWindowRemote) rather than box A's local tmux.
	// For a remote run s.pid is the WORKER's pane PID, which does NOT exist in the
	// daemon host's process table — so runWait's local processDead(s.pid) =
	// kill(pid,0) fast path would return ESRCH ("dead") on the very first tick and
	// prematurely conclude exitCodeClean while claude is still running on the
	// worker (hk-r1zq, the remote completion-detection misfire). When remote,
	// runWait skips the local-kill fast path and polls worker-side liveness via
	// s.adapter.WindowPanePID (which routes over the run's SSH runner). Local
	// sessions leave this false ⇒ the fast path is byte-identical (NFR7).
	remote bool

	// runner is the run's SSH-backed CommandRunner for a REMOTE session, nil for
	// a local one. Kill uses it to forcefully terminate the WORKER pane PID over
	// SSH (kill -TERM → grace → kill -KILL), the remote analog of the local
	// killProcessWithGrace(s.pid) syscall path — a worker agent that survives the
	// pane SIGHUP from KillWindow would otherwise leak on the worker (hk-btl1n).
	// s.pid MUST NOT be local-signalled for a remote run (it names the worker's
	// process table, not the daemon host's — hk-r1zq/H8), so the kill is routed
	// through this runner instead.
	runner tmux.CommandRunner

	// killOnce ensures Kill is idempotent.
	killOnce sync.Once

	outcome handler.Outcome

	// waitDone is closed when the Wait goroutine finishes.
	waitDone chan struct{}
	waitOnce sync.Once

	// isProcessDead is the liveness predicate used by runWait. In production
	// it is nil and processDead (the package-level function) is called directly.
	// Tests inject a deterministic stub via the function-valued field to exercise
	// the ctx.Done() and tick paths without real OS processes (hk-88nno).
	isProcessDead func(pid int) bool

	// releaseSlot, when non-nil, returns this session's slot to the parent
	// substrate's spawn semaphore. Called exactly once inside killOnce.Do.
	// Nil when no spawn cap was configured (WithSpawnCap was not passed).
	//
	// Bead ref: hk-xb5yi (concurrent-spawn cap).
	releaseSlot func()
}

const killGracePeriod = 3 * time.Second

func (s *tmuxSubstrateSession) Kill(ctx context.Context) error {
	var killErr error
	s.killOnce.Do(func() {
		if s.remote {
			if pt := s.PaneTarget(); pt != "" {
				_ = s.adapter.SendKeysQuit(ctx, pt) //nolint:errcheck // best-effort; KillWindow is authoritative
			}
			if s.runner != nil && s.pid > 0 {
				killRemoteProcessWithGrace(ctx, s.runner, s.pid, killGracePeriod)
			}
		} else if s.pid > 0 {
			livePID, panePIDErr := s.adapter.WindowPanePID(ctx, s.panePIDTarget())
			switch {
			case panePIDErr != nil:
				slog.WarnContext(ctx, "kill_skipped_pane_unknown_to_tmux",
					"spawn_pid", s.pid,
					"pane_target", string(s.panePIDTarget()),
					"err", panePIDErr,
					"reason", "tmux no longer resolves this pane; the spawn-time pid may have been recycled, so signal nothing",
					"bead", "hk-bl2k6")
			case livePID > 0:
				if livePID != s.pid {
					slog.WarnContext(ctx, "kill_pane_pid_changed_since_spawn",
						"spawn_pid", s.pid, "live_pid", livePID,
						"reason", "tmux reports a different pane pid than the one captured at spawn; killing the live one",
						"bead", "hk-bl2k6")
				}
				killProcessWithGrace(ctx, livePID, killGracePeriod)
			default:
				killProcessWithGrace(ctx, s.pid, killGracePeriod)
			}
		}
		killErr = s.adapter.KillWindow(ctx, s.handle)
		if s.releaseSlot != nil {
			s.releaseSlot()
		}
	})
	return killErr
}

var killProcessSignal = syscall.Kill

func killProcessWithGrace(ctx context.Context, pid int, grace time.Duration) {
	if pid <= 1 {
		slog.WarnContext(ctx, "kill_process_with_grace_invalid_pid",
			"pid", pid,
			"reason", "kill(-pid) with pid<=1 would signal the daemon's own process group or every process",
			"bead", "hk-bl2k6")
		return
	}

	target := pid
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		target = -pid
	} else {
		slog.WarnContext(ctx, "kill_process_with_grace_not_group_leader",
			"pid", pid, "pgid", pgid, "err", err,
			"reason", "pid is not its own process-group leader; signalling the single process only, since kill(-pid) would reach an unrelated group",
			"bead", "hk-3d9df")
	}
	_ = killProcessSignal(target, syscall.SIGTERM) //nolint:errcheck // best-effort; the process may already be gone and KillWindow is authoritative

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := killProcessSignal(target, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = killProcessSignal(target, syscall.SIGKILL) //nolint:errcheck // best-effort; KillWindow is authoritative
}

func killRemoteProcessWithGrace(ctx context.Context, runner tmux.CommandRunner, pid int, grace time.Duration) {
	steps := int(grace / (100 * time.Millisecond))
	if steps < 1 {
		steps = 1
	}
	script := fmt.Sprintf(
		"kill -TERM %d 2>/dev/null; n=0; while [ $n -lt %d ]; do kill -0 %d 2>/dev/null || exit 0; sleep 0.1; n=$((n+1)); done; kill -KILL %d 2>/dev/null",
		pid, steps, pid, pid,
	)
	cmd := runner.Command(ctx, "sh", "-c", script)
	_ = cmd.Run() //nolint:errcheck // best-effort; KillWindow is authoritative
}

// Wait blocks until the hosted process exits. It polls liveness at 500ms
// intervals and returns once the process is gone.
//
// When a pane PID was captured at SpawnWindow time (s.pid > 0), Wait polls
// process liveness directly via kill(pid, 0). This decouples liveness checking
// from tmux's name-resolution logic, which falls back silently to the session's
// active pane when the window name is no longer found — causing an infinite
// loop when Kill has already destroyed the window (hk-smuku).
//
// Secondary pane-presence check (hk-ry3be): when s.pid > 0 but processDead
// returns false, Wait also calls WindowPanePID on the slash-free pidTarget
// (hk-kuxxl — NOT the slash-bearing window-name handle).  If the
// window can no longer be found by tmux (ErrNoSession or ErrTmuxFailure), the
// daemon treats the pane as gone and unblocks even if the OS-level process is
// still reachable (e.g. a zombie, orphan, or launchd re-parented child).  This
// prevents the 15-hour heartbeat hang observed in the 2026-05-18 dogfood run
// (hk-ry3be): the tmux pane %29 disappeared but the shell PID remained alive in
// the OS process table, causing processDead to always return false.
//
// When s.pid == 0 (PID lookup failed at spawn time), Wait falls back to the
// WindowPanePID adapter call so that tests without real PIDs continue to work.
//
// If ctx is cancelled before the process exits, Wait returns ctx.Err().
func (s *tmuxSubstrateSession) Wait(ctx context.Context) error {
	s.waitOnce.Do(func() {
		go s.runWait(ctx)
	})
	select {
	case <-s.waitDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func processDead(pid int) bool {
	err := syscall.Kill(pid, 0)
	return errors.Is(err, syscall.ESRCH)
}

func (s *tmuxSubstrateSession) runWait(ctx context.Context) {
	defer close(s.waitDone)

	deadFn := s.isProcessDead
	if deadFn == nil {
		deadFn = processDead
	}

	startedAt := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			exitCode := exitCodeUnknown
			if s.pid > 0 && !s.remote && deadFn(s.pid) {
				exitCode = exitCodeClean
			}
			s.outcome = handler.Outcome{
				ExitCode: exitCode,
				Duration: time.Since(startedAt),
			}
			return
		case <-ticker.C:
			if s.pid > 0 && !s.remote {
				if deadFn(s.pid) {
					s.outcome = handler.Outcome{
						ExitCode: exitCodeClean,
						Duration: time.Since(startedAt),
					}
					return
				}
				if _, paneErr := s.adapter.WindowPanePID(ctx, s.panePIDTarget()); paneErr != nil {
					s.outcome = handler.Outcome{
						ExitCode: exitCodeUnknown, // pane gone, process state uncertain
						Duration: time.Since(startedAt),
					}
					return
				}
			} else {
				_, err := s.adapter.WindowPanePID(ctx, s.panePIDTarget())
				if err != nil {
					if tmux.IsSSHConnectionFailure(err) {
						s.outcome = handler.Outcome{
							ExitCode: exitCodeUnknown,
							Duration: time.Since(startedAt),
						}
						return
					}
					s.outcome = handler.Outcome{
						ExitCode: exitCodeClean,
						Duration: time.Since(startedAt),
					}
					return
				}
			}
		}
	}
}

// Outcome returns exit metadata once the Wait goroutine has finished.
//
// Semantics: Outcome blocks until the runWait goroutine closes waitDone.
// Because waitDone is initialized at SpawnWindow construction (not lazily
// inside waitOnce.Do), calling Outcome before Wait is safe — it will block
// until some caller eventually calls Wait (which launches the goroutine) and
// the goroutine finishes.  This prevents a silent zero-struct return when
// Outcome races ahead of Wait (hk-9to6j / R2).
//
// In the normal production call order (Wait → Outcome), waitDone is already
// closed and the receive returns instantly.
func (s *tmuxSubstrateSession) Outcome() handler.Outcome {
	<-s.waitDone
	return s.outcome
}

// PID returns the pane PID retrieved at spawn time. Returns 0 if unknown.
func (s *tmuxSubstrateSession) PID() int {
	return s.pid
}

func (s *tmuxSubstrateSession) panePIDTarget() tmux.WindowHandle {
	if s.pidTarget != "" {
		return s.pidTarget
	}
	return s.handle
}

// PaneTarget returns the tmux pane target string for this session: the stable
// pane ID ("%NNNN") captured at spawn time, or "handle.0" as a fallback, or
// empty string when neither is available.
//
// Implements paneTargeter, allowing perRunSubstrate.SpawnWindow to capture the
// pane target without hard-coding the tmuxSubstrateSession type (hk-012af).
func (s *tmuxSubstrateSession) PaneTarget() string {
	if s.paneID != "" {
		return s.paneID
	}
	if s.handle != "" {
		return string(s.handle) + ".0"
	}
	return ""
}

// WindowHandle returns the tmux window handle string (e.g. "session:window-name")
// for this session. Used by the crew handler to record the handle in the crew
// registry so crew-stop can tear down the pane.
//
// Implements windowHandleExposer (crewstart.go).
// Bead ref: hk-5tg5o (C2).
func (s *tmuxSubstrateSession) WindowHandle() string {
	return string(s.handle)
}

// Stdout returns nil: tmux-hosted sessions do not expose a stdout pipe to the
// daemon. The bridge wire is the daemon Unix socket (hook-relay). Handler.Launch
// detects nil and skips SpawnWatcher accordingly.
//
// Spec ref: handler-contract.md HC-054; design §4 "Substrate seam".
func (s *tmuxSubstrateSession) Stdout() io.Reader {
	return nil
}

func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
