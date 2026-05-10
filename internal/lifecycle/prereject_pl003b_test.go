package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"
)

// prereject_pl003b_test.go — PL-003b PreReadyGate unit tests.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
// Bead: hk-8mup.8.

// ─────────────────────────────────────────────────────────────────────────────
// PreReadyGate.IsReady
// ─────────────────────────────────────────────────────────────────────────────

// TestPL003b_Gate_InitiallyNotReady verifies that a new PreReadyGate starts in
// the pre-ready window (IsReady() == false).
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "Between socket bind (PL-005
// step 3a) and completion of the in-memory model build (PL-005 step 7)."
func TestPL003b_Gate_InitiallyNotReady(t *testing.T) {
	t.Parallel()

	var g PreReadyGate
	if g.IsReady() {
		t.Error("PL-003b: new PreReadyGate.IsReady() = true, want false")
	}
}

// TestPL003b_Gate_MarkReadySetsReady verifies that MarkReady transitions
// the gate from pre-ready to ready.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func TestPL003b_Gate_MarkReadySetsReady(t *testing.T) {
	t.Parallel()

	var g PreReadyGate
	g.MarkReady()
	if !g.IsReady() {
		t.Error("PL-003b: PreReadyGate.IsReady() = false after MarkReady, want true")
	}
}

