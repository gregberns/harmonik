package daemon_test

// projectconfig_hklhu2_test.go — unit tests for the keeper: block parser added
// to LoadProjectConfig (hk-lhu2).
//
// Covers:
//   - keeper block absent → KeeperConfig zero value (no error).
//   - keeper.context_thresholds values parsed correctly.
//   - keeper.warn_messages values parsed correctly.
//   - Values ≤ 0 for numeric thresholds → not configured (zero in KeeperConfig).
//   - Empty strings for warn_messages → stored as empty (caller treats as "not configured").
//   - keeper block only (no agents, no daemon) + schema_version: 1 → parsed correctly.
//   - Existing agents + daemon blocks still load alongside keeper block.
//   - Precedence simulation: CLI > config > compiled default (zero → applyDefaults).
//
// Helper prefix: keeperBlkFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-lhu2.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// keeperBlkFixtureDir reuses projCfgFixtureDir to create a temp dir with a
// .harmonik/ subdirectory and optionally writes config.yaml content.
func keeperBlkFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	return projCfgFixtureDir(t, yamlContent)
}

// ─────────────────────────────────────────────────────────────────────────────
// Keeper block absent / empty
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlock_Absent_ZeroValue(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: sonnet
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Keeper != (daemon.ExportedKeeperConfig{}) {
		t.Errorf("absent keeper block: want zero KeeperConfig; got %+v", cfg.Keeper)
	}
}

