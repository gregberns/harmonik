package main

// usage_cmd_coverage_test.go — behavior tests for `harmonik usage` flag parsing
// and validation. Every error path returns before any daemon/network work; the
// happy paths run the analysis over an empty temp project (which degrades to
// warnings, never a hard error) and assert exit 0 + well-formed output.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureUsageIO redirects both os.Stdout and os.Stderr around fn.
func captureUsageIO(t *testing.T, fn func()) (stdout, stderr string) {
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
	fn()
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
	return string(outB), string(errB)
}

func TestRunUsageSubcommand_FlagErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"since needs value", []string{"--since"}, "--since requires a value"},
		{"until needs value", []string{"--until"}, "--until requires a value"},
		{"format needs value", []string{"--format"}, "--format requires a value"},
		{"project needs value", []string{"--project"}, "--project requires a value"},
		{"unknown flag", []string{"--nope"}, "unknown flag"},
		{"bad format value", []string{"--format", "xml"}, "must be 'summary' or 'json'"},
		{"bad since value", []string{"--since", "nonsense"}, "cannot parse --since"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			_, stderr := captureUsageIO(t, func() { code = runUsageSubcommand(tc.args) })
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(stderr, tc.wantMsg) {
				t.Errorf("stderr %q: expected substring %q", stderr, tc.wantMsg)
			}
		})
	}
}

func TestRunUsageSubcommand_Help(t *testing.T) {
	var code int
	stdout, _ := captureUsageIO(t, func() { code = runUsageSubcommand([]string{"--help"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "harmonik usage") {
		t.Errorf("help output missing header; got:\n%s", stdout)
	}
}

// TestRunUsageSubcommand_JSONEmptyProject runs the analysis over an empty temp
// project via the =-form flags and asserts a valid JSON object on exit 0.
func TestRunUsageSubcommand_JSONEmptyProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	args := []string{"--format=json", "--since=24h", "--project=" + dir}

	var code int
	stdout, stderr := captureUsageIO(t, func() { code = runUsageSubcommand(args) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, stdout)
	}
}

// TestRunUsageSubcommand_SummaryDefault exercises the default summary format
// path (no --format flag) over an empty temp project.
func TestRunUsageSubcommand_SummaryDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	var code int
	_, stderr := captureUsageIO(t, func() {
		code = runUsageSubcommand([]string{"--project", dir, "--until", "2026-07-23T00:00:00Z"})
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}
}
