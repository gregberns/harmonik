package daemon_test

// pasteinject_hkaievp_test.go — scenario tests for the stale-pane misdirect fix (hk-aievp).
//
// Root cause: tmuxSubstrate.SpawnWindow called WindowPaneID(handle) where handle =
// "session:window-name" and the window name was a filesystem path with slashes.
// tmux misparses the slash-bearing target and returns the session's active pane
// (the old v49 pane %22) instead of the newly-spawned pane (%27). WriteLastPane
// then delivers the task kick-off to the wrong (stale) pane.
//
// Fix: OSAdapter.NewWindowIn now captures the pane ID atomically via
// `tmux new-window -P -F "#{pane_id}"`. SpawnWindow reads outcome.PaneID directly;
// the slash-bearing handle is never used as a pane target.
//
// Tests:
//
//  1. TestStalePaneFix_MultiRun_PasteGoesToFreshPane
//     Run 1 creates paneA → WriteLastPane MUST target paneA.
//     Run 2 creates paneB → WriteLastPane MUST target paneB, NOT paneA.
//
//  2. TestStalePaneFix_AtomicPaneID_UsedDirectly
//     When Outcome.PaneID is set (new -P -F path), SpawnWindow MUST NOT
//     call WindowPaneID (the fallback that uses the slash-bearing handle).
//
// Helper prefix: stalePaneFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-aievp).

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// stalePaneFixtureSequentialAdapter — fake tmux.Adapter that returns different
// pane IDs for successive NewWindowIn calls, simulating two sequential runs.
// ─────────────────────────────────────────────────────────────────────────────

// stalePaneFixtureSequentialAdapter is a fake [tmux.Adapter] whose NewWindowIn
// returns successive pane IDs from a pre-configured list. This simulates a
// production scenario where run 1 creates pane %22 and run 2 creates pane %27.
//
// windowPaneIDCallCount tracks how many times WindowPaneID is called, so tests
// can assert that the fallback path (which uses the slash-bearing handle) is
// NOT invoked when Outcome.PaneID is already set.
type stalePaneFixtureSequentialAdapter struct {
	mu sync.Mutex

	// paneSequence is the list of pane IDs returned by successive NewWindowIn
	// calls. The i-th call returns paneSequence[i % len(paneSequence)].
	paneSequence []string

	// callCount is the number of NewWindowIn calls made so far.
	callCount int

	// writeToPaneTargets records the paneTarget from each WriteToPane call.
	writeToPaneTargets []string

	// windowPaneIDCallCount records how many times WindowPaneID was called.
	// Expected to be 0 after SpawnWindow when Outcome.PaneID is non-empty.
	windowPaneIDCallCount int
}

// NewWindowIn returns successive Outcome values with PaneID pre-populated from
// paneSequence. The Handle is a slash-bearing "test-session:/path/to/worktree"
// string to reproduce the production scenario where the window name is a
// filesystem path.
func (f *stalePaneFixtureSequentialAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.callCount % len(f.paneSequence)
	paneID := f.paneSequence[idx]
	f.callCount++
	// Use a slash-bearing window name to reproduce the production scenario (hk-aievp).
	handle := tmux.WindowHandle("test-session:/harmonik/worktrees/run-" + paneID)
	return tmux.Outcome{Handle: handle, PaneID: paneID}
}

// WriteToPane records the paneTarget for assertion.
func (f *stalePaneFixtureSequentialAdapter) WriteToPane(_ context.Context, _, paneTarget string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeToPaneTargets = append(f.writeToPaneTargets, paneTarget)
	return nil
}

// WindowPaneID increments the call counter and returns "" so callers fall back
// to the legacy path. The counter lets tests assert that WindowPaneID was NOT
// called when Outcome.PaneID is already set.
func (f *stalePaneFixtureSequentialAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windowPaneIDCallCount++
	return "", nil
}

func (f *stalePaneFixtureSequentialAdapter) lastWriteTarget() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writeToPaneTargets) == 0 {
		return ""
	}
	return f.writeToPaneTargets[len(f.writeToPaneTargets)-1]
}

func (f *stalePaneFixtureSequentialAdapter) allWriteTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.writeToPaneTargets))
	copy(out, f.writeToPaneTargets)
	return out
}

