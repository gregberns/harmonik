package daemon_test

// projectconfig_hk9f3f_test.go — unit tests for the operator decision (hk-9f3f)
// that unknown keys under the keeper: block (and every keeper sub-block) are a
// HARD ERROR: LoadProjectConfig returns *ErrUnknownConfigKey naming the offending
// key path, so `harmonik keeper` refuses to start. The prior "silently ignored
// (forward-compat per hk-lhu2)" behaviour is removed for the keeper block.
//
// Covers:
//   - (a) a typo'd key in a keeper sub-block (context_thresholds.warn_abs_token)
//     → *ErrUnknownConfigKey whose KeyPath names the key.
//   - (b) a typo at the TOP of the keeper block (keeper.bogus_block) → error.
//   - (c) a FULL VALID keeper config (every real key) still parses with NO error.
//   - (d) an unknown key under the daemon: block is STILL tolerated (no error) —
//     proving the strict-rejection is scoped to the keeper block ONLY (PL-004b
//     daemon-block tolerance is untouched).
//   - schema_version is exempt: a valid config with schema_version: 1 alongside a
//     keeper block does not flag schema_version as unknown.
//
// Helper prefix: keeper9f3fFixture (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-9f3f.

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func keeper9f3fFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	return projCfgFixtureDir(t, yamlContent)
}

// ─────────────────────────────────────────────────────────────────────────────
// (a) typo'd key inside a keeper sub-block → rejected, KeyPath names the key
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9f3f_UnknownSubBlockKey_Rejected(t *testing.T) {
	t.Parallel()

	root := keeper9f3fFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_token: 250000
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatalf("LoadProjectConfig: want *ErrUnknownConfigKey for a typo'd keeper sub-block key; got nil")
	}
	var uerr *daemon.ExportedErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.context_thresholds.warn_abs_token" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.context_thresholds.warn_abs_token")
	}
	if !strings.Contains(err.Error(), "warn_abs_token") {
		t.Errorf("error message %q should name the offending key", err.Error())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) typo at the TOP of the keeper block → rejected
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9f3f_UnknownTopLevelKeeperKey_Rejected(t *testing.T) {
	t.Parallel()

	root := keeper9f3fFixtureDir(t, `
schema_version: 1
keeper:
  bogus_block: {}
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatalf("LoadProjectConfig: want *ErrUnknownConfigKey for a bogus top-level keeper key; got nil")
	}
	var uerr *daemon.ExportedErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.bogus_block" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.bogus_block")
	}
}

// An unknown key inside ANOTHER sub-block (hard_ceiling) is also named precisely.
func TestKeeper9f3f_UnknownHardCeilingKey_Rejected(t *testing.T) {
	t.Parallel()

	root := keeper9f3fFixtureDir(t, `
schema_version: 1
keeper:
  hard_ceiling:
    moed: restart
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	var uerr *daemon.ExportedErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.hard_ceiling.moed" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.hard_ceiling.moed")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) a FULL VALID keeper config (every real key) still parses with NO error
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9f3f_FullValidConfig_NoError(t *testing.T) {
	t.Parallel()

	// Every key the schema supports, post hk-9kgf. None of these may be flagged
	// as unknown; schema_version is exempt (a sibling top-level key, not a keeper key).
	root := keeper9f3fFixtureDir(t, `
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
		t.Fatalf("LoadProjectConfig: full valid keeper config must parse with no error; got %v", err)
	}
	// Spot-check a value to confirm the strict path did not break normal decoding.
	if cfg.Keeper.WarnAbsTokens != 250000 {
		t.Errorf("WarnAbsTokens = %d; want 250000", cfg.Keeper.WarnAbsTokens)
	}
	if cfg.Keeper.HardCeilingMode != "restart" {
		t.Errorf("HardCeilingMode = %q; want %q", cfg.Keeper.HardCeilingMode, "restart")
	}
}

// schema_version is exempt even with a keeper block present and a single sub-key.
func TestKeeper9f3f_SchemaVersion_NotFlagged(t *testing.T) {
	t.Parallel()

	root := keeper9f3fFixtureDir(t, `
schema_version: 1
keeper:
  context_thresholds:
    warn_abs_tokens: 250000
`)
	if _, err := daemon.ExportedLoadProjectConfig(root); err != nil {
		t.Fatalf("schema_version must not be flagged as an unknown keeper key; got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) SCOPE proof: an unknown key under daemon: is STILL tolerated (no error).
// ─────────────────────────────────────────────────────────────────────────────

func TestKeeper9f3f_UnknownDaemonKey_StillTolerated(t *testing.T) {
	t.Parallel()

	// The daemon: block carries an unknown sibling key. PL-004b mandates this be
	// tolerated. The keeper strict-rejection MUST NOT leak onto the daemon block.
	root := keeper9f3fFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 4
  some_future_daemon_key: hello
keeper:
  context_thresholds:
    warn_abs_tokens: 250000
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("unknown daemon key must be tolerated (PL-004b); got error %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 4 {
		t.Errorf("daemon.max_concurrent = %d; want 4 (daemon block must still parse)", cfg.Daemon.MaxConcurrent)
	}
	if cfg.Keeper.WarnAbsTokens != 250000 {
		t.Errorf("keeper.warn_abs_tokens = %d; want 250000", cfg.Keeper.WarnAbsTokens)
	}
}

// And an unknown daemon key WITHOUT any keeper block is also tolerated.
func TestKeeper9f3f_UnknownDaemonKey_NoKeeperBlock_Tolerated(t *testing.T) {
	t.Parallel()

	root := keeper9f3fFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 2
  bogus_daemon_key: 99
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("unknown daemon key (no keeper block) must be tolerated; got %v", err)
	}
	if cfg.Daemon.MaxConcurrent != 2 {
		t.Errorf("daemon.max_concurrent = %d; want 2", cfg.Daemon.MaxConcurrent)
	}
}
