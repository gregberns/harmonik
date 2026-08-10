package daemon

// verdictoverride.go — Daemon-side operator verdict-override surface (RC-027).
//
// RC-027 lets an operator pause the daemon's verdict-execution step until they
// explicitly confirm or veto the investigator's verdict. A reconciliation run
// whose YAML policy sets confirm_required: true (core.PolicyRequiresConfirmation)
// parks awaiting operator input; the CLI verbs `harmonik confirm-verdict` and
// `harmonik veto-verdict` send the operator's decision over the Unix socket
// (ops "confirm_verdict" / "veto_verdict"), which buildSocketRouter routes to the
// confirmVerdict / vetoVerdict socketDispatch methods.
//
// This file declares:
//
//   - VerdictOverrideHandler — the socket-dispatch interface. *OperatorPauseController
//     implements it (the RC-027 operator surface is part of operator-control, so
//     it rides the existing OperatorControlHandler value via interface
//     type-assertion in the socketDispatch methods rather than adding a new
//     handler field).
//
//   - VerdictConfirmationRegistry — the park/release rendezvous. A parked run
//     calls Await(run_id) and blocks on the returned channel; the socket handler
//     calls Resolve(request) to deliver the operator's decision. Keyed by the
//     run_id wire string carried in OperatorVerdictOverrideRequest.TargetRunID.
//
// # Wiring status — NOT CONNECTED, and it is wider than one missing call
//
// The socket route, the request validation, and the release rendezvous are
// complete and exercised by the daemon tests. Nothing calls them in production.
// Three call sites are missing, and connecting any ONE of them still leaves the
// operator commands unreachable:
//
//   - Await has no non-test caller, so pending is always empty and Resolve
//     returns false for every run_id.
//   - ExecuteVerdict (RC-025a, verdictexecutor_rc025a.go) has no non-test
//     caller, so there is no verdict-execution step to pause in the first
//     place. scripts/reachability.baseline carries it as accepted dead code.
//   - core.PolicyRequiresConfirmation has no non-test caller, so no policy's
//     confirm_required field is ever read.
//
// The earlier note here said "only the executor-side Await call remains". That
// was wrong in a way that cost real time: it reads as one small follow-up, and
// an operator running the CLI verb sees a refusal that names their run_id and
// goes looking for the run. cmd/harmonik/confirm_verdict.go and veto_verdict.go
// now say NOT CONNECTED on every surface they print.
//
// Whoever connects this must update those two CLI files in the same change.
// TestAwaitHasNoProductionCaller in verdictoverride_unwired_aqjxo_test.go fails
// the moment a production caller appears, and it names the files to fix.
//
// Bead ref: hk-verdict-override-unwired-aqjxo.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014.

import (
	"context"
	"fmt"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
)

// VerdictOverrideHandler is the interface the socketDispatch confirmVerdict /
// vetoVerdict methods invoke for the "confirm_verdict" and "veto_verdict" ops
// (RC-027). The concrete implementation is *OperatorPauseController.
//
// HandleVerdictOverride returns (errorCode, err):
//   - (0, nil)   — the decision was delivered to a parked run.
//   - (16, err)  — no run is parked for req.TargetRunID
//     (operator-control-invalid-state; the CLI exits 16).
//   - (0, err)   — the request was malformed (should not occur: the socket
//     methods validate via req.Valid() before calling).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
type VerdictOverrideHandler interface {
	HandleVerdictOverride(ctx context.Context, req core.OperatorVerdictOverrideRequest) (errorCode int, err error)
}

// VerdictConfirmationRegistry is the rendezvous between a reconciliation run
// parked awaiting operator confirmation (RC-027) and the operator's confirm/veto
// decision arriving over the socket.
//
// Concurrent-safe: Await is called from a verdict-executor goroutine while
// Resolve is called from a socket-handler goroutine.
type VerdictConfirmationRegistry struct {
	mu      sync.Mutex
	pending map[string]chan core.OperatorVerdictOverrideRequest
}

// NewVerdictConfirmationRegistry constructs an empty registry.
func NewVerdictConfirmationRegistry() *VerdictConfirmationRegistry {
	return &VerdictConfirmationRegistry{
		pending: make(map[string]chan core.OperatorVerdictOverrideRequest),
	}
}

// Await registers runID as parked awaiting an operator decision and returns a
// channel the caller blocks on until Resolve delivers the decision. The channel
// is buffered (cap 1) so Resolve never blocks. The verdict-executor (RC-025a)
// calls this when core.PolicyRequiresConfirmation is true.
//
// A second Await for the same runID replaces the prior entry; the earlier
// waiter's channel is left dangling (it never receives) — RC-027 has a single
// operator decision per parked run, so replacement is not expected in practice.
func (r *VerdictConfirmationRegistry) Await(runID string) <-chan core.OperatorVerdictOverrideRequest {
	ch := make(chan core.OperatorVerdictOverrideRequest, 1)
	r.mu.Lock()
	r.pending[runID] = ch
	r.mu.Unlock()
	return ch
}

// Resolve delivers req to the run parked under req.TargetRunID and removes the
// pending entry. It returns true when a run was parked (decision delivered) and
// false when no run is parked for that run_id.
func (r *VerdictConfirmationRegistry) Resolve(req core.OperatorVerdictOverrideRequest) bool {
	r.mu.Lock()
	ch, ok := r.pending[req.TargetRunID]
	if ok {
		delete(r.pending, req.TargetRunID)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- req // never blocks: buffered cap-1, one delivery per parked run
	return true
}

// HandleVerdictOverride implements VerdictOverrideHandler on
// *OperatorPauseController: it validates the operator's confirm/veto request and
// delivers it to the parked run via the VerdictConfirmationRegistry.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func (c *OperatorPauseController) HandleVerdictOverride(_ context.Context, req core.OperatorVerdictOverrideRequest) (int, error) {
	if !req.Valid() {
		return 0, fmt.Errorf("daemon: verdict-override: invalid request for run %q", req.TargetRunID)
	}
	if !c.verdicts.Resolve(req) {
		// error_code 16 = operator-control-invalid-state (no pending verdict).
		return 16, fmt.Errorf("daemon: verdict-override: no pending verdict for run %q", req.TargetRunID)
	}
	return 0, nil
}

// Verdicts returns the controller's VerdictConfirmationRegistry so the
// verdict-executor can park runs (Await) on the same instance the socket handler
// resolves against.
func (c *OperatorPauseController) Verdicts() *VerdictConfirmationRegistry {
	return c.verdicts
}