func (f *stalePaneFixtureSequentialAdapter) paneIDCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.windowPaneIDCallCount
}

// Stub implementations for unused Adapter methods.
func (f *stalePaneFixtureSequentialAdapter) ProbeTmux(_ context.Context) error { return nil }
func (f *stalePaneFixtureSequentialAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (f *stalePaneFixtureSequentialAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *stalePaneFixtureSequentialAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (f *stalePaneFixtureSequentialAdapter) KillSession(_ context.Context, _ string) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}
func (f *stalePaneFixtureSequentialAdapter) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}

// Compile-time assertion: stalePaneFixtureSequentialAdapter implements tmux.Adapter.
var _ tmux.Adapter = (*stalePaneFixtureSequentialAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// stalePaneFixtureWriteLastPane — helper to invoke WriteLastPane via interface
// ─────────────────────────────────────────────────────────────────────────────

// stalePaneFixtureWriteLastPane casts substrate to pasteInjecter and calls
// WriteLastPane with a fixed buffer name and payload.
//
// substrate MUST be a perRunSubstrate (obtained via
// daemon.ExportedNewPerRunSubstrate) with SpawnWindow already called.
// Calling this on a bare *tmuxSubstrate is not supported: tmuxSubstrate no
// longer implements WriteLastPane (hk-jfh59).
func stalePaneFixtureWriteLastPane(t *testing.T, substrate handler.Substrate) {
	t.Helper()
	pi, ok := substrate.(interface {
		WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
	})
	if !ok {
		t.Fatal("stalePaneFixtureWriteLastPane: substrate does not implement WriteLastPane (must be a perRunSubstrate)")
	}
	const bufName = "harmonik-01hwxyz-abc123-task"
	if err := pi.WriteLastPane(t.Context(), bufName, []byte("test payload")); err != nil {
		t.Fatalf("stalePaneFixtureWriteLastPane: WriteLastPane: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestStalePaneFix_MultiRun_PasteGoesToFreshPane is the core regression test
// for hk-aievp (stale-pane misdirect).
//
// Scenario:
//
//	Run 1: SpawnWindow creates pane %22 (simulating v49 session pane).
//	       WriteLastPane MUST target %22.
//	Run 2: SpawnWindow creates pane %27 (the fresh v50 pane).
//	       WriteLastPane MUST target %27, NOT the cached %22.
//
// Before the fix: SpawnWindow called WindowPaneID(handle) where handle =
// "session:/path/with/slashes". tmux misparsed the slash-bearing handle and
// returned the session's active pane (%22 from the prior run). lastPaneID was
// set to %22 for BOTH runs, so run 2's paste-inject targeted the stale pane.
//
// After the fix: outcome.PaneID is captured atomically in NewWindowIn via
// `-P -F "#{pane_id}"`. SpawnWindow reads outcome.PaneID directly; each run
// correctly sets lastPaneID to its own fresh pane ID.
func TestStalePaneFix_MultiRun_PasteGoesToFreshPane(t *testing.T) {
	t.Parallel()

	fake := &stalePaneFixtureSequentialAdapter{
		paneSequence: []string{"%22", "%27"},
	}
	sharedSubstrate := daemon.NewTmuxSubstrate(fake, "test-session")

	// ── Run 1: pane %22 ──────────────────────────────────────────────────────
	// Each run gets its own perRunSubstrate so pane-target capture is isolated
	// (hk-012af, hk-jfh59). WriteLastPane routes through perRunSubstrate.
	prs1 := daemon.ExportedNewPerRunSubstrate(sharedSubstrate)
	spawn1 := handler.SubstrateSpawn{
		WindowName: "/harmonik/worktrees/run1",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}
	if _, err := prs1.SpawnWindow(t.Context(), spawn1); err != nil {
		t.Fatalf("run1 SpawnWindow: %v", err)
	}
	stalePaneFixtureWriteLastPane(t, prs1)

	run1Target := fake.lastWriteTarget()
	if run1Target != "%22" {
		t.Errorf("run1 WriteLastPane target = %q; want %%22", run1Target)
	}

	// ── Run 2: pane %27 ──────────────────────────────────────────────────────
	prs2 := daemon.ExportedNewPerRunSubstrate(sharedSubstrate)
	spawn2 := handler.SubstrateSpawn{
		WindowName: "/harmonik/worktrees/run2",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}
	if _, err := prs2.SpawnWindow(t.Context(), spawn2); err != nil {
		t.Fatalf("run2 SpawnWindow: %v", err)
	}
	stalePaneFixtureWriteLastPane(t, prs2)

	run2Target := fake.lastWriteTarget()
	if run2Target != "%27" {
		t.Errorf("run2 WriteLastPane target = %q; want %%27 (must NOT reuse stale %%22)", run2Target)
	}

	// Verify the full sequence: both writes were captured.
	targets := fake.allWriteTargets()
	if len(targets) != 2 {
		t.Errorf("WriteToPane call count = %d; want 2", len(targets))
	}
}

// TestStalePaneFix_AtomicPaneID_UsedDirectly verifies that when Outcome.PaneID
// is set (the fixed -P -F "#{pane_id}" path), SpawnWindow does NOT call
// WindowPaneID as a fallback.
//
// WindowPaneID uses the slash-bearing "session:window-name" handle and is the
// source of the stale-pane bug. Its call count MUST be 0 when Outcome.PaneID
// is already populated.
func TestStalePaneFix_AtomicPaneID_UsedDirectly(t *testing.T) {
	t.Parallel()

	fake := &stalePaneFixtureSequentialAdapter{
		paneSequence: []string{"%42"},
	}
	sharedSubstrate := daemon.NewTmuxSubstrate(fake, "test-session")

	// Wrap in perRunSubstrate (hk-012af, hk-jfh59): this is the production path
	// that isolates pane-target capture per goroutine.
	prs := daemon.ExportedNewPerRunSubstrate(sharedSubstrate)
	spawn := handler.SubstrateSpawn{
		WindowName: "/harmonik/worktrees/run1",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}
	if _, err := prs.SpawnWindow(t.Context(), spawn); err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// WindowPaneID MUST NOT have been called: outcome.PaneID was already set.
	if n := fake.paneIDCallCount(); n != 0 {
		t.Errorf("WindowPaneID call count = %d; want 0 (Outcome.PaneID should be used directly)", n)
	}

	// Verify WriteLastPane uses the atomically-captured pane ID.
	stalePaneFixtureWriteLastPane(t, prs)
	if got := fake.lastWriteTarget(); got != "%42" {
		t.Errorf("WriteLastPane target = %q; want %%42", got)
	}
}

// TestStalePaneFix_FallbackToWindowPaneID_WhenOutcomePaneIDEmpty verifies that
// SpawnWindow falls back to WindowPaneID when outcome.PaneID is empty (e.g.
// when using test doubles that do not set PaneID on Outcome).
//
// This preserves backward compatibility for existing test doubles and future
// adapter implementations that do not support -P -F.
func TestStalePaneFix_FallbackToWindowPaneID_WhenOutcomePaneIDEmpty(t *testing.T) {
	t.Parallel()

	// Use the existing fakeTmuxAdapter from tmuxsubstrate_hkgql2011_test.go
	// (same package), which leaves Outcome.PaneID empty by default.
	const wantPaneID = "%99"
	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      1,
		paneIDResult:       wantPaneID, // returned by WindowPaneID fallback
	}
	sharedSubstrate := daemon.NewTmuxSubstrate(fake, "test-session")

	// Wrap in perRunSubstrate (hk-jfh59): production path for paste-inject.
	prs := daemon.ExportedNewPerRunSubstrate(sharedSubstrate)
	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}
	if _, err := prs.SpawnWindow(t.Context(), spawn); err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Verify the fallback reached WriteLastPane with the right pane ID.
	pi, ok := prs.(interface {
		WriteLastPane(ctx context.Context, bufferName string, payload []byte) error
	})
	if !ok {
		t.Fatal("perRunSubstrate does not implement WriteLastPane")
	}
	if err := pi.WriteLastPane(t.Context(), "harmonik-01hwxyz-abc123-task", []byte("hello")); err != nil {
		t.Fatalf("WriteLastPane: %v", err)
	}
	if fake.writeToPaneTarget != wantPaneID {
		t.Errorf("WriteLastPane fallback target = %q; want %q", fake.writeToPaneTarget, wantPaneID)
	}
}
