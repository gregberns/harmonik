package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearCommsSessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HARMONIK_SESSION_ID", "")
	t.Setenv("HARMONIK_RUN_ID", "")
	t.Setenv("HARMONIK_AGENT", "")
}

// TestCommsSend_PrintsMintedEventIDOnStdout verifies that a successful send
// writes the daemon-minted event id, and only that, to stdout. A sender needs
// the exact id to pass back as --reply-to; a friendly "ok" in its place leaves
// the caller with no way to thread a reply.
func TestCommsSend_PrintsMintedEventIDOnStdout(t *testing.T) {
	clearCommsSessionEnv(t)
	const minted = "01965b00-0000-7000-8000-00000000feed"

	d := startFakeDaemon(t, replyOnce(map[string]any{
		"ok":     true,
		"result": map[string]any{"event_id": minted},
	}))
	if err := os.MkdirAll(filepath.Join(d.Dir, ".harmonik", "agents", "alice"), 0o750); err != nil {
		t.Fatalf("declare recipient alice: %v", err)
	}

	var code int
	out, _ := captureStd(t, func() {
		code = runCommsSendSubcommand([]string{
			"--to", "alice",
			"--from", "captain",
			"--socket", d.SockPath,
			"--project", d.Dir,
			"--no-wake",
			"the body",
		})
	})

	if code != 0 {
		t.Fatalf("comms send: exit = %d, want 0", code)
	}
	if strings.TrimRight(out, "\n") != minted {
		t.Errorf("comms send stdout = %q, want exactly the minted event id %q — the sender quotes it back as --reply-to", out, minted)
	}

	payload := commsRequestPayload(commsAwaitRequest(t, d, "comms-send"))
	for key, want := range map[string]string{"from": "captain", "to": "alice", "body": "the body"} {
		if got := commsStringField(payload, key); got != want {
			t.Errorf("comms-send payload %q = %q, want %q", key, got, want)
		}
	}
}

// TestCommsSend_ThreadsReplyToAndTopic verifies the optional addressing fields
// reach the daemon under the names the recv printer echoes back.
func TestCommsSend_ThreadsReplyToAndTopic(t *testing.T) {
	clearCommsSessionEnv(t)
	const replyTo = "01965b00-0000-7000-8000-0000000000bb"

	d := startFakeDaemon(t, replyOnce(map[string]any{
		"ok":     true,
		"result": map[string]any{"event_id": "01965b00-0000-7000-8000-00000000feed"},
	}))

	captureStd(t, func() {
		runCommsSendSubcommand([]string{
			"--to", "alice",
			"--from", "captain",
			"--topic", "status",
			"--reply-to", replyTo,
			"--socket", d.SockPath,
			"--project", d.Dir,
			"--no-wake",
			"the body",
		})
	})

	payload := commsRequestPayload(commsAwaitRequest(t, d, "comms-send"))
	if got := commsStringField(payload, "topic"); got != "status" {
		t.Errorf("comms-send payload topic = %q, want %q", got, "status")
	}
	if got := commsStringField(payload, "in_reply_to"); got != replyTo {
		t.Errorf("comms-send payload in_reply_to = %q, want %q — --reply-to must thread under the same key recv emits", got, replyTo)
	}
}

// TestCommsSend_BroadcastAddressesTheStarSentinel verifies that --broadcast maps
// to the "*" recipient that the log and recv filters both special-case.
func TestCommsSend_BroadcastAddressesTheStarSentinel(t *testing.T) {
	clearCommsSessionEnv(t)

	d := startFakeDaemon(t, replyOnce(map[string]any{
		"ok":     true,
		"result": map[string]any{"event_id": "01965b00-0000-7000-8000-00000000feed"},
	}))

	captureStd(t, func() {
		runCommsSendSubcommand([]string{
			"--broadcast",
			"--from", "captain",
			"--socket", d.SockPath,
			"--project", d.Dir,
			"all hands",
		})
	})

	payload := commsRequestPayload(commsAwaitRequest(t, d, "comms-send"))
	if got := commsStringField(payload, "to"); got != "*" {
		t.Errorf("comms send --broadcast payload to = %q, want the broadcast sentinel %q", got, "*")
	}
}

