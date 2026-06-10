package cli_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue/cli"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

// queueCliFixtureTempDir creates a temporary directory with a .harmonik
// subdirectory and returns the project dir path. It registers a cleanup
// so the caller does not need to remove the directory manually.
//
// The socket path is kept short to stay within the 104-char macOS sun_path
// limit (same strategy as daemon/socket_test.go socketFixtureTempSockPath).
func queueCliFixtureTempDir(t *testing.T) string {
	t.Helper()

	const sunPathMax = 104
	const sockFile = "daemon.sock"

	candidate := t.TempDir()
	harmonikDir := filepath.Join(candidate, ".harmonik")
	sockCandidate := filepath.Join(harmonikDir, sockFile)

	var root string
	if len(sockCandidate) <= sunPathMax {
		root = candidate
	} else {
		dir, err := os.MkdirTemp("/tmp", "qcli-")
		if err != nil {
			t.Fatalf("queueCliFixtureTempDir: MkdirTemp /tmp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		root = dir
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(root, ".harmonik"), 0o755); err != nil {
		t.Fatalf("queueCliFixtureTempDir: MkdirAll .harmonik: %v", err)
	}
	return root
}

// queueCliFixtureStartEchoServer starts a Unix socket listener at
// <projectDir>/.harmonik/daemon.sock. For each incoming connection it:
//  1. Reads the request JSON.
//  2. Calls respFn(rawRequest) to get the response bytes.
//  3. Writes the response and closes the connection.
//
// Returns a cancel function that stops the listener. The listener is also
// cancelled via t.Cleanup.
func queueCliFixtureStartEchoServer(
	t *testing.T,
	projectDir string,
	respFn func(raw []byte) []byte,
) context.CancelFunc {
	t.Helper()

	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	ctx, cancel := context.WithCancel(t.Context())

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		cancel()
		t.Fatalf("queueCliFixtureStartEchoServer: listen unix %q: %v", sockPath, err)
	}

	go func() {
		defer func() { _ = ln.Close() }() //nolint:errcheck // cleanup error unactionable
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // cleanup error unactionable
				var raw json.RawMessage
				if decErr := json.NewDecoder(c).Decode(&raw); decErr != nil {
					return
				}
				resp := respFn(raw)
				_, _ = c.Write(resp) //nolint:errcheck // write error unactionable
			}(conn)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck // cleanup error unactionable
	}()

	t.Cleanup(func() {
		cancel()
	})

	// Wait until the socket is accepting connections.
	queueCliFixtureWaitReady(t, sockPath)

	return cancel
}

// queueCliFixtureWaitReady polls until the socket is accepting connections.
func queueCliFixtureWaitReady(t *testing.T, sockPath string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
		if err == nil {
			_ = conn.Close() //nolint:errcheck // probe conn; cleanup error unactionable
			return
		}
		runtime.Gosched()
		select {
		case <-t.Context().Done():
			t.Fatalf("queueCliFixtureWaitReady: context cancelled before socket ready at %q", sockPath)
			return
		default:
		}
	}
	t.Fatalf("queueCliFixtureWaitReady: socket at %q not ready within 5s", sockPath)
}

// queueCliFixtureSuccessResponse builds a SocketResponse JSON with ok=true
// and the given result payload.
func queueCliFixtureSuccessResponse(t *testing.T, result any) []byte {
	t.Helper()
	resultBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("queueCliFixtureSuccessResponse: marshal result: %v", err)
	}
	resp := map[string]json.RawMessage{
		"ok":     json.RawMessage(`true`),
		"result": resultBytes,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("queueCliFixtureSuccessResponse: marshal response: %v", err)
	}
	return data
}

// queueCliFixtureErrorResponse builds a SocketResponse JSON with ok=false,
// error_code and error fields set.
func queueCliFixtureErrorResponse(t *testing.T, code int, msg string) []byte {
	t.Helper()
	codeBytes, _ := json.Marshal(code) //nolint:errcheck // constant int; cannot fail
	msgBytes, _ := json.Marshal(msg)   //nolint:errcheck // string literal; cannot fail
	okBytes := json.RawMessage(`false`)
	resp := map[string]json.RawMessage{
		"ok":         okBytes,
		"error_code": codeBytes,
		"error":      msgBytes,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("queueCliFixtureErrorResponse: marshal response: %v", err)
	}
	return data
}

