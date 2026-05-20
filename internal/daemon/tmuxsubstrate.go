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

// ──────────────────────────────────────────────────────────────────────────────
// pasteInjecter — optional interface for tmux-backed substrates
// ──────────────────────────────────────────────────────────────────────────────

// pasteInjecter is the optional interface implemented by tmux-backed Substrates.
//
// After SpawnWindow returns (the pane is live), callers may check whether the
// substrate also implements pasteInjecter and, if so, call WriteLastPane to
// deliver an initial instruction to the pane via the PL-021d paste mechanism.
//
// The method name WriteLastPane reflects that the target pane is the most
// recently spawned window — not an arbitrary pane. This matches the MVH
// usage pattern (one window per dispatch at MaxConcurrent=1). Post-MVH
// parallel dispatch will require a pane-handle API; for now this is sufficient.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — daemon→pane write mechanism.
// Bead ref: hk-zrj83.
type pasteInjecter interface {
	// WriteLastPane delivers payload to the pane of the most recently spawned
	// window.  bufferName MUST follow the "harmonik-<session-id>-<purpose>"
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
// All methods are safe for concurrent use.
type tmuxSubstrate struct {
	adapter     tmux.Adapter
	sessionName string

	// lastHandleMu guards lastHandle and lastPaneID.
	lastHandleMu sync.Mutex
	// lastHandle is the WindowHandle of the most recently spawned window.
	// Set by SpawnWindow; read by WriteLastPane.  Zero value means no window
	// has been spawned yet.
	lastHandle tmux.WindowHandle
	// lastPaneID is the stable tmux pane identifier (e.g. "%1964") for the
	// first pane of the most recently spawned window. Captured once by
	// SpawnWindow via WindowPaneID; used by WriteLastPane as the pane target
	// instead of the slash-bearing "session:window-name.0" form (hk-yngq2).
	// Empty string means the lookup failed; WriteLastPane falls back to the
	// legacy handle+".0" form in that case.
	lastPaneID string
}

// Compile-time assertions.
var _ handler.Substrate = (*tmuxSubstrate)(nil)
var _ pasteInjecter = (*tmuxSubstrate)(nil)
var _ enterSender = (*tmuxSubstrate)(nil)
var _ quitSender = (*tmuxSubstrate)(nil)

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

	// Record the handle and pane ID so WriteLastPane can reference them.
	// This assignment happens BEFORE returning the session, so any subsequent
	// WriteLastPane call on this substrate will use the just-spawned pane.
	s.lastHandleMu.Lock()
	s.lastHandle = outcome.Handle
	s.lastPaneID = paneID
	s.lastHandleMu.Unlock()

	sess := &tmuxSubstrateSession{
		adapter: s.adapter,
		handle:  outcome.Handle,
		paneID:  paneID,
		pid:     pid,
	}
	return sess, nil
}

// WritePane delivers payload to this session's pane via the PL-021d
// load-buffer + paste-buffer sequence.  The pane target is the stable pane
// ID captured atomically at SpawnWindow time; it is per-session and is NOT
// affected by subsequent SpawnWindow calls on the same substrate (hk-wx8z8).
//
// Falls back to "handle.0" only when the pane ID lookup failed at spawn time
// (legacy behaviour for test doubles that do not implement WindowPaneID).
//
// bufferName MUST match "harmonik-<session-id>-<purpose>" per PL-021d.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d.
// Bead: hk-wx8z8 (parallel-dispatch fix), hk-zrj83 (original mechanism).
func (s *tmuxSubstrateSession) WritePane(ctx context.Context, bufferName string, payload []byte) error {
	target := s.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: tmuxSubstrateSession.WritePane: no pane recorded: %w", tmux.ErrStructural)
	}
	return s.adapter.WriteToPane(ctx, bufferName, target, payload)
}

// SendEnter sends a bare Enter keypress to this session's pane.  Used to
// dismiss the Claude Code welcome splash before paste-inject (hk-rf4ux).
//
// Bead: hk-wx8z8 (per-session routing), hk-rf4ux (splash mechanism).
func (s *tmuxSubstrateSession) SendEnter(ctx context.Context) error {
	target := s.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: tmuxSubstrateSession.SendEnter: no pane recorded: %w", tmux.ErrStructural)
	}
	return s.adapter.SendKeysEnter(ctx, target)
}

