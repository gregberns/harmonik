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

	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
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

	// PromoteTo carries the optional --promote-to target for the veto_verdict op
	// (RC-027 operator verdict-override; CLI surface: harmonik veto-verdict
	// --promote-to escalate-to-human). Empty for confirm_verdict and for a plain
	// veto. See specs/reconciliation/spec.md §4.5 RC-027.
	PromoteTo string `json:"promote_to,omitempty"`
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

	// HandleWorkerSetEnabled dispatches a worker-set-enabled request — the live
	// `harmonik worker enable/disable` toggle. Flips the named worker's enabled
	// state in the daemon's live worker registry (no restart) and returns the
	// resolved worker name + new state.
	//
	// Bead ref: hk-xjbvi.
	HandleWorkerSetEnabled(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)

	// HandleQueueCancel dispatches a queue-cancel request. Archives the named
	// queue's per-queue file on disk (same contract as the daemon-less CLI
	// path) AND reaps the daemon's live in-memory QueueStore slot for that
	// name — the reap is what the daemon-less CLI path cannot do, and its
	// absence is what let a cancelled-but-still-in-memory queue's dispatched
	// item hard-block re-dispatch of the same bead via cross_queue_duplicate
	// (hk-0mmy4).
	//
	// Bead ref: hk-0mmy4.
	HandleQueueCancel(ctx context.Context, params json.RawMessage) (json.RawMessage, *queue.RPCError)
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

// QuiesceOverrideHandler is the interface for manual operator sleep/wake
// commands (CLI surface: harmonik sleep / harmonik wake).
//
// The QuiesceArbiter implements this interface; the socket listener dispatches
// daemon-sleep and daemon-wake ops to it.
//
// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
type QuiesceOverrideHandler interface {
	// HandleDaemonSleep parks all LLM sessions now.
	// When force is false, the drain oracle is consulted first; the call fails
	// when the fleet is not yet drained.  force=true bypasses the oracle.
	HandleDaemonSleep(ctx context.Context, force bool) error

	// HandleDaemonWake wakes sleeping LLM sessions.
	// wakeAll=true wakes every session; agentName wakes one specific session.
	// Returns an error when neither flag is set.
	HandleDaemonWake(ctx context.Context, agentName string, wakeAll bool) error
}

// RunSocketListenerWithCrew is RunSocketListenerFull with an additional
// CrewHandler parameter. When crewh is nil, crew-start/crew-stop ops return an
// error response.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1.
// Bead ref: hk-5tg5o (C2 daemon handler).
func RunSocketListenerWithCrew(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler, qh ...QueueHandler) error {
	return RunSocketListenerWithSleepWake(ctx, sockPath, h, hr, sub, oh, ch, crewh, nil, qh...)
}

// RunSocketListenerWithSleepWake is RunSocketListenerWithCrew with an
// additional QuiesceOverrideHandler parameter. When sleepWakeh is nil,
// daemon-sleep and daemon-wake ops return an error response.
//
// Bead ref: hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake).
func RunSocketListenerWithSleepWake(ctx context.Context, sockPath string, h RequestHandler, hr HookRelayHandler, sub SubscribeHandler, oh OperatorControlHandler, ch CommsSendHandler, crewh CrewHandler, sleepWakeh QuiesceOverrideHandler, qh ...QueueHandler) error {
	return Serve(ctx, sockPath, SocketHandlers{
		Request: h, HookRelay: hr, Queue: firstQueueHandler(qh), Subscribe: sub,
		Operator: oh, Comms: ch, Crew: crewh, SleepWake: sleepWakeh,
	})
}

// SocketHandlers bundles the handler interfaces the daemon injects into the
// socket listener. It replaces the telescoping-constructor chain of positional
// RunSocketListener* wrappers (giant-retirement SR-3). Any field may be nil; the
// corresponding ops return their "… not registered" envelope.
type SocketHandlers struct {
	Request   RequestHandler
	HookRelay HookRelayHandler
	Queue     QueueHandler
	Subscribe SubscribeHandler
	Operator  OperatorControlHandler
	Comms     CommsSendHandler
	Crew      CrewHandler
	SleepWake QuiesceOverrideHandler
	State     StateHandler
	Dashboard DashboardHandler
}

