// runports.go — RunEnv / RunPorts / SharedHandles boundary bundles (RSM-010).
//
// C3 of the run-state-machine work (see .kerf/works/2026-07-14-run-state-machine
// /04-design/ports-design.md §1). This file introduces the narrow, structural
// ports through which the run shell reaches its behavioral dependencies, plus
// the two value/handle bundles (RunEnv, SharedHandles) that carry immutable
// per-run values and shared-by-reference cross-goroutine state respectively.
//
// Boundary-only: every port is a pass-through onto the exact concrete
// dependency already wired on workLoopDeps. No behavior changes — a port call
// resolves to the same underlying method the run path invoked directly before
// (RSM-010). The nil-default adapters preserve today's nil-means-production
// defaulting exactly (ports-design §3).
//
// Idiom mirror: internal/keeper/ports.go (structural narrow interfaces;
// EmitterPort = Emitter type alias, keeper ports.go:107).

package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/mergeq"
)

// EmitterPort is the event-emission surface of the run shell. It is a type
// alias for the production emitter interface (the keeper ports.go:107 trick):
// deps.bus already satisfies it, so wiring is identity.
type EmitterPort = handlercontract.EventEmitter

// LedgerPort is the Beads-ledger surface of the run path. It hides the ambient
// intentLogDir / brTimeoutCfg plumbing (carried internally by the adapter) and
// folds the pre-close .br_history trim into CloseBead (closeBeadWithHistoryTrim),
// so callers pass only the run-scoped identifiers.
type LedgerPort interface {
	// ShowBead returns the bead record (edges included) for id.
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
	// ReopenBead reopens beadID with reason, under the run's transition id.
	ReopenBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
	// CloseBead trims .br_history then closes beadID (closeBeadWithHistoryTrim).
	CloseBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error
}

// daemonLedger is the production LedgerPort adapter. It wraps the live
// *workLoopDeps so CloseBead routes through closeBeadWithHistoryTrim (history
// trim + brAdapter.CloseBead) and Reopen/Show route straight to brAdapter,
// carrying deps.intentLogDir and deps.brTimeoutCfg internally — byte-identical
// to the pre-port call sites.
type daemonLedger struct {
	deps *workLoopDeps
}

func (l daemonLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return l.deps.brAdapter.ShowBead(ctx, id)
}

func (l daemonLedger) ReopenBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error {
	return l.deps.brAdapter.ReopenBead(ctx, l.deps.intentLogDir, l.deps.brTimeoutCfg, runID, transitionID, beadID, reason)
}

func (l daemonLedger) CloseBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error {
	return l.deps.closeBeadWithHistoryTrim(ctx, runID, transitionID, beadID, needsAttention)
}

// ledgerPort returns the production LedgerPort bound to these deps. nil brAdapter
// is impossible on the run path (newWorkLoopDeps enforces it), so no nil-default
// is required here (ports-design §3).
func (deps *workLoopDeps) ledgerPort() LedgerPort {
	return daemonLedger{deps: deps}
}

// emitterPort returns the run shell's EmitterPort (identity over deps.bus).
func (deps *workLoopDeps) emitterPort() EmitterPort {
	return deps.bus
}

// MergePort is the merge exclusion-domain surface of the run path (RSM-015). It
// exposes the strictly-FIFO single-owner submit entry point that serialises the
// commit-phase merge, the post-merge escaped-worktree check, and the remote
// base-sync + worktree-add.
type MergePort interface {
	// Submit returns the exclusion-domain entry point (mergeq.Queue.Submit when a
	// queue is wired, else the inline nil-queue fallback).
	Submit() mergeSubmit
}

// daemonMerge is the production MergePort adapter over the RT3 mergeq handle: the
// queue's Submit when non-nil, else inlineMergeSubmit (the nil-queue
// single-beadRunOne fallback). This is the sole owner of that selection, which
// previously lived on the (now-removed) workLoopDeps.mergeSubmitFunc method.
type daemonMerge struct {
	q *mergeq.Queue
}

func (m daemonMerge) Submit() mergeSubmit {
	if m.q != nil {
		return m.q.Submit
	}
	return inlineMergeSubmit
}

// mergePort returns the production MergePort bound to deps.mergeQ.
func (deps *workLoopDeps) mergePort() MergePort {
	return daemonMerge{q: deps.mergeQ}
}

// GatePort is the DOT gate-node evaluation surface: it resolves a gate_ref to a
// Gate ControlPoint via the daemon's ControlPoint registry.
type GatePort interface {
	// LookupGate resolves gateRef. registryLoaded is false when no registry is
	// wired (nil cpRegistry); ok is false when gateRef is absent from a loaded
	// registry. The Kind check stays at the call site so the eval-failure reason
	// strings remain byte-identical to the pre-port path (ports-design §3).
	LookupGate(gateRef core.GateRef) (cp core.ControlPoint, ok bool, registryLoaded bool)
}

// daemonGate is the production GatePort adapter over deps.cpRegistry. A nil
// registry yields registryLoaded=false so the caller emits the exact
// "no ControlPoint registry loaded in daemon" eval-failure Outcome.
type daemonGate struct {
	reg core.Registry
}

func (g daemonGate) LookupGate(gateRef core.GateRef) (core.ControlPoint, bool, bool) {
	if g.reg == nil {
		return core.ControlPoint{}, false, false
	}
	cp, ok := g.reg.LookupByName(string(gateRef))
	return cp, ok, true
}

// gatePort returns the production GatePort bound to deps.cpRegistry.
func (deps *workLoopDeps) gatePort() GatePort {
	return daemonGate{reg: deps.cpRegistry}
}
