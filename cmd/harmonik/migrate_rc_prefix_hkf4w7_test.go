package main

// migrate_rc_prefix_hkf4w7_test.go — unit tests for harmonik migrate-rc-prefix
// (hk-f4w7).
//
// Tests cover:
//   - Already-set prefix: exits 0 immediately, nothing to prompt.
//   - Absent field (old config without remote_control_prefix): prompts with
//     suggested default; user accepts default (empty input).
//   - Absent field: user enters a custom prefix.
//   - Empty field (new-format config with remote_control_prefix: empty): prompts
//     and user provides a value; line is replaced in-place.
//   - No daemon: block: command inserts one.
//   - Missing config.yaml: exits 1 with actionable message.
//   - --help: exits 0 and prints usage.
//   - patchRCPrefixInConfig: targeted unit tests for each insertion path.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeConfigWithDaemon writes a minimal .harmonik/config.yaml with the given
// daemon.remote_control_prefix value. Pass "" to emit an empty prefix field,
// or the sentinel noFieldSentinel to omit the field entirely.
const noFieldSentinel = "\x00NOFIELD"

func makeConfigDir(t *testing.T, rcPrefix string) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}

	var content string
	if rcPrefix == noFieldSentinel {
		content = "schema_version: 1\ndaemon:\n  target_branch: main\n  max_concurrent: 4\n  workflow_mode: review-loop\n"
	} else {
		content = "schema_version: 1\ndaemon:\n  target_branch: main\n  max_concurrent: 4\n  workflow_mode: review-loop\n  remote_control_prefix: " + rcPrefix + "\n"
	}

	cfgPath := filepath.Join(harmonikDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return dir
}

