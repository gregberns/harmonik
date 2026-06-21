//go:build integration

package main

// config_e2e_integration_test.go — LIVE/integration definition-of-done test for
// hk-yy57: prove that config-driven keeper thresholds are honored END-TO-END with
// ZERO CLI flags, that a CLI flag overrides config (flag>config), and that the
// emitted session_keeper_hard_ceiling payload carries the CONFIGURED threshold
// (250000), NOT the compiled 280000 default.
//
// Why integration-tagged (NOT default `go test`/CI): it drives the REAL
// resolution + Watcher/Cycler-construction path the `harmonik keeper` command uses
// (daemon.LoadProjectConfig → ResolveKeeperConfig → buildKeeperConfigs), runs a
// live watcher loop, and optionally spawns a real tmux pane — consistent with the
// other *_integration_test.go files behind the same tag.
//
// What it exercises (NOT fakes):
//   - daemon.LoadProjectConfig reads a real on-disk .harmonik/config.yaml.
//   - ResolveKeeperConfig applies the REAL FLAG > CONFIG > DEFAULT precedence.
//   - buildKeeperConfigs builds the SAME WatcherConfig/CyclerConfig literals the
//     production keeper start path (runKeeperSubcommand) constructs.
//   - the watcher EMITS a real session_keeper_hard_ceiling event whose payload we
//     read back and assert against.
//
// Compiled defaults (for the "NOT the default" assertions): warn 200000, act
// 215000, force_act 240000 (215000+25000), hard_ceiling 280000, staleness 120s.

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
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// e2eConfigYAML is a .harmonik/config.yaml whose keeper: block carries NON-default
// but valid values. schema_version is uncommented (== 1). The thresholds obey the
// band invariant warn < act < force_act < hard_ceiling:
//
//	warn 180000 < act 195000 < force_act (195000+20000=215000) < hard_ceiling 250000.
//
// COMPLETE keeper block (operator-required-config change: every required value must
// be present or ResolveKeeperConfig refuses to start). The values under test are the
// NON-default ones (warn 180000, act 195000, force_act_abs_offset 20000, hard_ceiling
// 250000, staleness 90s); the rest are suggested values just to satisfy the gate.
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

