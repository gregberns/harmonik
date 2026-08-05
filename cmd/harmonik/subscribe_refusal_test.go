package main

// subscribe_refusal_test.go — the daemon can REFUSE a subscribe request, and a
// client that never reads the response envelope cannot tell that refusal from a
// normal empty stream.
//
// The refusal is a single JSON object written by writeSocketResponse, after
// which the daemon closes the connection:
//
//	{"ok":false,"error":"daemon: SubscribeHandler not registered"}
//
// A client that decodes stream lines as EVENTS sees an object with no "type",
// skips it as unrecognised, reads EOF, and reports a clean finish. The daemon
// is correct here; every defect below is downstream, in cmd/harmonik.
//
// Three clients already guard the envelope (`comms recv --follow`,
// `comms recv --wait`, `subscribe --follow`). The four tested here do not:
//
//   - `harmonik subscribe` (non-follow) io.Copy's the refusal to stdout as if it
//     were an event line and exits 0. Anything consuming that stream — including
//     the scratch batch's event capture — folds a refusal into "no events".
//   - `harmonik decisions wait` exits 0 with no output, so a blocked agent
//     silently unblocks. specs/hitl-decisions.md N5/N8 make waiting on an open
//     subscribe stream a MUST for a blocked agent, so this is a conformance
//     break and not only a bad message.
//   - `harmonik smoke` reports a timeout, blaming the daemon for never emitting
//     the signals it was never subscribed to.
//   - `harmonik run` (via daemon) exits 1 but names no cause.
//
// Bead ref: hk-1dwk2.
//
// These tests are written BEFORE the fix and are EXPECTED TO FAIL on the commit
// that introduces them. What turns them green is an envelope guard shared by
// every subscribe client.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// refusalReply is the exact envelope internal/daemon/socket.go handleSubscribe
// writes when no SubscribeHandler is registered (the subsystem is switched off).
func refusalReply() map[string]any {
	return map[string]any{"ok": false, "error": "daemon: SubscribeHandler not registered"}
}

// captureStd redirects os.Stdout and os.Stderr for the duration of fn and
// returns what was written to each. It mutates process globals, so no test
// using it may call t.Parallel().
func captureStd(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errCh <- string(b) }()

	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
	}()
	fn()
	_ = outW.Close()
	_ = errW.Close()
	return <-outCh, <-errCh
}

// TestSubscribeCommand_DaemonRefusalIsNotSuccess pins that `harmonik subscribe`
// reports a refused subscription instead of copying the refusal to stdout and
// exiting 0. The stdout assertion is the load-bearing half: a consumer piping
// this stream into jq would otherwise fold the refusal in as though it were an
// event, which is how the scratch batch's capture reports every item incomplete
// when the real cause is that it was never subscribed.
func TestSubscribeCommand_DaemonRefusalIsNotSuccess(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	var code int
	stdout, stderr := captureStd(t, func() {
		code = runSubscribeSubcommand([]string{"--socket", d.SockPath})
	})

	if code == 0 {
		t.Errorf("harmonik subscribe returned exit 0 on a refused subscription; want non-zero\nstdout: %q\nstderr: %q", stdout, stderr)
	}
	if strings.Contains(stdout, "SubscribeHandler not registered") {
		t.Errorf("harmonik subscribe copied the refusal envelope to stdout as if it were an event line:\n%s", stdout)
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("harmonik subscribe did not report the refusal on stderr; got: %q", stderr)
	}
}

