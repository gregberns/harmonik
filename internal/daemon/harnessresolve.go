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
// After each successful resolution, a harness_selected event is emitted (hk-lr5t)
// carrying bead_id, agent_type, and the resolving tier (1–4). This closes the
// observability gap: previously nothing in events.jsonl showed which harness was
// chosen, making silent claude-code fallbacks invisible.
//
// Spec ref: codex-harness design, C4-selection-spec.
// Bead: hk-y01k6 [C4/T4], hk-lr5t [observability]

import (
	"context"
	"encoding/json"
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
//     and harness_selected on every successful resolution (hk-lr5t).
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
			emitHarnessSelected(ctx, bus, bead, at, 1)
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
		emitHarnessSelected(ctx, bus, bead, queueDefault, 2)
		return queueDefault
	}

	// ── Tier 3: DOT node harness attribute ────────────────────────────────────
	//
	// Stub: hk-u67of (parse DOT node harness attribute) is not yet landed.
	// When it lands, the caller passes the node's harness attribute here.
	// Until then, nodeDefault is always empty and falls through.
	if nodeDefault.Valid() {
		emitHarnessSelected(ctx, bus, bead, nodeDefault, 3)
		return nodeDefault
	}

	// ── Tier 4: global Config.DefaultHarness / built-in fallback ──────────────
	if globalDefault.Valid() {
		emitHarnessSelected(ctx, bus, bead, globalDefault, 4)
		return globalDefault
	}

	// Built-in fallback: claude-code (the only harness until codex is wired in C5).
	emitHarnessSelected(ctx, bus, bead, core.AgentTypeClaudeCode, 4)
	return core.AgentTypeClaudeCode
}

// resolveHarnessAgentTypeQuiet resolves the harness agent-type using the same
// four-tier precedence walk as resolveHarness, but emits NO events (neither
// harness_selected nor bead_label_conflict).
//
// It exists so the claim-time model-preference resolution (workloop.go) can learn
// the ACTUAL harness agent-type — and therefore the correct tier-3 model default —
// WITHOUT double-emitting the observability events that resolveHarness fires later
// when routedLaunchSpecBuilder runs. For any given (bead, queueDefault, nodeDefault,
// globalDefault) tuple this returns exactly the same AgentType resolveHarness would.
//
// Malformed / multiple / invalid tier-1 harness labels are treated as absent (the
// walk continues to the next tier), matching resolveHarness — the conflict event is
// still emitted by resolveHarness at launch, so nothing is lost.
//
// Bead: hk-pkugu (codename:pi-model-leak).
func resolveHarnessAgentTypeQuiet(
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
) core.AgentType {
	// Tier 1: exactly one valid harness:<agent-type> label.
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
	// Tier 2 / Tier 3 / Tier 4.
	if queueDefault.Valid() {
		return queueDefault
	}
	if nodeDefault.Valid() {
		return nodeDefault
	}
	if globalDefault.Valid() {
		return globalDefault
	}
	// Built-in fallback.
	return core.AgentTypeClaudeCode
}

// emitHarnessSelected emits a harness_selected event (hk-lr5t) recording which
// harness was chosen and at which tier. Best-effort: emit errors are silently
// discarded (the selection result is already determined before this call).
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
	_ = bus.Emit(ctx, core.EventTypeHarnessSelected, b)
}
