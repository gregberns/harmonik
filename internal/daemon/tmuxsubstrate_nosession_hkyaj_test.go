package daemon_test

// tmuxsubstrate_nosession_hkyaj_test.go — regression tests for the lazy
// ErrNoSession recovery in SpawnWindow (hk-yaj).
//
// # The bug
//
// The daemon resolved its spawn-target tmux session at boot and froze the name
// into tmuxSubstrate.sessionName. SpawnWindow called `tmux new-window -t
// <sessionName>` with no recovery path: if the session was externally killed,
// new-window returned ErrNoSession and SpawnWindow hard-failed wrapping
// ErrStructural. Because all implementer/reviewer windows share a single spawn
// target, one killed session broke fleet-wide dispatch until the daemon was
// manually restarted (~70 min outage 2026-06-12).
//
// # The fix
//
// SpawnWindow now detects ErrNoSession from the first new-window attempt, calls
// EnsureSession on the adapter (when it implements the sessionEnsurer interface),
// and retries new-window once. Only after the retry fails does it hard-fail. If
// the adapter does not implement sessionEnsurer the old hard-fail path is taken.
//
// # What is tested
//
//   - NoSession_RecoverySucceeds: adapter's first NewWindowIn returns ErrNoSession;
//     EnsureSession creates the session; the retry NewWindowIn succeeds.
//     SpawnWindow must return a live session (not an error).
//
//   - NoSession_RecoveryRetryFails: adapter returns ErrNoSession on both the
//     initial and retry NewWindowIn calls (session still gone after EnsureSession).
//     SpawnWindow must return ErrStructural.
//
//   - NoSession_NoEnsureSupport_HardFails: adapter returns ErrNoSession but does
//     NOT implement sessionEnsurer. SpawnWindow must hard-fail immediately.
//
//   - NoSession_SlotReleasedOnHardFail: after a hard-fail the spawn slot must be
//     released so subsequent spawns are not wedged.
//
// # Bead
//
//   - hk-yaj (SpawnWindow ErrNoSession self-heal).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// noSessionFixtureBase implements the full tmux.Adapter interface. Embedded by
// the two fixture adapters below so each only needs to override the methods
// relevant to the scenario.
type noSessionFixtureBase struct {
	windowCount int
}

func (a *noSessionFixtureBase) ProbeTmux(_ context.Context) error                { return nil }
func (a *noSessionFixtureBase) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (a *noSessionFixtureBase) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *noSessionFixtureBase) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *noSessionFixtureBase) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *noSessionFixtureBase) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *noSessionFixtureBase) KillSession(_ context.Context, _ string) error { return nil }
func (a *noSessionFixtureBase) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *noSessionFixtureBase) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *noSessionFixtureBase) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *noSessionFixtureBase) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *noSessionFixtureBase) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *noSessionFixtureBase) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// ─── noSessionRecoveringAdapter ─────────────────────────────────────────────
//
// First NewWindowIn returns ErrNoSession; EnsureSession succeeds; retry succeeds.

type noSessionRecoveringAdapter struct {
	noSessionFixtureBase
	attempts    int
	ensureCalls int
}

func (a *noSessionRecoveringAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.attempts++
	if a.attempts == 1 {
		return tmux.Outcome{Err: tmux.ErrNoSession}
	}
	a.windowCount++
	return tmux.Outcome{Handle: tmux.WindowHandle("sess:win-recovered")}
}

func (a *noSessionRecoveringAdapter) EnsureSession(_ context.Context, _, _ string) error {
	a.ensureCalls++
	return nil
}

var _ tmux.Adapter = (*noSessionRecoveringAdapter)(nil)

// ─── noSessionPersistentAdapter ─────────────────────────────────────────────
//
// NewWindowIn always returns ErrNoSession; EnsureSession succeeds but recovery
// still fails (session gone again immediately).

type noSessionPersistentAdapter struct {
	noSessionFixtureBase
	ensureCalls int
}

func (a *noSessionPersistentAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Err: tmux.ErrNoSession}
}

func (a *noSessionPersistentAdapter) EnsureSession(_ context.Context, _, _ string) error {
	a.ensureCalls++
	return nil
}

var _ tmux.Adapter = (*noSessionPersistentAdapter)(nil)

// ─── noSessionNoEnsureAdapter ────────────────────────────────────────────────
//
// NewWindowIn returns ErrNoSession; does NOT implement sessionEnsurer.

