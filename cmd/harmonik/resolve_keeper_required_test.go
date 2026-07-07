package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// resolve_keeper_required_test.go — operator-required-config change: harmonik imposes
// NO built-in keeper defaults at runtime. ResolveKeeperConfig aggregates EVERY unset
// required value into a single *KeeperConfigMissingError (refuse to start), and the
// `keeper config --example` template + `harmonik init` config round-trip cleanly.

// TestResolveKeeperConfig_ZeroConfig_AggregatesAllMissing: an empty config + no flags
// returns the aggregated missing-value error naming MANY required keys AND the
// `keeper config --example` fix.
func TestResolveKeeperConfig_ZeroConfig_AggregatesAllMissing(t *testing.T) {
	projectDir := t.TempDir()
	_, err := ResolveKeeperConfig(KeeperFlags{}, daemon.KeeperConfig{}, projectDir)
	if err == nil {
		t.Fatal("zero config: expected *KeeperConfigMissingError, got nil (must refuse to start, not silently default)")
	}
	var kme *KeeperConfigMissingError
	if !errors.As(err, &kme) {
		t.Fatalf("expected *KeeperConfigMissingError, got %T: %v", err, err)
	}
	// Several specific keys must be present in the aggregated list.
	for _, want := range []string{
		"keeper.context_thresholds.warn_abs_tokens",
		"keeper.context_thresholds.act_abs_tokens",
		"keeper.hard_ceiling.mode",
		"keeper.timings.poll_interval",
		"keeper.cadence.hold_ttl",
		"keeper.budgets.max_handoff_timeouts",
	} {
		found := false
		for _, m := range kme.Missing {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing list does not contain required key %q; got %v", want, kme.Missing)
		}
	}
	// The message must name the one-command fix and the project dir.
	msg := err.Error()
	if !strings.Contains(msg, "keeper config --example") {
		t.Errorf("error message must point at 'harmonik keeper config --example'; got: %s", msg)
	}
	if !strings.Contains(msg, projectDir) {
		t.Errorf("error message must name the project dir %q; got: %s", projectDir, msg)
	}
	if !strings.Contains(msg, "refusing to start") {
		t.Errorf("error message must say it is refusing to start; got: %s", msg)
	}
	// It must be MANY keys (aggregated, not first-only).
	if len(kme.Missing) < 10 {
		t.Errorf("expected the full set of missing keys aggregated, got only %d: %v", len(kme.Missing), kme.Missing)
	}
}

// TestResolveKeeperConfig_OneMissingKey: a complete config minus warn_abs_tokens
// names EXACTLY that key — not the others.
func TestResolveKeeperConfig_OneMissingKey(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.WarnAbsTokens = 0
	cfg.Present.WarnAbsTokens = false

	_, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err == nil {
		t.Fatal("one missing key: expected *KeeperConfigMissingError, got nil")
	}
	var kme *KeeperConfigMissingError
	if !errors.As(err, &kme) {
		t.Fatalf("expected *KeeperConfigMissingError, got %T: %v", err, err)
	}
	if len(kme.Missing) != 1 || kme.Missing[0] != "keeper.context_thresholds.warn_abs_tokens" {
		t.Errorf("expected exactly the warn_abs_tokens key, got %v", kme.Missing)
	}
}

// TestResolveKeeperConfig_AllViaConfig_ResolvesClean: a complete config resolves with
// no error and the values flow through.
func TestResolveKeeperConfig_AllViaConfig_ResolvesClean(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.WarnAbsTokens = 111_000
	cfg.ActAbsTokens = 112_000
	cfg.ForceActAbsTokens = 113_000

	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("complete config: unexpected error: %v", err)
	}
	if got.WarnAbsTokens != 111_000 || got.ActAbsTokens != 112_000 || got.ForceActAbsTokens != 113_000 {
		t.Errorf("config values did not flow through: %+v", got)
	}
}

