package daemon_test

// tmuxsubstrate_hkgql2011_test.go — unit tests for tmuxSubstrate (hk-gql20.11).
//
// Helper prefix: tmuxSubstrateFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-gql20.11).
//
// Tests drive the public handler.Substrate interface returned by NewTmuxSubstrate
// using a fake tmux.Adapter; no real tmux binary is required.

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake tmux.Adapter
// ─────────────────────────────────────────────────────────────────────────────

// fakeTmuxAdapter is a deterministic test double for tmux.Adapter.
// All operations are safe for concurrent use (reads only after setup).
type fakeTmuxAdapter struct {
	// newWindowInOutcome is returned by NewWindowIn.
	newWindowInOutcome tmux.Outcome

	// newWindowInParams records the params from the most recent NewWindowIn call.
	newWindowInParams tmux.NewWindowIn

	// panePIDResult is returned by WindowPanePID when panePIDErr is nil.
	panePIDResult int

	// panePIDErr is returned by WindowPanePID when non-nil.
	panePIDErr error

	// killWindowErr is returned by KillWindow when non-nil.
	killWindowErr error

	// killWindowCalled is incremented each time KillWindow is called.
	killWindowCalled int
}

func (f *fakeTmuxAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (f *fakeTmuxAdapter) ListSessions(_ context.Context) ([]string, error)  { return nil, nil }
func (f *fakeTmuxAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeTmuxAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	f.newWindowInParams = params
	return f.newWindowInOutcome
}
func (f *fakeTmuxAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	f.killWindowCalled++
	return f.killWindowErr
}
func (f *fakeTmuxAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	if f.panePIDErr != nil {
		return 0, f.panePIDErr
	}
	return f.panePIDResult, nil
}
func (f *fakeTmuxAdapter) KillSession(_ context.Context, _ string) error { return nil }

// Compile-time assertion: fakeTmuxAdapter implements tmux.Adapter.
var _ tmux.Adapter = (*fakeTmuxAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// tmuxSubstrateFixtureNew constructs a handler.Substrate (backed by tmuxSubstrate)
// using a caller-supplied fake adapter.
func tmuxSubstrateFixtureNew(t *testing.T, adapter tmux.Adapter) handler.Substrate {
	t.Helper()
	return daemon.NewTmuxSubstrate(adapter, "test-session")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — NewTmuxSubstrate panics
// ─────────────────────────────────────────────────────────────────────────────

// TestNewTmuxSubstrate_PanicsOnNilAdapter verifies that NewTmuxSubstrate panics
// when adapter is nil (daemon-defect guard).
func TestNewTmuxSubstrate_PanicsOnNilAdapter(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewTmuxSubstrate with nil adapter should panic, but did not")
		}
	}()
	daemon.NewTmuxSubstrate(nil, "session")
}

// TestNewTmuxSubstrate_PanicsOnEmptySession verifies that NewTmuxSubstrate panics
// when sessionName is empty.
func TestNewTmuxSubstrate_PanicsOnEmptySession(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewTmuxSubstrate with empty sessionName should panic, but did not")
		}
	}()
	daemon.NewTmuxSubstrate(&fakeTmuxAdapter{}, "")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — SpawnWindow
// ─────────────────────────────────────────────────────────────────────────────

// TestTmuxSubstrate_SpawnWindow_Success verifies that SpawnWindow calls
// NewWindowIn on the adapter and returns a valid SubstrateSession.
func TestTmuxSubstrate_SpawnWindow_Success(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-mywindow")},
		panePIDResult:      1234,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-mywindow",
		Cwd:        t.TempDir(),
		Env:        []string{"FOO=bar"},
		Argv:       []string{"claude", "--session-id", "abc123"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnWindow: returned nil SubstrateSession")
	}

	// Verify NewWindowIn was called with the correct session and window name.
	if fake.newWindowInParams.Session != "test-session" {
		t.Errorf("NewWindowIn.Session: got %q, want %q", fake.newWindowInParams.Session, "test-session")
	}
	if fake.newWindowInParams.WindowName != "hk-mywindow" {
		t.Errorf("NewWindowIn.WindowName: got %q, want %q", fake.newWindowInParams.WindowName, "hk-mywindow")
	}

	// Verify PID.
	if pid := sess.PID(); pid != 1234 {
		t.Errorf("SubstrateSession.PID(): got %d, want 1234", pid)
	}

	// Verify Stdout() returns nil (tmux-hosted; bridge wire is Unix socket).
	var nilReader io.Reader
	_ = nilReader
	if stdout := sess.Stdout(); stdout != nil {
		t.Errorf("SubstrateSession.Stdout(): expected nil for tmux-hosted session, got non-nil %T", stdout)
	}
}

// TestTmuxSubstrate_SpawnWindow_AdapterError verifies that a tmux.Outcome with
// a non-nil Err propagates as a SpawnWindow error wrapping handler.ErrStructural.
func TestTmuxSubstrate_SpawnWindow_AdapterError(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Err: tmux.ErrWindowCollision},
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-collision",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err == nil {
		t.Fatal("SpawnWindow: expected error for window collision, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("SpawnWindow error: expected errors.Is(err, handler.ErrStructural) == true; got %v", err)
	}
}