// writeE2EProject creates a temp project dir with the e2e config.yaml and returns
// the project dir.
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

	// REAL config load (reads the on-disk .harmonik/config.yaml).
	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	keeperCfg := projCfg.Keeper

	// ── Part 1: ZERO CLI flags → effective == CONFIG (not compiled defaults). ──
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

	// Guard: every "want" must differ from the compiled default, else the test
	// would pass trivially.
	if wantWarn == keeper.DefaultWarnAbsTokens || wantAct == keeper.DefaultActAbsTokens ||
		wantCeiling == keeper.DefaultHardCeilingTokens || wantStale == keeper.DefaultStaleness {
		t.Fatalf("test config values collide with compiled defaults — assertions would be trivial")
	}

	// WatcherConfig effective thresholds (config-resolved, NOT defaults).
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
	// CyclerConfig effective thresholds.
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

	// ── Part 2: flag>config — an explicit --staleness override wins over the 90s
	// config value. We resolve again with the flag set (Set=true) and confirm the
	// effective staleness is the FLAG value, not the config 90s.
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

	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	keeperCfg := projCfg.Keeper

	resolved, err := ResolveKeeperConfig(KeeperFlags{}, keeperCfg, projectDir)
	if err != nil {
		t.Fatalf("ResolveKeeperConfig: %v", err)
	}

	// Spawn ONE throwaway tmux pane (bash), unique name, kill in cleanup. The watcher
	// tick does not strictly require a live Claude pane for the foreign-session
	// hard-ceiling alarm (it is gauge-file driven), but per hk-yy57 we exercise a real
	// pane lifecycle and prove no leak. tmux absence is non-fatal — the alarm path
	// runs regardless.
	tmuxTarget := spawnThrowawayTmuxPane(t)

	_, watcherCfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:    agent,
		ProjectDir:   projectDir,
		ResolvedTmux: tmuxTarget,
		KeeperCfg:    keeperCfg,
	})

	// Sanity: the config-resolved ceiling/mode reached the WatcherConfig.
	if watcherCfg.HardCeilingTokens != 250_000 {
		t.Fatalf("precondition: WatcherConfig.HardCeilingTokens = %d; want config 250000",
			watcherCfg.HardCeilingTokens)
	}
	if watcherCfg.HardCeilingMode != keeper.HardCeilingModeAlarm {
		t.Fatalf("precondition: WatcherConfig.HardCeilingMode = %v; want alarm", watcherCfg.HardCeilingMode)
	}

	// Wire the foreign-session seam (same pattern backstop_test.go uses): the managed
	// binding + .sid both endorse "sess-managed" while the gauge bears "sess-foreign",
	// so every tick is a foreign_session — the ONLY path the SID-independent
	// hard-ceiling gate runs on. Tighten the poll cadence + idle quiesce for test speed.
	watcherCfg.PollInterval = 5 * time.Millisecond
	watcherCfg.IdleQuiesce = 1 * time.Millisecond
	watcherCfg.Staleness = 120 * time.Second // keep the gauge fresh during the short run
	watcherCfg.HardCeilingCooldown = 10 * time.Second
	watcherCfg.ReadManagedSessionFn = func(_, _ string) (string, error) { return "sess-managed", nil }
	watcherCfg.WriteManagedSessionFn = func(_, _, _ string) error { return nil }
	watcherCfg.ReadSidFn = func(_, _ string) (string, time.Time, error) {
		return "sess-managed", time.Time{}, nil
	}
	// HeartbeatEnabled writes the gauge over the foreign sid; disable for this test so
	// the foreign-session gauge we wrote is the one the gate reads.
	watcherCfg.HeartbeatEnabled = false

	// Write the gauge (CtxFile) with a foreign session_id and tokens >= 250000 so the
	// configured ceiling trips (and BELOW the 280000 default, so it ONLY trips when the
	// configured value is honored).
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

	// Run the live watcher loop briefly; the foreign-session hard-ceiling alarm must
	// fire (alarm mode = emit-only).
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
	// THE CORE ASSERTION: the emitted payload carries the CONFIGURED 250000, NOT the
	// 280000 compiled default — config flowed config.yaml → ResolveKeeperConfig →
	// buildKeeperConfigs → WatcherConfig → emitted event.
	if payload.HardCeiling != 250_000 {
		t.Errorf("payload.HardCeiling = %d; want CONFIGURED 250000 (NOT the %d default)",
			payload.HardCeiling, keeper.DefaultHardCeilingTokens)
	}
	if payload.HardCeiling == keeper.DefaultHardCeilingTokens {
		t.Errorf("payload.HardCeiling = %d == compiled default; configured value NOT wired through",
			payload.HardCeiling)
	}
}

// spawnThrowawayTmuxPane starts ONE detached tmux session running bash, with a
// unique name, and registers a t.Cleanup that kills it. It snapshots `tmux ls`
// before and after (best-effort) to prove no pane leaked. If tmux is unavailable
// the test continues without a pane (the hard-ceiling alarm is gauge-file driven,
// not pane-dependent). Returns the tmux target "<session>:0" or "" when no pane was
// spawned. Refs: hk-yy57 (spawn ONE pane, kill in cleanup, do NOT leak).
func spawnThrowawayTmuxPane(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Logf("tmux not available (%v); continuing without a live pane (alarm path is gauge-driven)", err)
		return ""
	}

	// Session name must avoid characters tmux mangles (it rewrites "." and ":"),
	// else the kill-target string and the snapshot name diverge. Use a bare
	// nanosecond stamp (digits only).
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
		// Belt-and-suspenders: confirm we did not leave any NEW hk-yy57-e2e-* session.
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

// tmuxSessionSnapshot returns the set of current tmux session names (best-effort;
// empty set when tmux has no server / no sessions).
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
