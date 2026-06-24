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
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// Exit code constants for handler.Outcome.ExitCode values produced by the
// tmux substrate's runWait poll loop.
//
// Background (hk-cj0gm / hk-88nno): the EPERM/ESRCH distinction matters here.
//   - ESRCH (process not found) → process is dead → exitCodeClean
//   - EPERM (not permitted to signal) → process is alive → continue polling
//   - ctx-cancel with process still alive → exitCodeUnknown (daemon must NOT
//     classify this as a clean exit — it would suppress the claude_crashed branch)
//   - ctx-cancel with ESRCH (process gone before cancel is handled) → exitCodeClean
//   - pane gone externally (tmux kill-window) → exitCodeUnknown (process state
//     uncertain; workloop must use the crashed/unknown branch, not close-on-exit-0)
const (
	// exitCodeClean is returned when the polled process is confirmed dead via
	// ESRCH (processDead=true) or when the tmux pane's PID is no longer
	// resolvable and the PID was already unknown.  Triggers the
	// close-on-exit-0 fallback in the workloop.
	exitCodeClean = 0

	// exitCodeUnknown is returned when ctx is cancelled but the process is
	// still alive (EPERM / processDead=false), or when the pane disappears
	// externally while the PID is known.  Prevents misclassification as a
	// clean exit; the workloop's claude_crashed branch handles this.
	exitCodeUnknown = -1
)

// ──────────────────────────────────────────────────────────────────────────────
// pasteInjecter — optional interface for tmux-backed substrates
// ──────────────────────────────────────────────────────────────────────────────

// pasteInjecter is the optional interface implemented by perRunSubstrate.
//
// After SpawnWindow returns (the pane is live), callers check whether the
// substrate implements pasteInjecter and, if so, call WriteLastPane to deliver
// an initial instruction to the pane via the PL-021d paste mechanism.
//
// Implemented by perRunSubstrate (hk-012af, hk-jfh59). NOT implemented by
// tmuxSubstrate directly — use newPerRunSubstrate(tmuxSub) to get a substrate
// that implements this interface with per-run pane isolation.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — daemon→pane write mechanism.
// Bead ref: hk-zrj83, hk-jfh59.
type pasteInjecter interface {
	// WriteLastPane delivers payload to the pane spawned by this run's
	// SpawnWindow call.  bufferName MUST follow the "harmonik-<session-id>-<purpose>"
	// format required by PL-021d.  Returns a non-nil error if no window has
	// been spawned yet or if the underlying WriteToPane call fails.
	WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
}

// tmuxSubstrate implements handler.Substrate using a tmux.Adapter.
//
// The daemon composition root builds one tmuxSubstrate per daemon lifetime and
// injects it into handler.LaunchSpec.Substrate for every agent session that
// requires tmux hosting.
//
// Paste-inject operations (WriteLastPane, SendEnterToLastPane,
// SendQuitToLastPane) are NOT implemented on tmuxSubstrate directly. Instead,
// callers MUST wrap tmuxSubstrate in a perRunSubstrate (hk-012af) before
// calling SpawnWindow. perRunSubstrate captures the pane target of the window
// it spawned and routes paste-inject I/O there, ensuring per-goroutine
// isolation under MaxConcurrent>1.
//
// tmuxSubstrate also tracks every window it spawns in spawnedHandles so that
// KillAllWindows can clean them up on daemon exit or wave completion (hk-j6npz).
//
// All methods are safe for concurrent use.
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

	// spawnedMu guards spawnedHandles.
	spawnedMu sync.Mutex
	// spawnedHandles accumulates the WindowHandle of every window created by
	// SpawnWindow during this daemon instance's lifetime. KillAllWindows iterates
	// this slice to clean up orphan windows on wave completion or daemon exit.
	// Handles are appended-only; no removal on individual Kill calls (KillWindow
	// is idempotent on a non-existent window so re-killing is harmless).
	spawnedHandles []tmux.WindowHandle

	// spawnSem, when non-nil, is a buffered-channel semaphore of size cap+1 that
	// bounds the total number of concurrently active sessions. The +1 is the
	// reserved slot for terminal/consolidate nodes (hk-x882o). Nil when no cap
	// is configured (WithSpawnCap was not passed).
	//
	// Bead ref: hk-xb5yi (concurrent-spawn cap), hk-x882o (terminal reserve).
	spawnSem chan struct{}

	// nonTerminalSem, when non-nil, gates ordinary (non-terminal) spawns to the
	// user-configured cap. It has size cap — one fewer slot than spawnSem — so
	// that non-terminal sessions can never occupy the reserved +1 slot in
	// spawnSem. Terminal spawns bypass nonTerminalSem entirely and draw from
	// the reserved slot. Nil when no cap is configured.
	//
	// Bead ref: hk-x882o.
	nonTerminalSem chan struct{}

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

	// newWindowTimeout bounds how long SpawnWindow waits for the underlying
	// `tmux new-window` call (adapter.NewWindowIn) to return before treating the
	// launch as failed (hk-r1rup). A hung tmux invocation otherwise blocks
	// SpawnWindow → handler.Launch indefinitely (the no-spawn wedge); bounding it
	// converts that into a prompt, observable launch failure.
	//
	// Zero or negative disables the bound (blocks until ctx is cancelled, the
	// pre-hk-r1rup behaviour). NewTmuxSubstrate applies defaultNewWindowTimeout
	// when unset. Set via WithNewWindowTimeout.
	newWindowTimeout time.Duration

	// newWindowTimedOut, when non-nil, is invoked once when the `tmux new-window`
	// call does not return within newWindowTimeout. It is a diagnostic hook the
	// daemon wires to emit a tmux_new_window_timeout event (hk-r1rup). waited is
	// the duration spent blocked. Nil in tests that do not need the hook.
	newWindowTimedOut func(waited time.Duration)

	// spawnStagger, when positive, enforces a minimum interval between consecutive
	// window-creation calls (hk-hzj). Under a concurrent dispatch burst multiple
	// agent cold-starts compete for disk I/O and CPU simultaneously; spacing window
	// creation by spawnStagger reduces the peak contention window. Zero disables
	// staggering (the pre-hk-hzj behaviour). Set via WithSpawnStagger.
	//
	// lastWindowAt records when callNewWindowBounded last created a window.
	// Both are accessed only inside callNewWindowBounded while newWindowMu is held,
	// so they need no additional lock. Set via WithSpawnStagger.
	//
	// Bead ref: hk-hzj.
	spawnStagger time.Duration
	lastWindowAt time.Time // guarded by newWindowMu; set to time.Now() each SpawnWindow

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
// per in-flight bead). Override via HARMONIK_MAX_CONCURRENT_SESSIONS env var.
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
			s.spawnSem = make(chan struct{}, n+1) // +1 reserved for terminal spawns (hk-x882o)
			s.nonTerminalSem = make(chan struct{}, n)
		}
	}
}

// ErrSpawnCapTimeout is the sentinel wrapped by SpawnWindow when a non-terminal
// spawn cannot acquire a slot within spawnAcquireTimeout (hk-4l7zs, hk-x882o).
// Terminal spawns never return this error — they use a ctx-bounded wait that
// draws from the reserved +1 slot. The daemon launch paths detect it via
// errors.Is to emit a spawn_cap_blocked event with run context. It is also
// wrapped with handler.ErrStructural so existing structural-error handling
// continues to apply.
var ErrSpawnCapTimeout = errors.New("daemon: spawn cap acquire timed out")

// defaultSpawnAcquireTimeout is the default bound on how long SpawnWindow waits
// for a free spawn slot before failing the launch (hk-4l7zs). Generous enough to
// absorb a normal in-flight session finishing and releasing its slot, but far
// below the 30-min implementer commit budget so a leaked-slot wedge surfaces as
// a prompt launch failure rather than a 30-min no_commit timeout.
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

// defaultNewWindowTimeout is the default bound on how long SpawnWindow waits for
// the underlying `tmux new-window` call to return before treating the launch as
// failed (hk-r1rup). The actual shell call has no inherent timeout, so a hung
// tmux invocation (the recurring "no-spawn wedge") blocks handler.Launch
// indefinitely: launch_initiated never fires, the run wedges at
// launch_stall_detected → run_stale forever, holding a daemon slot until the
// 30-min implementer budget expires and it fails no_commit. Bounding the call
// converts that indefinite hang into a prompt, observable launch failure. Far
// below the 30-min budget so the wedge surfaces promptly, but generous enough to
// absorb a momentarily-busy tmux server under load.
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

