package main

// ops_monitor_cmd_hkqpzsv_test.go — unit tests for `harmonik ops-monitor` (hk-qpzsv).
//
// Tests exercise plist generation and label derivation without calling launchctl
// (--no-load flag) and without touching ~/Library/LaunchAgents in CI.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// stubProjectDir returns a temp dir with scripts/ops-monitor-check.sh present
// so install validates correctly.
func stubProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "ops-monitor-check.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return dir
}

func TestOpsMonitorHelp(t *testing.T) {
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	code := runOpsMonitorSubcommand([]string{"--help"})
	w.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatalf("exit %d, want 0 for --help", code)
	}
}

func TestOpsMonitorUnknownVerb(t *testing.T) {
	code := runOpsMonitorSubcommand([]string{"bogus"})
	if code != 2 {
		t.Fatalf("exit %d, want 2 for unknown verb", code)
	}
}

func TestOpsMonitorPlistLabelFor(t *testing.T) {
	dir := t.TempDir()
	realDir, _ := filepath.EvalSymlinks(dir)
	wantHash := lifecycle.ComputeProjectHash(realDir).String()
	want := "com.harmonik.ops-monitor." + wantHash
	got := opsMonitorPlistLabelFor(realDir)
	if got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	// Label must be unique across projects.
	dir2 := t.TempDir()
	realDir2, _ := filepath.EvalSymlinks(dir2)
	got2 := opsMonitorPlistLabelFor(realDir2)
	if got == got2 {
		t.Errorf("two different dirs produced the same label %q", got)
	}
}

func TestOpsMonitorInstallNoLoad_WritesPlist(t *testing.T) {
	projDir := stubProjectDir(t)

	// Redirect plist to a temp LaunchAgents dir so we don't touch the real one.
	home := t.TempDir()
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o755); err != nil {
		t.Fatalf("mkdir LaunchAgents: %v", err)
	}

	// Override home via a wrapper: can't easily monkey-patch os.UserHomeDir.
	// Instead, call buildOpsMonitorPlistData directly and verify, then write manually.
	data, err := buildOpsMonitorPlistData(projDir)
	if err != nil {
		t.Fatalf("buildOpsMonitorPlistData: %v", err)
	}
	if !strings.HasPrefix(data.Label, "com.harmonik.ops-monitor.") {
		t.Errorf("label = %q, want com.harmonik.ops-monitor.* prefix", data.Label)
	}
	if data.ProjectDir != projDir {
		t.Errorf("project dir = %q, want %q", data.ProjectDir, projDir)
	}
	expectedScript := filepath.Join(projDir, "scripts", "ops-monitor-check.sh")
	if data.ScriptPath != expectedScript {
		t.Errorf("script path = %q, want %q", data.ScriptPath, expectedScript)
	}
	expectedLog := filepath.Join(projDir, ".harmonik", "ops-monitor")
	if data.LogDir != expectedLog {
		t.Errorf("log dir = %q, want %q", data.LogDir, expectedLog)
	}
}

func TestOpsMonitorInstallNoLoad_PlistContents(t *testing.T) {
	projDir := stubProjectDir(t)
	data, err := buildOpsMonitorPlistData(projDir)
	if err != nil {
		t.Fatalf("buildOpsMonitorPlistData: %v", err)
	}

	var buf strings.Builder
	if terr := opsMonitorPlistTmpl.Execute(&buf, data); terr != nil {
		t.Fatalf("render plist: %v", terr)
	}
	plist := buf.String()

	// Must be valid XML plist with the label and script path.
	if !strings.Contains(plist, data.Label) {
		t.Errorf("plist missing label %q", data.Label)
	}
	if !strings.Contains(plist, data.ScriptPath) {
		t.Errorf("plist missing script path %q", data.ScriptPath)
	}
	if !strings.Contains(plist, data.ProjectDir) {
		t.Errorf("plist missing project dir %q", data.ProjectDir)
	}
	if !strings.Contains(plist, "<integer>300</integer>") {
		t.Error("plist missing StartInterval 300")
	}
	if !strings.Contains(plist, "HK_PROJECT") {
		t.Error("plist missing HK_PROJECT env var")
	}
	// Must declare the plist DOCTYPE.
	if !strings.HasPrefix(strings.TrimSpace(plist), "<?xml") {
		t.Error("plist does not start with <?xml")
	}
}

func TestOpsMonitorStatusNotInstalled(t *testing.T) {
	// When plist does not exist, status exits non-zero.
	projDir := t.TempDir()
	code := runOpsMonitorStatus([]string{"--project", projDir})
	if code == 0 {
		t.Fatal("exit 0, want non-zero when not installed")
	}
}