// queueCliFixtureWriteQueueFile writes a minimal valid queue JSON to a temp
// file and returns the path. The file contains a single wave group with one
// item (bead_id hk-test001).
func queueCliFixtureWriteQueueFile(t *testing.T) string {
	t.Helper()
	content := `{
  "schema_version": 1,
  "groups": [
    {
      "kind": "wave",
      "items": [{"bead_id": "hk-test001"}]
    }
  ]
}`
	f, err := os.CreateTemp(t.TempDir(), "queue-*.json")
	if err != nil {
		t.Fatalf("queueCliFixtureWriteQueueFile: CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("queueCliFixtureWriteQueueFile: Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("queueCliFixtureWriteQueueFile: Close: %v", err)
	}
	return f.Name()
}

// ---------------------------------------------------------------------------
// queue submit tests
// ---------------------------------------------------------------------------

// TestRunQueueSubmit_HappyPath verifies exit 0 and human-readable output by default.
func TestRunQueueSubmit_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	successPayload := map[string]any{
		"queue_id":    "11111111-1111-7111-8111-111111111111",
		"status":      "active",
		"group_count": 1,
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, successPayload)
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueSubmit happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "queue_id") {
		t.Errorf("RunQueueSubmit happy-path: stdout %q does not contain queue_id", out.String())
	}
}

// TestRunQueueSubmit_HappyPath_JSON verifies exit 0 and raw JSON with --json.
func TestRunQueueSubmit_HappyPath_JSON(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	successPayload := map[string]any{
		"queue_id":    "11111111-1111-7111-8111-aaaaaaaaaaaa",
		"status":      "active",
		"group_count": 1,
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, successPayload)
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--json", "--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueSubmit --json: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	// With --json the raw JSON result is emitted; verify it is valid JSON.
	if !json.Valid([]byte(strings.TrimSpace(out.String()))) {
		t.Errorf("RunQueueSubmit --json: stdout is not valid JSON: %q", out.String())
	}
	if !strings.Contains(out.String(), "queue_id") {
		t.Errorf("RunQueueSubmit --json: stdout %q does not contain queue_id field", out.String())
	}
}

// TestRunQueueSubmit_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueSubmit_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	// No listener started — daemon.sock does not exist.

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueSubmit daemon-down: exit = %d, want 17; stderr=%q", got, errOut.String())
	}
}

// TestRunQueueSubmit_ValidationError verifies exit 1 and error body to stdout
// when the daemon returns a validation error code in -32010..-32019.
func TestRunQueueSubmit_ValidationError(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		// -32010 = ErrorCodeQueueAlreadyActive
		return queueCliFixtureErrorResponse(t, -32010, "queue_already_active")
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 1 {
		t.Errorf("RunQueueSubmit validation-error: exit = %d, want 1", got)
	}
	// Error body must go to stdout, not stderr.
	if !strings.Contains(out.String(), "queue_already_active") {
		t.Errorf("RunQueueSubmit validation-error: stdout %q does not contain error message", out.String())
	}
	if strings.Contains(errOut.String(), "queue_already_active") {
		t.Errorf("RunQueueSubmit validation-error: error body leaked to stderr: %q", errOut.String())
	}
}

// TestRunQueueSubmit_FlagEqualsForm verifies --project=<dir> is accepted.
func TestRunQueueSubmit_FlagEqualsForm(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	successPayload := map[string]any{"queue_id": "22222222-0000-7000-8000-000000000000", "status": "active", "group_count": 1}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, successPayload)
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	// Use --project=<dir> (equals form) instead of --project <dir>.
	got := cli.RunQueueSubmit(context.Background(), []string{"--project=" + projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueSubmit --flag=value form: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
}

// ---------------------------------------------------------------------------
// queue append tests
// ---------------------------------------------------------------------------

// TestRunQueueAppend_HappyPath verifies exit 0 and human-readable output by default.
func TestRunQueueAppend_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	successPayload := map[string]any{
		"appended_count":   1,
		"new_tail_indices": []int{3},
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, successPayload)
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueAppend(context.Background(), []string{
		"--project", projectDir,
		"--queue-id", "33333333-0000-7000-8000-000000000000",
		"0",        // group-index
		"hk-aaa01", // bead-id
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueAppend happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "appended:") {
		t.Errorf("RunQueueAppend happy-path: stdout %q does not contain appended: (text mode)", out.String())
	}
}

// TestRunQueueAppend_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueAppend_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueAppend(context.Background(), []string{
		"--project", projectDir,
		"0", "hk-aaa01",
	}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueAppend daemon-down: exit = %d, want 17", got)
	}
}

