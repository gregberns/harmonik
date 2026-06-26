package main

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// watch_config_parity_we7_test.go — WE7 parity test: every key in allWatchValues
// (the single-source carrier) appears with its exact description in
// watchConfigExampleYAML(). Enforces the single-source-of-truth invariant so
// that --example output never drifts from the error-path descriptions.
//
// Bead ref: hk-we7-sender-redirect-clhh8.

// TestWatchConfigParityWE7 verifies that every requiredWatchValue in
// allWatchValues() has its keyPath AND description appearing verbatim in
// watchConfigExampleYAML(). This is the parity invariant: whatever the
// missing-key error would print, the --example must also show.
func TestWatchConfigParityWE7(t *testing.T) {
	example := watchConfigExampleYAML()
	if !strings.Contains(example, "watch:") {
		t.Fatalf("watchConfigExampleYAML() must contain 'watch:' block:\n%s", example)
	}

	for _, v := range allWatchValues(daemon.WatchConfig{}) {
		if !strings.Contains(example, v.keyPath) {
			t.Errorf("watch --example missing key path %q", v.keyPath)
		}
		if !strings.Contains(example, v.description) {
			t.Errorf("watch --example missing description for key %q:\n  want: %q\n  example:\n%s",
				v.keyPath, v.description, example)
		}
	}
}

// TestResolveWatchTargets_DefaultCaptain verifies that unset targets resolve to "captain".
func TestResolveWatchTargets_DefaultCaptain(t *testing.T) {
	status, opsmon := ResolveWatchTargets(daemon.WatchConfig{})
	if status != "captain" {
		t.Errorf("status_target unset: want 'captain', got %q", status)
	}
	if opsmon != "captain" {
		t.Errorf("opsmonitor_target unset: want 'captain', got %q", opsmon)
	}
}

// TestResolveWatchTargets_ConfigOverrides verifies that config values override the default.
func TestResolveWatchTargets_ConfigOverrides(t *testing.T) {
	cfg := daemon.WatchConfig{
		StatusTarget:     "watch",
		OpsmonitorTarget: "watch",
	}
	status, opsmon := ResolveWatchTargets(cfg)
	if status != "watch" {
		t.Errorf("status_target configured 'watch': want 'watch', got %q", status)
	}
	if opsmon != "watch" {
		t.Errorf("opsmonitor_target configured 'watch': want 'watch', got %q", opsmon)
	}
}

// TestCheckMissingWatchValues_WE7TargetKeysNeverMissing verifies that WE7 target keys
// (status_target, opsmonitor_target) are never fail-loud — they default to "captain".
// WE9 behavioral keys (absent_thresh_s, stall_ticks) ARE fail-loud when zero/absent.
// WE6 schedule interval keys (liveness_interval, digest_interval) ARE fail-loud when absent.
func TestCheckMissingWatchValues_WE7TargetKeysNeverMissing(t *testing.T) {
	// With empty config: WE7 target keys must NOT appear in missing; WE9+WE6 keys must.
	missing := checkMissingWatchValues(daemon.WatchConfig{})
	missingPaths := map[string]bool{}
	for _, m := range missing {
		missingPaths[m.keyPath] = true
	}
	if missingPaths["watch.status_target"] {
		t.Error("WE7: watch.status_target must never be missing (defaults to captain)")
	}
	if missingPaths["watch.opsmonitor_target"] {
		t.Error("WE7: watch.opsmonitor_target must never be missing (defaults to captain)")
	}
	if !missingPaths["watch.absent_thresh_s"] {
		t.Error("WE9: watch.absent_thresh_s must be missing when AbsentThreshSec=0 (fail-loud)")
	}
	if !missingPaths["watch.stall_ticks"] {
		t.Error("WE9: watch.stall_ticks must be missing when StallTicks=0 (fail-loud)")
	}
	if !missingPaths["watch.liveness_interval"] {
		t.Error("WE6: watch.liveness_interval must be missing when LivenessInterval='' (fail-loud)")
	}
	if !missingPaths["watch.digest_interval"] {
		t.Error("WE6: watch.digest_interval must be missing when DigestInterval='' (fail-loud)")
	}
	if !missingPaths["watch.staffing_starvation_grace"] {
		t.Error("staffing-starvation backstop: watch.staffing_starvation_grace must be missing when StaffingStarvationGrace=0 (fail-loud)")
	}

	// Fully populated config (all WE7 + WE9 + WE6 keys set) must have no missing entries.
	cfg := daemon.WatchConfig{
		StatusTarget:            "watch",
		OpsmonitorTarget:        "watch",
		AbsentThreshSec:         600,
		StallTicks:              3,
		LivenessInterval:        "1h",
		DigestInterval:          "1h",
		StaffingStarvationGrace: 3,
	}
	missing = checkMissingWatchValues(cfg)
	if len(missing) != 0 {
		t.Errorf("fully populated watch config must have no missing keys; got: %v", missing)
	}
}

// TestCheckMissingWatchValues_WE6IntervalKeysFail is the WE6 RED test (b): a
// missing interval key fails loud naming the key + description + 'see --example'.
func TestCheckMissingWatchValues_WE6IntervalKeysFail(t *testing.T) {
	// Only interval keys absent — all WE7+WE9 keys set.
	cfg := daemon.WatchConfig{
		StatusTarget:     "watch",
		OpsmonitorTarget: "watch",
		AbsentThreshSec:  600,
		StallTicks:       3,
		// LivenessInterval and DigestInterval intentionally left empty.
	}
	missing := checkMissingWatchValues(cfg)
	missingPaths := map[string]string{}
	for _, m := range missing {
		missingPaths[m.keyPath] = m.description
	}

	if _, ok := missingPaths["watch.liveness_interval"]; !ok {
		t.Error("WE6: watch.liveness_interval must appear in missing when LivenessInterval is empty")
	}
	if _, ok := missingPaths["watch.digest_interval"]; !ok {
		t.Error("WE6: watch.digest_interval must appear in missing when DigestInterval is empty")
	}

	// The WatchConfigMissingError must render "KeyPath — Description" and "see --example".
	err := &WatchConfigMissingError{ProjectDir: "/tmp/proj", Missing: missing}
	msg := err.Error()
	if !strings.Contains(msg, "watch.liveness_interval") {
		t.Errorf("error must name watch.liveness_interval; got: %s", msg)
	}
	if !strings.Contains(msg, "watch.digest_interval") {
		t.Errorf("error must name watch.digest_interval; got: %s", msg)
	}
	if !strings.Contains(msg, "--example") {
		t.Errorf("error must reference '--example'; got: %s", msg)
	}
}

// TestWatchConfigMissingError_Rendering verifies the "KeyPath — Description"
// rendering contract for a hypothetical future behavioral key.
func TestWatchConfigMissingError_Rendering(t *testing.T) {
	err := &WatchConfigMissingError{
		ProjectDir: "/tmp/test-proj",
		Missing: []requiredWatchValue{
			{keyPath: "watch.some_key", description: "some behavioral key description", satisfied: false},
		},
	}
	msg := err.Error()
	if !strings.Contains(msg, "watch.some_key — some behavioral key description") {
		t.Errorf("error must render as 'KeyPath — Description'; got: %s", msg)
	}
	if !strings.Contains(msg, "refusing to start watch") {
		t.Errorf("error must say 'refusing to start watch'; got: %s", msg)
	}
	if !strings.Contains(msg, "/tmp/test-proj") {
		t.Errorf("error must name the project dir; got: %s", msg)
	}
}
