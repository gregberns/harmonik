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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
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

	// spawnSem, when non-nil, is a buffered-channel semaphore that bounds the
	// number of concurrently active sessions. Each SpawnWindow acquires a slot;
	// the slot is released when the session's Kill() is called (once per session
	// via killOnce). Nil when no cap is configured (WithSpawnCap was not passed).
	//
	// Bead ref: hk-xb5yi (concurrent-spawn cap).
	spawnSem chan struct{}

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
// Bead ref: hk-xb5yi.
func WithSpawnCap(n int) TmuxSubstrateOption {
	return func(s *tmuxSubstrate) {
		if n > 0 {
			s.spawnSem = make(chan struct{}, n)
		}
	}
}

// ErrSpawnCapTimeout is the sentinel wrapped by SpawnWindow when it cannot
// acquire a spawn-semaphore slot within spawnAcquireTimeout (hk-4l7zs). The
// daemon launch paths detect it via errors.Is to emit a spawn_cap_blocked event
// with run context. It is also wrapped with handler.ErrStructural so existing
// structural-error handling continues to apply.
var ErrSpawnCapTimeout = errors.New("daemon: spawn cap acquire timed out (possible slot leak)")

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

// SpawnCapSize reports the configured spawn-cap ceiling (the channel capacity).
// Returns 0 when no cap is configured. Diagnostic accessor (hk-4l7zs).
func (s *tmuxSubstrate) SpawnCapSize() int {
	if s.spawnSem == nil {
		return 0
	}
	return cap(s.spawnSem)
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

// releaseSpawnSlot returns a slot to the spawn semaphore. Called exactly once
// per session via the callback stored in tmuxSubstrateSession.releaseSlot.
// No-op when spawnSem is nil.
func (s *tmuxSubstrate) releaseSpawnSlot() {
	if s.spawnSem == nil {
		return
	}
	select {
	case <-s.spawnSem:
	default:
		// Already released (should not happen under normal operation since
		// releaseSpawnSlot is called inside killOnce.Do, but guard defensively).
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
	// Acquire a spawn semaphore slot before creating the window. This enforces
	// the concurrent-session ceiling (hk-xb5yi). When the cap is not configured
	// (spawnSem is nil) this block is a no-op.
	//
	// hk-4l7zs: the acquire is bounded by spawnAcquireTimeout. Before the fix,
	// SpawnWindow blocked here forever when every slot was held by a leaked
	// (acquired-but-never-released) session — the run sat at launch_initiated
	// with no tmux window until the 30-min implementer budget expired and it
	// failed no_commit. A bounded wait turns that indefinite wedge into a prompt,
	// observable launch failure (spawn_cap_blocked diagnostic + ErrStructural).
	if s.spawnSem != nil {
		// Fast path: slot immediately available.
		select {
		case s.spawnSem <- struct{}{}:
			// Slot acquired; proceed.
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
			case s.spawnSem <- struct{}{}:
				// Slot acquired after waiting; proceed.
			case <-ctx.Done():
				return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: context cancelled: %w: %w",
					ctx.Err(), handler.ErrStructural)
			case <-timeoutCh:
				waited := time.Since(start)
				if s.spawnCapBlocked != nil {
					s.spawnCapBlocked(waited, s.SpawnSlotsInUse(), s.SpawnCapSize())
				}
				return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: spawn cap: no slot within %s (cap=%d in_use=%d): %w: %w",
					s.spawnAcquireTimeout, s.SpawnCapSize(), s.SpawnSlotsInUse(), ErrSpawnCapTimeout, handler.ErrStructural)
			}
		}
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
	// Argv[0] is the binary; Argv[1:] are the arguments. tmux new-window takes
	// a single shell command string, so we join with spaces. The argv is
	// validated by the caller (buildClaudeLaunchSpec) before reaching here.
	command := ""
	if len(in.Argv) > 0 {
		command = strings.Join(in.Argv, " ")
	}

	params := tmux.NewWindowIn{
		Session:    s.sessionName,
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
	outcome, timeoutErr := s.callNewWindowBounded(ctx, params)
	if timeoutErr != nil {
		s.releaseSpawnSlot()
		return nil, timeoutErr
	}
	if outcome.Err != nil {
		// Release the semaphore slot before returning the error — the window was
		// never created so the slot is immediately available for reuse.
		s.releaseSpawnSlot()
		return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: %w: %w", outcome.Err, handler.ErrStructural)
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
		if id, paneIDErr := s.adapter.WindowPaneID(ctx, outcome.Handle); paneIDErr == nil {
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
	pid, pidErr := s.adapter.WindowPanePID(ctx, pidTarget)
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
	// releaseSlot is the spawn-cap slot release callback (hk-xb5yi). It is called
	// exactly once inside killOnce.Do so the semaphore slot is returned when the
	// session ends. When no cap was configured, releaseSpawnSlot is a no-op.
	sess := &tmuxSubstrateSession{
		adapter:     s.adapter,
		handle:      outcome.Handle,
		paneID:      paneID,
		pidTarget:   pidTarget,
		pid:         pid,
		waitDone:    make(chan struct{}),
		releaseSlot: s.releaseSpawnSlot,
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
func (s *tmuxSubstrate) callNewWindowBounded(ctx context.Context, params tmux.NewWindowIn) (tmux.Outcome, error) {
	// hk-oihnf: serialize the new-window exec daemon-wide. The mutex is held ONLY
	// for the duration of this bounded call (the tmux-server-lock contention
	// point), never across the semaphore acquire or spec-build in SpawnWindow. The
	// defer guarantees release on EVERY return path — success, adapter error, the
	// new-window timeout, and the caller-ctx-cancelled path below — so a hung
	// new-window holds the mutex for at most newWindowTimeout (the bound below
	// fires, this function returns, and the unlock runs).
	s.newWindowMu.Lock()
	defer s.newWindowMu.Unlock()

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
		resCh <- result{outcome: s.adapter.NewWindowIn(callCtx, params)}
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
}

// Compile-time assertions for perRunSubstrate.
var _ handler.Substrate = (*perRunSubstrate)(nil)
var _ pasteInjecter = (*perRunSubstrate)(nil)
var _ enterSender = (*perRunSubstrate)(nil)
var _ quitSender = (*perRunSubstrate)(nil)
var _ paneLivenessChecker = (*perRunSubstrate)(nil)

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

// newPerRunSubstrate constructs a perRunSubstrate that delegates SpawnWindow to
// sub and captures the spawned pane target from the returned SubstrateSession.
//
// handlerBinary is the handler executable path (e.g. "claude" or a custom
// binary).  It is used to derive agentCommandFragments for pane-liveness
// matching via agentCommandFragmentsFor; pass "" to fall back to the global
// livePaneCommandSubstrings default.
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
func newPerRunSubstrate(sub handler.Substrate, handlerBinary string) *perRunSubstrate {
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
	return p.inner.adapter.WriteToPane(ctx, bufferName, target, payload)
}

// SendEnterToLastPane sends a bare Enter key to this run's pane.
//
// Implements enterSender.
func (p *perRunSubstrate) SendEnterToLastPane(ctx context.Context) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.SendEnterToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.inner.adapter.SendKeysEnter(ctx, target)
}

// SendQuitToLastPane sends /quit followed by Enter to this run's pane.
//
// Implements quitSender.
func (p *perRunSubstrate) SendQuitToLastPane(ctx context.Context) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.SendQuitToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.inner.adapter.SendKeysQuit(ctx, target)
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
	pid, err := p.inner.adapter.WindowPanePID(ctx, tmux.WindowHandle(target))
	if err != nil || pid <= 0 {
		return false
	}
	if hasAnyDirectChild(pid) {
		return true
	}
	return commandMatchesLiveAgent(pid, p.agentCommandFragments)
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
			if s.pid > 0 && deadFn(s.pid) {
				exitCode = exitCodeClean
			}
			s.outcome = handler.Outcome{
				ExitCode: exitCode,
				Duration: time.Since(startedAt),
			}
			return
		case <-ticker.C:
			if s.pid > 0 {
				// Fast path: check OS process table directly. This avoids the
				// tmux display-message fallback that returns the active-pane PID
				// when the window name is no longer resolvable (hk-smuku).
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
				// Slow path: PID unknown; fall back to WindowPanePID.
				// hk-kuxxl: use the slash-free pidTarget for the same reason.
				_, err := s.adapter.WindowPanePID(ctx, s.panePIDTarget())
				if err != nil {
					// Window or session gone — treat as process exited.
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

// Stdout returns nil: tmux-hosted sessions do not expose a stdout pipe to the
// daemon. The bridge wire is the daemon Unix socket (hook-relay). Handler.Launch
// detects nil and skips SpawnWatcher accordingly.
//
// Spec ref: handler-contract.md HC-054; design §4 "Substrate seam".
func (s *tmuxSubstrateSession) Stdout() io.Reader {
	return nil
}