// TestMigrateRCPrefix_AlreadySet verifies that a project whose config already
// has a non-empty remote_control_prefix exits 0 and prints "nothing to do".
func TestMigrateRCPrefix_AlreadySet(t *testing.T) {
	dir := makeConfigDir(t, "hk")

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already set") {
		t.Errorf("stdout %q: expected 'already set'", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

// TestMigrateRCPrefix_AbsentField_UserAcceptsDefault checks that when the
// remote_control_prefix field is absent and the user presses Enter (empty
// input), the default suggestion shown in the prompt is what gets written into
// config.yaml. The exact suggestion depends on the environment (br on PATH →
// beads issue_prefix; otherwise → deriveBeadPrefix), so we capture it from
// stdout rather than hardcoding a value.
func TestMigrateRCPrefix_AbsentField_UserAcceptsDefault(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "my-project")
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "schema_version: 1\ndaemon:\n  target_branch: main\n  max_concurrent: 4\n  workflow_mode: review-loop\n"
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader("\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	// Extract the default shown in "Enter prefix [<default>]: " from stdout.
	out := stdout.String()
	start := strings.Index(out, "Enter prefix [")
	if start < 0 {
		t.Fatalf("stdout %q: expected prompt with 'Enter prefix ['", out)
	}
	rest := out[start+len("Enter prefix ["):]
	end := strings.Index(rest, "]:")
	if end < 0 {
		t.Fatalf("stdout %q: malformed prompt, no closing ']:'", out)
	}
	wantPrefix := rest[:end]

	// config.yaml must contain that exact value.
	updated, _ := os.ReadFile(filepath.Join(dir, ".harmonik", "config.yaml"))
	if !strings.Contains(string(updated), "remote_control_prefix: "+wantPrefix) {
		t.Errorf("config.yaml: expected 'remote_control_prefix: %s'; got:\n%s", wantPrefix, updated)
	}
}

// TestMigrateRCPrefix_AbsentField_UserEntersCustom checks that a custom slug
// typed by the user is written to config.yaml instead of the default.
func TestMigrateRCPrefix_AbsentField_UserEntersCustom(t *testing.T) {
	dir := makeConfigDir(t, noFieldSentinel)

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader("myslug\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}

	updated, _ := os.ReadFile(filepath.Join(dir, ".harmonik", "config.yaml"))
	if !strings.Contains(string(updated), "remote_control_prefix: myslug") {
		t.Errorf("config.yaml: expected 'remote_control_prefix: myslug'; got:\n%s", updated)
	}
}

// TestMigrateRCPrefix_EmptyField_UserEntersPrefix checks the case where the
// config has an explicitly-empty remote_control_prefix: (bare label, generated
// by 'harmonik init' but not yet configured). The field should be replaced.
func TestMigrateRCPrefix_EmptyField_UserEntersPrefix(t *testing.T) {
	// Empty-string prefix: field exists but is empty → migration should prompt.
	dir := makeConfigDir(t, "")

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader("ab\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}

	updated, _ := os.ReadFile(filepath.Join(dir, ".harmonik", "config.yaml"))
	if !strings.Contains(string(updated), "remote_control_prefix: ab") {
		t.Errorf("config.yaml: expected 'remote_control_prefix: ab'; got:\n%s", updated)
	}
	// Ensure the original empty line is gone.
	if strings.Contains(string(updated), "remote_control_prefix: \n") {
		t.Errorf("config.yaml still contains empty prefix line:\n%s", updated)
	}
}

// TestMigrateRCPrefix_NoDaemonBlock verifies that when the config has no
// daemon: block at all, a new one is appended with the chosen prefix.
func TestMigrateRCPrefix_NoDaemonBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Config with no daemon: block (like harmonik's own config.yaml).
	content := "schema_version: 1\nsentinel:\n  mode: observe\n"
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader("nd\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}

	updated, _ := os.ReadFile(filepath.Join(dir, ".harmonik", "config.yaml"))
	if !strings.Contains(string(updated), "remote_control_prefix: nd") {
		t.Errorf("config.yaml: expected 'remote_control_prefix: nd'; got:\n%s", updated)
	}
	// Original content must be preserved.
	if !strings.Contains(string(updated), "mode: observe") {
		t.Errorf("config.yaml: original sentinel content lost:\n%s", updated)
	}
}

// TestMigrateRCPrefix_NoConfigYAML verifies exit 1 with an actionable message
// when .harmonik/config.yaml does not exist.
func TestMigrateRCPrefix_NoConfigYAML(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--project", dir}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "harmonik init") {
		t.Errorf("stderr %q: expected 'harmonik init' guidance", stderr.String())
	}
}

// TestMigrateRCPrefix_Help verifies --help exits 0 and prints usage.
func TestMigrateRCPrefix_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMigrateRCPrefix([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "migrate-rc-prefix") {
		t.Errorf("stdout %q: expected usage text with 'migrate-rc-prefix'", stdout.String())
	}
}

// ── patchRCPrefixInConfig unit tests ────────────────────────────────────────

// TestPatchRCPrefixInConfig_ReplaceExisting verifies that an existing
// remote_control_prefix: line (with any value) is replaced in-place.
func TestPatchRCPrefixInConfig_ReplaceExisting(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\ndaemon:\n  workflow_mode: review-loop\n  remote_control_prefix: old\n")
	if err := patchRCPrefixInConfig(f, "new"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "remote_control_prefix: new") {
		t.Errorf("expected 'remote_control_prefix: new'; got:\n%s", got)
	}
	if strings.Contains(got, "remote_control_prefix: old") {
		t.Errorf("old value should be gone; got:\n%s", got)
	}
	if !strings.Contains(got, "workflow_mode: review-loop") {
		t.Errorf("other fields must be preserved; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_InsertAfterWorkflowMode verifies insertion after
// workflow_mode: when no remote_control_prefix: line exists.
func TestPatchRCPrefixInConfig_InsertAfterWorkflowMode(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\ndaemon:\n  target_branch: main\n  workflow_mode: review-loop\n")
	if err := patchRCPrefixInConfig(f, "xy"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "remote_control_prefix: xy") {
		t.Errorf("expected 'remote_control_prefix: xy'; got:\n%s", got)
	}
	// Insertion must be AFTER workflow_mode: not before.
	wmIdx := strings.Index(got, "workflow_mode:")
	rcIdx := strings.Index(got, "remote_control_prefix:")
	if rcIdx < wmIdx {
		t.Errorf("remote_control_prefix should appear after workflow_mode; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_InsertAfterMaxConcurrent verifies insertion after
// max_concurrent: when workflow_mode: is absent.
func TestPatchRCPrefixInConfig_InsertAfterMaxConcurrent(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\ndaemon:\n  target_branch: main\n  max_concurrent: 4\n")
	if err := patchRCPrefixInConfig(f, "mc"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "remote_control_prefix: mc") {
		t.Errorf("expected 'remote_control_prefix: mc'; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_InsertAfterDaemonLine verifies insertion directly
// after "daemon:" when no known sub-fields are present.
func TestPatchRCPrefixInConfig_InsertAfterDaemonLine(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\ndaemon:\n")
	if err := patchRCPrefixInConfig(f, "dl"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "remote_control_prefix: dl") {
		t.Errorf("expected 'remote_control_prefix: dl'; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_AppendDaemonBlock verifies that when no daemon:
// block exists, a new one is appended with the chosen prefix.
func TestPatchRCPrefixInConfig_AppendDaemonBlock(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\nsentinel:\n  mode: observe\n")
	if err := patchRCPrefixInConfig(f, "nd"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "daemon:") {
		t.Errorf("expected 'daemon:' block; got:\n%s", got)
	}
	if !strings.Contains(got, "remote_control_prefix: nd") {
		t.Errorf("expected 'remote_control_prefix: nd'; got:\n%s", got)
	}
	if !strings.Contains(got, "mode: observe") {
		t.Errorf("original content must be preserved; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_EmptyPrefix verifies that an empty string prefix
// is written as "remote_control_prefix: " (bare label, no trailing garbage).
func TestPatchRCPrefixInConfig_EmptyPrefix(t *testing.T) {
	f := writeTmpConfig(t, "schema_version: 1\ndaemon:\n  workflow_mode: review-loop\n")
	if err := patchRCPrefixInConfig(f, ""); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "remote_control_prefix: ") {
		t.Errorf("expected 'remote_control_prefix: '; got:\n%s", got)
	}
}

// TestPatchRCPrefixInConfig_PreservesComments verifies that existing comment
// lines adjacent to the patched area are not removed.
func TestPatchRCPrefixInConfig_PreservesComments(t *testing.T) {
	f := writeTmpConfig(t, `schema_version: 1
daemon:
  # Branch
  target_branch: main
  # Workers
  max_concurrent: 4
  # Mode
  workflow_mode: review-loop
`)
	if err := patchRCPrefixInConfig(f, "pc"); err != nil {
		t.Fatalf("patchRCPrefixInConfig: %v", err)
	}
	got := readTmpConfig(t, f)
	if !strings.Contains(got, "# Branch") || !strings.Contains(got, "# Mode") {
		t.Errorf("comments stripped; got:\n%s", got)
	}
	if !strings.Contains(got, "remote_control_prefix: pc") {
		t.Errorf("expected 'remote_control_prefix: pc'; got:\n%s", got)
	}
}

// helpers

func writeTmpConfig(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp config: %v", err)
	}
	return f
}

func readTmpConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tmp config: %v", err)
	}
	return string(data)
}