// TestPL003b_Gate_MarkReadyIdempotent verifies that calling MarkReady multiple
// times has no additional effect (gate remains ready).
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — MarkReady is idempotent.
func TestPL003b_Gate_MarkReadyIdempotent(t *testing.T) {
	t.Parallel()

	var g PreReadyGate
	g.MarkReady()
	g.MarkReady()
	g.MarkReady()
	if !g.IsReady() {
		t.Error("PL-003b: PreReadyGate.IsReady() = false after repeated MarkReady, want true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PreReadyGate.CheckRequest — pre-ready window
// ─────────────────────────────────────────────────────────────────────────────

// TestPL003b_CheckRequest_AgentMethods_PreReadyRejected verifies that all
// agent-originated methods are rejected during the pre-ready window.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "the daemon MUST reject any
// emit-outcome, claim-next, or other agent-originated request."
// Spec ref: process-lifecycle.md §4.1 PL-003a — agent-facing method inventory.
func TestPL003b_CheckRequest_AgentMethods_PreReadyRejected(t *testing.T) {
	t.Parallel()

	agentMethods := []string{"claim-next", "emit-outcome", "dispatch-status"}
	for _, method := range agentMethods {
		var g PreReadyGate // not ready
		err := g.CheckRequest(method)
		if err == nil {
			t.Errorf("PL-003b: CheckRequest(%q) in pre-ready window: got nil, want ErrPreReadyRejection", method)
			continue
		}
		var rejection *ErrPreReadyRejection
		if !func() bool {
			e, ok := err.(*ErrPreReadyRejection)
			if ok {
				rejection = e
			}
			return ok
		}() {
			t.Errorf("PL-003b: CheckRequest(%q): error type = %T, want *ErrPreReadyRejection", method, err)
			continue
		}
		if rejection.Method != method {
			t.Errorf("PL-003b: ErrPreReadyRejection.Method = %q, want %q", rejection.Method, method)
		}
	}
}

// TestPL003b_CheckRequest_CLIMethods_Exempt verifies that CLI-facing methods
// (daemon-status probes per PL-009b) are not rejected during the pre-ready window.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "CLI daemon-status requests
// are exempt."
func TestPL003b_CheckRequest_CLIMethods_Exempt(t *testing.T) {
	t.Parallel()

	cliMethods := []string{"status", "pause", "resume", "stop", "upgrade", "attach", "enqueue", "list"}
	for _, method := range cliMethods {
		var g PreReadyGate // not ready
		err := g.CheckRequest(method)
		if err != nil {
			t.Errorf("PL-003b: CheckRequest(%q) CLI method in pre-ready window: got err %v, want nil", method, err)
		}
	}
}

// TestPL003b_CheckRequest_UnknownMethods_Exempt verifies that unknown methods
// (not in the agent-originated set) are passed through during the pre-ready window.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — rejection applies only to
// agent-originated methods.
func TestPL003b_CheckRequest_UnknownMethods_Exempt(t *testing.T) {
	t.Parallel()

	var g PreReadyGate // not ready
	err := g.CheckRequest("unknown-method")
	if err != nil {
		t.Errorf("PL-003b: CheckRequest unknown method in pre-ready window: got err %v, want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PreReadyGate.CheckRequest — after ready
// ─────────────────────────────────────────────────────────────────────────────

// TestPL003b_CheckRequest_AgentMethods_AfterReady_NotRejected verifies that
// agent-originated methods are NOT rejected after MarkReady is called.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b — "completion of the in-memory
// model build (PL-005 step 7)" ends the rejection window.
func TestPL003b_CheckRequest_AgentMethods_AfterReady_NotRejected(t *testing.T) {
	t.Parallel()

	agentMethods := []string{"claim-next", "emit-outcome", "dispatch-status"}
	for _, method := range agentMethods {
		var g PreReadyGate
		g.MarkReady()
		err := g.CheckRequest(method)
		if err != nil {
			t.Errorf("PL-003b: CheckRequest(%q) after ready: got err %v, want nil", method, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ErrPreReadyRejection
// ─────────────────────────────────────────────────────────────────────────────

// TestPL003b_ErrPreReadyRejection_ErrorContainsDaemonNotReady verifies that
// ErrPreReadyRejection.Error() includes the normative "daemon_not_ready" string.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func TestPL003b_ErrPreReadyRejection_ErrorContainsDaemonNotReady(t *testing.T) {
	t.Parallel()

	e := &ErrPreReadyRejection{Method: "claim-next"}
	if !strings.Contains(e.Error(), "daemon_not_ready") {
		t.Errorf("PL-003b: ErrPreReadyRejection.Error() = %q; want to contain %q", e.Error(), "daemon_not_ready")
	}
	if !strings.Contains(e.Error(), "unknown_run_id") {
		t.Errorf("PL-003b: ErrPreReadyRejection.Error() = %q; want to contain %q", e.Error(), "unknown_run_id")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PreReadyRejectionResponse
// ─────────────────────────────────────────────────────────────────────────────

// TestPL003b_RejectionResponse_JSONShape verifies that PreReadyRejectionResponse
// produces a valid JSON-RPC 2.0 error object with the correct code and message.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func TestPL003b_RejectionResponse_JSONShape(t *testing.T) {
	t.Parallel()

	data, err := PreReadyRejectionResponse(42)
	if err != nil {
		t.Fatalf("PL-003b: PreReadyRejectionResponse: %v", err)
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("PL-003b: unmarshal rejection response: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("PL-003b: rejection response jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
	}
	if resp.ID != 42 {
		t.Errorf("PL-003b: rejection response id = %d, want 42", resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("PL-003b: rejection response error field is nil")
	}
	if resp.Error.Code != JSONRPCErrorCodeDaemonNotReady {
		t.Errorf("PL-003b: rejection response error.code = %d, want %d", resp.Error.Code, JSONRPCErrorCodeDaemonNotReady)
	}
	if resp.Error.Message != JSONRPCErrorMessageDaemonNotReady {
		t.Errorf("PL-003b: rejection response error.message = %q, want %q", resp.Error.Message, JSONRPCErrorMessageDaemonNotReady)
	}
}

// TestPL003b_ErrorCodeConstant_Is_Minus32001 verifies the JSON-RPC error code
// constant value.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func TestPL003b_ErrorCodeConstant_Is_Minus32001(t *testing.T) {
	t.Parallel()

	const want = -32001
	if JSONRPCErrorCodeDaemonNotReady != want {
		t.Errorf("PL-003b: JSONRPCErrorCodeDaemonNotReady = %d, want %d", JSONRPCErrorCodeDaemonNotReady, want)
	}
}

// TestPL003b_ErrorMessageConstant_ContainsDaemonNotReady verifies the
// JSONRPCErrorMessageDaemonNotReady constant contains the required substrings.
//
// Spec ref: process-lifecycle.md §4.1 PL-003b.
func TestPL003b_ErrorMessageConstant_ContainsDaemonNotReady(t *testing.T) {
	t.Parallel()

	if !strings.Contains(JSONRPCErrorMessageDaemonNotReady, "daemon_not_ready") {
		t.Errorf("PL-003b: JSONRPCErrorMessageDaemonNotReady = %q; want to contain %q", JSONRPCErrorMessageDaemonNotReady, "daemon_not_ready")
	}
	if !strings.Contains(JSONRPCErrorMessageDaemonNotReady, "unknown_run_id") {
		t.Errorf("PL-003b: JSONRPCErrorMessageDaemonNotReady = %q; want to contain %q", JSONRPCErrorMessageDaemonNotReady, "unknown_run_id")
	}
}
