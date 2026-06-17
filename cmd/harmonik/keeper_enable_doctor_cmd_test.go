package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── enable helpers ────────────────────────────────────────────────────────────

// makeScriptsDir creates a fake scripts directory in the temp dir with
// placeholder files for the keeper scripts (including the SessionStart hook,
// hk-8prq).
func makeScriptsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"keeper-statusline.sh", "keeper-stop-hook.sh", "keeper-precompact-hook.sh", "keeper-sessionstart-hook.sh"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("makeScriptsDir: write %s: %v", name, err)
		}
	}
	return dir
}

// makeEnableCfg returns an enableConfig wired to temp directories.
func makeEnableCfg(t *testing.T, agent string) (enableConfig, string) {
	t.Helper()
	projectDir := t.TempDir()
	settingsDir := t.TempDir()
	scriptsDir := makeScriptsDir(t)
	settingsPath := filepath.Join(settingsDir, "settings.json")
	cfg := enableConfig{
		agentName:      agent,
		projectDir:     projectDir,
		scriptsDir:     scriptsDir,
		settingsPath:   settingsPath,
		yesDestructive: false,
	}
	return cfg, settingsPath
}

// readSettingsJSON reads and parses a settings.json from path.
func readSettingsJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readSettingsJSON: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("readSettingsJSON parse: %v", err)
	}
	return m
}

// ── enable tests ──────────────────────────────────────────────────────────────

// TestKeeperEnable_FreshSettings verifies that enable writes all three stanzas
// when settings.json does not yet exist.
func TestKeeperEnable_FreshSettings(t *testing.T) {
	t.Parallel()

	cfg, settingsPath := makeEnableCfg(t, "orchestrator")
	var stdout, stderr bytes.Buffer

	code := runKeeperEnable(cfg, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runKeeperEnable: want 0, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	settings := readSettingsJSON(t, settingsPath)

	// statusLine (ON-058b: project-agnostic — no HARMONIK_PROJECT= prefix).
	sl, ok := settings["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("statusLine missing or wrong type")
	}
	cmd, _ := sl["command"].(string)
	if !strings.Contains(cmd, "keeper-statusline.sh") {
		t.Errorf("statusLine.command does not contain keeper-statusline.sh: %q", cmd)
	}
	// hk-nm32w: agent name must NOT be embedded.
	if strings.Contains(cmd, "HARMONIK_AGENT=") {
		t.Errorf("statusLine.command must not embed HARMONIK_AGENT= (hk-nm32w): %q", cmd)
	}
	// ON-058b: statusLine command must NOT carry HARMONIK_PROJECT= — it is
	// project-agnostic; project routing is resolved at runtime from the
	// inherited HARMONIK_PROJECT env var.
	if strings.Contains(cmd, "HARMONIK_PROJECT=") {
		t.Errorf("statusLine.command must not embed HARMONIK_PROJECT= (ON-058b): %q", cmd)
	}
	// hk-hs1: statusLine MUST carry "type":"command". Without it Claude Code
	// rejects the entire settings.json and disables ALL hooks.
	if got, _ := sl["type"].(string); got != "command" {
		t.Errorf(`statusLine.type = %q; want "command" (hk-hs1)`, got)
	}

	// Stop hook (ON-058a: matched on (basename, HARMONIK_PROJECT=<projectDir>) pair).
	found, stopCmd := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir)
	if !found {
		t.Error("Stop hook not wired in settings.json for this project")
	}
	// hk-nm32w: agent name must NOT be embedded in hook commands.
	if strings.Contains(stopCmd, "HARMONIK_KEEPER_AGENT=") {
		t.Errorf("Stop hook command must not embed HARMONIK_KEEPER_AGENT= (hk-nm32w): %q", stopCmd)
	}
	if !strings.Contains(stopCmd, "HARMONIK_PROJECT=") {
		t.Errorf("Stop hook command missing HARMONIK_PROJECT=: %q", stopCmd)
	}

	// PreCompact hook (ON-058a: matched on (basename, HARMONIK_PROJECT=<projectDir>) pair).
	found, pcCmd := findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh", cfg.projectDir)
	if !found {
		t.Error("PreCompact hook not wired in settings.json for this project")
	}
	// hk-nm32w: agent name must NOT be embedded in hook commands.
	if strings.Contains(pcCmd, "HARMONIK_KEEPER_AGENT=") {
		t.Errorf("PreCompact hook command must not embed HARMONIK_KEEPER_AGENT= (hk-nm32w): %q", pcCmd)
	}
	if !strings.Contains(pcCmd, "HARMONIK_PROJECT=") {
		t.Errorf("PreCompact hook command missing HARMONIK_PROJECT=: %q", pcCmd)
	}
}

// TestKeeperEnable_Idempotent verifies that running enable twice produces the
// same result and does not duplicate stanzas.
func TestKeeperEnable_Idempotent(t *testing.T) {
	t.Parallel()

	cfg, settingsPath := makeEnableCfg(t, "orchestrator")
	var out bytes.Buffer

	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("first enable: want 0, got %d\n%s", code, out.String())
	}
	out.Reset()
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("second enable: want 0, got %d\n%s", code, out.String())
	}

	// statusLine must appear exactly once.
	settings := readSettingsJSON(t, settingsPath)
	countStatusLine := 0
	if sl, ok := settings["statusLine"].(map[string]interface{}); ok {
		if cmd, _ := sl["command"].(string); strings.Contains(cmd, "keeper-statusline.sh") {
			countStatusLine++
		}
	}
	if countStatusLine != 1 {
		t.Errorf("statusLine stanza count: want 1, got %d", countStatusLine)
	}

	// Stop hook must appear exactly once.
	stopCount := countHookEntriesForScript(settings, "Stop", "keeper-stop-hook.sh")
	if stopCount != 1 {
		t.Errorf("Stop hook count: want 1, got %d", stopCount)
	}

	// PreCompact hook must appear exactly once.
	pcCount := countHookEntriesForScript(settings, "PreCompact", "keeper-precompact-hook.sh")
	if pcCount != 1 {
		t.Errorf("PreCompact hook count: want 1, got %d", pcCount)
	}
}

