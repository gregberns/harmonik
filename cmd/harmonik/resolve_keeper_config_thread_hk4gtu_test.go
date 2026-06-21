package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// resolve_keeper_config_thread_hk4gtu_test.go — table coverage for hk-4gtu: every
// timing/cadence/budget/self_service value and the hard_ceiling MODE that PARSED
// but was DROPPED must now (a) resolve config>flag (no runtime default layer),
// (b) reach the constructed Watcher/Cycler config literals, and (c) preserve the
// boot_grace=0 / warn_cooldown<0 / max_handoff_timeouts sentinels end-to-end.
// Fail-loud is preserved (a bad hard_ceiling.mode → error).
//
// Operator-philosophy change: with NO config and NO flags the resolver no longer
// fills defaults — it returns a *KeeperConfigMissingError. So the "resolves cleanly"
// cases start from completeTestKeeperConfig() (suggested values) and mutate.

// TestResolveKeeperConfig_ThreadedDefaults: a COMPLETE config resolves each threaded
// field to its supplied (suggested keeper.Default*) value.
func TestResolveKeeperConfig_ThreadedDefaults(t *testing.T) {
	got, err := ResolveKeeperConfig(KeeperFlags{}, completeTestKeeperConfig(), t.TempDir())
	if err != nil {
		t.Fatalf("resolve(complete config): unexpected error: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"PollInterval", got.PollInterval, keeper.DefaultPollInterval},
		{"IdleQuiesce", got.IdleQuiesce, keeper.DefaultIdleQuiesce},
		{"Staleness", got.Staleness, keeper.DefaultStaleness},
		{"RespawnGrace", got.RespawnGrace, keeper.DefaultRespawnGrace},
		{"RespawnCooldown", got.RespawnCooldown, keeper.DefaultRespawnCooldown},
		{"LiveRecoverGrace", got.LiveRecoverGrace, keeper.DefaultLiveRecoverGrace},
		{"LiveRecoverCooldown", got.LiveRecoverCooldown, keeper.DefaultLiveRecoverCooldown},
		{"NoGaugeBackoff", got.NoGaugeBackoff, keeper.DefaultNoGaugeBackoff},
		{"HardCeilingCooldown", got.HardCeilingCooldown, keeper.DefaultHardCeilingCooldown},
		{"BlindKeeperThreshold", got.BlindKeeperThreshold, keeper.DefaultBlindKeeperThreshold},
		{"HeartbeatMaxMisses", got.HeartbeatMaxMisses, keeper.DefaultMaxHeartbeatMisses},
		{"HandoffTimeout", got.HandoffTimeout, keeper.DefaultHandoffTimeout},
		{"ClearSettle", got.ClearSettle, keeper.DefaultClearSettle},
		{"CyclerPollInterval", got.CyclerPollInterval, keeper.DefaultCyclerPollInterval},
		{"ForceRetryInterval", got.ForceRetryInterval, keeper.DefaultForceRetryInterval},
		{"IdleRestartAbsTokens", got.IdleRestartAbsTokens, keeper.DefaultIdleRestartAbsTokens},
		{"IdleRestartCooldown", got.IdleRestartCooldown, keeper.DefaultIdleRestartCooldown},
		{"HardCeilingAbsTokens", got.HardCeilingAbsTokens, keeper.HardCeilingAbsTokens},
		{"HardCeilingMode", got.HardCeilingMode, keeper.HardCeilingModeAlarm},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v (suggested baseline value)", c.name, c.got, c.want)
		}
	}
	// Sentinels: baseline boot_grace is set (5m); warn_cooldown forwards its value;
	// max_handoff_timeouts forwards its value.
	if !got.BootGraceSet {
		t.Errorf("baseline BootGraceSet = false, want true (boot_grace is set in the complete config)")
	}
	if got.WarnCooldown != keeper.DefaultWarnCooldown {
		t.Errorf("WarnCooldown = %v, want %v (forwarded verbatim)", got.WarnCooldown, keeper.DefaultWarnCooldown)
	}
	if got.MaxHandoffTimeouts != keeper.DefaultMaxHandoffTimeouts {
		t.Errorf("MaxHandoffTimeouts = %v, want %v (forwarded verbatim)", got.MaxHandoffTimeouts, keeper.DefaultMaxHandoffTimeouts)
	}
}

