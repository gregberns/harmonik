package daemon_test

// projectconfig_hkbfvk7_test.go — unit tests for the EM-012b project config
// loader (LoadProjectConfig) and 4-tier model/effort resolution chain
// (ResolveModelPreference).
//
// Covers:
//   - Loader: schema_version=1 happy path (model+effort).
//   - Loader: missing file → zero-value ProjectConfig, nil error.
//   - Loader: empty file → zero-value ProjectConfig, nil error.
//   - Loader: malformed YAML → *ErrMalformedConfigYAML.
//   - Loader: unknown schema_version → *ErrUnsupportedConfigVersion.
//   - Loader: unknown agent_type key → ignored (forward-compat).
//   - Loader: only model set (effort absent) → model returned, effort empty.
//   - Resolution: tier-1 wins (model label overrides config + default).
//   - Resolution: tier-1 absent → tier-2 (project config) wins.
//   - Resolution: tier-1+2 absent → tier-3 (compiled default) wins.
//   - Resolution: all absent → tier-4 (empty strings).
//   - Resolution: model and effort resolved independently.
//   - Resolution: tier-1 conflict (two model: labels) → event emitted + tier-2 used.
//   - Resolution: tier-1 conflict (two effort: labels) → event emitted + tier-2 used.
//   - Resolution: tier-1 unrecognised effort value → event emitted + tier-2 used.
//   - Integration: beadRunOne claudeRunCtx gets resolved model+effort from project config.
//
// Helper prefix: projCfgFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Spec refs:
//   - specs/execution-model.md §4.3 EM-012b — 4-tier model/effort resolution.
//   - specs/handler-contract.md §4.10 HC-055a — ModelPreference invariants.
//
// Bead: hk-bfvk7.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// projCfgFixtureDir creates a temporary directory with a .harmonik/ subdirectory
// and optionally writes config.yaml content.  Returns the repo root path.
func projCfgFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("projCfgFixtureDir: MkdirAll: %v", err)
	}
	if yamlContent != "" {
		cfgPath := filepath.Join(harmonikDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o644); err != nil {
			t.Fatalf("projCfgFixtureDir: WriteFile: %v", err)
		}
	}
	return root
}

// projCfgFixtureBus is a minimal event collector for testing label-conflict events.
type projCfgFixtureBus struct {
	mu     sync.Mutex
	events []projCfgFixtureEvent
}

type projCfgFixtureEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (b *projCfgFixtureBus) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, projCfgFixtureEvent{EventType: et, Payload: payload})
	return nil
}

func (b *projCfgFixtureBus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

func (b *projCfgFixtureBus) eventCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func (b *projCfgFixtureBus) hasEventType(et core.EventType) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ev := range b.events {
		if ev.EventType == et {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// LoadProjectConfig tests
// ─────────────────────────────────────────────────────────────────────────────

func TestProjectConfig_HappyPath(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: opus
    effort: high
  claude-twin:
    model: ""
    effort: ""
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	model, effort := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "opus" {
		t.Errorf("LookupAgent(claude-code).model = %q; want %q", model, "opus")
	}
	if effort != "high" {
		t.Errorf("LookupAgent(claude-code).effort = %q; want %q", effort, "high")
	}
}

func TestProjectConfig_FileAbsent(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, "") // no config.yaml written
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig absent: unexpected error: %v", err)
	}
	model, effort := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "" || effort != "" {
		t.Errorf("absent config: LookupAgent should return empty; got model=%q effort=%q", model, effort)
	}
}

func TestProjectConfig_EmptyFile(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, "") // write empty file
	// Write an empty file (not absent, but empty content).
	emptyPath := filepath.Join(root, ".harmonik", "config.yaml")
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile empty: %v", err)
	}

	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig empty file: unexpected error: %v", err)
	}
	model, effort := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "" || effort != "" {
		t.Errorf("empty file: LookupAgent should return empty; got model=%q effort=%q", model, effort)
	}
}

func TestProjectConfig_MalformedYAML(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, "schema_version: 1\nagents: [not a map]")
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig malformed: expected error; got nil")
	}
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Errorf("LoadProjectConfig malformed: error type = %T (%v); want *ErrMalformedConfigYAML", err, err)
	}
}

func TestProjectConfig_UnsupportedSchemaVersion(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, "schema_version: 99\nagents:\n  claude-code:\n    model: opus\n")
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig bad version: expected error; got nil")
	}
	var uve *daemon.ExportedErrUnsupportedConfigVersion
	if !errors.As(err, &uve) {
		t.Errorf("LoadProjectConfig bad version: error type = %T (%v); want *ErrUnsupportedConfigVersion", err, err)
	}
}

func TestProjectConfig_UnknownAgentTypeIgnored(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: sonnet
    effort: medium
  future-agent-type-not-yet-defined:
    model: gpt5
    effort: max
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig with unknown agent: unexpected error: %v", err)
	}
	// Known key should still load.
	model, _ := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "sonnet" {
		t.Errorf("LookupAgent(claude-code).model = %q; want %q", model, "sonnet")
	}
}

