package daemon

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
