package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

const workflowLabelPrefix = "workflow:"

const dotRefLabelPrefix = "dot:"

func resolveWorkflowMode(
	ctx context.Context,
	bead core.BeadRecord,
	daemonDefault core.WorkflowMode,
	bus handlercontract.EventEmitter,
) core.WorkflowMode {
	return resolveWorkflowModeWithAudit(ctx, bead, daemonDefault, bus, true)
}

func resolveWorkflowModeWithAudit(
	ctx context.Context,
	bead core.BeadRecord,
	daemonDefault core.WorkflowMode,
	bus handlercontract.EventEmitter,
	emitBypassAudit bool,
) core.WorkflowMode {
	var workflowLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, workflowLabelPrefix) {
			workflowLabels = append(workflowLabels, lbl)
		}
	}

	if len(workflowLabels) == 1 {
		modePart := strings.TrimPrefix(workflowLabels[0], workflowLabelPrefix)
		mode := core.WorkflowMode(modePart)
		if mode.Valid() {
			if mode == core.WorkflowModeSingle && emitBypassAudit {
				emitReviewBypassed(ctx, bus, bead, workflowLabels[0])
			}
			return mode
		}
		emitBeadLabelConflict(ctx, bus, bead, workflowLabels,
			"tier-1 input treated as absent: unknown mode value; precedence walk continues to tier 2")
	} else if len(workflowLabels) > 1 {
		emitBeadLabelConflict(ctx, bus, bead, workflowLabels,
			"tier-1 input treated as absent: multiple workflow:<mode> labels; precedence walk continues to tier 2")
	}

	if daemonDefault.Valid() {
		return daemonDefault
	}

	return core.WorkflowModeDot
}

func hasExactWorkflowSingleLabel(labels []string) bool {
	var workflowLabels []string
	for _, label := range labels {
		if strings.HasPrefix(label, workflowLabelPrefix) {
			workflowLabels = append(workflowLabels, label)
		}
	}
	return len(workflowLabels) == 1 && workflowLabels[0] == workflowLabelPrefix+string(core.WorkflowModeSingle)
}

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
	if emitErr := bus.Emit(ctx, core.EventTypeReviewBypassed, b); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit review_bypassed failed", "err", emitErr, "bead_id", string(bead.BeadID))
	}
}

func resolveWorkflowRef(bead core.BeadRecord, itemWorkflowRef string) string {
	if itemWorkflowRef != "" {
		return itemWorkflowRef
	}

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

	for _, lbl := range bead.Labels {
		if lbl == "codename:eval" {
			return "eval-bead.dot"
		}
	}

	return ""
}

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
	if emitErr := bus.Emit(ctx, core.EventTypeBeadLabelConflict, b); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit bead_label_conflict failed", "err", emitErr, "bead_id", string(bead.BeadID))
	}
}
