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

// TestCheckMissingWatchValues_WE7AlwaysEmpty verifies that WE7 target keys never
// produce missing-value errors (they default to "captain", not fail-loud).
func TestCheckMissingWatchValues_WE7AlwaysEmpty(t *testing.T) {
	// With empty config (no watch: block)
	missing := checkMissingWatchValues(daemon.WatchConfig{})
	if len(missing) != 0 {
		t.Errorf("WE7 target keys must never be missing (they default to captain); got: %v", missing)
	}
	// With fully populated config
	cfg := daemon.WatchConfig{StatusTarget: "watch", OpsmonitorTarget: "watch"}
	missing = checkMissingWatchValues(cfg)
	if len(missing) != 0 {
		t.Errorf("fully populated watch config must have no missing keys; got: %v", missing)
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
