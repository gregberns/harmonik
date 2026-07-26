package main

// subscribe_cmd_coverage_test.go — behavior tests for runSubscribeSubcommand,
// the `harmonik subscribe` CLI entry point (hk-6ynv4). Covers flag/arg
// validation exit codes, socket resolution (--socket vs --project), the
// non-follow streaming path (event forwarding + request construction), the
// --json no-op alias, and the socket-absent → exit 17 contract.
//
// The follow (--follow) streaming loop is covered separately by
// subscribe_follow_hk5hs5b_test.go and subscribe_follow_heartbeatfile_hkq6yrw_test.go.
//
// Built on the shared fake-daemon toolkit in testsupport_daemon_test.go.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSubscribe_ArgValidation_ExitCodes is the exit-code truth table for
// argument handling that never reaches the socket: help prints and exits 0,
// malformed/unknown flags exit 1.
func TestSubscribe_ArgValidation_ExitCodes(t *testing.T) {
	vgSilenceStd(t)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"help-long", []string{"--help"}, 0},
		{"help-short", []string{"-h"}, 0},
		{"unknown-flag", []string{"--bogus"}, 1},
		{"stray-positional", []string{"run-123"}, 1},
		{"heartbeat-bad-space", []string{"--heartbeat", "notaduration"}, 1},
		{"heartbeat-bad-equals", []string{"--heartbeat=notaduration"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSubscribeSubcommand(tc.args); got != tc.want {
				t.Errorf("runSubscribeSubcommand(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

// TestSubscribe_SocketAbsent_NonFollow pins the contract (hk-y49eu): the
// non-follow path exits 17 when the daemon socket is missing. The former bug
// guarded its ENOENT check behind errors.As(err, &sysErr) for *os.PathError,
// whereas net.Dialer.DialContext returns a *net.OpError (errors.Is(err, ENOENT)
// is true, but the *os.PathError type-assert failed), so the missing-socket
// case fell through to the generic exit-1 arm. The fix routes the dial error
// through the shared commsIsSocketAbsent / commsIsConnRefused predicates, the
// same idiom the follow path and confirm-verdict already use.
func TestSubscribe_SocketAbsent_NonFollow(t *testing.T) {
	vgSilenceStd(t)

	dir := newProjectFixture(t) // .harmonik exists, but no daemon bound
	const want = 17             // hk-y49eu: daemon not running
	if got := runSubscribeSubcommand([]string{"--project", dir}); got != want {
		t.Errorf("--project with no daemon: exit %d, want %d (hk-y49eu; daemon not running)", got, want)
	}
	if got := runSubscribeSubcommand([]string{"--socket", dir + "/.harmonik/nope.sock"}); got != want {
		t.Errorf("--socket to missing path: exit %d, want %d (hk-y49eu; daemon not running)", got, want)
	}
}

// TestSubscribe_StreamsAndRendersEvents drives the non-follow path against a
// fake daemon that streams two NDJSON events then closes. It asserts a clean
// exit 0 and that both events are copied verbatim to stdout.
func TestSubscribe_StreamsAndRendersEvents(t *testing.T) {
	d := startFakeDaemon(t, streamEvents(
		map[string]any{"type": "run_completed", "event_id": "evt-aaa"},
		map[string]any{"type": "run_failed", "event_id": "evt-bbb"},
	))

	var code int
	out := captureStdoutDuring(t, func() {
		code = runSubscribeSubcommand([]string{"--socket", d.SockPath})
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0 on clean EOF after stream", code)
	}
	if !strings.Contains(out, "evt-aaa") || !strings.Contains(out, "evt-bbb") {
		t.Errorf("stdout missing streamed events:\n%s", out)
	}
	// Two events → two NDJSON lines forwarded.
	if n := strings.Count(out, "event_id"); n != 2 {
		t.Errorf("expected 2 forwarded event lines, saw %d:\n%s", n, out)
	}
}

// TestSubscribe_BuildsRequestWithFilters asserts the request the command
// marshals from its flags: op=subscribe, the split --types list, the three
// agent-message addressing filters, and the clamped-seconds heartbeat. The fake
// records the request and closes immediately (zero events → clean exit 0).
func TestSubscribe_BuildsRequestWithFilters(t *testing.T) {
	vgSilenceStd(t)
	d := startFakeDaemon(t, streamEvents()) // no events; close after request

	code := runSubscribeSubcommand([]string{
		"--socket", d.SockPath,
		"--types", "run_completed, run_failed ,",
		"--to", "alice",
		"--from", "bob",
		"--topic", "status",
		"--heartbeat", "30s",
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}

	raw := <-d.Requests()
	var req struct {
		Op               string   `json:"op"`
		Types            []string `json:"types"`
		To               string   `json:"to"`
		From             string   `json:"from"`
		Topic            string   `json:"topic"`
		HeartbeatSeconds int      `json:"heartbeat_seconds"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request: %v (raw=%q)", err, raw)
	}
	if req.Op != "subscribe" {
		t.Errorf("op = %q, want subscribe", req.Op)
	}
	if len(req.Types) != 2 || req.Types[0] != "run_completed" || req.Types[1] != "run_failed" {
		t.Errorf("types = %v, want [run_completed run_failed] (trimmed, empties dropped)", req.Types)
	}
	if req.To != "alice" || req.From != "bob" || req.Topic != "status" {
		t.Errorf("addressing = to:%q from:%q topic:%q, want alice/bob/status", req.To, req.From, req.Topic)
	}
	if req.HeartbeatSeconds != 30 {
		t.Errorf("heartbeat_seconds = %d, want 30", req.HeartbeatSeconds)
	}
}

// TestSubscribe_SinceEventIDInRequest verifies the non-follow path threads
// --since-event-id into the request body as since_event_id.
func TestSubscribe_SinceEventIDInRequest(t *testing.T) {
	vgSilenceStd(t)
	d := startFakeDaemon(t, streamEvents())

	if code := runSubscribeSubcommand([]string{"--socket", d.SockPath, "--since-event-id", "cursor-xyz"}); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	var req struct {
		SinceEventID string `json:"since_event_id"`
	}
	if err := json.Unmarshal(<-d.Requests(), &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.SinceEventID != "cursor-xyz" {
		t.Errorf("since_event_id = %q, want cursor-xyz", req.SinceEventID)
	}
}

// TestSubscribe_JSONFlagIsNoOp confirms --json is accepted (not an unknown-arg
// error) and leaves the NDJSON output unchanged — the stream is already JSON.
func TestSubscribe_JSONFlagIsNoOp(t *testing.T) {
	d := startFakeDaemon(t, streamEvents(
		map[string]any{"type": "run_completed", "event_id": "evt-json"},
	))
	var code int
	out := captureStdoutDuring(t, func() {
		code = runSubscribeSubcommand([]string{"--json", "--socket", d.SockPath})
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0 with --json", code)
	}
	if !strings.Contains(out, "evt-json") {
		t.Errorf("stdout missing event with --json:\n%s", out)
	}
}

// TestSubscribe_ProjectResolvesSocket verifies the --project route resolves
// dir/.harmonik/daemon.sock and reaches the same fake daemon (no --socket).
func TestSubscribe_ProjectResolvesSocket(t *testing.T) {
	d := startFakeDaemon(t, streamEvents(
		map[string]any{"type": "heartbeat", "event_id": "evt-proj"},
	))
	var code int
	out := captureStdoutDuring(t, func() {
		code = runSubscribeSubcommand([]string{"--project", d.Dir})
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0 via --project socket resolution", code)
	}
	if !strings.Contains(out, "evt-proj") {
		t.Errorf("stdout missing event via --project:\n%s", out)
	}
}
