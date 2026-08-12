package main

// comms_daemon_down_provenance_11zpm_test.go — `comms who` and `comms log` must
// say where their answer came from when the daemon is down.
//
// Both verbs read events.jsonl directly and need no daemon. That is deliberate
// and it stays: reading the traffic after the daemon dies is exactly when an
// operator needs it. The defect was the missing label.
//
//	rc=17  queue status  -> daemon not running        CORRECT
//	rc=17  comms recv    -> daemon not running        CORRECT
//	rc=2   comms send    -> daemon not running        CORRECT
//	rc=0   comms who     -> "no agents currently online"        WRONG
//	rc=0   comms log     -> two-month-old traffic, unmarked     WRONG
//
// `comms who` is the sharp one. An empty roster is a legitimate state, so its
// answer is the same sentence a healthy bus with nobody joined prints. The
// honest-degradation path (marking an entry "stale") has nothing to mark when
// the registry is empty, which is the state right after any full restart and
// the state an operator is most likely to be asking about.
//
// Bead ref: hk-11zpm.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commsShortProjectWithEvents builds a project under /tmp (short enough for a
// unix socket path) holding the given events.jsonl lines.
func commsShortProjectWithEvents(t *testing.T, lines ...string) string {
	t.Helper()
	dir := shortProjectDir(t)
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o750); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(filepath.Join(eventsDir, "events.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatalf("write events.jsonl: %v", err)
	}
	return dir
}

// serveCannedUnixSocket stands up a unix listener at sockPath that answers every
// connection with reply and closes. A nil reply accepts and closes at once,
// which is enough to make a "is the daemon up?" probe say yes.
//
// Shared by the daemon-down, wake and send tests in this package: all three
// need a daemon that answers, and none of them needs a real one.
//
// A reply is written only after the request has been read to EOF. The clients
// in this package write their request, half-close, and only then read. A server
// that answers and closes before the request arrives tears the connection down
// under a client that has not written yet, and the client reports "broken pipe"
// or "socket is not connected" instead of the answer. That ordering is decided
// by the scheduler, so it inverts under load and it is what made the merge
// decision red at 0d50b2d45. Reading first is also what the real daemon does.
func serveCannedUnixSocket(t *testing.T, sockPath string, reply []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
	if err != nil {
		t.Skipf("cannot listen on %q: %v", sockPath, err)
	}
	t.Cleanup(func() {
		if closeErr := ln.Close(); closeErr != nil {
			t.Logf("close listener: %v", closeErr)
		}
	})
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			if len(reply) > 0 {
				drainCannedRequest(c)
				if _, werr := c.Write(reply); werr != nil {
					closeConn(c)
					return
				}
			}
			closeConn(c)
		}
	}()
}

// drainCannedRequest reads one client request to EOF, so the reply cannot be
// written and the connection cannot be closed before the client has finished
// writing. The deadline is a backstop: a client that dies mid-request must not
// park this goroutine for the rest of the run. A read fault needs no report --
// the write that follows fails on its own and the caller sees that instead.
func drainCannedRequest(c net.Conn) {
	if err := c.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	if _, err := io.Copy(io.Discard, c); err != nil {
		return
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		return
	}
}

// closeConn closes one accepted connection. The test is already finished with
// it, so a close fault says nothing a caller can act on.
func closeConn(c net.Conn) {
	_ = c.Close()
}

// commsServeIdleDaemon stands up a listener at the project's daemon.sock that
// accepts and closes. It is enough to make the probe report the daemon as up.
func commsServeIdleDaemon(t *testing.T, projectDir string) {
	t.Helper()
	serveCannedUnixSocket(t, filepath.Join(projectDir, ".harmonik", "daemon.sock"), nil)
}

