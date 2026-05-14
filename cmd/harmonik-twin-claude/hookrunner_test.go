package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Test helpers use the per-bead prefix declared in implementer-protocol.md:
// twinHookFixture (bead hk-e66ht).

// twinHookFixtureEmitter returns a wireEmitter writing to a bytes.Buffer plus
// the buffer itself, for round-trip message inspection.
func twinHookFixtureEmitter(t *testing.T) (*wireEmitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return newWireEmitter(&buf), &buf
}

// twinHookFixtureDecode decodes the nth NDJSON line (0-indexed) from buf into
// a map[string]any. Calls t.Fatalf if line is missing or malformed.
func twinHookFixtureDecode(t *testing.T, buf *bytes.Buffer, n int) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if n >= len(lines) {
		t.Fatalf("twinHookFixtureDecode: want line %d, only %d lines in buffer", n, len(lines))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[n]), &m); err != nil {
		t.Fatalf("twinHookFixtureDecode: line %d unmarshal: %v — raw: %q", n, err, lines[n])
	}
	return m
}

// trueCmd returns the path to the "true" binary (which returns exit 0).
// On macOS the binary is at /usr/bin/true; on Linux it is at /bin/true or
// /usr/bin/true. exec.LookPath is used to find the canonical location.
func trueCmd(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("'true' binary not found in PATH: %v", err)
	}
	return p
}

// falseCmd returns the path to the "false" binary (which returns exit 1).
func falseCmd(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("'false' binary not found in PATH: %v", err)
	}
	return p
}

// TestCallStopHook_TrueCommand verifies that callStopHook with the system
// "true" binary returns exit code 0 and a non-negative duration.
func TestCallStopHook_TrueCommand(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	code, dur := callStopHook(ctx, trueCmd(t), dir)
	if code != 0 {
		t.Errorf("callStopHook true: exit code = %d, want 0", code)
	}
	if dur < 0 {
		t.Errorf("callStopHook true: duration = %d, want >= 0", dur)
	}
}

// TestCallStopHook_FalseCommand verifies that callStopHook with the system
// "false" binary returns a non-zero exit code.
func TestCallStopHook_FalseCommand(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	code, _ := callStopHook(ctx, falseCmd(t), dir)
	if code == 0 {
		t.Error("callStopHook false: exit code = 0, want non-zero")
	}
}

// TestCallStopHook_NonexistentCommand verifies that callStopHook with a
// nonexistent binary returns exit code -1 (launch failure).
func TestCallStopHook_NonexistentCommand(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	code, _ := callStopHook(ctx, "/nonexistent-binary-harmonik-test", dir)
	if code != -1 {
		t.Errorf("callStopHook nonexistent: exit code = %d, want -1", code)
	}
}

// TestRunCallStopHook_NilSettings verifies that runCallStopHook emits a
// twin_error and returns an error when cfg.settings is nil.
func TestRunCallStopHook_NilSettings(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)
	ctx := context.Background()
	cfg := scriptRunConfig{
		settings:     nil,
		worktreePath: t.TempDir(),
	}

	err := runCallStopHook(ctx, emitter, cfg)
	if err == nil {
		t.Fatal("runCallStopHook nil settings: expected error, got nil")
	}

	// Verify that a twin_error message was emitted.
	m := twinHookFixtureDecode(t, buf, 0)
	if got, _ := m["type"].(string); got != "twin_error" {
		t.Errorf("emitted type = %q, want %q", got, "twin_error")
	}
}

// TestRunCallStopHook_NoStopHook verifies that runCallStopHook emits a
// twin_error when settings are loaded but stopHookPresent is false.
func TestRunCallStopHook_NoStopHook(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)
	ctx := context.Background()
	cfg := scriptRunConfig{
		settings:     &cloneSettings{stopHookPresent: false},
		worktreePath: t.TempDir(),
	}

	err := runCallStopHook(ctx, emitter, cfg)
	if err == nil {
		t.Fatal("runCallStopHook no stop hook: expected error, got nil")
	}

	m := twinHookFixtureDecode(t, buf, 0)
	if got, _ := m["type"].(string); got != "twin_error" {
		t.Errorf("emitted type = %q, want %q", got, "twin_error")
	}
}