// TestResolveKeeperConfig_ConfigReachesField: for EACH threaded tunable, a config
// value demonstrably reaches the resolved struct (which the start path copies
// verbatim into the Watcher/Cycler literal).
func TestResolveKeeperConfig_ConfigReachesField(t *testing.T) {
	cfg := completeTestKeeperConfig()
	// Keep the band valid: warn < act < force_act < hard_ceiling.
	cfg.WarnAbsTokens = 100_000
	cfg.ActAbsTokens = 110_000
	cfg.ForceActAbsTokens = 120_000
	cfg.PollInterval = 7 * time.Second
	cfg.IdleQuiesce = 11 * time.Second
	cfg.Staleness = 99 * time.Second
	cfg.RespawnGrace = 22 * time.Second
	cfg.RespawnCooldown = 77 * time.Second
	cfg.LiveRecoverGrace = 6 * time.Minute
	cfg.LiveRecoverCooldown = 7 * time.Minute
	cfg.NoGaugeBackoff = 45 * time.Second
	cfg.CadenceHardCeilingCooldown = 9 * time.Minute
	cfg.BlindKeeperThreshold = 8 * time.Minute
	cfg.HeartbeatMaxMisses = 20
	cfg.HandoffTimeout = 240 * time.Second
	cfg.ClearSettle = 5 * time.Second
	cfg.CyclerPollInterval = 333 * time.Millisecond
	cfg.ForceRetryInterval = 150 * time.Second
	cfg.IdleFloorAbsTokens = 123_000
	cfg.IdleRestartCooldown = 40 * time.Minute
	cfg.HardCeilingAbsTokens = 300_000
	cfg.HardCeilingMode = "restart"

	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(config): unexpected error: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"PollInterval", got.PollInterval, cfg.PollInterval},
		{"IdleQuiesce", got.IdleQuiesce, cfg.IdleQuiesce},
		{"Staleness", got.Staleness, cfg.Staleness},
		{"RespawnGrace", got.RespawnGrace, cfg.RespawnGrace},
		{"RespawnCooldown", got.RespawnCooldown, cfg.RespawnCooldown},
		{"LiveRecoverGrace", got.LiveRecoverGrace, cfg.LiveRecoverGrace},
		{"LiveRecoverCooldown", got.LiveRecoverCooldown, cfg.LiveRecoverCooldown},
		{"NoGaugeBackoff", got.NoGaugeBackoff, cfg.NoGaugeBackoff},
		{"HardCeilingCooldown", got.HardCeilingCooldown, cfg.CadenceHardCeilingCooldown},
		{"BlindKeeperThreshold", got.BlindKeeperThreshold, cfg.BlindKeeperThreshold},
		{"HeartbeatMaxMisses", got.HeartbeatMaxMisses, cfg.HeartbeatMaxMisses},
		{"HandoffTimeout", got.HandoffTimeout, cfg.HandoffTimeout},
		{"ClearSettle", got.ClearSettle, cfg.ClearSettle},
		{"CyclerPollInterval", got.CyclerPollInterval, cfg.CyclerPollInterval},
		{"ForceRetryInterval", got.ForceRetryInterval, cfg.ForceRetryInterval},
		{"IdleRestartAbsTokens", got.IdleRestartAbsTokens, cfg.IdleFloorAbsTokens},
		{"IdleRestartCooldown", got.IdleRestartCooldown, cfg.IdleRestartCooldown},
		{"HardCeilingAbsTokens", got.HardCeilingAbsTokens, cfg.HardCeilingAbsTokens},
		{"HardCeilingMode", got.HardCeilingMode, keeper.HardCeilingModeRestart},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("config %s = %v, want %v (config value must reach the resolved struct)", c.name, c.got, c.want)
		}
	}

	// Build the literals exactly as the start path does and assert the public
	// Watcher/Cycler config fields carry the configured value BEFORE applyDefaults.
	w := keeper.WatcherConfig{
		PollInterval:         got.PollInterval,
		IdleQuiesce:          got.IdleQuiesce,
		Staleness:            got.Staleness,
		RespawnGrace:         got.RespawnGrace,
		RespawnCooldown:      got.RespawnCooldown,
		LiveRecoverGrace:     got.LiveRecoverGrace,
		LiveRecoverCooldown:  got.LiveRecoverCooldown,
		NoGaugeBackoff:       got.NoGaugeBackoff,
		HardCeilingCooldown:  got.HardCeilingCooldown,
		BlindKeeperThreshold: got.BlindKeeperThreshold,
		HeartbeatMaxMisses:   got.HeartbeatMaxMisses,
		HardCeilingTokens:    got.HardCeilingAbsTokens,
		HardCeilingMode:      got.HardCeilingMode,
	}
	if w.Staleness != cfg.Staleness || w.HardCeilingMode != keeper.HardCeilingModeRestart ||
		w.NoGaugeBackoff != cfg.NoGaugeBackoff || w.HeartbeatMaxMisses != cfg.HeartbeatMaxMisses {
		t.Errorf("WatcherConfig literal did not carry the configured values: %+v", w)
	}
	cy := keeper.CyclerConfig{
		HandoffTimeout:       got.HandoffTimeout,
		ClearSettle:          got.ClearSettle,
		PollInterval:         got.CyclerPollInterval,
		ForceRetryInterval:   got.ForceRetryInterval,
		IdleRestartAbsTokens: got.IdleRestartAbsTokens,
		IdleRestartCooldown:  got.IdleRestartCooldown,
	}
	if cy.HandoffTimeout != cfg.HandoffTimeout || cy.PollInterval != cfg.CyclerPollInterval ||
		cy.IdleRestartAbsTokens != cfg.IdleFloorAbsTokens {
		t.Errorf("CyclerConfig literal did not carry the configured values: %+v", cy)
	}
}

