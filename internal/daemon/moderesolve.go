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
// Tier 4 — hard fallback: dot (hk-30vlb)
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

// dotRefLabelPrefix is the label prefix for per-bead .dot workflow-file selection
// (hk-30q6). A bead carrying dot:<name> routes to <name>.dot in the project dir.
// This is the tier-1 path in resolveWorkflowRef; the tier-0 per-item WorkflowRef
// from queue.Item takes precedence over it.
const dotRefLabelPrefix = "dot:"

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
			if mode == core.WorkflowModeSingle {
				// Emit review_bypassed audit event (hk-81n9r): single mode is only
				// reachable via an explicit per-bead label; the daemon default and
				// tier-4 fallback both resolve to dot (hk-30vlb).
				emitReviewBypassed(ctx, bus, bead, workflowLabels[0])
			}
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
	// hk-30vlb: dot is the system default (embedded standard-bead.dot).
	// single is only reachable via an explicit workflow:single per-bead label
	// or --workflow-mode single flag — NEVER via tier-3 or tier-4 resolution.
	return core.WorkflowModeDot
}

// emitReviewBypassed emits a review_bypassed event (hk-81n9r) when a bead's
// explicit workflow:single label resolves at tier-1. Best-effort: emit errors
// are silently discarded (the resolution path continues regardless).
func emitReviewBypassed(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	bead core.BeadRecord,
	label string,
) {
	pl := core.ReviewBypassedPayload{
		BeadID:     string(bead.BeadID),
		Label:      label,
		BypassedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeReviewBypassed, b)
}

// resolveWorkflowRef resolves the .dot workflow file path for a bead using the
// EM-012a tier hierarchy (hk-30q6).
//
// Precedence:
//
//	Tier 0: itemWorkflowRef — explicit per-item override from queue.Item.WorkflowRef.
//	        If set, returned unchanged (highest priority).
//	Tier 1: per-bead dot:<name> label.
//	        Exactly one dot: label → resolve to <name> (appending ".dot" if absent).
//	        Zero or multiple dot: labels → tier 1 absent; fall through.
//	Tier 2–4: absent; returns "" so the caller falls through to project-level
//	          workflow.dot or the embedded standard-bead.dot.
//
// This function is purely a string resolver: it does not validate that the
// returned path exists on disk. Path validation happens in beadRunOne's DOT case.
func resolveWorkflowRef(bead core.BeadRecord, itemWorkflowRef string) string {
	// Tier 0: per-item explicit ref wins over label.
	if itemWorkflowRef != "" {
		return itemWorkflowRef
	}

	// Tier 1: per-bead dot:<name> label.
	var dotLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, dotRefLabelPrefix) {
			dotLabels = append(dotLabels, lbl)
		}
	}
	if len(dotLabels) == 1 {
		name := strings.TrimPrefix(dotLabels[0], dotRefLabelPrefix)
		if name != "" {
			if !strings.HasSuffix(name, ".dot") {
				name += ".dot"
			}
			return name
		}
	}

	// Tier 2–4: no label present (or ambiguous / empty value); let caller fall
	// through to the project-level workflow.dot or the embedded standard-bead.dot.
	return ""
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
