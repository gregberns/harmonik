package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/queue"
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

	// Queue is the optional named-queue scope for operator-pause and
	// operator-resume ops (NQ-C1 hk-tigaf.6). When non-empty, the operation
	// is scoped to the named queue; when absent/empty, the operation is global.
	Queue string `json:"queue,omitempty"`

	// Payload carries op-specific request data (present for "comms-send").
	// The shape of Payload depends on the op; for comms-send it is a CommsSendRequest.
	// Bead ref: hk-nbrmf (comms-send T4).
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SocketResponse is the daemon's reply to a SocketRequest.
//
// On success: Ok=true, Result carries the op-specific payload.
// On failure: Ok=false, Error carries a human-readable message.
//
// For queue operations that produce typed JSON-RPC validation errors per
// specs/queue-model.md §6.11a QM-029b, ErrorCode carries the numeric error
// code from the -32010..-32019 range. ErrorCode is zero for all non-queue
// operations and for queue operations that succeed or fail with an internal
// (non-validation) error.
//
// Spec ref: MVH_ROADMAP row #5.
// Spec ref: specs/process-lifecycle.md §4.4 PL-003a (queue method error codes).
type SocketResponse struct {
	// Ok is true when the operation succeeded.
	Ok bool `json:"ok"`

	// Result carries the op-specific response payload on success.
	Result json.RawMessage `json:"result,omitempty"`

	// Error carries a human-readable error message on failure.
	Error string `json:"error,omitempty"`

	// ErrorCode carries the JSON-RPC error code for typed queue validation
	// errors (specs/queue-model.md §6.11a QM-029b). Zero for all other errors.
	ErrorCode int `json:"error_code,omitempty"`
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

// QueueHandler is the interface the daemon registers to process queue
// JSON-RPC requests received over the Unix socket.
//
// Each method receives the raw JSON bytes of the request params and returns
// the JSON-encoded response bytes plus a typed *queue.RPCError on failure.
// This keeps the socket transport layer decoupled from the queue request/
// response RECORD types; the concrete adapter (queueHandlerAdapter in
// socket.go) handles encode/decode.
//
// A nil QueueHandler causes all queue-* op requests to return an error response.
//
// Spec ref: specs/queue-model.md §2.10, §6; specs/process-lifecycle.md §4.4 PL-003a.
type QueueHandler interface {
	// HandleQueueSubmit dispatches a queue-submit request.
	HandleQueueSubmit(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)

	// HandleQueueAppend dispatches a queue-append request.
	HandleQueueAppend(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)

	// HandleQueueStatus dispatches a queue-status request.
	// params carries an optional QueueStatusRequest JSON payload (name / queue_id
	// filter). When nil or empty the handler defaults to the "main" queue.
	//
	// Bead ref: hk-1k5as.
	HandleQueueStatus(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)

	// HandleQueueDryRun dispatches a queue-dry-run request.
	HandleQueueDryRun(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)

	// HandleQueueList dispatches a queue-list request.
	//
	// Bead ref: hk-tigaf.8.
	HandleQueueList(ctx context.Context) (json.RawMessage, *queue.RPCError)

	// HandleQueueSetConcurrency dispatches a queue-set-concurrency request.
	// Validates n >= 1 and updates the runtime ceiling atomically. Returns the
	// old and new ceiling values.
	//
	// Bead ref: hk-ohiaf.
	HandleQueueSetConcurrency(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)
}

// noopRequestHandler is a minimal RequestHandler that rejects every request
// with a clear error. It is used at MVH where the real claim-next / emit-outcome
// wiring is deferred to a follow-up bead. Hook-relay envelopes never reach
// RequestHandler (they are dispatched via HookRelayHandler), so this stub has
// no impact on the hook-relay path.
//
// Spec ref: MVH_ROADMAP row #5.
type noopRequestHandler struct{}

func (n *noopRequestHandler) EmitOutcome(_ context.Context, _ OutcomeRequest) (json.RawMessage, error) {
	return nil, errors.New("daemon: RequestHandler not wired at MVH")
}

func (n *noopRequestHandler) ClaimNext(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, errors.New("daemon: RequestHandler not wired at MVH")
}

// errLiveDaemon is returned by removeStaleSocket when a dial to the socket
// path succeeds, indicating another daemon process is actively listening.
// RunSocketListener surfaces this as a startup error so the caller can exit
// with the appropriate exit code.
var errLiveDaemon = errors.New("daemon: live daemon already listening on socket")

// removeStaleSocket checks whether the socket file at sockPath is stale (i.e.
// no process is listening) and removes it so the caller can rebind.
//
// Decision logic:
//   - If no file exists at sockPath: nothing to do (return nil).
//   - If a dial to sockPath succeeds: a live daemon is using the socket.
//     Return errLiveDaemon so the caller can abort startup with exit code 6.
//   - If the dial returns ECONNREFUSED, timeout, or any other connection error:
//     the socket file is stale. Remove it and return nil.
//
// The dial timeout is 100 ms — sufficient for a local Unix socket handshake
// on any supported platform.
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-003 (socket exclusivity, Gap-2).
func removeStaleSocket(sockPath string) error {
	// Fast path: no file at all, nothing to remove.
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		return nil
	}

	// Probe: attempt a connection with a short timeout.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer probeCancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "unix", sockPath)
	if err == nil {
		// Dial succeeded → a live daemon owns this socket.
		_ = conn.Close() //nolint:errcheck // probe conn; close error unactionable
		return errLiveDaemon
	}
	// Any dial error (ECONNREFUSED, context deadline, no-such-file, etc.)
	// indicates the socket is stale. Remove and proceed.
	if removeErr := os.Remove(sockPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("daemon: removeStaleSocket: remove %q: %w", sockPath, removeErr)
	}
	return nil
}

