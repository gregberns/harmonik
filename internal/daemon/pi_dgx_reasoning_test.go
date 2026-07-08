package daemon_test

// pi_dgx_reasoning_test.go — Scenario 3 of the pi-provider-switch C5
// two-provider e2e harness corpus (hk-m6uu2.6, C5-wiring; hk-4ir08): an
// ornith/DGX reasoning-model profile bead hermetically reaches the loopback
// launch spec + generated models.json.
//
// IMPORTANT — scope boundary: this test proves everything up to the launch
// spec the real Pi turn consumes (argv, env, models.json). It does NOT drive
// an actual `tool_calls` round-trip against the DGX reasoning model — that
// requires a live loopback tunnel (http://127.0.0.1:8551/v1) and is a SEPARATE
// operator canary (the DoD proof for pi-provider-switch), not part of this CI
// gate. See C5-spec.md §"Verification — Operator canary".
//
// Helper prefix: hkppsDgx (per implementer-protocol.md §Helper-prefix
// discipline).
//
// Bead: hk-m6uu2.6 (pi-provider-switch C5-wiring); hk-4ir08.

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
)

const (
	hkppsDgxProfile     = "ornith-dgx-reasoning"
	hkppsDgxAPIKeyEnv   = "HKPPS_DGX_REASONING_PI_KEY"
	hkppsDgxBaseURL     = "http://127.0.0.1:8551/v1"
	hkppsDgxAPI         = "openai-completions"
	hkppsDgxProvider    = "ornith-provider"
	hkppsDgxReasonModel = "ornith-provider/reasoning-large"
)

// hkppsDgxArgFlagValue returns the token following the first occurrence of
// flag in args, or "" if flag is absent or has no following token.
func hkppsDgxArgFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hkppsDgxKeyFile writes a dummy provider key to a temp file so the PI-040
// billing guard passes hermetically without a live DGX key.
func hkppsDgxKeyFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "pi.key")
	if err := os.WriteFile(f, []byte("dummy-dgx-reasoning-key-for-hk-4ir08\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return f
}

// TestPiDgxReasoning_LoopbackLaunchSpecAndModelsJSON drives an
// ornith/DGX-profile bead selecting the reasoning model through the real
// claim-time → launch-spec seam and hermetically asserts the loopback launch
// spec (argv --provider/--model) and the generated models.json (baseUrl +
// api: openai-completions). No network; the actual reasoning + tool_calls
// round-trip is the separate live-tunnel operator canary (see file doc).
func TestPiDgxReasoning_LoopbackLaunchSpecAndModelsJSON(t *testing.T) {
	// Not t.Parallel: t.Setenv makes the PI-042 on-disk credential check
	// hermetic by pointing HOME at a fresh temp dir.
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	piCfg := daemon.PiHarnessConfig{
		Profiles: map[string]daemon.PiProfileConfig{
			hkppsDgxProfile: {
				Provider:   hkppsDgxProvider,
				Model:      hkppsDgxReasonModel,
				APIKeyEnv:  hkppsDgxAPIKeyEnv,
				APIKeyFile: hkppsDgxKeyFile(t),
				BaseURL:    hkppsDgxBaseURL,
				API:        hkppsDgxAPI,
			},
		},
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	bead := core.BeadRecord{
		BeadID: "hk-4ir08-dgx-reasoning-bead",
		Title:  "pi-provider-switch DGX reasoning e2e bead",
		Labels: []string{"profile:" + hkppsDgxProfile},
	}

	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead, core.AgentType(""), core.AgentType(""), core.AgentTypePi,
	)
	if agentType != core.AgentTypePi {
		t.Fatalf("resolved agentType = %q; want pi", agentType)
	}

	profile, profErr := daemon.ExportedResolvePiProfile(ctx, bead.Labels, agentType, piCfg, bus, string(bead.BeadID))
	if profErr != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", profErr)
	}
	if profile.Provider != hkppsDgxProvider || profile.Model != hkppsDgxReasonModel {
		t.Fatalf("resolvePiProfile: got (provider=%q, model=%q); want (%q, %q)",
			profile.Provider, profile.Model, hkppsDgxProvider, hkppsDgxReasonModel)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(reg, bead, core.AgentType(""), core.AgentType(""), core.AgentTypePi, bus)

	ws := t.TempDir()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	rc := daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        string(bead.BeadID),
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-4ir08-dgx.sock",
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
	spec, _, err := build(ctx, rc)
	if err != nil {
		t.Fatalf("routed launch spec build (dgx reasoning): %v", err)
	}

	if got := hkppsDgxArgFlagValue(spec.Args, "--provider"); got != hkppsDgxProvider {
		t.Errorf("argv --provider = %q; want %q\nargv=%v", got, hkppsDgxProvider, spec.Args)
	}
	if got := hkppsDgxArgFlagValue(spec.Args, "--model"); got != hkppsDgxReasonModel {
		t.Errorf("argv --model = %q; want %q\nargv=%v", got, hkppsDgxReasonModel, spec.Args)
	}

	modelsPath := filepath.Join(ws, ".harmonik", "pi-agent", "models.json")
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("read generated models.json: %v", err)
	}
	var parsed struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("models.json JSON parse failed: %v\ncontent: %s", err, data)
	}
	prov, ok := parsed.Providers[hkppsDgxProvider]
	if !ok {
		t.Fatalf("models.json has no %q provider entry; got providers: %v", hkppsDgxProvider, parsed.Providers)
	}
	if prov.BaseURL != hkppsDgxBaseURL {
		t.Errorf("models.json baseUrl = %q; want %q", prov.BaseURL, hkppsDgxBaseURL)
	}
	if prov.API != hkppsDgxAPI {
		t.Errorf("models.json api = %q; want %q", prov.API, hkppsDgxAPI)
	}
	if len(prov.Models) != 1 || prov.Models[0].ID == "" {
		t.Errorf("models.json models = %v; want exactly one non-empty model id", prov.Models)
	}

	// The live reasoning + tool_calls round-trip over this loopback endpoint is
	// the operator canary (DoD proof), NOT asserted here — see file doc.
}
