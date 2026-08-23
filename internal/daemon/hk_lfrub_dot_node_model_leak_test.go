package daemon_test

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

	piH := daemon.ExportedNewPiHarness("pi", "ornith-provider", "ornith", "PI_KEY", "", "", "openai-completions")

	piNodeModel := daemon.ExportedNodeModelForHarness(runResolved, claudePin, core.AgentTypePi)
	if piNodeModel != "" {
		t.Errorf("pi node: model= pin leaked into rc.model = %q; want empty (run-level resolvedModel)", piNodeModel)
	}
	if got := daemon.ExportedEffectiveModel(piH, piNodeModel); got != "ornith" {
		t.Errorf("pi node: effectiveModel = %q; want %q (configured pi model — no claude leak)", got, "ornith")
	}

	if leaked := daemon.ExportedEffectiveModel(piH, claudePin); leaked != claudePin {
		t.Fatalf("sanity: a non-empty rc.model must override the pi config fallback (got %q)", leaked)
	}

	if codexNodeModel := daemon.ExportedNodeModelForHarness(runResolved, claudePin, core.AgentTypeCodex); codexNodeModel != "" {
		t.Errorf("codex node: model= pin leaked into rc.model = %q; want empty", codexNodeModel)
	}

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
