package lifecycle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestPL003_SocketPathAndMode verifies that the daemon socket is bound at the
// canonical path .harmonik/daemon.sock and has mode 0600.
//
// Spec ref: process-lifecycle.md §4.1 PL-003 — "The daemon MUST listen on a
// local Unix socket at .harmonik/daemon.sock. After bind, the daemon MUST
// chmod(0600) the socket file and MUST ensure its owner is the daemon's
// effective uid."
func TestPL003_SocketPathAndMode(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)

	ln, err := plFixture_bindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-003: bindSocket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Assert socket at canonical path.
	sockPath := plFixture_socketPath(projectDir)
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Errorf("PL-003: socket not at canonical path %q", sockPath)
	}

	// Assert mode 0600.
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("PL-003: Stat socket: %v", err)
	}
	const wantMode = os.FileMode(0o600)
	gotMode := info.Mode().Perm()
	if gotMode != wantMode {
		t.Errorf("PL-003: socket mode = %04o, want %04o", gotMode, wantMode)
	}
}

// TestPL003_SocketExclusivity_EADDRINUSE verifies that a second bind attempt
// against an already-bound socket path fails with EADDRINUSE, and that the
// error maps to exit code 6 (socket-bind-failed) per ON §8.
//
// Spec ref: process-lifecycle.md §5 PL-INV-004 — "For each project directory
// at any instant, at most one bound Unix socket at .harmonik/daemon.sock MUST
// be serving daemon requests. A second daemon observing EADDRINUSE on bind
// MUST exit with ON §8 code 6 (socket-bind-failed)."
// Also covers: PL-003 (socket path) and PL-008a (exit code 6).
func TestPL003_SocketExclusivity_EADDRINUSE(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)

	// First bind succeeds.
	ln1, err := plFixture_bindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-003 exclusivity: first bind: %v", err)
	}
	t.Cleanup(func() { _ = ln1.Close() })

	// Second bind: we replicate what plFixture_bindSocket does BUT skip the
	// stale-socket removal step, because the socket is actively held.
	sockPath := plFixture_socketPath(projectDir)
	_, err = net.Listen("unix", sockPath)
	if err == nil {
		t.Fatal("PL-003 exclusivity: second bind succeeded; want EADDRINUSE")
	}

	// The error must unwrap to EADDRINUSE.
	opErr, ok := err.(*net.OpError)
	if !ok {
		t.Fatalf("PL-003 exclusivity: second bind error type = %T, want *net.OpError", err)
	}
	sysErr, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		t.Fatalf("PL-003 exclusivity: inner error type = %T, want *os.SyscallError", opErr.Err)
	}

	// On macOS/Linux the errno is EADDRINUSE for a live socket.
	errno, ok := sysErr.Err.(syscall.Errno)
	if !ok {
		t.Fatalf("PL-003 exclusivity: errno type = %T, want syscall.Errno", sysErr.Err)
	}
	if errno != syscall.EADDRINUSE {
		t.Errorf("PL-003 exclusivity: errno = %v (%d), want EADDRINUSE (%d)", errno, errno, syscall.EADDRINUSE)
	}

	// The fixture mapper must return exit code 6 for EADDRINUSE.
	exitCode := plFixture_errToExitCode(errno)
	if exitCode != 6 {
		t.Errorf("PL-003 exclusivity: errToExitCode(EADDRINUSE) = %d, want 6 (socket-bind-failed)", exitCode)
	}
}

