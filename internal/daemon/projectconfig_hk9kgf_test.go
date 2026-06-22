package daemon_test

// projectconfig_hk9kgf_test.go — unit tests for the widened keeper: block
// (hk-9kgf): the new context_thresholds fields, and the hard_ceiling, timings,
// cadence, budgets, self_service sub-blocks plus warn_messages.actionable_warn_text.
//
// Covers:
//   - (a) a sample config exercising EVERY new key parses into KeeperConfig with
//     correct values.
//   - (b) a bare-number duration (handoff_timeout: 120) produces
//     *ErrMalformedConfigYAML naming the key — durations are NEVER silently coerced.
//   - (c) a partial file leaves unset fields zero.
//   - (d) keeperBlockAbsent still returns true for a fully-zero block and false
//     when ANY new field is set (one sub-test per newly-added field, so a field
//     forgotten in keeperBlockAbsent is caught — hk-exg3 invariant).
//   - hard_ceiling.mode enum validation; pct >1 validation.
//
// Helper prefix: keeper9kgfFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-9kgf.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

func keeper9kgfFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	return projCfgFixtureDir(t, yamlContent)
}

// ─────────────────────────────────────────────────────────────────────────────
// (a) full config — every new key parses correctly
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9kgf_FullConfig_AllNewKeys(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 250000
    act_abs_tokens: 290000
    force_act_abs_tokens: 340000
    force_act_abs_offset: 40000
    idle_floor_abs_tokens: 200000
    act_pct_ceil: 0.82
    warn_pct_ceil: 0.65
  hard_ceiling:
    mode: restart
    abs_tokens: 360000
    cooldown: 30m
  timings:
    poll_interval: 60s
    idle_quiesce: 5m
    staleness: 10m
    handoff_timeout: 7m
    clear_settle: 30s
    boot_grace: 2m
    max_boot_grace_total: 10m
  cadence:
    warn_cooldown: 15m
    no_gauge_backoff: 2m
    respawn_grace: 1m
    respawn_cooldown: 5m
    live_recover_grace: 90s
    live_recover_cooldown: 4m
    force_retry_interval: 2m
    idle_restart_cooldown: 11m
    hard_ceiling_cooldown: 31m
    blind_keeper_threshold: 20m
    hold_ttl: 33m
    reap_decisions_cadence: 45s
  budgets:
    heartbeat_max_misses: 3
    max_handoff_timeouts: 2
  self_service:
    enabled: true
    grace_seconds: 30
    instruct_only_when_idle: true
    crews_enabled: true
  warn_messages:
    default_warn_text: "wrap up"
    on_demand_warn_text: "restart now"
    actionable_warn_text: "do this thing"
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper

	// context_thresholds new fields
	checkInt64(t, "ForceActAbsOffset", k.ForceActAbsOffset, 40000)
	checkInt64(t, "IdleFloorAbsTokens", k.IdleFloorAbsTokens, 200000)

	// hard_ceiling
	if k.HardCeilingMode != "restart" {
		t.Errorf("HardCeilingMode: want restart, got %q", k.HardCeilingMode)
	}
	checkInt64(t, "HardCeilingAbsTokens", k.HardCeilingAbsTokens, 360000)
	checkDur(t, "HardCeilingCooldownDur", k.HardCeilingCooldownDur, 30*time.Minute)

	// timings
	checkDur(t, "PollInterval", k.PollInterval, 60*time.Second)
	checkDur(t, "IdleQuiesce", k.IdleQuiesce, 5*time.Minute)
	checkDur(t, "Staleness", k.Staleness, 10*time.Minute)
	checkDur(t, "HandoffTimeout", k.HandoffTimeout, 7*time.Minute)
	checkDur(t, "ClearSettle", k.ClearSettle, 30*time.Second)
	checkDur(t, "BootGrace", k.BootGrace, 2*time.Minute)
	checkDur(t, "MaxBootGraceTotal", k.MaxBootGraceTotal, 10*time.Minute)

	// cadence
	checkDur(t, "WarnCooldown", k.WarnCooldown, 15*time.Minute)
	checkDur(t, "NoGaugeBackoff", k.NoGaugeBackoff, 2*time.Minute)
	checkDur(t, "RespawnGrace", k.RespawnGrace, 1*time.Minute)
	checkDur(t, "RespawnCooldown", k.RespawnCooldown, 5*time.Minute)
	checkDur(t, "LiveRecoverGrace", k.LiveRecoverGrace, 90*time.Second)
	checkDur(t, "LiveRecoverCooldown", k.LiveRecoverCooldown, 4*time.Minute)
	checkDur(t, "ForceRetryInterval", k.ForceRetryInterval, 2*time.Minute)
	checkDur(t, "IdleRestartCooldown", k.IdleRestartCooldown, 11*time.Minute)
	checkDur(t, "CadenceHardCeilingCooldown", k.CadenceHardCeilingCooldown, 31*time.Minute)
	checkDur(t, "BlindKeeperThreshold", k.BlindKeeperThreshold, 20*time.Minute)
	checkDur(t, "HoldTTL", k.HoldTTL, 33*time.Minute) // hk-9waz
	checkDur(t, "ReapDecisionsCadence", k.ReapDecisionsCadence, 45*time.Second) // hk-jrftk

	// budgets
	if k.HeartbeatMaxMisses != 3 {
		t.Errorf("HeartbeatMaxMisses: want 3, got %d", k.HeartbeatMaxMisses)
	}
	if k.MaxHandoffTimeouts != 2 {
		t.Errorf("MaxHandoffTimeouts: want 2, got %d", k.MaxHandoffTimeouts)
	}

	// self_service
	if !k.SelfServiceEnabled {
		t.Errorf("SelfServiceEnabled: want true")
	}
	if k.SelfServiceGraceSeconds != 30 {
		t.Errorf("SelfServiceGraceSeconds: want 30, got %d", k.SelfServiceGraceSeconds)
	}
	if !k.SelfServiceInstructOnlyWhenIdle {
		t.Errorf("SelfServiceInstructOnlyWhenIdle: want true")
	}
	// hk-vs4u: SelfServiceCrewsEnabled is now a *bool (nil = absent → resolved
	// true downstream). Here the key was set explicitly to true.
	if k.SelfServiceCrewsEnabled == nil || !*k.SelfServiceCrewsEnabled {
		t.Errorf("SelfServiceCrewsEnabled: want explicit true (non-nil *bool=true), got %v", k.SelfServiceCrewsEnabled)
	}

	// warn_messages.actionable_warn_text (hk-vs4u: the new key wins over the
	// deprecated on_demand_warn_text alias when both are present).
	if k.ActionableWarnText != "do this thing" {
		t.Errorf("ActionableWarnText: want %q, got %q", "do this thing", k.ActionableWarnText)
	}
}

