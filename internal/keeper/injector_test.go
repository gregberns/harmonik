package keeper

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// TestInjectorSleep_FullDurationElapsed verifies that the injector's settle
// sleep returns true when the full duration elapses without cancellation.
func TestInjectorSleep_FullDurationElapsed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := substrate.SystemClock{}.Sleep(ctx, 5*time.Millisecond)
	if !got {
		t.Error("Sleep: want true (full duration elapsed), got false")
	}
}

// TestInjectorSleep_CancelledBefore verifies that the settle sleep returns
// false when the context is already cancelled before the call.
func TestInjectorSleep_CancelledBefore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	got := substrate.SystemClock{}.Sleep(ctx, 10*time.Second) // would block for 10s if not properly handled
	if got {
		t.Error("Sleep: want false (context already cancelled), got true")
	}
}

// TestInjectorSleep_CancelledDuring verifies that the settle sleep returns
// false when the context is cancelled while the sleep is in progress.
func TestInjectorSleep_CancelledDuring(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := substrate.SystemClock{}.Sleep(ctx, 10*time.Second) // would block for 10s without cancel
	elapsed := time.Since(start)

	if got {
		t.Error("Sleep: want false (context cancelled during wait), got true")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Sleep: did not respect context cancellation (elapsed %v; want <2s)", elapsed)
	}
}

// TestInjectText_EmptyTargetReturnsError verifies that InjectText returns a
// non-nil error immediately when tmuxTarget is empty, without spawning any tmux
// process. This is the guard at the top of InjectText before any exec calls.
func TestInjectText_EmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	err := InjectText(context.Background(), "", "some text")
	if err == nil {
		t.Error("InjectText(\"\", ...): want non-nil error for empty target, got nil")
	}
}

// Settle-timing constants (hk-89g) are now owned by tmuxhost, the package the
// tmux inject mechanics were extracted to (KH-1); see
// panehost/tmuxhost/inject_test.go.