// WithNewWindowTimeout sets the bound on how long SpawnWindow waits for the
// underlying `tmux new-window` call (adapter.NewWindowIn) to return before
// treating the launch as failed (hk-r1rup).
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
// so consecutive windows are always separated by at least d regardless of how many
// goroutines are concurrently waiting to spawn. The wait uses the caller's context,
// so an operator SIGTERM cancels a pending stagger and returns ErrStructural.
//
// Bead ref: hk-hzj.
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

// defaultSessionKeepaliveInterval is the default period between EnsureSession
// probes in RunSessionKeepalive (hk-9ptu). Short enough that a killed session
// is recreated well within the 30-min implementer commit budget; long enough
// that the tmux round-trip overhead is negligible.
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
			// Idempotent: if the session exists, EnsureSession returns nil
			// (duplicate-session treated as success). If the session was killed,
			// EnsureSession recreates it so the next SpawnWindow succeeds without
			// requiring the hk-yaj retry path.
			_ = se.EnsureSession(ctx, s.sessionName, "")
		}
	}
}

// setDiagnosticHooks installs the spawn-cap-blocked and new-window-timed-out
// diagnostic callbacks AFTER the substrate has been constructed (hk-oihnf).
//
// # Why a post-construction setter
//
// The substrate is built by the composition root (cmd/harmonik) inside
// daemon.Config BEFORE daemon.Start runs — but the event bus the hooks must emit
// onto does not exist until Start builds it. The WithSpawnCapBlockedHook /
// WithNewWindowTimedOutHook construction options therefore could not be wired at
// the call site (no bus in scope), which is exactly why s.spawnCapBlocked /
// s.newWindowTimedOut were left nil and the diagnostic events never fired from
// the substrate layer. Start probes cfg.Substrate for this setter once the bus is
// live and installs hooks that emit the non-run-scoped diagnostic events. (The
// run-scoped, runID-bearing emission already happens in the dispatch paths —
// workloop / reviewloop / dot_cascade — via errors.Is on the structural launch
// error.)
//
// Either fn may be nil to leave that hook unset.
func (s *tmuxSubstrate) setDiagnosticHooks(spawnCapBlocked func(waited time.Duration, inUse, capSize int), newWindowTimedOut func(waited time.Duration)) {
	if spawnCapBlocked != nil {
		s.spawnCapBlocked = spawnCapBlocked
	}
	if newWindowTimedOut != nil {
		s.newWindowTimedOut = newWindowTimedOut
	}
}

// substrateDiagnosticHookSetter is the optional interface a Substrate may
// implement to receive the spawn-cap-blocked / new-window-timed-out diagnostic
// hooks after construction (hk-oihnf). daemon.Start probes cfg.Substrate for this
// interface once the event bus is live and wires hooks that emit the diagnostic
// events. *tmuxSubstrate implements it.
type substrateDiagnosticHookSetter interface {
	setDiagnosticHooks(spawnCapBlocked func(waited time.Duration, inUse, capSize int), newWindowTimedOut func(waited time.Duration))
}

// Compile-time assertion: tmuxSubstrate implements handler.Substrate.
// Note: tmuxSubstrate does NOT implement pasteInjecter/enterSender/quitSender —
// those are implemented by perRunSubstrate (hk-jfh59).
var _ handler.Substrate = (*tmuxSubstrate)(nil)

// Compile-time assertion: tmuxSubstrate implements windowCleaner.
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
	// hk-4l7zs: when a spawn cap is configured but no explicit acquire timeout
	// was supplied, apply the default bound so a leaked slot surfaces as a prompt
	// launch failure instead of an indefinite SpawnWindow hang.
	if sub.spawnSem != nil && sub.spawnAcquireTimeout == 0 {
		sub.spawnAcquireTimeout = defaultSpawnAcquireTimeout
	}
	// hk-r1rup: when no explicit new-window timeout was supplied, apply the
	// default bound so a hung `tmux new-window` call surfaces as a prompt launch
	// failure instead of an indefinite SpawnWindow hang. Unlike the spawn-cap
	// acquire timeout this is NOT gated on a configured cap — the no-spawn wedge
	// can hang any new-window call regardless of whether a spawn cap is set.
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
// Kill. len(chan) on a buffered channel is the count of buffered (held) slots
// and is safe to read concurrently.
func (s *tmuxSubstrate) SpawnSlotsInUse() int {
	if s.spawnSem == nil {
		return 0
	}
	return len(s.spawnSem)
}

// SpawnCapSize reports the user-configured spawn-cap ceiling for non-terminal
// sessions (cap(nonTerminalSem)). Returns 0 when no cap is configured.
// Diagnostic accessor (hk-4l7zs). hk-x882o: returns the non-terminal cap (n),
// not the total semaphore capacity (n+1), so diagnostics report the value the
// operator configured rather than the internal oversized semaphore.
func (s *tmuxSubstrate) SpawnCapSize() int {
	if s.nonTerminalSem == nil {
		return 0
	}
	return cap(s.nonTerminalSem)
}

// substrateSpawnStats reports (slotsInUse, capSize) for a substrate that is, or
// wraps, a *tmuxSubstrate (hk-4l7zs). Returns (0, 0) for other substrates.
// Used by the daemon launch paths to enrich the spawn_cap_blocked event.
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

// releaseSpawnSlot returns a slot to the spawn semaphore for a non-terminal
// session. Kept for call sites that predate the terminal-reserve split (e.g.
// crew sessions outside the cap, and early-return error paths that haven't
// acquired nonTerminalSem). Callers that track terminality should prefer
// releaseSpawnSlotFor.
func (s *tmuxSubstrate) releaseSpawnSlot() {
	s.releaseSpawnSlotFor(false /* non-terminal: also release nonTerminalSem */)
}

// releaseSpawnSlotFor returns the slot(s) acquired by a spawn back to the
// semaphores. For non-terminal sessions it releases both spawnSem and
// nonTerminalSem. For terminal sessions it releases only spawnSem (the reserved
// slot). Called exactly once per session via the closure in
// tmuxSubstrateSession.releaseSlot. No-op when spawnSem is nil (hk-x882o).
func (s *tmuxSubstrate) releaseSpawnSlotFor(terminal bool) {
	if s.spawnSem == nil {
		return
	}
	select {
	case <-s.spawnSem:
	default:
		// Already released (should not happen since this is called inside
		// killOnce.Do, but guard defensively).
	}
	if !terminal && s.nonTerminalSem != nil {
		select {
		case <-s.nonTerminalSem:
		default:
			// Guard defensively.
		}
	}
}

// makeReleaseSlotFn returns a closure that releases the slot(s) acquired for a
// spawn. For non-terminal sessions the closure releases both spawnSem and
// nonTerminalSem. For terminal sessions it releases only spawnSem. The closure
// is stored in tmuxSubstrateSession.releaseSlot and called exactly once inside
// killOnce.Do (hk-x882o).
//
// Returns a no-op when spawnSem is nil (no cap configured).
func (s *tmuxSubstrate) makeReleaseSlotFn(terminal bool) func() {
	if s.spawnSem == nil {
		return func() {}
	}
	return func() {
		s.releaseSpawnSlotFor(terminal)
	}
}