// TestTmuxSubstrate_SpawnWindow_PIDFailureIsNonFatal verifies that a failed
// WindowPanePID lookup does not cause SpawnWindow to fail — session returns PID=0.
func TestTmuxSubstrate_SpawnWindow_PIDFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDErr:         errors.New("tmux: display-message failed"),
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: expected non-fatal PID failure, got error: %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnWindow: returned nil SubstrateSession despite non-fatal PID failure")
	}
	if pid := sess.PID(); pid != 0 {
		t.Errorf("SubstrateSession.PID(): got %d, want 0 after PID lookup failure", pid)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — SubstrateSession lifecycle
// ─────────────────────────────────────────────────────────────────────────────

// TestTmuxSubstrateSession_Kill_Delegates verifies that Kill calls KillWindow.
func TestTmuxSubstrateSession_Kill_Delegates(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      999,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("SubstrateSession.Kill: %v", err)
	}
}

// TestTmuxSubstrateSession_Kill_TerminatesProcessAndCleansWindow verifies that
// Kill (hk-kqdpf.7) signals the hosted process and then calls KillWindow to
// clean up the tmux window. The test uses a zero PID (pid=0 skips the signal
// step) so it does not need a real process; it verifies that KillWindow is
// always called exactly once.
func TestTmuxSubstrateSession_Kill_TerminatesProcessAndCleansWindow(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		// panePIDResult=0 keeps pid=0 in the session; kill path skips the signal
		// step (pid <= 0 guard) and goes straight to KillWindow.
		panePIDResult: 0,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("SubstrateSession.Kill: unexpected error: %v", err)
	}

	// KillWindow MUST have been called exactly once.
	if fake.killWindowCalled != 1 {
		t.Errorf("KillWindow call count: got %d, want 1", fake.killWindowCalled)
	}
}

// TestTmuxSubstrateSession_Kill_IdempotentWithWindow verifies that calling Kill
// twice does NOT call KillWindow a second time (killOnce guard).
func TestTmuxSubstrateSession_Kill_IdempotentWithWindow(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      0,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// First call.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("SubstrateSession.Kill (first): %v", err)
	}
	// Second call must be idempotent.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("SubstrateSession.Kill (second): %v", err)
	}

	// KillWindow MUST have been called exactly once despite two Kill calls.
	if fake.killWindowCalled != 1 {
		t.Errorf("KillWindow call count after two Kill calls: got %d, want 1", fake.killWindowCalled)
	}
}

// TestTmuxSubstrateSession_Kill_WithRealChildProcess verifies the signal path
// end-to-end by spawning a real OS process (sleep), then calling Kill and
// confirming the process is gone. This test exercises killProcessWithGrace
// with a live PID. It is not a tmux test — it bypasses SpawnWindow and
// constructs the session manually via a helper to inspect the exported Kill
// method's real signal behaviour.
//
// The real PID is injected via the panePIDResult field of the fake adapter so
// that SpawnWindow populates sess.pid, which Kill uses to signal the process.
func TestTmuxSubstrateSession_Kill_WithRealChildProcess(t *testing.T) {
	t.Parallel()

	// Spawn a long-lived subprocess (sleep 60) as a stand-in for a claude process.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep subprocess: %v", err)
	}
	childPID := cmd.Process.Pid
	t.Cleanup(func() {
		// Best-effort cleanup: kill the child if the test failed before Kill did.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		// Inject the real child PID so Kill signals it.
		panePIDResult: childPID,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"sleep", "60"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Kill should terminate the child process via SIGTERM.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("SubstrateSession.Kill: %v", err)
	}

	// Verify the process is gone: cmd.Wait should return quickly.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// Process exited — Kill worked correctly.
	case <-time.After(5 * time.Second):
		t.Error("child process still alive 5s after Kill — signal path did not work")
		_ = cmd.Process.Kill()
	}
}

// TestTmuxSubstrateSession_Wait_ExitsWhenWindowGone verifies that Wait returns
// once WindowPanePID signals the window is gone.
func TestTmuxSubstrateSession_Wait_ExitsWhenWindowGone(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		// panePIDErr set after spawn so the first poll sees the window gone.
		panePIDErr: errors.New("tmux: no such window"),
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Errorf("SubstrateSession.Wait: %v", err)
	}
}

// TestTmuxSubstrateSession_Stdout_AlwaysNil verifies Stdout() is always nil
// for tmux-hosted sessions.
func TestTmuxSubstrateSession_Stdout_AlwaysNil(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      1,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	if stdout := sess.Stdout(); stdout != nil {
		t.Errorf("SubstrateSession.Stdout(): expected nil, got %T", stdout)
	}
}

// TestTmuxSubstrateSession_Outcome_BeforeWait verifies that Outcome() returns
// a zero-value Outcome before Wait has returned.
func TestTmuxSubstrateSession_Outcome_BeforeWait(t *testing.T) {
	t.Parallel()

	// panePIDErr is NOT set so WindowPanePID succeeds — the poll loop keeps running,
	// keeping Wait blocked so we can observe Outcome before completion.
	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      1,
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Do NOT call Wait. Outcome before Wait MUST be zero.
	o := sess.Outcome()
	if o.ExitCode != 0 || o.Duration != 0 {
		t.Errorf("Outcome before Wait: expected zero-value, got %+v", o)
	}
}
