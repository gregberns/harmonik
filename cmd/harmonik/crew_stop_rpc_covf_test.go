package main

// crew_stop_rpc_covf_test.go — coverage-drain chunk F: the `harmonik crew stop`
// verb, covering both its arg-validation surface and its daemon-RPC construction
// via the in-process fake daemon (testsupport_daemon_test.go).
//
// What is covered:
//   - runCrewStopSubcommand   help/arg errors + the marshalled crew-stop request
//                             (op + payload.name + payload.pause_queue), plus the
//                             daemon-error (exit 1) and daemon-down (exit 17) legs
//   - crewDialAndSend         the ok / not-ok / socket-absent(17) branches
//
// SKIPPED: the daemon-side teardown itself (StopCrewSession/KillSession) lives in
// internal/daemon and is not exercised by a CLI-side fake.
//
// Mutates os.Stdout/os.Stderr → no t.Parallel.

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCrewStopSubcommand_ArgErrors: help exits 0; an unknown flag and a wrong
// positional count both exit 1, with no RPC attempted.
func TestRunCrewStopSubcommand_ArgErrors(t *testing.T) {
	silenceStderr(t)

	var helpCode int
	_ = captureStdoutDuring(t, func() { helpCode = runCrewStopSubcommand([]string{"--help"}) })
	if helpCode != 0 {
		t.Fatalf("--help: exit = %d, want 0", helpCode)
	}
	if code := runCrewStopSubcommand([]string{"--bogus"}); code != 1 {
		t.Fatalf("unknown flag: exit = %d, want 1", code)
	}
	if code := runCrewStopSubcommand([]string{"alpha", "beta"}); code != 1 {
		t.Fatalf("two positionals: exit = %d, want 1", code)
	}
	if code := runCrewStopSubcommand([]string{"--socket", "/x/y.sock"}); code != 1 {
		t.Fatalf("missing name: exit = %d, want 1", code)
	}
}

// TestRunCrewStopSubcommand_RPCSuccess drives a full crew-stop through the fake
// daemon and asserts the exact marshalled request, then a happy exit 0.
func TestRunCrewStopSubcommand_RPCSuccess(t *testing.T) {
	silenceStderr(t)
	d := startFakeDaemon(t, replyOnce(map[string]any{"ok": true}))

	var code int
	out := captureStdoutDuring(t, func() {
		code = runCrewStopSubcommand([]string{"alpha", "--pause-queue", "--socket", d.SockPath})
	})
	if code != 0 {
		t.Fatalf("crew stop: exit = %d, want 0", code)
	}
	if want := "crew alpha stopped"; !strings.Contains(out, want) {
		t.Errorf("stdout = %q, want to contain %q", out, want)
	}

	// Assert the marshalled request the CLI sent.
	raw := <-d.Requests()
	var req struct {
		Op      string `json:"op"`
		Payload struct {
			Name       string `json:"name"`
			PauseQueue bool   `json:"pause_queue"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request %q: %v", raw, err)
	}
	if req.Op != "crew-stop" {
		t.Errorf("op = %q, want crew-stop", req.Op)
	}
	if req.Payload.Name != "alpha" {
		t.Errorf("payload.name = %q, want alpha", req.Payload.Name)
	}
	if !req.Payload.PauseQueue {
		t.Errorf("payload.pause_queue = false, want true (--pause-queue given)")
	}
}

// TestRunCrewStopSubcommand_RPCDaemonError: a not-ok daemon response makes the
// CLI exit 1 (crewDialAndSend's !resp.Ok branch).
func TestRunCrewStopSubcommand_RPCDaemonError(t *testing.T) {
	silenceStderr(t)
	d := startFakeDaemon(t, replyOnce(map[string]any{"ok": false, "error": "no such crew"}))

	if code := runCrewStopSubcommand([]string{"ghost", "--socket", d.SockPath}); code != 1 {
		t.Fatalf("daemon-error: exit = %d, want 1", code)
	}
	<-d.Requests() // drain
}

// TestRunCrewStopSubcommand_DaemonDown: dialing an absent socket is the exit-17
// "daemon not running" leg of crewDialAndSend.
func TestRunCrewStopSubcommand_DaemonDown(t *testing.T) {
	silenceStderr(t)
	dir := newProjectFixture(t) // .harmonik exists but no daemon is listening
	absentSock := filepath.Join(dir, ".harmonik", "daemon.sock")

	if code := runCrewStopSubcommand([]string{"alpha", "--socket", absentSock}); code != 17 {
		t.Fatalf("daemon-down: exit = %d, want 17", code)
	}
}

// TestCrewDialAndSend_SocketAbsent covers crewDialAndSend directly on a missing
// socket path: it maps ENOENT to the exit-17 daemon-not-running contract.
func TestCrewDialAndSend_SocketAbsent(t *testing.T) {
	silenceStderr(t)
	dir := newProjectFixture(t)
	sock := filepath.Join(dir, ".harmonik", "does-not-exist.sock")

	_, code := crewDialAndSend(sock, "crew stop", []byte(`{"op":"crew-stop"}`))
	if code != 17 {
		t.Fatalf("crewDialAndSend absent socket: exit = %d, want 17", code)
	}
}

// TestCrewDialAndSend_OK covers the success leg end-to-end against the fake.
func TestCrewDialAndSend_OK(t *testing.T) {
	silenceStderr(t)
	d := startFakeDaemon(t, func(conn net.Conn, _ []byte) {
		_, _ = conn.Write([]byte(`{"ok":true,"result":{"session_id":"sid-1"}}`))
	})

	resp, code := crewDialAndSend(d.SockPath, "crew start", []byte(`{"op":"crew-start"}`))
	if code != 0 {
		t.Fatalf("crewDialAndSend ok: exit = %d, want 0", code)
	}
	if !resp.Ok {
		t.Errorf("resp.Ok = false, want true")
	}
	<-d.Requests()
}