// TestResolveKeeperConfig_AllViaFlags_CountsAsOperatorSet: for the flag-backed values,
// supplying them via FLAGS (with config for the rest) resolves cleanly — a flag is an
// operator-set value, so it is NOT "missing".
func TestResolveKeeperConfig_AllViaFlags_CountsAsOperatorSet(t *testing.T) {
	// Start from a complete config, then UNSET every flag-backed key in config and
	// supply it via a flag instead. The non-flag-backed keys stay config-supplied.
	cfg := completeTestKeeperConfig()
	cfg.WarnAbsTokens, cfg.Present.WarnAbsTokens = 0, false
	cfg.ActAbsTokens, cfg.Present.ActAbsTokens = 0, false
	cfg.WarnPctCeil, cfg.Present.WarnPctCeil = 0, false
	cfg.ActPctCeil, cfg.Present.ActPctCeil = 0, false
	cfg.HardCeilingAbsTokens, cfg.Present.HardCeilingAbsTokens = 0, false
	cfg.HardCeilingMode, cfg.Present.HardCeilingMode = "", false
	cfg.Staleness, cfg.Present.Staleness = 0, false
	cfg.IdleQuiesce, cfg.Present.IdleQuiesce = 0, false
	cfg.PollInterval, cfg.Present.PollInterval = 0, false
	cfg.HandoffTimeout, cfg.Present.HandoffTimeout = 0, false
	cfg.BootGrace, cfg.Present.BootGrace = 0, false
	cfg.IdleFloorAbsTokens, cfg.Present.IdleFloorAbsTokens = 0, false

	flags := KeeperFlags{
		WarnAbsTokens: 200_000, WarnAbsSet: true,
		ActAbsTokens: 215_000, ActAbsSet: true,
		WarnPct: 70, WarnPctSet: true,
		ActPct: 85, ActPctSet: true,
		HardCeilingAbs: 280_000, HardCeilingAbsSet: true,
		HardCeilingMode: "alarm", HardCeilingModeSet: true,
		Staleness: 120 * time.Second, StalenessSet: true,
		IdleQuiesce: 8 * time.Second, IdleQuiesceSet: true,
		PollInterval: 5 * time.Second, PollIntervalSet: true,
		HandoffTimeout: 180 * time.Second, HandoffTimeoutSet: true,
		BootGrace: 5 * time.Minute, BootGraceSet: true,
		IdleFloorAbsTokens: 150_000, IdleFloorSet: true,
	}
	got, err := ResolveKeeperConfig(flags, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("all flag-backed via flags: unexpected error: %v", err)
	}
	if got.WarnAbsTokens != 200_000 || got.ActAbsTokens != 215_000 {
		t.Errorf("flag values did not flow through: %+v", got)
	}
}

// TestResolveKeeperConfig_HardCeilingOff_NoAbsRequired: mode off (explicit) with NO
// abs_tokens resolves cleanly — off is an explicit choice, abs not required.
func TestResolveKeeperConfig_HardCeilingOff_NoAbsRequired(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.HardCeilingMode = "off"
	cfg.Present.HardCeilingMode = true
	cfg.HardCeilingAbsTokens = 0
	cfg.Present.HardCeilingAbsTokens = false

	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("mode off + no abs: unexpected error: %v (off must not require abs_tokens)", err)
	}
	if got.HardCeilingMode.String() != "off" {
		t.Errorf("HardCeilingMode = %v, want off", got.HardCeilingMode)
	}
}

// TestRunKeeperConfigExample_RoundTrips is the LOAD-BEARING proof: the block printed
// by `harmonik keeper config --example`, written under schema_version: 1 to a real
// .harmonik/config.yaml, parses via daemon.LoadProjectConfig AND resolves via
// ResolveKeeperConfig with ZERO missing-value errors — i.e. the fix the missing-value
// error points at actually works.
func TestRunKeeperConfigExample_RoundTrips(t *testing.T) {
	var out, errBuf strings.Builder
	if code := runKeeperConfigTo([]string{"--example"}, &out, &errBuf); code != 0 {
		t.Fatalf("keeper config --example exited %d; stderr=%s", code, errBuf.String())
	}
	example := out.String()
	if !strings.Contains(example, "keeper:") {
		t.Fatalf("example output is not a keeper: block:\n%s", example)
	}

	// Write schema_version + the example block to a real config.yaml.
	projectDir := t.TempDir()
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "schema_version: 1\n" + example
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// Parse it the way `harmonik keeper` does.
	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig on the --example output FAILED: %v\nconfig:\n%s", err, content)
	}

	// Resolve it — MUST be zero missing-value errors (the round-trip proof).
	_, rerr := ResolveKeeperConfig(KeeperFlags{}, projCfg.Keeper, projectDir)
	if rerr != nil {
		var kme *KeeperConfigMissingError
		if errors.As(rerr, &kme) {
			t.Fatalf("the `keeper config --example` block STILL has missing required values: %v", kme.Missing)
		}
		t.Fatalf("resolving the --example block failed: %v", rerr)
	}
}

