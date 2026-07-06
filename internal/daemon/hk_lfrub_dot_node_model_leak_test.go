package daemon_test

// hk_lfrub_dot_node_model_leak_test.go — regression test for the DOT per-node
// model= leak (hk-lfrub, codename:pi-model-leak).
//
// # The bug
//
// The active project workflow (sonnet-triple-review workflow.dot) pins
// model="claude-sonnet-4-6" as a per-node DOT attribute on EVERY node. In the DOT
// cascade the per-node model= override was applied unconditionally:
//
//	nodeModel := resolvedModel
//	if node.Model != "" { nodeModel = node.Model }  // pins claude-sonnet-4-6
//
// For a pi-resolved run the run-level resolvedModel is correctly empty (the sibling
// hk-pkugu claim-time fix), but this per-node pin then re-seals the claude model
// into rc.model. effectiveModel() for a *PiHarness returns rc.model when non-empty,
// so every DOT-path pi run asked the ornith/DGX provider for claude-sonnet-4-6 and
// failed. A single-mode canary (which bypasses the DOT cascade) resolved
// pi/ornith correctly; every DOT-path pi canary showed pi/claude-sonnet-4-6.
//
// # The fix
//
// nodeModelForHarness applies the node model= pin ONLY when the node's effective
// harness is the claude-code family. For a pi/codex effective harness rc.model is
// left = the run-level resolvedModel (empty for a pi run), so effectiveModel()
// falls through to the configured pi model (ornith). effort= stays harness-agnostic
// and is unaffected.
//
// Helper prefix: hklfrub (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-lfrub.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestDotNodeModelLeak_PinScopedToClaudeHarness is the focused regression: a DOT
// node carrying model=claude-sonnet-4-6 must NOT leak that pin into rc.model when
// the node's effective harness is pi. The resulting model must route (via
// effectiveModel) to the configured pi model (ornith), NOT claude-sonnet-4-6. A
// claude effective harness must still honor the pin.
func TestDotNodeModelLeak_PinScopedToClaudeHarness(t *testing.T) {
	t.Parallel()

	const (
		claudePin   = "claude-sonnet-4-6"
		runResolved = "" // run-level resolvedModel for a pi run (hk-pkugu: empty)
	)

	// A *PiHarness configured with the ornith model. effectiveModel returns
	// rc.model when non-empty, else this configured model.
	piH := daemon.ExportedNewPiHarness("pi", "ornith-provider", "ornith", "PI_KEY", "", "", "openai-completions")

	// ── Pi effective harness: the claude pin must be DROPPED ────────────────────
	piNodeModel := daemon.ExportedNodeModelForHarness(runResolved, claudePin, core.AgentTypePi)
	if piNodeModel != "" {
		t.Errorf("pi node: model= pin leaked into rc.model = %q; want empty (run-level resolvedModel)", piNodeModel)
	}
	if got := daemon.ExportedEffectiveModel(piH, piNodeModel); got != "ornith" {
		t.Errorf("pi node: effectiveModel = %q; want %q (configured pi model — no claude leak)", got, "ornith")
	}

	// Guard against regression: the OLD unconditional pin would have sealed the
	// claude model here, which effectiveModel would then return for the pi harness.
	if leaked := daemon.ExportedEffectiveModel(piH, claudePin); leaked != claudePin {
		t.Fatalf("sanity: a non-empty rc.model must override the pi config fallback (got %q)", leaked)
	}

	// ── Codex effective harness: the claude pin must also be dropped ────────────
	if codexNodeModel := daemon.ExportedNodeModelForHarness(runResolved, claudePin, core.AgentTypeCodex); codexNodeModel != "" {
		t.Errorf("codex node: model= pin leaked into rc.model = %q; want empty", codexNodeModel)
	}

	// ── Claude effective harness: the pin is STILL honored ──────────────────────
	claudeNodeModel := daemon.ExportedNodeModelForHarness(runResolved, claudePin, core.AgentTypeClaudeCode)
	if claudeNodeModel != claudePin {
		t.Errorf("claude node: model= pin = %q; want %q (claude nodes still honor the pin)", claudeNodeModel, claudePin)
	}
}

// TestDotNodeModelLeak_NoPinInheritsRunLevel verifies the no-op case: a node with
// NO model= attribute inherits the run-level resolvedModel regardless of harness
// family (the pin gate only affects a NON-empty node.Model).
func TestDotNodeModelLeak_NoPinInheritsRunLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		resolved    string
		harness     core.AgentType
		wantInherit string
	}{
		{"pi, empty run-level → empty", "", core.AgentTypePi, ""},
		{"claude, run-level sonnet inherited", "sonnet", core.AgentTypeClaudeCode, "sonnet"},
		{"pi, run-level value inherited verbatim", "some-pi-model", core.AgentTypePi, "some-pi-model"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := daemon.ExportedNodeModelForHarness(tc.resolved, "", tc.harness); got != tc.wantInherit {
				t.Errorf("no-pin node: got %q; want %q (inherit run-level resolvedModel)", got, tc.wantInherit)
			}
		})
	}
}