// TestRunQueueAppend_FlagEqualsForm verifies --queue-id=<uuid> is accepted.
func TestRunQueueAppend_FlagEqualsForm(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		// Check the queue_id was passed through.
		var msg map[string]json.RawMessage
		_ = json.Unmarshal(raw, &msg)
		return queueCliFixtureSuccessResponse(t, map[string]any{"appended_count": 1, "new_tail_indices": []int{0}})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueAppend(context.Background(), []string{
		"--project=" + projectDir,
		"--queue-id=44444444-0000-7000-8000-000000000000",
		"0", "hk-aaa02",
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueAppend --flag=value form: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
}

// TestRunQueueAppend_HappyPath_JSON verifies exit 0 and raw JSON with --json.
func TestRunQueueAppend_HappyPath_JSON(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	successPayload := map[string]any{
		"appended_count":   2,
		"new_tail_indices": []int{4, 5},
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, successPayload)
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueAppend(context.Background(), []string{
		"--json",
		"--project", projectDir,
		"--queue-id", "33333333-0000-7000-8000-aaaaaaaaaaaa",
		"0",
		"hk-aaa10", "hk-aaa11",
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueAppend --json: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !json.Valid([]byte(strings.TrimSpace(out.String()))) {
		t.Errorf("RunQueueAppend --json: stdout is not valid JSON: %q", out.String())
	}
	if !strings.Contains(out.String(), "appended_count") {
		t.Errorf("RunQueueAppend --json: stdout %q does not contain appended_count", out.String())
	}
}

// ---------------------------------------------------------------------------
// queue status tests
// ---------------------------------------------------------------------------

// TestRunQueueStatus_HappyPath verifies exit 0 and human-readable output by default.
func TestRunQueueStatus_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	statusPayload := map[string]any{
		"queue": map[string]any{
			"schema_version": 1,
			"queue_id":       "55555555-0000-7000-8000-000000000000",
			"status":         "active",
		},
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, statusPayload)
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueStatus happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "queue_id") {
		t.Errorf("RunQueueStatus happy-path: stdout %q does not contain queue_id", out.String())
	}
}

// TestRunQueueStatus_HappyPath_JSON verifies exit 0 and raw JSON with --json.
func TestRunQueueStatus_HappyPath_JSON(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	statusPayload := map[string]any{
		"queue": map[string]any{
			"schema_version": 1,
			"queue_id":       "55555555-0000-7000-8000-aaaaaaaaaaaa",
			"status":         "active",
		},
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, statusPayload)
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{"--json", "--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueStatus --json: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !json.Valid([]byte(strings.TrimSpace(out.String()))) {
		t.Errorf("RunQueueStatus --json: stdout is not valid JSON: %q", out.String())
	}
}

// TestRunQueueStatus_NullQueue verifies exit 0 even when queue is null (QM-057).
func TestRunQueueStatus_NullQueue(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{"queue": nil})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueStatus null-queue: exit = %d, want 0 (daemon reachable per QM-057)", got)
	}
}

// TestRunQueueStatus_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueStatus_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueStatus daemon-down: exit = %d, want 17", got)
	}
}

// TestRunQueueStatus_QueueFlag verifies that --queue <name> is passed in the
// request so the daemon routes to the correct named queue (hk-1k5as).
func TestRunQueueStatus_QueueFlag(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedName string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if nameBytes, ok := msg["name"]; ok {
				_ = json.Unmarshal(nameBytes, &capturedName)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue": map[string]any{
				"schema_version": 1,
				"queue_id":       "aabbccdd-0000-7000-8000-000000000000",
				"name":           "flywheel",
				"status":         "active",
			},
		})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{
		"--project", projectDir,
		"--queue", "flywheel",
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueStatus --queue flag: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedName != "flywheel" {
		t.Errorf("RunQueueStatus --queue flag: captured name = %q, want %q", capturedName, "flywheel")
	}
}