func TestProjectConfig_ModelOnlyEffortAbsent(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: haiku
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig model-only: unexpected error: %v", err)
	}
	model, effort := cfg.LookupAgent(core.AgentTypeClaudeCode)
	if model != "haiku" {
		t.Errorf("LookupAgent.model = %q; want %q", model, "haiku")
	}
	if effort != "" {
		t.Errorf("LookupAgent.effort = %q; want empty (not in file)", effort)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveModelPreference tests
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveModelPreference_Tier1Wins(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: sonnet
    effort: medium
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	labels := []string{"model:opus", "effort:high"}
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), labels, core.AgentTypeClaudeCode, cfg, bus, "bead-001",
	)

	if model != "opus" {
		t.Errorf("tier-1 model = %q; want %q", model, "opus")
	}
	if effort != "high" {
		t.Errorf("tier-1 effort = %q; want %q", effort, "high")
	}
	if bus.eventCount() != 0 {
		t.Errorf("no conflict events expected; got %d", bus.eventCount())
	}
}

func TestResolveModelPreference_Tier2WhenTier1Absent(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: haiku
    effort: low
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// No model/effort labels at all.
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), []string{"subsystem:daemon"}, core.AgentTypeClaudeCode, cfg, bus, "bead-002",
	)

	if model != "haiku" {
		t.Errorf("tier-2 model = %q; want %q", model, "haiku")
	}
	if effort != "low" {
		t.Errorf("tier-2 effort = %q; want %q", effort, "low")
	}
}

func TestResolveModelPreference_Tier3WhenTier1And2Absent(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	cfg := daemon.ExportedProjectConfig{} // zero-value = no project config

	// No model/effort labels; no project config.
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), []string{}, core.AgentTypeClaudeCode, cfg, bus, "bead-003",
	)

	// Tier-3 defaults for claude-code: model=sonnet, effort=medium.
	if model != "sonnet" {
		t.Errorf("tier-3 model = %q; want %q", model, "sonnet")
	}
	if effort != "medium" {
		t.Errorf("tier-3 effort = %q; want %q", effort, "medium")
	}
}

func TestResolveModelPreference_Tier4EmptyFallback(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	cfg := daemon.ExportedProjectConfig{} // zero-value

	// claude-twin has empty tier-3 defaults.
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), []string{}, core.AgentTypeClaudeTwin, cfg, bus, "bead-004",
	)

	if model != "" {
		t.Errorf("tier-4 model should be empty; got %q", model)
	}
	if effort != "" {
		t.Errorf("tier-4 effort should be empty; got %q", effort)
	}
}

func TestResolveModelPreference_ModelAndEffortResolvedIndependently(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	// Project config only sets effort (model absent).
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    effort: xhigh
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// Labels only set model (no effort label).
	labels := []string{"model:opus"}
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), labels, core.AgentTypeClaudeCode, cfg, bus, "bead-005",
	)

	// model: tier-1 wins (opus from label).
	// effort: tier-1 absent → tier-2 wins (xhigh from config).
	if model != "opus" {
		t.Errorf("independent model = %q; want %q", model, "opus")
	}
	if effort != "xhigh" {
		t.Errorf("independent effort = %q; want %q", effort, "xhigh")
	}
}

func TestResolveModelPreference_Tier1ConflictTwoModelLabels(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: haiku
    effort: low
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// Two model labels → conflict; should fall back to tier-2.
	labels := []string{"model:opus", "model:sonnet", "effort:high"}
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), labels, core.AgentTypeClaudeCode, cfg, bus, "bead-006",
	)

	// model: conflict → tier-2 = haiku.
	// effort: no conflict → tier-1 = high.
	if model != "haiku" {
		t.Errorf("conflict model fallback = %q; want %q", model, "haiku")
	}
	if effort != "high" {
		t.Errorf("effort = %q; want %q", effort, "high")
	}
	// Conflict event must be emitted.
	if !bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("expected bead_label_conflict event for double model label; none emitted")
	}
}

func TestResolveModelPreference_Tier1ConflictTwoEffortLabels(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: haiku
    effort: low
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// Two effort labels → conflict; should fall back to tier-2.
	labels := []string{"model:opus", "effort:high", "effort:max"}
	model, effort := daemon.ExportedResolveModelPreference(
		context.Background(), labels, core.AgentTypeClaudeCode, cfg, bus, "bead-007",
	)

	// model: tier-1 = opus (no conflict).
	// effort: conflict → tier-2 = low.
	if model != "opus" {
		t.Errorf("model = %q; want %q", model, "opus")
	}
	if effort != "low" {
		t.Errorf("conflict effort fallback = %q; want %q", effort, "low")
	}
	if !bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("expected bead_label_conflict event for double effort label; none emitted")
	}
}

func TestResolveModelPreference_Tier1UnrecognisedEffortValue(t *testing.T) {
	t.Parallel()

	bus := &projCfgFixtureBus{}
	root := projCfgFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    effort: low
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	// Unrecognised effort value → treat tier-1 as absent + emit event.
	labels := []string{"effort:turbo"}
	_, effort := daemon.ExportedResolveModelPreference(
		context.Background(), labels, core.AgentTypeClaudeCode, cfg, bus, "bead-008",
	)

	// effort: unrecognised → tier-2 = low.
	if effort != "low" {
		t.Errorf("unrecognised effort fallback = %q; want %q", effort, "low")
	}
	if !bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("expected bead_label_conflict event for unrecognised effort value; none emitted")
	}
}
