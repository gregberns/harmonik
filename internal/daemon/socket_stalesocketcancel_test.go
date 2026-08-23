package daemon

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