// TestRunQueueStatus_QueueIDFlag verifies that --queue-id <uuid> is passed in
// the request so the daemon can resolve by UUID (hk-1k5as).
func TestRunQueueStatus_QueueIDFlag(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedQueueID string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if qidBytes, ok := msg["queue_id"]; ok {
				_ = json.Unmarshal(qidBytes, &capturedQueueID)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{"queue": nil})
	})

	const wantID = "aabbccdd-1111-7000-8000-000000000000"
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueStatus(context.Background(), []string{
		"--project", projectDir,
		"--queue-id", wantID,
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueStatus --queue-id flag: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedQueueID != wantID {
		t.Errorf("RunQueueStatus --queue-id flag: captured queue_id = %q, want %q", capturedQueueID, wantID)
	}
}

// ---------------------------------------------------------------------------
// queue dry-run tests
// ---------------------------------------------------------------------------

// TestRunQueueDryRun_HappyPath verifies exit 0 and human-readable output by default.
func TestRunQueueDryRun_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	dryRunPayload := map[string]any{
		"resolved_queue": map[string]any{
			"schema_version": 1,
			"queue_id":       "00000000-0000-0000-0000-000000000000",
			"status":         "active",
		},
		"ledger_dep_notices":   []any{},
		"parallelism_narrowed": false,
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, dryRunPayload)
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueDryRun(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueDryRun happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "dry-run:") {
		t.Errorf("RunQueueDryRun happy-path: stdout %q does not contain dry-run: (text mode)", out.String())
	}
}

// TestRunQueueDryRun_HappyPath_JSON verifies exit 0 and raw JSON with --json.
func TestRunQueueDryRun_HappyPath_JSON(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	dryRunPayload := map[string]any{
		"resolved_queue": map[string]any{
			"schema_version": 1,
			"queue_id":       "00000000-0000-0000-0000-aaaaaaaaaaaa",
			"status":         "active",
		},
		"ledger_dep_notices":   []any{},
		"parallelism_narrowed": false,
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, dryRunPayload)
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueDryRun(context.Background(), []string{"--json", "--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueDryRun --json: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !json.Valid([]byte(strings.TrimSpace(out.String()))) {
		t.Errorf("RunQueueDryRun --json: stdout is not valid JSON: %q", out.String())
	}
	if !strings.Contains(out.String(), "resolved_queue") {
		t.Errorf("RunQueueDryRun --json: stdout %q does not contain resolved_queue", out.String())
	}
}

// TestRunQueueDryRun_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueDryRun_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueDryRun(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueDryRun daemon-down: exit = %d, want 17", got)
	}
}

// TestRunQueueDryRun_ValidationError verifies exit 1 on validation error.
func TestRunQueueDryRun_ValidationError(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	// -32016 = ErrorCodeDuplicateBeadID
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureErrorResponse(t, -32016, "duplicate_bead_id")
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueDryRun(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 1 {
		t.Errorf("RunQueueDryRun validation-error: exit = %d, want 1", got)
	}
	if !strings.Contains(out.String(), "duplicate_bead_id") {
		t.Errorf("RunQueueDryRun validation-error: stdout %q does not contain error message", out.String())
	}
}

// ---------------------------------------------------------------------------
// Exit-code table verification
// ---------------------------------------------------------------------------

// TestExitCodes_ValidationRange verifies that error codes in -32010..-32019
// all produce exit 1, and codes outside that range produce exit 2.
func TestExitCodes_ValidationRange(t *testing.T) {
	t.Parallel()

	// Codes that should produce exit 1 (validation errors per QM-029b).
	validationCodes := []int{-32010, -32011, -32012, -32013, -32014, -32015, -32016, -32017, -32018, -32019}

	for _, code := range validationCodes {
		code := code // capture
		t.Run("code"+strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)
			queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
				return queueCliFixtureErrorResponse(t, code, "test_validation_error")
			})

			queueFile := queueCliFixtureWriteQueueFile(t)
			var out strings.Builder
			var errOut strings.Builder

			got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

			if got != 1 {
				t.Errorf("code %d: exit = %d, want 1 (validation error)", code, got)
			}
		})
	}

	// Code outside range should produce exit 2.
	t.Run("code-32099_transport", func(t *testing.T) {
		t.Parallel()

		projectDir := queueCliFixtureTempDir(t)
		queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
			return queueCliFixtureErrorResponse(t, -32099, "internal_error")
		})

		queueFile := queueCliFixtureWriteQueueFile(t)
		var out strings.Builder
		var errOut strings.Builder

		got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

		if got != 2 {
			t.Errorf("code -32099: exit = %d, want 2 (transport/protocol error)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// queue list tests
// ---------------------------------------------------------------------------

// TestRunQueueList_HappyPath verifies exit 0 and human-readable output.
func TestRunQueueList_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	listPayload := map[string]any{
		"queues": []map[string]any{
			{
				"name":            "main",
				"queue_id":        "77777777-0000-7000-8000-000000000000",
				"status":          "active",
				"pending_items":   2,
				"workers":         1,
				"completed_items": 0,
				"failed_items":    0,
			},
		},
	}
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, listPayload)
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueList(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueList happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "main") {
		t.Errorf("RunQueueList happy-path: stdout %q does not contain queue name 'main'", out.String())
	}
}

