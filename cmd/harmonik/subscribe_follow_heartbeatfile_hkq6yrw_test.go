package main

// subscribe_follow_heartbeatfile_hkq6yrw_test.go — regression test for bead
// hk-q6yrw: ops-monitor's watch-stalled detector previously keyed liveness off
// whether the watch process itself had SENT a comms message recently
// (last_watch_msg_ts). A healthy, idle, event-driven watch with nothing to
// escalate legitimately goes quiet on outbound messages while its subscribe
// stream is still alive — the daemon keeps delivering periodic heartbeat
// lines over the open connection regardless of whether anything actionable
// happened. That gap produced repeated false watch-stalled pages.
//
// Fix: `harmonik subscribe --follow --heartbeat-file <path>` touches <path>'s
// mtime on EVERY decoded line, including idle heartbeat lines that carry no
// escalation-worthy content. ops-monitor can then use that mtime as a genuine
// stream-liveness signal, independent of whether the watch sent anything.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestSubscribeFollow_HeartbeatFileTouchedOnHeartbeatLine_Q6YRW verifies that
// a heartbeat-only line (no escalation-worthy event, nothing the watch would
// ever send a comms message about) still updates the heartbeat file's mtime.
// This is the core of the fix: liveness must be provable WITHOUT the watch
// having sent anything.
func TestSubscribeFollow_HeartbeatFileTouchedOnHeartbeatLine_Q6YRW(t *testing.T) {
	sockPath := "/tmp/hkq6yrw-hbfile.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var connCount int32
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
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return
				}
				// A pure idle heartbeat — no event_id, nothing escalation-worthy.
				hb := map[string]any{"type": "heartbeat", "last_event_id": ""}
				_ = json.NewEncoder(c).Encode(hb)
				// Keep the connection open briefly so the client has time to
				// decode before the test asserts; then let it close naturally
				// on test cleanup (follow loop will just reconnect, which is
				// fine — the test only waits for the first touch).
				time.Sleep(200 * time.Millisecond)
			}(conn)
		}
	}()

	heartbeatFile := filepath.Join(t.TempDir(), "stream.heartbeat")
	outFile, _ := os.CreateTemp(t.TempDir(), "sub-hbfile-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	// Cancel + join in cleanup so the reconnect goroutine cannot outlive the
	// test and race a later os.Stderr swap (hk-me8ru).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSubscribeFollowIO(ctx, subscribeFollowBaseReq, sockPath, "" /*sinceEventID*/, outFile, heartbeatFile)
	}()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, statErr := os.Stat(heartbeatFile); statErr == nil {
			if time.Since(info.ModTime()) < 5*time.Second {
				return // touched — test passes
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat file %s was not touched within deadline; stream-liveness signal is not being written on heartbeat-only lines", heartbeatFile)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSubscribeFollow_NoHeartbeatFileWhenPathEmpty_Q6YRW verifies the
// heartbeat-file mechanism is fully opt-in: passing an empty path must not
// create any file, preserving behavior for every existing --follow caller
// that does not pass --heartbeat-file.
func TestSubscribeFollow_NoHeartbeatFileWhenPathEmpty_Q6YRW(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/missing.sock"
	code := runSubscribeFollowIO(context.Background(), subscribeFollowBaseReq, sockPath, "", os.Stdout, "")
	if code != 17 {
		t.Fatalf("runSubscribeFollowIO with missing socket: exit %d, want 17", code)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files created in %s when heartbeat-file path is empty, found %d", dir, len(entries))
	}
}