// TestRunCallStopHook_TrueCommand verifies that runCallStopHook with the system
// "true" binary emits twin_hook_called with hook_type="Stop" and exit_code=0.
func TestRunCallStopHook_TrueCommand(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)
	ctx := context.Background()
	cfg := scriptRunConfig{
		settings: &cloneSettings{
			stopHookPresent: true,
			stopHookCommand: trueCmd(t),
		},
		worktreePath: t.TempDir(),
	}

	err := runCallStopHook(ctx, emitter, cfg)
	if err != nil {
		t.Fatalf("runCallStopHook /bin/true: unexpected error: %v", err)
	}

	m := twinHookFixtureDecode(t, buf, 0)
	if got, _ := m["type"].(string); got != "twin_hook_called" {
		t.Errorf("emitted type = %q, want %q", got, "twin_hook_called")
	}
	if got, _ := m["hook_type"].(string); got != "Stop" {
		t.Errorf("hook_type = %q, want %q", got, "Stop")
	}
	// exit_code is JSON number → float64 in map[string]any.
	if code, ok := m["exit_code"].(float64); !ok || int(code) != 0 {
		t.Errorf("exit_code = %v (type %T), want 0 float64", m["exit_code"], m["exit_code"])
	}
	if _, ok := m["duration_ms"]; !ok {
		t.Error("duration_ms field missing from twin_hook_called")
	}
}

// TestRunCallStopHook_FalseCommand verifies that runCallStopHook with the system
// "false" binary emits twin_hook_called with non-zero exit_code, and returns nil
// error (non-zero hook exit does NOT fail the twin per bead error policy).
func TestRunCallStopHook_FalseCommand(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)
	ctx := context.Background()
	cfg := scriptRunConfig{
		settings: &cloneSettings{
			stopHookPresent: true,
			stopHookCommand: falseCmd(t),
		},
		worktreePath: t.TempDir(),
	}

	err := runCallStopHook(ctx, emitter, cfg)
	if err != nil {
		t.Fatalf("runCallStopHook /bin/false: expected nil error (non-zero hook exit should not fail twin), got: %v", err)
	}

	m := twinHookFixtureDecode(t, buf, 0)
	if got, _ := m["type"].(string); got != "twin_hook_called" {
		t.Errorf("emitted type = %q, want %q", got, "twin_hook_called")
	}
	if code, ok := m["exit_code"].(float64); !ok || int(code) == 0 {
		t.Errorf("exit_code = %v (type %T), want non-zero float64", m["exit_code"], m["exit_code"])
	}
}

// TestEmitTwinSettingsLoaded_Fields verifies the wire message shape of
// twin_settings_loaded.
func TestEmitTwinSettingsLoaded_Fields(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)

	if err := emitter.emitTwinSettingsLoaded(true, true, "harmonik"); err != nil {
		t.Fatalf("emitTwinSettingsLoaded: %v", err)
	}

	m := twinHookFixtureDecode(t, buf, 0)
	if got, _ := m["type"].(string); got != "twin_settings_loaded" {
		t.Errorf("type = %q, want %q", got, "twin_settings_loaded")
	}
	if got, _ := m["permissions_present"].(bool); !got {
		t.Error("permissions_present = false, want true")
	}
	if got, _ := m["stop_hook_present"].(bool); !got {
		t.Error("stop_hook_present = false, want true")
	}
	if got, _ := m["stop_hook_command"].(string); got != "harmonik" {
		t.Errorf("stop_hook_command = %q, want %q", got, "harmonik")
	}
}

// TestEmitTwinSettingsLoaded_Truncation verifies that stop_hook_command is
// truncated to 200 characters in the emitted wire message.
func TestEmitTwinSettingsLoaded_Truncation(t *testing.T) {
	emitter, buf := twinHookFixtureEmitter(t)

	longCmd := strings.Repeat("x", 300)
	if err := emitter.emitTwinSettingsLoaded(false, true, longCmd); err != nil {
		t.Fatalf("emitTwinSettingsLoaded: %v", err)
	}

	m := twinHookFixtureDecode(t, buf, 0)
	cmd, _ := m["stop_hook_command"].(string)
	if len(cmd) > 200 {
		t.Errorf("stop_hook_command len = %d, want <= 200", len(cmd))
	}
}
