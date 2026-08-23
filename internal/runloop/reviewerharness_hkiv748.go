package runloop

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// reviewerDefaultHarness applies the hk-pkxju rule to the DEFAULT (inherited)
// reviewer harness: a reviewer never inherits a SessionIDCaptured harness.
//
// Why: a SessionIDCaptured harness (codex, pi) cannot review today. Two concrete
// gaps, both observed under HARMONIK_SUBSTRATE=codexdriver:
//   - codexlaunchspec.go builds ONLY an implementer seed prompt
//     (codexSeedPromptTemplate = "read .harmonik/agent-task.md, implement…"); there is
//     no reviewer-phase branch, so a codex "reviewer" runs IMPLEMENT instructions and
//     never writes .harmonik/review.json.
//   - codex never emits agent_ready, but the reviewer dispatch blocks on it → the
//     REVIEW node always fails with "reviewer agent_ready_timeout" and the bead never
//     closes.
//
// So when the harness the reviewer would INHERIT is SessionIDCaptured, fall back to
// the claude harness (core.AgentTypeClaudeCode). Teaching a SessionIDCaptured harness
// to REVIEW is an explicit fast-follow; until then the explicit pins
// (reviewer_harness= / the reviewer node's own harness=) remain the only way to point
// a reviewer at one, and this helper is deliberately NOT consulted on those paths.
//
// When implementer is claude (SessionIDMinted) the return is implementer unchanged,
// so all-claude runs are byte-identical to pre-hk-pkxju behaviour. A nil registry, an
// invalid agent type, or an unregistered agent type also return implementer unchanged
// (fail open — the caller's existing error handling still applies).
//
// beadID is used only for the stderr breadcrumb, matching the neighbouring
// "daemon: reviewloop: …" / "daemon: dot: …" logging idiom. The harness_selected
// event still records the ACTUAL selection: both call sites feed the returned agent
// type back into routedLaunchSpecBuilder / pinnedHarnessLaunchSpecBuilder, which emit
// harness_selected at tier 3 with the fallback value.
//
// Bead: hk-pkxju.
// ReviewerDefaultHarness applies the inherited-reviewer fallback.
//
// Exported for a caller in internal/daemon. The comment here used to name
// reviewloop.go as that caller and to promise a lift that would let this narrow
// back to an unexported name. That file is deleted, so re-derive the remaining
// caller before acting on the narrowing.
func ReviewerDefaultHarness(
	reg *handlercontract.HarnessRegistry,
	implementer core.AgentType,
	beadID string,
) core.AgentType {
	if reg == nil || !implementer.Valid() {
		return implementer
	}
	h, err := reg.ForAgent(implementer)
	if err != nil {
		return implementer
	}
	if h.SessionIDPolicy() != handlercontract.SessionIDCaptured {
		return implementer
	}
	fmt.Fprintf(os.Stderr,
		"daemon: reviewer harness: hk-pkxju: implementer harness %q is SessionIDCaptured "+
			"and cannot review; reviewer falls back to %q (bead %s)\n",
		implementer, core.AgentTypeClaudeCode, beadID)
	return core.AgentTypeClaudeCode
}

// dotReviewerInheritedHarnessOverride is the DOT-cascade adapter for the hk-pkxju
// rule. It returns a non-empty AgentType ONLY when all of the following hold:
//
//   - the node being dispatched is the reviewer, AND
//   - the reviewer is on the DEFAULT/INHERITED leg — neither the implementer node's
//     reviewer_harness= override nor the reviewer node's own harness= attr is valid, AND
//   - the harness the reviewer would therefore inherit (the same tier-1/tier-2/tier-4 walk
//     deps.launchSpecBuilder performs, run quietly) is SessionIDCaptured.
//
// In every other case it returns the empty AgentType and the caller's existing
// precedence stands untouched. In particular an EXPLICIT operator pin — legs 1 and 2
// of the T14 walk — is never clobbered, even when it points at a SessionIDCaptured
// harness: that is the seam the "teach codex to review" fast-follow will use.
//
// Bead: hk-pkxju.
// DotReviewerInheritedHarnessOverride applies the inherited-reviewer fallback
// to a DOT reviewer.
//
// Temporary export: dot_gate.go and dot_cascade.go still call this from
// internal/daemon. LIFT L9 moves the gate caller and LIFT L12 moves the cascade
// caller; after LIFT L12 this narrows back to
// dotReviewerInheritedHarnessOverride.
func DotReviewerInheritedHarnessOverride(
	reg *handlercontract.HarnessRegistry,
	resolveQuiet func(
		core.BeadRecord,
		core.AgentType,
		core.AgentType,
		core.AgentType,
	) core.AgentType,
	isReviewer bool,
	reviewerHarnessOverride core.AgentType,
	nodeHarness core.AgentType,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	globalDefault core.AgentType,
	beadID string,
) core.AgentType {
	if !isReviewer || reg == nil || resolveQuiet == nil {
		return core.AgentType("")
	}
	if reviewerHarnessOverride.Valid() || nodeHarness.Valid() {
		return core.AgentType("") // explicit operator pin — leave it alone
	}
	inherited := resolveQuiet(
		bead,
		queueDefault,
		core.AgentType(""), // node default: absent, that is this branch's premise
		globalDefault,
	)
	fallback := ReviewerDefaultHarness(reg, inherited, beadID)
	if fallback == inherited {
		return core.AgentType("") // nothing to correct; keep deps.launchSpecBuilder
	}
	return fallback
}