// RunSocketListener binds a Unix-domain socket at sockPath, sets its
// permissions to 0600, and accepts connections until ctx is cancelled.
// Each connection is handled in its own goroutine: one JSON request is
// read, dispatched to h, hr, or qh, and one JSON response is written before
// the connection is closed.
//
// Stale socket removal: if a file already exists at sockPath, RunSocketListener
// probes it with a 100 ms dial. If the dial succeeds, another daemon is live
// and RunSocketListener returns an error wrapping errLiveDaemon. If the dial
// fails (ECONNREFUSED, timeout, etc.) the socket is stale — it is removed and
// binding proceeds normally.
//
// RunSocketListener returns nil when ctx is cancelled and the listener
// closes cleanly. It returns a non-nil error only on bind failure or when a
// live daemon is detected at sockPath.
//
// Dispatch rules:
//   - Non-empty "type" field → hook-relay envelope, dispatched to hr
//     (HookRelayHandler); hr may be nil (bad_envelope response).
//   - Op ∈ {"queue-submit","queue-append","queue-status","queue-dry-run"}
//     → queue request, dispatched to qh (QueueHandler); qh may be nil
//     (error response with code -32099).
//   - All other ops → dispatched to h (RequestHandler).
//
// Spec ref: MVH_ROADMAP row #5 — "Production Unix socket listener …
// request loop for emit-outcome / claim-next from agent subprocesses."
// Spec ref: specs/claude-hook-bridge.md §4.6 CHB-015, §4.10 CHB-025.
// Spec ref: specs/process-lifecycle.md §4.4 PL-003a (queue method set).
func RunSocketListener(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, qh ...QueueHandler) error {
	return RunSocketListenerWithSubscribe(ctx, sockPath, h, hr, nil, qh...)
}

// RunSocketListenerWithSubscribe is RunSocketListener with an optional
// SubscribeHandler. When sub is nil, "subscribe" ops return an error response.
func RunSocketListenerWithSubscribe(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, qh ...QueueHandler) error {
	return RunSocketListenerFull(ctx, sockPath, h, hr, sub, nil, nil, qh...)
}

// RunSocketListenerFull is RunSocketListenerWithSubscribe with additional
// OperatorControlHandler and CommsSendHandler parameters. When oh is nil,
// operator-pause/resume ops return an error response. When ch is nil,
// comms-send ops return an error response.
//
// Bead ref: hk-ry8q1 (operator control), hk-nbrmf (comms-send T4).
func RunSocketListenerFull(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, qh ...QueueHandler) error {
	return RunSocketListenerWithCrew(ctx, sockPath, h, hr, sub, oh, ch, nil, qh...)
}

