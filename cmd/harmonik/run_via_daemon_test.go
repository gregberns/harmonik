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
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
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
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("socketSafeTempDir cleanup: %v", err)
		}
	})
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
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// Register the accept goroutine's JOIN before the listener close, so cleanup
	// runs them in the other order (LIFO): close the listener first, which is what
	// unblocks Accept, then wait for the goroutine to be gone. See the note on
	// acceptErrs below for why waiting matters.
	acceptErrs := make(chan error, 8)
	acceptDone := make(chan struct{})
	t.Cleanup(func() {
		<-acceptDone
		for {
			select {
			case err := <-acceptErrs:
				t.Errorf("close accepted connection: %v", err)
			default:
				return
			}
		}
	})
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})

	// Accept connections in a goroutine so the dial succeeds.
	//
	// The goroutine reports through a channel rather than calling t.Errorf
	// directly. A t.Errorf from a goroutine the test never joins panics the WHOLE
	// test binary with "Log in goroutine after ... has completed" whenever it lands
	// after the last cleanup, and this goroutine only leaves its Accept loop when
	// the listener closes — which IS a cleanup. So the unjoined shape has no
	// ordering that makes it safe.
	go func() {
		defer close(acceptDone)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			if err := conn.Close(); err != nil {
				select {
				case acceptErrs <- err:
				default:
				}
			}
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
func injectAndWatch(t *testing.T, ndjsonLines []string, queueID string, groupIndex ...int) int {
	t.Helper()
	watchedGroupIndex := 0
	if len(groupIndex) > 0 {
		watchedGroupIndex = groupIndex[0]
	}
	server, client := net.Pipe()
	defer func() {
		if err := server.Close(); err != nil {
			t.Errorf("close pipe server: %v", err)
		}
	}()

	// The writer goroutine reports its close error on a channel and is JOINED
	// below. Calling t.Errorf from a goroutine the test never waits for panics the
	// whole binary when it lands after the test completes.
	writerErr := make(chan error, 1)
	go func() {
		defer func() {
			if err := client.Close(); err != nil {
				writerErr <- err
				return
			}
			close(writerErr)
		}()
		for _, line := range ndjsonLines {
			if _, err := fmt.Fprintln(client, line); err != nil {
				return
			}
		}
	}()

	code := viaWatchGroupCompletion(server, queueID, watchedGroupIndex, nil, nil)
	// Close the read end BEFORE joining the writer. viaWatchGroupCompletion
	// returns on the first matching completion event and does not drain, so over
	// an unbuffered net.Pipe a caller whose lines continue past that event would
	// leave the writer parked in Fprintln and the join below would never return.
	// Dropping the result is correct and not a shortcut: net.Pipe's Close is
	// `once.Do(close(done)); return nil` — idempotent AND unconditionally nil. So
	// this call cannot report anything, and neither can the deferred one.
	_ = server.Close()
	if err := <-writerErr; err != nil {
		t.Errorf("close pipe client: %v", err)
	}
	return code
}

func mustMarshalViaDaemon(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}
	return data
}

