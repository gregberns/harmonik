package daemon

// modelpreference.go — ModelPreference shape validation + 4-tier resolution per
// HC-055a and EM-012b (hk-xo03m, hk-bfvk7).
//
// Provides:
//   - regex + enum guards for model/effort fields (shared.ValidateModel,
//     shared.ValidateEffort — moved to internal/harness/shared by P2 unit E1b-prep).
//   - compiled tier-3 defaults per agent type (defaultModelEntries).
//   - ResolveModelPreference: the EM-012b 4-tier precedence walk.
//
// Judgment calls (hk-bfvk7, hk-c5oxy):
//   - Tier-3 defaults live here, adjacent to the validator functions and the
//     resolution function; this avoids a separate file for a small static map.
//   - Tier-1 conflict detection emits bead_label_conflict per EM-012a precedent
//     (emitBeadLabelConflict in moderesolve.go is reused).
//   - model and effort are resolved independently: each walks all four tiers.
//   - mtime invalidation for .harmonik/config.yaml is NOT implemented; operators
//     restart the daemon to reload (matches WorkflowModeDefault and other startup-
//     time fields; documented in projectconfig.go as well).
//   - Tier-2.5 env vars (HARMONIK_CLAUDE_MODEL / HARMONIK_CLAUDE_EFFORT) are read
//     at ResolveModelPreference call time, not daemon startup.  This gives hot-
//     reload for foreground sessions (export before the next dispatch) without a
//     daemon restart.  Invalid values are silently skipped so a mis-set env var
//     never breaks dispatch (walk continues to tier 3).
//
// Spec refs:
//   - specs/handler-contract.md §4.10 HC-055a — ModelPreference descriptor invariants.
//   - specs/execution-model.md §4.3 EM-012b — model/effort resolution chain.
//
// Beads: hk-xo03m (validators), hk-bfvk7 (defaults + resolution).

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

// ─────────────────────────────────────────────────────────────────────────────
// Tier-2.5: operator env-var defaults (hk-c5oxy)
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// Tier-3: compiled per-agent-type defaults (EM-012b §3)
// ─────────────────────────────────────────────────────────────────────────────

// modelDefaultEntry is the (model, effort) pair stored in defaultModelEntries.
type modelDefaultEntry struct {
	model  string
	effort string
}

// defaultModelEntries is the compiled tier-3 default map keyed by AgentType.
// Entries here are normative for the agent type they describe; they are the
// "current operator practice" defaults and are updated by adding new agent
// adapters (EM-012b §3: "entries are normative for the binding they describe
// and additive over time").
//
// Judgment call (hk-bfvk7): claude-code defaults to model=sonnet, effort=medium
// to match current operator practice. claude-twin leaves both empty: the twin
// binary uses a local fixture stream and does not forward --model/--effort to
// a real model API. Unknown agent types receive empty defaults (tier 4 fallback).
var defaultModelEntries = map[core.AgentType]modelDefaultEntry{
	core.AgentTypeClaudeCode: {model: "sonnet", effort: "medium"},
	core.AgentTypeClaudeTwin: {model: "", effort: ""},
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveModelPreference — EM-012b 4-tier precedence walk
// ─────────────────────────────────────────────────────────────────────────────

// labelPrefixModel is the label prefix for per-bead model overrides (EM-012b §1).
const labelPrefixModel = "model:"

// labelPrefixEffort is the label prefix for per-bead effort overrides (EM-012b §1).
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

// resolveModelField resolves the model field through the four tiers.
func resolveModelField(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg projectconfig.ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) string {
	// Tier 1: collect model:<alias> labels.
	var modelLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixModel) {
			modelLabels = append(modelLabels, lbl)
		}
	}

	if len(modelLabels) == 1 {
		// Exactly one model label: accept the alias value as-is (shape validation
		// deferred to claudelaunchspec.go Step 6 per HC-055a value-opacity invariant).
		return strings.TrimPrefix(modelLabels[0], labelPrefixModel)
	}
	if len(modelLabels) > 1 {
		// Conflict: multiple model labels → treat tier 1 as absent, emit event.
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			modelLabels,
			"tier-1 model absent: multiple model:<alias> labels; walk continues to tier 2")
		// Fall through to tier 2.
	}
	// len == 0: tier 1 absent, no event.

	// Tier 2: per-project config.
	cfgModel, _ := projectCfg.LookupAgent(agentType)
	if cfgModel != "" {
		return cfgModel
	}

	// Tier 2.5: operator env var — read at call time for hot-reload (hk-c5oxy).
	if envModel := os.Getenv(EnvModelKey); envModel != "" {
		if shared.ValidateModel(envModel) == nil {
			return envModel
		}
		// Invalid shape: skip silently, fall through to tier 3.
	}

	// Tier 3: compiled default.
	if e, ok := defaultModelEntries[agentType]; ok && e.model != "" {
		return e.model
	}

	// Tier 4: empty (handler tool default).
	return ""
}

// resolveEffortField resolves the effort field through the four tiers.
func resolveEffortField(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg projectconfig.ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) string {
	// Tier 1: collect effort:<level> labels.
	var effortLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixEffort) {
			effortLabels = append(effortLabels, lbl)
		}
	}

	if len(effortLabels) == 1 {
		val := strings.TrimPrefix(effortLabels[0], labelPrefixEffort)
		// Validate the effort value per EM-012b: unrecognised → tier absent + event.
		if shared.ValidateEffort(val) != nil {
			emitBeadLabelConflict(ctx, bus,
				core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
				effortLabels,
				"tier-1 effort absent: unrecognised effort value "+val+"; walk continues to tier 2")
			// Fall through to tier 2.
		} else {
			return val
		}
	} else if len(effortLabels) > 1 {
		// Conflict: multiple effort labels → treat tier 1 as absent, emit event.
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			effortLabels,
			"tier-1 effort absent: multiple effort:<level> labels; walk continues to tier 2")
		// Fall through to tier 2.
	}
	// len == 0: tier 1 absent, no event.

	// Tier 2: per-project config.
	_, cfgEffort := projectCfg.LookupAgent(agentType)
	if cfgEffort != "" {
		return cfgEffort
	}

	// Tier 2.5: operator env var — read at call time for hot-reload (hk-c5oxy).
	if envEffort := os.Getenv(EnvEffortKey); envEffort != "" {
		if shared.ValidateEffort(envEffort) == nil {
			return envEffort
		}
		// Invalid value: skip silently, fall through to tier 3.
	}

	// Tier 3: compiled default.
	if e, ok := defaultModelEntries[agentType]; ok && e.effort != "" {
		return e.effort
	}

	// Tier 4: empty (handler tool default).
	return ""
}
