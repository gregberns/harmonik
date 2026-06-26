package supervisecmd

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestApplySuperviseProjectConfig_WritesConfigSnapshot(t *testing.T) {
	cfg := Config{
		RestartBaseMS: 1000,
		RestartCapMS:  60000,
	}
	applySuperviseProjectConfig(&cfg, daemon.SuperviseConfig{
		HeartbeatTTL:        2 * time.Minute,
		StartTimeout:        45 * time.Second,
		CrashLoopWindow:     3 * time.Minute,
		HealthProbeInterval: 20 * time.Second,
		StopTimeout:         12 * time.Second,
		RestartBackoffBase:  1500 * time.Millisecond,
		RestartBackoffCap:   90 * time.Second,
		DaemonWatchdog: daemon.SuperviseDaemonWatchdogConfig{
			CheckInterval: 2 * time.Minute,
			DialTimeout:   5 * time.Second,
			ReviveBackoff: 11 * time.Second,
			ReviveWindow:  20 * time.Minute,
		},
	})

	want := map[string]int{
		"HeartbeatTTLMS":    120000,
		"StartTimeoutMS":    45000,
		"CrashLoopWindowMS": 180000,
		"HealthProbeMS":     20000,
		"StopTimeoutMS":     12000,
		"RestartBaseMS":     1500,
		"RestartCapMS":      90000,
		"DWCheckIntervalMS": 120000,
		"DWDialTimeoutMS":   5000,
		"DWReviveBackoffMS": 11000,
		"DWReviveWindowMS":  1200000,
	}
	got := map[string]int{
		"HeartbeatTTLMS":    cfg.HeartbeatTTLMS,
		"StartTimeoutMS":    cfg.StartTimeoutMS,
		"CrashLoopWindowMS": cfg.CrashLoopWindowMS,
		"HealthProbeMS":     cfg.HealthProbeMS,
		"StopTimeoutMS":     cfg.StopTimeoutMS,
		"RestartBaseMS":     cfg.RestartBaseMS,
		"RestartCapMS":      cfg.RestartCapMS,
		"DWCheckIntervalMS": cfg.DWCheckIntervalMS,
		"DWDialTimeoutMS":   cfg.DWDialTimeoutMS,
		"DWReviveBackoffMS": cfg.DWReviveBackoffMS,
		"DWReviveWindowMS":  cfg.DWReviveWindowMS,
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("%s = %d, want %d", key, got[key], wantValue)
		}
	}
}

func TestDaemonWatchdogSpecFromConfig_UsesSnapshotTimings(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DWCheckIntervalMS: 120000,
		DWDialTimeoutMS:   5000,
		DWReviveBackoffMS: 11000,
		DWReviveWindowMS:  1200000,
	}

	spec := daemonWatchdogSpecFromConfig(cfg, dir, []string{"harmonik", "--project", dir})

	if spec.CheckInterval != 2*time.Minute {
		t.Errorf("CheckInterval = %v, want 2m", spec.CheckInterval)
	}
	if spec.DialTimeout != 5*time.Second {
		t.Errorf("DialTimeout = %v, want 5s", spec.DialTimeout)
	}
	if spec.ReviveBackoff != 11*time.Second {
		t.Errorf("ReviveBackoff = %v, want 11s", spec.ReviveBackoff)
	}
	if spec.ReviveWindow != 20*time.Minute {
		t.Errorf("ReviveWindow = %v, want 20m", spec.ReviveWindow)
	}
}
