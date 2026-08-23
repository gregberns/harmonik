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
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/codex"
)

func harnessRegistryFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("harnessRegistryFixtureWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

func harnessRegistryFixtureRunCtx(t *testing.T, workspacePath string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("harnessRegistryFixtureRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:          core.RunID(runUID),
		BeadID:         "test-bead-harness-registry-hk-hj9ld",
		WorkspacePath:  workspacePath,
		DaemonSocket:   "/tmp/harmonik-test-harness-registry.sock",
		WorkflowMode:   core.WorkflowModeSingle,
		Phase:          "",
		IterationCount: 0,
		HandlerBinary:  "claude",
		BaseEnv:        []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

func harnessRegistryFixtureBus(t *testing.T) handlercontract.EventEmitter {
	t.Helper()
	return eventbus.NewBusImpl()
}

// TestHarnessRegistry_ForAgent_Claude verifies newHarnessRegistry registers the
// claude.Harness under core.AgentTypeClaudeCode and ForAgent returns it.
func TestHarnessRegistry_ForAgent_Claude(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	h, err := reg.ForAgent(core.AgentTypeClaudeCode)
	if err != nil {
		t.Fatalf("ForAgent(claude-code): %v", err)
	}
	if got := h.AgentType(); got != core.AgentTypeClaudeCode {
		t.Errorf("ForAgent(claude-code).AgentType() = %q; want %q", got, core.AgentTypeClaudeCode)
	}
	if _, ok := h.(*claude.Harness); !ok {
		t.Errorf("ForAgent(claude-code) returned %T; want *claude.Harness", h)
	}
}

// TestHarnessRegistry_RegisteredTypes_AllHarnesses verifies that claude-code,
// codex, and pi harnesses are all registered in newHarnessRegistry.
func TestHarnessRegistry_RegisteredTypes_AllHarnesses(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	types := reg.RegisteredTypes()
	if len(types) != 3 {
		t.Fatalf("RegisteredTypes = %v; want exactly [claude-code codex pi]", types)
	}
	typeSet := make(map[core.AgentType]bool, len(types))
	for _, at := range types {
		typeSet[at] = true
	}
	if !typeSet[core.AgentTypeClaudeCode] {
		t.Errorf("RegisteredTypes missing claude-code; got %v", types)
	}
	if !typeSet[core.AgentTypeCodex] {
		t.Errorf("RegisteredTypes missing codex; got %v", types)
	}
	if !typeSet[core.AgentTypePi] {
		t.Errorf("RegisteredTypes missing pi; got %v", types)
	}
}

// TestHarnessRegistry_ForAgent_Codex_Registered verifies that after T12 codex IS
// registered: ForAgent(codex) succeeds and returns a *codex.Harness.
func TestHarnessRegistry_ForAgent_Codex_Registered(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	h, err := reg.ForAgent(core.AgentTypeCodex)
	if err != nil {
		t.Fatalf("ForAgent(codex): expected success after T12 registration, got %v", err)
	}
	if h == nil {
		t.Fatal("ForAgent(codex): expected non-nil harness")
	}
	if _, ok := h.(*codex.Harness); !ok {
		t.Errorf("ForAgent(codex) returned %T; want *codex.Harness", h)
	}
}

// TestRoutedLaunchSpecBuilder_ClaudeParity verifies that the registry-routed
// launchSpecBuilder produces a LaunchSpec byte-identical (Binary/Args/Env/WorkDir)
// to a direct buildClaudeLaunchSpec call for a bead with no harness label (default
// resolution → claude-code). This is the C1/T3 no-behavior-change guarantee.
func TestRoutedLaunchSpecBuilder_ClaudeParity(t *testing.T) {
	wsRef := harnessRegistryFixtureWorkspace(t)
	rcRef := harnessRegistryFixtureRunCtx(t, wsRef)
	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcRef)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := core.BeadRecord{BeadID: "test-bead-routed-hk-hj9ld"} // no harness label → default
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		harnessRegistryFixtureBus(t),
	)

	wsRouted := harnessRegistryFixtureWorkspace(t)
	rcRouted := harnessRegistryFixtureRunCtx(t, wsRouted)
	routedSpec, _, err := build(context.Background(), rcRouted)
	if err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	if routedSpec.Binary != refSpec.Binary {
		t.Errorf("Binary: routed = %q; want %q", routedSpec.Binary, refSpec.Binary)
	}
	if got, want := normalizeSessionID(routedSpec.Args), normalizeSessionID(refSpec.Args); !equalStrings(got, want) {
		t.Errorf("Args (session-id normalised):\n routed = %v\n  want  = %v", got, want)
	}
	if got, want := envKeys(routedSpec.Env), envKeys(refSpec.Env); !equalStringSet(got, want) {
		t.Errorf("Env key set differs:\n routed = %v\n  want  = %v", got, want)
	}
	if routedSpec.WorkDir != wsRouted {
		t.Errorf("routed WorkDir = %q; want %q", routedSpec.WorkDir, wsRouted)
	}
	if refSpec.WorkDir != wsRef {
		t.Errorf("ref WorkDir = %q; want %q", refSpec.WorkDir, wsRef)
	}
}

// TestRoutedLaunchSpecBuilder_SideEffects verifies the routed builder produces the
// same workspace side-effects as buildClaudeLaunchSpec (settings.json + agent-task.md),
// confirming it delegates to the real claude build path.
func TestRoutedLaunchSpecBuilder_SideEffects(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := core.BeadRecord{BeadID: "test-bead-routed-sideeffects-hk-hj9ld"}
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		harnessRegistryFixtureBus(t),
	)

	ws := harnessRegistryFixtureWorkspace(t)
	rc := harnessRegistryFixtureRunCtx(t, ws)
	if _, _, err := build(context.Background(), rc); err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	for _, rel := range [][]string{
		{".claude", "settings.json"},
		{".harmonik", "agent-task.md"},
	} {
		p := filepath.Join(append([]string{ws}, rel...)...)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to be created by routed builder: %v", p, err)
		}
	}
}

// TestHarnessRegistry_DefaultResolvesToClaude verifies resolveHarness with all
// tiers absent resolves to core.AgentTypeClaudeCode (the routed builder's claude
// path is reached for an ordinary bead).
func TestHarnessRegistry_DefaultResolvesToClaude(t *testing.T) {
	bead := core.BeadRecord{BeadID: "test-bead-default-hk-hj9ld"} // no labels
	got := daemon.ExportedResolveHarness(
		context.Background(), bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		harnessRegistryFixtureBus(t),
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("resolveHarness(default) = %q; want %q", got, core.AgentTypeClaudeCode)
	}
}

func normalizeSessionID(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if (a == "--session-id" || a == "--resume") && i+1 < len(out) {
			out[i+1] = "<session-id>"
		}
	}
	return out
}

func envKeys(env []string) map[string]bool {
	keys := map[string]bool{}
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			keys[e[:i]] = true
		}
	}
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