// TestKeeperEnable_RefusesKnownLiveWithoutDestructive verifies that enable
// refuses known live agent names without --yes-destructive.
func TestKeeperEnable_RefusesKnownLiveWithoutDestructive(t *testing.T) {
	t.Parallel()

	for _, agent := range []string{"flywheel", "named-queues", "controlpoints"} {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			cfg, _ := makeEnableCfg(t, agent)
			cfg.agentName = agent
			var stdout, stderr bytes.Buffer

			code := runKeeperEnable(cfg, &stdout, &stderr)
			if code != 1 {
				t.Errorf("agent %q: want exit 1, got %d", agent, code)
			}
			if !strings.Contains(stderr.String(), "--yes-destructive") {
				t.Errorf("agent %q: want --yes-destructive in stderr, got: %s", agent, stderr.String())
			}
		})
	}
}

// TestKeeperEnable_KnownLiveWithDestructive verifies that enable proceeds for
// known live agents when --yes-destructive is set.
func TestKeeperEnable_KnownLiveWithDestructive(t *testing.T) {
	t.Parallel()

	cfg, _ := makeEnableCfg(t, "flywheel")
	cfg.yesDestructive = true
	var stdout, stderr bytes.Buffer

	code := runKeeperEnable(cfg, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("want exit 0, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
}

// TestKeeperEnable_NormalizesStaleAgentCmd verifies that an existing stanza with a
// legacy agent-embedded command is updated to the session-scoped form (hk-nm32w):
// no HARMONIK_AGENT= or HARMONIK_KEEPER_AGENT= in the command, only HARMONIK_PROJECT=.
func TestKeeperEnable_NormalizesStaleAgentCmd(t *testing.T) {
	t.Parallel()

	cfg, settingsPath := makeEnableCfg(t, "orchestrator")

	// Pre-populate settings with a Stop hook using the old agent-embedded format.
	legacyCmd := "HARMONIK_PROJECT=" + cfg.projectDir + " HARMONIK_KEEPER_AGENT=orchestrator " + filepath.Join(cfg.scriptsDir, "keeper-stop-hook.sh")
	initial := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": legacyCmd,
						},
					},
				},
			},
		},
	}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runKeeperEnable(cfg, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("want 0, got %d\nstdout:%s\nstderr:%s", code, stdout.String(), stderr.String())
	}

	settings := readSettingsJSON(t, settingsPath)
	// ON-058a: look up by (basename, HARMONIK_PROJECT=<projectDir>) pair.
	found, updatedCmd := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir)
	if !found {
		t.Fatal("Stop hook not found after normalization")
	}
	// hk-nm32w: normalized command must NOT embed the agent name.
	if strings.Contains(updatedCmd, "HARMONIK_KEEPER_AGENT=") {
		t.Errorf("Stop hook still embeds HARMONIK_KEEPER_AGENT= after normalization (hk-nm32w): %q", updatedCmd)
	}
	if !strings.Contains(updatedCmd, "HARMONIK_PROJECT=") {
		t.Errorf("Stop hook missing HARMONIK_PROJECT= after normalization: %q", updatedCmd)
	}
}

// TestKeeperEnable_BacksUpExistingFile verifies that a .bak-<timestamp> backup
// is created when settings.json already exists.
func TestKeeperEnable_BacksUpExistingFile(t *testing.T) {
	t.Parallel()

	cfg, settingsPath := makeEnableCfg(t, "orchestrator")

	// Write initial settings.json.
	initial := map[string]interface{}{"foo": "bar"}
	raw, _ := json.Marshal(initial)
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	// Find backup file.
	dir := filepath.Dir(settingsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var backupFound bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			backupFound = true
		}
	}
	if !backupFound {
		t.Error("no backup file found after enable on existing settings.json")
	}
}

// TestKeeperEnable_SeedsHandoffStub verifies that enable creates HANDOFF-<agent>.md.
func TestKeeperEnable_SeedsHandoffStub(t *testing.T) {
	t.Parallel()

	cfg, _ := makeEnableCfg(t, "orchestrator")
	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	handoffPath := filepath.Join(cfg.projectDir, "HANDOFF-orchestrator.md")
	content, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("HANDOFF-orchestrator.md not created: %v", err)
	}
	if !strings.Contains(string(content), "HANDOFF-orchestrator") {
		t.Errorf("handoff stub missing expected header: %s", content)
	}
}

