// Package cli implements the client-side helpers for the hk queue subcommand
// family. Each subcommand opens daemon.sock, sends a JSON-RPC-shaped request
// over the Unix socket (the same SocketRequest / SocketResponse protocol used
// by agent subprocesses — see internal/daemon/socket.go), reads the response,
// and exits with the exit code specified by PL-008a / PL-028c.
//
// Exit-code contract (applies to all four subcommands):
//
//	 0   — success; response is written to stdout (human-readable by default,
//	         JSON with --json or --format json).
//	 1   — validation error (any QueueValidationReason per QM-029b); the full
//	         error body is written to stdout.
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
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
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
const (
	validationErrorCodeMin = -32019
	validationErrorCodeMax = -32010
)

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
// earlyExit is -1 on a clean response (even if resp.Ok is false), so the
// caller processes resp. It is exitDaemonDown if the socket is absent or the
// connection is refused, and exitTransportError for any other dial or I/O error.
func sendRequest(ctx context.Context, harmonikDir string, payload []byte) (resp socketResponse, earlyExit int) {
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
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "queue/cli: close daemon conn", "err", closeErr, "path", sockPath)
		}
	}()

	// Write request.
	if _, writeErr := conn.Write(payload); writeErr != nil {
		return socketResponse{}, exitTransportError
	}

	// Half-close the write side so the server's json.Decoder can detect EOF.
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // cleanup error unactionable
	}

	// Read response. A truncated frame (io.EOF / io.ErrUnexpectedEOF) and a
	// malformed one are both protocol failures, so they share an exit code.
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		return socketResponse{}, exitTransportError
	}

	return resp, -1 // -1 = no early exit; caller processes resp
}

// handleResponse converts a socketResponse to an exit code.
//
// When outputJSON is true the raw JSON is written verbatim (machine-readable).
// When false the renderFn is called to produce human-readable output.
//
// On error the error body is written as plain text (human-readable) when
// outputJSON is false, and as JSON when outputJSON is true — both to stdout
// per PL-028c.
//
//   - resp.Ok == true  → calls renderFn (or writes JSON), returns exitSuccess.
//   - resp.Ok == false, validation error code → writes error, returns exitValidationError.
//   - resp.Ok == false, other error → writes error, returns exitTransportError.
func handleResponse(resp socketResponse, out io.Writer, outputJSON bool, renderFn func(result json.RawMessage, out io.Writer) int) int {
	p := newPrinter(out)
	if resp.Ok {
		if outputJSON {
			data, err := json.Marshal(resp.Result)
			if err != nil {
				return exitTransportError
			}
			p.printf("%s\n", data)
			if p.failed() {
				return exitTransportError
			}
			return exitSuccess
		}
		return renderFn(resp.Result, out)
	}

	// Error path: write the error body to stdout (not stderr) per PL-028c.
	if outputJSON {
		body := errorBody{
			Code:    resp.ErrorCode,
			Message: resp.Error,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return exitTransportError
		}
		p.printf("%s\n", data)
	} else {
		p.printf("error: %s (code %d)\n", resp.Error, resp.ErrorCode)
	}
	if p.failed() {
		return exitTransportError
	}

	// Classify the error code.
	if resp.ErrorCode >= validationErrorCodeMin && resp.ErrorCode <= validationErrorCodeMax {
		return exitValidationError
	}
	return exitTransportError
}

// ---------------------------------------------------------------------------
// Human-readable renderers (one per queue subcommand)
// ---------------------------------------------------------------------------

// renderExit maps a finished renderer's printer to its exit code. A stdout
// write that failed part-way through means the caller received a TRUNCATED
// report, so it must not be reported as success.
func renderExit(p *printer) int {
	if p.failed() {
		return exitTransportError
	}
	return exitSuccess
}

