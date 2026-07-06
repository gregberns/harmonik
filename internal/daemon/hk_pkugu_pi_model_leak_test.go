package daemon_test

// hk_pkugu_pi_model_leak_test.go — regression test for the pi-model-leak bug
// (hk-pkugu, codename:pi-model-leak).
//
// # The bug
//
// At claim time, workloop.go resolved the (model, effort) preference with the
// agentType HARDCODED to core.AgentTypeClaudeCode. That sealed the claude tier-3
// default (claude-sonnet-4-6 / "sonnet") into rc.model even for a pi-resolved run.
// effectiveModel() then returned that leaked claude model for a *PiHarness, so the
// daemon asked the pi provider for a claude model → provider error → the run exited
// in ~3s without advancing HEAD.
//
// # The fix
//
// workloop resolves the harness agent-type up front (resolveHarnessAgentTypeQuiet,
// mirroring resolveHarness's precedence walk without emitting events) and passes
// THAT into ResolveModelPreference. For a pi-resolved run the tier-3 default is
// empty (pi has no defaultModelEntries entry), so resolvedModel is empty and
// effectiveModel correctly falls back to the configured pi model.
//
// Helper prefix: hkpkugu (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-pkugu.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hkpkuguBead builds a minimal BeadRecord with the given labels.
func hkpkuguBead(labels []string) core.BeadRecord {
	return core.BeadRecord{BeadID: core.BeadID("hk-pkugu-test"), Labels: labels}
}

// TestPiModelLeak_QuietResolutionMatchesResolveHarness verifies the quiet resolver
// (used at claim time for model resolution) returns exactly the same AgentType as
// resolveHarness (used at launch), for the tier-1-label and tier-4-global cases the
// production callsite exercises (queue/node defaults are always "").
func TestPiModelLeak_QuietResolutionMatchesResolveHarness(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		labels        []string
		globalDefault core.AgentType
		want          core.AgentType
	}{
		{"global pi, no label", nil, core.AgentTypePi, core.AgentTypePi},
		{"global claude, no label", nil, core.AgentTypeClaudeCode, core.AgentTypeClaudeCode},
		{"global empty → fallback claude", nil, core.AgentType(""), core.AgentTypeClaudeCode},
		{"tier-1 pi label overrides global claude", []string{"harness:pi"}, core.AgentTypeClaudeCode, core.AgentTypePi},
		{"tier-1 claude label overrides global pi", []string{"harness:claude-code"}, core.AgentTypePi, core.AgentTypeClaudeCode},
		{"empty label value treated as absent → global pi", []string{"harness:"}, core.AgentTypePi, core.AgentTypePi},
		{"multiple labels treated as absent → global pi", []string{"harness:pi", "harness:codex"}, core.AgentTypePi, core.AgentTypePi},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bead := hkpkuguBead(tc.labels)

			quiet := daemon.ExportedResolveHarnessAgentTypeQuiet(
				bead, core.AgentType(""), core.AgentType(""), tc.globalDefault,
			)
			if quiet != tc.want {
				t.Errorf("quiet resolution = %q; want %q", quiet, tc.want)
			}

			// Must equal the launch-time resolveHarness result for the same inputs.
			launch := daemon.ExportedResolveHarness(
				context.Background(), bead,
				core.AgentType(""), core.AgentType(""), tc.globalDefault,
				eventbus.NewBusImpl(),
			)
			if quiet != launch {
				t.Errorf("quiet resolution %q != launch resolveHarness %q — model default will diverge from harness",
					quiet, launch)
			}
		})
	}
}

// TestPiModelLeak_PiRunDoesNotInheritClaudeDefault is the end-to-end regression:
// a pi-resolved run (global default = pi, no explicit model: label) must NOT seal
// the claude tier-3 default into the model. resolvedModel must be empty so that
// effectiveModel falls back to the configured pi model ("ornith"). A claude-resolved
// run must still get the claude tier-3 default ("sonnet").
func TestPiModelLeak_PiRunDoesNotInheritClaudeDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := hkpkuguBead(nil) // no model:/effort:/harness: labels

	// ── Pi-resolved run (global default harness = pi) ──────────────────────────
	piAgentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if piAgentType != core.AgentTypePi {
		t.Fatalf("pi agentType = %q; want pi", piAgentType)
	}

	piModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, piAgentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if piModel != "" {
		t.Errorf("pi-resolved run leaked model %q; want empty (no pi tier-3 default → config fallback)", piModel)
	}

	// effectiveModel for a PiHarness with the leaked-empty model must fall back to
	// the configured pi model.
	piH := daemon.ExportedNewPiHarness("pi", "ornith-provider", "ornith", "PI_KEY", "", "", "openai-completions")
	if got := daemon.ExportedEffectiveModel(piH, piModel); got != "ornith" {
		t.Errorf("effectiveModel(pi, %q) = %q; want %q (configured pi model)", piModel, got, "ornith")
	}

	// Guard against regression: the OLD hardcoded-claude behavior would have sealed
	// "sonnet" here, which effectiveModel would then return instead of "ornith".
	if daemon.ExportedEffectiveModel(piH, "sonnet") == "ornith" {
		t.Fatal("sanity: a non-empty rc.model should override the pi config fallback")
	}

	// ── Claude-resolved run (global default harness = claude-code) ─────────────
	claudeAgentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode,
	)
	claudeModel, claudeEffort := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, claudeAgentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if claudeModel != "sonnet" {
		t.Errorf("claude-resolved run model = %q; want %q (claude tier-3 default unchanged)", claudeModel, "sonnet")
	}
	if claudeEffort != "medium" {
		t.Errorf("claude-resolved run effort = %q; want %q (claude tier-3 default unchanged)", claudeEffort, "medium")
	}
}
