package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// SocketRequest is a single request sent by an agent subprocess over the
// Unix socket. One request is sent per connection (simple request/response
// model at MVH).
//
// The "op" field selects the operation:
//   - "emit-outcome": agent reports a completed run's outcome.
//   - "claim-next": agent asks the daemon for the next ready bead.
//
// Spec ref: MVH_ROADMAP row #5.
type SocketRequest struct {
	// Op selects the operation: "emit-outcome" or "claim-next".
	Op string `json:"op"`

	// RunID identifies the run (present for "emit-outcome").
	RunID string `json:"run_id,omitempty"`

	// BeadID is the bead being completed (present for "emit-outcome").
	BeadID string `json:"bead_id,omitempty"`

	// Outcome carries the outcome payload (present for "emit-outcome").
	// TODO(hk-qdxw7): replace json.RawMessage with typed OutcomeRecord once
	// core/ type lands.
	Outcome json.RawMessage `json:"outcome,omitempty"`

	// Role is the agent role requesting a claim (present for "claim-next").
	Role string `json:"role,omitempty"`
}

// SocketResponse is the daemon's reply to a SocketRequest.
//
// On success: Ok=true, Result carries the op-specific payload.
// On failure: Ok=false, Error carries a human-readable message.
//
// Spec ref: MVH_ROADMAP row #5.
type SocketResponse struct {
	// Ok is true when the operation succeeded.
	Ok bool `json:"ok"`

	// Result carries the op-specific response payload on success.
	Result json.RawMessage `json:"result,omitempty"`

	// Error carries a human-readable error message on failure.
	Error string `json:"error,omitempty"`
}

// OutcomeRequest is the parsed body of an "emit-outcome" request.
type OutcomeRequest struct {
	// RunID identifies the run reporting its outcome.
	RunID string

	// BeadID is the bead being completed.
	BeadID string

	// Outcome is the raw outcome payload.
	Outcome json.RawMessage
}

// RequestHandler is the interface the daemon registers to process socket
// requests from agent subprocesses.
//
// The concrete implementation that talks to the bus and brcli is a
// follow-up bead (DO NOT wire here — expose only the listener).
//
// Spec ref: MVH_ROADMAP row #5.
type RequestHandler interface {
	// EmitOutcome handles an "emit-outcome" request from an agent subprocess.
	// The returned json.RawMessage is serialised into SocketResponse.Result.
	EmitOutcome(ctx context.Context, req OutcomeRequest) (json.RawMessage, error)

	// ClaimNext handles a "claim-next" request from an agent subprocess.
	// role is the agent role requesting the next bead.
	// The returned json.RawMessage is serialised into SocketResponse.Result.
	ClaimNext(ctx context.Context, role string) (json.RawMessage, error)
}

// RunSocketListener binds a Unix-domain socket at sockPath, sets its
// permissions to 0700, and accepts connections until ctx is cancelled.
// Each connection is handled in its own goroutine: one JSON request is
// read, dispatched to h, and one JSON response is written before the
// connection is closed.
//
// Stale socket removal: if a file already exists at sockPath (e.g. from a
// previously crashed daemon), it is removed before binding so that the new
// listen call succeeds. A live socket at sockPath will cause
// (&net.ListenConfig{}).Listen to return EADDRINUSE.
//
// RunSocketListener returns nil when ctx is cancelled and the listener
// closes cleanly. It returns a non-nil error only on bind failure.
//
// Spec ref: MVH_ROADMAP row #5 — "Production Unix socket listener …
// request loop for emit-outcome / claim-next from agent subprocesses."
func RunSocketListener(ctx context.Context, sockPath string, h RequestHandler) error {
	// Remove a stale socket file left by a previously crashed daemon.
	// Ignore the error: if the file doesn't exist, Remove returns an error
	// which we discard intentionally.
	_ = os.Remove(sockPath) //nolint:errcheck // stale-removal: not-exist errors are expected and harmless

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon: RunSocketListener: listen unix %q: %w", sockPath, err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // cleanup error unactionable

	// Restrict access to the daemon's own uid (PL-003 spirit: operator-local
	// socket). The bead spec says 0700; PL-003 in lifecycle says 0600 for its
	// own binding. The bead body wins per implementer-protocol path-discrepancy
	// rule.
	if err := os.Chmod(sockPath, 0o700); err != nil {
		return fmt.Errorf("daemon: RunSocketListener: chmod 0700 %q: %w", sockPath, err)
	}

	// Close the listener when ctx is cancelled so Accept unblocks.
	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck // cleanup error unactionable
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// ctx cancellation causes ln.Close(), which makes Accept return an
			// error. Treat any Accept error after ctx cancellation as clean exit.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("daemon: RunSocketListener: accept: %w", err)
		}
		go handleSocketConn(ctx, conn, h)
	}
}

// handleSocketConn reads one JSON SocketRequest from conn, dispatches it to h,
// writes one JSON SocketResponse, and closes the connection.
func handleSocketConn(ctx context.Context, conn net.Conn, h RequestHandler) {
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	var req SocketRequest
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		writeSocketResponse(conn, SocketResponse{
			Ok:    false,
			Error: fmt.Sprintf("daemon: decode request: %v", err),
		})
		return
	}

	var resp SocketResponse
	switch req.Op {
	case "emit-outcome":
		result, err := h.EmitOutcome(ctx, OutcomeRequest{
			RunID:   req.RunID,
			BeadID:  req.BeadID,
			Outcome: req.Outcome,
		})
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "claim-next":
		result, err := h.ClaimNext(ctx, req.Role)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	default:
		resp = SocketResponse{
			Ok:    false,
			Error: fmt.Sprintf("daemon: unknown op %q", req.Op),
		}
	}

	writeSocketResponse(conn, resp)
}

// writeSocketResponse encodes resp as JSON and writes it to conn.
// Write errors are silently discarded (the connection is about to close).
func writeSocketResponse(conn net.Conn, resp SocketResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Marshal of SocketResponse with only string/json.RawMessage fields
		// should never fail; log nothing, connection closes.
		return
	}
	_, _ = conn.Write(data) //nolint:errcheck // write error unactionable; connection closing
}