// renderQueueStatusText prints a human-readable summary of a QueueStatusResponse.
// The result bytes are the raw JSON from the daemon (resp.Result).
func renderQueueStatusText(result json.RawMessage, out io.Writer) int {
	var envelope struct {
		Queue *struct {
			QueueID string `json:"queue_id"`
			Status  string `json:"status"`
			Groups  []struct {
				Status string `json:"status"`
				Items  []struct {
					BeadID string `json:"bead_id"`
					Status string `json:"status"`
				} `json:"items"`
			} `json:"groups"`
		} `json:"queue"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &envelope); err != nil {
		// Fallback: print raw JSON on parse failure.
		p.printf("%s\n", result)
		return renderExit(p)
	}

	if envelope.Queue == nil {
		p.println("(no queue active)")
		return renderExit(p)
	}

	q := envelope.Queue
	p.printf("queue:    %s\n", q.Status)
	p.printf("queue_id: %s\n", q.QueueID)
	if len(q.Groups) > 0 {
		p.printf("groups:   %d\n", len(q.Groups))
		for gi, g := range q.Groups {
			p.printf("  group %d  [%s]  %d item(s)\n", gi, g.Status, len(g.Items))
			for _, item := range g.Items {
				p.printf("    %-20s  %s\n", item.BeadID, item.Status)
			}
		}
	}
	return renderExit(p)
}

// renderQueueSubmitText prints a human-readable confirmation of a QueueSubmitResponse.
func renderQueueSubmitText(result json.RawMessage, out io.Writer) int {
	var resp struct {
		QueueID    string `json:"queue_id"`
		Status     string `json:"status"`
		GroupCount int    `json:"group_count"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &resp); err != nil {
		p.printf("%s\n", result)
		return renderExit(p)
	}
	p.printf("submitted: queue_id=%s\n", resp.QueueID)
	p.printf("status:    %s\n", resp.Status)
	if resp.GroupCount > 0 {
		p.printf("groups:    %d\n", resp.GroupCount)
	}
	return renderExit(p)
}

// renderQueueAppendText prints a human-readable confirmation of a QueueAppendResponse.
func renderQueueAppendText(result json.RawMessage, out io.Writer) int {
	var resp struct {
		AppendedCount  int   `json:"appended_count"`
		NewTailIndices []int `json:"new_tail_indices"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &resp); err != nil {
		p.printf("%s\n", result)
		return renderExit(p)
	}
	p.printf("appended: %d bead(s)\n", resp.AppendedCount)
	if len(resp.NewTailIndices) > 0 {
		indices := make([]string, len(resp.NewTailIndices))
		for i, idx := range resp.NewTailIndices {
			indices[i] = strconv.Itoa(idx)
		}
		p.printf("indices:  %s\n", strings.Join(indices, ", "))
	}
	return renderExit(p)
}

// renderQueueDryRunText prints a human-readable validation summary of a QueueDryRunResponse.
func renderQueueDryRunText(result json.RawMessage, out io.Writer) int {
	var resp struct {
		ResolvedQueue struct {
			QueueID string `json:"queue_id"`
			Groups  []struct {
				Items []struct {
					BeadID string `json:"bead_id"`
				} `json:"items"`
			} `json:"groups"`
		} `json:"resolved_queue"`
		LedgerDepNotices []struct {
			BeadID        string `json:"bead_id"`
			BlockerBeadID string `json:"blocker_bead_id"`
		} `json:"ledger_dep_notices"`
		ParallelismNarrowed bool `json:"parallelism_narrowed"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &resp); err != nil {
		p.printf("%s\n", result)
		return renderExit(p)
	}

	// Count total items across groups.
	totalItems := 0
	for _, g := range resp.ResolvedQueue.Groups {
		totalItems += len(g.Items)
	}

	p.printf("dry-run:    OK\n")
	p.printf("items:      %d\n", totalItems)
	p.printf("validation: passed — queue would be accepted\n")
	if resp.ParallelismNarrowed {
		p.printf("warning:    parallelism narrowed (%d ledger-dep notice(s))\n", len(resp.LedgerDepNotices))
		for _, n := range resp.LedgerDepNotices {
			p.printf("  %s blocked by %s\n", n.BeadID, n.BlockerBeadID)
		}
	}
	return renderExit(p)
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