// acquireSpawnSlot acquires the semaphore slot(s) required for a spawn.
//
// Non-terminal spawns (terminal==false): acquires nonTerminalSem first
// (bounded by spawnAcquireTimeout) then spawnSem (fast-path only — always
// available when non-terminal count ≤ cap, because spawnSem has cap+1 slots).
// Returns ErrSpawnCapTimeout (wrapped in ErrStructural) if nonTerminalSem is
// saturated and the timeout fires.
//
// Terminal spawns (terminal==true): acquires only spawnSem, bounded by ctx
// only (no timeout). The reserved +1 slot guarantees that a terminal spawn
// succeeds immediately whenever all non-terminal cap slots are occupied by
// non-terminal sessions — the incident scenario (hk-x882o).
//
// Returns nil when spawnSem is nil (no cap configured — no-op).
func (s *tmuxSubstrate) acquireSpawnSlot(ctx context.Context, terminal bool) error {
	if s.spawnSem == nil {
		return nil
	}

	if !terminal && s.nonTerminalSem != nil {
		// Non-terminal path: acquire nonTerminalSem first to prevent occupying
		// the reserved slot. Fast path first.
		select {
		case s.nonTerminalSem <- struct{}{}:
		default:
			// Slow path: wait, bounded by ctx and (when set) the acquire timeout.
			var timeoutCh <-chan time.Time
			if s.spawnAcquireTimeout > 0 {
				t := time.NewTimer(s.spawnAcquireTimeout)
				defer t.Stop()
				timeoutCh = t.C
			}
			start := time.Now()
			select {
			case s.nonTerminalSem <- struct{}{}:
			case <-ctx.Done():
				return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
					ctx.Err(), handler.ErrStructural)
			case <-timeoutCh:
				waited := time.Since(start)
				if s.spawnCapBlocked != nil {
					s.spawnCapBlocked(waited, s.SpawnSlotsInUse(), s.SpawnCapSize())
				}
				return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: no slot within %s (cap=%d in_use=%d): %w: %w",
					s.spawnAcquireTimeout, s.SpawnCapSize(), s.SpawnSlotsInUse(), ErrSpawnCapTimeout, handler.ErrStructural)
			}
		}
		// nonTerminalSem acquired. spawnSem must have room (non-terminal count ≤
		// cap; spawnSem capacity = cap+1). Fast-path acquire only.
		select {
		case s.spawnSem <- struct{}{}:
			return nil
		default:
			// Unexpected: nonTerminalSem was acquired but spawnSem is full. Release
			// nonTerminalSem to avoid a permanent hold, then return a structural error.
			select {
			case <-s.nonTerminalSem:
			default:
			}
			return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: unexpected spawnSem saturation after nonTerminalSem acquired: %w",
				handler.ErrStructural)
		}
	}

	// Terminal path: acquire only spawnSem, bounded by ctx only (no timeout so
	// reviewed work is never discarded due to spawn-slot starvation). Fast path first.
	select {
	case s.spawnSem <- struct{}{}:
		return nil
	default:
	}
	select {
	case s.spawnSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
			ctx.Err(), handler.ErrStructural)
	}
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
	// Local path (box A): use the shared adapter and the daemon-owned spawn-target
	// session, unchanged. Remote runs route through spawnWindowVia with an
	// SSH-backed adapter + a worker-scoped session (see perRunSubstrate.SpawnWindow).
	// NFR7: the local path is byte-identical to the pre-remote behaviour.
	return s.spawnWindowVia(ctx, in, s.adapter, s.sessionName, false /* local */)
}

// spawnWindowVia is the adapter/session-parameterised core of SpawnWindow. The
// local path passes s.adapter / s.sessionName (byte-identical to the original
// behaviour). The remote path (perRunSubstrate.SpawnWindow with a non-local
// runner) passes an SSH-backed adapter and a worker-scoped session so the
// `tmux new-window`, pane-PID resolution, and the spawned session's Wait/Kill
// all execute on the WORKER's tmux server rather than box A's.
//
// All shared machinery — spawn semaphore, new-window mutex, stagger, the
// new-window timeout, and spawnedHandles tracking — is preserved for both paths.
// The remote flag marks the spawned session as worker-hosted so runWait polls
// worker-side liveness instead of a local kill(s.pid,0) (hk-r1zq). Local callers
// pass false ⇒ unchanged behaviour (NFR7).
func (s *tmuxSubstrate) spawnWindowVia(ctx context.Context, in handler.SubstrateSpawn, adapter tmux.Adapter, sessionName string, remote bool) (handler.SubstrateSession, error) {
	// Acquire spawn semaphore slot(s) before creating the window. This enforces
	// the concurrent-session ceiling (hk-xb5yi). When the cap is not configured
	// (spawnSem is nil) this block is a no-op.
	//
	// hk-4l7zs: bounded acquire so a slot-saturated pool surfaces as a prompt
	// launch failure rather than an indefinite hang.
	//
	// hk-x882o: terminal/consolidate nodes use a reserved +1 slot so a
	// completed+reviewed run can always get its final merge node scheduled even
	// when all ordinary non-terminal slots are occupied. See acquireSpawnSlot.
	releaseSlotFn := s.makeReleaseSlotFn(in.Terminal)
	if err := s.acquireSpawnSlot(ctx, in.Terminal); err != nil {
		return nil, err
	}
	windowName := in.WindowName
	if windowName == "" {
		// Fallback: derive a deterministic name from the first argv component
		// when the caller hasn't set one. This should only happen in tests or
		// misconfigured callers; log a note for diagnostics.
		windowName = "hk-unnamed"
		if len(in.Argv) > 0 {
			parts := strings.Split(in.Argv[0], "/")
			if len(parts) > 0 {
				windowName = "hk-" + parts[len(parts)-1]
			}
		}
	}

	// Build the tmux.NewWindowIn from SubstrateSpawn.
	// Argv[0] is the binary; Argv[1:] are the arguments. tmux new-window passes
	// the joined string to `sh -c`, which re-word-splits on whitespace. Shell-
	// quote each element with single-quotes so argv values containing spaces
	// (e.g. the codex multi-word seed prompt) survive as a single sh token.
	// hk-rpr6: fixes codex ARGC=15 shattering that caused immediate exit-2.
	command := ""
	if len(in.Argv) > 0 {
		quoted := make([]string, len(in.Argv))
		for i, arg := range in.Argv {
			quoted[i] = shellQuoteArg(arg)
		}
		command = strings.Join(quoted, " ")
	}
	// hk-rpr6: ProcessExit harnesses (codex) run in a tmux pane whose PTY never
	// sends EOF, causing codex 0.139.0 to block on stdin indefinitely. Redirect
	// stdin from /dev/null so codex does not stall after completing its work.
	// The claude paste-inject path is unaffected (StdinDevNull is false for claude).
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

	// hk-r1rup: bound the underlying `tmux new-window` shell call. The call has
	// no inherent timeout, so a hung tmux invocation (the recurring "no-spawn
	// wedge") otherwise blocks here indefinitely — handler.Launch never returns,
	// launch_initiated never fires, and the run wedges at launch_stall_detected →
	// run_stale forever, holding a daemon slot until the 30-min implementer budget
	// expires and fails no_commit. callNewWindowBounded converts that indefinite
	// hang into a prompt, observable launch failure (tmux_new_window_timeout
	// diagnostic + ErrStructural). The semaphore slot is released on the timeout
	// path so the leak does not compound.
	outcome, timeoutErr := s.callNewWindowBounded(ctx, adapter, params)
	if timeoutErr != nil {
		releaseSlotFn()
		return nil, timeoutErr
	}
	if outcome.Err != nil {
		// hk-yaj: if the spawn-target session was externally killed, try to
		// re-ensure it and retry the window creation once before hard-failing.
		// This is the lazy-recovery symmetric to the boot-time EnsureSession in
		// main.go: boot ensures the session exists, SpawnWindow re-ensures it on
		// the first ErrNoSession at dispatch time so the whole fleet does not stall
		// until a daemon restart.
		recovered := false
		if errors.Is(outcome.Err, tmux.ErrNoSession) {
			if se, ok := adapter.(sessionEnsurer); ok {
				// Empty cwd preserves the box-A recovery behaviour byte-for-byte
				// (NFR7). For the remote path the worker session is ensured up
				// front (with the worker repo_path as cwd) in
				// perRunSubstrate.SpawnWindow, so this lazy recovery is a backstop.
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
			// Release the semaphore slot(s) before returning the error — the window
			// was never created so the slot is immediately available for reuse.
			releaseSlotFn()
			return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: %w: %w", outcome.Err, handler.ErrStructural)
		}
	}

	// Track the spawned window handle for cleanup on wave completion / daemon
	// exit (hk-j6npz). Appended under lock; reads happen only in KillAllWindows
	// (called after wg.Wait(), so no concurrent SpawnWindow calls are live).
	s.spawnedMu.Lock()
	s.spawnedHandles = append(s.spawnedHandles, outcome.Handle)
	s.spawnedMu.Unlock()

	// Resolve the slash-free pane ID BEFORE capturing the pane PID (hk-kuxxl).
	//
	// Prefer the pane ID captured atomically by NewWindowIn (hk-aievp fix):
	// outcome.PaneID is set by OSAdapter.NewWindowIn via `-P -F "#{pane_id}"`,
	// which captures the ID in the same tmux invocation that creates the window.
	// This avoids a follow-up WindowPaneID call that uses the slash-bearing
	// "session:window-name" handle — tmux misparsing that handle when the window
	// name is a filesystem path caused the stale-pane misdirect (pane %22 instead
	// of the fresh %27).
	//
	// Fall back to a separate WindowPaneID call only when outcome.PaneID is empty
	// (e.g. fake adapters in tests that do not yet set PaneID, or future adapter
	// implementations that do not support -P -F).
	//
	// hk-yngq2: window name is a worktree path with slashes — tmux cannot parse
	// "session:path/to/dir.0" as a pane target; "%NNNN" is always slash-free.
	paneID := outcome.PaneID
	if paneID == "" {
		if id, paneIDErr := adapter.WindowPaneID(ctx, outcome.Handle); paneIDErr == nil {
			paneID = id
		}
	}

	// pidTarget is the handle used for all #{pane_pid} resolution (here and in
	// runWait's secondary pane-presence check). When a slash-free pane ID was
	// resolved, target the pane directly via "%NNNN"; otherwise fall back to the
	// slash-bearing "session:window-name" handle.
	//
	// hk-kuxxl: the slash-bearing handle (window name = "<bead_id>/i<n>",
	// windowname.go WM-002a) makes `tmux display-message -t session:bead/i1` MISPARSE
	// the target and SILENTLY FALL BACK to the session's currently-active pane.
	// Under MaxConcurrent>1, concurrent SpawnWindow calls then capture a SIBLING
	// run's pane PID into s.pid. When the fast sibling's pane shell exits, the slow
	// siblings' runWait sees the aliased s.pid as dead via processDead(s.pid),
	// returns exitCodeClean=0, ends the implementer phase prematurely, and the
	// no-commit guard fails the run (no_commit_during_implementer ... exit=0).
	// Using the slash-free pane ID pins PID resolution to THIS run's pane.
	pidTarget := outcome.Handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}

	// Retrieve the pane PID immediately so SubstrateSession.PID() is available.
	pid, pidErr := adapter.WindowPanePID(ctx, pidTarget)
	if pidErr != nil {
		// PID retrieval failure is non-fatal: the window is alive. Log and
		// continue with pid=0; callers should not depend on PID for correctness.
		pid = 0
	}

	// waitDone is initialized here at construction so that callers of Outcome()
	// that arrive before Wait() is called can block on the channel rather than
	// observe a nil-channel receive (which would block forever) or a zero struct
	// (which is silently wrong).  waitOnce then guards only the goroutine launch,
	// not the channel allocation — the channel is always valid after SpawnWindow
	// returns.  See architectural review R2 (hk-9to6j).
	//
	// releaseSlot is the spawn-cap slot release callback (hk-xb5yi, hk-x882o).
	// It is called exactly once inside killOnce.Do so the semaphore slot(s) are
	// returned when the session ends. The closure captures in.Terminal so that
	// terminal sessions release only spawnSem and non-terminal sessions release
	// both spawnSem and nonTerminalSem.
	sess := &tmuxSubstrateSession{
		adapter:     adapter,
		handle:      outcome.Handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		remote:      remote,
		waitDone:    make(chan struct{}),
		releaseSlot: releaseSlotFn,
	}
	return sess, nil
}