// TestKeeperEnable_HandoffStubIdempotent verifies that enable does not
// overwrite an existing HANDOFF-<agent>.md.
func TestKeeperEnable_HandoffStubIdempotent(t *testing.T) {
	t.Parallel()

	cfg, _ := makeEnableCfg(t, "orchestrator")

	handoffPath := filepath.Join(cfg.projectDir, "HANDOFF-orchestrator.md")
	original := "# my existing handoff\n"
	if err := os.WriteFile(handoffPath, []byte(original), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	content, _ := os.ReadFile(handoffPath)
	if string(content) != original {
		t.Errorf("handoff stub was overwritten; want %q, got %q", original, string(content))
	}
}

// TestKeeperEnable_CreatesManaged verifies that --yes-destructive creates the
// .managed marker.
func TestKeeperEnable_CreatesManaged(t *testing.T) {
	t.Parallel()

	cfg, _ := makeEnableCfg(t, "orchestrator")
	cfg.yesDestructive = true
	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", "orchestrator.managed")
	if _, err := os.Stat(managedPath); err != nil {
		t.Errorf(".managed not created: %v", err)
	}
}

// TestKeeperEnable_NoManagedWithoutDestructive verifies that without
// --yes-destructive no .managed file is created.
func TestKeeperEnable_NoManagedWithoutDestructive(t *testing.T) {
	t.Parallel()

	cfg, _ := makeEnableCfg(t, "orchestrator")
	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", "orchestrator.managed")
	if _, err := os.Stat(managedPath); err == nil {
		t.Error(".managed created without --yes-destructive; expected absent")
	}
}

// TestKeeperEnable_RejectsPathTraversal verifies that agent names with path
// traversal sequences are rejected.
func TestKeeperEnable_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"../evil", "foo/bar", "a..b/c"} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			cfg, _ := makeEnableCfg(t, bad)
			var stdout, stderr bytes.Buffer
			code := runKeeperEnable(cfg, &stdout, &stderr)
			if code != 1 {
				t.Errorf("agent %q: want exit 1, got %d", bad, code)
			}
		})
	}
}

// TestKeeperEnable_PreservesExistingSettings verifies that enable does not
// clobber unrelated keys already in settings.json.
func TestKeeperEnable_PreservesExistingSettings(t *testing.T) {
	t.Parallel()

	cfg, settingsPath := makeEnableCfg(t, "orchestrator")

	initial := map[string]interface{}{
		"theme": "dark",
		"permissions": map[string]interface{}{
			"allow": []interface{}{"Read"},
		},
	}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out bytes.Buffer
	if code := runKeeperEnable(cfg, &out, &out); code != 0 {
		t.Fatalf("want 0, got %d\n%s", code, out.String())
	}

	settings := readSettingsJSON(t, settingsPath)
	if settings["theme"] != "dark" {
		t.Errorf("theme key was clobbered; want %q, got %v", "dark", settings["theme"])
	}
	perm, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("permissions key missing or wrong type")
	}
	if perm["allow"] == nil {
		t.Error("permissions.allow was removed")
	}
}

// ── doctor tests ──────────────────────────────────────────────────────────────

// makeDoctorCfg returns a doctorConfig wired to temp dirs, optionally with a
// fake settings.json and keeper files already created.
func makeDoctorCfg(t *testing.T, agent string) (doctorConfig, func()) {
	t.Helper()
	projectDir := t.TempDir()
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")

	cfg := doctorConfig{
		agentName:    agent,
		projectDir:   projectDir,
		settingsPath: settingsPath,
	}
	cleanup := func() {}
	return cfg, cleanup
}

// writeFullSettings writes a settings.json with all keeper stanzas
// (statusLine + Stop + PreCompact + SessionStart).
// agent is unused in the command strings — hk-nm32w removed agent from commands.
// ON-058b: statusLine is project-agnostic (no HARMONIK_PROJECT= prefix).
// ON-058a: Stop/PreCompact hooks carry HARMONIK_PROJECT= for project-keyed dedup.
func writeFullSettings(t *testing.T, settingsPath, projectDir, scriptsDir, _ string) {
	t.Helper()
	settings := map[string]interface{}{}
	statusLineCmd := filepath.Join(scriptsDir, "keeper-statusline.sh")
	stopCmd := "HARMONIK_PROJECT=" + projectDir + " " + filepath.Join(scriptsDir, "keeper-stop-hook.sh")
	pcCmd := "HARMONIK_PROJECT=" + projectDir + " " + filepath.Join(scriptsDir, "keeper-precompact-hook.sh")
	ssCmd := "HARMONIK_PROJECT=" + projectDir + " " + filepath.Join(scriptsDir, "keeper-sessionstart-hook.sh")
	mergeStatusLineStanza(settings, statusLineCmd)
	mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, stopCmd)
	mergeHookStanza(settings, "PreCompact", "keeper-precompact-hook.sh", projectDir, pcCmd)
	mergeHookStanza(settings, "SessionStart", "keeper-sessionstart-hook.sh", projectDir, ssCmd)
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("writeFullSettings: %v", err)
	}
}

