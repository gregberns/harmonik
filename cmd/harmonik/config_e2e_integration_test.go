//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const e2eConfigYAML = `schema_version: 1
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
  budgets:
    heartbeat_max_misses: 12
    max_handoff_timeouts: 3
`

func writeE2EProject(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(e2eConfigYAML), 0o600); err != nil {
		t.Fatalf("WriteFile config.yaml: %v", err)
	}
	return projectDir
}

// TestConfigE2E_ZeroFlagInheritanceAndFlagOverride drives the REAL resolve+construct
// path with NO CLI threshold flags and asserts the constructed WatcherConfig /
// CyclerConfig EFFECTIVE thresholds equal the CONFIG values (not the compiled
// defaults) — proving zero-flag inheritance — then asserts flag>config for an
// explicit --staleness override.
func TestConfigE2E_ZeroFlagInheritanceAndFlagOverride(t *testing.T) {
	projectDir := writeE2EProject(t)

	projCfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	keeperCfg := projCfg.Keeper

	resolved, err := ResolveKeeperConfig(KeeperFlags{}, keeperCfg, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig (zero-flag): %v", err)
	}
	_, watcherCfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:  "e2e-zeroflag-agent",
		ProjectDir: projectDir,
		KeeperCfg:  keeperCfg,
	})
	cyclerCfg, _ := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:  "e2e-zeroflag-agent",
		ProjectDir: projectDir,
		KeeperCfg:  keeperCfg,
	})

	const (
		wantWarn     int64 = 180_000
		wantAct      int64 = 195_000
		wantForceAct int64 = 215_000 // act 195000 + offset 20000
		wantCeiling  int64 = 250_000
		wantStale          = 90 * time.Second
	)

	if wantWarn == keeper.DefaultWarnAbsTokens || wantAct == keeper.DefaultActAbsTokens ||
		wantCeiling == keeper.DefaultHardCeilingTokens || wantStale == keeper.DefaultStaleness {
		t.Fatalf("test config values collide with compiled defaults — assertions would be trivial")
	}

	if watcherCfg.WarnAbsTokens != wantWarn {
		t.Errorf("WatcherConfig.WarnAbsTokens = %d; want config %d (NOT default %d)",
			watcherCfg.WarnAbsTokens, wantWarn, keeper.DefaultWarnAbsTokens)
	}
	if watcherCfg.HardCeilingTokens != wantCeiling {
		t.Errorf("WatcherConfig.HardCeilingTokens = %d; want config %d (NOT default %d)",
			watcherCfg.HardCeilingTokens, wantCeiling, keeper.DefaultHardCeilingTokens)
	}
	if watcherCfg.HardCeilingMode != keeper.HardCeilingModeAlarm {
		t.Errorf("WatcherConfig.HardCeilingMode = %v; want alarm", watcherCfg.HardCeilingMode)
	}
	if watcherCfg.Staleness != wantStale {
		t.Errorf("WatcherConfig.Staleness = %v; want config %v (NOT default %v)",
			watcherCfg.Staleness, wantStale, keeper.DefaultStaleness)
	}
	if cyclerCfg.WarnAbsTokens != wantWarn {
		t.Errorf("CyclerConfig.WarnAbsTokens = %d; want config %d", cyclerCfg.WarnAbsTokens, wantWarn)
	}
	if cyclerCfg.ActAbsTokens != wantAct {
		t.Errorf("CyclerConfig.ActAbsTokens = %d; want config %d (NOT default %d)",
			cyclerCfg.ActAbsTokens, wantAct, keeper.DefaultActAbsTokens)
	}
	if cyclerCfg.ForceActAbsTokens != wantForceAct {
		t.Errorf("CyclerConfig.ForceActAbsTokens = %d; want act+offset %d (NOT default %d)",
			cyclerCfg.ForceActAbsTokens, wantForceAct, keeper.DefaultActAbsTokens+keeper.DefaultForceActAbsOffset)
	}

	const flagStale = 45 * time.Second
	if flagStale == wantStale {
		t.Fatalf("flag staleness collides with config staleness — override assertion trivial")
	}
	resolvedOverride, err := ResolveKeeperConfig(KeeperFlags{
		Staleness:    flagStale,
		StalenessSet: true,
	}, keeperCfg, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig (flag override): %v", err)
	}
	_, watcherCfgOverride := buildKeeperConfigs(resolvedOverride, keeperBuildParams{
		AgentName:  "e2e-flagoverride-agent",
		ProjectDir: projectDir,
		KeeperCfg:  keeperCfg,
	})
	if watcherCfgOverride.Staleness != flagStale {
		t.Errorf("flag>config: WatcherConfig.Staleness = %v; want FLAG %v (not config %v)",
			watcherCfgOverride.Staleness, flagStale, wantStale)
	}
}

// TestConfigE2E_HardCeilingPayloadCarriesConfiguredThreshold proves the
// hard-ceiling event payload carries the CONFIGURED 250000, NOT the 280000
// default — END-TO-END from the on-disk config.yaml. It takes the WatcherConfig
// produced by the REAL resolve+construct path (so HardCeilingTokens=250000,
// mode=alarm come from config), wires the foreign-session seam (so the
// SID-independent hard-ceiling gate is reached), drives a real CtxFile at >=250000,
// runs the live watcher loop, and reads the ACTUAL emitted event payload.
//
// Mode is alarm, so the watcher EMITS the event but does not call any restart fn
// (emit-only). A throwaway tmux pane is spawned and killed in t.Cleanup so the test
// also exercises a real pane lifecycle without leaking.
func TestConfigE2E_HardCeilingPayloadCarriesConfiguredThreshold(t *testing.T) {
	projectDir := writeE2EProject(t)
	agent := "e2e-hardceiling-agent"

	projCfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	keeperCfg := projCfg.Keeper

	resolved, err := ResolveKeeperConfig(KeeperFlags{}, keeperCfg, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig: %v", err)
	}

	tmuxTarget := spawnThrowawayTmuxPane(t)

	_, watcherCfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:    agent,
		ProjectDir:   projectDir,
		ResolvedTmux: tmuxTarget,
		KeeperCfg:    keeperCfg,
	})

	if watcherCfg.HardCeilingTokens != 250_000 {
		t.Fatalf("precondition: WatcherConfig.HardCeilingTokens = %d; want config 250000",
			watcherCfg.HardCeilingTokens)
	}
	if watcherCfg.HardCeilingMode != keeper.HardCeilingModeAlarm {
		t.Fatalf("precondition: WatcherConfig.HardCeilingMode = %v; want alarm", watcherCfg.HardCeilingMode)
	}

	watcherCfg.PollInterval = 5 * time.Millisecond
	watcherCfg.IdleQuiesce = 1 * time.Millisecond
	watcherCfg.Staleness = 120 * time.Second // keep the gauge fresh during the short run
	watcherCfg.HardCeilingCooldown = 10 * time.Second
	watcherCfg.ReadManagedSessionFn = func(_, _ string) (string, error) { return "sess-managed", nil }
	watcherCfg.WriteManagedSessionFn = func(_, _, _ string) error { return nil }
	watcherCfg.ReadSidFn = func(_, _ string) (string, time.Time, error) {
		return "sess-managed", time.Time{}, nil
	}
	watcherCfg.HeartbeatEnabled = false

	const tokens int64 = 255_000
	if tokens >= keeper.DefaultHardCeilingTokens {
		t.Fatalf("test tokens %d must be below the 280000 default ceiling to prove config wins", tokens)
	}
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll keeper dir: %v", err)
	}
	ctxData, err := json.Marshal(keeper.CtxFile{
		Pct:       60.0,
		Tokens:    tokens,
		SessionID: "sess-foreign",
		Ts:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Marshal CtxFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), append(ctxData, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile ctx: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	w := keeper.NewWatcher(watcherCfg, em)
	_ = w.Run(ctx) //nolint:errcheck // context.DeadlineExceeded is expected

	ceilEvents := em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)
	if len(ceilEvents) == 0 {
		t.Fatal("want >=1 session_keeper_hard_ceiling event at 255000 tokens with a 250000 configured ceiling; got 0")
	}
	var payload core.SessionKeeperHardCeilingPayload
	if err := json.Unmarshal(ceilEvents[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal hard_ceiling payload: %v", err)
	}
	if payload.AgentName != agent {
		t.Errorf("payload.AgentName = %q; want %q", payload.AgentName, agent)
	}
	if payload.ContextLen != tokens {
		t.Errorf("payload.ContextLen = %d; want %d", payload.ContextLen, tokens)
	}
	if payload.HardCeiling != 250_000 {
		t.Errorf("payload.HardCeiling = %d; want CONFIGURED 250000 (NOT the %d default)",
			payload.HardCeiling, keeper.DefaultHardCeilingTokens)
	}
	if payload.HardCeiling == keeper.DefaultHardCeilingTokens {
		t.Errorf("payload.HardCeiling = %d == compiled default; configured value NOT wired through",
			payload.HardCeiling)
	}
}

func spawnThrowawayTmuxPane(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Logf("tmux not available (%v); continuing without a live pane (alarm path is gauge-driven)", err)
		return ""
	}

	session := "hk-yy57-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	before := tmuxSessionSnapshot()

	cmd := exec.Command("tmux", "new-session", "-d", "-s", session, "bash")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("tmux new-session failed (%v: %s); continuing without a live pane", err, bytes.TrimSpace(out))
		return ""
	}

	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run() //nolint:errcheck
		after := tmuxSessionSnapshot()
		if _, stillThere := after[session]; stillThere {
			t.Errorf("LEAK: tmux session %q survived cleanup", session)
		}
		for s := range after {
			if _, existedBefore := before[s]; !existedBefore && s != session {
				if len(s) >= 10 && s[:10] == "hk-yy57-e2" {
					t.Errorf("LEAK: unexpected new tmux session %q after test", s)
				}
			}
		}
	})

	return session + ":0"
}

func tmuxSessionSnapshot() map[string]struct{} {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	set := map[string]struct{}{}
	if err != nil {
		return set
	}
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		set[string(line)] = struct{}{}
	}
	return set
}