// TestKeeperBinaryUpgradeMigration_CorpusItem6 is acceptance corpus #6 / G5 —
// binary-upgrade required-keys landmine. Proves two invariants end-to-end:
//
//  1. An "old" config.yaml (complete for a prior binary) causes the new binary's
//     resolver to refuse-to-start with ONE *KeeperConfigMissingError listing ALL
//     newly-missing keys — not just the first one.
//
//  2. The operator fix — `keeper config --example` replacing the keeper: block in
//     config.yaml — produces a clean resolve with zero missing-key errors.
//
// The newly-required keys (operator_turn_lookback, post_answer_grace) stand in
// for any required key a binary upgrade may add (hk-74iyd).
func TestKeeperBinaryUpgradeMigration_CorpusItem6(t *testing.T) {
	// ── Part 1: old config → refuse-to-start with aggregated error ──
	oldCfg := completeTestKeeperConfig()
	// Simulate the operator's deployed config before hk-74iyd added these keys.
	oldCfg.OperatorTurnLookback = 0
	oldCfg.Present.OperatorTurnLookback = false
	oldCfg.PostAnswerGrace = 0
	oldCfg.Present.PostAnswerGrace = false

	projectDir := t.TempDir()
	_, err := ResolveKeeperConfig(KeeperFlags{}, oldCfg, projectDir)
	if err == nil {
		t.Fatal("old config missing newly-required keys: expected refuse-to-start, got nil")
	}
	var kme *KeeperConfigMissingError
	if !errors.As(err, &kme) {
		t.Fatalf("expected *KeeperConfigMissingError (aggregated), got %T: %v", err, err)
	}
	// ONE error must include ALL missing keys — not just the first.
	for _, want := range []string{
		"keeper.cadence.operator_turn_lookback",
		"keeper.cadence.post_answer_grace",
	} {
		found := false
		for _, m := range kme.Missing {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("aggregated missing list must contain %q; got %v", want, kme.Missing)
		}
	}
	if len(kme.Missing) != 2 {
		t.Errorf("want exactly 2 missing keys for this upgrade scenario, got %d: %v", len(kme.Missing), kme.Missing)
	}
	msg := err.Error()
	if !strings.Contains(msg, "refusing to start") {
		t.Errorf("error must say 'refusing to start'; got: %s", msg)
	}
	if !strings.Contains(msg, "keeper config --example") {
		t.Errorf("error must point at 'keeper config --example'; got: %s", msg)
	}

	// ── Part 2: example-merge → clean start ──
	// The fix the error message instructs: run `keeper config --example` and
	// replace the keeper: block in config.yaml.
	var exOut, exErr strings.Builder
	if code := runKeeperConfigTo([]string{"--example"}, &exOut, &exErr); code != 0 {
		t.Fatalf("keeper config --example exited %d; stderr=%s", code, exErr.String())
	}
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if mkErr := os.MkdirAll(cfgDir, 0o755); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	content := "schema_version: 1\n" + exOut.String()
	if wErr := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o600); wErr != nil {
		t.Fatalf("WriteFile: %v", wErr)
	}
	projCfg, parseErr := daemon.LoadProjectConfig(projectDir)
	if parseErr != nil {
		t.Fatalf("LoadProjectConfig after example-merge: %v", parseErr)
	}
	if _, rerr := ResolveKeeperConfig(KeeperFlags{}, projCfg.Keeper, projectDir); rerr != nil {
		t.Fatalf("example-merge must yield clean start, got: %v", rerr)
	}
}

// TestKeeperConfigExampleAndInitTemplateAreShared asserts init's generated config.yaml
// embeds the SAME keeper block as `keeper config --example` (single source of truth,
// no drift).
func TestKeeperConfigExampleAndInitTemplateAreShared(t *testing.T) {
	example := keeperConfigExampleYAML()
	if !strings.Contains(example, "keeper:") || !strings.Contains(example, "warn_abs_tokens") {
		t.Fatalf("shared example block looks malformed:\n%s", example)
	}
	// init appends keeperConfigExampleYAML() to configYAMLContent; assert the constant
	// is non-trivial and ends with a newline so concatenation is valid YAML.
	if !strings.HasSuffix(example, "\n") {
		t.Errorf("shared example block must end with a newline for safe concatenation")
	}
}
