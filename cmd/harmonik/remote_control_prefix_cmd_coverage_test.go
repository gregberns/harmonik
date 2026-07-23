package main

// remote_control_prefix_cmd_coverage_test.go — behavior tests for
// `harmonik remote-control-prefix`. The command reads
// daemon.remote_control_prefix from .harmonik/config.yaml and prints it (empty
// line when absent), exit 0; a config-load error exits 1. Side-effect-free — no
// daemon involved.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureRCPrefixIO redirects stdout+stderr around a runRemoteControlPrefixSubcommand call.
func captureRCPrefixIO(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	code = runRemoteControlPrefixSubcommand(args)
	if err := wOut.Close(); err != nil {
		t.Fatalf("close stdout: %v", err)
	}
	if err := wErr.Close(); err != nil {
		t.Fatalf("close stderr: %v", err)
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	outB, _ := io.ReadAll(rOut)
	errB, _ := io.ReadAll(rErr)
	_ = rOut.Close()
	_ = rErr.Close()
	return string(outB), string(errB), code
}

// writeRCConfig writes a .harmonik/config.yaml with the given daemon block body
// and returns the project dir.
func writeRCConfig(t *testing.T, body string) string {
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

func TestRemoteControlPrefix_Help(t *testing.T) {
	stdout, _, code := captureRCPrefixIO(t, []string{"--help"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "remote-control-prefix") {
		t.Errorf("help output missing header; got:\n%s", stdout)
	}
}

func TestRemoteControlPrefix_PrintsConfiguredPrefix(t *testing.T) {
	dir := writeRCConfig(t, "schema_version: 1\ndaemon:\n  target_branch: main\n  remote_control_prefix: hk\n")
	stdout, stderr, code := captureRCPrefixIO(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}
	if strings.TrimRight(stdout, "\n") != "hk" {
		t.Errorf("stdout = %q, want 'hk\\n'", stdout)
	}
}

func TestRemoteControlPrefix_AbsentPrefixPrintsEmptyLine(t *testing.T) {
	// No remote_control_prefix field: prints a bare newline, exit 0.
	dir := writeRCConfig(t, "schema_version: 1\ndaemon:\n  target_branch: main\n")
	stdout, stderr, code := captureRCPrefixIO(t, []string{"--project=" + dir})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}
	if stdout != "\n" {
		t.Errorf("stdout = %q, want a single newline", stdout)
	}
}

func TestRemoteControlPrefix_EmptyProjectFlagIsError(t *testing.T) {
	_, stderr, code := captureRCPrefixIO(t, []string{"--project="})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "requires a directory") {
		t.Errorf("stderr %q: expected 'requires a directory'", stderr)
	}
}

func TestRemoteControlPrefix_MalformedConfigIsError(t *testing.T) {
	// A structurally broken YAML config triggers a load error → exit 1.
	dir := writeRCConfig(t, "schema_version: 1\ndaemon: [not-a: mapping\n  broken\n")
	_, stderr, code := captureRCPrefixIO(t, []string{"--project", dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "config.yaml") {
		t.Errorf("stderr %q: expected a config.yaml load diagnostic", stderr)
	}
}
