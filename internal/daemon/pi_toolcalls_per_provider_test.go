package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const (
	hkppsToolcallsOpenRouterProfile = "openrouter-cloud"
	hkppsToolcallsOrnithProfile     = "ornith-dgx"
	hkppsToolcallsOpenRouterEnv     = "HKPPS_OPENROUTER_API_KEY"
	hkppsToolcallsOrnithEnv         = "HKPPS_ORNITH_PI_KEY"
	hkppsToolcallsOrnithBaseURL     = "http://127.0.0.1:8551/v1"
	hkppsToolcallsOrnithAPI         = "openai-completions"
)

func hkppsToolcallsArgFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hkppsToolcallsKeyFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "pi.key")
	if err := os.WriteFile(f, []byte("dummy-ornith-key-for-hk-m6uu2\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return f
}

func hkppsToolcallsRunCtx(t *testing.T, ws, beadID string, profile projectconfig.PiProfileConfig) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        beadID,
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-m6uu2-toolcalls.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		Model:         profile.Model,
		Provider:      profile.Provider,
		APIKeyEnv:     profile.APIKeyEnv,
		APIKeyFile:    profile.APIKeyFile,
		BaseURL:       profile.BaseURL,
		API:           profile.API,
		BaseEnv:       []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

func hkppsToolcallsPiCfg(t *testing.T) projectconfig.PiHarnessConfig {
	t.Helper()
	return projectconfig.PiHarnessConfig{
		Profiles: map[string]projectconfig.PiProfileConfig{
			hkppsToolcallsOpenRouterProfile: {
				Provider:  "openrouter",
				Model:     "openrouter/qwen/qwen3-coder",
				APIKeyEnv: hkppsToolcallsOpenRouterEnv,
			},
			hkppsToolcallsOrnithProfile: {
				Provider:   "ornith-provider",
				Model:      "ornith-provider/reasoning-id",
				APIKeyEnv:  hkppsToolcallsOrnithEnv,
				APIKeyFile: hkppsToolcallsKeyFile(t),
				BaseURL:    hkppsToolcallsOrnithBaseURL,
				API:        hkppsToolcallsOrnithAPI,
			},
		},
	}
}

// TestPiToolcallsPerProvider_TwoBeadsSameRegistry drives an OpenRouter-profile
// bead and an ornith-profile bead through the SAME harness registry (as two
// beads dispatched by the same daemon would be) and asserts each produces the
// argv AND models.json matching THEIR wire format — the two-provider "both
// work completely" success criterion made executable at the hermetic
// launch-spec/models.json layer (C5-spec.md Scenario 1).
func TestPiToolcallsPerProvider_TwoBeadsSameRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(hkppsToolcallsOpenRouterEnv, "dummy-openrouter-key-for-hk-m6uu2")

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	piCfg := hkppsToolcallsPiCfg(t)

	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	orBead := core.BeadRecord{
		BeadID: "hk-m6uu2-toolcalls-or-bead",
		Title:  "pi-provider-switch toolcalls e2e: openrouter",
		Labels: []string{"profile:" + hkppsToolcallsOpenRouterProfile},
	}
	orAgentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		orBead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if orAgentType != core.AgentTypePi {
		t.Fatalf("openrouter bead resolved agentType = %q; want pi", orAgentType)
	}
	orProfile, orErr := daemon.ExportedResolvePiProfile(ctx, orBead.Labels, orAgentType, piCfg, bus, string(orBead.BeadID))
	if orErr != nil {
		t.Fatalf("resolvePiProfile (openrouter): unexpected error: %v", orErr)
	}
	if orProfile.Provider != "openrouter" {
		t.Fatalf("resolvePiProfile (openrouter): Provider = %q; want %q", orProfile.Provider, "openrouter")
	}

	orBuild := daemon.ExportedRoutedLaunchSpecBuilder(reg, orBead, core.AgentType(""), core.AgentType(""), core.AgentTypePi, bus)
	orWS := t.TempDir()
	orSpec, _, err := orBuild(ctx, hkppsToolcallsRunCtx(t, orWS, string(orBead.BeadID), orProfile))
	if err != nil {
		t.Fatalf("routed launch spec build (openrouter): %v", err)
	}

	if got := hkppsToolcallsArgFlagValue(orSpec.Args, "--provider"); got != "openrouter" {
		t.Errorf("openrouter argv --provider = %q; want %q\nargv=%v", got, "openrouter", orSpec.Args)
	}
	if got := hkppsToolcallsArgFlagValue(orSpec.Args, "--model"); got != orProfile.Model {
		t.Errorf("openrouter argv --model = %q; want %q\nargv=%v", got, orProfile.Model, orSpec.Args)
	}
	if _, statErr := os.Stat(filepath.Join(orWS, ".harmonik", "pi-agent", "models.json")); statErr == nil {
		t.Errorf("openrouter bead wrote models.json; want absent (no base_url in profile)")
	}

	ornithBead := core.BeadRecord{
		BeadID: "hk-m6uu2-toolcalls-ornith-bead",
		Title:  "pi-provider-switch toolcalls e2e: ornith",
		Labels: []string{"profile:" + hkppsToolcallsOrnithProfile},
	}
	ornithAgentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		ornithBead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if ornithAgentType != core.AgentTypePi {
		t.Fatalf("ornith bead resolved agentType = %q; want pi", ornithAgentType)
	}
	ornithProfile, ornithErr := daemon.ExportedResolvePiProfile(ctx, ornithBead.Labels, ornithAgentType, piCfg, bus, string(ornithBead.BeadID))
	if ornithErr != nil {
		t.Fatalf("resolvePiProfile (ornith): unexpected error: %v", ornithErr)
	}
	if ornithProfile.Provider != "ornith-provider" {
		t.Fatalf("resolvePiProfile (ornith): Provider = %q; want %q", ornithProfile.Provider, "ornith-provider")
	}

	ornithBuild := daemon.ExportedRoutedLaunchSpecBuilder(reg, ornithBead, core.AgentType(""), core.AgentType(""), core.AgentTypePi, bus)
	ornithWS := t.TempDir()
	ornithSpec, _, err := ornithBuild(ctx, hkppsToolcallsRunCtx(t, ornithWS, string(ornithBead.BeadID), ornithProfile))
	if err != nil {
		t.Fatalf("routed launch spec build (ornith): %v", err)
	}

	if got := hkppsToolcallsArgFlagValue(ornithSpec.Args, "--provider"); got != "ornith-provider" {
		t.Errorf("ornith argv --provider = %q; want %q\nargv=%v", got, "ornith-provider", ornithSpec.Args)
	}
	if got := hkppsToolcallsArgFlagValue(ornithSpec.Args, "--model"); got != ornithProfile.Model {
		t.Errorf("ornith argv --model = %q; want %q\nargv=%v", got, ornithProfile.Model, ornithSpec.Args)
	}

	modelsPath := filepath.Join(ornithWS, ".harmonik", "pi-agent", "models.json")
	modelsBytes, readErr := os.ReadFile(modelsPath)
	if readErr != nil {
		t.Fatalf("ornith bead: models.json not found at %q: %v", modelsPath, readErr)
	}
	var parsed struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsBytes, &parsed); err != nil {
		t.Fatalf("ornith bead: models.json JSON parse failed: %v\ncontent: %s", err, modelsBytes)
	}
	prov, ok := parsed.Providers["ornith-provider"]
	if !ok {
		t.Fatalf("ornith bead: models.json has no 'ornith-provider' entry; got providers: %v", parsed.Providers)
	}
	if prov.BaseURL != hkppsToolcallsOrnithBaseURL {
		t.Errorf("ornith bead: models.json baseUrl = %q; want %q", prov.BaseURL, hkppsToolcallsOrnithBaseURL)
	}
	if prov.API != hkppsToolcallsOrnithAPI {
		t.Errorf("ornith bead: models.json api = %q; want %q", prov.API, hkppsToolcallsOrnithAPI)
	}

	if hkppsToolcallsArgFlagValue(orSpec.Args, "--provider") == hkppsToolcallsArgFlagValue(ornithSpec.Args, "--provider") {
		t.Fatalf("openrouter and ornith argv --provider are identical (%q) — per-bead profile routing is not independent",
			hkppsToolcallsArgFlagValue(orSpec.Args, "--provider"))
	}
}
