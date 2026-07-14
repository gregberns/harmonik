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
