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
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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
}

// Compile-time assertion: tmuxSubstrate implements handler.Substrate.
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

	sess := &tmuxSubstrateSession{
		adapter: s.adapter,
		handle:  outcome.Handle,
		pid:     pid,
	}
	return sess, nil
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
	pid     int

	// killOnce ensures Kill is idempotent.
	killOnce sync.Once

	// outcomeReady is set once Wait has completed.
	outcomeReady atomic.Bool
	outcome      handler.Outcome

	// waitDone is closed when the Wait goroutine finishes.
	waitDone chan struct{}
	waitOnce sync.Once
}

// Kill issues `tmux kill-window` for the handle. Idempotent: subsequent calls
// return nil.
func (s *tmuxSubstrateSession) Kill(ctx context.Context) error {
	var killErr error
	s.killOnce.Do(func() {
		killErr = s.adapter.KillWindow(ctx, s.handle)
	})
	return killErr
}

// Wait blocks until the tmux window hosting the subprocess is gone. It polls
// WindowPanePID at 500ms intervals; when the PID disappears or the window is
// gone (ErrNoSession / non-zero failure), Wait returns.
//
// If ctx is cancelled before the window exits, Wait returns ctx.Err().
//
// This is a best-effort MVH implementation. A production implementation
// should use a PID-existence check (syscall.Kill(pid, 0)) or a side-channel
// completion signal from the hook-bridge socket.
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

// runWait polls until the tmux window/process exits, then populates outcome.
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
			_, err := s.adapter.WindowPanePID(ctx, s.handle)
			if err != nil {
				// Window or session gone — process has exited.
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
