package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// TestHkLfrubDotPiLaunchPath_PinScopedOut_RoutesToOrnith is the ISOLATED e2e
// regression. A DOT node pins model="claude-sonnet-4-6" (as every workflow.dot
// node does) and resolves to the pi harness. The node model decision must scope
// the claude pin OUT, and the real launch path must then emit the configured pi
// model (ornith) in both the argv and models.json — never the claude pin.
func TestHkLfrubDotPiLaunchPath_PinScopedOut_RoutesToOrnith(t *testing.T) {
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

	nodeModel := daemon.ExportedNodeModelForHarness("", claudePin, core.AgentTypePi)
	if nodeModel != "" {
		t.Fatalf("nodeModelForHarness scoped the claude pin INTO a pi run: %q; want empty", nodeModel)
	}

	piCfg := projectconfig.PiHarnessConfig{
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

	badSpec, _, err := build(ctx, hkpkuguE2ERunCtx(t, t.TempDir(), claudePin))
	if err != nil {
		t.Fatalf("routed launch spec build (counterfactual): %v", err)
	}
	if got := hkpkuguE2EArgFlagValue(badSpec.Args, "--model"); got != claudePin {
		t.Fatalf("counterfactual sanity: expected leaked --model %q, got %q — the launch path is not threading rc.model, assertions above would be vacuous", claudePin, got)
	}

	if got := daemon.ExportedNodeModelForHarness("", claudePin, core.AgentTypeClaudeCode); got != claudePin {
		t.Errorf("nodeModelForHarness dropped the pin for a claude node: %q; want %q", got, claudePin)
	}
}
