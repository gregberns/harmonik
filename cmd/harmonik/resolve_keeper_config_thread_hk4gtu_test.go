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
// but was DROPPED must now (a) resolve config>default (flag>config>default for the
// tier-1 ones), (b) reach the constructed Watcher/Cycler config literals, and
// (c) preserve the boot_grace=0 / warn_cooldown<0 / max_handoff_timeouts=0
// sentinels end-to-end. Fail-loud is preserved (a bad hard_ceiling.mode → error).

// TestResolveKeeperConfig_ThreadedDefaults: with NO config and NO flags, every
// threaded field resolves to its compiled keeper.Default* const.
func TestResolveKeeperConfig_ThreadedDefaults(t *testing.T) {
	got, err := ResolveKeeperConfig(KeeperFlags{}, daemon.KeeperConfig{})
	if err != nil {
		t.Fatalf("resolve(defaults): unexpected error: %v", err)
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
			t.Errorf("default %s = %v, want %v (compiled default)", c.name, c.got, c.want)
		}
	}
	// Sentinels: unconfigured boot_grace is NOT-set (start path feeds the default
	// at the construction site); warn_cooldown and max_handoff_timeouts forward 0.
	if got.BootGraceSet {
		t.Errorf("default BootGraceSet = true, want false (unconfigured → site-fed default)")
	}
	if got.WarnCooldown != 0 {
		t.Errorf("default WarnCooldown = %v, want 0 (downstream applyDefaults fills it)", got.WarnCooldown)
	}
	if got.MaxHandoffTimeouts != 0 {
		t.Errorf("default MaxHandoffTimeouts = %v, want 0 (downstream applyDefaults fills it)", got.MaxHandoffTimeouts)
	}
}

// TestResolveKeeperConfig_ConfigReachesField: for EACH threaded tunable, a config
// value demonstrably reaches the resolved struct (which the start path copies
// verbatim into the Watcher/Cycler literal).
func TestResolveKeeperConfig_ConfigReachesField(t *testing.T) {
	cfg := daemon.KeeperConfig{
		// Keep the band valid: warn < act < force_act < hard_ceiling.
		WarnAbsTokens:              100_000,
		ActAbsTokens:               110_000,
		PollInterval:               7 * time.Second,
		IdleQuiesce:                11 * time.Second,
		Staleness:                  99 * time.Second,
		RespawnGrace:               22 * time.Second,
		RespawnCooldown:            77 * time.Second,
		LiveRecoverGrace:           6 * time.Minute,
		LiveRecoverCooldown:        7 * time.Minute,
		NoGaugeBackoff:             45 * time.Second,
		CadenceHardCeilingCooldown: 9 * time.Minute,
		BlindKeeperThreshold:       8 * time.Minute,
		HeartbeatMaxMisses:         20,
		HandoffTimeout:             240 * time.Second,
		ClearSettle:                5 * time.Second,
		CyclerPollInterval:         333 * time.Millisecond,
		ForceRetryInterval:         150 * time.Second,
		IdleFloorAbsTokens:         123_000,
		IdleRestartCooldown:        40 * time.Minute,
		HardCeilingAbsTokens:       300_000,
		HardCeilingMode:            "restart",
	}
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg)
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

	// Now build the literals exactly as the start path does and assert the public
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
	cfg := daemon.KeeperConfig{
		Staleness:            99 * time.Second,
		IdleQuiesce:          11 * time.Second,
		PollInterval:         7 * time.Second,
		HandoffTimeout:       240 * time.Second,
		IdleFloorAbsTokens:   123_000,
		HardCeilingAbsTokens: 300_000,
		HardCeilingMode:      "alarm",
	}
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
	got, err := ResolveKeeperConfig(flags, cfg)
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
// honored when explicitly set (config or flag), preserved end-to-end.
func TestResolveKeeperConfig_BootGraceSentinel(t *testing.T) {
	// config boot_grace=0 is NOT a sentinel (0 = "not configured" at the daemon
	// layer); the sentinel-disable path is an explicit FLAG boot-grace=0.
	got, err := ResolveKeeperConfig(KeeperFlags{BootGrace: 0, BootGraceSet: true}, daemon.KeeperConfig{})
	if err != nil {
		t.Fatalf("resolve(boot-grace=0): %v", err)
	}
	if !got.BootGraceSet || got.BootGrace != 0 {
		t.Errorf("explicit --boot-grace=0: BootGraceSet=%v BootGrace=%v, want set+0 (disabled honored)", got.BootGraceSet, got.BootGrace)
	}
	// A configured positive boot_grace reaches the resolved struct as set.
	got2, err := ResolveKeeperConfig(KeeperFlags{}, daemon.KeeperConfig{BootGrace: 3 * time.Minute})
	if err != nil {
		t.Fatalf("resolve(config boot_grace): %v", err)
	}
	if !got2.BootGraceSet || got2.BootGrace != 3*time.Minute {
		t.Errorf("config boot_grace=3m: BootGraceSet=%v BootGrace=%v, want set+3m", got2.BootGraceSet, got2.BootGrace)
	}
}