// callNewWindowBounded invokes adapter.NewWindowIn with a bound on how long the
// underlying `tmux new-window` shell call may take (hk-r1rup). The call runs in
// a goroutine so a hung tmux invocation — one that returns NEITHER a value nor
// an error — cannot block SpawnWindow forever even if the adapter ignores ctx
// cancellation. The select races the call's completion against a bounded
// context (newWindowTimeout) and the caller's ctx.
//
// Returns (outcome, nil) when the call completes in time — the caller then
// inspects outcome.Err as before. Returns (zero, err) when the call does not
// return within the bound (err wraps ErrTmuxNewWindowTimeout + ErrStructural,
// firing the newWindowTimedOut diagnostic hook) or the caller's ctx is cancelled
// (err wraps ErrStructural). A non-positive newWindowTimeout disables the bound,
// blocking until the call returns or the caller's ctx is cancelled — the
// pre-hk-r1rup behaviour.
//
// The bounded ctx is passed to NewWindowIn so a ctx-aware adapter (OSAdapter
// uses exec.CommandContext) also gets its tmux subprocess SIGKILLed on timeout;
// the goroutine+select wrapper is the backstop for adapters that ignore ctx.
func (s *tmuxSubstrate) callNewWindowBounded(ctx context.Context, adapter tmux.Adapter, params tmux.NewWindowIn) (tmux.Outcome, error) {
	// hk-oihnf: serialize the new-window exec daemon-wide. The mutex is held ONLY
	// for the duration of this bounded call (the tmux-server-lock contention
	// point), never across the semaphore acquire or spec-build in SpawnWindow. The
	// defer guarantees release on EVERY return path — success, adapter error, the
	// new-window timeout, and the caller-ctx-cancelled path below — so a hung
	// new-window holds the mutex for at most newWindowTimeout (the bound below
	// fires, this function returns, and the unlock runs).
	s.newWindowMu.Lock()
	defer s.newWindowMu.Unlock()

	// hk-hzj: spawn stagger — enforce a minimum interval between consecutive
	// window creations to reduce concurrent cold-start contention. Under a burst of
	// N dispatches all claude agents start near-simultaneously, competing for disk
	// I/O and CPU during cold-start; with disk at ≥90% utilisation this pushed
	// cold-start past the (then-)30s agent_ready_timeout. Spacing window creation
	// by spawnStagger gives each agent a head start before the next competes for
	// the same resources.
	//
	// The stagger runs inside newWindowMu so lastWindowAt is updated atomically
	// with window creation — no separate mutex needed. The wait uses ctx (not
	// callCtx) so an operator SIGTERM cancels a pending stagger immediately without
	// being subject to the new-window timeout. The mutex is held during the sleep;
	// this extends how long newWindowMu is held per call by at most spawnStagger,
	// which is acceptable since the 60s bound (defaultNewWindowTimeout) already
	// allows multi-second holds for slow tmux servers.
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
		s.lastWindowAt = time.Now()
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if s.newWindowTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, s.newWindowTimeout)
		defer cancel()
	}

	type result struct {
		outcome tmux.Outcome
	}
	// Buffered so the goroutine never leaks if we return on the timeout path
	// before it finishes (the hung-tmux case).
	resCh := make(chan result, 1)
	start := time.Now()
	go func() {
		resCh <- result{outcome: adapter.NewWindowIn(callCtx, params)}
	}()

	select {
	case r := <-resCh:
		return r.outcome, nil
	case <-callCtx.Done():
		waited := time.Since(start)
		// Distinguish the caller's ctx cancellation from our own bounded timeout.
		// The bounded timeout fires only when the caller's ctx is still live, so
		// check the parent first.
		if ctx.Err() != nil {
			return tmux.Outcome{}, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: tmux new-window: context cancelled: %w: %w",
				ctx.Err(), handler.ErrStructural)
		}
		if s.newWindowTimedOut != nil {
			s.newWindowTimedOut(waited)
		}
		return tmux.Outcome{}, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: tmux new-window did not return within %s: %w: %w",
			s.newWindowTimeout, ErrTmuxNewWindowTimeout, handler.ErrStructural)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// perRunSubstrate — per-bead-run substrate wrapper (hk-012af)
// ─────────────────────────────────────────────────────────────────────────────

// perRunSubstrate wraps a tmuxSubstrate and captures the pane ID of the single
// window spawned for one bead run. It implements handler.Substrate for
// SpawnWindow (delegating to the shared substrate) and the three paste-inject
// interfaces (pasteInjecter, enterSender, quitSender) using the captured pane
// ID rather than the shared "lastPaneID" field on tmuxSubstrate.
//
// # Why this exists
//
// Under MaxConcurrent>1, two concurrent beadRunOne goroutines both call
// handler.Launch → SpawnWindow. If paste-inject state were stored on the shared
// tmuxSubstrate, the second SpawnWindow would overwrite the pane target from the
// first, causing the first run's kick-off message to land in the wrong pane,
// waitAgentReady to hang indefinitely, and both runs to stall. (hk-012af
// dogfood: 7-hour stall after two run_started events at 22:29:08 UTC on
// 2026-05-20.)
//
// perRunSubstrate carries per-goroutine pane state so each run targets exactly
// the pane it spawned. The vestigial shared-state methods on tmuxSubstrate were
// removed in hk-jfh59.
//
// # Usage
//
//	prs := newPerRunSubstrate(tmuxSub)
//	spec.Substrate = prs                     // used for handler.Launch
//	// ... after Launch returns ...
//	go pasteInjectOnLaunch(ctx, prs, ...)    // safe: prs.paneID is run-local
//	go pasteInjectQuitOnCommit(ctx, prs, ...) // same
//
// Bead ref: hk-012af.
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

	// remoteAdapter caches the SSH-backed adapter built once at SpawnWindow time
	// from the inner adapter via WithRunner(runner). Paste-inject calls
	// (WriteLastPane / SendEnter / SendQuit) and PaneHasActiveProcess's PID
	// resolution use it so all tmux I/O for a remote run reaches the worker's
	// tmux server. Nil for local runs (paste-inject uses inner.adapter, unchanged).
	//
	// Bead ref: remote-substrate worker-spawn gap.
	remoteAdapter tmux.Adapter

	// onConnectionFailure is called when an SSH connection failure (exit-255) is
	// detected during PaneHasActiveProcess. Wired by the workloop for remote runs
	// to emit worker_offline and disable the worker in-memory. Nil for local runs.
	//
	// Bead: hk-rs-b11-offline-dh57.
	onConnectionFailure func(ctx context.Context, detail string)
}

