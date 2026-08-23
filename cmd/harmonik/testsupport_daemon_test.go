package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type fakeDaemon struct {
	Dir      string // project dir; socket lives at Dir/.harmonik/daemon.sock
	SockPath string // absolute path to the fake daemon.sock
	requests chan []byte
}

// Requests delivers, per accepted connection, the raw JSON request bytes the
// client sent (nil when the request could not be decoded).
func (d *fakeDaemon) Requests() <-chan []byte { return d.requests }

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
	if len(sockPath) >= 104 {
		t.Fatalf("socket path too long for unix sun_path (%d bytes): %s", len(sockPath), sockPath)
	}
	return base
}

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

func replyOnce(reply map[string]any) func(net.Conn, []byte) {
	return func(conn net.Conn, _ []byte) {
		out, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_, _ = conn.Write(out)
	}
}

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
