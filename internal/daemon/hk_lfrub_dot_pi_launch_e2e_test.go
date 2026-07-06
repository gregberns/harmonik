package daemon_test

// hk_lfrub_dot_pi_launch_e2e_test.go — ISOLATED end-to-end regression for the DOT
// per-node model= leak (hk-lfrub, codename:pi-model-leak), driving the REAL daemon
// launch path (nodeModelForHarness → routedLaunchSpecBuilder → PiHarness.LaunchSpec
// → buildPiLaunchSpec + buildPiModelsJSON).
//
// # What "real launch path" means here
//
// This composes the two production seams that together produced the bug:
//  1. The DOT cascade's per-node model decision (nodeModelForHarness), fed the
//     exact pin every sonnet-triple-review workflow.dot node carries
//     (model="claude-sonnet-4-6") and the pi effective harness.
//  2. The real routed launch path, fed the model that decision produces — the
//     same builder dispatchDotAgenticNode uses — asserting the pi argv and the
//     generated models.json carry the configured pi model (ornith), never the
//     leaked claude pin.
//
// Nothing is stubbed except temp dirs and the dummy provider key.
//
// Helper prefix: hklfrubE2E. Reuses hkpkuguE2E* helpers (same package).
//
// Bead: hk-lfrub.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestHkLfrubDotPiLaunchPath_PinScopedOut_RoutesToOrnith is the ISOLATED e2e
// regression. A DOT node pins model="claude-sonnet-4-6" (as every workflow.dot
// node does) and resolves to the pi harness. The node model decision must scope
// the claude pin OUT, and the real launch path must then emit the configured pi
// model (ornith) in both the argv and models.json — never the claude pin.
func TestHkLfrubDotPiLaunchPath_PinScopedOut_RoutesToOrnith(t *testing.T) {
	// Not t.Parallel: hermetic HOME for the PI-042 on-disk credential check.
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-lfrub-e2e-bead",
		Title:  "dot model-pin pi-leak e2e bead",
		Labels: nil,
	}

	const (
		claudePin = "claude-sonnet-4-6" // the pin every sonnet-triple-review node carries
		wantModel = "ornith"
	)

	// ── Seam 1: the DOT per-node model decision, verbatim ──────────────────────
	// Run-level resolvedModel is empty for a pi run (the pi harness has no tier-3
	// default). effHarness = pi → the claude pin MUST be scoped out.
	nodeModel := daemon.ExportedNodeModelForHarness("", claudePin, core.AgentTypePi)
	if nodeModel != "" {
		t.Fatalf("nodeModelForHarness scoped the claude pin INTO a pi run: %q; want empty", nodeModel)
	}

	// ── Seam 2: the real routed launch path, fed the scoped model ──────────────
	piCfg := daemon.PiHarnessConfig{
		Provider:   "ornith",
		Model:      wantModel,
		APIKeyEnv:  "HK_LFRUB_PI_KEY",
		APIKeyFile: hkpkuguE2EKeyFile(t),
		BaseURL:    "http://127.0.0.1:8551/v1",
		API:        "openai-completions",
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypePi,
		bus,
	)
	ws := t.TempDir()
	spec, _, err := build(ctx, hkpkuguE2ERunCtx(t, ws, nodeModel))
	if err != nil {
		t.Fatalf("routed launch spec build (pi, scoped pin): %v", err)
	}

	if got := hkpkuguE2EArgFlagValue(spec.Args, "--model"); got != wantModel {
		t.Errorf("pi argv --model = %q; want %q\nargv=%v", got, wantModel, spec.Args)
	}
	if hkpkuguE2EArgsContain(spec.Args, claudePin) {
		t.Errorf("pi argv leaked the claude DOT pin %q — the bug is back\nargv=%v", claudePin, spec.Args)
	}
	modelsPath := filepath.Join(ws, ".harmonik", "pi-agent", "models.json")
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("read generated models.json: %v", err)
	}
	if body := string(data); !strings.Contains(body, wantModel) || strings.Contains(body, claudePin) {
		t.Errorf("models.json wrong model: want %q present and %q absent\n%s", wantModel, claudePin, body)
	}

	// ── Adversarial counterfactual: prove the seam is real ─────────────────────
	// Had nodeModelForHarness NOT scoped the pin (the pre-fix behaviour), the pin
	// would seal into rc.model and the SAME launch path would carry claude-sonnet-4-6
	// into the pi argv — exactly the failure mode. Feed the unscoped pin directly
	// to prove the assertions above are not vacuous.
	badSpec, _, err := build(ctx, hkpkuguE2ERunCtx(t, t.TempDir(), claudePin))
	if err != nil {
		t.Fatalf("routed launch spec build (counterfactual): %v", err)
	}
	if got := hkpkuguE2EArgFlagValue(badSpec.Args, "--model"); got != claudePin {
		t.Fatalf("counterfactual sanity: expected leaked --model %q, got %q — the launch path is not threading rc.model, assertions above would be vacuous", claudePin, got)
	}

	// And prove the pin IS still honored for a claude effective harness (no regression).
	if got := daemon.ExportedNodeModelForHarness("", claudePin, core.AgentTypeClaudeCode); got != claudePin {
		t.Errorf("nodeModelForHarness dropped the pin for a claude node: %q; want %q", got, claudePin)
	}
}