// commandRunner returns the effective CommandRunner for this run: the
// caller-supplied runner when set, otherwise tmux.LocalRunner{} (unchanged
// local behaviour).  Implements commandRunnerProvider so
// pasteInjectQuitOnCommit can route git and process probes through the same
// runner as PaneHasActiveProcess.
//
// Bead: hk-rs-b9-liveness-1m9n.
func (p *perRunSubstrate) commandRunner() tmux.CommandRunner {
	if p.runner != nil {
		return p.runner
	}
	return tmux.LocalRunner{}
}

// Compile-time assertions for perRunSubstrate.
var (
	_ handler.Substrate     = (*perRunSubstrate)(nil)
	_ pasteInjecter         = (*perRunSubstrate)(nil)
	_ enterSender           = (*perRunSubstrate)(nil)
	_ quitSender            = (*perRunSubstrate)(nil)
	_ paneLivenessChecker   = (*perRunSubstrate)(nil)
	_ paneOutputSizer       = (*perRunSubstrate)(nil)
	_ commandRunnerProvider = (*perRunSubstrate)(nil)
)

// paneTargeter is an optional interface a SubstrateSession may implement to
// expose its specific tmux pane target string (e.g. "%1964").  perRunSubstrate
// probes for this interface on the SubstrateSession returned by SpawnWindow so
// it can capture the pane target without a hard dependency on the concrete
// tmuxSubstrateSession type.
//
// Test doubles that need per-run pane isolation (e.g. the hk-012af concurrent
// dispatch test) implement this interface to expose the pane target assigned at
// spawn time.
type paneTargeter interface {
	// PaneTarget returns the tmux pane target string for this session.
	// Returns an empty string when no pane target is available.
	PaneTarget() string
}

// substrateWithAdapter is an optional interface a Substrate may implement to
// expose the underlying tmux.Adapter.  perRunSubstrate probes for this
// interface so it can call WriteToPane/SendKeysEnter/SendKeysQuit directly on
// the adapter using the captured per-run pane target.
//
// *tmuxSubstrate implements this interface.  Test doubles may implement it as
// well to allow perRunSubstrate to route paste-inject calls to a recording
// adapter.
type substrateWithAdapter interface {
	tmuxAdapter() tmux.Adapter
}

// tmuxAdapter exposes the adapter field of tmuxSubstrate so perRunSubstrate can
// call it on the concrete type.  This satisfies substrateWithAdapter.
func (s *tmuxSubstrate) tmuxAdapter() tmux.Adapter { return s.adapter }

// substrateWithSessionName is an optional interface a Substrate may implement to
// expose the tmux session name it spawns implementer windows into.  The boot
// orphan sweep probes for this so it can EXCLUDE the daemon's own spawn-target
// session from the session-level kill sweep (hk-9vp51): when the daemon falls
// back to a freshly-created "harmonik-<hash>-default" session, that session has
// only an idle zsh window at boot, so sessionIsOrphaned would classify it as
// orphaned and the daemon's own sweep would kill it before the first dispatch —
// reproducing the original sub-fix #3 "session does not exist" regression.
type substrateWithSessionName interface {
	daemonSessionName() string
}

// daemonSessionName exposes the session name this substrate spawns windows into,
// satisfying substrateWithSessionName (hk-9vp51).
func (s *tmuxSubstrate) daemonSessionName() string { return s.sessionName }

// substrateWithKeepalive is an optional interface a Substrate may implement to
// expose a background keepalive loop for its daemon-owned spawn-target session
// (hk-9ptu). daemon.Start probes cfg.Substrate for this interface after the
// boot orphan sweep and starts RunSessionKeepalive as a goroutine when found.
//
// Only tmuxSubstrate instances built with WithSessionKeepalive satisfy this
// interface (keepaliveEnabled=true). Normal "live ambient session" substrates
// do not implement it — their session is managed by the operator's shell.
type substrateWithKeepalive interface {
	RunSessionKeepalive(ctx context.Context)
}

// Compile-time assertion: *tmuxSubstrate always satisfies substrateWithKeepalive.
// RunSessionKeepalive is a no-op when keepaliveEnabled=false (WithSessionKeepalive
// was not passed), so daemon.Start can unconditionally start the goroutine for
// any *tmuxSubstrate — it exits immediately for the non-keepalive path.
var _ substrateWithKeepalive = (*tmuxSubstrate)(nil)

// substrateWithSpawnCap is an optional interface a Substrate may implement to
// expose the user-configured non-terminal spawn ceiling (cap(nonTerminalSem)).
// daemon.Start probes cfg.Substrate for this interface when wiring the
// HandlerAdapter so that HandleQueueSetConcurrency can reject set-concurrency
// requests that would oversubscribe the spawn cap (hk-vfeeo).
//
// Returns 0 when no cap is configured.
//
// Bead ref: hk-vfeeo.
type substrateWithSpawnCap interface {
	SpawnCapSize() int
}

// Compile-time assertion: *tmuxSubstrate satisfies substrateWithSpawnCap.
// SpawnCapSize returns 0 when no cap is configured (nonTerminalSem is nil).
var _ substrateWithSpawnCap = (*tmuxSubstrate)(nil)

// substrateSpawnReadier is an optional interface a Substrate may implement to
// expose a lightweight pre-dispatch spawn-readiness probe. daemon.Start probes
// cfg.Substrate for this interface after a restart-backoff boot (hk-bk33):
// when the backoff delay is non-zero, Start launches ProbeSpawnReady in a
// goroutine and closes a channel when it returns; runWorkLoop waits on that
// channel (spawnSubstrateReadyCh) before its first dispatch tick.
//
// Bead ref: hk-bk33.
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
	handles := make([]tmux.WindowHandle, len(s.spawnedHandles))
	copy(handles, s.spawnedHandles)
	s.spawnedMu.Unlock()

	for _, h := range handles {
		// Ignore errors: the window may have already been killed by
		// tmuxSubstrateSession.Kill or by an external tmux kill-window command.
		_ = s.adapter.KillWindow(ctx, h)
	}
	return nil
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
	// Best-effort /quit: sends /quit\n to the first pane of the window.
	// Errors here are swallowed; the KillWindow below is authoritative.
	paneTarget := handle + ".0"
	_ = s.adapter.SendKeysQuit(ctx, paneTarget) //nolint:errcheck // best-effort; kill is authoritative

	// Grace period: wait for the crew session to exit cleanly before hard kill.
	select {
	case <-ctx.Done():
		// Context cancelled — proceed to kill immediately.
	case <-time.After(crewStopQuitGrace):
	}

	return s.adapter.KillWindow(ctx, tmux.WindowHandle(handle))
}

// crewStopQuitGrace is the grace period between sending /quit and force-killing
// a crew window in StopWindowByHandle (C2 crew-stop path).
const crewStopQuitGrace = 30 * time.Second

// ─────────────────────────────────────────────────────────────────────────────
// Crew independent-session support (hk-mmlqt)
// ─────────────────────────────────────────────────────────────────────────────

// sessionCreator is an optional interface a tmux.Adapter may implement to
// create a new independent tmux session atomically with a running command.
//
// Implemented by tmux.OSAdapter (NewSessionIn method). NOT added to the
// tmux.Adapter interface to avoid breaking existing test doubles (hk-mmlqt).
type sessionCreator interface {
	NewSessionIn(ctx context.Context, params tmux.NewWindowIn) tmux.Outcome
}

// sessionEnsurer is an optional interface a tmux.Adapter may implement to
// create-or-recover the named session (idempotent). SpawnWindow uses this on
// ErrNoSession to lazily re-create the daemon's spawn-target session when it
// has been externally killed, rather than hard-failing.
//
// Implemented by tmux.OSAdapter (EnsureSession method). NOT added to the
// tmux.Adapter interface to avoid breaking existing test doubles (hk-yaj).
type sessionEnsurer interface {
	EnsureSession(ctx context.Context, name, workDir string) error
}

