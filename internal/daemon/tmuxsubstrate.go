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
	"sync/atomic"
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
// All methods are safe for concurrent use.
type tmuxSubstrate struct {
	adapter     tmux.Adapter
	sessionName string
}

// Compile-time assertion: tmuxSubstrate implements handler.Substrate.
// Note: tmuxSubstrate does NOT implement pasteInjecter/enterSender/quitSender —
// those are implemented by perRunSubstrate (hk-jfh59).
var _ handler.Substrate = (*tmuxSubstrate)(nil)

// NewTmuxSubstrate constructs a tmuxSubstrate that delegates to adapter and
// creates new windows in sessionName.
//
// adapter MUST be non-nil. sessionName MUST be non-empty.
//
// The daemon composition root calls NewTmuxSubstrate after ProbeTmux and
// ResolveSession have succeeded per the PL-005 startup sequence.
func NewTmuxSubstrate(adapter tmux.Adapter, sessionName string) handler.Substrate {
	if adapter == nil {
		panic("daemon: NewTmuxSubstrate: adapter is nil — daemon defect")
	}
	if sessionName == "" {
		panic("daemon: NewTmuxSubstrate: sessionName is empty — daemon defect")
	}
	return &tmuxSubstrate{
		adapter:     adapter,
		sessionName: sessionName,
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
// Returns a non-nil error (wrapping handler.ErrStructural) when the tmux
// adapter reports a failure.
//
// Spec ref: process-lifecycle.md §4.7 PL-021b obligation 1.
func (s *tmuxSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
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

	outcome := s.adapter.NewWindowIn(ctx, params)
	if outcome.Err != nil {
		return nil, fmt.Errorf("daemon: tmuxSubstrate.SpawnWindow: %w: %w", outcome.Err, handler.ErrStructural)
	}

	// Retrieve the pane PID immediately so SubstrateSession.PID() is available.
	pid, pidErr := s.adapter.WindowPanePID(ctx, outcome.Handle)
	if pidErr != nil {
		// PID retrieval failure is non-fatal: the window is alive. Log and
		// continue with pid=0; callers should not depend on PID for correctness.
		pid = 0
	}

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

	// waitDone is initialized here at construction so that callers of Outcome()
	// that arrive before Wait() is called can block on the channel rather than
	// observe a nil-channel receive (which would block forever) or a zero struct
	// (which is silently wrong).  waitOnce then guards only the goroutine launch,
	// not the channel allocation — the channel is always valid after SpawnWindow
	// returns.  See architectural review R2 (hk-9to6j).
	sess := &tmuxSubstrateSession{
		adapter:  s.adapter,
		handle:   outcome.Handle,
		paneID:   paneID,
		pid:      pid,
		waitDone: make(chan struct{}),
	}
	return sess, nil
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
}

// Compile-time assertions for perRunSubstrate.
var _ handler.Substrate = (*perRunSubstrate)(nil)
var _ pasteInjecter = (*perRunSubstrate)(nil)
var _ enterSender = (*perRunSubstrate)(nil)
var _ quitSender = (*perRunSubstrate)(nil)

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

// newPerRunSubstrate constructs a perRunSubstrate that delegates SpawnWindow to
// sub and captures the spawned pane target from the returned SubstrateSession.
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
func newPerRunSubstrate(sub handler.Substrate) *perRunSubstrate {
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
	return &perRunSubstrate{inner: ts}
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
	pid    int

	// killOnce ensures Kill is idempotent.
	killOnce sync.Once

	// outcomeReady is set once Wait has completed.
	outcomeReady atomic.Bool
	outcome      handler.Outcome

	// waitDone is closed when the Wait goroutine finishes.
	waitDone chan struct{}
	waitOnce sync.Once

	// isProcessDead is the liveness predicate used by runWait. In production
	// it is nil and processDead (the package-level function) is called directly.
	// Tests inject a deterministic stub via the function-valued field to exercise
	// the ctx.Done() and tick paths without real OS processes (hk-88nno).
	isProcessDead func(pid int) bool
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
// returns false, Wait also calls WindowPanePID on the stored handle.  If the
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
			s.outcomeReady.Store(true)
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
					s.outcomeReady.Store(true)
					return
				}
				// Secondary check: the OS process appears alive, but the tmux
				// pane may have been killed externally (tmux kill-window, session
				// closed, or host process survived as an orphan/zombie).  If the
				// window is gone from tmux's perspective, unblock immediately so
				// the daemon does not hang indefinitely emitting heartbeats for a
				// pane that no longer exists (hk-ry3be dogfood-blocker).
				if _, paneErr := s.adapter.WindowPanePID(ctx, s.handle); paneErr != nil {
					s.outcome = handler.Outcome{
						ExitCode: exitCodeUnknown, // pane gone, process state uncertain
						Duration: time.Since(startedAt),
					}
					s.outcomeReady.Store(true)
					return
				}
				// Process and pane both appear alive — continue polling.
			} else {
				// Slow path: PID unknown; fall back to WindowPanePID.
				_, err := s.adapter.WindowPanePID(ctx, s.handle)
				if err != nil {
					// Window or session gone — treat as process exited.
					s.outcome = handler.Outcome{
						ExitCode: exitCodeClean,
						Duration: time.Since(startedAt),
					}
					s.outcomeReady.Store(true)
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
