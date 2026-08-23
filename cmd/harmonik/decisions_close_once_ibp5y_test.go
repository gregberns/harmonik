package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ibpDecisionID = "01965b00-0000-7000-8000-00000000d0d1"

func ibpDaemonHoldingStream(t *testing.T) *fakeDaemon {
	t.Helper()
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	return startFakeDaemon(t, func(_ net.Conn, _ []byte) { <-held })
}

func ibpWriteResolved(t *testing.T, dir, chosenOption string) {
	t.Helper()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o750); err != nil {
		t.Fatalf("ibpWriteResolved: mkdir: %v", err)
	}
	line := dx9Resolved(t, dx9R2, ibpDecisionID, chosenOption) + "\n"
	path := filepath.Join(eventsDir, "events.jsonl")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("ibpWriteResolved: write: %v", err)
	}
}

// TestDecisionsWait_SuccessfulWaitPrintsNoConnectionError is the user-visible
// half. The decision is answered, the wait succeeds and prints the chosen
// option — and it must say NOTHING on stderr. A connection error here is a false
// alarm on the surface a blocked agent is watching.
func TestDecisionsWait_SuccessfulWaitPrintsNoConnectionError(t *testing.T) {
	d := ibpDaemonHoldingStream(t)
	ibpWriteResolved(t, d.Dir, "ship")

	var code int
	stdout, stderr := captureStd(t, func() {
		code = decisionsBlockedWait(d.Dir, d.SockPath, ibpDecisionID)
	})

	if code != 0 {
		t.Errorf("decisions wait returned exit %d on an answered decision, want 0\nstdout: %q\nstderr: %q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "ship" {
		t.Errorf("decisions wait printed stdout %q, want the chosen option %q", stdout, "ship")
	}
	if stderr != "" {
		t.Errorf("a SUCCESSFUL decisions wait printed to stderr, which reads as a failed wait to the agent watching it: %q", stderr)
	}
}

// TestDecisionsArmSubscribe_CloseIsSynchronousAndIdempotent pins the mechanism
// under the test above.
//
// SYNCHRONOUS is the load-bearing half: when the returned func comes back, the
// close has ALREADY HAPPENED and the goroutine that did it has exited. That is
// what stops a write to os.Stderr from landing after the caller returned. An
// arrangement that merely signals the goroutine and returns would satisfy "one
// owner" and still race.
func TestDecisionsArmSubscribe_CloseIsSynchronousAndIdempotent(t *testing.T) {
	d := ibpDaemonHoldingStream(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, closeConn, rc := decisionsArmSubscribe(ctx, d.SockPath)
	if rc != 0 {
		t.Fatalf("decisionsArmSubscribe returned rc=%d, want 0", rc)
	}

	stdout, stderr := captureStd(t, func() {
		closeConn()

		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("SetReadDeadline after closeConn returned %v, want a closed connection", err)
		}
		buf := make([]byte, 1)
		if _, err := conn.Read(buf); !errors.Is(err, net.ErrClosed) {
			t.Errorf("read after closeConn returned %v, want net.ErrClosed — the close was not complete when closeConn returned", err)
		}

		closeConn()
	})

	if stdout != "" || stderr != "" {
		t.Errorf("closing an armed subscribe printed output; a close on the normal path is not a fault worth reporting\nstdout: %q\nstderr: %q", stdout, stderr)
	}
}

// TestDecisionsArmSubscribe_SignalClosesAndCloseConnStillJoins covers the path
// the goroutine exists for: a SIGINT cancels the ctx, which closes the
// connection so a blocked scan unblocks. The caller's deferred close must still
// return cleanly afterwards rather than blocking on a goroutine that has already
// finished, or closing a second time.
func TestDecisionsArmSubscribe_SignalClosesAndCloseConnStillJoins(t *testing.T) {
	d := ibpDaemonHoldingStream(t)

	ctx, cancel := context.WithCancel(context.Background())
	conn, closeConn, rc := decisionsArmSubscribe(ctx, d.SockPath)
	if rc != 0 {
		t.Fatalf("decisionsArmSubscribe returned rc=%d, want 0", rc)
	}

	stdout, stderr := captureStd(t, func() {
		cancel() // the signal path

		done := make(chan error, 1)
		go func() {
			buf := make([]byte, 1)
			_, err := conn.Read(buf)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, net.ErrClosed) {
				t.Errorf("read after ctx cancel returned %v, want net.ErrClosed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("a cancelled ctx did not close the connection, so a blocked scan would never unblock")
		}

		closeConn() // the caller's defer, after the signal already closed
	})

	if stdout != "" || stderr != "" {
		t.Errorf("the signal close path printed output\nstdout: %q\nstderr: %q", stdout, stderr)
	}
}
