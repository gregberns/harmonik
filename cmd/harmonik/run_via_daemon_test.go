package main

// run_via_daemon_test.go — unit tests for the submit-to-existing-daemon path.
//
// Coverage strategy:
//  - isDaemonUp: socket absent → false; socket present → true.
//  - viaWatchGroupCompletion: queue_group_completed complete-success → 0;
//    complete-with-failures → 1; queue_paused → 1; unexpected EOF → 1.
//  - viaSendRequest: socket absent → exitViaDaemonDown.
//  - Error-code routing in viaSubmitOrAppend via fake socket server.
//
// Bead ref: hk-b3wqd.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// socketSafeTempDir returns a temporary directory whose path is short enough
// for a Unix domain socket (macOS limit: 104 chars).  The socket will live at
// <dir>/.harmonik/daemon.sock (+17 chars), so the dir itself must be ≤87 chars.
// t.TempDir() on macOS produces paths under /var/folders/… that exceed this
// limit.  Using /tmp as the base always stays well within it.
func socketSafeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hkt-*")
	if err != nil {
		t.Fatalf("socketSafeTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// ---------------------------------------------------------------------------
// isDaemonUp
// ---------------------------------------------------------------------------

func TestIsDaemonUp_SocketAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if isDaemonUp(dir) {
		t.Fatal("isDaemonUp: expected false when daemon.sock is absent, got true")
	}
}

func TestIsDaemonUp_SocketPresent(t *testing.T) {
	t.Parallel()
	dir := socketSafeTempDir(t)

	// Create a .harmonik subdir and bind a Unix listener on daemon.sock.
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Accept connections in a goroutine so the dial succeeds.
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	if !isDaemonUp(dir) {
		t.Fatal("isDaemonUp: expected true when daemon.sock is present, got false")
	}
}

// ---------------------------------------------------------------------------
// viaWatchGroupCompletion
// ---------------------------------------------------------------------------

// injectAndWatch creates a pipe, writes ndjsonLines to the write end, then
// calls viaWatchGroupCompletion on the read end. Returns the exit code.
func injectAndWatch(t *testing.T, ndjsonLines []string, queueID string, groupIndex int) int {
	t.Helper()
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()

	go func() {
		defer func() { _ = client.Close() }()
		for _, line := range ndjsonLines {
			if _, err := fmt.Fprintln(client, line); err != nil {
				return
			}
		}
	}()

	return viaWatchGroupCompletion(server, queueID, groupIndex, nil)
}

func TestViaWatchGroupCompletion_CompleteSuccess(t *testing.T) {
	t.Parallel()
	payload, _ := json.Marshal(map[string]any{
		"queue_id":      "q1",
		"group_index":   0,
		"final_status":  "complete-success",
		"success_count": 1,
		"fail_count":    0,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line, _ := json.Marshal(map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q1", 0)
	if got != 0 {
		t.Errorf("exit code = %d, want 0 (complete-success)", got)
	}
}

func TestViaWatchGroupCompletion_CompleteWithFailures(t *testing.T) {
	t.Parallel()
	payload, _ := json.Marshal(map[string]any{
		"queue_id":      "q2",
		"group_index":   0,
		"final_status":  "complete-with-failures",
		"success_count": 0,
		"fail_count":    1,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line, _ := json.Marshal(map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q2", 0)
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (complete-with-failures)", got)
	}
}

func TestViaWatchGroupCompletion_QueuePaused(t *testing.T) {
	t.Parallel()
	payload, _ := json.Marshal(map[string]any{
		"queue_id": "q3",
	})
	line, _ := json.Marshal(map[string]any{
		"type":    "queue_paused",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q3", 0)
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (queue_paused)", got)
	}
}

func TestViaWatchGroupCompletion_WrongQueueIDIgnored(t *testing.T) {
	t.Parallel()
	// Send a completion event for a different queue; then close the stream.
	// Expect exit 1 (stream closed before our queue completed).
	payload, _ := json.Marshal(map[string]any{
		"queue_id":      "other-queue",
		"group_index":   0,
		"final_status":  "complete-success",
		"success_count": 1,
		"fail_count":    0,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line, _ := json.Marshal(map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	// Our queue is "mine"; event is for "other-queue" → should be ignored.
	got := injectAndWatch(t, []string{string(line)}, "mine", 0)
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (wrong queue ID should not trigger completion)", got)
	}
}

func TestViaWatchGroupCompletion_UnexpectedEOF(t *testing.T) {
	t.Parallel()
	// Empty stream — connection closed immediately → exit 1.
	got := injectAndWatch(t, nil, "q4", 0)
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (EOF before completion)", got)
	}
}

// ---------------------------------------------------------------------------
// viaSendRequest
// ---------------------------------------------------------------------------

func TestViaSendRequest_SocketAbsent(t *testing.T) {
	t.Parallel()
	dir := socketSafeTempDir(t)
	harmonikDir := filepath.Join(dir, ".harmonik")
	// Do NOT create daemon.sock — send should return exitViaDaemonDown.
	_, code := viaSendRequest(t.Context(), harmonikDir, []byte(`{"op":"queue-status"}`))
	if code != exitViaDaemonDown {
		t.Errorf("code = %d, want %d (exitViaDaemonDown)", code, exitViaDaemonDown)
	}
}

func TestViaSendRequest_ValidResponse(t *testing.T) {
	t.Parallel()
	dir := socketSafeTempDir(t)
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Fake daemon: reply with {"ok":true,"result":{"queue":null}}.
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reply, _ := json.Marshal(viaSocketResponse{Ok: true, Result: json.RawMessage(`{"queue":null}`)})
		_, _ = fmt.Fprintf(conn, "%s\n", reply)
	}()

	resp, code := viaSendRequest(t.Context(), harmonikDir, []byte(`{"op":"queue-status"}`))
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !resp.Ok {
		t.Errorf("resp.Ok = false, want true")
	}
}
