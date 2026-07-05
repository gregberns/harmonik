package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// resolve_stall_sentinel_config_test.go — tests for stall-sentinel config resolver.
// Mirrors the watch config parity + missing-key test patterns (watch_config_parity_we7_test.go).
// Bead ref: hk-hm09z.

// TestStallSentinelConfigParity verifies that every requiredStallSentinelValue in
// allStallSentinelValues() has its keyPath AND description appearing verbatim in
// stallSentinelConfigExampleYAML(). Enforces the single-source-of-truth invariant
// so the --example output and the error-path descriptions can never drift.
func TestStallSentinelConfigParity(t *testing.T) {
	example := stallSentinelConfigExampleYAML()
	if !strings.Contains(example, "stall_sentinel:") {
		t.Fatalf("stallSentinelConfigExampleYAML() must contain 'stall_sentinel:' block:\n%s", example)
	}

	for _, v := range allStallSentinelValues(daemon.StallSentinelConfig{}) {
		if !strings.Contains(example, v.keyPath) {
			t.Errorf("stall_sentinel --example missing key path %q", v.keyPath)
		}
		if !strings.Contains(example, v.description) {
			t.Errorf("stall_sentinel --example missing description for key %q:\n  want: %q\n  example:\n%s",
				v.keyPath, v.description, example)
		}
	}
}

// TestCheckMissingStallSentinelValues_AllMissingWhenEmpty verifies that all 7
// required keys appear as missing when StallSentinelConfig is the zero value.
func TestCheckMissingStallSentinelValues_AllMissingWhenEmpty(t *testing.T) {
	missing := checkMissingStallSentinelValues(daemon.StallSentinelConfig{})
	if len(missing) != 7 {
		t.Errorf("empty config: want 7 missing keys, got %d: %v", len(missing), keyPaths(missing))
	}

	paths := keyPathSet(missing)
	required := []string{
		"stall_sentinel.escalation.tier1_crew",
		"stall_sentinel.escalation.tier2_captain",
		"stall_sentinel.escalation.tier3_operator",
		"stall_sentinel.detection.run_silence_stall",
		"stall_sentinel.detection.review_finalize_stall",
		"stall_sentinel.detection.run_max_age",
		"stall_sentinel.detection.lane_noprogress_stall",
	}
	for _, k := range required {
		if !paths[k] {
			t.Errorf("empty config: expected key %q to be reported missing", k)
		}
	}
}

// TestCheckMissingStallSentinelValues_NoneWhenFull verifies that a fully populated
// StallSentinelConfig reports no missing keys.
func TestCheckMissingStallSentinelValues_NoneWhenFull(t *testing.T) {
	cfg := fullStallSentinelConfig()
	missing := checkMissingStallSentinelValues(cfg)
	if len(missing) != 0 {
		t.Errorf("fully populated config: want 0 missing keys; got: %v", keyPaths(missing))
	}
}

// TestResolveStallSentinelConfig_FullRoundTrip verifies that a fully populated config
// resolves without error and the output fields match the inputs.
func TestResolveStallSentinelConfig_FullRoundTrip(t *testing.T) {
	cfg := fullStallSentinelConfig()
	resolved, err := ResolveStallSentinelConfig(cfg, "/tmp/proj")
	if err != nil {
		t.Fatalf("expected no error; got: %v", err)
	}
	if resolved.Tier1Crew != cfg.Tier1Crew {
		t.Errorf("Tier1Crew: want %v, got %v", cfg.Tier1Crew, resolved.Tier1Crew)
	}
	if resolved.Tier2Captain != cfg.Tier2Captain {
		t.Errorf("Tier2Captain: want %v, got %v", cfg.Tier2Captain, resolved.Tier2Captain)
	}
	if resolved.Tier3Operator != cfg.Tier3Operator {
		t.Errorf("Tier3Operator: want %v, got %v", cfg.Tier3Operator, resolved.Tier3Operator)
	}
	if resolved.RunSilenceStall != cfg.RunSilenceStall {
		t.Errorf("RunSilenceStall: want %v, got %v", cfg.RunSilenceStall, resolved.RunSilenceStall)
	}
	if resolved.ReviewFinalizeStall != cfg.ReviewFinalizeStall {
		t.Errorf("ReviewFinalizeStall: want %v, got %v", cfg.ReviewFinalizeStall, resolved.ReviewFinalizeStall)
	}
	if resolved.RunMaxAge != cfg.RunMaxAge {
		t.Errorf("RunMaxAge: want %v, got %v", cfg.RunMaxAge, resolved.RunMaxAge)
	}
	if resolved.LaneNoprogressStall != cfg.LaneNoprogressStall {
		t.Errorf("LaneNoprogressStall: want %v, got %v", cfg.LaneNoprogressStall, resolved.LaneNoprogressStall)
	}
}

