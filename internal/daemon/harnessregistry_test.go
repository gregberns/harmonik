package daemon_test

// harnessregistry_test.go — daemon-side HarnessRegistry wiring + routed
// launchSpecBuilder tests (codex-harness C1/T3, hk-hj9ld).
//
// Covers:
//  1. newHarnessRegistry registers ClaudeHarness for core.AgentTypeClaudeCode and
//     no other type (claude-only in T3).
//  2. The registry-routed launchSpecBuilder produces a LaunchSpec equivalent to a
//     direct buildClaudeLaunchSpec call for the default (claude) resolution —
//     no behavior change.
//  3. resolveHarness default resolution lands on AgentTypeClaudeCode, so the
//     routed builder uses the claude harness for a bead with no harness label.
//
// These tests do NOT call t.Parallel(): like claudeharness_test.go they invoke
// buildClaudeLaunchSpec (EnsureWorktreeTrust writes under a ~/.claude file lock),
// so running them in parallel with the integration suite causes spurious
// lock-timeout failures.
//
// Helper prefix: harnessRegistryFixture.

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
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// harnessRegistryFixtureWorkspace mirrors claudeHarnessFixtureWorkspace.
func harnessRegistryFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("harnessRegistryFixtureWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

// harnessRegistryFixtureRunCtx builds an ExportedClaudeRunCtx for single-mode.
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

// harnessRegistryFixtureBus returns an in-process event bus for resolveHarness.
func harnessRegistryFixtureBus(t *testing.T) handlercontract.EventEmitter {
	t.Helper()
	return eventbus.NewBusImpl()
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Registry contents (claude-only)
// ─────────────────────────────────────────────────────────────────────────────

// TestHarnessRegistry_ForAgent_Claude verifies newHarnessRegistry registers the
// ClaudeHarness under core.AgentTypeClaudeCode and ForAgent returns it.
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
	// It must be the concrete *ClaudeHarness.
	if _, ok := h.(*daemon.ClaudeHarness); !ok {
		t.Errorf("ForAgent(claude-code) returned %T; want *daemon.ClaudeHarness", h)
	}
}

// TestHarnessRegistry_RegisteredTypes_BothHarnesses verifies that after T12 both
// claude-code and codex harnesses are registered in newHarnessRegistry.
func TestHarnessRegistry_RegisteredTypes_BothHarnesses(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	types := reg.RegisteredTypes()
	if len(types) != 2 {
		t.Fatalf("RegisteredTypes = %v; want exactly [claude-code codex] (T12 adds codex)", types)
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
}

// TestHarnessRegistry_ForAgent_Codex_Registered verifies that after T12 codex IS
// registered: ForAgent(codex) succeeds and returns a *CodexHarness.
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
	if _, ok := h.(*daemon.CodexHarness); !ok {
		t.Errorf("ForAgent(codex) returned %T; want *daemon.CodexHarness", h)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Routed builder == direct buildClaudeLaunchSpec (no behavior change)
// ─────────────────────────────────────────────────────────────────────────────

// TestRoutedLaunchSpecBuilder_ClaudeParity verifies that the registry-routed
// launchSpecBuilder produces a LaunchSpec byte-identical (Binary/Args/Env/WorkDir)
// to a direct buildClaudeLaunchSpec call for a bead with no harness label (default
// resolution → claude-code). This is the C1/T3 no-behavior-change guarantee.
func TestRoutedLaunchSpecBuilder_ClaudeParity(t *testing.T) {
	// Reference: direct buildClaudeLaunchSpec on a fresh workspace.
	wsRef := harnessRegistryFixtureWorkspace(t)
	rcRef := harnessRegistryFixtureRunCtx(t, wsRef)
	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcRef)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	// Routed: through newHarnessRegistry + resolveHarness (default → claude) on a
	// fresh workspace (avoids WriteAgentTask side-effect collision).
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

	// Binary parity.
	if routedSpec.Binary != refSpec.Binary {
		t.Errorf("Binary: routed = %q; want %q", routedSpec.Binary, refSpec.Binary)
	}
	// Arg-shape parity: the routed path must produce a --session-id flag and the
	// same flag set as the direct path (session-id values differ per-run, which
	// is expected — they are freshly minted UUIDv7s). Compare argv with the
	// session-id VALUE normalised out.
	if got, want := normalizeSessionID(routedSpec.Args), normalizeSessionID(refSpec.Args); !equalStrings(got, want) {
		t.Errorf("Args (session-id normalised):\n routed = %v\n  want  = %v", got, want)
	}
	// Env-key parity: the same set of HARMONIK_* keys must be present. Values for
	// run/session IDs differ per-run, so compare the key set, not values.
	if got, want := envKeys(routedSpec.Env), envKeys(refSpec.Env); !equalStringSet(got, want) {
		t.Errorf("Env key set differs:\n routed = %v\n  want  = %v", got, want)
	}
	// WorkDir parity: each spec points at its own workspace; both must be the
	// supplied workspace path (non-empty, equal to rc.WorkspacePath).
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

// ─────────────────────────────────────────────────────────────────────────────
// 3. resolveHarness default → claude (routed builder uses claude for no-label bead)
// ─────────────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// normalizeSessionID returns a copy of args with the value following any
// --session-id or --resume flag replaced by a placeholder, so per-run UUID
// differences do not defeat structural argv comparison.
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

// envKeys extracts the sorted-into-set of "KEY" portions from "KEY=VALUE" entries.
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
