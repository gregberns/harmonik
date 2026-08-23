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

func hkppsNoLeakArgFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hkppsNoLeakArgsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hkppsNoLeakKeyFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "pi.key")
	if err := os.WriteFile(f, []byte("dummy-pi-key-for-hk-m6uu2-no-leak\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return f
}

func hkppsNoLeakRunCtx(t *testing.T, ws, model string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "hk-m6uu2-no-leak-bead",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-m6uu2-no-leak.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		Model:         model,
		BaseEnv:       []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

// TestPiNoTier3Leak_NoLabelBead_UsesHarnessGlobalModel is the no-label
// sub-case: a pi-resolved bead with NO profile:/model: label MUST NOT seal
// claude `sonnet`; the harness-global pi model/provider is used. Drives
// resolvePiProfile (C3) explicitly in the loop (returning the zero tuple for
// an absent profile: label) before the real routed launch path, mirroring the
// claim-time sequence at workloop.go:3077-3116.
func TestPiNoTier3Leak_NoLabelBead_UsesHarnessGlobalModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-m6uu2-no-leak-bead",
		Title:  "pi-provider-switch no-tier3-leak e2e bead",
		Labels: nil, // no model:/profile:/harness: labels
	}

	const wantModel = "ornith"
	piCfg := projectconfig.PiHarnessConfig{
		Provider:   "ornith",
		Model:      wantModel,
		APIKeyEnv:  "HK_M6UU2_NO_LEAK_PI_KEY",
		APIKeyFile: hkppsNoLeakKeyFile(t),
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

	resolvedProfile, profErr := daemon.ExportedResolvePiProfile(ctx, bead.Labels, agentType, piCfg, bus, string(bead.BeadID))
	if profErr != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", profErr)
	}
	if resolvedProfile != (projectconfig.PiProfileConfig{}) {
		t.Fatalf("resolvePiProfile: got non-zero profile %+v for a no-label bead; want zero tuple", resolvedProfile)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypePi,
		bus,
	)
	ws := t.TempDir()
	spec, _, err := build(ctx, hkppsNoLeakRunCtx(t, ws, sealedModel))
	if err != nil {
		t.Fatalf("routed launch spec build (pi): %v", err)
	}

	if got := hkppsNoLeakArgFlagValue(spec.Args, "--model"); got != wantModel {
		t.Errorf("pi argv --model = %q; want %q\nargv=%v", got, wantModel, spec.Args)
	}
	for _, leaked := range []string{"sonnet", "claude-sonnet-4-6"} {
		if hkppsNoLeakArgsContain(spec.Args, leaked) {
			t.Errorf("pi argv leaked claude model %q — the bug is back\nargv=%v", leaked, spec.Args)
		}
	}

	leakedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, core.AgentTypeClaudeCode, projectconfig.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if leakedModel != "sonnet" {
		t.Fatalf("counterfactual: claude tier-3 default = %q; want sonnet", leakedModel)
	}
	badSpec, _, err := build(ctx, hkppsNoLeakRunCtx(t, t.TempDir(), leakedModel))
	if err != nil {
		t.Fatalf("routed launch spec build (counterfactual): %v", err)
	}
	if got := hkppsNoLeakArgFlagValue(badSpec.Args, "--model"); got != "sonnet" {
		t.Fatalf("counterfactual sanity: expected leaked --model sonnet, got %q — the seam is not threading rc.model, assertions above would be vacuous", got)
	}
}

// TestPiNoTier3Leak_DotPathVariant_ProviderTupleUnclobbered is the DOT-path
// variant (LOCKED C3-Q5): the sonnet-triple-review workflow.dot pins
// model="claude-sonnet-4-6" as a per-node attribute on every node. For a
// pi-resolved run this pin MUST be dropped (nodeModelForHarness/
// ExportedNodeModelForHarness, dot_cascade.go:1274-1281) so the configured pi
// provider tuple (provider/base_url/api — baked into the harness registry
// from the resolved profile) rides the cascade UNCLOBBERED across node
// re-launches, rather than asking the DGX provider for a claude model.
//
// Simulates TWO node re-launches of the SAME pi-resolved run (as
// driveDotWorkflow would dispatch consecutively) and asserts each produces
// the identical, correct ornith argv + models.json — the per-node claude
// model= pin never reaches rc.model for either launch.
func TestPiNoTier3Leak_DotPathVariant_ProviderTupleUnclobbered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-m6uu2-no-leak-dot-bead",
		Title:  "pi-provider-switch no-tier3-leak DOT-path e2e bead",
		Labels: nil,
	}

	const (
		wantProvider = "ornith-provider"
		wantModel    = "ornith-provider/reasoning-id"
		wantBaseURL  = "http://127.0.0.1:8551/v1"
		wantAPI      = "openai-completions"
		claudePin    = "claude-sonnet-4-6" // every node's DOT model= attribute
	)

	piCfg := projectconfig.PiHarnessConfig{
		Provider:   wantProvider,
		Model:      wantModel,
		APIKeyEnv:  "HK_M6UU2_NO_LEAK_DOT_PI_KEY",
		APIKeyFile: hkppsNoLeakKeyFile(t),
		BaseURL:    wantBaseURL,
		API:        wantAPI,
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	const runResolvedModel = ""

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypePi,
		bus,
	)

	for nodeIdx, nodeID := range []string{"node-1", "node-2"} {
		nodeModel := daemon.ExportedNodeModelForHarness(runResolvedModel, claudePin, core.AgentTypePi)
		if nodeModel != "" {
			t.Fatalf("node %s: DOT model= pin leaked into nodeModel = %q; want empty (run-level resolvedModel)", nodeID, nodeModel)
		}

		ws := t.TempDir()
		spec, _, err := build(ctx, hkppsNoLeakRunCtx(t, ws, nodeModel))
		if err != nil {
			t.Fatalf("node %s: routed launch spec build: %v", nodeID, err)
		}

		if got := hkppsNoLeakArgFlagValue(spec.Args, "--provider"); got != wantProvider {
			t.Errorf("node %s: argv --provider = %q; want %q (provider tuple must not change across node re-launches)\nargv=%v", nodeID, got, wantProvider, spec.Args)
		}
		if got := hkppsNoLeakArgFlagValue(spec.Args, "--model"); got != wantModel {
			t.Errorf("node %s: argv --model = %q; want %q (harness-global pi model, not the claude pin)\nargv=%v", nodeID, got, wantModel, spec.Args)
		}
		if hkppsNoLeakArgsContain(spec.Args, claudePin) {
			t.Errorf("node %s: argv leaked the DOT claude model= pin %q — the harness-family scoping regressed\nargv=%v", nodeID, claudePin, spec.Args)
		}

		modelsPath := filepath.Join(ws, ".harmonik", "pi-agent", "models.json")
		data, err := os.ReadFile(modelsPath)
		if err != nil {
			t.Fatalf("node %s: read generated models.json: %v", nodeID, err)
		}
		body := string(data)
		if !strings.Contains(body, wantBaseURL) || !strings.Contains(body, wantAPI) {
			t.Errorf("node %s: models.json missing expected baseUrl/api (%q/%q); unchanged across re-launches\n%s", nodeID, wantBaseURL, wantAPI, body)
		}
		if strings.Contains(body, claudePin) {
			t.Errorf("node %s: models.json leaked the DOT claude model= pin %q\n%s", nodeID, claudePin, body)
		}

		_ = nodeIdx // both nodes assert the identical tuple; index only labels failures.
	}
}