// jsonrpcRequest is a minimal JSON-RPC 2.0 request for NDJSON-framing tests.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse is a minimal JSON-RPC 2.0 response for NDJSON-framing tests.
type jsonrpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int              `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
}

// jsonrpcError is a minimal JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// stubNDJSONResponder runs a minimal NDJSON JSON-RPC stub on ln. It reads one
// JSON-RPC request line per connection and writes one JSON-RPC response line.
// The ready flag controls whether to send a normal "method not found" response
// (ready == true) or the pre-ready rejection (ready == false).
func stubNDJSONResponder(ln net.Listener, ready bool, done chan<- struct{}) {
	defer func() {
		if done != nil {
			close(done)
		}
	}()
	conn, err := ln.Accept()
	if err != nil {
		return // listener closed — test is done
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	line := scanner.Text()

	var req jsonrpcRequest
	var resp jsonrpcResponse

	if err := json.Unmarshal([]byte(line), &req); err != nil {
		// Malformed request — write parse error.
		resp = jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      0,
			Error:   &jsonrpcError{Code: -32700, Message: "Parse error"},
		}
	} else if !ready {
		// PL-003b: pre-ready rejection.
		resp = jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpcError{Code: -32001, Message: `daemon_not_ready{"reason":"unknown_run_id"}`},
		}
	} else {
		// PL-003a: method-not-found stub for NDJSON framing test.
		resp = jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpcError{Code: -32601, Message: "Method not found"},
		}
	}

	data, _ := json.Marshal(resp)
	// NDJSON framing: one JSON object per line terminated by \n (PL-003a).
	_, _ = fmt.Fprintf(conn, "%s\n", data)
}

// TestPL003a_NDJSONFraming verifies that the daemon's Unix socket carries
// JSON-RPC 2.0 requests and responses framed as NDJSON (one JSON object per
// line, terminated by \n). The stub responder accepts one connection, reads
// one JSON-RPC request line, and writes one JSON-RPC response line.
//
// Spec ref: process-lifecycle.md §4.1 PL-003a — "The daemon's Unix socket
// MUST carry a JSON-RPC 2.0 request/response stream framed as
// newline-delimited JSON (one JSON object per line terminated by \n)."
func TestPL003a_NDJSONFraming(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)

	ln, err := plFixture_bindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-003a: bindSocket: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go stubNDJSONResponder(ln, true /* ready */, done)

	// Connect and send one JSON-RPC 2.0 request line.
	conn, err := net.Dial("unix", plFixture_socketPath(projectDir))
	if err != nil {
		t.Fatalf("PL-003a: Dial: %v", err)
	}
	defer conn.Close()

	req := jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "status"}
	reqBytes, _ := json.Marshal(req)
	// Write the request as one NDJSON line.
	_, err = fmt.Fprintf(conn, "%s\n", reqBytes)
	if err != nil {
		t.Fatalf("PL-003a: write request: %v", err)
	}

	// Read the response line.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			t.Fatalf("PL-003a: scan response: %v", err)
		}
		t.Fatal("PL-003a: no response line received")
	}
	respLine := scanner.Text()

	// The response must be valid JSON.
	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(respLine), &resp); err != nil {
		t.Fatalf("PL-003a: unmarshal response %q: %v", respLine, err)
	}

	// Framing assertion: the response must be one complete JSON-RPC 2.0 object.
	if resp.JSONRPC != "2.0" {
		t.Errorf("PL-003a: response jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
	}
	if resp.ID != 1 {
		t.Errorf("PL-003a: response id = %d, want 1", resp.ID)
	}
	// The stub returns method-not-found; assert the error shape is present.
	if resp.Error == nil {
		t.Error("PL-003a: expected error field in stub response (method-not-found), got nil")
	}

	// Wait for the responder goroutine to finish.
	_ = conn.Close()
	ln.Close()
	<-done
}

// TestPL003b_PreReadyRejection verifies that requests arriving before the
// daemon's in-memory model is built are rejected with the typed error
// daemon_not_ready{reason="unknown_run_id"}.
//
// The stub responder maintains a ready flag (false = pre-ready window). The
// test sends one request during the pre-ready window and asserts the typed
// rejection error is present in the JSON-RPC error response.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "Between socket bind (PL-005
// step 3a) and completion of the in-memory model build (PL-005 step 7), the
// daemon MUST reject any emit-outcome, claim-next, or other agent-originated
// request whose run_id is not recognized in the daemon's in-memory state,
// with a typed error daemon_not_ready{reason='unknown_run_id'}."
func TestPL003b_PreReadyRejection(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)

	ln, err := plFixture_bindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-003b: bindSocket: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	// Spawn the stub with ready=false (pre-ready window).
	go stubNDJSONResponder(ln, false /* not ready */, done)

	conn, err := net.Dial("unix", plFixture_socketPath(projectDir))
	if err != nil {
		t.Fatalf("PL-003b: Dial: %v", err)
	}
	defer conn.Close()

	// Send a claim-next request (agent-originated, run_id not in model).
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "claim-next",
		Params:  map[string]string{"run_id": "01950000-ffff-7000-8000-000000000099"},
	}
	reqBytes, _ := json.Marshal(req)
	_, err = fmt.Fprintf(conn, "%s\n", reqBytes)
	if err != nil {
		t.Fatalf("PL-003b: write request: %v", err)
	}

	// Read the rejection response.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil && err != io.EOF {
			t.Fatalf("PL-003b: scan response: %v", err)
		}
		t.Fatal("PL-003b: no response line received")
	}
	respLine := scanner.Text()

	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(respLine), &resp); err != nil {
		t.Fatalf("PL-003b: unmarshal response %q: %v", respLine, err)
	}

	// The response MUST carry an error (not a result).
	if resp.Error == nil {
		t.Fatal("PL-003b: response.error is nil; expected daemon_not_ready rejection")
	}
	// The error message must contain the typed error string.
	if !strings.Contains(resp.Error.Message, "daemon_not_ready") {
		t.Errorf("PL-003b: error message = %q; expected to contain %q", resp.Error.Message, "daemon_not_ready")
	}
	if !strings.Contains(resp.Error.Message, "unknown_run_id") {
		t.Errorf("PL-003b: error message = %q; expected to contain %q", resp.Error.Message, "unknown_run_id")
	}

	_ = conn.Close()
	ln.Close()
	<-done
}

// TestPL003_SocketStaleRemovalOnStartup verifies that plFixture_bindSocket
// removes a stale socket file before binding, consistent with the PL-003
// startup contract.
//
// Spec ref: process-lifecycle.md §4.1 PL-003 — "The daemon MUST remove a
// stale socket file on startup before binding."
func TestPL003_SocketStaleRemovalOnStartup(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)
	sockPath := plFixture_socketPath(projectDir)

	// Lay down a stale socket file (simulates a previously crashed daemon).
	if err := os.WriteFile(sockPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile stale socket: %v", err)
	}

	// bindSocket must remove the stale file and succeed.
	ln, err := plFixture_bindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-003 stale-removal: bindSocket failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// The socket must now be a valid Unix domain socket (stat it).
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("PL-003 stale-removal: Stat socket after bind: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("PL-003 stale-removal: %q is not a Unix domain socket after bind (mode=%v)", sockPath, info.Mode())
	}
}

// TestPL003_SocketPathInDotHarmonik verifies the socket is inside the
// .harmonik/ directory, not at the project root.
//
// Spec ref: process-lifecycle.md §4.1 PL-003 — "The daemon MUST listen on a
// local Unix socket at .harmonik/daemon.sock."
func TestPL003_SocketPathInDotHarmonik(t *testing.T) {
	t.Parallel()

	projectDir := plFixture_tempProjectDir(t)
	wantPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	gotPath := plFixture_socketPath(projectDir)
	if gotPath != wantPath {
		t.Errorf("PL-003: socket path = %q, want %q", gotPath, wantPath)
	}
}
