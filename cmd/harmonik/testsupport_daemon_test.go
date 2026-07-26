package main

// testsupport_daemon_test.go — reusable test infrastructure for the
// integration-heavy `cmd/harmonik` socket commands (subscribe, confirm-verdict,
// and the F/H/I coverage waves to come). It generalizes the one-off fake in
// confirm_verdict_test.go into a small, documented toolkit.
//
// Provided API (all package-main test helpers):
//
//	newProjectFixture(t) string
//	    A temp project dir under a SHORT /tmp base with the .harmonik/ layout.
//	    The resolved socket path dir/.harmonik/daemon.sock stays well under the
//	    ~104-byte unix sun_path limit (macOS). No daemon is started — pair it
//	    with startFakeDaemon, or use it alone to exercise the socket-absent path.
//
//	startFakeDaemon(t, handle) *fakeDaemon
//	    An in-process stand-in for the daemon's Unix-socket RPC server. For each
//	    accepted connection it decodes exactly ONE JSON request object (works for
//	    both half-closing request/response clients AND streaming clients that
//	    keep their write side open), publishes the raw request bytes on
//	    Requests(), then calls handle(conn, req) to produce the reply. The
//	    connection is closed when handle returns.
//
//	(*fakeDaemon).Requests() <-chan []byte
//	    One entry per accepted connection: the raw JSON request bytes the client
//	    sent (nil if the request could not be decoded). Lets a test assert the
//	    exact request the command marshalled.
//
//	replyOnce(reply) handler
//	    A handle func that writes reply (JSON-marshalled) as a single response —
//	    the request/response shape confirm-verdict-style commands consume.
//
//	streamEvents(events...) handler
//	    A handle func that writes each event as one NDJSON line then returns
//	    (closing the connection) — the streaming shape `harmonik subscribe`
//	    consumes. Pass zero events to close immediately after the request.
//
// Stdout capture reuses the existing captureStdoutDuring helper; process-stream
// silencing reuses vgSilenceStd. Because those mutate process globals, tests
// built on this toolkit must NOT call t.Parallel().

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeDaemon is an in-process Unix-socket server that impersonates the harmonik
// daemon for a single test. See the file header for the full contract.
type fakeDaemon struct {
	Dir      string // project dir; socket lives at Dir/.harmonik/daemon.sock
	SockPath string // absolute path to the fake daemon.sock
	requests chan []byte
}

// Requests delivers, per accepted connection, the raw JSON request bytes the
// client sent (nil when the request could not be decoded).
func (d *fakeDaemon) Requests() <-chan []byte { return d.requests }

// newProjectFixture returns a temp project dir with the .harmonik/ layout,
// rooted at a short /tmp base so dir/.harmonik/daemon.sock stays under the unix
// sun_path limit. It fatals if the resolved socket path would be too long.
func newProjectFixture(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "hkfx")
	if err != nil {
		t.Fatalf("mkdtemp project fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.MkdirAll(filepath.Join(base, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	sockPath := filepath.Join(base, ".harmonik", "daemon.sock")
	// 104 is the macOS sun_path ceiling; guard so an over-long TMPDIR fails loud
	// rather than as an opaque "invalid argument" at Listen time.
	if len(sockPath) >= 104 {
		t.Fatalf("socket path too long for unix sun_path (%d bytes): %s", len(sockPath), sockPath)
	}
	return base
}

// startFakeDaemon binds a fake daemon on a fresh project fixture and serves
// connections with handle until the test ends. See the file header for the
// per-connection contract.
func startFakeDaemon(t *testing.T, handle func(conn net.Conn, req []byte)) *fakeDaemon {
	t.Helper()
	dir := newProjectFixture(t)
	sockPath := filepath.Join(dir, ".harmonik", "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %q: %v", sockPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeDaemon{Dir: dir, SockPath: sockPath, requests: make(chan []byte, 8)}
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return // listener closed at test end
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				// Decode exactly one request object. This returns after a full
				// JSON value even when the client keeps its write side open
				// (subscribe), and also works for half-closing clients.
				var req json.RawMessage
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					d.requests <- nil
					return
				}
				d.requests <- []byte(req)
				handle(c, req)
			}(conn)
		}
	}()
	return d
}

// replyOnce returns a handle that writes reply as a single JSON response.
func replyOnce(reply map[string]any) func(net.Conn, []byte) {
	return func(conn net.Conn, _ []byte) {
		out, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_, _ = conn.Write(out)
	}
}

// streamEvents returns a handle that writes each event as one NDJSON line (via
// json.Encoder, which appends the newline) then returns, closing the stream.
func streamEvents(events ...map[string]any) func(net.Conn, []byte) {
	return func(conn net.Conn, _ []byte) {
		enc := json.NewEncoder(conn)
		for _, ev := range events {
			if err := enc.Encode(ev); err != nil {
				return
			}
		}
	}
}