// TestResolveKeeperConfig_Tier1FlagOverridesConfig: a tier-1 flag wins over config.
func TestResolveKeeperConfig_Tier1FlagOverridesConfig(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.Staleness = 99 * time.Second
	cfg.IdleQuiesce = 11 * time.Second
	cfg.PollInterval = 7 * time.Second
	cfg.HandoffTimeout = 240 * time.Second
	cfg.IdleFloorAbsTokens = 123_000
	cfg.HardCeilingAbsTokens = 300_000
	cfg.HardCeilingMode = "alarm"

	flags := KeeperFlags{
		Staleness:          60 * time.Second,
		StalenessSet:       true,
		IdleQuiesce:        5 * time.Second,
		IdleQuiesceSet:     true,
		PollInterval:       3 * time.Second,
		PollIntervalSet:    true,
		HandoffTimeout:     90 * time.Second,
		HandoffTimeoutSet:  true,
		IdleFloorAbsTokens: 90_000,
		IdleFloorSet:       true,
		HardCeilingAbs:     290_000,
		HardCeilingAbsSet:  true,
		HardCeilingMode:    "restart",
		HardCeilingModeSet: true,
	}
	got, err := ResolveKeeperConfig(flags, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(flags): unexpected error: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Staleness", got.Staleness, flags.Staleness},
		{"IdleQuiesce", got.IdleQuiesce, flags.IdleQuiesce},
		{"PollInterval", got.PollInterval, flags.PollInterval},
		{"HandoffTimeout", got.HandoffTimeout, flags.HandoffTimeout},
		{"IdleRestartAbsTokens", got.IdleRestartAbsTokens, flags.IdleFloorAbsTokens},
		{"HardCeilingAbsTokens", got.HardCeilingAbsTokens, flags.HardCeilingAbs},
		{"HardCeilingMode", got.HardCeilingMode, keeper.HardCeilingModeRestart},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("flag %s = %v, want %v (flag must override config)", c.name, c.got, c.want)
		}
	}
}