// TestCommsShouldWake_DirectedByDefault pins the wake decision. Durable
// delivery is only actionable if an idle agent is nudged, so a directed send
// wakes unless the caller opts out; a broadcast never wakes.
func TestCommsShouldWake_DirectedByDefault(t *testing.T) {
	cases := []struct {
		directed bool
		noWake   bool
		want     bool
	}{
		{directed: true, noWake: false, want: true},
		{directed: true, noWake: true, want: false},
		{directed: false, noWake: false, want: false},
		{directed: false, noWake: true, want: false},
	}
	for _, tc := range cases {
		if got := commsShouldWake(tc.directed, tc.noWake); got != tc.want {
			t.Errorf("commsShouldWake(directed=%v, noWake=%v) = %v, want %v", tc.directed, tc.noWake, got, tc.want)
		}
	}
}

// TestCommsSendDaemonDown_ExitsSeventeen verifies that a stopped daemon gives
// the shared daemon-down code. Callers read exit 1 as "your arguments were
// wrong" and stop retrying, so collapsing 17 into 1 turns a transient outage
// into a permanent-looking failure.
func TestCommsSendDaemonDown_ExitsSeventeen(t *testing.T) {
	clearCommsSessionEnv(t)
	project := newProjectFixture(t)
	absent := filepath.Join(project, ".harmonik", "daemon.sock")

	var code int
	captureStd(t, func() {
		code = runCommsSendSubcommand([]string{
			"--to", "alice",
			"--from", "captain",
			"--socket", absent,
			"--project", project,
			"--no-wake",
			"the body",
		})
	})
	if code != 17 {
		t.Errorf("comms send with no daemon: exit = %d, want 17 (daemon down, not an argument error)", code)
	}
}

// TestCommsSend_FlagValidationExitCodes verifies that every bad flag
// combination exits 1 and never reaches the socket. The socket given is a live
// fake daemon, so a request arriving there proves validation ran too late.
func TestCommsSend_FlagValidationExitCodes(t *testing.T) {
	clearCommsSessionEnv(t)

	d := startFakeDaemon(t, replyOnce(map[string]any{
		"ok":     true,
		"result": map[string]any{"event_id": "01965b00-0000-7000-8000-00000000feed"},
	}))

	cases := []struct {
		name string
		args []string
	}{
		{"to and broadcast together", []string{"--to", "alice", "--broadcast", "--from", "captain", "body"}},
		{"neither to nor broadcast", []string{"--from", "captain", "body"}},
		{"wake and no-wake together", []string{"--to", "alice", "--wake", "--no-wake", "--from", "captain", "body"}},
		{"wake cannot target a broadcast", []string{"--broadcast", "--wake", "--from", "captain", "body"}},
		{"from is required", []string{"--to", "alice", "--no-wake", "body"}},
		{"body is required", []string{"--to", "alice", "--from", "captain", "--no-wake"}},
		{"unknown flag", []string{"--to", "alice", "--from", "captain", "--nope", "body"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--socket", d.SockPath, "--project", d.Dir}, tc.args...)
			var code int
			captureStd(t, func() { code = runCommsSendSubcommand(args) })
			if code != 1 {
				t.Errorf("comms send %v: exit = %d, want 1", tc.args, code)
			}
		})
	}

	if n := commsPendingRequestCount(d); n != 0 {
		t.Errorf("comms send dialled the daemon %d time(s) on invalid arguments; validation must run first", n)
	}
}

// TestCommsSend_DaemonErrorExitsOne verifies that a refused send is reported as
// an error, not swallowed with a zero exit and an empty id on stdout.
func TestCommsSend_DaemonErrorExitsOne(t *testing.T) {
	clearCommsSessionEnv(t)

	d := startFakeDaemon(t, func(conn net.Conn, _ []byte) {
		commsRespond(conn, map[string]any{"ok": false, "error": "recipient_unknown"})
	})

	var code int
	out, _ := captureStd(t, func() {
		code = runCommsSendSubcommand([]string{
			"--to", "nobody",
			"--from", "captain",
			"--socket", d.SockPath,
			"--project", d.Dir,
			"--no-wake",
			"the body",
		})
	})
	if code != 1 {
		t.Errorf("comms send refused by the daemon: exit = %d, want 1", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("comms send refused by the daemon printed %q to stdout; a caller must not read that as an event id", out)
	}
}