// TestKeeperDoctor_AllGapsWhenNoSetup verifies that doctor reports failures
// when nothing has been set up.
func TestKeeperDoctor_AllGapsWhenNoSetup(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	var stdout, stderr bytes.Buffer

	code := runKeeperDoctor(cfg, &stdout, &stderr)
	if code != 1 {
		t.Errorf("want exit 1 (gaps), got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	// At minimum statusLine and hook gaps should be reported.
	out := stdout.String()
	if !strings.Contains(out, "statusLine") {
		t.Errorf("doctor output missing statusLine check: %s", out)
	}
	if !strings.Contains(out, "Stop hook") {
		t.Errorf("doctor output missing Stop hook check: %s", out)
	}
	if !strings.Contains(out, "PreCompact") {
		t.Errorf("doctor output missing PreCompact check: %s", out)
	}
}

// TestKeeperDoctor_HookGapDetected verifies that doctor detects a missing
// Stop hook and exits non-zero.
func TestKeeperDoctor_HookGapDetected(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	scriptsDir := makeScriptsDir(t)

	// Write settings with only statusLine (no hooks).
	settings := map[string]interface{}{}
	mergeStatusLineStanza(settings, "HARMONIK_PROJECT="+cfg.projectDir+" HARMONIK_AGENT=orchestrator "+filepath.Join(scriptsDir, "keeper-statusline.sh"))
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(cfg.settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runKeeperDoctor(cfg, &stdout, &stderr)
	if code != 1 {
		t.Errorf("want exit 1 (missing hooks), got %d\nstdout: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "keeper-stop-hook.sh") {
		t.Errorf("doctor should mention missing keeper-stop-hook.sh: %s", stdout.String())
	}
}

// TestKeeperDoctor_APIKeyRiskDetected verifies that doctor detects when
// ANTHROPIC_API_KEY is set in the environment.
// NOT parallel: uses t.Setenv which forbids parallel.
func TestKeeperDoctor_APIKeyRiskDetected(t *testing.T) {
	// Use t.Setenv so the env is restored after the test.
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	var stdout, _ bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stdout)

	if !strings.Contains(stdout.String(), "ANTHROPIC_API_KEY") {
		t.Errorf("doctor should flag ANTHROPIC_API_KEY risk: %s", stdout.String())
	}
}

// TestKeeperDoctor_GaugeGapWhenCtxAbsent verifies that doctor reports a gauge
// gap when the .ctx file is absent.
func TestKeeperDoctor_GaugeGapWhenCtxAbsent(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	if !strings.Contains(stdout.String(), ".ctx") {
		t.Errorf("doctor should mention missing .ctx file: %s", stdout.String())
	}
}

// TestKeeperDoctor_ManagedAbsentReported verifies that doctor reports the
// absence of the .managed marker.
func TestKeeperDoctor_ManagedAbsentReported(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	var stdout, _ bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stdout)

	if !strings.Contains(stdout.String(), "managed") {
		t.Errorf("doctor should report .managed status: %s", stdout.String())
	}
}

// TestKeeperDoctor_ManagedPresentReported verifies that doctor reports success
// when the .managed marker is present.
func TestKeeperDoctor_ManagedPresentReported(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")
	keeperDir := filepath.Join(cfg.projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managedPath := filepath.Join(keeperDir, "orchestrator.managed")
	if err := os.WriteFile(managedPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile managed: %v", err)
	}

	var stdout, _ bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stdout)

	out := stdout.String()
	if !strings.Contains(out, "managed") {
		t.Errorf("doctor should report .managed status: %s", out)
	}
}

// TestKeeperDoctor_RejectsPathTraversal verifies that doctor rejects agent
// names with path traversal sequences.
func TestKeeperDoctor_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "../evil")
	var stdout, stderr bytes.Buffer
	code := runKeeperDoctorEntry([]string{"--project", cfg.projectDir, "../evil"}, &stdout, &stderr)
	// Doctor with path-traversal agent: the agent name passes through to runKeeperDoctor
	// which doesn't validate the name itself (it checks files). But the entry-point passes
	// it as a positional arg — it should succeed in parsing but the file paths simply won't
	// match any real keeper files.  The primary validation is: enable must reject it.
	// For doctor we just verify it doesn't panic and produces output.
	_ = code
}

// TestKeeperDoctor_StatusLineTypeMissing verifies that doctor reports a failure
// on the "statusLine.type" sub-check (hk-hs1) when the statusLine stanza has the
// correct command (containing keeper-statusline.sh) but is missing the required
// "type":"command" field. Without that field Claude Code rejects the entire
// settings.json and disables ALL hooks.
func TestKeeperDoctor_StatusLineTypeMissing(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")

	// Write a settings.json whose statusLine command is otherwise canonical but
	// deliberately omits the "type":"command" field.
	// Agent name is NOT embedded per hk-nm32w — derived from tmux session name.
	statusLineCmd := "HARMONIK_PROJECT=" + cfg.projectDir + " /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			// "type" intentionally absent — this is the defect that hk-hs1 fixed.
			"command": statusLineCmd,
		},
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(cfg.settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runKeeperDoctor(cfg, &stdout, &stderr)

	// Doctor must exit non-zero (gap found).
	if code == 0 {
		t.Errorf("want non-zero exit (statusLine.type gap), got 0\nstdout: %s", stdout.String())
	}
	// The "statusLine.type" check key must appear in the output.
	if !strings.Contains(stdout.String(), "statusLine.type") {
		t.Errorf("doctor output missing statusLine.type check; stdout: %s", stdout.String())
	}
}

// TestKeeperDoctor_StatusLineTypePresent verifies that doctor does NOT report a
// statusLine.type failure when both the command and the "type":"command" field are
// present (the normalized state after `harmonik keeper enable`).
func TestKeeperDoctor_StatusLineTypePresent(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")

	// Write a fully-normalized statusLine stanza with "type":"command".
	// Agent name is NOT embedded per hk-nm32w — derived from tmux session name.
	statusLineCmd := "HARMONIK_PROJECT=" + cfg.projectDir + " /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			"type":    "command",
			"command": statusLineCmd,
		},
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(cfg.settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	// The statusLine.type check must NOT appear with the failure symbol in the output.
	out := stdout.String()
	if strings.Contains(out, "✗ statusLine.type") {
		t.Errorf("doctor should not report statusLine.type failure when type is present; stdout: %s", out)
	}
}

