package daemon

import (
	"context"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// ModelPreferenceError is the typed error returned by shared.ValidateModel and
// shared.ValidateEffort when a ModelPreference field fails its shape or enum
// constraint (HC-055a).
//
// The validators moved to internal/harness/shared (P2 unit E1b-prep) because the
// claude launch-spec builder calls them and is itself leaving the daemon. This
// alias — not a fresh struct — is load-bearing: export_test.go re-exports it as
// ExportedModelPreferenceError and modelpreference_hkxo03m_test.go does errors.As
// against that, which only matches if the two names denote the same type.
type ModelPreferenceError = shared.ModelPreferenceError

// EnvModelKey is the env var name for the operator-level model default.
// Checked at each ResolveModelPreference call (after tier-2 project config,
// before tier-3 compiled defaults), so it takes effect on the next dispatch
// without a daemon restart.  An invalid value is silently skipped and the walk
// continues to tier 3.
const EnvModelKey = "HARMONIK_CLAUDE_MODEL"

// EnvEffortKey is the env var name for the operator-level effort default.
// Same semantics as EnvModelKey.  Invalid effort values (not in the closed
// enum) are silently skipped.
const EnvEffortKey = "HARMONIK_CLAUDE_EFFORT"

type modelDefaultEntry struct {
	model  string
	effort string
}

var defaultModelEntries = map[core.AgentType]modelDefaultEntry{
	core.AgentTypeClaudeCode: {model: "sonnet", effort: "medium"},
	core.AgentTypeClaudeTwin: {model: "", effort: ""},
}

const labelPrefixModel = "model:"

const labelPrefixEffort = "effort:"

// ResolveModelPreference resolves the (model, effort) pair for a run by walking
// the EM-012b four-tier precedence list (plus tier-2.5 env vars per hk-c5oxy).
// model and effort are resolved independently: each walks all tiers and uses
// the first non-empty result.
//
//   - Tier 1:   per-bead model:<alias> and effort:<level> labels.
//   - Tier 2:   per-project .harmonik/config.yaml (projectCfg.LookupAgent).
//   - Tier 2.5: operator env vars HARMONIK_CLAUDE_MODEL / HARMONIK_CLAUDE_EFFORT
//     (read at call time; invalid values silently skipped).
//   - Tier 3:   compiled default map (defaultModelEntries).
//   - Tier 4:   empty strings — handler applies its own tool default.
//
// Tier-1 conflict handling (EM-012b): multiple model:<alias> labels → treat as
// absent + emit bead_label_conflict. Same for multiple effort:<level> labels.
// An unrecognised effort value also treats tier 1 as absent + emits
// bead_label_conflict. model values are not validated here (shape validation
// happens in claudelaunchspec.go Step 6); only the conflict/count rule applies.
//
// Returns ("", "") when no tier supplies a value (pure tier-4 fallback).
func ResolveModelPreference(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg projectconfig.ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (model, effort string) {
	model = resolveModelField(ctx, beadLabels, agentType, projectCfg, bus, beadID)
	effort = resolveEffortField(ctx, beadLabels, agentType, projectCfg, bus, beadID)
	return model, effort
}

func resolveModelField(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg projectconfig.ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) string {
	var modelLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixModel) {
			modelLabels = append(modelLabels, lbl)
		}
	}

	if len(modelLabels) == 1 {
		return strings.TrimPrefix(modelLabels[0], labelPrefixModel)
	}
	if len(modelLabels) > 1 {
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			modelLabels,
			"tier-1 model absent: multiple model:<alias> labels; walk continues to tier 2")
	}

	cfgModel, _ := projectCfg.LookupAgent(agentType)
	if cfgModel != "" {
		return cfgModel
	}

	if envModel := os.Getenv(EnvModelKey); envModel != "" {
		if shared.ValidateModel(envModel) == nil {
			return envModel
		}
	}

	if e, ok := defaultModelEntries[agentType]; ok && e.model != "" {
		return e.model
	}

	return ""
}

func resolveEffortField(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg projectconfig.ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) string {
	var effortLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixEffort) {
			effortLabels = append(effortLabels, lbl)
		}
	}

	if len(effortLabels) == 1 {
		val := strings.TrimPrefix(effortLabels[0], labelPrefixEffort)
		if shared.ValidateEffort(val) != nil {
			emitBeadLabelConflict(ctx, bus,
				core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
				effortLabels,
				"tier-1 effort absent: unrecognised effort value "+val+"; walk continues to tier 2")
		} else {
			return val
		}
	} else if len(effortLabels) > 1 {
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			effortLabels,
			"tier-1 effort absent: multiple effort:<level> labels; walk continues to tier 2")
	}

	_, cfgEffort := projectCfg.LookupAgent(agentType)
	if cfgEffort != "" {
		return cfgEffort
	}

	if envEffort := os.Getenv(EnvEffortKey); envEffort != "" {
		if shared.ValidateEffort(envEffort) == nil {
			return envEffort
		}
	}

	if e, ok := defaultModelEntries[agentType]; ok && e.effort != "" {
		return e.effort
	}

	return ""
}