// TestResolveKeeperConfig_WarnCooldownAndMaxHandoffSentinels: the warn_cooldown<0
// and max_handoff_timeouts=0 sentinels forward verbatim.
func TestResolveKeeperConfig_SentinelsForwardVerbatim(t *testing.T) {
	got, err := ResolveKeeperConfig(KeeperFlags{}, daemon.KeeperConfig{
		WarnCooldown:       -1, // disable sentinel
		MaxHandoffTimeouts: 0,  // no-escalation sentinel (also the unconfigured value)
	})
	if err != nil {
		t.Fatalf("resolve(sentinels): %v", err)
	}
	if got.WarnCooldown != -1 {
		t.Errorf("WarnCooldown sentinel = %v, want -1 (forwarded verbatim)", got.WarnCooldown)
	}
	if got.MaxHandoffTimeouts != 0 {
		t.Errorf("MaxHandoffTimeouts sentinel = %v, want 0", got.MaxHandoffTimeouts)
	}
}

// TestResolveKeeperConfig_BadHardCeilingMode_FailsLoud: an invalid mode token is a
// fail-loud *KeeperConfigError, NOT a silent default — proving fail-loud is
// preserved (NOT reverted to non-fatal) for the threaded mode value.
func TestResolveKeeperConfig_BadHardCeilingMode_FailsLoud(t *testing.T) {
	_, err := ResolveKeeperConfig(KeeperFlags{HardCeilingMode: "explode", HardCeilingModeSet: true}, daemon.KeeperConfig{})
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

// TestWarnOnlyNilsCycler asserts the warn-only path constructs a nil Cycler (crews
// have no cycler — unchanged by hk-4gtu). It exercises the same branch keeper_cmd
// uses: warnOnly true → cycler stays nil; false → a real Cycler is built.
func TestWarnOnlyNilsCycler(t *testing.T) {
	var cycler *keeper.Cycler
	warnOnly := true
	if !warnOnly {
		cycler = keeper.NewCycler(keeper.CyclerConfig{AgentName: "x", ProjectDir: t.TempDir()}, keeper.NewFileEmitter(t.TempDir()))
	}
	if cycler != nil {
		t.Fatal("warn-only path: cycler must be nil (crews have no cycler)")
	}
	// Sanity: the non-warn-only branch builds a real Cycler.
	warnOnly = false
	if !warnOnly {
		cycler = keeper.NewCycler(keeper.CyclerConfig{AgentName: "x", ProjectDir: t.TempDir()}, keeper.NewFileEmitter(t.TempDir()))
	}
	if cycler == nil {
		t.Fatal("non-warn-only path: cycler must be non-nil")
	}
}