// runnerSwapper is an optional interface a tmux.Adapter may implement to return
// a copy of itself that tunnels every tmux command through a different
// CommandRunner (e.g. an SSHRunner targeting a remote worker). This is the seam
// the remote-substrate path uses so a remote run's `tmux new-window`,
// pane-PID resolution, paste-inject, and session Wait/Kill all execute on the
// WORKER's tmux server rather than box A's.
//
// Implemented by tmux.OSAdapter (WithRunner method, value receiver returning a
// copy). NOT added to the tmux.Adapter interface to avoid breaking existing
// test doubles.
//
// Bead ref: remote-substrate worker-spawn gap (worker tmux session never
// created; `tmux new-window` targeted box A's session over the local runner).
type runnerSwapper interface {
	WithRunner(r tmux.CommandRunner) tmux.OSAdapter
}

// Compile-time assertion: tmux.OSAdapter (the production adapter) satisfies
// runnerSwapper, so the remote-substrate spawn path can swap in an SSH runner.
var _ runnerSwapper = tmux.OSAdapter{}

// crewSessionSpawner is an optional interface a Substrate may implement to
// spawn a crew member in its own independent tmux session (hk-mmlqt).
//
// When the substrate implements this interface, HandleCrewStart uses
// SpawnCrewSession instead of SpawnWindow so crew sessions are independent of
// the daemon's session and survive daemon SIGTERM / supervisor-revive cycles.
//
// *tmuxSubstrate implements crewSessionSpawner.
type crewSessionSpawner interface {
	// SpawnCrewSession creates an independent tmux session for crewName and runs
	// spawn.Argv inside it. The session name is derived via crewSessionName.
	SpawnCrewSession(ctx context.Context, crewName string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error)
}

// crewSessionStopper is an optional interface a Substrate may implement to
// kill the independent tmux session for a named crew member (hk-mmlqt).
//
// When the substrate implements this interface, HandleCrewStop uses
// StopCrewSession instead of StopWindowByHandle so the whole independent
// session is cleanly torn down.
//
// *tmuxSubstrate implements crewSessionStopper.
type crewSessionStopper interface {
	// StopCrewSession sends /quit to the crew pane (best-effort), waits a grace
	// period, then kills the crew's dedicated tmux session.
	StopCrewSession(ctx context.Context, crewName string, handle string) error
}

// crewSessionName returns the deterministic tmux session name for crewName.
//
// The name is ALWAYS the project-qualified form
// "harmonik-<projectHash>-crew-<crewName>" routed through
// lifecycle.TmuxSessionName (one prefix family for the whole fleet, per the
// tmux-session-organization CONTRACT §"Shared symbols"). The legacy
// "hk-crew-<crewName>" no-hash form was DELETED (hk-rmy1, slice C): there is no
// fallback. In production the project hash is always present (both daemon
// construction sites pass WithCrewProjectHash(ComputeProjectHash(projectDir))),
// so the error path is a defensive guard rather than a real runtime branch —
// surfacing a missing hash as an error is strictly safer than minting a session
// under a name outside the swept "harmonik-<hash>-*" namespace.
func (s *tmuxSubstrate) crewSessionName(name string) (string, error) {
	if s.projectHash == "" {
		return "", fmt.Errorf("daemon: crewSessionName: project hash unavailable for crew %q "+
			"(NewTmuxSubstrate must be built with WithCrewProjectHash): %w", name, handler.ErrStructural)
	}
	return lifecycle.TmuxSessionName(s.projectHash, "crew-"+name), nil
}

// workerSpawnSessionName returns the tmux session name a REMOTE run spawns its
// implementer/reviewer window into ON THE WORKER. This session lives on the
// worker's own tmux server (created via the SSH-backed adapter's EnsureSession),
// so it never collides with box A's "-default" session.
//
// When projectHash is set: "harmonik-<projectHash>-worker-<workerName>" — one
// shared spawn-target session per worker, mirroring how box A shares one
// "-default" session for all its local runs. A single shared worker session is
// safe because each run gets its OWN window (and worktree) inside it.
//
// Fallback (no projectHash / no workerName): the box-A spawn-target session
// name (s.sessionName). On the worker's own tmux server this is still a fresh,
// collision-free session that EnsureSession creates.
//
// Bead ref: remote-substrate worker-spawn gap.
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
// prior daemon restart), SpawnCrewSession returns an error so the caller can
// decide whether to stop-and-restart or leave the existing crew running.
func (s *tmuxSubstrate) SpawnCrewSession(ctx context.Context, crewName string, spawn handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	sc, ok := s.adapter.(sessionCreator)
	if !ok {
		return nil, fmt.Errorf("daemon: SpawnCrewSession: adapter does not support session creation: %w", handler.ErrStructural)
	}

	sessName, nameErr := s.crewSessionName(crewName)
	if nameErr != nil {
		return nil, fmt.Errorf("daemon: SpawnCrewSession: %w", nameErr)
	}

	// The crew's claude --remote-control always runs in the "agent" window
	// (tmux.WindowAgent), per the tmux-session-organization CONTRACT. The keeper
	// targets this window's active pane via "--tmux <session>:agent".
	command := ""
	if len(spawn.Argv) > 0 {
		command = strings.Join(spawn.Argv, " ")
	}

	params := tmux.NewWindowIn{
		Session:    sessName,
		WindowName: tmux.WindowAgent,
		Env:        spawn.Env,
		WorkDir:    spawn.Cwd,
		Command:    command,
	}

	outcome := sc.NewSessionIn(ctx, params)
	if outcome.Err != nil {
		return nil, fmt.Errorf("daemon: SpawnCrewSession: new-session for crew %q: %w", crewName, outcome.Err)
	}

	// Add the sibling "keeper" window (tmux.WindowKeeper) in the SAME session,
	// running the per-crew session-keeper. The keeper injects into the agent
	// window's active pane (slice K: "--tmux <session>:agent"). Because the keeper
	// lives in its own window it survives an agent-window respawn (invariant I1)
	// and is torn down with the session on crew-stop. Best-effort: a failed keeper
	// window does NOT fail the crew start — the agent is already live.
	s.spawnCrewKeeperWindow(ctx, crewName, sessName, spawn)

	paneID := outcome.PaneID
	pidTarget := outcome.Handle
	if paneID != "" {
		pidTarget = tmux.WindowHandle(paneID)
	}
	pid, _ := s.adapter.WindowPanePID(ctx, pidTarget)

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
		// RespawnCmd left empty — see doc comment (follow-up).
	})
}

// spawnCrewKeeperWindow creates the "keeper" window inside the crew's session
// (already created by SpawnCrewSession) and launches the per-crew keeper in it.
//
// projectDir is derived from the crew spawn: spawn.Cwd is the crew's WorkDir
// (the project root per buildCrewLaunchSpec), with HARMONIK_PROJECT from
// spawn.Env as a fallback. The keeper binary is the currently-running harmonik
// executable (os.Executable, "harmonik" on failure), matching the CLI crew
// keeper resolution.
//
// Best-effort: any failure (no project dir, NewWindowIn error) is logged and
// returns without failing the crew start — the agent window is already live and
// an operator can attach a keeper externally (the .managed marker still records
// the crew as keeper-managed).
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

	outcome := s.adapter.NewWindowIn(ctx, params)
	if outcome.Err != nil {
		fmt.Fprintf(os.Stderr, "daemon: SpawnCrewSession: launch keeper window for crew %q (%s:%s): %v (non-fatal)\n",
			crewName, sessName, tmux.WindowKeeper, outcome.Err)
	}
}

// shellJoinArgv single-quotes each argv element and joins with spaces so the
// command survives `tmux new-window`'s `sh -c` re-word-splitting (mirrors the
// SpawnWindow quoting at the top of spawnWindowVia). The keeper inject target
// "<session>:agent" contains no shell metacharacters, but the binary path and
// project dir may contain spaces, so quote uniformly.
//
// Delegates to the SHARED agentlaunch.ShellJoinArgv (review outcome A) so the
// daemon and the CLI captain launcher quote argv identically.
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
	// Best-effort /quit to the first pane of the crew window.
	if handle != "" {
		paneTarget := handle + ".0"
		_ = s.adapter.SendKeysQuit(ctx, paneTarget) //nolint:errcheck // best-effort; session kill is authoritative
	}

	// Grace period before hard kill.
	select {
	case <-ctx.Done():
	case <-time.After(crewStopQuitGrace):
	}

	sessName, nameErr := s.crewSessionName(crewName)
	if nameErr != nil {
		return nameErr
	}
	// Killing the whole crew session tears down BOTH the "agent" and "keeper"
	// windows (and the keeper process running in the keeper window). No separate
	// keeper-window teardown is needed on crew-stop.
	return s.adapter.KillSession(ctx, sessName)
}

