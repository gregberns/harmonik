package main

// decisions_close_once_ibp5y_test.go — `harmonik decisions wait` used to register
// TWO independent closers for one connection, and neither of them was joined.
//
// The ctx comes from signal.NotifyContext, whose stop() cancels on EVERY return
// and not only on SIGINT, so both closers ran on every normal exit. The loser
// got "use of closed network connection" and PRINTED it. Two defects followed:
//
//   - A SUCCESSFUL wait could print a connection error. This is the surface a
//     blocked agent watches for its answer, so a spurious connection error there
//     reads as "the wait failed" at the exact moment the decision was answered.
//   - The unjoined goroutine wrote to os.Stderr after the function that started
//     it had returned. Under -race, with a test harness swapping os.Stderr, that
//     is a hard failure — which is how the merge decision surfaced it.
//
// Bead ref: hk-ibp5y.
//
// The first test below pins the user-visible half and holds for any close
// arrangement. The second pins the mechanism: ONE owner closes, and the close is
// SYNCHRONOUS, so nothing the wait starts can outlive it.

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

// ibpDecisionID is the decision under test — a UUIDv7-shaped id, matching the
// canonical ids used across the other decisions tests.
const ibpDecisionID = "01965b00-0000-7000-8000-00000000d0d1"

// ibpDaemonHoldingStream starts a fake daemon that accepts the subscribe request
// and then HOLDS the connection open for the rest of the test, the way a live
// daemon holds a blocked agent's stream. A handler that returned immediately
// would close the connection from the server side and hide which client-side
// closer ran.
func ibpDaemonHoldingStream(t *testing.T) *fakeDaemon {
	t.Helper()
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	return startFakeDaemon(t, func(_ net.Conn, _ []byte) { <-held })
}

// ibpWriteResolved writes a durable events.jsonl under dir containing one
// decision_resolved for ibpDecisionID, so the wait's step-2 re-project finds a
// terminal already logged and returns without blocking.
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
	// The assertion this file exists for. Kept as an emptiness check rather than
	// a match on the old wording: any stderr output on a successful wait is the
	// defect, whatever it happens to say.
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

		// Already closed, with no waiting and no retry: a Read that reports the
		// connection still open would mean the close was merely scheduled.
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("SetReadDeadline after closeConn returned %v, want a closed connection", err)
		}
		buf := make([]byte, 1)
		if _, err := conn.Read(buf); !errors.Is(err, net.ErrClosed) {
			t.Errorf("read after closeConn returned %v, want net.ErrClosed — the close was not complete when closeConn returned", err)
		}

		// A caller that closes early and also defers must not panic or double-close.
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

		// A blocked reader unblocks because the goroutine closed the connection.
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
