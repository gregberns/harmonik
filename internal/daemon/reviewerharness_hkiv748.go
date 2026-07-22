package daemon

// reviewerharness_hkiv748.go — reviewer harness resolution (codex-harness C5/T14, hk-iv748).
//
// Implements the reviewer harness precedence for the builtin review-loop and the
// DOT cascade:
//
//   DEFAULT: reviewer uses the SAME resolved harness as the implementer for that run.
//   OPTIONAL OVERRIDE: driven by the reviewer_harness DOT node attribute (parsed by
//     T5 hk-u67of into dot.Node.ReviewerHarness). When present on the IMPLEMENTER node,
//     the reviewer uses that harness even when the implementer used a different one
//     (e.g. codex-implemented bead reviewed by claude).
//
// Precedence walk for the reviewer (DOT mode):
//  1. reviewerHarnessOverride — implementer node's reviewer_harness= attr, if valid.
//  2. node.Harness — reviewer node's own harness= attr, if valid.
//  3. deps.launchSpecBuilder — DEFAULT: same resolved harness as the implementer.
//
// Review-loop mode (runReviewLoop):
//   The reviewer specBuilder is built with nodeDefault = implArtifacts.resolvedAgentType
//   (the implementer's resolved harness). For all-claude runs this is byte-identical to
//   pre-T14 behaviour. No DOT reviewer_harness override is applicable in review-loop mode
//   (no DOT node exists); that override is handled in dispatchDotAgenticNode.
//
// hk-pkxju amends the DEFAULT (inherited) leg of both walks: a reviewer must never
// INHERIT a SessionIDCaptured harness (codex, pi). See reviewerDefaultHarness below.
// The EXPLICIT legs (reviewer_harness= override, reviewer node's own harness= attr)
// are untouched — an operator pin stays an operator pin.
//
// Spec ref: codex-harness C5, T14.
// Bead: hk-iv748 [C5/T14], hk-pkxju [reviewer-never-inherits-captured]

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
func reviewerDefaultHarness(
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
//   - the harness the reviewer would therefore inherit (the same tier-1/tier-4 walk
//     deps.launchSpecBuilder performs, run quietly) is SessionIDCaptured.
//
// In every other case it returns the empty AgentType and the caller's existing
// precedence stands untouched. In particular an EXPLICIT operator pin — legs 1 and 2
// of the T14 walk — is never clobbered, even when it points at a SessionIDCaptured
// harness: that is the seam the "teach codex to review" fast-follow will use.
//
// Bead: hk-pkxju.
func dotReviewerInheritedHarnessOverride(
	reg *handlercontract.HarnessRegistry,
	isReviewer bool,
	reviewerHarnessOverride core.AgentType,
	nodeHarness core.AgentType,
	bead core.BeadRecord,
	globalDefault core.AgentType,
	beadID string,
) core.AgentType {
	if !isReviewer || reg == nil {
		return core.AgentType("")
	}
	if reviewerHarnessOverride.Valid() || nodeHarness.Valid() {
		return core.AgentType("") // explicit operator pin — leave it alone
	}
	inherited := resolveHarnessAgentTypeQuiet(
		bead,
		core.AgentType(""), // queue default (hk-4x3rg not landed)
		core.AgentType(""), // node default: absent, that is this branch's premise
		globalDefault,
	)
	fallback := reviewerDefaultHarness(reg, inherited, beadID)
	if fallback == inherited {
		return core.AgentType("") // nothing to correct; keep deps.launchSpecBuilder
	}
	return fallback
}