// newPerRunSubstrate constructs a perRunSubstrate that delegates SpawnWindow to
// sub and captures the spawned pane target from the returned SubstrateSession.
//
// handlerBinary is the handler executable path (e.g. "claude" or a custom
// binary).  It is used to derive agentCommandFragments for pane-liveness
// matching via agentCommandFragmentsFor; pass "" to fall back to the global
// livePaneCommandSubstrings default.
//
// runner is the CommandRunner used for liveness probes (pgrep, ps) and
// worktree git probes (rev-parse HEAD, git status).  Pass nil to fall back to
// tmux.LocalRunner{} (unchanged local behaviour).  For remote-substrate workers
// (B9) pass the SSHRunner built from the worker's host config.
//
// When sub implements substrateWithAdapter, perRunSubstrate can forward
// paste-inject calls to the underlying tmux.Adapter using the captured pane
// target; this is the production path (*tmuxSubstrate).
//
// When sub does NOT implement substrateWithAdapter but the returned session
// implements paneTargeter, perRunSubstrate can record the pane target — paste
// inject calls will fail gracefully (no adapter available) but the pane routing
// is still isolated, which is sufficient for test fixtures that do not need
// actual paste-inject to succeed.
//
// Returns nil when sub is nil (safe: the caller falls back to the shared-substrate
// path which is correct for the exec.CommandContext / no-tmux code path).
//
// Bead: hk-rs-b9-liveness-1m9n (runner parameter).
func newPerRunSubstrate(sub handler.Substrate, handlerBinary string, runner tmux.CommandRunner) *perRunSubstrate {
	// test seam (hk-fxy9 / hk-538l regression); no-op in prod. Fired with the
	// `runner` ARG this constructor actually received — the single source of truth
	// for the SUBSTRATE-SPAWN runner. A regression that drops a call site's 3rd arg
	// back to nil is caught here regardless of whether sub is a *tmuxSubstrate (the
	// review-loop/DOT test fixtures use a non-tmux substrate, so the returned *prs
	// is nil and the stored field would be unobservable — observing the arg is not).
	notifySubstrateRunner(runner)
	if sub == nil {
		return nil
	}
	ts, ok := sub.(*tmuxSubstrate)
	if !ok {
		// deps.substrate is not a *tmuxSubstrate (e.g. a test double that does not
		// implement substrateWithAdapter). Return nil so the caller falls back to
		// the original shared-substrate path for test doubles that don't need pane
		// isolation (e.g. spy substrates in pasteinject_hk2hb2y_test.go).
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
	// Remote path: a non-nil runner marks this as a remote run. Route the spawn
	// through an SSH-backed adapter + worker-scoped session so `tmux new-window`,
	// pane-PID resolution, and the spawned session's Wait/Kill all execute on the
	// WORKER's tmux server. Without this the inner SpawnWindow would target box
	// A's local tmux server and the box-A "-default" session, which does NOT
	// exist on the worker — the launch wedges at launch_initiated with no spawn.
	//
	// NFR7: when runner is nil (local run) we fall through to the unchanged
	// p.inner.SpawnWindow delegation below, byte-identical to the pre-remote path.
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
	// Capture the pane target from the just-spawned session via paneTargeter.
	// tmuxSubstrateSession implements paneTargeter (PaneTarget() string).
	if pt, ok := sess.(paneTargeter); ok {
		if target := pt.PaneTarget(); target != "" {
			p.paneTargetMu.Lock()
			p.cachedPaneTarget = target
			p.paneTargetMu.Unlock()
		}
	}
	return sess, nil
}

// spawnWindowRemote performs the remote-run spawn: it builds an SSH-backed
// adapter from the inner adapter (WithRunner(p.runner)), ENSURES the worker's
// target tmux session exists on the worker (idempotent — mirroring box A's
// "-default" EnsureSession), then delegates to p.inner.spawnWindowVia with the
// remote adapter + worker session so the `tmux new-window` and the spawned
// session's pane-PID/Wait/Kill all run on the WORKER's tmux server.
//
// The ensured remote adapter is cached on p.remoteAdapter so the paste-inject
// methods (WriteLastPane / SendEnterToLastPane / SendQuitToLastPane) and
// PaneHasActiveProcess's PID resolution route through the worker too.
//
// Worker session name: p.workerSessionName (worker-scoped, set by the
// workloop). Session cwd: p.workerSessionCwd (the worker's repo_path).
//
// Bead ref: remote-substrate worker-spawn gap.
func (p *perRunSubstrate) spawnWindowRemote(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	// The inner adapter must support runner-swapping (OSAdapter does). A test
	// double that does not implement runnerSwapper cannot exercise the remote
	// path; fail closed with a structural error rather than silently spawning
	// against box A's local tmux.
	sw, ok := p.inner.adapter.(runnerSwapper)
	if !ok {
		return nil, fmt.Errorf("daemon: perRunSubstrate.spawnWindowRemote: inner adapter does not support WithRunner (cannot target worker tmux): %w", handler.ErrStructural)
	}
	remoteAdapter := tmux.Adapter(sw.WithRunner(p.runner))

	sessName := p.workerSessionName
	if sessName == "" {
		// Defensive: a remote run without an explicit worker session name still
		// must NOT spawn into box A's "-default". Reuse the inner (box-A
		// project-hash-derived) session name; on the worker's own tmux server
		// this name is collision-free, and EnsureSession below creates it.
		sessName = p.inner.sessionName
	}

	// ENSURE the worker session exists on the worker BEFORE new-window. This is
	// the fix: nothing else creates a target session on a remote worker, so
	// `tmux new-window -t <session>` would fail/hang. EnsureSession is idempotent
	// (`tmux has-session || tmux new-session -d -s <name> -c <cwd>` semantics via
	// the duplicate-session-is-success path) and runs over the SSH runner.
	if se, ok := remoteAdapter.(sessionEnsurer); ok {
		if ensErr := se.EnsureSession(ctx, sessName, p.workerSessionCwd); ensErr != nil {
			return nil, fmt.Errorf("daemon: perRunSubstrate.spawnWindowRemote: ensure worker session %q: %w: %w", sessName, ensErr, handler.ErrStructural)
		}
	}

	// Cache the remote adapter for paste-inject + liveness PID resolution.
	p.remoteAdapter = remoteAdapter

	return p.inner.spawnWindowVia(ctx, in, remoteAdapter, sessName, true /* remote: worker-hosted, runWait must poll worker liveness not local kill (hk-r1zq) */)
}

// pasteAdapter returns the adapter that paste-inject and liveness PID-resolution
// calls should target: the cached remote (SSH-backed) adapter for a remote run,
// otherwise the shared inner adapter (unchanged local behaviour, NFR7).
func (p *perRunSubstrate) pasteAdapter() tmux.Adapter {
	if p.remoteAdapter != nil {
		return p.remoteAdapter
	}
	return p.inner.adapter
}

// paneTarget returns the tmux pane target captured at SpawnWindow time for
// this run's pane.  Returns empty string when SpawnWindow has not yet been
// called or when the spawned session did not expose a pane target via
// paneTargeter.
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
	// Use a tmux.WindowHandle from the per-run pane target. WindowPanePID
	// accepts either a "%NNNN" pane ID or a "session:window.index" handle;
	// paneTarget() already returns the stable pane ID captured at spawn time.
	// For a remote run pasteAdapter() resolves the pane PID on the WORKER's tmux
	// server; the probeLivenessOrSSHFail below then checks that PID over the same
	// SSH runner.
	pid, err := p.pasteAdapter().WindowPanePID(ctx, tmux.WindowHandle(target))
	if err != nil || pid <= 0 {
		return false
	}
	r := p.commandRunner()
	// Use the SSH-failure-aware probe so that an unreachable worker (exit-255)
	// is reported to the workloop for worker_offline emission and in-memory
	// disable (B11). The run still returns false (not wedged) and recovers via
	// the existing run_stale path.
	alive, connFailed := probeLivenessOrSSHFail(ctx, r, pid, p.agentCommandFragments)
	if connFailed {
		p.notifyConnectionFailure(ctx, "liveness probe returned ssh exit-255")
	}
	return alive
}

// notifyConnectionFailure calls p.onConnectionFailure if set.
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
	// Route through the per-run CommandRunner so the probe queries the tmux
	// server that actually hosts this run's pane: the WORKER's tmux for a REMOTE
	// run (p.runner is an SSHRunner), box A's tmux for a LOCAL run (p.runner nil
	// ⇒ LocalRunner, which execs the identical bare `tmux display-message` — NFR7
	// byte-identical). A bare exec.CommandContext here would query box A's tmux
	// for a remote run (the wrong pane), silently disabling the output-growth
	// ceiling-kill safety probe for remote runs.
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

// ─────────────────────────────────────────────────────────────────────────────
// tmuxSubstrateSession — handler.SubstrateSession backed by a tmux.WindowHandle
// ─────────────────────────────────────────────────────────────────────────────

