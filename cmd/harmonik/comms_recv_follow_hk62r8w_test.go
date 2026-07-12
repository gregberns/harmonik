package main

// comms_recv_follow_hk62r8w_test.go — regression test for bead hk-62r8w.
//
// Bug: when the server sends a SocketResponse error ({"ok":false,"error":"..."})
// on the subscribe connection, the client's decode loop sees env.Type=="" and
// skips it, then gets EOF and reconnects. Because every successful dial resets
// the backoff to 1 s, the client flaps at ~1 s intervals indefinitely.
//
// Fix: detect SocketResponse errors (ok:false) and exit with code 1 instead of
// treating the server-close as a transient connection drop.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestCommsRecvFollow_ServerErrorExitsWithCode1 verifies that a SocketResponse
// error from the server causes --follow to exit with code 1, not enter an
// infinite reconnect loop. Pre-fix the client would skip {"ok":false,...},
// get EOF, and reconnect at ~1 s intervals.
//
// The mock server writes a subscribe_capacity_exceeded SocketResponse error on
// every accepted connection and closes. The test verifies that runCommsRecvFollowIO
// exits with code 1 within a short timeout and that only one connection was made
// (no reconnect loop).
func TestCommsRecvFollow_ServerErrorExitsWithCode1(t *testing.T) {
	t.Parallel()

	sockPath := "/tmp/hk62r8w-commsrecv.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	var connCount int32

	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Server: drain the subscribe request then send a SocketResponse error.
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			atomic.AddInt32(&connCount, 1)
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				// Drain the subscribe request so the TCP buffer is cleared.
				var req map[string]any
				_ = json.NewDecoder(c).Decode(&req)
				// Write a SocketResponse error (no "type" field — that's the crux of the bug).
				errResp, _ := json.Marshal(map[string]any{
					"ok":    false,
					"error": "subscribe_capacity_exceeded",
				})
				_, _ = c.Write(errResp)
				// Close immediately — server side done.
			}(conn)
		}
	}()

	// Wait for listener to be ready.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Discard stdout output.
	devNull, _ := os.Open(os.DevNull)
	defer func() { _ = devNull.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int, 1)
	go func() {
		done <- runCommsRecvFollowIO(ctx, sockPath, "alice", "", "", "", false, io.Discard)
	}()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("runCommsRecvFollowIO returned %d, want 1 (server error)", code)
		}
		n := atomic.LoadInt32(&connCount)
		// Allow at most 2 connections to tolerate a single spurious retry, but
		// the pre-fix loop would produce dozens within a few seconds.
		if n > 2 {
			t.Errorf("server received %d connections — flapping reconnect loop not fixed (want ≤2)", n)
		}
	case <-time.After(5 * time.Second):
		t.Error("runCommsRecvFollowIO did not exit within 5s — stuck in reconnect loop (pre-fix behaviour)")
		cancel()
		<-done
	}
}

// TestCommsRecvFollow_ServerErrorExitsWithCode1_MultipleErrors verifies that
// even when the server sends a plain SocketResponse error ({"ok":false}) with
// no "error" field text, the client still exits cleanly with code 1.
func TestCommsRecvFollow_ServerErrorExitsWithCode1_MultipleErrors(t *testing.T) {
	t.Parallel()

	sockPath := "/tmp/hk62r8w-commsrecv2.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				var req map[string]any
				_ = json.NewDecoder(c).Decode(&req)
				// Minimal SocketResponse error with no error text.
				_, _ = c.Write([]byte(`{"ok":false}`))
			}(conn)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int, 1)
	go func() {
		done <- runCommsRecvFollowIO(ctx, sockPath, "alice", "", "", "", false, io.Discard)
	}()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("runCommsRecvFollowIO returned %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("runCommsRecvFollowIO did not exit within 5s — stuck in reconnect loop")
		cancel()
		<-done
	}
}

// TestSubscribeFollow_ServerErrorExitsWithCode1 verifies that runSubscribeFollowIO
// also exits with code 1 on a SocketResponse error instead of forwarding the
// error JSON to the writer and entering a reconnect loop.
func TestSubscribeFollow_ServerErrorExitsWithCode1(t *testing.T) {
	t.Parallel()

	sockPath := "/tmp/hk62r8w-subfollow.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	var connCount int32

	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			atomic.AddInt32(&connCount, 1)
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				var req map[string]any
				_ = json.NewDecoder(c).Decode(&req)
				errResp, _ := json.Marshal(map[string]any{
					"ok":    false,
					"error": "subscribe_capacity_exceeded",
				})
				_, _ = c.Write(errResp)
			}(conn)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reqBody := map[string]any{
		"op":    "subscribe",
		"types": []string{"agent_message"},
		"to":    "alice",
	}

	done := make(chan int, 1)
	go func() {
		done <- runSubscribeFollowIO(ctx, reqBody, sockPath, "", io.Discard, "")
	}()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("runSubscribeFollowIO returned %d, want 1 (server error)", code)
		}
		n := atomic.LoadInt32(&connCount)
		if n > 2 {
			t.Errorf("server received %d connections — flapping reconnect loop not fixed (want ≤2)", n)
		}
	case <-time.After(5 * time.Second):
		t.Error("runSubscribeFollowIO did not exit within 5s — stuck in reconnect loop")
		cancel()
		<-done
	}
}

// TestCommsRecvFollow_ServerErrorAfterMessages verifies that a server error
// encountered mid-stream (after some events) causes an immediate exit with
// code 1 rather than reconnecting and replaying from the watermark.
func TestCommsRecvFollow_ServerErrorAfterMessages(t *testing.T) {
	t.Parallel()

	sockPath := "/tmp/hk62r8w-midstream.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	outFile, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create outFile: %v", err)
	}
	t.Cleanup(func() { _ = outFile.Close() })

	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Server loops so the readiness-probe connection (which sends no data) is
	// handled gracefully. The probe dials and closes immediately; Decode returns
	// EOF and the handler returns without writing. The real runCommsRecvFollowIO
	// connection sends a subscribe request and gets the message + error.
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				var req map[string]any
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return // probe connection — no subscribe request sent
				}
				enc := json.NewEncoder(c)
				// Send one valid agent_message.
				_ = enc.Encode(map[string]any{
					"type":     "agent_message",
					"event_id": "019f0000-0000-7000-8000-000000000001",
					"payload": map[string]any{
						"from": "captain",
						"to":   "alice",
						"body": "hello",
					},
				})
				// Then send a SocketResponse error.
				errResp, _ := json.Marshal(map[string]any{
					"ok":    false,
					"error": "session_terminated",
				})
				_, _ = c.Write(errResp)
			}(conn)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int, 1)
	go func() {
		done <- runCommsRecvFollowIO(ctx, sockPath, "alice", "", "", "", true /*jsonOut*/, outFile)
	}()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("runCommsRecvFollowIO returned %d, want 1 after mid-stream server error", code)
		}
	case <-time.After(5 * time.Second):
		t.Error("runCommsRecvFollowIO did not exit within 5s — stuck after mid-stream error")
		cancel()
		<-done
	}
}
