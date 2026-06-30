package daemon_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestSuperviseConfig_ParsesTimings(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
supervise:
  heartbeat_ttl: 2m
  start_timeout: 45s
  crash_loop_window: 3m
  health_probe_interval: 20s
  stop_timeout: 12s
  restart_backoff:
    base: 1500ms
    cap: 90s
  daemon_watchdog:
    check_interval: 2m
    dial_timeout: 5s
    revive_backoff: 11s
    revive_window: 20m
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	got := cfg.Supervise
	assertDuration := func(name string, got, want time.Duration) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	assertDuration("HeartbeatTTL", got.HeartbeatTTL, 2*time.Minute)
	assertDuration("StartTimeout", got.StartTimeout, 45*time.Second)
	assertDuration("CrashLoopWindow", got.CrashLoopWindow, 3*time.Minute)
	assertDuration("HealthProbeInterval", got.HealthProbeInterval, 20*time.Second)
	assertDuration("StopTimeout", got.StopTimeout, 12*time.Second)
	assertDuration("RestartBackoffBase", got.RestartBackoffBase, 1500*time.Millisecond)
	assertDuration("RestartBackoffCap", got.RestartBackoffCap, 90*time.Second)
	assertDuration("DaemonWatchdog.CheckInterval", got.DaemonWatchdog.CheckInterval, 2*time.Minute)
	assertDuration("DaemonWatchdog.DialTimeout", got.DaemonWatchdog.DialTimeout, 5*time.Second)
	assertDuration("DaemonWatchdog.ReviveBackoff", got.DaemonWatchdog.ReviveBackoff, 11*time.Second)
	assertDuration("DaemonWatchdog.ReviveWindow", got.DaemonWatchdog.ReviveWindow, 20*time.Minute)
}

func TestSuperviseConfig_AbsentZeroValue(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  workflow_mode: review-loop
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Supervise != (daemon.ExportedSuperviseConfig{}) {
		t.Errorf("absent supervise block: got %+v, want zero value", cfg.Supervise)
	}
}

func TestSuperviseConfig_BadDurationFailsLoud(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
supervise:
  daemon_watchdog:
    check_interval: nope
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: expected malformed duration error, got nil")
	}
	var malformed *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &malformed) {
		t.Fatalf("LoadProjectConfig: error type = %T (%v), want *ErrMalformedConfigYAML", err, err)
	}
	if !strings.Contains(err.Error(), "supervise.daemon_watchdog.check_interval") {
		t.Fatalf("error %q does not name supervise.daemon_watchdog.check_interval", err.Error())
	}
}

func TestDaemonRestartBackoffConfig_ParsesTimings(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  restart_backoff:
    base: 5s
    cap: 2m
    window: 45m
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}

	got := cfg.Daemon.RestartBackoff
	assertDuration := func(name string, got, want time.Duration) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	assertDuration("RestartBackoff.Base", got.Base, 5*time.Second)
	assertDuration("RestartBackoff.Cap", got.Cap, 2*time.Minute)
	assertDuration("RestartBackoff.Window", got.Window, 45*time.Minute)
}

func TestDaemonRestartBackoffConfig_BadDurationFailsLoud(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  restart_backoff:
    window: not-a-duration
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: expected malformed duration error, got nil")
	}
	var malformed *daemon.ExportedErrMalformedConfigYAML
	if !errors.As(err, &malformed) {
		t.Fatalf("LoadProjectConfig: error type = %T (%v), want *ErrMalformedConfigYAML", err, err)
	}
	if !strings.Contains(err.Error(), "daemon.restart_backoff.window") {
		t.Fatalf("error %q does not name daemon.restart_backoff.window", err.Error())
	}
}