func TestKeeperBlock_EmptyBlock_ZeroValue(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper: {}
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Keeper != (daemon.ExportedKeeperConfig{}) {
		t.Errorf("empty keeper block: want zero KeeperConfig; got %+v", cfg.Keeper)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// context_thresholds parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlock_ContextThresholds_AllSet(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 250000
    act_abs_tokens: 290000
    force_act_abs_tokens: 340000
    act_pct_ceil: 0.82
    warn_pct_ceil: 0.65
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.WarnAbsTokens != 250000 {
		t.Errorf("WarnAbsTokens: want 250000, got %d", k.WarnAbsTokens)
	}
	if k.ActAbsTokens != 290000 {
		t.Errorf("ActAbsTokens: want 290000, got %d", k.ActAbsTokens)
	}
	if k.ForceActAbsTokens != 340000 {
		t.Errorf("ForceActAbsTokens: want 340000, got %d", k.ForceActAbsTokens)
	}
	if k.ActPctCeil != 0.82 {
		t.Errorf("ActPctCeil: want 0.82, got %f", k.ActPctCeil)
	}
	if k.WarnPctCeil != 0.65 {
		t.Errorf("WarnPctCeil: want 0.65, got %f", k.WarnPctCeil)
	}
}

func TestKeeperBlock_ContextThresholds_ZeroValue_NotConfigured(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 0
    act_abs_tokens: 0
    force_act_abs_tokens: 0
    act_pct_ceil: 0
    warn_pct_ceil: 0
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.WarnAbsTokens != 0 {
		t.Errorf("WarnAbsTokens ≤0: want 0 (not-configured), got %d", k.WarnAbsTokens)
	}
	if k.ActAbsTokens != 0 {
		t.Errorf("ActAbsTokens ≤0: want 0 (not-configured), got %d", k.ActAbsTokens)
	}
	if k.ForceActAbsTokens != 0 {
		t.Errorf("ForceActAbsTokens ≤0: want 0 (not-configured), got %d", k.ForceActAbsTokens)
	}
	if k.ActPctCeil != 0 {
		t.Errorf("ActPctCeil ≤0: want 0 (not-configured), got %f", k.ActPctCeil)
	}
	if k.WarnPctCeil != 0 {
		t.Errorf("WarnPctCeil ≤0: want 0 (not-configured), got %f", k.WarnPctCeil)
	}
}

func TestKeeperBlock_ContextThresholds_NegativeValue_NotConfigured(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: -1
    act_abs_tokens: -100
    force_act_abs_tokens: -999
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.WarnAbsTokens != 0 {
		t.Errorf("WarnAbsTokens negative: want 0 (not-configured), got %d", k.WarnAbsTokens)
	}
	if k.ActAbsTokens != 0 {
		t.Errorf("ActAbsTokens negative: want 0 (not-configured), got %d", k.ActAbsTokens)
	}
	if k.ForceActAbsTokens != 0 {
		t.Errorf("ForceActAbsTokens negative: want 0 (not-configured), got %d", k.ForceActAbsTokens)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// warn_messages parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlock_WarnMessages_Override(t *testing.T) {
	t.Parallel()

	const defaultText = "custom default warn"
	const onDemandText = "custom on-demand warn"

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  warn_messages:
    default_warn_text: "custom default warn"
    on_demand_warn_text: "custom on-demand warn"
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.DefaultWarnText != defaultText {
		t.Errorf("DefaultWarnText: want %q, got %q", defaultText, k.DefaultWarnText)
	}
	// hk-vs4u: on_demand_warn_text is now a DEPRECATED alias of actionable_warn_text.
	// Its value maps onto ActionableWarnText (with a log warning).
	if k.ActionableWarnText != onDemandText {
		t.Errorf("ActionableWarnText (via deprecated on_demand_warn_text alias): want %q, got %q", onDemandText, k.ActionableWarnText)
	}
}

func TestKeeperBlock_WarnMessages_EmptyStrings_StillEmpty(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  warn_messages:
    default_warn_text: ""
    on_demand_warn_text: ""
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.DefaultWarnText != "" {
		t.Errorf("DefaultWarnText empty: want \"\", got %q", k.DefaultWarnText)
	}
	// hk-vs4u: empty on_demand_warn_text does not populate ActionableWarnText.
	if k.ActionableWarnText != "" {
		t.Errorf("ActionableWarnText empty: want \"\", got %q", k.ActionableWarnText)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// keeper block alongside existing blocks
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeperBlock_AlongsideDaemonAndAgents(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
agents:
  claude-code:
    model: sonnet
daemon:
  max_concurrent: 4
keeper:
  context_thresholds:
    warn_abs_tokens: 260000
    act_abs_tokens: 295000
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	// agents block still works
	m, _ := cfg.LookupAgent("claude-code")
	if m != "sonnet" {
		t.Errorf("agents block: model: want %q, got %q", "sonnet", m)
	}
	// daemon block still works
	if cfg.Daemon.MaxConcurrent != 4 {
		t.Errorf("daemon block: max_concurrent: want 4, got %d", cfg.Daemon.MaxConcurrent)
	}
	// keeper block parsed correctly
	if cfg.Keeper.WarnAbsTokens != 260000 {
		t.Errorf("keeper.WarnAbsTokens: want 260000, got %d", cfg.Keeper.WarnAbsTokens)
	}
	if cfg.Keeper.ActAbsTokens != 295000 {
		t.Errorf("keeper.ActAbsTokens: want 295000, got %d", cfg.Keeper.ActAbsTokens)
	}
}

func TestKeeperBlock_OnlyKeeperBlock_NoAgentsNoDaemon(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    force_act_abs_tokens: 340000
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Keeper.ForceActAbsTokens != 340000 {
		t.Errorf("ForceActAbsTokens: want 340000, got %d", cfg.Keeper.ForceActAbsTokens)
	}
	if cfg.Daemon.WorkflowMode != "" || cfg.Daemon.MaxConcurrent != 0 || cfg.Daemon.TargetBranch != "" || len(cfg.Daemon.AllowedRepos) != 0 {
		t.Errorf("daemon block should be zero when absent; got %+v", cfg.Daemon)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Precedence: config value vs zero (compiled default applied by applyDefaults)
// ─────────────────────────────────────────────────────────────────────────────

// TestKeeperBlock_Precedence_ConfigOverridesZero verifies that a non-zero config
// value flows through to the caller (non-zero → caller should prefer it over the
// compiled default). The actual applyDefaults() skip-when-non-zero logic lives in
// the CyclerConfig — this test just ensures the config value is delivered.
func TestKeeperBlock_Precedence_ConfigOverridesZero(t *testing.T) {
	t.Parallel()

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 250001
    act_abs_tokens: 299999
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	// Non-zero config values are delivered to the caller (CLI > config > default logic
	// is in keeper_cmd.go; here we just verify the value reaches KeeperConfig).
	if k.WarnAbsTokens != 250001 {
		t.Errorf("WarnAbsTokens: want 250001 (config), got %d", k.WarnAbsTokens)
	}
	if k.ActAbsTokens != 299999 {
		t.Errorf("ActAbsTokens: want 299999 (config), got %d", k.ActAbsTokens)
	}
}