func TestViaWatchGroupCompletion_CompleteSuccess(t *testing.T) {
	t.Parallel()
	payload := mustMarshalViaDaemon(t, map[string]any{
		"queue_id":      "q1",
		"group_index":   0,
		"final_status":  "complete-success",
		"success_count": 1,
		"fail_count":    0,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line := mustMarshalViaDaemon(t, map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q1")
	if got != 0 {
		t.Errorf("exit code = %d, want 0 (complete-success)", got)
	}
}

func TestViaWatchGroupCompletion_CompleteWithFailures(t *testing.T) {
	t.Parallel()
	payload := mustMarshalViaDaemon(t, map[string]any{
		"queue_id":      "q2",
		"group_index":   0,
		"final_status":  "complete-with-failures",
		"success_count": 0,
		"fail_count":    1,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line := mustMarshalViaDaemon(t, map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q2")
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (complete-with-failures)", got)
	}
}

func TestViaWatchGroupCompletion_QueuePaused(t *testing.T) {
	t.Parallel()
	payload := mustMarshalViaDaemon(t, map[string]any{
		"queue_id": "q3",
	})
	line := mustMarshalViaDaemon(t, map[string]any{
		"type":    "queue_paused",
		"payload": json.RawMessage(payload),
	})
	got := injectAndWatch(t, []string{string(line)}, "q3")
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (queue_paused)", got)
	}
}

func TestViaWatchGroupCompletion_WrongQueueIDIgnored(t *testing.T) {
	t.Parallel()
	// Send a completion event for a different queue; then close the stream.
	// Expect exit 1 (stream closed before our queue completed).
	payload := mustMarshalViaDaemon(t, map[string]any{
		"queue_id":      "other-queue",
		"group_index":   0,
		"final_status":  "complete-success",
		"success_count": 1,
		"fail_count":    0,
		"completed_at":  "2026-01-01T00:00:00Z",
	})
	line := mustMarshalViaDaemon(t, map[string]any{
		"type":    "queue_group_completed",
		"payload": json.RawMessage(payload),
	})
	// Our queue is "mine"; event is for "other-queue" → should be ignored.
	got := injectAndWatch(t, []string{string(line)}, "mine")
	if got != 1 {
		t.Errorf("exit code = %d, want 1 (wrong queue ID should not trigger completion)", got)
	}
}

func TestViaWatchGroupCompletion_UnexpectedEOF(t *testing.T) {
	t.Parallel()
	// Empty stream — connection closed immediately → exit 1.
	got := injectAndWatch(t, nil, "q4")
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
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	sockPath := filepath.Join(harmonikDir, "daemon.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// Fake daemon: reply with {"ok":true,"result":{"queue":null}}.
	//
	// Errors travel back on a channel, not through t.Errorf. This goroutine used
	// to call t.Errorf directly with nothing joining it, and a t.Errorf that lands
	// after the last cleanup panics the whole cmd/harmonik binary — reported as
	// some unrelated test failing.
	daemonErrs := make(chan error, 4)
	daemonDone := make(chan struct{})
	t.Cleanup(func() {
		<-daemonDone
		for {
			select {
			case err := <-daemonErrs:
				t.Errorf("fake daemon: %v", err)
			default:
				return
			}
		}
	})
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	go func() {
		defer close(daemonDone)
		report := func(err error) {
			select {
			case daemonErrs <- err:
			default:
			}
		}
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				report(fmt.Errorf("close daemon connection: %w", err))
			}
		}()
		// Read the request to EOF BEFORE replying. This is the synchronisation
		// between the two sides, not a formality — delete it and the test fails
		// under load. viaSendRequest writes its payload and only then half-closes,
		// so this read returns exactly when the client's write has landed. A fake
		// daemon that replies and closes straight after Accept can finish the whole
		// exchange while the client goroutine is still descheduled between its dial
		// and its write; the client then writes to a closed peer, gets EPIPE, and
		// viaSendRequest reports a transport error (exit 1) against a daemon that
		// answered correctly. Reading first also drains this side's receive buffer,
		// so the close below cannot discard the reply the client has yet to read.
		// TestViaSubmitOrAppendWritesPendingGroup below reads first for the same
		// reason, as does every other fake daemon in this package.
		if _, readErr := io.ReadAll(conn); readErr != nil {
			report(fmt.Errorf("read daemon request: %w", readErr))
			return
		}
		reply, marshalErr := json.Marshal(viaSocketResponse{Ok: true, Result: json.RawMessage(`{"queue":null}`)})
		if marshalErr != nil {
			report(fmt.Errorf("marshal daemon response: %w", marshalErr))
			return
		}
		if _, writeErr := fmt.Fprintf(conn, "%s\n", reply); writeErr != nil {
			report(fmt.Errorf("write daemon response: %w", writeErr))
		}
	}()

	resp, code := viaSendRequest(t.Context(), harmonikDir, []byte(`{"op":"queue-status"}`))
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !resp.Ok {
		t.Errorf("resp.Ok = false, want true")
	}
}

func TestViaSubmitOrAppendWritesPendingGroup(t *testing.T) {
	t.Parallel()

	dir := socketSafeTempDir(t)
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", filepath.Join(harmonikDir, "daemon.sock"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// Same shape as TestViaSendRequest_ValidResponse: the fake daemon reports
	// through a channel and is joined in cleanup, because a t.Errorf from an
	// unjoined goroutine panics the whole binary once the test has completed.
	// The join is registered FIRST so it runs LAST — the listener close is what
	// releases a goroutine still sitting in Accept.
	fakeErrs := make(chan error, 4)
	fakeDone := make(chan struct{})
	t.Cleanup(func() {
		<-fakeDone
		for {
			select {
			case err := <-fakeErrs:
				t.Errorf("fake daemon: %v", err)
			default:
				return
			}
		}
	})
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Errorf("close listener: %v", closeErr)
		}
	})

	received := make(chan []byte, 1)
	go func() {
		defer close(fakeDone)
		report := func(err error) {
			select {
			case fakeErrs <- err:
			default:
			}
		}
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			if closeErr := conn.Close(); closeErr != nil {
				report(fmt.Errorf("close daemon connection: %w", closeErr))
			}
		}()
		payload, readErr := io.ReadAll(conn)
		if readErr != nil {
			return
		}
		received <- payload
		if encodeErr := json.NewEncoder(conn).Encode(viaSocketResponse{
			Ok: true, Result: json.RawMessage(`{"queue_id":"q-submit"}`),
		}); encodeErr != nil {
			report(fmt.Errorf("encode daemon response: %w", encodeErr))
		}
	}()

	items := []queue.Item{queue.NewPendingItem(queue.Item{BeadID: "hk-via-submit"})}
	queueID, groupIndex, appended, exitCode := viaSubmitOrAppend(t.Context(), harmonikDir, items, queue.GroupKindStream)
	if exitCode != 0 || queueID != "q-submit" || groupIndex != 0 || appended {
		t.Fatalf("viaSubmitOrAppend = (%q, %d, %t, %d), want successful new group", queueID, groupIndex, appended, exitCode)
	}

	payload := <-received
	var envelope struct {
		Groups []struct {
			Status queue.GroupStatus `json:"status"`
		} `json:"groups"`
	}
	if unmarshalErr := json.Unmarshal(payload, &envelope); unmarshalErr != nil {
		t.Fatalf("decode queue-submit payload: %v", unmarshalErr)
	}
	if len(envelope.Groups) != 1 || envelope.Groups[0].Status != queue.GroupStatusPending {
		t.Fatalf("submitted groups = %+v, want one pending group", envelope.Groups)
	}
}
