package daemon

// codexmodeguard_hkofm89.go — the DOT-only guard for per-bead harness labels.
//
// THE PROPERTY. A per-bead tier-1 harness:<agent-type> label that resolves to a
// SessionIDCaptured harness (codex, pi) is safe ONLY when the bead dispatches
// through the DOT cascade. There, reviewer nodes carry an explicit harness=
// attr and pinnedHarnessLaunchSpecBuilder makes that pin win unconditionally, so
// the implementer runs on codex and the reviewers stay claude — the split the
// codex ramp is built on.
//
// WHY A GUARD AND NOT A COMMENT. The control this replaces is a two-part manual
// check before every ramp wave (plans/2026-07-21-codex-first/RAMP-PLAN.md §1a):
// confirm the global workflow_mode is dot, and confirm the bead carries no
// workflow:<mode> label. That is a procedural control over a SILENT failure, and
// procedural controls over silent failures get skipped — most likely once they
// have worked a few times and start to feel like ceremony.
//
// WHAT IT DOES IN EACH NON-DOT MODE, and the split is deliberate — the two modes
// fail differently, so treating them alike would either under-protect or break a
// legitimate flow:
//
//   - review-loop — REFUSE. This is the mode with a reviewer but no way to pin it:
//     no DOT node exists to carry a harness= attr, so the reviewer's harness comes
//     from inheritance. Two fixes already close it (reviewloop.go routes the
//     default through reviewerDefaultHarness, hk-pkxju, and the reviewer's spec
//     builder is handed an empty bead record so the tier-1 label cannot reach it),
//     but both are runtime fallbacks living in three files. This is one refusal at
//     the door, and it holds if either regresses.
//
//   - single — AUDIT, do not refuse. Single mode has NO reviewer at all: it emits
//     review_bypassed and closes. There is nothing to hijack, and the missing
//     review is what the operator's explicit workflow:single label ASKED for. An
//     earlier draft of this guard refused it as "codex work closing unreviewed";
//     that is a policy question, not this bead's safety property, and refusing it
//     would break a live fixture — scenarios/core-loop-proof/seed-beads.json runs
//     the pi arm on exactly harness:pi + workflow:single by design. So the guard
//     records a codex_mode_guard{outcome: audited} event and lets the run proceed,
//     which makes "captured harness landed work unreviewed" searchable instead of
//     silent. Flipping this to a refusal is a one-line change here plus a fixture
//     update, and it needs an explicit ramp decision, not a code review.
//
// SCOPE, deliberately narrow. The guard fires only on a TIER-1 LABEL. A daemon
// whose GLOBAL default harness is codex (--default-harness codex, tier 4) running
// in a non-dot mode has the same exposure, and is deliberately not covered: that
// is an explicit operator-wide posture rather than a per-bead ramp action, and
// refusing every bead on such a daemon would be a different decision from the one
// this bead authorizes. Worth revisiting if the ramp ever flips a global default.
//
// Bead: hk-ofm89. Reference: plans/2026-07-21-codex-first/RAMP-PLAN.md §§1, 1a, 3.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// codexModeGuardVerdict is the guard's decision for one bead: dispatch silently,
// dispatch with the situation recorded, or refuse.
type codexModeGuardVerdict struct {
	// Refuse stops the dispatch. False for an audited verdict, which still emits.
	Refuse bool

	// Emit is true when the guard has something to record — a refusal, or an
	// audited captured-harness run in single mode. False means the guard is silent
	// and the bead dispatches exactly as it did before the guard existed.
	Emit bool

	// Outcome is the recorded outcome (refused | audited); meaningful when Emit.
	Outcome core.CodexModeGuardOutcome

	// Label is the tier-1 harness:<agent-type> label verbatim as it appears on
	// the bead — what an operator would remove to make the bead dispatchable.
	Label string

	// AgentType is the agent type Label resolved to.
	AgentType core.AgentType

	// Reason is the human-readable refusal cause, used as both the event's reason
	// field and the run's terminal failure reason.
	Reason string
}

