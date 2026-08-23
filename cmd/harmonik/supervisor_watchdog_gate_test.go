package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/supervise"
)

func writeSubsystemsConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return dir
}

func probeSpec(t *testing.T, alarm chan<- struct{}) supervise.SupervisorWatchdogSpec {
	t.Helper()
	return supervise.SupervisorWatchdogSpec{
		PidfilePath:   filepath.Join(t.TempDir(), "supervisor.pid"),
		CheckInterval: 5 * time.Millisecond,
		ReviveCmd:     nil,
		OnAlarm: func() {
			select {
			case alarm <- struct{}{}:
			default:
			}
		},
	}
}

// A daemon whose config switches supervisor_watchdog off must build no
// watchdog at all. This is the property the assessor depends on: with no
// watchdog there is no pidfile probe, so nothing runs `harmonik supervise
// restart` a minute after the daemon is killed, and the shutdown drain can be
// observed to the end.
func TestSupervisorWatchdog_DisabledSubsystemIsNeverConstructed(t *testing.T) {
	dir := writeSubsystemsConfig(t, "schema_version: 1\nsubsystems:\n  supervisor_watchdog:\n    enabled: false\n")

	cfg, err := projectconfig.LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if cfg.Subsystems.Enabled(projectconfig.SubsystemSupervisorWatchdog) {
		t.Fatalf("config edge did not record the disable for %s", projectconfig.SubsystemSupervisorWatchdog)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	alarm := make(chan struct{}, 1)
	var logOut strings.Builder

	sw := startSupervisorWatchdogIfEnabled(ctx, cfg.Subsystems, probeSpec(t, alarm), &logOut)

	if sw != nil {
		t.Fatalf("watchdog was CONSTRUCTED with subsystem %s disabled; off must mean absent, not inert",
			projectconfig.SubsystemSupervisorWatchdog)
	}

	select {
	case <-alarm:
		t.Fatalf("watchdog loop RAN with subsystem %s disabled", projectconfig.SubsystemSupervisorWatchdog)
	case <-time.After(200 * time.Millisecond):
	}

	if !strings.Contains(logOut.String(), "disabled") {
		t.Errorf("the partition was silent; boot log = %q, want a line saying the watchdog is off", logOut.String())
	}
}

// The default is ON. A config with no subsystems: block must build the
// watchdog AND start its loop, so an existing deployment keeps its only path
// back from a dead supervisor.
func TestSupervisorWatchdog_DefaultConfigConstructsAndRunsIt(t *testing.T) {
	dir := writeSubsystemsConfig(t, "schema_version: 1\ndaemon:\n  max_concurrent: 1\n")

	cfg, err := projectconfig.LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	alarm := make(chan struct{}, 1)

	sw := startSupervisorWatchdogIfEnabled(ctx, cfg.Subsystems, probeSpec(t, alarm), io.Discard)

	if sw == nil {
		t.Fatalf("no watchdog built with no subsystems: block; the default must be ON")
	}
	select {
	case <-alarm:
	case <-time.After(5 * time.Second):
		t.Fatalf("watchdog was built but its loop never ran")
	}
}

// An explicit `enabled: true` reads the same as absent.
func TestSupervisorWatchdog_ExplicitTrueConstructsIt(t *testing.T) {
	dir := writeSubsystemsConfig(t, "schema_version: 1\nsubsystems:\n  supervisor_watchdog:\n    enabled: true\n")

	cfg, err := projectconfig.LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	alarm := make(chan struct{}, 1)
	if sw := startSupervisorWatchdogIfEnabled(ctx, cfg.Subsystems, probeSpec(t, alarm), io.Discard); sw == nil {
		t.Fatalf("no watchdog built with supervisor_watchdog enabled: true")
	}
}

// Adding a fourteenth name must not soften the closed set. A misspelt name is
// still a hard error, so an operator who writes `supervisor_watchdogs:` learns
// it at boot instead of believing a partition took effect.
func TestSupervisorWatchdog_MisspeltNameStillFailsLoud(t *testing.T) {
	dir := writeSubsystemsConfig(t, "schema_version: 1\nsubsystems:\n  supervisor_watchdogs:\n    enabled: false\n")

	_, err := projectconfig.LoadProjectConfig(dir)
	if err == nil {
		t.Fatalf("LoadProjectConfig accepted the unknown subsystem name supervisor_watchdogs")
	}
	if !strings.Contains(err.Error(), "unknown subsystem") {
		t.Fatalf("error does not name the problem: %v", err)
	}
	if !strings.Contains(err.Error(), string(projectconfig.SubsystemSupervisorWatchdog)) {
		t.Errorf("error does not list the real name %q, so the operator cannot see the typo: %v",
			projectconfig.SubsystemSupervisorWatchdog, err)
	}
}