// TestResolveStallSentinelConfig_MissingError verifies that an empty config returns
// a *StallSentinelConfigMissingError naming all 7 keys and the project dir.
func TestResolveStallSentinelConfig_MissingError(t *testing.T) {
	_, err := ResolveStallSentinelConfig(daemon.StallSentinelConfig{}, "/tmp/myproj")
	if err == nil {
		t.Fatal("empty config: expected an error, got nil")
	}
	merr, ok := err.(*StallSentinelConfigMissingError)
	if !ok {
		t.Fatalf("empty config: expected *StallSentinelConfigMissingError; got %T: %v", err, err)
	}
	if merr.ProjectDir != "/tmp/myproj" {
		t.Errorf("ProjectDir: want '/tmp/myproj', got %q", merr.ProjectDir)
	}
	if len(merr.Missing) != 7 {
		t.Errorf("want 7 missing keys; got %d: %v", len(merr.Missing), keyPaths(merr.Missing))
	}

	msg := merr.Error()
	if !strings.Contains(msg, "refusing to start stall-sentinel") {
		t.Errorf("error must say 'refusing to start stall-sentinel'; got: %s", msg)
	}
	if !strings.Contains(msg, "/tmp/myproj") {
		t.Errorf("error must name the project dir; got: %s", msg)
	}
	if !strings.Contains(msg, "--example") {
		t.Errorf("error must reference '--example'; got: %s", msg)
	}
	// Spot-check key paths appear in the error message.
	for _, k := range []string{
		"stall_sentinel.escalation.tier1_crew",
		"stall_sentinel.detection.run_silence_stall",
		"stall_sentinel.detection.lane_noprogress_stall",
	} {
		if !strings.Contains(msg, k) {
			t.Errorf("error must name key %q; got: %s", k, msg)
		}
	}
}

// TestStallSentinelConfigMissingError_Rendering verifies the "KeyPath — Description"
// rendering contract.
func TestStallSentinelConfigMissingError_Rendering(t *testing.T) {
	err := &StallSentinelConfigMissingError{
		ProjectDir: "/tmp/test-proj",
		Missing: []requiredStallSentinelValue{
			{keyPath: "stall_sentinel.escalation.tier1_crew", description: "Go duration after stall detection before escalating to the crew (Tier 1 / X; fail-loud when unset)", satisfied: false},
		},
	}
	msg := err.Error()
	if !strings.Contains(msg, "stall_sentinel.escalation.tier1_crew — Go duration after stall detection before escalating to the crew") {
		t.Errorf("error must render as 'KeyPath — Description'; got: %s", msg)
	}
	if !strings.Contains(msg, "refusing to start stall-sentinel") {
		t.Errorf("error must say 'refusing to start stall-sentinel'; got: %s", msg)
	}
	if !strings.Contains(msg, "/tmp/test-proj") {
		t.Errorf("error must name the project dir; got: %s", msg)
	}
}

// TestStallSentinelExampleBlock_ParseRoundTrip verifies that the example block, when
// embedded in a schema_version: 1 config, parses via daemon.LoadProjectConfig and
// resolves via ResolveStallSentinelConfig with no missing-value errors.
func TestStallSentinelExampleBlock_ParseRoundTrip(t *testing.T) {
	projectDir := t.TempDir()
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "schema_version: 1\n" + stallSentinelConfigExampleYAML()
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	projCfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig on the --example output FAILED: %v\nconfig:\n%s", err, content)
	}

	_, resolveErr := ResolveStallSentinelConfig(projCfg.StallSentinel, projectDir)
	if resolveErr != nil {
		t.Errorf("resolve example block: %v", resolveErr)
	}
}

// fullStallSentinelConfig returns a StallSentinelConfig with all required fields set.
func fullStallSentinelConfig() daemon.StallSentinelConfig {
	return daemon.StallSentinelConfig{
		Tier1Crew:           10 * time.Minute,
		Tier2Captain:        25 * time.Minute,
		Tier3Operator:       45 * time.Minute,
		RunSilenceStall:     20 * time.Minute,
		ReviewFinalizeStall: 20 * time.Minute,
		RunMaxAge:           4 * time.Hour,
		LaneNoprogressStall: 25 * time.Minute,
	}
}

func keyPaths(vals []requiredStallSentinelValue) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = v.keyPath
	}
	return out
}

func keyPathSet(vals []requiredStallSentinelValue) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v.keyPath] = true
	}
	return m
}