// evaluateCodexModeGuard decides what to do with one bead.
//
// It engages when ALL of these hold:
//
//	(a) the bead carries exactly one harness:<agent-type> label whose value is a
//	    valid agent type — the same tier-1 shape resolveHarness accepts, so a
//	    malformed or duplicated label is tier-1-absent here exactly as it is
//	    there and never trips the guard;
//	(b) that agent type resolves, in the live harness registry, to a harness whose
//	    SessionIDPolicy is SessionIDCaptured;
//	(c) the RESOLVED workflow mode is not dot.
//
// It then REFUSES in review-loop and AUDITS (emit, allow) in single, for the
// reasons in this file's header. Every other input is a silent pass.
//
// (b) is a registry lookup rather than a name comparison on purpose: the property
// that makes these harnesses unsafe outside DOT is the captured-session policy —
// no reviewer seed prompt, no agent_ready — not the string "codex". Pi has the
// same policy and the same exposure, and any future captured harness inherits the
// guard without an edit here.
//
// A nil registry, an unregistered agent type, or a lookup error all FAIL OPEN
// (no refusal). This matches reviewerDefaultHarness: the guard is a safety net
// over a boundary that is otherwise procedural, and a daemon with a broken
// registry has larger problems than this bead's routing. Failing closed here
// would turn a registry defect into a total dispatch outage.
func evaluateCodexModeGuard(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	resolvedMode core.WorkflowMode,
) codexModeGuardVerdict {
	if resolvedMode == core.WorkflowModeDot {
		return codexModeGuardVerdict{}
	}

	// (a) tier-1 label, same shape resolveHarness accepts.
	var harnessLabels []string
	for _, lbl := range bead.Labels {
		if strings.HasPrefix(lbl, harnessLabelPrefix) {
			harnessLabels = append(harnessLabels, lbl)
		}
	}
	if len(harnessLabels) != 1 {
		return codexModeGuardVerdict{}
	}
	label := harnessLabels[0]
	at := core.AgentType(strings.TrimPrefix(label, harnessLabelPrefix))
	if !at.Valid() {
		return codexModeGuardVerdict{}
	}

	// (b) captured-session policy — the property, not the name.
	if reg == nil {
		return codexModeGuardVerdict{}
	}
	h, err := reg.ForAgent(at)
	if err != nil {
		return codexModeGuardVerdict{}
	}
	if h.SessionIDPolicy() != handlercontract.SessionIDCaptured {
		return codexModeGuardVerdict{}
	}

	switch resolvedMode {
	case core.WorkflowModeReviewLoop:
		return codexModeGuardVerdict{
			Refuse:    true,
			Emit:      true,
			Outcome:   core.CodexModeGuardRefused,
			Label:     label,
			AgentType: at,
			Reason: fmt.Sprintf(
				"codex_mode_guard: bead carries %q but its resolved workflow mode is %q, not dot; "+
					"a captured-session harness cannot review and review-loop mode has no node pin to "+
					"stop the reviewer inheriting it (hk-ofm89). Remove the harness label, or remove the "+
					"per-bead workflow: label / restore workflow_mode: dot so the bead runs through DOT",
				label, resolvedMode),
		}
	case core.WorkflowModeSingle:
		return codexModeGuardVerdict{
			Emit:      true,
			Outcome:   core.CodexModeGuardAudited,
			Label:     label,
			AgentType: at,
			Reason: fmt.Sprintf(
				"codex_mode_guard: bead carries %q in resolved workflow mode %q, which has no review "+
					"node — this run's work lands unreviewed by explicit request (hk-ofm89). Allowed, "+
					"recorded so it is searchable; use dot mode if the work should be reviewed",
				label, resolvedMode),
		}
	default:
		// A mode neither dot, review-loop nor single does not exist today. Stay
		// silent rather than guess at a mode whose review semantics are unknown.
		return codexModeGuardVerdict{}
	}
}

// emitCodexModeGuard emits the codex_mode_guard event for a refusal.
//
// Best-effort in the same sense as the neighbouring guard emitters: an emit
// failure is logged and swallowed rather than escalated, because the refusal
// itself is already carried by the run's terminal failure reason — the operator
// is not left guessing even if the event never lands.
func emitCodexModeGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID core.BeadID,
	verdict codexModeGuardVerdict,
	resolvedMode core.WorkflowMode,
) {
	if bus == nil {
		return
	}
	pl := core.CodexModeGuardPayload{
		RunID:        runID.String(),
		BeadID:       string(beadID),
		HarnessLabel: verdict.Label,
		AgentType:    string(verdict.AgentType),
		ResolvedMode: string(resolvedMode),
		Outcome:      verdict.Outcome,
		Reason:       verdict.Reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeCodexModeGuard, b); emitErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: codex_mode_guard: emit failed for bead %s run %s: %v (refusal still applies)\n",
			beadID, runID.String(), emitErr)
	}
}
