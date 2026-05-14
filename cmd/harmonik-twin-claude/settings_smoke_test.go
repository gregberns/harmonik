package main_test

// settings_smoke_test.go — smoke test for twin --worktree-path + Stop hook
// execution (hk-e66ht).
//
// Builds the twin binary, runs it against a tmp worktree with a real
// .claude/settings.json that declares a Stop hook script. Asserts:
//   1. twin_settings_loaded appears in the wire stream with stop_hook_present=true.
//   2. twin_hook_called appears with hook_type="Stop" and exit_code=0.
//   3. The hook script's sentinel file was written (proves hook actually ran).
//
// Helper prefix: twinSettingsSmokeFixture (bead hk-e66ht).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// twinSettingsSmokeFixtureBinary builds the harmonik-twin-claude binary and
// returns its path. Reuses the same build pattern as chbE2EFixtureBuildBinary.
func twinSettingsSmokeFixtureBinary(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	binPath := filepath.Join(outDir, "harmonik-twin-claude")

	goTool, lookErr := exec.LookPath("go")
	if lookErr != nil {
		t.Skipf("go toolchain not found in PATH; skipping binary-level smoke test: %v", lookErr)
	}

	// Find the module root by walking up from the test source file.
	// The twin source lives at cmd/harmonik-twin-claude; module root is two dirs up.
	wd, wdErr := os.Getwd()
	if wdErr != nil {
		t.Fatalf("twinSettingsSmokeFixtureBinary: getwd: %v", wdErr)
	}
	pkgPath := filepath.Join(wd)

	cmd := exec.Command(goTool, "build", "-o", binPath, pkgPath) //nolint:gosec // G204: goTool from LookPath
	var buildStderr bytes.Buffer
	cmd.Stderr = &buildStderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("twinSettingsSmokeFixtureBinary: build failed: %v\nstderr: %s", err, buildStderr.String())
	}
	return binPath
}

// twinSettingsSmokeFixtureWorktree creates a tmp directory that acts as the
// worktree. It writes a .claude/settings.json with a Stop hook that writes a
// sentinel file, and a simple hook script. Returns (worktreePath, sentinelPath).
func twinSettingsSmokeFixtureWorktree(t *testing.T) (worktreePath, sentinelPath string) {
	t.Helper()
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("twinSettingsSmokeFixtureWorktree: mkdir .claude: %v", err)
	}

	// Write a hook script that creates the sentinel file.
	sentinelPath = filepath.Join(root, "hook-ran.sentinel")
	hookScript := filepath.Join(root, "hook.sh")
	hookBody := "#!/bin/sh\ntouch " + sentinelPath + "\n"
	if err := os.WriteFile(hookScript, []byte(hookBody), 0o755); err != nil { //nolint:gosec // G306: hook script needs executable bit
		t.Fatalf("twinSettingsSmokeFixtureWorktree: write hook script: %v", err)
	}

	// Write settings.json declaring the hook script as the Stop hook command.
	settings := map[string]any{
		"dangerouslyAllowedPermissions": []string{"Bash(*)", "Read(*)"},
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": hookScript,
							"args":    []string{},
							"timeout": 30,
						},
					},
				},
			},
		},
	}
	settingsBytes, _ := json.MarshalIndent(settings, "", "  ")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, settingsBytes, 0o600); err != nil {
		t.Fatalf("twinSettingsSmokeFixtureWorktree: write settings.json: %v", err)
	}

	return root, sentinelPath
}

// twinSettingsSmokeFixtureScript writes a minimal YAML script that includes
// call_stop_hook, and returns its path.
func twinSettingsSmokeFixtureScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "twin-script.yaml")
	// Minimal script: emit agent_ready, call stop hook, emit outcome, complete.
	const scriptYAML = `heartbeat_mode: scripted
messages:
  - type: handler_capabilities
    payload:
      run_id: "run-e66ht-smoke-001"
      session_id: "sess-e66ht-smoke-001"
      protocol_versions_supported: [1]
  - type: agent_ready
    payload:
      run_id: "run-e66ht-smoke-001"
      session_id: "sess-e66ht-smoke-001"
      capabilities: ["scripted"]
  - type: call_stop_hook
  - type: outcome_emitted
    payload:
      run_id: "run-e66ht-smoke-001"
      session_id: "sess-e66ht-smoke-001"
      node_id: "node-e66ht-smoke-001"
      outcome_status: "WORK_COMPLETE"
  - type: agent_completed
    payload:
      run_id: "run-e66ht-smoke-001"
      session_id: "sess-e66ht-smoke-001"
      ended_at: "2026-05-14T00:00:00Z"
      exit_code: 0
      outcome_ref: "run-e66ht-smoke-001/outcome"
`
	if err := os.WriteFile(scriptPath, []byte(scriptYAML), 0o600); err != nil {
		t.Fatalf("twinSettingsSmokeFixtureScript: write: %v", err)
	}
	return scriptPath
}

// TestTwinSettingsSmoke runs the built twin binary with --worktree-path and a
// YAML script containing call_stop_hook, and asserts the expected wire stream.
func TestTwinSettingsSmoke(t *testing.T) {
	binPath := twinSettingsSmokeFixtureBinary(t)
	worktreePath, sentinelPath := twinSettingsSmokeFixtureWorktree(t)
	scriptPath := twinSettingsSmokeFixtureScript(t)

	// Run the twin in scenario=stdout mode (no --socket-path).
	cmd := exec.Command(binPath, //nolint:gosec // G204: binPath from temp build above
		"--script-path", scriptPath,
		"--worktree-path", worktreePath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		t.Fatalf("twin exited non-zero: %v\nstderr: %s\nstdout: %s", runErr, stderr.String(), stdout.String())
	}

	// Parse the wire stream: collect all message types by order.
	type wireMsg struct {
		Type               string  `json:"type"`
		PermissionsPresent bool    `json:"permissions_present"`
		StopHookPresent    bool    `json:"stop_hook_present"`
		HookType           string  `json:"hook_type"`
		ExitCode           float64 `json:"exit_code"`
	}
	var msgs []wireMsg
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m wireMsg
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("parse wire line %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}

	// Locate twin_settings_loaded.
	var settingsLoaded *wireMsg
	for i := range msgs {
		if msgs[i].Type == "twin_settings_loaded" {
			settingsLoaded = &msgs[i]
			break
		}
	}
	if settingsLoaded == nil {
		t.Fatal("twin_settings_loaded not found in wire stream")
	}
	if !settingsLoaded.PermissionsPresent {
		t.Error("twin_settings_loaded: permissions_present = false, want true")
	}
	if !settingsLoaded.StopHookPresent {
		t.Error("twin_settings_loaded: stop_hook_present = false, want true")
	}

	// Locate twin_hook_called.
	var hookCalled *wireMsg
	for i := range msgs {
		if msgs[i].Type == "twin_hook_called" {
			hookCalled = &msgs[i]
			break
		}
	}
	if hookCalled == nil {
		t.Fatal("twin_hook_called not found in wire stream")
	}
	if hookCalled.HookType != "Stop" {
		t.Errorf("twin_hook_called: hook_type = %q, want %q", hookCalled.HookType, "Stop")
	}
	if int(hookCalled.ExitCode) != 0 {
		t.Errorf("twin_hook_called: exit_code = %d, want 0", int(hookCalled.ExitCode))
	}

	// Assert sentinel file was written by the hook script.
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Errorf("sentinel file %q not found; hook script did not execute", sentinelPath)
	}
}