// TestCommsWho_EmptyRosterWithNoDaemonSaysSo is the dangerous case: the answer
// is plausible, correct-looking and describes a bus that is not there.
func TestCommsWho_EmptyRosterWithNoDaemonSaysSo(t *testing.T) {
	dir := commsShortProjectWithEvents(t) // no presence beats, no daemon.sock

	var code int
	stdout, stderr := captureStd(t, func() { code = runCommsWhoSubcommand([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("comms who exited %d, want 0 — reading the log without a daemon is deliberate (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stderr, "the daemon is not running") {
		t.Errorf("an empty roster with no daemon reads exactly like a live bus nobody has joined, and nothing here says which it is:\n%s", stderr)
	}
	if !strings.Contains(stderr, "No agent can be online now") {
		t.Errorf("the note does not say that no agent CAN be online, which is the fact the reader is missing:\n%s", stderr)
	}
	if !strings.Contains(stderr, filepath.Join(dir, ".harmonik", "events", "events.jsonl")) {
		t.Errorf("the note does not name the file the answer came from:\n%s", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("comms who wrote %q to stdout for an empty roster, want nothing", stdout)
	}
}

// TestCommsWho_WithADaemonUpAddsNoNote is the control. The note is about a
// missing daemon, so a running daemon must not produce it.
func TestCommsWho_WithADaemonUpAddsNoNote(t *testing.T) {
	dir := commsShortProjectWithEvents(t)
	commsServeIdleDaemon(t, dir)

	var code int
	_, stderr := captureStd(t, func() { code = runCommsWhoSubcommand([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("comms who exited %d, want 0 (stderr=%q)", code, stderr)
	}
	if strings.Contains(stderr, "the daemon is not running") {
		t.Errorf("comms who claims the daemon is down while a daemon is listening:\n%s", stderr)
	}
}

// TestCommsLog_WithNoDaemonLabelsTheOutputAsHistory pins the slower form of the
// same failure: old traffic served with nothing marking it as old.
func TestCommsLog_WithNoDaemonLabelsTheOutputAsHistory(t *testing.T) {
	old := time.Now().Add(-70 * 24 * time.Hour).UTC().Format(time.RFC3339)
	dir := commsShortProjectWithEvents(t,
		commsEventLine(t, "01965b00-0000-7000-8000-000000000001", old, "agent_message", map[string]any{
			"from": "alpha", "to": "bravo", "topic": "status", "body": "the gate is green",
		}),
	)

	var code int
	stdout, stderr := captureStd(t, func() { code = runCommsLogSubcommand([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("comms log exited %d, want 0 — reading the log without a daemon is deliberate (stderr=%q)", code, stderr)
	}
	// The answer itself must survive. Refusing here would remove the one view
	// that still works after the daemon dies.
	if !strings.Contains(stdout, "the gate is green") {
		t.Fatalf("comms log stopped serving history: %q", stdout)
	}
	if !strings.Contains(stderr, "the daemon is not running") {
		t.Errorf("comms log served two-month-old traffic with nothing marking it as history:\n%s", stderr)
	}
	if !strings.Contains(stderr, "history, not live traffic") {
		t.Errorf("the note does not tell the reader the lines are not current:\n%s", stderr)
	}
}

// TestCommsLog_NoteStaysOffStdoutUnderJSON protects the machine-readable path.
// A note on stdout would break every reader that decodes the NDJSON.
func TestCommsLog_NoteStaysOffStdoutUnderJSON(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339)
	dir := commsShortProjectWithEvents(t,
		commsEventLine(t, "01965b00-0000-7000-8000-000000000001", ts, "agent_message", map[string]any{
			"from": "alpha", "to": "bravo", "body": "hello",
		}),
	)

	var code int
	stdout, stderr := captureStd(t, func() { code = runCommsLogSubcommand([]string{"--project", dir, "--json"}) })
	if code != 0 {
		t.Fatalf("comms log --json exited %d, want 0 (stderr=%q)", code, stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("comms log --json wrote a non-JSON line to stdout: %q", line)
		}
	}
	if !strings.Contains(stderr, "the daemon is not running") {
		t.Errorf("the daemon-down note vanished under --json instead of moving to stderr:\n%s", stderr)
	}
}
