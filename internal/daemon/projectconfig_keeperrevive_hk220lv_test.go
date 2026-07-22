package daemon_test

// projectconfig_keeperrevive_hk220lv_test.go — pins the keeper-revive config
// surface through the REAL parse path (LoadProjectConfig → parseKeeperBlock),
// not through a struct literal.
//
// The single behaviour these tests exist to protect: the sweep is DEFAULT ON.
// Every other keeper key in this config block is "absent = not configured";
// the revive keys deliberately are not, because a self-heal that ships
// disabled-by-default is the exact failure hk-220lv closes — the pre-existing
// post-spawn probe was gated on keeper.timings.flock_acquire_grace, that key is
// absent from production config, and so the detector sat dead while a crew ran
// unmonitored for 43 hours.
//
// Tests cover:
//   - No keeper block at all → not disabled; zero values so the constructor
//     applies the compiled defaults.
//   - A keeper block WITH other keys but NO revive keys → still not disabled.
//   - Explicit `revive_scan_interval: 0s` → Present is true and the value is 0,
//     which is the ONE combination that disables the sweep.
//   - Explicit non-zero values → parsed verbatim, Present true, still enabled.
//   - revive_max_attempts parses under keeper.budgets.
//   - A typo'd revive key is still rejected by the hk-9f3f strict-key check.
//
// Helper prefix: krcfg
//
// Bead ref: hk-220lv.

import (
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// krcfgLoad parses yamlContent as a project .harmonik/config.yaml and returns
// the resulting KeeperConfig.
func krcfgLoad(t *testing.T, yamlContent string) daemon.KeeperConfig {
	t.Helper()
	root := projCfgFixtureDir(t, yamlContent)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	return cfg.Keeper
}

// TestKeeperRevive_NoKeeperBlock_DefaultsOn_hk220lv: the production shape today —
// no revive keys anywhere — must leave the sweep ENABLED.
func TestKeeperRevive_NoKeeperBlock_DefaultsOn_hk220lv(t *testing.T) {
	t.Parallel()

	kc := krcfgLoad(t, "schema_version: 1\n")

	if kc.Present.ReviveScanInterval {
		t.Error("Present.ReviveScanInterval = true with no keeper block; want false")
	}
	if daemon.KeeperReviveDisabledByConfig(kc) {
		t.Error("KeeperReviveDisabledByConfig = true for an ABSENT key; want false " +
			"(regression: the keeper self-heal ships disabled-by-default — the same silent-no-op that " +
			"left flock_acquire_grace dead in production and a crew unmonitored for 43h)")
	}
	if kc.ReviveScanInterval != 0 || kc.ReviveGrace != 0 || kc.ReviveMaxAttempts != 0 {
		t.Errorf("unset revive values = (%s, %s, %d); want all zero so NewKeeperReviveWatcher applies "+
			"the compiled defaults", kc.ReviveScanInterval, kc.ReviveGrace, kc.ReviveMaxAttempts)
	}
}

// TestKeeperRevive_KeeperBlockWithoutReviveKeys_DefaultsOn_hk220lv: an operator
// who configures OTHER keeper keys must not lose the self-heal by omission.
func TestKeeperRevive_KeeperBlockWithoutReviveKeys_DefaultsOn_hk220lv(t *testing.T) {
	t.Parallel()

	kc := krcfgLoad(t, `
schema_version: 1
keeper:
  timings:
    poll_interval: 5s
    boot_grace: 5m
  budgets:
    heartbeat_max_misses: 12
`)

	if daemon.KeeperReviveDisabledByConfig(kc) {
		t.Error("KeeperReviveDisabledByConfig = true for a keeper block that simply omits the revive keys; " +
			"want false (regression: configuring unrelated keeper keys silently switches the self-heal off)")
	}
	if kc.Present.ReviveScanInterval || kc.Present.ReviveGrace || kc.Present.ReviveMaxAttempts {
		t.Error("a revive Present bit is set for keys that were never written")
	}
}

// TestKeeperRevive_ExplicitZeroScanInterval_Disabled_hk220lv: the operator's ONLY
// kill-switch. An explicit "0s" must be distinguishable from an absent key.
func TestKeeperRevive_ExplicitZeroScanInterval_Disabled_hk220lv(t *testing.T) {
	t.Parallel()

	kc := krcfgLoad(t, `
schema_version: 1
keeper:
  timings:
    revive_scan_interval: 0s
`)

	if !kc.Present.ReviveScanInterval {
		t.Error("Present.ReviveScanInterval = false for an explicit `0s`; want true " +
			"(regression: an explicit zero is indistinguishable from an absent key, so the operator's " +
			"only opt-out silently becomes a no-op)")
	}
	if kc.ReviveScanInterval != 0 {
		t.Errorf("ReviveScanInterval = %s; want 0", kc.ReviveScanInterval)
	}
	if !daemon.KeeperReviveDisabledByConfig(kc) {
		t.Error("KeeperReviveDisabledByConfig = false for an explicit `revive_scan_interval: 0s`; want true " +
			"(regression: the operator wrote the documented opt-out and the sweep kept running anyway)")
	}
}

// TestKeeperRevive_ExplicitValues_ParsedAndEnabled_hk220lv: real values travel
// the config path verbatim and leave the sweep on.
func TestKeeperRevive_ExplicitValues_ParsedAndEnabled_hk220lv(t *testing.T) {
	t.Parallel()

	kc := krcfgLoad(t, `
schema_version: 1
keeper:
  timings:
    revive_scan_interval: 2m
    revive_grace: 5m
  budgets:
    revive_max_attempts: 7
`)

	if got, want := kc.ReviveScanInterval, 2*time.Minute; got != want {
		t.Errorf("ReviveScanInterval = %s; want %s", got, want)
	}
	if got, want := kc.ReviveGrace, 5*time.Minute; got != want {
		t.Errorf("ReviveGrace = %s; want %s", got, want)
	}
	if got, want := kc.ReviveMaxAttempts, 7; got != want {
		t.Errorf("ReviveMaxAttempts = %d; want %d (regression: the operator's cap is ignored and the "+
			"compiled default silently applies)", got, want)
	}
	if !kc.Present.ReviveGrace || !kc.Present.ReviveMaxAttempts {
		t.Error("Present bits not set for explicitly-written revive keys")
	}
	if daemon.KeeperReviveDisabledByConfig(kc) {
		t.Error("KeeperReviveDisabledByConfig = true for a non-zero scan interval; want false")
	}
}

// TestKeeperRevive_TypoedKey_Rejected_hk220lv: the new keys join the hk-9f3f
// strict-key regime — a typo is a hard error, not a silently ignored setting.
func TestKeeperRevive_TypoedKey_Rejected_hk220lv(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
keeper:
  timings:
    revive_scan_intervall: 60s
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: want *ErrUnknownConfigKey for a typo'd revive key; got nil " +
			"(regression: a misspelled kill-switch parses clean and the operator believes the sweep is off)")
	}
	var uerr *daemon.ExportedErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
}