// TestDecisionsWait_DaemonRefusalDoesNotSilentlyUnblock is the sharp case. A
// blocked agent calls `harmonik decisions wait <id>`; if the subscribe op is
// refused, the wait must NOT return as though the decision were answered.
// Returning 0 here unblocks an agent that no human ever answered.
func TestDecisionsWait_DaemonRefusalDoesNotSilentlyUnblock(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	var code int
	stdout, stderr := captureStd(t, func() {
		code = decisionsBlockedWait(d.Dir, d.SockPath, "0192f5a1-0000-7000-8000-000000000000")
	})

	if code == 0 {
		t.Errorf("decisions wait returned exit 0 on a refused subscription — a blocked agent silently unblocks\nstdout: %q\nstderr: %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("decisions wait did not report the refusal on stderr; got: %q", stderr)
	}
}

// TestSmokeWatchSignals_DaemonRefusalIsReported pins that the smoke command
// blames the refused subscription rather than reporting a timeout waiting for
// signals it was never subscribed to. Exit 2 is smoke's timeout code.
func TestSmokeWatchSignals_DaemonRefusalIsReported(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	_, code := smokeWatchSignals(ctx, d.SockPath, d.Dir, "main", "hk-smoke", &stdout, &stderr)

	if code == 2 {
		t.Errorf("smoke reported a timeout (exit 2) on a refused subscription; the cause is the refusal, not a slow daemon\nstderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "SubscribeHandler not registered") {
		t.Errorf("smoke did not report the refusal on stderr; got: %q", stderr.String())
	}
}

// The three clients below already guarded the envelope before this change
// (landed as hk-62r8w) but had NO test — the mutation that proved the four
// tests above are load-bearing turned nothing else red. They now share the same
// predicate as everything else, so a change to it must fail here too, otherwise
// consolidating the guard would have removed the only reason to keep it right.

// TestSubscribeFollow_DaemonRefusalStopsInsteadOfReconnecting pins that a
// refusal ends `subscribe --follow` rather than sending it round the reconnect
// loop. A refusal is permanent for this connection: the daemon is up and has
// declined, so retrying spins at the backoff floor for ever.
func TestSubscribeFollow_DaemonRefusalStopsInsteadOfReconnecting(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	// The context bounds a regression: without the guard this reconnects for
	// ever, and the test must fail rather than hang.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out bytes.Buffer
	var code int
	_, stderr := captureStd(t, func() {
		code = runSubscribeFollowIO(ctx, map[string]any{"op": "subscribe"}, d.SockPath, "", &out, "")
	})

	if ctx.Err() != nil {
		t.Fatalf("subscribe --follow never returned on a refused subscription — it is reconnect-looping")
	}
	if code == 0 {
		t.Errorf("subscribe --follow returned exit 0 on a refused subscription; want non-zero")
	}
	if strings.Contains(out.String(), "SubscribeHandler not registered") {
		t.Errorf("subscribe --follow forwarded the refusal to its writer as an event line:\n%s", out.String())
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("subscribe --follow did not report the refusal on stderr; got: %q", stderr)
	}
}

// TestCommsRecvWait_DaemonRefusalIsNotATimeout pins that `comms recv --wait`
// separates "refused" from "nothing arrived in time". They have different exit
// codes and different fixes.
func TestCommsRecvWait_DaemonRefusalIsNotATimeout(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	var code int
	_, stderr := captureStd(t, func() {
		code = runCommsRecvWait(d.SockPath, "bravo", "", "", "", false, 3*time.Second)
	})

	if code == 0 {
		t.Errorf("comms recv --wait returned exit 0 on a refused subscription; want non-zero")
	}
	if code == commsRecvWaitTimeoutExit {
		t.Errorf("comms recv --wait reported its timeout code on a refused subscription; the cause is the refusal, not a quiet bus")
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("comms recv --wait did not report the refusal on stderr; got: %q", stderr)
	}
}

// TestCommsRecvFollow_DaemonRefusalStopsInsteadOfReconnecting is the follow-mode
// counterpart: a refusal must end the stream, not restart it.
func TestCommsRecvFollow_DaemonRefusalStopsInsteadOfReconnecting(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out bytes.Buffer
	var code int
	_, stderr := captureStd(t, func() {
		code = runCommsRecvFollowIO(ctx, d.SockPath, "bravo", "", "", "", false, &out)
	})

	if ctx.Err() != nil {
		t.Fatalf("comms recv --follow never returned on a refused subscription — it is reconnect-looping")
	}
	if code == 0 {
		t.Errorf("comms recv --follow returned exit 0 on a refused subscription; want non-zero")
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("comms recv --follow did not report the refusal on stderr; got: %q", stderr)
	}
}

// TestRunViaDaemon_DaemonRefusalIsNamed pins that `harmonik run` names the
// refusal. It already exits 1, so the exit code is not the defect here — the
// silent, causeless failure is.
func TestRunViaDaemon_DaemonRefusalIsNamed(t *testing.T) {
	d := startFakeDaemon(t, replyOnce(refusalReply()))

	conn, err := net.Dial("unix", d.SockPath)
	if err != nil {
		t.Fatalf("dial fake daemon: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The fake daemon replies only after it decodes one request, so send the
	// same subscribe request runViaDaemon sends.
	req, err := json.Marshal(map[string]any{
		"op":                "subscribe",
		"types":             []string{"queue_group_completed"},
		"heartbeat_seconds": 60,
	})
	if err != nil {
		t.Fatalf("marshal subscribe request: %v", err)
	}
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write subscribe request: %v", err)
	}

	var code int
	_, stderr := captureStd(t, func() {
		code = viaWatchGroupCompletion(conn, "q-1", 0, nil, nil)
	})

	if code == 0 {
		t.Errorf("harmonik run returned exit 0 on a refused subscription; want non-zero")
	}
	if !strings.Contains(stderr, "SubscribeHandler not registered") {
		t.Errorf("harmonik run did not name the refusal on stderr; got: %q", stderr)
	}
}