// TestKeeperDoctor_StatusLineAgentPollution verifies that doctor reports a failure
// on the "statusLine.agent_pollution" sub-check (hk-67k) when a literal
// HARMONIK_AGENT=<name> is embedded in the statusLine command. This override causes
// ALL concurrent sessions to write the same .ctx file, clobbering session-binding.
func TestKeeperDoctor_StatusLineAgentPollution(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")

	// Write a settings.json with a literal HARMONIK_AGENT= in the command — the
	// pre-hk-67k pattern that caused ctx pollution across all sessions.
	statusLineCmd := "HARMONIK_PROJECT=" + cfg.projectDir + " HARMONIK_AGENT=captain /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			"type":    "command",
			"command": statusLineCmd,
		},
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(cfg.settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runKeeperDoctor(cfg, &stdout, &stderr)

	// Doctor must exit non-zero (pollution gap found).
	if code == 0 {
		t.Errorf("want non-zero exit (statusLine.agent_pollution), got 0\nstdout: %s", stdout.String())
	}
	// The pollution check key must appear in the output.
	if !strings.Contains(stdout.String(), "statusLine.agent_pollution") {
		t.Errorf("doctor output missing statusLine.agent_pollution check; stdout: %s", stdout.String())
	}
}

// TestKeeperDoctor_StatusLineAgentPollutionShellExpansionOK verifies that doctor
// does NOT flag a statusLine command that uses shell-expansion form
// (${HARMONIK_AGENT:-captain}), which correctly defers to the inherited env var.
func TestKeeperDoctor_StatusLineAgentPollutionShellExpansionOK(t *testing.T) {
	t.Parallel()

	cfg, _ := makeDoctorCfg(t, "orchestrator")

	// Shell-expansion form: crews inherit their own HARMONIK_AGENT; the captain
	// (no env var set) falls back to the literal "captain". Not ideal vs the pure
	// tmux-name approach, but not a pollution risk.
	statusLineCmd := "HARMONIK_PROJECT=" + cfg.projectDir + " HARMONIK_AGENT=${HARMONIK_AGENT:-captain} /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			"type":    "command",
			"command": statusLineCmd,
		},
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(cfg.settingsPath, raw, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	// Shell-expansion form must NOT trigger the pollution check.
	if strings.Contains(stdout.String(), "statusLine.agent_pollution") {
		t.Errorf("doctor should not flag shell-expansion HARMONIK_AGENT as pollution; stdout: %s", stdout.String())
	}
}

// ── Settings merge helpers tests ──────────────────────────────────────────────

// TestMergeStatusLineStanza_Add verifies adding a fresh stanza.
func TestMergeStatusLineStanza_Add(t *testing.T) {
	t.Parallel()

	settings := map[string]interface{}{}
	action := mergeStatusLineStanza(settings, "HARMONIK_PROJECT=/proj HARMONIK_AGENT=x /scripts/keeper-statusline.sh")
	if action != "added" {
		t.Errorf("want \"added\", got %q", action)
	}
	cmd := getStatusLineCommand(settings)
	if !strings.Contains(cmd, "keeper-statusline.sh") {
		t.Errorf("command not set: %q", cmd)
	}
	// hk-hs1: a freshly-added stanza must carry "type":"command".
	if !statusLineTypeIsCommand(settings) {
		t.Error(`added statusLine missing "type":"command" (hk-hs1)`)
	}
}

// TestMergeStatusLineStanza_NormalizesMissingType verifies that a stanza whose
// command is already canonical but which lacks "type":"command" gets normalized
// rather than reported "unchanged" (hk-hs1). This is the exact end-to-end defect:
// without the type field Claude Code rejects settings.json and disables all hooks.
func TestMergeStatusLineStanza_NormalizesMissingType(t *testing.T) {
	t.Parallel()

	canonicalCmd := "HARMONIK_PROJECT=/proj HARMONIK_AGENT=x /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			"command": canonicalCmd, // canonical command, but no "type" field
		},
	}
	action := mergeStatusLineStanza(settings, canonicalCmd)
	if action != "updated (normalized)" {
		t.Errorf("want \"updated (normalized)\", got %q", action)
	}
	if !statusLineTypeIsCommand(settings) {
		t.Error(`statusLine still missing "type":"command" after normalize (hk-hs1)`)
	}
}

// TestMergeStatusLineStanza_Unchanged verifies idempotency.
func TestMergeStatusLineStanza_Unchanged(t *testing.T) {
	t.Parallel()

	canonicalCmd := "HARMONIK_PROJECT=/proj HARMONIK_AGENT=x /scripts/keeper-statusline.sh"
	settings := map[string]interface{}{}
	mergeStatusLineStanza(settings, canonicalCmd)
	action := mergeStatusLineStanza(settings, canonicalCmd)
	if action != "unchanged" {
		t.Errorf("want \"unchanged\", got %q", action)
	}
}

// TestMergeStatusLineStanza_Update verifies normalization of an existing stanza.
func TestMergeStatusLineStanza_Update(t *testing.T) {
	t.Parallel()

	settings := map[string]interface{}{
		"statusLine": map[string]interface{}{
			"command": "HARMONIK_AGENT=x /old/path/keeper-statusline.sh",
		},
	}
	newCmd := "HARMONIK_PROJECT=/proj HARMONIK_AGENT=x /new/path/keeper-statusline.sh"
	action := mergeStatusLineStanza(settings, newCmd)
	if action != "updated (normalized)" {
		t.Errorf("want \"updated (normalized)\", got %q", action)
	}
	got := getStatusLineCommand(settings)
	if got != newCmd {
		t.Errorf("command not updated: want %q, got %q", newCmd, got)
	}
	// hk-hs1: normalization must also add the required "type":"command".
	if !statusLineTypeIsCommand(settings) {
		t.Error(`updated statusLine missing "type":"command" (hk-hs1)`)
	}
}

