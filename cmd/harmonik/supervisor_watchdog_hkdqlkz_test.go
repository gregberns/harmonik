package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildSupervisorWatchdogSpec verifies that the daemon-side supervisor
// watchdog wiring (hk-dqlkz) produces a correctly-configured spec: the
// cognition-dir pidfile path, the 'harmonik supervise restart --watch-restart'
// revival command, and the project working directory.
func TestBuildSupervisorWatchdogSpec(t *testing.T) {
	proj := t.TempDir()
	bin := "/usr/local/bin/harmonik"

	spec := buildSupervisorWatchdogSpec(proj, bin)

	wantPidfile := filepath.Join(proj, ".harmonik", "cognition", "supervisor.pid")
	if spec.PidfilePath != wantPidfile {
		t.Errorf("PidfilePath: got %q, want %q", spec.PidfilePath, wantPidfile)
	}

	if spec.WorkDir != proj {
		t.Errorf("WorkDir: got %q, want %q", spec.WorkDir, proj)
	}

	if len(spec.ReviveCmd) == 0 {
		t.Fatal("ReviveCmd is empty")
	}
	if spec.ReviveCmd[0] != bin {
		t.Errorf("ReviveCmd[0] (binary): got %q, want %q", spec.ReviveCmd[0], bin)
	}
	joined := strings.Join(spec.ReviveCmd, " ")
	for _, want := range []string{"supervise", "restart", "--watch-restart", proj} {
		if !strings.Contains(joined, want) {
			t.Errorf("ReviveCmd missing %q; full cmd: %v", want, spec.ReviveCmd)
		}
	}
}
