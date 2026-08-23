package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

const harnessLabelPrefix = "harness:"

func resolveHarness(
	ctx context.Context,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) core.AgentType {
	var harnessLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, harnessLabelPrefix) {
			harnessLabels = append(harnessLabels, lbl)
		}
	}

	if len(harnessLabels) == 1 {
		agentTypePart := strings.TrimPrefix(harnessLabels[0], harnessLabelPrefix)
		at := core.AgentType(agentTypePart)
		if at.Valid() {
			emitHarnessSelected(ctx, bus, bead, at, 1)
			return at
		}
		emitBeadLabelConflict(ctx, bus, bead, harnessLabels,
			"tier-1 harness input treated as absent: invalid agent-type value; precedence walk continues to tier 2")
	} else if len(harnessLabels) > 1 {
		emitBeadLabelConflict(ctx, bus, bead, harnessLabels,
			"tier-1 harness input treated as absent: multiple harness:<agent-type> labels; precedence walk continues to tier 2")
	}

	if queueDefault.Valid() {
		emitHarnessSelected(ctx, bus, bead, queueDefault, 2)
		return queueDefault
	}

	if nodeDefault.Valid() {
		emitHarnessSelected(ctx, bus, bead, nodeDefault, 3)
		return nodeDefault
	}

	if globalDefault.Valid() {
		emitHarnessSelected(ctx, bus, bead, globalDefault, 4)
		return globalDefault
	}

	emitHarnessSelected(ctx, bus, bead, core.AgentTypeClaudeCode, 4)
	return core.AgentTypeClaudeCode
}

func resolveHarnessAgentTypeQuiet(
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
) core.AgentType {
	var harnessLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, harnessLabelPrefix) {
			harnessLabels = append(harnessLabels, lbl)
		}
	}
	if len(harnessLabels) == 1 {
		at := core.AgentType(strings.TrimPrefix(harnessLabels[0], harnessLabelPrefix))
		if at.Valid() {
			return at
		}
	}
	if queueDefault.Valid() {
		return queueDefault
	}
	if nodeDefault.Valid() {
		return nodeDefault
	}
	if globalDefault.Valid() {
		return globalDefault
	}
	return core.AgentTypeClaudeCode
}

func emitHarnessSelected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	bead core.BeadRecord,
	agentType core.AgentType,
	tier int,
) {
	pl := core.HarnessSelectedPayload{
		BeadID:    string(bead.BeadID),
		AgentType: string(agentType),
		Tier:      tier,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.Emit(ctx, core.EventTypeHarnessSelected, b); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit harness_selected failed", "err", emitErr, "bead_id", string(bead.BeadID))
	}
}