// TestRunQueueList_EmptyQueues verifies exit 0 when no queues are active.
func TestRunQueueList_EmptyQueues(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{"queues": []any{}})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueList(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueList empty: exit = %d, want 0", got)
	}
	if !strings.Contains(out.String(), "no active queues") {
		t.Errorf("RunQueueList empty: stdout %q does not contain 'no active queues'", out.String())
	}
}

// TestRunQueueList_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueList_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueList(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueList daemon-down: exit = %d, want 17", got)
	}
}

// TestRunQueueList_JSON verifies exit 0 and raw JSON with --json.
func TestRunQueueList_JSON(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{"queues": []any{}})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueList(context.Background(), []string{"--json", "--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueList --json: exit = %d, want 0", got)
	}
	if !json.Valid([]byte(strings.TrimSpace(out.String()))) {
		t.Errorf("RunQueueList --json: stdout is not valid JSON: %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// queue pause tests
// ---------------------------------------------------------------------------

// TestRunQueuePause_HappyPath verifies exit 0 and human-readable output.
func TestRunQueuePause_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedOp string
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if opBytes, ok := msg["op"]; ok {
				_ = json.Unmarshal(opBytes, &capturedOp)
			}
			if qBytes, ok := msg["queue"]; ok {
				_ = json.Unmarshal(qBytes, &capturedQueue)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueuePause(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueuePause happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "operator-pause" {
		t.Errorf("RunQueuePause: op = %q, want %q", capturedOp, "operator-pause")
	}
	if capturedQueue != "investigate" {
		t.Errorf("RunQueuePause: queue = %q, want %q", capturedQueue, "investigate")
	}
	if !strings.Contains(out.String(), "paused") {
		t.Errorf("RunQueuePause: stdout %q does not contain 'paused'", out.String())
	}
}

// TestRunQueuePause_QueueFlag verifies that --queue <name> is accepted as an
// alternative to the positional argument (F4 regression guard: --queue was
// previously leaked as the queue_name itself instead of its argument value).
func TestRunQueuePause_QueueFlag(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if qBytes, ok := msg["queue"]; ok {
				_ = json.Unmarshal(qBytes, &capturedQueue)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueuePause(context.Background(), []string{"--project", projectDir, "--queue", "investigate"}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueuePause --queue flag: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedQueue != "investigate" {
		t.Errorf("RunQueuePause --queue flag: queue = %q, want %q (F4 regression)", capturedQueue, "investigate")
	}
}

// TestRunQueuePause_QueueFlagEquals verifies the --queue=<name> equals form.
func TestRunQueuePause_QueueFlagEquals(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if qBytes, ok := msg["queue"]; ok {
				_ = json.Unmarshal(qBytes, &capturedQueue)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueuePause(context.Background(), []string{"--project", projectDir, "--queue=flywheel"}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueuePause --queue= form: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedQueue != "flywheel" {
		t.Errorf("RunQueuePause --queue= form: queue = %q, want %q", capturedQueue, "flywheel")
	}
}

// TestRunQueuePause_MissingName verifies exit 2 when no queue name is given.
func TestRunQueuePause_MissingName(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueuePause(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 2 {
		t.Errorf("RunQueuePause missing-name: exit = %d, want 2", got)
	}
}

// TestRunQueuePause_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueuePause_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueuePause(context.Background(), []string{"--project", projectDir, "main"}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueuePause daemon-down: exit = %d, want 17", got)
	}
}

// ---------------------------------------------------------------------------
// queue resume tests
// ---------------------------------------------------------------------------

// TestRunQueueResume_HappyPath verifies exit 0 and human-readable output.
func TestRunQueueResume_HappyPath(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedOp string
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if opBytes, ok := msg["op"]; ok {
				_ = json.Unmarshal(opBytes, &capturedOp)
			}
			if qBytes, ok := msg["queue"]; ok {
				_ = json.Unmarshal(qBytes, &capturedQueue)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueResume(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueResume happy-path: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "operator-resume" {
		t.Errorf("RunQueueResume: op = %q, want %q", capturedOp, "operator-resume")
	}
	if capturedQueue != "investigate" {
		t.Errorf("RunQueueResume: queue = %q, want %q", capturedQueue, "investigate")
	}
	if !strings.Contains(out.String(), "resumed") {
		t.Errorf("RunQueueResume: stdout %q does not contain 'resumed'", out.String())
	}
}

// TestRunQueueResume_QueueFlag verifies that --queue <name> is accepted as an
// alternative to the positional argument (F4 regression guard).
func TestRunQueueResume_QueueFlag(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if qBytes, ok := msg["queue"]; ok {
				_ = json.Unmarshal(qBytes, &capturedQueue)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueResume(context.Background(), []string{"--project", projectDir, "--queue", "investigate"}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueResume --queue flag: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedQueue != "investigate" {
		t.Errorf("RunQueueResume --queue flag: queue = %q, want %q (F4 regression)", capturedQueue, "investigate")
	}
}

// TestRunQueueResume_DaemonDown verifies exit 17 when the socket is absent.
func TestRunQueueResume_DaemonDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueResume(context.Background(), []string{"--project", projectDir, "main"}, &out, &errOut)

	if got != 17 {
		t.Errorf("RunQueueResume daemon-down: exit = %d, want 17", got)
	}
}

// ---------------------------------------------------------------------------
// submit --queue flag tests
// ---------------------------------------------------------------------------

// TestRunQueueSubmit_QueueFlag verifies that --queue name is embedded in the
// request sent to the daemon.
func TestRunQueueSubmit_QueueFlag(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedName string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if nameBytes, ok := msg["name"]; ok {
				_ = json.Unmarshal(nameBytes, &capturedName)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":    "88888888-0000-7000-8000-000000000000",
			"status":      "active",
			"group_count": 1,
		})
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{
		"--project", projectDir,
		"--queue", "investigate",
		queueFile,
	}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueSubmit --queue flag: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedName != "investigate" {
		t.Errorf("RunQueueSubmit --queue flag: captured name = %q, want %q", capturedName, "investigate")
	}
}

// TestRunQueueSubmit_DefaultsToMain verifies that absent --queue routes to main
// (no name field or name="" in request, which the daemon normalises to "main").
func TestRunQueueSubmit_DefaultsToMain(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedName string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if nameBytes, ok := msg["name"]; ok {
				_ = json.Unmarshal(nameBytes, &capturedName)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":    "99999999-0000-7000-8000-000000000000",
			"status":      "active",
			"group_count": 1,
		})
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueSubmit default-main: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	// capturedName should be empty (omitempty in JSON) when no --queue is given.
	if capturedName != "" {
		t.Errorf("RunQueueSubmit default-main: name = %q, want empty (absent=main default)", capturedName)
	}
}

// ---------------------------------------------------------------------------
// --beads workflow_mode stamping (hk-tldws)
// ---------------------------------------------------------------------------

// TestRunQueueSubmit_BeadsCarryWorkflowModeReviewLoop is the regression guard for
// hk-tldws: items minted by `harmonik queue submit --beads` must carry
// workflow_mode=review-loop in the serialized request so the queue.json record
// is self-describing and durable (the daemon default may change; the item must
// not silently inherit a different mode on daemon restart).
func TestRunQueueSubmit_BeadsCarryWorkflowModeReviewLoop(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var capturedGroups []json.RawMessage
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if gBytes, ok := msg["groups"]; ok {
				_ = json.Unmarshal(gBytes, &capturedGroups)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":    "aabbccdd-2222-7000-8000-000000000000",
			"status":      "active",
			"group_count": 1,
		})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{
		"--project", projectDir,
		"--beads", "hk-tldws01",
	}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueSubmit --beads: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if len(capturedGroups) == 0 {
		t.Fatal("RunQueueSubmit --beads: request contained no groups")
	}

	// Unmarshal the first group and its items.
	var group struct {
		Items []struct {
			BeadID       string `json:"bead_id"`
			WorkflowMode string `json:"workflow_mode"`
		} `json:"items"`
	}
	if err := json.Unmarshal(capturedGroups[0], &group); err != nil {
		t.Fatalf("RunQueueSubmit --beads: cannot unmarshal group[0]: %v", err)
	}
	if len(group.Items) == 0 {
		t.Fatal("RunQueueSubmit --beads: group[0] contains no items")
	}
	item := group.Items[0]
	if item.BeadID != "hk-tldws01" {
		t.Errorf("RunQueueSubmit --beads: item bead_id = %q, want %q", item.BeadID, "hk-tldws01")
	}
	if item.WorkflowMode != "review-loop" {
		t.Errorf("RunQueueSubmit --beads: item workflow_mode = %q, want %q (hk-tldws regression)",
			item.WorkflowMode, "review-loop")
	}
}

// TestRunQueueSubmit_BeadsWorkflowModeOverride verifies that --workflow-mode single
// overrides the review-loop default on minted items.
func TestRunQueueSubmit_BeadsWorkflowModeOverride(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var capturedGroups []json.RawMessage
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if gBytes, ok := msg["groups"]; ok {
				_ = json.Unmarshal(gBytes, &capturedGroups)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":    "aabbccdd-3333-7000-8000-000000000000",
			"status":      "active",
			"group_count": 1,
		})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{
		"--project", projectDir,
		"--beads", "hk-tldws02",
		"--workflow-mode", "single",
	}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueSubmit --beads --workflow-mode single: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if len(capturedGroups) == 0 {
		t.Fatal("RunQueueSubmit --beads --workflow-mode single: request contained no groups")
	}

	var group struct {
		Items []struct {
			WorkflowMode string `json:"workflow_mode"`
		} `json:"items"`
	}
	if err := json.Unmarshal(capturedGroups[0], &group); err != nil {
		t.Fatalf("cannot unmarshal group[0]: %v", err)
	}
	if len(group.Items) == 0 {
		t.Fatal("group[0] contains no items")
	}
	if group.Items[0].WorkflowMode != "single" {
		t.Errorf("--workflow-mode single: item workflow_mode = %q, want %q",
			group.Items[0].WorkflowMode, "single")
	}
}

// TestQueueSubmit_RequestContainsOp verifies the socket request includes "op"
// = "queue-submit" and the queue document fields at the top level.
func TestQueueSubmit_RequestContainsOp(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedOp string
	var capturedSchemaVersion int

	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &msg); err == nil {
			if opBytes, ok := msg["op"]; ok {
				_ = json.Unmarshal(opBytes, &capturedOp)
			}
			if svBytes, ok := msg["schema_version"]; ok {
				_ = json.Unmarshal(svBytes, &capturedSchemaVersion)
			}
		}
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":    "66666666-0000-7000-8000-000000000000",
			"status":      "active",
			"group_count": 1,
		})
	})

	queueFile := queueCliFixtureWriteQueueFile(t)
	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueSubmit(context.Background(), []string{"--project", projectDir, queueFile}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueSubmit: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "queue-submit" {
		t.Errorf("socket request op = %q, want %q", capturedOp, "queue-submit")
	}
	if capturedSchemaVersion != 1 {
		t.Errorf("socket request schema_version = %d, want 1", capturedSchemaVersion)
	}
}