// RunSocketListenerWithCrew is RunSocketListenerFull with an additional
// CrewHandler parameter. When crewh is nil, crew-start/crew-stop ops return an
// error response.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1.
// Bead ref: hk-5tg5o (C2 daemon handler).
func RunSocketListenerWithCrew(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler, qh ...QueueHandler) error {
	if err := removeStaleSocket(sockPath); err != nil {
		return fmt.Errorf("daemon: RunSocketListener: stale-socket check: %w", err)
	}

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

	var queueHandler QueueHandler
	if len(qh) > 0 {
		queueHandler = qh[0]
	}

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
		go handleSocketConn(ctx, conn, h, hr, queueHandler, sub, oh, ch, crewh)
	}
}

// handleSocketConn reads one JSON message from conn and dispatches it to the
// appropriate handler:
//   - If the decoded JSON has a non-empty "type" field → hook-relay envelope,
//     dispatched to hr (HookRelayHandler); response is a hookRelayAckMsg.
//   - If op ∈ {"queue-submit","queue-append","queue-status","queue-dry-run"}
//     → queue request, dispatched to qh (QueueHandler); response is a
//     SocketResponse with optional ErrorCode per QM-029b.
//   - Otherwise → SocketRequest, dispatched to h (RequestHandler); response is
//     a SocketResponse.
//
// CHB-027: if the relay sent zero complete lines (abrupt EOF before the '\n'
// terminator), json.Decoder.Decode returns an error and the connection is dropped
// with no response after writing a bad_envelope ack — the relay will have exited
// already in this case, so the write is best-effort.
func handleSocketConn(ctx context.Context, conn net.Conn, h RequestHandler, hr HookRelayHandler, qh QueueHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler) {
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

	// -----------------------------------------------------------------------
	// Queue control-surface methods (specs/process-lifecycle.md §4.4 PL-003a)
	// -----------------------------------------------------------------------

	case "queue-submit":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueSubmit(ctx, reEncoded)
		})

	case "queue-append":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueAppend(ctx, reEncoded)
		})

	case "queue-status":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueStatus(ctx, reEncoded)
		})

	case "queue-dry-run":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueDryRun(ctx, reEncoded)
		})

	case "queue-list":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueList(ctx)
		})

	case "queue-set-concurrency":
		resp = handleQueueOp(ctx, qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
			return h.HandleQueueSetConcurrency(ctx, reEncoded)
		})

	case "subscribe":
		// Long-running op: streams NDJSON events on conn until the client
		// disconnects or ctx is cancelled. No SocketResponse is written —
		// the caller's connection IS the stream.
		//
		// Spec ref: operator-nfr.md §4.9 ON-055 (subscribe is read-only observation).
		// Bead ref: hk-6ynv4.
		if sub == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: SubscribeHandler not registered"}
			break
		}
		var subReq SubscribeRequest
		if err := json.Unmarshal(reEncoded, &subReq); err != nil {
			resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: decode subscribe request: %v", err)}
			break
		}
		// Validate since_event_id format when provided. Must be a parseable
		// UUID (expected UUIDv7). Replay is implemented in HandleSubscribe
		// per hk-a5sil; only format validation lives here.
		if subReq.SinceEventID != "" {
			if _, parseErr := uuid.Parse(subReq.SinceEventID); parseErr != nil {
				resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: since_event_id %q is not a valid UUID: %v", subReq.SinceEventID, parseErr)}
				break
			}
		}
		sub.HandleSubscribe(ctx, conn, subReq)
		return // suppress SocketResponse write; conn is closed by defer

	// -----------------------------------------------------------------------
	// Agent-comms ops (agent-comms spec §2.1 C2, bead hk-nbrmf)
	// -----------------------------------------------------------------------

	case "comms-send":
		if ch == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: CommsSendHandler not registered"}
			break
		}
		result, err := ch.HandleCommsSend(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "comms-presence":
		// Type-assert ch to CommsPresenceHandler. In production, *commsSendHandlerImpl
		// implements both CommsSendHandler and CommsPresenceHandler (hk-7t27s T10).
		cp, ok := ch.(CommsPresenceHandler)
		if !ok || cp == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: CommsPresenceHandler not registered"}
			break
		}
		result, err := cp.HandleCommsPresence(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "comms-recv":
		// Type-assert ch to CommsRecvHandler. In production, *commsSendHandlerImpl
		// implements CommsSendHandler, CommsPresenceHandler, and CommsRecvHandler
		// (hk-nnwaa T8). The recv deps are set via SetRecvDeps after handler creation.
		cr, ok := ch.(CommsRecvHandler)
		if !ok || cr == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: CommsRecvHandler not registered"}
			break
		}
		result, err := cr.HandleCommsRecv(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	// -----------------------------------------------------------------------
	// hitl-decisions agent-side emit ops (hitl-decisions SPEC §2, bead hk-xz9 K2)
	// -----------------------------------------------------------------------

	case "decisions-raise":
		// Type-assert ch to DecisionsHandler. In production, *commsSendHandlerImpl
		// implements DecisionsHandler (decisionshandler_xz9.go) alongside the
		// comms handlers, so it rides the same ch value — no new socket-listener
		// parameter. K4 (list/answer) and K5 (reaper) are separate later beads.
		dh, ok := ch.(DecisionsHandler)
		if !ok || dh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: DecisionsHandler not registered"}
			break
		}
		result, err := dh.HandleDecisionsRaise(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "decisions-withdraw":
		dh, ok := ch.(DecisionsHandler)
		if !ok || dh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: DecisionsHandler not registered"}
			break
		}
		result, err := dh.HandleDecisionsWithdraw(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	// -----------------------------------------------------------------------
	// hitl-decisions operator-side ops (hitl-decisions SPEC §2, bead hk-kba K4)
	//   decisions-list   → pure read of the K3 open-decision projection (S6).
	//   decisions-answer → emit decision_resolved (N7 option check, N3 no-op).
	// Both ride the same DecisionsHandler value as the K2 emit ops.
	// -----------------------------------------------------------------------

	case "decisions-list":
		dh, ok := ch.(DecisionsHandler)
		if !ok || dh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: DecisionsHandler not registered"}
			break
		}
		result, err := dh.HandleDecisionsList(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "decisions-answer":
		dh, ok := ch.(DecisionsHandler)
		if !ok || dh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: DecisionsHandler not registered"}
			break
		}
		result, err := dh.HandleDecisionsAnswer(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: err.Error()}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	// -----------------------------------------------------------------------
	// Operator control ops (specs/operator-nfr.md §4.3 ON-007–ON-010)
	// -----------------------------------------------------------------------

	case "operator-pause":
		if oh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: OperatorControlHandler not registered"}
			break
		}
		if err := oh.HandleOperatorPause(ctx, req.Queue); err != nil {
			resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: operator-pause: %v", err)}
		} else {
			resp = SocketResponse{Ok: true}
		}

	case "operator-resume":
		if oh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: OperatorControlHandler not registered"}
			break
		}
		if err := oh.HandleOperatorResume(ctx, req.Queue); err != nil {
			resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: operator-resume: %v", err)}
		} else {
			resp = SocketResponse{Ok: true}
		}

	case "crew-start":
		// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1–§3.4.
		// Bead ref: hk-5tg5o.
		if crewh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: CrewHandler not registered"}
			break
		}
		result, err := crewh.HandleCrewStart(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: crew-start: %v", err)}
		} else {
			resp = SocketResponse{Ok: true, Result: result}
		}

	case "crew-stop":
		// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.5.
		// Bead ref: hk-5tg5o.
		if crewh == nil {
			resp = SocketResponse{Ok: false, Error: "daemon: CrewHandler not registered"}
			break
		}
		result, err := crewh.HandleCrewStop(ctx, req.Payload)
		if err != nil {
			resp = SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: crew-stop: %v", err)}
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

// handleQueueOp dispatches a single queue operation to qh and converts the
// result to a SocketResponse. If qh is nil, returns an error response.
//
// The fn closure calls the appropriate QueueHandler method; the closure
// receives the handler so callers do not repeat the nil check.
//
// Spec ref: specs/queue-model.md §6.11a QM-029b (error codes in ErrorCode field).
func handleQueueOp(_ context.Context, qh QueueHandler, fn func(QueueHandler) (json.RawMessage, *queue.RPCError)) SocketResponse {
	if qh == nil {
		return SocketResponse{
			Ok:        false,
			ErrorCode: -32099,
			Error:     "daemon: QueueHandler not registered",
		}
	}
	result, rpcErr := fn(qh)
	if rpcErr != nil {
		return SocketResponse{
			Ok:        false,
			ErrorCode: rpcErr.Code,
			Error:     rpcErr.Message,
		}
	}
	return SocketResponse{Ok: true, Result: result}
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
