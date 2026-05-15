// Package cli implements the client-side helpers for the hk queue subcommand
// family. Each subcommand opens daemon.sock, sends a JSON-RPC-shaped request
// over the Unix socket (the same SocketRequest / SocketResponse protocol used
// by agent subprocesses — see internal/daemon/socket.go), reads the response,
// and exits with the exit code specified by PL-008a / PL-028c.
//
// Exit-code contract (applies to all four subcommands):
//
//	 0   — success; response JSON is written to stdout.
//	 1   — validation error (any QueueValidationReason per QM-029b); the full
//	         JSON-RPC error body (code + message + detail) is written to stdout.
//	 2   — transport or protocol error (malformed response, framing error,
//	         unknown JSON-RPC error code outside -32010..-32019).
//	17   — daemon not running (socket absent or ECONNREFUSED) per PL-008a /
//	         ON §8 code 17 (multi-daemon-target-missing).
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/process-lifecycle.md §4.4 PL-008a (exit-code taxonomy)
//   - specs/queue-model.md §2.10 (request/response RECORD shapes)
//
// Bead ref: hk-eblue.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

// exitSuccess is the exit code for a successful operation.
const exitSuccess = 0

// exitValidationError is the exit code for a queue validation error (QM-029b).
// The error body is written to stdout (not stderr) so callers can parse it.
const exitValidationError = 1

// exitTransportError is the exit code for transport or protocol errors
// (malformed response, framing error, unknown error code outside -32010..-32019).
const exitTransportError = 2

// exitDaemonDown is the exit code when the daemon socket is absent or the
// connection is refused (PL-008a code 17: multi-daemon-target-missing).
const exitDaemonDown = 17

// validationErrorCodeMin and validationErrorCodeMax bound the reserved
// JSON-RPC error code range for queue validation errors per QM-029b.
const validationErrorCodeMin = -32019
const validationErrorCodeMax = -32010

// socketRequest is the wire envelope sent to the daemon socket for all
// queue operations. The "op" field selects the handler; the remaining
// fields carry the operation-specific payload (silently ignored by the
// server for irrelevant ops). This mirrors daemon.SocketRequest.
type socketRequest struct {
	Op            string          `json:"op"`
	QueueID       string          `json:"queue_id,omitempty"`
	GroupIndex    *int            `json:"group_index,omitempty"`
	BeadIDs       []string        `json:"bead_ids,omitempty"`
	SchemaVersion *int            `json:"schema_version,omitempty"`
	Groups        json.RawMessage `json:"groups,omitempty"`
}

// socketResponse is the wire envelope received from the daemon socket.
// This mirrors daemon.SocketResponse.
type socketResponse struct {
	Ok        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	ErrorCode int             `json:"error_code,omitempty"`
}

// errorBody is the JSON body written to stdout for validation errors.
// It carries the structured error so callers can parse the error type.
type errorBody struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// sendRequest opens daemon.sock under harmonikDir, sends the given raw JSON
// bytes as a single socket message, reads the SocketResponse, and returns it.
//
// Returns (resp, nil) on a clean response (even if resp.Ok is false).
// Returns exitDaemonDown if the socket is absent or connection is refused.
// Returns exitTransportError for any other dial or I/O error.
func sendRequest(ctx context.Context, harmonikDir string, payload []byte) (socketResponse, int) {
	sockPath := harmonikDir + "/daemon.sock"

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		// Distinguish "daemon not running" (socket absent or ECONNREFUSED) from
		// other network errors.
		if isSocketAbsent(err) || isConnectionRefused(err) {
			return socketResponse{}, exitDaemonDown
		}
		return socketResponse{}, exitTransportError
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	// Write request.
	if _, writeErr := conn.Write(payload); writeErr != nil {
		return socketResponse{}, exitTransportError
	}

	// Half-close the write side so the server's json.Decoder can detect EOF.
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // cleanup error unactionable
	}

	// Read response.
	var resp socketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		if errors.Is(decErr, io.EOF) || errors.Is(decErr, io.ErrUnexpectedEOF) {
			return socketResponse{}, exitTransportError
		}
		return socketResponse{}, exitTransportError
	}

	return resp, -1 // -1 = no early exit; caller processes resp
}

// handleResponse converts a socketResponse to an exit code, writing the
// appropriate JSON to out.
//
//   - resp.Ok == true  → writes resp.Result to out, returns exitSuccess.
//   - resp.Ok == false, validation error code → writes errorBody JSON to out,
//     returns exitValidationError.
//   - resp.Ok == false, other error → writes errorBody JSON to out,
//     returns exitTransportError.
func handleResponse(resp socketResponse, out io.Writer) int {
	if resp.Ok {
		// Success: write the result JSON to stdout.
		data, err := json.Marshal(resp.Result)
		if err != nil {
			return exitTransportError
		}
		_, _ = fmt.Fprintf(out, "%s\n", data) //nolint:errcheck // write to stdout; unactionable
		return exitSuccess
	}

	// Error path: write the error body to stdout (not stderr) per PL-028c.
	body := errorBody{
		Code:    resp.ErrorCode,
		Message: resp.Error,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return exitTransportError
	}
	_, _ = fmt.Fprintf(out, "%s\n", data) //nolint:errcheck // write to stdout; unactionable

	// Classify the error code.
	if resp.ErrorCode >= validationErrorCodeMin && resp.ErrorCode <= validationErrorCodeMax {
		return exitValidationError
	}
	return exitTransportError
}

// isSocketAbsent reports whether err is a "no such file or directory" error —
// indicating the daemon socket file does not exist.
func isSocketAbsent(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.PathError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.ENOENT)
		}
		return errors.Is(opErr.Err, syscall.ENOENT)
	}
	return errors.Is(err, syscall.ENOENT)
}

// isConnectionRefused reports whether err is a connection-refused error —
// indicating the daemon socket file exists but no listener is bound.
func isConnectionRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.ECONNREFUSED)
		}
		return errors.Is(opErr.Err, syscall.ECONNREFUSED)
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}
