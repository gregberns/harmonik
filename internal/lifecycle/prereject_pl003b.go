package lifecycle

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// prereject_pl003b.go — PL-003b pre-ready request rejection gate.
//
// Between socket bind (PL-005 step 3a) and completion of the in-memory model
// build (PL-005 step 7), the daemon MUST reject any emit-outcome, claim-next,
// or other agent-originated request whose run_id is not recognised in the
// daemon's in-memory state, with the typed error
// daemon_not_ready{reason="unknown_run_id"}.
//
// CLI daemon-status requests are exempt (they are ready-detection probes per
// PL-009b).
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.

// agentOriginatedMethods is the set of JSON-RPC method names that are
// agent-originated and must be rejected during the pre-ready window.
// CLI-facing methods (status, pause, resume, stop, upgrade, attach, enqueue,
// list) are exempt.
//
// Spec ref: process-lifecycle.md §4.1 PL-003a — agent-facing method inventory.
var agentOriginatedMethods = map[string]bool{
	"claim-next":      true,
	"emit-outcome":    true,
	"dispatch-status": true,
}

// PreReadyGate guards the JSON-RPC socket during the pre-ready window defined
// by PL-003b. It tracks whether the daemon has completed in-memory model
// construction (PL-005 step 7) and, until that point, rejects agent-originated
// requests whose run_id is not in the daemon's in-memory state.
//
// The gate is safe for concurrent use; ready transitions happen exactly once
// (0 → 1) via MarkReady. Callers MUST NOT revert the gate to the pre-ready
// state.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
type PreReadyGate struct {
	// ready is 0 in the pre-ready window and 1 after MarkReady is called.
	// Access via atomic load/store only.
	ready atomic.Int32
}

// MarkReady transitions the gate from the pre-ready window to the ready state.
// After this call, IsReady() returns true and CheckRequest() will not reject
// any request.
//
// MarkReady is idempotent; calling it more than once has no additional effect.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "completion of the in-memory
// model build (PL-005 step 7)" is the event that ends the pre-ready window.
func (g *PreReadyGate) MarkReady() {
	g.ready.Store(1)
}

// IsReady reports whether the daemon has exited the pre-ready window.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func (g *PreReadyGate) IsReady() bool {
	return g.ready.Load() == 1
}

// CheckRequest evaluates a JSON-RPC method name and optional run_id parameter
// against the gate's current ready state.
//
// If the gate is in the pre-ready window (IsReady() == false) AND the method
// is agent-originated (claim-next, emit-outcome, or dispatch-status per
// PL-003a), CheckRequest returns a non-nil typed rejection error that MUST be
// returned to the caller as a JSON-RPC error response with code -32001 and
// message daemon_not_ready{reason="unknown_run_id"}.
//
// If the gate is ready, or the method is not agent-originated, CheckRequest
// returns nil (pass through).
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func (g *PreReadyGate) CheckRequest(method string) error {
	if g.ready.Load() == 1 {
		return nil // ready window has opened; pass through
	}
	if !agentOriginatedMethods[method] {
		return nil // CLI-facing or unknown method; exempt from rejection
	}
	return &ErrPreReadyRejection{Method: method}
}

// ErrPreReadyRejection is returned by PreReadyGate.CheckRequest when the gate
// is in the pre-ready window and an agent-originated request arrives.
//
// The wire-format JSON-RPC error response for this condition is:
//
//	{"jsonrpc":"2.0","id":<id>,"error":{"code":-32001,"message":"daemon_not_ready{\"reason\":\"unknown_run_id\"}"}}
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
type ErrPreReadyRejection struct {
	// Method is the JSON-RPC method name that triggered the rejection.
	Method string
}

// Error implements the error interface. The message is the typed error string
// mandated by PL-003b: daemon_not_ready{reason="unknown_run_id"}.
func (e *ErrPreReadyRejection) Error() string {
	return fmt.Sprintf(`daemon_not_ready{"reason":"unknown_run_id"} (method %q rejected in pre-ready window)`, e.Method)
}

// JSONRPCErrorCode is the JSON-RPC error code for daemon_not_ready rejections.
// -32001 is in the implementation-defined error range per JSON-RPC 2.0 spec.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
const JSONRPCErrorCodeDaemonNotReady = -32001

// JSONRPCErrorMessageDaemonNotReady is the normative typed error string that
// MUST appear in the JSON-RPC error response message field for PL-003b
// rejections. The watcher and clients match on this substring.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
const JSONRPCErrorMessageDaemonNotReady = `daemon_not_ready{"reason":"unknown_run_id"}`

// PreReadyRejectionResponse constructs the JSON-RPC 2.0 error response bytes
// (NDJSON line, without trailing newline) for a PL-003b pre-ready rejection.
// The id parameter should be the id from the incoming request; pass 0 if
// parsing failed.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b; PL-003a (NDJSON framing).
func PreReadyRejectionResponse(id int) ([]byte, error) {
	resp := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      id,
	}
	resp.Error.Code = JSONRPCErrorCodeDaemonNotReady
	resp.Error.Message = JSONRPCErrorMessageDaemonNotReady
	return json.Marshal(resp)
}