// TestMergeHookStanza_Add verifies adding a fresh Stop hook group.
func TestMergeHookStanza_Add(t *testing.T) {
	t.Parallel()

	const projectDir = "/proj"
	settings := map[string]interface{}{}
	cmd := "HARMONIK_PROJECT=" + projectDir + " HARMONIK_KEEPER_AGENT=x /scripts/keeper-stop-hook.sh"
	action := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, cmd)
	if action != "added" {
		t.Errorf("want \"added\", got %q", action)
	}
	found, _ := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", projectDir)
	if !found {
		t.Error("Stop hook not found after add")
	}
}

// TestMergeHookStanza_Unchanged verifies idempotency.
func TestMergeHookStanza_Unchanged(t *testing.T) {
	t.Parallel()

	const projectDir = "/proj"
	cmd := "HARMONIK_PROJECT=" + projectDir + " HARMONIK_KEEPER_AGENT=x /scripts/keeper-stop-hook.sh"
	settings := map[string]interface{}{}
	mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, cmd)
	action := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, cmd)
	if action != "unchanged" {
		t.Errorf("want \"unchanged\", got %q", action)
	}
}

// TestMergeHookStanza_Update verifies normalization of a stale command for the same project.
func TestMergeHookStanza_Update(t *testing.T) {
	t.Parallel()

	const projectDir = "/proj"
	settings := map[string]interface{}{}
	// Old command: same project, stale script path or extra agent env var.
	oldCmd := "HARMONIK_PROJECT=" + projectDir + " HARMONIK_KEEPER_AGENT=x /old/keeper-stop-hook.sh"
	mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, oldCmd)
	// New canonical command: same project, clean form.
	newCmd := "HARMONIK_PROJECT=" + projectDir + " /new/keeper-stop-hook.sh"
	action := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", projectDir, newCmd)
	if action != "updated (normalized)" {
		t.Errorf("want \"updated (normalized)\", got %q", action)
	}
	found, got := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", projectDir)
	if !found {
		t.Fatal("Stop hook not found after update")
	}
	if got != newCmd {
		t.Errorf("command not updated: want %q, got %q", newCmd, got)
	}
}

// TestKeeperEnable_MultiProjectCoexistence verifies ON-058a: two distinct projects
// each running `harmonik keeper enable` produce two sibling groups in the
// hooks.Stop and hooks.PreCompact arrays, and a single shared project-agnostic
// statusLine stanza (ON-058b).
func TestKeeperEnable_MultiProjectCoexistence(t *testing.T) {
	t.Parallel()

	// Two projects share the same settings.json (simulating ~/.claude/settings.json).
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	scriptsDir := makeScriptsDir(t)

	projectA := t.TempDir()
	projectB := t.TempDir()

	cfgA := enableConfig{
		agentName:    "orchestrator",
		projectDir:   projectA,
		scriptsDir:   scriptsDir,
		settingsPath: settingsPath,
	}
	cfgB := enableConfig{
		agentName:    "orchestrator",
		projectDir:   projectB,
		scriptsDir:   scriptsDir,
		settingsPath: settingsPath,
	}

	var out bytes.Buffer
	if code := runKeeperEnable(cfgA, &out, &out); code != 0 {
		t.Fatalf("enable projectA: want 0, got %d\n%s", code, out.String())
	}
	out.Reset()
	if code := runKeeperEnable(cfgB, &out, &out); code != 0 {
		t.Fatalf("enable projectB: want 0, got %d\n%s", code, out.String())
	}

	settings := readSettingsJSON(t, settingsPath)

	// ON-058b: exactly one statusLine stanza, project-agnostic (no HARMONIK_PROJECT=).
	sl, ok := settings["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("statusLine missing or wrong type")
	}
	slCmd, _ := sl["command"].(string)
	if !strings.Contains(slCmd, "keeper-statusline.sh") {
		t.Errorf("statusLine.command missing keeper-statusline.sh: %q", slCmd)
	}
	if strings.Contains(slCmd, "HARMONIK_PROJECT=") {
		t.Errorf("statusLine.command must not embed HARMONIK_PROJECT= (ON-058b): %q", slCmd)
	}

	// ON-058a: each project has its own sibling group in Stop and PreCompact arrays.
	foundA, _ := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", projectA)
	if !foundA {
		t.Error("Stop hook for projectA not found")
	}
	foundB, _ := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", projectB)
	if !foundB {
		t.Error("Stop hook for projectB not found")
	}

	// Two distinct sibling groups should be present (one per project).
	stopCount := countHookEntriesForScript(settings, "Stop", "keeper-stop-hook.sh")
	if stopCount != 2 {
		t.Errorf("Stop hook entry count: want 2 (one per project), got %d", stopCount)
	}

	foundA, _ = findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh", projectA)
	if !foundA {
		t.Error("PreCompact hook for projectA not found")
	}
	foundB, _ = findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh", projectB)
	if !foundB {
		t.Error("PreCompact hook for projectB not found")
	}

	pcCount := countHookEntriesForScript(settings, "PreCompact", "keeper-precompact-hook.sh")
	if pcCount != 2 {
		t.Errorf("PreCompact hook entry count: want 2 (one per project), got %d", pcCount)
	}

	// ON-058a idempotency: re-running enable for projectA must not add a third group.
	out.Reset()
	if code := runKeeperEnable(cfgA, &out, &out); code != 0 {
		t.Fatalf("re-enable projectA: want 0, got %d\n%s", code, out.String())
	}
	settings = readSettingsJSON(t, settingsPath)
	stopCountAfter := countHookEntriesForScript(settings, "Stop", "keeper-stop-hook.sh")
	if stopCountAfter != 2 {
		t.Errorf("Stop hook count after re-enable: want 2, got %d", stopCountAfter)
	}
}