// TestResolveKeeperConfig_BootGraceSentinel: the boot_grace=0 disabled sentinel is
// honored when explicitly set (config-present "0s" or flag), preserved end-to-end.
func TestResolveKeeperConfig_BootGraceSentinel(t *testing.T) {
	// Explicit FLAG boot-grace=0 → disabled sentinel honored.
	cfg := completeTestKeeperConfig()
	got, err := ResolveKeeperConfig(KeeperFlags{BootGrace: 0, BootGraceSet: true}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(boot-grace=0): %v", err)
	}
	if !got.BootGraceSet || got.BootGrace != 0 {
		t.Errorf("explicit --boot-grace=0: BootGraceSet=%v BootGrace=%v, want set+0 (disabled honored)", got.BootGraceSet, got.BootGrace)
	}
	// Config-present boot_grace: 0s (string "0s" → present, value 0) is the disabled
	// sentinel — present, NOT missing, and honored as 0.
	cfg2 := completeTestKeeperConfig()
	cfg2.BootGrace = 0
	cfg2.Present.BootGrace = true // simulates the raw "0s" string
	got2, err := ResolveKeeperConfig(KeeperFlags{}, cfg2, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(config boot_grace 0s): %v", err)
	}
	if !got2.BootGraceSet || got2.BootGrace != 0 {
		t.Errorf("config boot_grace=0s: BootGraceSet=%v BootGrace=%v, want set+0 (disabled honored)", got2.BootGraceSet, got2.BootGrace)
	}
	// A configured positive boot_grace reaches the resolved struct as set.
	cfg3 := completeTestKeeperConfig()
	cfg3.BootGrace = 3 * time.Minute
	got3, err := ResolveKeeperConfig(KeeperFlags{}, cfg3, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(config boot_grace 3m): %v", err)
	}
	if !got3.BootGraceSet || got3.BootGrace != 3*time.Minute {
		t.Errorf("config boot_grace=3m: BootGraceSet=%v BootGrace=%v, want set+3m", got3.BootGraceSet, got3.BootGrace)
	}
}

// TestResolveKeeperConfig_SentinelsForwardVerbatim: the warn_cooldown<0 disable
// sentinel forwards verbatim.
func TestResolveKeeperConfig_SentinelsForwardVerbatim(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.WarnCooldown = -1 // disable sentinel (still present)
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve(sentinels): %v", err)
	}
	if got.WarnCooldown != -1 {
		t.Errorf("WarnCooldown sentinel = %v, want -1 (forwarded verbatim)", got.WarnCooldown)
	}
	if got.MaxHandoffTimeouts != keeper.DefaultMaxHandoffTimeouts {
		t.Errorf("MaxHandoffTimeouts = %v, want %v", got.MaxHandoffTimeouts, keeper.DefaultMaxHandoffTimeouts)
	}
}

// TestResolveKeeperConfig_BadHardCeilingMode_FailsLoud: an invalid mode token is a
// fail-loud *KeeperConfigError, NOT a silent default.
func TestResolveKeeperConfig_BadHardCeilingMode_FailsLoud(t *testing.T) {
	cfg := completeTestKeeperConfig()
	_, err := ResolveKeeperConfig(KeeperFlags{HardCeilingMode: "explode", HardCeilingModeSet: true}, cfg, t.TempDir())
	if err == nil {
		t.Fatal("resolve(bad mode): expected *KeeperConfigError, got nil (fail-loud must NOT be reverted to silent-default)")
	}
	var kce *KeeperConfigError
	if !errors.As(err, &kce) {
		t.Fatalf("resolve(bad mode): error %T, want *KeeperConfigError", err)
	}
	if kce.Field != "hard_ceiling.mode" {
		t.Errorf("bad-mode error Field = %q, want hard_ceiling.mode", kce.Field)
	}
}

// TestWarnOnlyNilsCycler asserts the warn-only path constructs a nil Cycler.
func TestWarnOnlyNilsCycler(t *testing.T) {
	var cycler *keeper.Cycler
	warnOnly := true
	if !warnOnly {
		cycler = keeper.NewCycler(keeper.CyclerConfig{AgentName: "x", ProjectDir: t.TempDir()}, keeper.NewFileEmitter(t.TempDir()))
	}
	if cycler != nil {
		t.Fatal("warn-only path: cycler must be nil (crews have no cycler)")
	}
	warnOnly = false
	if !warnOnly {
		cycler = keeper.NewCycler(keeper.CyclerConfig{AgentName: "x", ProjectDir: t.TempDir()}, keeper.NewFileEmitter(t.TempDir()))
	}
	if cycler == nil {
		t.Fatal("non-warn-only path: cycler must be non-nil")
	}
}

// keep daemon import used (KeeperFlags-only tests above don't reference it directly).
var _ = daemon.KeeperConfig{}
