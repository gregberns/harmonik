package main

// config_keeper_delivery_e1mdc_test.go — T2 (hk-keeper-delivery-config-surface-e1mdc):
// the new keeper.warn_messages keys reach WatcherConfig through the REAL
// load→resolve→construct path (daemon.LoadProjectConfig → ResolveKeeperConfig →
// buildKeeperConfigs), with no rebuild, exactly like default_warn_text /
// actionable_warn_text. Also pins crew default-off. Spec: SK-032.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// e1mdcConfigBase is a COMPLETE keeper block (ResolveKeeperConfig refuses an
// incomplete one). Self-contained (not the integration-tagged e2eConfigYAML) so
// this acceptance test runs in the default suite. The warn_messages block is
// appended per-case below.
const e1mdcConfigBase = `schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 180000
    act_abs_tokens: 195000
    force_act_abs_offset: 20000
    idle_floor_abs_tokens: 150000
    warn_pct_ceil: 0.70
    act_pct_ceil: 0.85
  hard_ceiling:
    abs_tokens: 250000
    mode: alarm
    cooldown: 5m
  timings:
    poll_interval: 5s
    cycler_poll_interval: 200ms
    idle_quiesce: 8s
    staleness: 90s
    handoff_timeout: 3m
    clear_settle: 3s
    boot_grace: 5m
  cadence:
    warn_cooldown: 30s
    no_gauge_backoff: 30s
    respawn_grace: 20s
    respawn_cooldown: 90s
    live_recover_grace: 5m
    live_recover_cooldown: 5m
    force_retry_interval: 2m
    idle_restart_cooldown: 30m
    hard_ceiling_cooldown: 5m
    blind_keeper_threshold: 5m
    hold_ttl: 45m
    operator_turn_lookback: 5m
    post_answer_grace: 45s
  budgets:
    heartbeat_max_misses: 12
    max_handoff_timeouts: 3
`

// e1mdcConfigWithKeys carries both T2 delivery keys under test.
const e1mdcConfigWithKeys = e1mdcConfigBase + `  warn_messages:
    leader_defer_text: "finish the unit, then harmonik keeper restart-now --agent x"
    crew_defer_text: "crew finish-then-self-restart"
`

// e1mdcConfigNoCrewKey sets ONLY the leader key, to prove crew_defer_text
// defaults empty/off end-to-end.
const e1mdcConfigNoCrewKey = e1mdcConfigBase + `  warn_messages:
    leader_defer_text: "leader only"
`

func writeE1mdcProject(t *testing.T, yaml string) string {
	t.Helper()
	projectDir := t.TempDir()
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile config.yaml: %v", err)
	}
	return projectDir
}

func TestConfigE2E_DeliveryKeysReachWatcherConfig_e1mdc(t *testing.T) {
	projectDir := writeE1mdcProject(t, e1mdcConfigWithKeys)

	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	keeperCfg := projCfg.Keeper

	resolved, err := ResolveKeeperConfig(KeeperFlags{}, keeperCfg, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig: %v", err)
	}
	_, watcherCfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:  "e1mdc-agent",
		ProjectDir: projectDir,
		KeeperCfg:  keeperCfg,
	})

	const wantLeader = "finish the unit, then harmonik keeper restart-now --agent x"
	const wantCrew = "crew finish-then-self-restart"
	if watcherCfg.LeaderDeferText != wantLeader {
		t.Errorf("WatcherConfig.LeaderDeferText = %q; want config value %q (must reach WatcherConfig)", watcherCfg.LeaderDeferText, wantLeader)
	}
	if watcherCfg.CrewDeferText != wantCrew {
		t.Errorf("WatcherConfig.CrewDeferText = %q; want config value %q", watcherCfg.CrewDeferText, wantCrew)
	}
}

func TestConfigE2E_CrewDeferKeyDefaultsOff_e1mdc(t *testing.T) {
	projectDir := writeE1mdcProject(t, e1mdcConfigNoCrewKey)

	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	resolved, err := ResolveKeeperConfig(KeeperFlags{}, projCfg.Keeper, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig: %v", err)
	}
	_, watcherCfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:  "e1mdc-agent",
		ProjectDir: projectDir,
		KeeperCfg:  projCfg.Keeper,
	})

	// Crew key absent → empty at the watcher; K7 config hook is default-off and
	// nothing consumes it, so no crew behavior fires.
	if watcherCfg.CrewDeferText != "" {
		t.Errorf("WatcherConfig.CrewDeferText = %q; want empty (crew key default-off)", watcherCfg.CrewDeferText)
	}
	// The leader key still threads.
	if watcherCfg.LeaderDeferText != "leader only" {
		t.Errorf("WatcherConfig.LeaderDeferText = %q; want %q", watcherCfg.LeaderDeferText, "leader only")
	}
}