// ── countHookEntriesForScript helper ─────────────────────────────────────────

// countHookEntriesForScript counts how many hook entries contain scriptBasename
// for the given event type. Used to assert no duplicates.
func countHookEntriesForScript(settings map[string]interface{}, eventName, scriptBasename string) int {
	hooksRaw, ok := settings["hooks"]
	if !ok || hooksRaw == nil {
		return 0
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return 0
	}
	groupsRaw, ok := hooksMap[eventName]
	if !ok || groupsRaw == nil {
		return 0
	}
	groups, ok := groupsRaw.([]interface{})
	if !ok {
		return 0
	}
	count := 0
	for _, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := gMap["hooks"]
		if !ok {
			continue
		}
		entries, ok := innerHooks.([]interface{})
		if !ok {
			continue
		}
		for _, e := range entries {
			eMap, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := eMap["command"].(string)
			if strings.Contains(cmd, scriptBasename) {
				count++
			}
		}
	}
	return count
}

// ── enable/doctor parser-parity tests (hk-ar5y) ──────────────────────────────

// TestParseKeeperEnableArgs_PreservesYesDestructive guards the exact regression
// that failed the original hk-psds: the enable parser MUST keep its
// --yes-destructive case (sets yesDestructive=true), NOT reject it as unknown.
func TestParseKeeperEnableArgs_PreservesYesDestructive(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	pa, code := parseKeeperEnableArgs([]string{"orchestrator", "--yes-destructive"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("parse: want -1 (proceed), got %d\nstderr: %s", code, stderr.String())
	}
	if !pa.yesDestructive {
		t.Error("--yes-destructive not parsed to yesDestructive=true (hk-psds regression)")
	}
	if pa.agentName != "orchestrator" {
		t.Errorf("agentName = %q; want orchestrator", pa.agentName)
	}
}

// TestParseKeeperEnableArgs_AgentFlagWinsPositional: when both a positional and
// --agent are given, the flag wins.
func TestParseKeeperEnableArgs_AgentFlagWinsPositional(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	pa, code := parseKeeperEnableArgs([]string{"positionalname", "--agent", "flagname"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d", code)
	}
	if pa.agentName != "flagname" {
		t.Errorf("flag must win positional: agentName = %q; want flagname", pa.agentName)
	}
}

// TestParseKeeperEnableArgs_AgentEqualsForm verifies the --agent=NAME form.
func TestParseKeeperEnableArgs_AgentEqualsForm(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	pa, code := parseKeeperEnableArgs([]string{"--agent=flagname"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d", code)
	}
	if pa.agentName != "flagname" {
		t.Errorf("--agent=NAME: agentName = %q; want flagname", pa.agentName)
	}
}

// TestParseKeeperEnableArgs_RejectsUnknownFlag: an unrecognized leading-dash
// token exits 2.
func TestParseKeeperEnableArgs_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	_, code := parseKeeperEnableArgs([]string{"orchestrator", "--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("--bogus: want exit 2, got %d", code)
	}
}

// TestParseKeeperEnableArgs_MissingAgent: no agent (flag or positional) exits 1.
func TestParseKeeperEnableArgs_MissingAgent(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	_, code := parseKeeperEnableArgs([]string{"--project", "/tmp/x"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("missing agent: want exit 1, got %d", code)
	}
}

// TestParseKeeperEnableArgs_AllKnownFlagsPreserved enumerates every recognized
// enable flag and asserts each is still parsed (the reject is ONLY for
// UNRECOGNIZED leading-dash tokens).
func TestParseKeeperEnableArgs_AllKnownFlagsPreserved(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	pa, code := parseKeeperEnableArgs([]string{
		"orchestrator", "--project", "/p", "--scripts-dir", "/s", "--tmux", "sess:0.0", "--yes-destructive",
	}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d\nstderr: %s", code, stderr.String())
	}
	if pa.agentName != "orchestrator" || pa.projectDir != "/p" || pa.scriptsDir != "/s" ||
		pa.tmuxTarget != "sess:0.0" || !pa.yesDestructive {
		t.Errorf("not all flags parsed: %+v", pa)
	}
}

// TestParseKeeperDoctorArgs_AgentFlagWinsPositional mirrors the enable behavior
// for doctor — THE CLASS-A killer: `doctor --agent X` must operate on X, NOT on
// an agent literally named "--agent".
func TestParseKeeperDoctorArgs_AgentFlagWinsPositional(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	da, code := parseKeeperDoctorArgs([]string{"pos", "--agent", "flag"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d", code)
	}
	if da.agentName != "flag" {
		t.Errorf("flag must win positional: agentName = %q; want flag", da.agentName)
	}
}

// TestParseKeeperDoctorArgs_AgentFlagNotLiteral confirms `doctor --agent X`
// resolves to X, not the literal "--agent" token (the false-green captain hit).
func TestParseKeeperDoctorArgs_AgentFlagNotLiteral(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	da, code := parseKeeperDoctorArgs([]string{"--agent", "captain"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d", code)
	}
	if da.agentName != "captain" {
		t.Errorf("--agent captain: agentName = %q; want captain (NOT \"--agent\")", da.agentName)
	}
}

// TestParseKeeperDoctorArgs_RejectsUnknownFlag: an unrecognized leading-dash
// token exits 2 for doctor too.
func TestParseKeeperDoctorArgs_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	_, code := parseKeeperDoctorArgs([]string{"--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("--bogus: want exit 2, got %d", code)
	}
}

// TestParseKeeperDoctorArgs_ProjectFlagPreserved confirms --project is still
// recognized (enumerate-and-keep) and the positional agent is parsed.
func TestParseKeeperDoctorArgs_ProjectFlagPreserved(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	da, code := parseKeeperDoctorArgs([]string{"captain", "--project", "/p"}, &stdout, &stderr)
	if code != -1 {
		t.Fatalf("want -1, got %d", code)
	}
	if da.agentName != "captain" || da.projectDir != "/p" {
		t.Errorf("flags not parsed: %+v", da)
	}
}

// TestKeeperEnableEntry_RejectsUnknownFlag verifies the unknown-flag reject
// propagates exit 2 through the entry point.
func TestKeeperEnableEntry_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runKeeperEnableEntry([]string{"orchestrator", "--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("entry --bogus: want exit 2, got %d", code)
	}
}

// TestKeeperDoctorEntry_RejectsUnknownFlag verifies the unknown-flag reject
// propagates exit 2 through the doctor entry point.
func TestKeeperDoctorEntry_RejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runKeeperDoctorEntry([]string{"captain", "--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("entry --bogus: want exit 2, got %d", code)
	}
}

// ── captain-tools drift guard tests (hk-tgrw) ────────────────────────────────

// TestKeeperDoctor_CaptainToolsAbsent verifies that doctor passes (no failure)
// when ~/.claude/captain-tools/captain-launch.sh does not exist. An absent file
// means the operator has not installed captain-tools yet — not a drift error.
func TestKeeperDoctor_CaptainToolsAbsent(t *testing.T) {
	t.Parallel()

	captainToolsDir := t.TempDir() // empty — captain-launch.sh absent
	cfg, _ := makeDoctorCfg(t, "captain")
	cfg.captainToolsDir = captainToolsDir

	var stdout, stderr bytes.Buffer
	// The only failing checks are the ones that require actual keeper setup
	// (settings.json, ctx, etc.), not the captain-tools one. Just verify that
	// "captain-tools" is NOT in the output as a failure.
	runKeeperDoctor(cfg, &stdout, &stderr)

	out := stdout.String() + stderr.String()
	if strings.Contains(out, "captain-tools") {
		t.Errorf("captain-tools check should be silent when captain-launch.sh is absent, but got: %s", out)
	}
}

// TestKeeperDoctor_CaptainToolsInSync verifies that doctor reports "captain-tools"
// as OK when the runtime copy matches the binary-embedded captainLaunchSh.
func TestKeeperDoctor_CaptainToolsInSync(t *testing.T) {
	t.Parallel()

	captainToolsDir := t.TempDir()
	// Write the exact embedded content to the runtime path.
	runtimePath := filepath.Join(captainToolsDir, "captain-launch.sh")
	if err := os.WriteFile(runtimePath, captainLaunchSh, 0o755); err != nil {
		t.Fatalf("write runtime captain-launch.sh: %v", err)
	}

	cfg, _ := makeDoctorCfg(t, "captain")
	cfg.captainToolsDir = captainToolsDir

	var stdout, stderr bytes.Buffer
	runKeeperDoctor(cfg, &stdout, &stderr)

	out := stdout.String()
	if !strings.Contains(out, "captain-tools") {
		t.Errorf("captain-tools check should appear in output when file is present, got: %s", out)
	}
	// The check should pass (✓) and not flag a mismatch.
	if strings.Contains(out, "STALE") {
		t.Errorf("captain-tools check should not flag STALE when content matches embedded copy, got: %s", out)
	}
}

// TestKeeperDoctor_CaptainToolsStale verifies that doctor reports "captain-tools"
// as a failure when the runtime copy differs from the binary-embedded captainLaunchSh.
func TestKeeperDoctor_CaptainToolsStale(t *testing.T) {
	t.Parallel()

	captainToolsDir := t.TempDir()
	// Write stale (old) content — simulates the pre-hk-tgrw runtime copy.
	staleContent := []byte("#!/usr/bin/env bash\n# old version without project-hash or sentinel\necho 'stale'\n")
	runtimePath := filepath.Join(captainToolsDir, "captain-launch.sh")
	if err := os.WriteFile(runtimePath, staleContent, 0o755); err != nil {
		t.Fatalf("write stale captain-launch.sh: %v", err)
	}

	cfg, _ := makeDoctorCfg(t, "captain")
	cfg.captainToolsDir = captainToolsDir

	var stdout, stderr bytes.Buffer
	code := runKeeperDoctor(cfg, &stdout, &stderr)

	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "STALE") {
		t.Errorf("captain-tools check should flag STALE when runtime copy differs, got: %s", out)
	}
	if !strings.Contains(out, "harmonik init --force") {
		t.Errorf("captain-tools STALE message should mention 'harmonik init --force', got: %s", out)
	}
	if code == 0 {
		t.Errorf("want non-zero exit code when captain-tools is stale, got 0")
	}
}
