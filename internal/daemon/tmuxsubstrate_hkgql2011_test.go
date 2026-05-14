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
	"strings"
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

	// paneIDResult is returned by WindowPaneID. When empty string, WriteLastPane
	// falls back to the legacy handle+".0" form. Set to a "%NNNN" value to
	// exercise the pane-ID fast path (hk-yngq2).
	paneIDResult string

	// writeToPaneTarget records the paneTarget passed to the most recent
	// WriteToPane call. Used by slash-path tests (hk-yngq2).
	writeToPaneTarget string

	// killWindowErr is returned by KillWindow when non-nil.
	killWindowErr error

	// killWindowCalled is incremented each time KillWindow is called.
	killWindowCalled int
}

func (f *fakeTmuxAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (f *fakeTmuxAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
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

// WindowPaneID returns paneIDResult when set, or "" to trigger the
// handle+".0" fallback in WriteLastPane (hk-yngq2).
func (f *fakeTmuxAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return f.paneIDResult, nil
}

func (f *fakeTmuxAdapter) KillSession(_ context.Context, _ string) error { return nil }

// LoadBuffer is a no-op stub to satisfy the [tmux.Adapter] interface.
func (f *fakeTmuxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }

// PasteBuffer is a no-op stub to satisfy the [tmux.Adapter] interface.
func (f *fakeTmuxAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }

// SendKeysLiteral is a no-op stub to satisfy the [tmux.Adapter] interface.
func (f *fakeTmuxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }

// SendKeysEnter is a no-op stub to satisfy the [tmux.Adapter] interface.
func (f *fakeTmuxAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }

// WriteToPane records the paneTarget and returns nil.
func (f *fakeTmuxAdapter) WriteToPane(_ context.Context, _, paneTarget string, _ []byte) error {
	f.writeToPaneTarget = paneTarget
	return nil
}

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

// TestTmuxSubstrateSession_Wait_ReturnAfterExternalKill verifies that Wait
// returns promptly (within 3 seconds) after the hosted process is killed
// externally, even when the fake adapter's WindowPanePID would return no error
// (simulating the tmux active-pane fallback bug described in hk-smuku).
//
// The test injects the real child PID via panePIDResult so that s.pid > 0 and
// the fast kill(pid,0) path is exercised. panePIDErr is intentionally left nil
// so that the WindowPanePID fallback would loop forever if the fast path were
// absent — proving the fix works.
func TestTmuxSubstrateSession_Wait_ReturnAfterExternalKill(t *testing.T) {
	t.Parallel()

	// Spawn a long-lived subprocess as a stand-in for a claude process.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep subprocess: %v", err)
	}
	childPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// panePIDErr is nil — WindowPanePID would return a live-looking PID forever,
	// simulating the tmux active-pane fallback. The fix must NOT consult it.
	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      childPID,
		panePIDErr:         nil,
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

	// Kill the child process externally (bypassing Kill() — simulating HC-056
	// KillWindow path that already destroyed the window but Wait is still running).
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child externally: %v", err)
	}
	_ = cmd.Wait() // reap zombie

	// Wait must return within 3 seconds; without the fix it would hang forever.
	waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Errorf("SubstrateSession.Wait: expected nil after process exit, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — WriteLastPane pane-ID addressing (hk-yngq2)
// ─────────────────────────────────────────────────────────────────────────────

// TestTmuxSubstrate_WriteLastPane_UsesPaneID verifies that WriteLastPane passes
// the stable pane ID (e.g. "%1964") captured at SpawnWindow time as the
// paneTarget to WriteToPane, rather than constructing "session:window-name.0".
//
// This is the hk-yngq2 regression: when the window name is a filesystem path
// containing slashes, the "session:path/to/dir.0" form cannot be parsed by tmux
// and paste-buffer exits 1. Using the pane ID directly avoids the issue.
func TestTmuxSubstrate_WriteLastPane_UsesPaneID(t *testing.T) {
	t.Parallel()

	const wantPaneID = "%1964"
	// Window name is a slash-bearing worktree path — the real production case.
	const slashWindowName = "/private/var/folders/s9/tmp.kjv2d1cswF/.harmonik/worktrees/019e2565-8fac-79f2"
	handle := tmux.WindowHandle("smoke-1778743872:" + slashWindowName)

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: handle},
		panePIDResult:      1234,
		paneIDResult:       wantPaneID, // Simulate tmux returning "%1964".
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: slashWindowName,
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Cast to pasteInjecter to access WriteLastPane.
	pi, ok := substrate.(interface {
		WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
	})
	if !ok {
		t.Fatal("substrate does not implement WriteLastPane; check daemon.pasteInjecter interface")
	}

	const bufferName = "harmonik-01hwxyz-abc123-task"
	if err := pi.WriteLastPane(t.Context(), bufferName, []byte("Please read .harmonik/agent-task.md")); err != nil {
		t.Fatalf("WriteLastPane: %v", err)
	}

	// Verify that WriteToPane was called with the pane ID, not with a slash-bearing string.
	if fake.writeToPaneTarget != wantPaneID {
		t.Errorf("WriteLastPane paneTarget = %q; want %q (pane ID, not slash-bearing handle+.0)",
			fake.writeToPaneTarget, wantPaneID)
	}
	// Sanity-check: the target must NOT contain slashes (the bug being fixed).
	if strings.Contains(fake.writeToPaneTarget, "/") {
		t.Errorf("WriteLastPane paneTarget %q contains slashes — tmux cannot parse it as a pane target",
			fake.writeToPaneTarget)
	}
}

// TestTmuxSubstrate_WriteLastPane_FallbackOnEmptyPaneID verifies that
// WriteLastPane falls back to "handle.0" when WindowPaneID returned "" at
// spawn time (e.g. test doubles that do not implement the new method).
func TestTmuxSubstrate_WriteLastPane_FallbackOnEmptyPaneID(t *testing.T) {
	t.Parallel()

	handle := tmux.WindowHandle("test-session:hk-simple-win")
	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: handle},
		panePIDResult:      1,
		paneIDResult:       "", // Simulate empty pane ID — triggers fallback.
	}
	substrate := tmuxSubstrateFixtureNew(t, fake)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-simple-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}

	_, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	pi, ok := substrate.(interface {
		WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
	})
	if !ok {
		t.Fatal("substrate does not implement WriteLastPane")
	}

	if err := pi.WriteLastPane(t.Context(), "harmonik-01hwxyz-abc123-task", []byte("hello")); err != nil {
		t.Fatalf("WriteLastPane: %v", err)
	}

	// With empty pane ID, fall back to handle+".0".
	wantFallback := string(handle) + ".0"
	if fake.writeToPaneTarget != wantFallback {
		t.Errorf("WriteLastPane fallback paneTarget = %q; want %q", fake.writeToPaneTarget, wantFallback)
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