// boolPtr returns a pointer to b. Used to set the *bool crews_enabled field in
// rawKeeperSelfService literals (hk-vs4u).
func boolPtr(b bool) *bool { return &b }

func checkInt64(t *testing.T, name string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %d, got %d", name, want, got)
	}
}

func checkDur(t *testing.T, name string, got, want time.Duration) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) bare-number duration fails loudly with the key name
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9kgf_BareNumberDuration_FailsLoud(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  timings:
    handoff_timeout: 120
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatalf("bare-number duration: want error, got nil")
	}
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Fatalf("want *ErrMalformedConfigYAML, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "handoff_timeout") {
		t.Errorf("error must name the offending key handoff_timeout; got: %v", err)
	}
}

// A bare-number cadence duration likewise fails, naming its key.
func TestKeeper9kgf_BareNumberCadence_FailsLoud(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  cadence:
    warn_cooldown: 900
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Fatalf("want *ErrMalformedConfigYAML, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "warn_cooldown") {
		t.Errorf("error must name warn_cooldown; got: %v", err)
	}
}

// hard_ceiling.mode with an unknown value is rejected, naming mode.
func TestKeeper9kgf_BadHardCeilingMode_FailsLoud(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  hard_ceiling:
    mode: explode
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Fatalf("want *ErrMalformedConfigYAML, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error must name hard_ceiling.mode; got: %v", err)
	}
}

