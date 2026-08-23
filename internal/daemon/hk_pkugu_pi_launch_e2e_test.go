package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

func hkpkuguE2EArgFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hkpkuguE2EArgsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hkpkuguE2EKeyFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "pi.key")
	if err := os.WriteFile(f, []byte("dummy-pi-key-for-hk-pkugu\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return f
}

func hkpkuguE2ERunCtx(t *testing.T, ws, model string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "hk-pkugu-e2e-bead",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-pkugu-e2e.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		Model:         model,
		BaseEnv:       []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

// TestPkuguPiLaunchPath_EmitsConfiguredModelNotClaudeDefault is the ISOLATED e2e
// regression: a pi-resolved run (global default harness = pi, no explicit model
// label) drives the real routed launch path and MUST produce the configured pi
// model ("ornith") in both the argv and the generated models.json — never the
// leaked claude default.
func TestPkuguPiLaunchPath_EmitsConfiguredModelNotClaudeDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-pkugu-e2e-bead",
		Title:  "pi-model-leak e2e bead",
		Labels: nil, // no model:/harness: labels — the common flip case
	}

	const wantModel = "ornith"
	piCfg := projectconfig.PiHarnessConfig{
		Provider:   "ornith",
		Model:      wantModel,
		APIKeyEnv:  "HK_PKUGU_PI_KEY",
		APIKeyFile: hkpkuguE2EKeyFile(t),
		BaseURL:    "http://127.0.0.1:8551/v1",
		API:        "openai-completions",
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if agentType != core.AgentTypePi {
		t.Fatalf("resolved agentType = %q; want pi", agentType)
	}
	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, projectconfig.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "" {
		t.Fatalf("pi run sealed model = %q; want empty (no pi tier-3 default → config fallback)", sealedModel)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypePi,
		bus,
	)
	ws := t.TempDir()
	spec, _, err := build(ctx, hkpkuguE2ERunCtx(t, ws, sealedModel))
	if err != nil {
		t.Fatalf("routed launch spec build (pi): %v", err)
	}

	if got := hkpkuguE2EArgFlagValue(spec.Args, "--model"); got != wantModel {
		t.Errorf("pi argv --model = %q; want %q\nargv=%v", got, wantModel, spec.Args)
	}
	for _, leaked := range []string{"sonnet", "claude-sonnet-4-6"} {
		if hkpkuguE2EArgsContain(spec.Args, leaked) {
			t.Errorf("pi argv leaked claude model %q — the bug is back\nargv=%v", leaked, spec.Args)
		}
	}

	modelsPath := filepath.Join(ws, ".harmonik", "pi-agent", "models.json")
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("read generated models.json: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, wantModel) {
		t.Errorf("models.json missing model id %q\n%s", wantModel, body)
	}
	for _, leaked := range []string{"sonnet", "claude-sonnet-4-6"} {
		if strings.Contains(body, leaked) {
			t.Errorf("models.json leaked claude model %q — the bug is back\n%s", leaked, body)
		}
	}

	leakedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, core.AgentTypeClaudeCode, projectconfig.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if leakedModel != "sonnet" {
		t.Fatalf("counterfactual: claude tier-3 default = %q; want sonnet", leakedModel)
	}
	badSpec, _, err := build(ctx, hkpkuguE2ERunCtx(t, t.TempDir(), leakedModel))
	if err != nil {
		t.Fatalf("routed launch spec build (counterfactual): %v", err)
	}
	if got := hkpkuguE2EArgFlagValue(badSpec.Args, "--model"); got != "sonnet" {
		t.Fatalf("counterfactual sanity: expected leaked --model sonnet, got %q — the seam is not threading rc.model, assertions above would be vacuous", got)
	}
}

// TestPkuguClaudeLaunchPath_ModelUnchanged is the companion negative guard: a
// claude-resolved run (global default = claude-code) still threads the claude
// tier-3 default ("sonnet") into the real claude launch argv — proving the fix
// left the claude path byte-identical.
func TestPkuguClaudeLaunchPath_ModelUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-pkugu-e2e-bead",
		Title:  "pi-model-leak claude guard bead",
		Labels: nil,
	}

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode,
	)
	if agentType != core.AgentTypeClaudeCode {
		t.Fatalf("resolved agentType = %q; want claude-code", agentType)
	}
	sealedModel, sealedEffort := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, projectconfig.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "sonnet" || sealedEffort != "medium" {
		t.Fatalf("claude tier-3 default = (%q,%q); want (sonnet,medium) — claude path changed", sealedModel, sealedEffort)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode,
		bus,
	)
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	spec, _, err := build(ctx, hkpkuguE2ERunCtx(t, ws, sealedModel))
	if err != nil {
		t.Fatalf("routed launch spec build (claude): %v", err)
	}

	if spec.Binary != "claude" {
		t.Errorf("claude Binary = %q; want claude", spec.Binary)
	}
	if got := hkpkuguE2EArgFlagValue(spec.Args, "--model"); got != "sonnet" {
		t.Errorf("claude argv --model = %q; want sonnet (unchanged)\nargv=%v", got, spec.Args)
	}
}
