package daemon_test

// hk_pkugu_pi_launch_e2e_test.go — ISOLATED end-to-end regression for the
// pi-model-leak bug (hk-pkugu, codename:pi-model-leak), driving the REAL daemon
// launch path (no mock of the model resolver).
//
// # What "real launch path" means here
//
// This test wires the exact production seam that was broken, in isolation:
//
//	resolveHarnessAgentTypeQuiet(bead, "", "", globalDefault)   ← claim-time (workloop.go)
//	    → ResolveModelPreference(labels, agentType, ...)          ← seals rc.model
//	        → routedLaunchSpecBuilder(reg, bead, "", "", global)  ← real builder
//	            → buildCodexRoutedLaunchSpec → PiHarness.LaunchSpec
//	                → buildPiLaunchSpec (argv + buildPiModelsJSON) ← real argv/models.json
//
// Nothing here is stubbed except the temp dirs and the dummy provider key (so the
// PI-040/PI-042 billing guard passes without a live provider). The model that ends
// up in the pi argv and models.json is whatever the production resolution seam
// produces — precisely the value the bug corrupted.
//
// # The bug it reproduces
//
// Claim-time model resolution hardcoded agentType=claude-code, sealing the claude
// tier-3 default ("sonnet") into rc.model. PiHarness.LaunchSpec's
// `if rc.Model != "" { model = rc.Model }` then overrode the configured pi model
// ("ornith") with "sonnet" → the pi provider was asked for a claude model → fail.
//
// Helper prefix: hkpkuguE2E (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-pkugu.

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
)

// hkpkuguE2EArgFlagValue returns the token following the first occurrence of flag
// in args, or "" if flag is absent or has no following token.
func hkpkuguE2EArgFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hkpkuguE2EArgsContain reports whether any arg equals want.
func hkpkuguE2EArgsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hkpkuguE2EKeyFile writes a dummy provider key to a temp file so the PI-040
// billing guard (which requires a non-empty resolved key) passes without a live
// provider. Returns the file path.
func hkpkuguE2EKeyFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "pi.key")
	if err := os.WriteFile(f, []byte("dummy-pi-key-for-hk-pkugu\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return f
}

// hkpkuguE2ERunCtx builds an ExportedClaudeRunCtx for a single-mode initial-turn
// dispatch with the given workspace and the (claim-time-resolved) model sealed in.
// PriorClaudeSessID is nil → initial turn → buildPiLaunchSpec generates models.json.
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
	// Not t.Parallel: t.Setenv makes the PI-042 on-disk credential check hermetic
	// by pointing HOME at a fresh temp dir (no ~/.pi/auth.json → guard is a no-op).
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-pkugu-e2e-bead",
		Title:  "pi-model-leak e2e bead",
		Labels: nil, // no model:/harness: labels — the common flip case
	}

	const wantModel = "ornith"
	piCfg := daemon.PiHarnessConfig{
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

	// ── The production claim-time seam (workloop.go), verbatim ─────────────────
	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if agentType != core.AgentTypePi {
		t.Fatalf("resolved agentType = %q; want pi", agentType)
	}
	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "" {
		t.Fatalf("pi run sealed model = %q; want empty (no pi tier-3 default → config fallback)", sealedModel)
	}

	// ── The real routed launch path ───────────────────────────────────────────
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

	// argv: --model ornith, and NEVER the claude default.
	if got := hkpkuguE2EArgFlagValue(spec.Args, "--model"); got != wantModel {
		t.Errorf("pi argv --model = %q; want %q\nargv=%v", got, wantModel, spec.Args)
	}
	for _, leaked := range []string{"sonnet", "claude-sonnet-4-6"} {
		if hkpkuguE2EArgsContain(spec.Args, leaked) {
			t.Errorf("pi argv leaked claude model %q — the bug is back\nargv=%v", leaked, spec.Args)
		}
	}

	// models.json: contains the ornith model id, never the claude default.
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

	// ── Adversarial counterfactual: prove the seam is real and the fix matters ─
	// Had claim-time kept the OLD hardcoded agentType=claude-code, the sealed model
	// would be "sonnet", and the SAME real launch path would carry it into the pi
	// argv — exactly the failure mode. This proves the assertions above are not
	// vacuous (the path genuinely threads rc.model into pi's --model).
	leakedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, core.AgentTypeClaudeCode, daemon.ProjectConfig{}, bus, string(bead.BeadID),
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

	// Production claim-time seam with global default = claude-code.
	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode,
	)
	if agentType != core.AgentTypeClaudeCode {
		t.Fatalf("resolved agentType = %q; want claude-code", agentType)
	}
	sealedModel, sealedEffort := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "sonnet" || sealedEffort != "medium" {
		t.Fatalf("claude tier-3 default = (%q,%q); want (sonnet,medium) — claude path changed", sealedModel, sealedEffort)
	}

	// Real routed claude launch path (needs a .claude/ dir in the workspace).
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
