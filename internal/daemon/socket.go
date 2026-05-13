package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// HookRelayHandler is the interface the daemon registers to process hook-relay
// messages received from harmonik hook-relay subprocesses over the Unix socket.
//
// Hook-relay messages carry a "type" field (e.g., "outcome_emitted") rather
// than the "op" field of SocketRequest. The socket acceptor dispatches to this
// interface when a connection's JSON payload has a non-empty "type" field.
//
// Spec ref: specs/claude-hook-bridge.md §4.6 CHB-015, §6.1 HookRelayMessage,
// §6.2 HookRelayAck, §4.10 CHB-025.
type HookRelayHandler interface {
	// HandleHookRelay dispatches a single hookRelayEnvelope and returns the
	// ACK/typed-error to be written back to the relay connection per §6.2.
	HandleHookRelay(env hookRelayEnvelope) hookRelayAckMsg
}

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
// permissions to 0600, and accepts connections until ctx is cancelled.
// Each connection is handled in its own goroutine: one JSON request is
// read, dispatched to h or hr, and one JSON response is written before the
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
// Dispatch: if the connection's JSON payload contains a non-empty "type" field,
// the message is routed to hr (HookRelayHandler) as a hookRelayEnvelope; otherwise
// it is routed to h (RequestHandler) as a SocketRequest. hr may be nil, in which
// case hook-relay messages are rejected with a bad_envelope response.
//
// Spec ref: MVH_ROADMAP row #5 — "Production Unix socket listener …
// request loop for emit-outcome / claim-next from agent subprocesses."
// Spec ref: specs/claude-hook-bridge.md §4.6 CHB-015, §4.10 CHB-025.
func RunSocketListener(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler) error {
	// Remove a stale socket file left by a previously crashed daemon.
	// Ignore the error: if the file doesn't exist, Remove returns an error
	// which we discard intentionally.
	_ = os.Remove(sockPath) //nolint:errcheck // stale-removal: not-exist errors are expected and harmless

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon: RunSocketListener: listen unix %q: %w", sockPath, err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // cleanup error unactionable

	// Restrict access to the daemon's own uid per specs/process-lifecycle.md PL-003.
	if err := os.Chmod(sockPath, 0o600); err != nil {
		return fmt.Errorf("daemon: RunSocketListener: chmod 0600 %q: %w", sockPath, err)
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
		go handleSocketConn(ctx, conn, h, hr)
	}
}

// handleSocketConn reads one JSON message from conn and dispatches it to the
// appropriate handler:
//   - If the decoded JSON has a non-empty "type" field → hook-relay envelope,
//     dispatched to hr (HookRelayHandler); response is a hookRelayAckMsg.
//   - Otherwise → SocketRequest, dispatched to h (RequestHandler); response is
//     a SocketResponse.
//
// CHB-027: if the relay sent zero complete lines (abrupt EOF before the '\n'
// terminator), json.Decoder.Decode returns an error and the connection is dropped
// with no response after writing a bad_envelope ack — the relay will have exited
// already in this case, so the write is best-effort.
func handleSocketConn(ctx context.Context, conn net.Conn, h RequestHandler, hr HookRelayHandler) {
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	// Decode into a raw map first to detect the message format (type vs op).
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&raw); err != nil {
		// CHB-027: orphan connection / partial write. Drop silently; best-effort
		// ack so relay can observe the error if it is still alive.
		writeHookRelayAck(conn, hookRelayAckMsg{
			Status: "bad_envelope",
			Reason: fmt.Sprintf("decode: %v", err),
		})
		return
	}

	// Distinguish hook-relay envelope from SocketRequest by the "type" field.
	if typeRaw, hasType := raw["type"]; hasType && len(typeRaw) > 2 {
		// Looks like a hookRelayEnvelope (has non-empty "type").
		// Re-marshal the raw map back to JSON so we can Unmarshal into the typed struct.
		reEncoded, encErr := json.Marshal(raw)
		if encErr != nil {
			writeHookRelayAck(conn, hookRelayAckMsg{Status: "bad_envelope", Reason: "re-encode failed"})
			return
		}
		var env hookRelayEnvelope
		if err := json.Unmarshal(reEncoded, &env); err != nil {
			writeHookRelayAck(conn, hookRelayAckMsg{Status: "bad_envelope", Reason: fmt.Sprintf("envelope decode: %v", err)})
			return
		}
		if hr == nil {
			writeHookRelayAck(conn, hookRelayAckMsg{Status: "bad_envelope", Reason: "no hook-relay handler registered"})
			return
		}
		ack := hr.HandleHookRelay(env)
		writeHookRelayAck(conn, ack)
		return
	}

	// SocketRequest path (op-based protocol).
	reEncoded, encErr := json.Marshal(raw)
	if encErr != nil {
		writeSocketResponse(conn, SocketResponse{Ok: false, Error: "re-encode failed"})
		return
	}
	var req SocketRequest
	if err := json.Unmarshal(reEncoded, &req); err != nil {
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

// writeHookRelayAck serialises ack as NDJSON and writes it to conn.
// Write errors are silently discarded (connection is about to close).
func writeHookRelayAck(conn net.Conn, ack hookRelayAckMsg) {
	data, err := json.Marshal(ack)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(data, '\n')) //nolint:errcheck // write error unactionable; connection closing
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