// pct > 1 is rejected (per-field validation, no cross-field invariants here).
func TestKeeper9kgf_PctOutOfRange_FailsLoud(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    act_pct_ceil: 1.5
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	var mfe *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &mfe) {
		t.Fatalf("want *ErrMalformedConfigYAML, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "act_pct_ceil") {
		t.Errorf("error must name act_pct_ceil; got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) partial file → unset fields stay zero
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9kgf_PartialFile_UnsetFieldsZero(t *testing.T) {
	t.Parallel()

	root := keeper9kgfFixtureDir(t, `
schema_version: 1
keeper:
  hard_ceiling:
    mode: alarm
  self_service:
    enabled: true
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper

	// Set fields:
	if k.HardCeilingMode != "alarm" {
		t.Errorf("HardCeilingMode: want alarm, got %q", k.HardCeilingMode)
	}
	if !k.SelfServiceEnabled {
		t.Errorf("SelfServiceEnabled: want true")
	}

	// Everything else stays zero.
	if k.HardCeilingAbsTokens != 0 || k.HardCeilingCooldownDur != 0 {
		t.Errorf("hard_ceiling unset fields not zero: abs=%d cooldown=%v", k.HardCeilingAbsTokens, k.HardCeilingCooldownDur)
	}
	if k.PollInterval != 0 || k.HandoffTimeout != 0 || k.MaxBootGraceTotal != 0 {
		t.Errorf("timings should be zero: %+v", k)
	}
	if k.WarnCooldown != 0 || k.BlindKeeperThreshold != 0 {
		t.Errorf("cadence should be zero: %+v", k)
	}
	if k.HeartbeatMaxMisses != 0 || k.MaxHandoffTimeouts != 0 {
		t.Errorf("budgets should be zero: %+v", k)
	}
	if k.ForceActAbsOffset != 0 || k.IdleFloorAbsTokens != 0 {
		t.Errorf("new threshold fields should be zero: %+v", k)
	}
	// hk-vs4u: SelfServiceCrewsEnabled is a *bool; absent → nil (resolved true downstream).
	if k.SelfServiceGraceSeconds != 0 || k.SelfServiceInstructOnlyWhenIdle || k.SelfServiceCrewsEnabled != nil {
		t.Errorf("self_service unset fields not zero: %+v", k)
	}
	if k.ActionableWarnText != "" {
		t.Errorf("ActionableWarnText should be empty, got %q", k.ActionableWarnText)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) keeperBlockAbsent: zero → true; any NEW field set → false (per-field)
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9kgf_BlockAbsent_ZeroValue_True(t *testing.T) {
	t.Parallel()
	if !daemon.ExportedKeeperBlockAbsent(daemon.ExportedRawKeeperConfig{}) {
		t.Errorf("keeperBlockAbsent(zero): want true, got false")
	}
}

func TestKeeper9kgf_BlockAbsent_AnyNewFieldSet_False(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  daemon.ExportedRawKeeperConfig
	}{
		// context_thresholds new fields
		{"ForceActAbsOffset", daemon.ExportedRawKeeperConfig{ContextThresholds: daemon.ExportedRawKeeperContextThresholds{ForceActAbsOffset: 40000}}},
		{"IdleFloorAbsTokens", daemon.ExportedRawKeeperConfig{ContextThresholds: daemon.ExportedRawKeeperContextThresholds{IdleFloorAbsTokens: 200000}}},
		// hard_ceiling
		{"HardCeiling.Mode", daemon.ExportedRawKeeperConfig{HardCeiling: daemon.ExportedRawKeeperHardCeiling{Mode: "off"}}},
		{"HardCeiling.AbsTokens", daemon.ExportedRawKeeperConfig{HardCeiling: daemon.ExportedRawKeeperHardCeiling{AbsTokens: 1}}},
		{"HardCeiling.Cooldown", daemon.ExportedRawKeeperConfig{HardCeiling: daemon.ExportedRawKeeperHardCeiling{Cooldown: "5m"}}},
		// timings
		{"Timings.PollInterval", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{PollInterval: "1m"}}},
		{"Timings.IdleQuiesce", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{IdleQuiesce: "1m"}}},
		{"Timings.Staleness", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{Staleness: "1m"}}},
		{"Timings.HandoffTimeout", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{HandoffTimeout: "1m"}}},
		{"Timings.ClearSettle", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{ClearSettle: "1m"}}},
		{"Timings.BootGrace", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{BootGrace: "1m"}}},
		{"Timings.MaxBootGraceTotal", daemon.ExportedRawKeeperConfig{Timings: daemon.ExportedRawKeeperTimings{MaxBootGraceTotal: "1m"}}},
		// cadence
		{"Cadence.WarnCooldown", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{WarnCooldown: "1m"}}},
		{"Cadence.NoGaugeBackoff", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{NoGaugeBackoff: "1m"}}},
		{"Cadence.RespawnGrace", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{RespawnGrace: "1m"}}},
		{"Cadence.RespawnCooldown", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{RespawnCooldown: "1m"}}},
		{"Cadence.LiveRecoverGrace", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{LiveRecoverGrace: "1m"}}},
		{"Cadence.LiveRecoverCooldown", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{LiveRecoverCooldown: "1m"}}},
		{"Cadence.ForceRetryInterval", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{ForceRetryInterval: "1m"}}},
		{"Cadence.IdleRestartCooldown", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{IdleRestartCooldown: "1m"}}},
		{"Cadence.HardCeilingCooldown", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{HardCeilingCooldown: "1m"}}},
		{"Cadence.BlindKeeperThreshold", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{BlindKeeperThreshold: "1m"}}},
		{"Cadence.ReapDecisionsCadence", daemon.ExportedRawKeeperConfig{Cadence: daemon.ExportedRawKeeperCadence{ReapDecisionsCadence: "1m"}}},
		// budgets
		{"Budgets.HeartbeatMaxMisses", daemon.ExportedRawKeeperConfig{Budgets: daemon.ExportedRawKeeperBudgets{HeartbeatMaxMisses: 1}}},
		{"Budgets.MaxHandoffTimeouts", daemon.ExportedRawKeeperConfig{Budgets: daemon.ExportedRawKeeperBudgets{MaxHandoffTimeouts: 1}}},
		// self_service
		{"SelfService.Enabled", daemon.ExportedRawKeeperConfig{SelfService: daemon.ExportedRawKeeperSelfService{Enabled: true}}},
		{"SelfService.GraceSeconds", daemon.ExportedRawKeeperConfig{SelfService: daemon.ExportedRawKeeperSelfService{GraceSeconds: 1}}},
		{"SelfService.InstructOnlyWhenIdle", daemon.ExportedRawKeeperConfig{SelfService: daemon.ExportedRawKeeperSelfService{InstructOnlyWhenIdle: true}}},
		{"SelfService.CrewsEnabled", daemon.ExportedRawKeeperConfig{SelfService: daemon.ExportedRawKeeperSelfService{CrewsEnabled: boolPtr(true)}}},
		// warn_messages new field
		{"WarnMessages.ActionableWarnText", daemon.ExportedRawKeeperConfig{WarnMessages: daemon.ExportedRawKeeperWarnMessages{ActionableWarnText: "x"}}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if daemon.ExportedKeeperBlockAbsent(tc.raw) {
				t.Errorf("keeperBlockAbsent(%s set): want false, got true — field missing from keeperBlockAbsent (hk-exg3 invariant)", tc.name)
			}
		})
	}
}

// TestKeeper9kgf_PresenceTracksSuppliedKeys verifies the KeeperConfigPresence the
// parser populates for the operator-required-config gate: a present key (including a
// duration string that parses to 0, like boot_grace: "0s") is marked present, while an
// absent key is NOT. This is what lets ResolveKeeperConfig distinguish "unset" (→
// refuse to start) from an explicit zero.
func TestKeeper9kgf_PresenceTracksSuppliedKeys(t *testing.T) {
	const yaml = `schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 180000
  timings:
    boot_grace: 0s
`
	dir := keeper9kgfFixtureDir(t, yaml)
	cfg, err := daemon.LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	p := cfg.Keeper.Present
	// warn_abs_tokens present (value > 0).
	if !p.WarnAbsTokens {
		t.Error("Present.WarnAbsTokens = false, want true (key supplied)")
	}
	// boot_grace present even though it parses to 0 (the disabled sentinel "0s").
	if !p.BootGrace {
		t.Error("Present.BootGrace = false, want true (boot_grace: 0s is an explicit value)")
	}
	if cfg.Keeper.BootGrace != 0 {
		t.Errorf("BootGrace = %v, want 0 (parsed from 0s)", cfg.Keeper.BootGrace)
	}
	// An UNsupplied key is NOT present.
	if p.ActAbsTokens {
		t.Error("Present.ActAbsTokens = true, want false (key absent)")
	}
	if p.Staleness {
		t.Error("Present.Staleness = true, want false (key absent)")
	}
}