// firstQueueHandler returns the first variadic QueueHandler, or nil. Bridges the
// back-compat variadic wrappers onto SocketHandlers.Queue.
func firstQueueHandler(qh []QueueHandler) QueueHandler {
	if len(qh) > 0 {
		return qh[0]
	}
	return nil
}

// Serve binds a Unix-domain socket at sockPath, sets its permissions to 0600,
// and accepts connections until ctx is cancelled. It is the single shared
// Accept-loop body — the three formerly-duplicated RunSocketListener* bodies
// (WithSleepWake/WithState/WithDashboard) now delegate here (SR-3).
//
// The router is built ONCE (buildSocketRouter), before the Accept loop, from the
// injected handlers — never per-connection (design risk #7). Each connection is
// handled in its own goroutine.
//
// Serve returns nil when ctx is cancelled and the listener closes cleanly. It
// returns a non-nil error only on bind failure or when a live daemon is detected
// at sockPath (errLiveDaemon, wrapped).
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-003 (socket exclusivity), §4.4
// PL-003a (queue method set); specs/claude-hook-bridge.md §4.6 CHB-015.
func Serve(ctx context.Context, sockPath string, hs SocketHandlers) error {
	if err := removeStaleSocket(sockPath); err != nil {
		return fmt.Errorf("daemon: Serve: stale-socket check: %w", err)
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon: Serve: listen unix %q: %w", sockPath, err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // cleanup error unactionable

	// Restrict access to the daemon's own uid per specs/process-lifecycle.md PL-003.
	if err := os.Chmod(sockPath, 0o600); err != nil {
		return fmt.Errorf("daemon: Serve: chmod 0600 %q: %w", sockPath, err)
	}

	// Close the listener when ctx is cancelled so Accept unblocks.
	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck // cleanup error unactionable
	}()

	// Build the router ONCE, before the Accept loop (never per-connection).
	router := buildSocketRouter(&socketDispatch{
		h: hs.Request, qh: hs.Queue, oh: hs.Operator, ch: hs.Comms,
		crewh: hs.Crew, sleepWakeh: hs.SleepWake, stateh: hs.State, dashh: hs.Dashboard,
	})

	for {
		conn, err := ln.Accept()
		if err != nil {
			// ctx cancellation causes ln.Close(), which makes Accept return an
			// error. Treat any Accept error after ctx cancellation as clean exit.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("daemon: Serve: accept: %w", err)
		}
		go handleSocketConn(ctx, conn, hs.HookRelay, hs.Subscribe, router)
	}
}

// handleSocketConn reads one JSON message from conn and dispatches it. The
// giant switch was carved into the pure socketrouter.Router (op→Result lookup)
// plus the daemon-side socketDispatch adapter methods; two response-shape-
// breaking ops stay as daemon pre-branches:
//   - a non-empty "type" field → hook-relay envelope (handleHookRelayEnvelope);
//     response is a hookRelayAckMsg written as NDJSON (+ '\n').
//   - op == "subscribe" → a long-running NDJSON stream (handleSubscribe) that
//     writes no SocketResponse on success.
//
// Every other op routes through router.Dispatch → resultToResponse →
// writeSocketResponse (no trailing newline). router is built once per listener
// body (buildSocketRouter), never per-connection.
//
// CHB-027: if the relay sent zero complete lines (abrupt EOF before the '\n'
// terminator), the raw decode fails and the connection is dropped after a
// best-effort bad_envelope ack — the relay will have exited already, so the
// write is best-effort.
func handleSocketConn(ctx context.Context, conn net.Conn, hr HookRelayHandler, sub SubscribeHandler, router *socketrouter.Router) {
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	raw, err := decodeRawMap(conn)
	if err != nil {
		return
	}
	if socketrouter.Classify(raw) == socketrouter.KindHookRelay {
		handleHookRelayEnvelope(conn, hr, raw) // daemon pre-branch #1
		return
	}
	req, reEncoded, ok := decodeSocketRequest(conn, raw)
	if !ok {
		return
	}
	if req.Op == "subscribe" {
		handleSubscribe(ctx, conn, sub, reEncoded) // daemon pre-branch #2
		return
	}
	res := router.Dispatch(ctx, req.Op, reEncoded)
	writeSocketResponse(conn, resultToResponse(res, req.Op))
}