// tmuxSubstrateSession implements handler.SubstrateSession for a tmux-hosted
// subprocess. The session is identified by a tmux.WindowHandle; lifecycle
// operations (Kill) issue tmux commands via the stored adapter.
//
// Wait blocks until the pane PID disappears from the OS process table (polled
// at 500ms intervals). This is a best-effort implementation for MVH; a
// production implementation would use tmux wait-for or a side-channel signal.
//
// All methods are safe for concurrent use.
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

// Kill terminates the hosted process and then destroys the tmux window.
// Idempotent: subsequent calls return nil.
//
// When the session holds a pane PID (s.pid > 0), Kill sends SIGTERM to the
// process and waits up to killGracePeriod for it to exit. If the process is
// still alive after the grace period, Kill sends SIGKILL. It then calls
// KillWindow to remove the tmux window regardless of whether the PID step
// succeeded. This ensures that killing the tmux window shell alone (which
// previously sent SIGHUP to the child) is not relied upon to terminate the
// hosted process.
//
// Background: the tmux pane PID is the shell that was started by tmux
// new-window. The hosted claude process is a child of that shell. Sending
// SIGTERM/SIGKILL directly to the shell is more reliable than relying on
// tmux kill-window to propagate a signal to the child process.
const killGracePeriod = 3 * time.Second

func (s *tmuxSubstrateSession) Kill(ctx context.Context) error {
	var killErr error
	s.killOnce.Do(func() {
		// Step 1: terminate the hosted process if we have a PID.
		if s.pid > 0 {
			killProcessWithGrace(s.pid, killGracePeriod)
		}
		// Step 2: destroy the tmux window (cleans up pane/window state).
		killErr = s.adapter.KillWindow(ctx, s.handle)
		// Step 3: release the spawn semaphore slot (hk-xb5yi). No-op when
		// no cap was configured (releaseSlot is nil).
		if s.releaseSlot != nil {
			s.releaseSlot()
		}
	})
	return killErr
}

// killProcessWithGrace sends SIGTERM to pid, waits up to grace for the process
// to exit, then sends SIGKILL if it is still alive. It is a best-effort
// helper: all errors are silently swallowed because the window cleanup in
// KillWindow is the authoritative cleanup step.
func killProcessWithGrace(pid int, grace time.Duration) {
	// Send SIGTERM. Ignore errors: process may already be gone.
	_ = syscall.Kill(pid, syscall.SIGTERM)

	// Poll for process exit using kill(pid, 0) which returns ESRCH when gone.
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			// ESRCH means no such process — it has exited.
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Grace period elapsed; escalate to SIGKILL.
	_ = syscall.Kill(pid, syscall.SIGKILL)
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
	// waitOnce guards only the goroutine launch.  waitDone is always non-nil
	// because SpawnWindow initializes it at construction (hk-9to6j / R2).
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

// processDead reports whether the process with the given pid is no longer
// alive in the OS process table.
//
// It sends signal 0 to the pid (kill(pid, 0)) and interprets the result:
//   - nil error  → process exists and we own it → alive
//   - ESRCH      → no such process                → dead
//   - EPERM      → process exists but is owned by another user (e.g. PID
//     recycled after the original process exited)  → treat as alive, not dead,
//     to avoid a false "process gone" classification
//   - any other errno → treat as alive (conservative)
func processDead(pid int) bool {
	err := syscall.Kill(pid, 0)
	return errors.Is(err, syscall.ESRCH)
}

// runWait polls until the hosted process/window exits, then populates outcome.
func (s *tmuxSubstrateSession) runWait(ctx context.Context) {
	defer close(s.waitDone)

	// deadFn is the liveness predicate. Tests inject a stub via isProcessDead;
	// production uses the package-level processDead (hk-88nno).
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
			// Context cancelled: do one final pid check before reporting -1.
			// If the pid is already gone (common when claude exits cleanly and
			// the ctx is cancelled by the grace timer or workloop teardown),
			// report exitCode=0 so the workloop's close-on-exit-0 fallback
			// fires instead of the claude_crashed branch.
			// Diagnostic note (hk-cj0gm / hk-ajhqw): the 5-minute gap between
			// claude exit (21:18) and run_failed (21:23:36) was caused by a
			// zombie/slow-poll race where processDead returned false during the
			// polling ticks; ctx.Done() fired first with exitCode=-1, causing
			// a false claude_crashed classification.
			exitCode := exitCodeUnknown
			// hk-r1zq: only trust the local kill(s.pid,0) for a LOCAL session. A
			// remote session's s.pid is the WORKER's pane PID — local deadFn would
			// return ESRCH ("dead") unconditionally and falsely report exitCodeClean
			// on a mid-flight cancel while claude is still alive on the worker, which
			// can drive the close-on-exit-0 fallback to close an incomplete bead. For
			// remote we leave exitCodeUnknown (honest "unverified"); the Stop-hook
			// socket outcome, when present, takes precedence for the real
			// classification, and otherwise the incomplete/crashed branch handles it.
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
				// Fast path (LOCAL sessions only): check OS process table directly.
				// This avoids the tmux display-message fallback that returns the
				// active-pane PID when the window name is no longer resolvable
				// (hk-smuku).
				//
				// hk-r1zq: a REMOTE session's s.pid is the WORKER's pane PID, which
				// is NOT in the daemon host's process table — processDead(s.pid) =
				// kill(pid,0) would return ESRCH ("dead") on the first tick and
				// falsely conclude exitCodeClean while claude is still running on the
				// worker. Remote sessions therefore fall through to the worker-side
				// WindowPanePID poll below (s.adapter routes over the run's SSH
				// runner), treating worker-pane-gone as the exit signal.
				if deadFn(s.pid) {
					s.outcome = handler.Outcome{
						ExitCode: exitCodeClean,
						Duration: time.Since(startedAt),
					}
					return
				}
				// Secondary check: the OS process appears alive, but the tmux
				// pane may have been killed externally (tmux kill-window, session
				// closed, or host process survived as an orphan/zombie).  If the
				// window is gone from tmux's perspective, unblock immediately so
				// the daemon does not hang indefinitely emitting heartbeats for a
				// pane that no longer exists (hk-ry3be dogfood-blocker).
				//
				// hk-kuxxl: resolve via the slash-free pidTarget, not s.handle —
				// the slash-bearing window-name handle makes tmux fall back to the
				// session's active pane, which under MaxConcurrent>1 reports a
				// sibling's pane and produces a false "pane gone" classification.
				if _, paneErr := s.adapter.WindowPanePID(ctx, s.panePIDTarget()); paneErr != nil {
					s.outcome = handler.Outcome{
						ExitCode: exitCodeUnknown, // pane gone, process state uncertain
						Duration: time.Since(startedAt),
					}
					return
				}
				// Process and pane both appear alive — continue polling.
			} else {
				// Worker / PID-unknown path: poll pane liveness via WindowPanePID.
				// Reached by (a) a LOCAL session whose spawn-time PID fetch failed
				// (s.pid==0) and (b) every REMOTE session (hk-r1zq) — for a remote
				// run s.adapter is the SSH-backed worker adapter, so this resolves
				// #{pane_pid} on the WORKER's tmux server, i.e. it polls the actual
				// claude process on the worker rather than a local kill.
				// hk-kuxxl: use the slash-free pidTarget for the same reason.
				_, err := s.adapter.WindowPanePID(ctx, s.panePIDTarget())
				if err != nil {
					// Window or session gone (the worker pane closed when claude
					// exited) — treat as process exited.
					s.outcome = handler.Outcome{
						ExitCode: exitCodeClean,
						Duration: time.Since(startedAt),
					}
					return
				}
				// Window still alive — continue polling.
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

// panePIDTarget returns the handle used to resolve this session's #{pane_pid}.
//
// It prefers the slash-free pidTarget captured at SpawnWindow time (the
// "%NNNN" pane ID) and falls back to the slash-bearing window-name handle only
// when pidTarget was never populated (e.g. legacy test doubles that construct a
// session directly without going through SpawnWindow). Using the slash-free
// target prevents tmux from misparsing the window-name handle and falling back
// to the session's active pane — the root cause of the concurrent-wave
// implementer-phase-barrier failure (hk-kuxxl).
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

// shellQuoteArg wraps s in POSIX single-quotes, escaping any embedded
// single-quote characters so the argument survives `sh -c` word-splitting as a
// single token. Used by spawnWindowVia to protect multi-word argv values (e.g.
// the codex seed prompt) from being shattered by the sh -c join.
//
// Bead: hk-rpr6.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
