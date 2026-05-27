package daemon_test

// workloop_tmux_window_cleanup_hkj6npz_test.go — tmux window cleanup on workloop
// exit (hk-j6npz).
//
// Bug: when harmonik run completes a wave (all items terminal), the daemon exits
// but any tmux windows it created for claude sessions remain alive. These orphan
// windows accumulate across runs.
//
// Fix: tmuxSubstrate.SpawnWindow appends each spawned WindowHandle to a
// mutex-protected slice. On workloop exit, exitClean() probes deps.substrate for
// the windowCleaner interface and calls KillAllWindows(), which iterates the
// slice and issues tmux kill-window for each handle. Errors are silently swallowed
// (KillWindow is idempotent on already-killed windows).
//
// Tests:
//   1. KillAllWindows kills every handle accumulated via SpawnWindow.
//   2. After runWorkLoop exits, the injected substrate's KillAllWindows was called
//      (windows accumulated before the loop were killed).
//   3. Nil substrate (exec.CommandContext path): exitClean exits cleanly without
//      panicking.
//
// Helper prefix: tmuxCleanupFixture (per implementer-protocol.md §Helper-prefix).
// Bead ref: hk-j6npz.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// tmuxCleanupFixtureAdapter — recording tmux.Adapter for window cleanup test
// ─────────────────────────────────────────────────────────────────────────────

// tmuxCleanupFixtureAdapter is a recording tmux.Adapter stub that:
//   - Returns a predictable handle from NewWindowIn.
//   - Records every KillWindow call.
//   - Returns ErrNoSession from WindowPanePID immediately (panePIDOKCount=0)
//     so tmuxSubstrateSession.Wait unblocks quickly.
//
// All methods are safe for concurrent use.
type tmuxCleanupFixtureAdapter struct {
	mu sync.Mutex

	windowCounter  int
	killedHandles  []tmux.WindowHandle
	panePIDCalls   int
	panePIDOKCount int // number of WindowPanePID calls that return (0, nil)
}

func (a *tmuxCleanupFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *tmuxCleanupFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *tmuxCleanupFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *tmuxCleanupFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.windowCounter++
	handle := tmux.WindowHandle(params.Session + ":" + params.WindowName)
	return tmux.Outcome{Handle: handle}
}

func (a *tmuxCleanupFixtureAdapter) KillWindow(_ context.Context, h tmux.WindowHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killedHandles = append(a.killedHandles, h)
	return nil
}

func (a *tmuxCleanupFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.panePIDCalls++
	if a.panePIDCalls > a.panePIDOKCount {
		return 0, tmux.ErrNoSession
	}
	return 0, nil
}

func (a *tmuxCleanupFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}

func (a *tmuxCleanupFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *tmuxCleanupFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *tmuxCleanupFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (a *tmuxCleanupFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *tmuxCleanupFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *tmuxCleanupFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *tmuxCleanupFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

func (a *tmuxCleanupFixtureAdapter) killedCopy() []tmux.WindowHandle {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]tmux.WindowHandle, len(a.killedHandles))
	copy(out, a.killedHandles)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// windowCleaner interface (local redeclaration for test access)
// ─────────────────────────────────────────────────────────────────────────────

// tmuxCleanupWindowCleaner mirrors the package-internal windowCleaner interface
// so tests can assert that NewTmuxSubstrate result implements it without importing
// unexported identifiers.
type tmuxCleanupWindowCleaner interface {
	KillAllWindows(ctx context.Context) error
}

// ─────────────────────────────────────────────────────────────────────────────
// TestTmuxSubstrate_KillAllWindows_KillsSpawnedHandles
// ─────────────────────────────────────────────────────────────────────────────

// TestTmuxSubstrate_KillAllWindows_KillsSpawnedHandles verifies that
// KillAllWindows kills every window handle accumulated by SpawnWindow.
//
// Bead ref: hk-j6npz.
func TestTmuxSubstrate_KillAllWindows_KillsSpawnedHandles(t *testing.T) {
	t.Parallel()

	fakeAdapter := &tmuxCleanupFixtureAdapter{}
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "test-session")

	// Spawn 3 windows; each appends a handle to the substrate's tracked list.
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := substrate.SpawnWindow(ctx, handler.SubstrateSpawn{
			WindowName: "hk-test-window",
			Argv:       []string{"/bin/sh", "-c", "exit 0"},
		}); err != nil {
			t.Fatalf("SpawnWindow[%d]: unexpected error: %v", i, err)
		}
	}

	// substrate must implement windowCleaner.
	wc, ok := substrate.(tmuxCleanupWindowCleaner)
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement windowCleaner")
	}
	if err := wc.KillAllWindows(ctx); err != nil {
		t.Fatalf("KillAllWindows: unexpected error: %v", err)
	}

	// All 3 handles must have been passed to KillWindow.
	killed := fakeAdapter.killedCopy()
	if len(killed) != 3 {
		t.Errorf("expected 3 KillWindow calls, got %d: %v", len(killed), killed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_KillsWindowsOnExit
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_KillsWindowsOnExit verifies that when runWorkLoop exits (context
// cancellation), the window-cleanup path fires via exitClean → KillAllWindows:
// any windows tracked by the injected tmuxSubstrate are killed.
//
// The test pre-spawns a window on the substrate before starting the loop, cancels
// the workloop context immediately (no beads dispatched), and asserts that
// KillWindow was called for the pre-spawned handle.
//
// Bead ref: hk-j6npz.
func TestWorkLoop_KillsWindowsOnExit(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	fakeAdapter := &tmuxCleanupFixtureAdapter{}
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "test-session")

	// Pre-spawn one window so the substrate has a handle to kill.
	if _, err := substrate.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		WindowName: "hk-pre-spawn",
		Argv:       []string{"/bin/sh", "-c", "exit 0"},
	}); err != nil {
		t.Fatalf("pre-spawn SpawnWindow: %v", err)
	}

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		Substrate:        substrate,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// Cancel immediately — simulates daemon exit with no beads dispatched.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workloop did not exit within 5s after context cancel")
	}

	// The pre-spawned window must have been killed by exitClean → KillAllWindows.
	killed := fakeAdapter.killedCopy()
	if len(killed) == 0 {
		t.Error("expected ≥1 KillWindow call after workloop exit, got 0")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_NilSubstrate_ExitCleanNoPanic
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_NilSubstrate_ExitCleanNoPanic verifies that exitClean does not
// panic when deps.substrate is nil (the exec.CommandContext / no-tmux path).
//
// Bead ref: hk-j6npz.
func TestWorkLoop_NilSubstrate_ExitCleanNoPanic(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	ledger := &stubBeadLedger{}
	bus := &stubEventCollector{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		Substrate:        nil, // exec.CommandContext path — no windowCleaner
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-done:
		// success — no panic
	case <-time.After(5 * time.Second):
		t.Fatal("workloop did not exit within 5s")
	}
}