// decodeRawMap reads one JSON message from conn into a raw map to detect the
// message format (type vs op). On decode failure it writes a bad_envelope ack
// via the hook-relay writer (NDJSON + '\n', preserving the pre-carve behavior;
// the initial raw-decode failure uses the hook-relay writer even for what would
// have been an op request — wire-F3) and returns the error.
func decodeRawMap(conn net.Conn) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&raw); err != nil {
		writeHookRelayAck(conn, hookRelayAckMsg{
			Status: "bad_envelope",
			Reason: fmt.Sprintf("decode: %v", err),
		})
		return nil, err
	}
	return raw, nil
}

// handleHookRelayEnvelope is daemon pre-branch #1: re-marshal the raw map,
// unmarshal into hookRelayEnvelope, nil-guard hr, and dispatch. Every ack is
// written as NDJSON (+ '\n') via writeHookRelayAck.
func handleHookRelayEnvelope(conn net.Conn, hr HookRelayHandler, raw map[string]json.RawMessage) {
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
}

// decodeSocketRequest re-encodes the raw map (byte-identical reEncoded bytes)
// and unmarshals it into a SocketRequest. On failure it writes a SocketResponse
// error envelope (no trailing newline) and returns ok=false. On success it
// returns the parsed request plus the reEncoded bytes for downstream Dispatch.
func decodeSocketRequest(conn net.Conn, raw map[string]json.RawMessage) (SocketRequest, json.RawMessage, bool) {
	reEncoded, encErr := json.Marshal(raw)
	if encErr != nil {
		writeSocketResponse(conn, SocketResponse{Ok: false, Error: "re-encode failed"})
		return SocketRequest{}, nil, false
	}
	var req SocketRequest
	if err := json.Unmarshal(reEncoded, &req); err != nil {
		writeSocketResponse(conn, SocketResponse{
			Ok:    false,
			Error: fmt.Sprintf("daemon: decode request: %v", err),
		})
		return SocketRequest{}, nil, false
	}
	return req, reEncoded, true
}

// handleSubscribe is daemon pre-branch #2: the long-running subscribe op. It
// streams NDJSON events on conn until the client disconnects or ctx is
// cancelled; on success no SocketResponse is written (the connection IS the
// stream, and conn is closed by handleSocketConn's defer). The three error
// sub-paths (nil handler, bad decode, invalid uuid) fall through to
// writeSocketResponse. uuid.Parse validation stays daemon-side (off the router's
// $gostd-only edge).
//
// Spec ref: operator-nfr.md §4.9 ON-055 (subscribe is read-only observation).
// Bead ref: hk-6ynv4, hk-a5sil.
func handleSubscribe(ctx context.Context, conn net.Conn, sub SubscribeHandler, reEncoded json.RawMessage) {
	if sub == nil {
		writeSocketResponse(conn, SocketResponse{Ok: false, Error: "daemon: SubscribeHandler not registered"})
		return
	}
	var subReq SubscribeRequest
	if err := json.Unmarshal(reEncoded, &subReq); err != nil {
		writeSocketResponse(conn, SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: decode subscribe request: %v", err)})
		return
	}
	// Validate since_event_id format when provided. Must be a parseable UUID
	// (expected UUIDv7). Replay is implemented in HandleSubscribe per hk-a5sil;
	// only format validation lives here.
	if subReq.SinceEventID != "" {
		if _, parseErr := uuid.Parse(subReq.SinceEventID); parseErr != nil {
			writeSocketResponse(conn, SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: since_event_id %q is not a valid UUID: %v", subReq.SinceEventID, parseErr)})
			return
		}
	}
	sub.HandleSubscribe(ctx, conn, subReq) // suppress SocketResponse write; conn is closed by defer
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