// SendQuit sends `/quit Enter` to this session's pane to trigger Claude Code's
// Stop hook (CHB-028 session-completion-instruction).
//
// Bead: hk-wx8z8 (per-session routing), hk-cmybm (mechanism).
func (s *tmuxSubstrateSession) SendQuit(ctx context.Context) error {
	target := s.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: tmuxSubstrateSession.SendQuit: no pane recorded: %w", tmux.ErrStructural)
	}
	return s.adapter.SendKeysQuit(ctx, target)
}

// paneTarget returns the tmux pane target string for this session.  Prefers
// the atomically captured paneID ("%NNNN"); falls back to handle+".0" only
// when paneID is empty (test-double compatibility).
func (s *tmuxSubstrateSession) paneTarget() string {
	if s.paneID != "" {
		return s.paneID
	}
	if s.handle == "" {
		return ""
	}
	return string(s.handle) + ".0"
}

// SendEnterToLastPane sends a bare "Enter" key event to the first pane of the
// most recently spawned window via `tmux send-keys -t <pane> Enter`.
//
// This bypasses bracketed-paste mode so that TUI applications (e.g. Claude
// Code's React/ink welcome splash) receive it as a real keypress.
//
// Implements the [enterSender] interface (pasteinject.go) used by
// pasteInjectOnLaunch to dismiss the splash before the kick-off message
// arrives (hk-rf4ux).
//
// Returns [tmux.ErrStructural] (wrapped) when no window has been spawned yet.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — send-keys Enter.
// Bead: hk-rf4ux.
func (s *tmuxSubstrate) SendEnterToLastPane(ctx context.Context) error {
	s.lastHandleMu.Lock()
	handle := s.lastHandle
	paneID := s.lastPaneID
	s.lastHandleMu.Unlock()

	if handle == "" {
		return fmt.Errorf("daemon: tmuxSubstrate.SendEnterToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}

	var paneTarget string
	if paneID != "" {
		paneTarget = paneID
	} else {
		paneTarget = string(handle) + ".0"
	}
	return s.adapter.SendKeysEnter(ctx, paneTarget)
}

// SendQuitToLastPane sends `/quit` followed by Enter to the first pane of the
// most recently spawned window via `tmux send-keys -t <pane> /quit Enter`.
//
// Both `/quit` and `Enter` are dispatched as real key events (not through
// bracketed-paste mode), causing Claude Code's interactive REPL to execute
// the /quit slash command and exit the session.  This fires the Stop hook,
// which delivers outcome_emitted to the daemon socket and unblocks the
// workloop's waitWithSocketGrace call.
//
// Called from pasteInjectQuitOnCommit after the task commit lands in the
// worktree.
//
// Returns [tmux.ErrStructural] (wrapped) when no window has been spawned yet.
//
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Bead: hk-cmybm.
func (s *tmuxSubstrate) SendQuitToLastPane(ctx context.Context) error {
	s.lastHandleMu.Lock()
	handle := s.lastHandle
	paneID := s.lastPaneID
	s.lastHandleMu.Unlock()

	if handle == "" {
		return fmt.Errorf("daemon: tmuxSubstrate.SendQuitToLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}

	var paneTarget string
	if paneID != "" {
		paneTarget = paneID
	} else {
		paneTarget = string(handle) + ".0"
	}
	return s.adapter.SendKeysQuit(ctx, paneTarget)
}

// WriteLastPane delivers payload to the first pane of the most recently spawned
// window using the PL-021d load-buffer + paste-buffer sequence.
//
// The pane target is the stable pane ID captured at SpawnWindow time (e.g.
// "%1964"). Pane IDs are slash-free and remain valid regardless of the window
// name — critical when the window name is a filesystem path (hk-yngq2).
//
// Falls back to "handle.0" only when the pane ID lookup failed at spawn time
// (legacy behaviour for test doubles that do not implement WindowPaneID).
//
// bufferName MUST match the format "harmonik-<session-id>-<purpose>" required
// by PL-021d.
//
// Returns [tmux.ErrStructural] when no window has been spawned yet.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — daemon→pane write mechanism.
// Bead ref: hk-zrj83, hk-yngq2.
func (s *tmuxSubstrate) WriteLastPane(ctx context.Context, bufferName string, payload []byte) error {
	s.lastHandleMu.Lock()
	handle := s.lastHandle
	paneID := s.lastPaneID
	s.lastHandleMu.Unlock()

	if handle == "" {
		return fmt.Errorf("daemon: tmuxSubstrate.WriteLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}

	// Prefer the stable pane ID ("%NNNN") captured at spawn time. This is
	// slash-free and works even when the window name is a filesystem path.
	// Fall back to "handle.0" only when paneID is empty (e.g. in test doubles
	// that return "" from WindowPaneID).
	var paneTarget string
	if paneID != "" {
		paneTarget = paneID
	} else {
		paneTarget = string(handle) + ".0"
	}
	return s.adapter.WriteToPane(ctx, bufferName, paneTarget, payload)
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
// Per-session pane fields (hk-wx8z8): handle and paneID are captured at
// SpawnWindow time and are immutable for the lifetime of the session. They
// MUST NOT be re-derived from any daemon-shared state. This is the fix for
// the parallel-dispatch pane collision bug: the substrate's lastHandle /
// lastPaneID fields are shared across all SpawnWindow calls, so concurrent
// sessions overwrote each other's pane addresses. The per-session WritePane /
// SendEnter / SendQuit methods (below) read these fields only, never the
// substrate-shared ones.
//
// All methods are safe for concurrent use.
type tmuxSubstrateSession struct {
	adapter tmux.Adapter
	handle  tmux.WindowHandle
	// paneID is the stable tmux pane identifier (e.g. "%1964") captured atomically
	// in SpawnWindow. Slash-free; usable directly as a tmux pane target. Empty
	// when WindowPaneID lookup also returned "" (legacy fallback path).
	// Per-session — never overwritten after SpawnWindow returns (hk-wx8z8).
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
	s.waitOnce.Do(func() {
		s.waitDone = make(chan struct{})
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
// alive in the OS process table. It uses kill(pid, 0): ESRCH means gone,
// any other result means still running.
func processDead(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err != nil // ESRCH when gone; EPERM means alive but unowned
}

// runWait polls until the hosted process/window exits, then populates outcome.
func (s *tmuxSubstrateSession) runWait(ctx context.Context) {
	defer close(s.waitDone)

	startedAt := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled: record a canceled outcome and return.
			s.outcome = handler.Outcome{
				ExitCode: -1,
				Duration: time.Since(startedAt),
			}
			s.outcomeReady.Store(true)
			return
		case <-ticker.C:
			if s.pid > 0 {
				// Fast path: check OS process table directly. This avoids the
				// tmux display-message fallback that returns the active-pane PID
				// when the window name is no longer resolvable (hk-smuku).
				if processDead(s.pid) {
					s.outcome = handler.Outcome{
						ExitCode: 0,
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
						ExitCode: -1, // unknown: pane gone, process state uncertain
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
						ExitCode: 0,
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

// Outcome returns exit metadata once Wait has returned.
func (s *tmuxSubstrateSession) Outcome() handler.Outcome {
	if !s.outcomeReady.Load() {
		return handler.Outcome{}
	}
	return s.outcome
}

// PID returns the pane PID retrieved at spawn time. Returns 0 if unknown.
func (s *tmuxSubstrateSession) PID() int {
	return s.pid
}

// Stdout returns nil: tmux-hosted sessions do not expose a stdout pipe to the
// daemon. The bridge wire is the daemon Unix socket (hook-relay). Handler.Launch
// detects nil and skips SpawnWatcher accordingly.
//
// Spec ref: handler-contract.md HC-054; design §4 "Substrate seam".
func (s *tmuxSubstrateSession) Stdout() io.Reader {
	return nil
}
