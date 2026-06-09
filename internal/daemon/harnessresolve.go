package daemon

// harnessresolve.go — harness-selection four-tier precedence resolver (codex-harness C4/T4, hk-y01k6).
//
// Implements the harness-selection chain for the daemon claim path:
//
//   Tier 1 — per-bead harness:<agent-type> label
//   Tier 2 — per-queue harness default (stub: always absent until hk-4x3rg lands)
//   Tier 3 — DOT node harness attribute (stub: always absent until hk-u67of lands)
//   Tier 4 — global Config.DefaultHarness, falling back to core.AgentTypeClaudeCode
//
// The four-tier chain is modelled exactly on resolveWorkflowMode (moderesolve.go)
// and the EM-012a precedence pattern. The resolved value is used by the dispatch
// path (C5/T12, hk-xhawy) to select the concrete Harness implementation from the
// AdapterRegistry.
//
// Label convention: "harness:codex", "harness:claude-code" — the value after the
// colon MUST satisfy core.AgentType.Valid() (AR-025). Unknown or malformed values
// are treated as absent and a bead_label_conflict event is emitted.
//
// Spec ref: codex-harness design, C4-selection-spec.
// Bead: hk-y01k6 [C4/T4]

import (
	"context"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// harnessLabelPrefix is the label prefix for per-bead harness overrides.
// Usage: "harness:codex", "harness:claude-code".
const harnessLabelPrefix = "harness:"

// resolveHarness implements the four-tier harness-selection precedence walk.
//
//   - bead         — the ready-work BeadRecord; labels carry harness:<agent-type> if set
//   - queueDefault — per-queue harness default (pass core.AgentType("") when absent/unimplemented)
//   - nodeDefault  — DOT node harness attribute (pass core.AgentType("") when absent/unimplemented)
//   - globalDefault — daemon Config.DefaultHarness (pass core.AgentType("") to get the built-in fallback)
//   - bus          — event emitter; receives bead_label_conflict when tier 1 is ambiguous or malformed
//
// Returns the resolved AgentType. The returned value is always a valid AgentType
// per AR-025. When no tier resolves, falls back to core.AgentTypeClaudeCode.
func resolveHarness(
	ctx context.Context,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) core.AgentType {
	// ── Tier 1: per-bead harness:<agent-type> label ───────────────────────────
	//
	// Collect all labels that start with "harness:".
	var harnessLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, harnessLabelPrefix) {
			harnessLabels = append(harnessLabels, lbl)
		}
	}

	if len(harnessLabels) == 1 {
		// Exactly one harness label: parse the agent-type portion.
		agentTypePart := strings.TrimPrefix(harnessLabels[0], harnessLabelPrefix)
		at := core.AgentType(agentTypePart)
		if at.Valid() {
			return at
		}
		// Invalid agent-type value — treat tier 1 as absent and emit conflict event.
		emitBeadLabelConflict(ctx, bus, bead, harnessLabels,
			"tier-1 harness input treated as absent: invalid agent-type value; precedence walk continues to tier 2")
	} else if len(harnessLabels) > 1 {
		// More than one harness label — conflict.
		emitBeadLabelConflict(ctx, bus, bead, harnessLabels,
			"tier-1 harness input treated as absent: multiple harness:<agent-type> labels; precedence walk continues to tier 2")
	}
	// len(harnessLabels) == 0: tier 1 is simply absent; no event emitted.

	// ── Tier 2: per-queue harness default ─────────────────────────────────────
	//
	// Stub: hk-4x3rg (per-queue harness field) is not yet landed.
	// When it lands, the caller passes the queue's harness field here.
	// Until then, queueDefault is always empty and falls through.
	if queueDefault.Valid() {
		return queueDefault
	}

	// ── Tier 3: DOT node harness attribute ────────────────────────────────────
	//
	// Stub: hk-u67of (parse DOT node harness attribute) is not yet landed.
	// When it lands, the caller passes the node's harness attribute here.
	// Until then, nodeDefault is always empty and falls through.
	if nodeDefault.Valid() {
		return nodeDefault
	}

	// ── Tier 4: global Config.DefaultHarness / built-in fallback ──────────────
	if globalDefault.Valid() {
		return globalDefault
	}

	// Built-in fallback: claude-code (the only harness until codex is wired in C5).
	return core.AgentTypeClaudeCode
}
