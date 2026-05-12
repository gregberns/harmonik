package brcli

// workflowlabelconflict.go — BI-009a multi-workflow:-label conflict detection.
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009a
//   - specs/event-model.md §8.8.6
//
// BI-009a: A bead carrying two or more workflow:<mode> labels is malformed.
// The br-CLI adapter MUST detect this condition during the ready-work query
// (BI-013) and the bead-detail query (BI-015), emit a bead_label_conflict
// observability event per event-model.md §8.8.6, surface the bead to the
// dispatch loop with workflow_mode = <unresolved>, and fall back to the
// next-lower precedence tier.
//
// Detection also fires when a single workflow:<mode> label names a mode value
// not in {single, review-loop, dot} (per BI-009a / event-model.md §8.8.6
// emission rule).
//
// This file provides:
//   - LabelConflictEmitter — the narrow interface the helper needs for event
//     emission; eventbus.EventBus satisfies it. A nil value is accepted and
//     triggers the structured-log fallback per ON-035.
//   - DetectWorkflowLabelConflict — the detection + emission helper consumed by
//     the daemon's claim path and the ready-work adapter.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// fallbackAction is the canonical fallback-action string placed in the
// bead_label_conflict payload (event-model.md §8.8.6 fallback_action field).
const fallbackAction = "tier-1 input treated as absent; precedence walk continues to tier 2"

// LabelConflictEmitter is the narrow event-emission surface required by
// DetectWorkflowLabelConflict.
//
// eventbus.EventBus satisfies this interface via its Emit method.  Callers MAY
// pass nil; in that case DetectWorkflowLabelConflict falls back to a
// structured-log record per operator-nfr.md §4.9 ON-035.
//
// NOTE(hk-872.57): full event-bus integration in the brcli adapter is tracked
// by hk-872.57.  Until that bead lands, production callers SHOULD pass nil and
// rely on the structured-log fallback.  Tests that exercise the emit path MUST
// supply a non-nil stub satisfying this interface.
type LabelConflictEmitter interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// WorkflowLabelConflictResult carries the outcome of a label-conflict
// detection pass.
//
// When Conflicted is false, ConflictingLabels is nil and all other fields
// are zero-valued.  When Conflicted is true, ConflictingLabels contains the
// offending labels (length ≥ 1) and the bead_label_conflict event has been
// emitted (or logged if the bus was unavailable).
type WorkflowLabelConflictResult struct {
	// Conflicted is true when at least one of the two BI-009a conflict
	// conditions was detected:
	//   (a) more than one workflow:<mode> label on the bead, OR
	//   (b) a single workflow:<mode> label whose <mode> is not in
	//       {single, review-loop, dot}.
	Conflicted bool

	// ConflictingLabels is the set of offending workflow:<mode> labels.
	// Non-nil and non-empty iff Conflicted is true.
	ConflictingLabels []string
}

// DetectWorkflowLabelConflict scans labels for BI-009a workflow-mode label
// conflicts on beadID and emits a bead_label_conflict event when a conflict
// is detected.
//
// A conflict is declared under two conditions (per BI-009a and
// event-model.md §8.8.6 emission rule):
//
//  1. The labels slice contains more than one string with the "workflow:"
//     prefix.
//  2. The labels slice contains exactly one "workflow:" label, but the mode
//     value after the colon is not one of {single, review-loop, dot}.
//
// When a conflict is detected the function:
//   - Marshals a core.BeadLabelConflictPayload and emits it via bus.Emit using
//     the "bead_label_conflict" event type.
//   - If bus is nil OR bus.Emit returns an error, falls back to a structured-log
//     record at level=error with subsystem=beads-adapter, bead_id, and
//     conflicting_labels fields per operator-nfr.md §4.9 ON-035.
//
// The returned WorkflowLabelConflictResult.Conflicted flag MUST be used by the
// caller to treat tier-1 mode resolution as absent and continue the precedence
// walk (daemon falls back to tier 2).
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009a
//   - specs/event-model.md §8.8.6
func DetectWorkflowLabelConflict(
	ctx context.Context,
	beadID string,
	labels []string,
	bus LabelConflictEmitter,
) WorkflowLabelConflictResult {
	// Collect all labels that start with the "workflow:" prefix.
	var workflowLabels []string
	for _, l := range labels {
		if strings.HasPrefix(l, workflowLabelPrefix) {
			workflowLabels = append(workflowLabels, l)
		}
	}

	// No workflow: labels at all — no conflict, no event.
	if len(workflowLabels) == 0 {
		return WorkflowLabelConflictResult{}
	}

	// Check condition (a): more than one workflow:<mode> label.
	if len(workflowLabels) > 1 {
		emitLabelConflict(ctx, beadID, workflowLabels, bus)
		return WorkflowLabelConflictResult{
			Conflicted:        true,
			ConflictingLabels: workflowLabels,
		}
	}

	// Exactly one workflow:<mode> label: check condition (b) — unknown mode.
	label := workflowLabels[0]
	mode := core.WorkflowMode(strings.TrimPrefix(label, workflowLabelPrefix))
	if !mode.Valid() {
		emitLabelConflict(ctx, beadID, workflowLabels, bus)
		return WorkflowLabelConflictResult{
			Conflicted:        true,
			ConflictingLabels: workflowLabels,
		}
	}

	// Single valid workflow:<mode> label — no conflict.
	return WorkflowLabelConflictResult{}
}

// emitLabelConflict marshals and emits the bead_label_conflict event.
// Falls back to a structured-log record when bus is nil or emission fails.
func emitLabelConflict(
	ctx context.Context,
	beadID string,
	conflictingLabels []string,
	bus LabelConflictEmitter,
) {
	payload := core.BeadLabelConflictPayload{
		BeadID:            beadID,
		ConflictingLabels: conflictingLabels,
		FallbackAction:    fallbackAction,
		DetectedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		// Marshal of a known-shape struct should never fail, but guard anyway.
		slog.ErrorContext(ctx, "brcli: bead_label_conflict: payload marshal failed; falling back to structured-log",
			"subsystem", "beads-adapter",
			"bead_id", beadID,
			"conflicting_labels", conflictingLabels,
			"error", marshalErr,
		)
		return
	}

	if bus != nil {
		if emitErr := bus.Emit(ctx, core.EventType("bead_label_conflict"), raw); emitErr != nil {
			// Bus emission failed — emit structured-log record per ON-035.
			slog.ErrorContext(ctx, "brcli: bead_label_conflict: bus emission failed; structured-log fallback",
				"subsystem", "beads-adapter",
				"bead_id", beadID,
				"conflicting_labels", conflictingLabels,
				"error", emitErr,
			)
		}
		return
	}

	// Bus is nil (not yet wired — hk-872.57): structured-log fallback per ON-035.
	slog.ErrorContext(ctx, "brcli: bead_label_conflict: bus unavailable; structured-log fallback",
		"subsystem", "beads-adapter",
		"bead_id", beadID,
		"conflicting_labels", conflictingLabels,
	)
}
