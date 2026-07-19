package handler

// session_w4_test.go — Wave-4 regression: SendInput is ctx-bounded (a wedged
// child that never drains stdin cannot block the caller forever), and repeated
// Kill calls are safe (single shared reap-observer goroutine).

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

func sessionW4Spawn(t *testing.T, name string, args ...string) Session {
	t.Helper()
	ctx := t.Context()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())
	sess, err := NewSession(ctx, cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sess.Kill(killCtx)              //nolint:errcheck // test teardown; best-effort reap
		_ = sess.Wait(context.Background()) //nolint:errcheck // test teardown; best-effort reap
	})
	return sess
}

// TestSendInput_CtxBounded verifies that a stdin write to a child that never
// reads (pipe buffer full) returns promptly when ctx expires instead of
// blocking the caller forever.
func TestSendInput_CtxBounded(t *testing.T) {
	t.Parallel()

	// A child that sleeps without ever reading stdin.
	sess := sessionW4Spawn(t, "sleep", "30")

	// Larger than any OS pipe buffer (~64 KiB) so the write must block.
	big := strings.Repeat("x", 1<<20)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sess.SendInput(ctx, big)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("SendInput returned nil; want ctx-bounded error on wedged stdin")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want wrapping context.DeadlineExceeded", err)
	}
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("err = %v, want wrapping ErrCanceled", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("SendInput blocked %v; want prompt return at ctx deadline", elapsed)
	}
}

// TestKill_RepeatedCallsSafe verifies repeated Kill calls succeed and share
// the single reap-observer goroutine (no panic on double-close, no wedge).
func TestKill_RepeatedCallsSafe(t *testing.T) {
	t.Parallel()

	sess := sessionW4Spawn(t, "sleep", "30")

	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := sess.Kill(ctx); err != nil {
			cancel()
			t.Fatalf("Kill #%d: %v", i+1, err)
		}
		cancel()
	}
	if err := sess.Wait(context.Background()); err == nil {
		t.Log("Wait returned nil (child may exit 0 on SIGTERM-default platforms)")
	}
}
