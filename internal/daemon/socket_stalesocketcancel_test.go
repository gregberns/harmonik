package daemon

// socket_stalesocketcancel_test.go — pins the one rule that keeps the PL-003
// socket-exclusivity guard from destroying the thing it guards.
//
// removeStaleSocket reads EVERY dial error as "this socket file is stale" and
// then deletes the file. So the liveness probe must fail only when the dial
// really fails. It runs on context.WithoutCancel for that reason: a caller whose
// context is already done must still get a true answer, not a fast "no".
//
// Without this test the invariant is invisible. A later change back to a plain
// cancellable context compiles, passes every other test, and deletes a live
// daemon's socket the first time a daemon boots while it is shutting down.

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveStaleSocket_CancelledContextStillSeesTheLiveDaemon starts a real
// listener, calls removeStaleSocket with an ALREADY-cancelled context, and
// asserts the probe still reports the live daemon and leaves the file alone.
func TestRemoveStaleSocket_CancelledContextStillSeesTheLiveDaemon(t *testing.T) {
	t.Parallel()

	// A short path: a Unix socket address has a low length limit and t.TempDir
	// names are long.
	dir, err := os.MkdirTemp("", "hkstale")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Errorf("cleanup: remove %q: %v", dir, rmErr)
		}
	})
	sockPath := filepath.Join(dir, "d.sock")

	// The listener stands in for a live daemon. The context bounds the listen
	// call only — the listener itself stays up until Close.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %q: %v", sockPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // the caller has given up before the probe runs

	probeErr := removeStaleSocket(ctx, sockPath)
	if !errors.Is(probeErr, errLiveDaemon) {
		t.Fatalf("removeStaleSocket: got %v, want errLiveDaemon — a live daemon owns this socket", probeErr)
	}

	if _, statErr := os.Stat(sockPath); statErr != nil {
		t.Fatalf("removeStaleSocket deleted a live daemon's socket: %v", statErr)
	}
}