type noSessionNoEnsureAdapter struct {
	noSessionFixtureBase
}

func (a *noSessionNoEnsureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Err: tmux.ErrNoSession}
}

var _ tmux.Adapter = (*noSessionNoEnsureAdapter)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

func noSessionFixtureSpawn(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-nosession-test",
	})
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestNoSession_RecoverySucceeds verifies that SpawnWindow recovers when the
// first new-window returns ErrNoSession: EnsureSession is called, the retry
// new-window succeeds, and SpawnWindow returns a live session with no error.
func TestNoSession_RecoverySucceeds(t *testing.T) {
	t.Parallel()

	adapter := &noSessionRecoveringAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-test-session",
		daemon.WithNewWindowTimeout(2*time.Second))

	ctx := context.Background()
	sess, err := noSessionFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("SpawnWindow should recover on ErrNoSession, got error: %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnWindow should return a non-nil session on recovery")
	}
	if adapter.ensureCalls != 1 {
		t.Errorf("EnsureSession called %d times, want 1", adapter.ensureCalls)
	}
	if adapter.attempts != 2 {
		t.Errorf("NewWindowIn called %d times, want 2 (initial + retry)", adapter.attempts)
	}
	_ = sess.Kill(ctx)
}

// TestNoSession_RecoveryRetryFails verifies that when the retry new-window also
// returns ErrNoSession (session gone again after EnsureSession), SpawnWindow
// hard-fails with ErrStructural.
func TestNoSession_RecoveryRetryFails(t *testing.T) {
	t.Parallel()

	adapter := &noSessionPersistentAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-test-session",
		daemon.WithNewWindowTimeout(2*time.Second))

	ctx := context.Background()
	_, err := noSessionFixtureSpawn(ctx, sub)
	if err == nil {
		t.Fatal("SpawnWindow should fail when retry also returns ErrNoSession")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error should wrap ErrStructural, got: %v", err)
	}
	if adapter.ensureCalls != 1 {
		t.Errorf("EnsureSession called %d times, want 1", adapter.ensureCalls)
	}
}

// TestNoSession_NoEnsureSupport_HardFails verifies that when the adapter does
// not implement sessionEnsurer, SpawnWindow hard-fails on ErrNoSession without
// any recovery attempt.
func TestNoSession_NoEnsureSupport_HardFails(t *testing.T) {
	t.Parallel()

	adapter := &noSessionNoEnsureAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-test-session",
		daemon.WithNewWindowTimeout(2*time.Second))

	ctx := context.Background()
	_, err := noSessionFixtureSpawn(ctx, sub)
	if err == nil {
		t.Fatal("SpawnWindow should hard-fail on ErrNoSession when adapter lacks EnsureSession")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error should wrap ErrStructural, got: %v", err)
	}
}

// TestNoSession_SlotReleasedOnHardFail verifies that a spawn-cap slot is
// released after a hard-fail so subsequent spawns are not indefinitely blocked.
func TestNoSession_SlotReleasedOnHardFail(t *testing.T) {
	t.Parallel()

	// A recovering adapter: first call fails (ErrNoSession), second succeeds.
	adapter := &noSessionRecoveringAdapter{}
	// Cap of 1; if the failed spawn leaks the slot, the second spawn wedges.
	sub := daemon.NewTmuxSubstrate(adapter, "hk-test-session",
		daemon.WithSpawnCap(1),
		daemon.WithSpawnAcquireTimeout(500*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second))

	ctx := context.Background()

	// First spawn: ErrNoSession → EnsureSession → retry succeeds (recovery).
	sess, err := noSessionFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("first spawn (recovery path) failed unexpectedly: %v", err)
	}
	// Release the slot.
	_ = sess.Kill(ctx)

	// Second spawn: both attempts succeed immediately.
	adapter2 := &noSessionRecoveringAdapter{}
	// Reuse a fresh substrate to get clean attempt counters without coupling state.
	sub2 := daemon.NewTmuxSubstrate(adapter2, "hk-test-session",
		daemon.WithSpawnCap(1),
		daemon.WithSpawnAcquireTimeout(500*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second))
	// Reset adapter so the first attempt in sub2 succeeds.
	adapter2.attempts = 1 // skip the ErrNoSession branch

	sess2, err := noSessionFixtureSpawn(ctx, sub2)
	if err != nil {
		t.Fatalf("second spawn should succeed, got: %v", err)
	}
	_ = sess2.Kill(ctx)
}
