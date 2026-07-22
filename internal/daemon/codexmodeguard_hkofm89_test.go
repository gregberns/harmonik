package daemon_test

// codexmodeguard_hkofm89_test.go — the DOT-only guard for per-bead harness labels.
//
// These cases bind to evaluateCodexModeGuard via ExportedEvaluateCodexModeGuard,
// i.e. the production decision function, not a restatement of its rules.
//
// The case that matters most is the per-bead defeat vector: a workflow: label
// defeats the DOT boundary for one bead alone, with the global config untouched
// and looking correct — which is why the guard keys on the RESOLVED mode the
// caller passes, never on the daemon's configured default.
//
// The single-mode cases pin a decision, not just a behaviour: a captured harness
// in single mode is AUDITED, not refused. Single mode has no reviewer to hijack,
// the missing review is what the explicit workflow:single label asked for, and
// scenarios/core-loop-proof/seed-beads.json runs its pi arm on exactly
// harness:pi + workflow:single. A test that asserted a refusal there would be
// pinning a regression.
//
// Bead: hk-ofm89.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func guardBead(labels ...string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:   core.BeadID("hk-guard1"),
		Title:    "guard fixture",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   labels,
	}
}

func TestCodexModeGuard_RefusesCapturedHarnessOutsideDot_hkofm89(t *testing.T) {
	t.Parallel()

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	tests := []struct {
		name        string
		bead        core.BeadRecord
		mode        core.WorkflowMode
		wantRefuse  bool
		wantEmit    bool
		wantOutcome core.CodexModeGuardOutcome
		wantLabel   string
		wantAgent   core.AgentType
	}{
		{
			// Defence in depth: reviewerDefaultHarness (hk-pkxju) already stops the
			// reviewer inheriting codex here, but that is a runtime fallback in three
			// files and this is one refusal at the door.
			name:        "codex label in review-loop mode is refused",
			bead:        guardBead("codename:codex-first", "harness:codex"),
			mode:        core.WorkflowModeReviewLoop,
			wantRefuse:  true,
			wantEmit:    true,
			wantOutcome: core.CodexModeGuardRefused,
			wantLabel:   "harness:codex",
			wantAgent:   core.AgentTypeCodex,
		},
		{
			// THE PROPERTY IS THE POLICY, NOT THE NAME: pi is SessionIDCaptured too,
			// so it is covered without the guard ever naming it.
			name:        "pi label in review-loop mode is refused",
			bead:        guardBead("harness:pi"),
			mode:        core.WorkflowModeReviewLoop,
			wantRefuse:  true,
			wantEmit:    true,
			wantOutcome: core.CodexModeGuardRefused,
			wantLabel:   "harness:pi",
			wantAgent:   core.AgentTypePi,
		},
		{
			// AUDITED, NOT REFUSED — and this is the case that would break the
			// core-loop-proof pi arm if it were a refusal.
			name:        "codex label in single mode is audited and dispatches",
			bead:        guardBead("codename:codex-first", "harness:codex", "workflow:single"),
			mode:        core.WorkflowModeSingle,
			wantRefuse:  false,
			wantEmit:    true,
			wantOutcome: core.CodexModeGuardAudited,
			wantLabel:   "harness:codex",
			wantAgent:   core.AgentTypeCodex,
		},
		{
			// The exact shape of the core-loop-proof pi seed. If this ever refuses,
			// that fixture stops dispatching.
			name:        "pi label in single mode is audited and dispatches",
			bead:        guardBead("core-loop-seed", "harness:pi", "workflow:single"),
			mode:        core.WorkflowModeSingle,
			wantRefuse:  false,
			wantEmit:    true,
			wantOutcome: core.CodexModeGuardAudited,
			wantLabel:   "harness:pi",
			wantAgent:   core.AgentTypePi,
		},
		{
			// The whole point of the boundary: DOT is where reviewer nodes carry an
			// explicit harness pin, so the label is safe and the guard stays silent.
			name:       "codex label in dot mode dispatches silently",
			bead:       guardBead("harness:codex"),
			mode:       core.WorkflowModeDot,
			wantRefuse: false,
			wantEmit:   false,
		},
		{
			// A claude bead in single mode is the ordinary pre-existing case and must
			// stay byte-identical: claude is SessionIDMinted, not captured.
			name:       "claude label in single mode dispatches silently",
			bead:       guardBead("harness:claude-code", "workflow:single"),
			mode:       core.WorkflowModeSingle,
			wantRefuse: false,
			wantEmit:   false,
		},
		{
			name:       "no harness label in single mode dispatches silently",
			bead:       guardBead("codename:codex-first", "workflow:single"),
			mode:       core.WorkflowModeSingle,
			wantRefuse: false,
			wantEmit:   false,
		},
		{
			// Tier-1 shape parity with resolveHarness: two harness labels mean tier 1
			// is ABSENT there, so they must not trip the guard here either. If the
			// guard were stricter than the resolver it would refuse beads that would
			// not have run on codex at all.
			name:       "two harness labels are tier-1-absent and dispatch silently",
			bead:       guardBead("harness:codex", "harness:claude-code", "workflow:review-loop"),
			mode:       core.WorkflowModeReviewLoop,
			wantRefuse: false,
			wantEmit:   false,
		},
		{
			name:       "malformed harness label is tier-1-absent and dispatches silently",
			bead:       guardBead("harness:not-a-real-harness", "workflow:review-loop"),
			mode:       core.WorkflowModeReviewLoop,
			wantRefuse: false,
			wantEmit:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := daemon.ExportedEvaluateCodexModeGuard(reg, tc.bead, tc.mode)
			if got.Refuse != tc.wantRefuse {
				t.Fatalf("refuse = %v, want %v (reason %q)", got.Refuse, tc.wantRefuse, got.Reason)
			}
			if got.Emit != tc.wantEmit {
				t.Fatalf("emit = %v, want %v (reason %q)", got.Emit, tc.wantEmit, got.Reason)
			}
			if !tc.wantEmit {
				return
			}
			if got.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			label, agent, reason := got.Label, got.AgentType, got.Reason
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
			if agent != tc.wantAgent {
				t.Errorf("agentType = %q, want %q", agent, tc.wantAgent)
			}
			// The reason is operator-facing: it must name the label and the mode it
			// keyed on, or the event is a dead end for whoever reads it in the run's
			// failure reason or in the log.
			if !strings.Contains(reason, tc.wantLabel) {
				t.Errorf("reason %q does not name the label %q", reason, tc.wantLabel)
			}
			if !strings.Contains(reason, string(tc.mode)) {
				t.Errorf("reason %q does not name the resolved mode %q", reason, tc.mode)
			}
		})
	}
}

// TestCodexModeGuard_NilRegistryFailsOpen_hkofm89 pins the fail-open choice.
//
// A broken or absent registry must not become a total dispatch outage: the guard
// is a safety net over a boundary that is otherwise procedural, and a daemon
// whose registry cannot resolve codex has a larger problem than this bead's
// routing. This matches reviewerDefaultHarness, which fails open the same way.
func TestCodexModeGuard_NilRegistryFailsOpen_hkofm89(t *testing.T) {
	t.Parallel()

	got := daemon.ExportedEvaluateCodexModeGuard(
		nil, guardBead("harness:codex"), core.WorkflowModeReviewLoop)
	if got.Refuse || got.Emit {
		t.Fatalf("nil registry must fail open and stay silent; got refuse=%v emit=%v", got.Refuse, got.Emit)
	}
}
