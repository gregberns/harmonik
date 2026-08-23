package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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

type noSessionNoEnsureAdapter struct {
	noSessionFixtureBase
}

func (a *noSessionNoEnsureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Err: tmux.ErrNoSession}
}

var _ tmux.Adapter = (*noSessionNoEnsureAdapter)(nil)

func noSessionFixtureSpawn(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-nosession-test",
	})
}

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

	adapter := &noSessionRecoveringAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-test-session",
		daemon.WithSpawnCap(1),
		daemon.WithSpawnAcquireTimeout(500*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second))

	ctx := context.Background()

	sess, err := noSessionFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("first spawn (recovery path) failed unexpectedly: %v", err)
	}
	_ = sess.Kill(ctx)

	adapter2 := &noSessionRecoveringAdapter{}
	sub2 := daemon.NewTmuxSubstrate(adapter2, "hk-test-session",
		daemon.WithSpawnCap(1),
		daemon.WithSpawnAcquireTimeout(500*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second))
	adapter2.attempts = 1 // skip the ErrNoSession branch

	sess2, err := noSessionFixtureSpawn(ctx, sub2)
	if err != nil {
		t.Fatalf("second spawn should succeed, got: %v", err)
	}
	_ = sess2.Kill(ctx)
}
