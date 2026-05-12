package daemon

// moderesolve.go — workflow-mode resolution for the daemon claim path.
//
// Implements execution-model.md §4.3.EM-012a: a four-tier precedence walk
// that resolves workflow_mode exactly once at claim time, before any node in
// the run is dispatched.
//
// Tier 1 — per-bead workflow:<mode> label (beads-integration.md §4.3 BI-009a)
// Tier 2 — per-project config (reserved no-op for MVH; always absent)
// Tier 3 — daemon default (workLoopDeps.workflowModeDefault per hk-7om2q.8)
// Tier 4 — hard fallback: single
//
// The resolved value MUST be sealed into the Run record before dispatch and
// MUST NOT be re-evaluated for the run's lifetime per §4.3.EM-012a.
//
// Spec refs: specs/execution-model.md §4.3 EM-012a.
// Bead: hk-7om2q.9.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// workflowLabelPrefix is the label prefix for per-bead workflow-mode overrides
// per beads-integration.md §4.3 BI-009a.
const workflowLabelPrefix = "workflow:"

// resolveWorkflowMode implements the EM-012a four-tier precedence walk.
//
//   - bead      — carries the labels from the ready-work record (BI-013)
//   - daemon    — the daemon-level default cached in workLoopDeps (tier 3)
//   - bus / ctx — used to emit bead_label_conflict when tier-1 is ambiguous
//
// Returns the resolved WorkflowMode. The returned value is always a valid
// WorkflowMode constant (one of single, review-loop, dot).
//
// Tier-1 conflict handling: when the bead carries more than one workflow:<mode>
// label, OR carries a workflow:<mode> label whose mode value is not a declared
// constant, the daemon MUST emit bead_label_conflict per event-model.md §8.8.6
// and treat tier 1 as absent, continuing the walk to tier 2.
func resolveWorkflowMode(
	ctx context.Context,
	bead core.BeadRecord,
	daemonDefault core.WorkflowMode,
	bus handlercontract.EventEmitter,
) core.WorkflowMode {
	// ── Tier 1: per-bead workflow:<mode> label ─────────────────────────────
	//
	// Collect all labels that start with "workflow:".
	var workflowLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, workflowLabelPrefix) {
			workflowLabels = append(workflowLabels, lbl)
		}
	}

	if len(workflowLabels) == 1 {
		// Exactly one workflow label: parse the mode portion.
		modePart := strings.TrimPrefix(workflowLabels[0], workflowLabelPrefix)
		mode := core.WorkflowMode(modePart)
		if mode.Valid() {
			return mode
		}
		// Unknown mode value — treat tier 1 as absent and emit conflict event.
		emitBeadLabelConflict(ctx, bus, bead, workflowLabels,
			"tier-1 input treated as absent: unknown mode value; precedence walk continues to tier 2")
	} else if len(workflowLabels) > 1 {
		// More than one workflow label — conflict per EM-012a.
		emitBeadLabelConflict(ctx, bus, bead, workflowLabels,
			"tier-1 input treated as absent: multiple workflow:<mode> labels; precedence walk continues to tier 2")
	}
	// len(workflowLabels) == 0: tier 1 is simply absent; no event emitted.

	// ── Tier 2: per-project config (reserved no-op for MVH) ───────────────
	//
	// No per-project config mechanism exists at MVH; tier 2 is always absent.
	// Falls through to tier 3.

	// ── Tier 3: daemon default ─────────────────────────────────────────────
	if daemonDefault.Valid() {
		return daemonDefault
	}

	// ── Tier 4: hard fallback ──────────────────────────────────────────────
	return core.WorkflowModeSingle
}

// emitBeadLabelConflict emits a bead_label_conflict event per
// event-model.md §8.8.6. The call is best-effort: emit errors are silently
// discarded (the resolution path continues regardless).
func emitBeadLabelConflict(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	bead core.BeadRecord,
	conflictingLabels []string,
	fallbackAction string,
) {
	pl := core.BeadLabelConflictPayload{
		BeadID:            string(bead.BeadID),
		ConflictingLabels: conflictingLabels,
		FallbackAction:    fallbackAction,
		DetectedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeBeadLabelConflict, b)
}
