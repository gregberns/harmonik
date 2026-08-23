//go:build scenario

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kfe058MakeScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range keeperScriptNames {
		//nolint:gosec // G306: executable mode is required for shell-script fixtures
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("kfe058MakeScripts: write %s: %v", name, err)
		}
	}
	return dir
}

func kfe058ParseSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("kfe058ParseSettings: %v", err)
	}
	var m map[string]interface{}
	if jsonErr := json.Unmarshal(raw, &m); jsonErr != nil {
		t.Fatalf("kfe058ParseSettings parse: %v", jsonErr)
	}
	return m
}

func kfe058CountHookEntries(settings map[string]interface{}, eventName, scriptBasename string) int {
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
	n := 0
	for _, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		inner, ok := gMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, e := range inner {
			eMap, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, _ := eMap["command"].(string); strings.Contains(cmd, scriptBasename) {
				n++
			}
		}
	}
	return n
}

// TestScenario_KeeperEnableOn058_HKF5Z is the end-to-end scenario test for
// ON-058a/b (hk-f5z): two projects produce sibling hook groups in a shared
// settings.json without perturbing each other, and a single project-agnostic
// statusLine stanza is maintained across both enables.
func TestScenario_KeeperEnableOn058_HKF5Z(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	scriptsDir := kfe058MakeScripts(t)
	projectA := t.TempDir()
	projectB := t.TempDir()

	kfe058Cfg := func(projectDir string) enableConfig {
		return enableConfig{
			agentName:    "orchestrator",
			projectDir:   projectDir,
			scriptsDir:   scriptsDir,
			settingsPath: settingsPath,
		}
	}
	kfe058Doc := func(projectDir string) doctorConfig {
		return doctorConfig{
			agentName:    "orchestrator",
			projectDir:   projectDir,
			settingsPath: settingsPath,
		}
	}

	var out bytes.Buffer
	if code := runKeeperEnable(kfe058Cfg(projectA), &out, &out); code != 0 {
		t.Fatalf("Part A: enable projectA: want 0, got %d\n%s", code, out.String())
	}

	settingsA := kfe058ParseSettings(t, settingsPath)

	if n := kfe058CountHookEntries(settingsA, "Stop", "keeper-stop-hook.sh"); n != 1 {
		t.Errorf("Part A: Stop hook count: want 1, got %d", n)
	}
	if n := kfe058CountHookEntries(settingsA, "PreCompact", "keeper-precompact-hook.sh"); n != 1 {
		t.Errorf("Part A: PreCompact hook count: want 1, got %d", n)
	}

	sl, ok := settingsA["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("Part A: statusLine missing after projectA enable")
	}
	slCmd, _ := sl["command"].(string)
	if !strings.Contains(slCmd, "keeper-statusline.sh") {
		t.Errorf("Part A: statusLine.command missing keeper-statusline.sh: %q", slCmd)
	}
	if strings.Contains(slCmd, "HARMONIK_PROJECT=") {
		t.Errorf("Part A: ON-058b VIOLATED: statusLine.command has HARMONIK_PROJECT= after projectA enable: %q", slCmd)
	}
	if tp, _ := sl["type"].(string); tp != "command" {
		t.Errorf("Part A: statusLine.type = %q, want \"command\" (hk-hs1)", tp)
	}

	out.Reset()
	if code := runKeeperEnable(kfe058Cfg(projectB), &out, &out); code != 0 {
		t.Fatalf("Part B: enable projectB: want 0, got %d\n%s", code, out.String())
	}

	settingsB := kfe058ParseSettings(t, settingsPath)

	if n := kfe058CountHookEntries(settingsB, "Stop", "keeper-stop-hook.sh"); n != 2 {
		t.Errorf("Part B: ON-058a(2) VIOLATED: Stop hook count: want 2 (one per project), got %d", n)
	}
	if n := kfe058CountHookEntries(settingsB, "PreCompact", "keeper-precompact-hook.sh"); n != 2 {
		t.Errorf("Part B: ON-058a(2) VIOLATED: PreCompact hook count: want 2 (one per project), got %d", n)
	}

	foundA, cmdA := findHookForScript(settingsB, "Stop", "keeper-stop-hook.sh", projectA)
	if !foundA {
		t.Error("Part B: ON-058a(3) VIOLATED: project A's Stop hook group is gone after project B enable")
	}
	if !strings.Contains(cmdA, "HARMONIK_PROJECT="+projectA) {
		t.Errorf("Part B: ON-058a(3) VIOLATED: project A's Stop hook command perturbed: %q", cmdA)
	}

	if found, _ := findHookForScript(settingsB, "Stop", "keeper-stop-hook.sh", projectB); !found {
		t.Error("Part B: project B's Stop hook group missing after its own enable")
	}

	sl2, ok := settingsB["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("Part B: statusLine missing after projectB enable")
	}
	if cmd2, _ := sl2["command"].(string); strings.Contains(cmd2, "HARMONIK_PROJECT=") {
		t.Errorf("Part B: ON-058b VIOLATED: statusLine.command has HARMONIK_PROJECT= after projectB enable: %q", cmd2)
	}

	out.Reset()
	if code := runKeeperEnable(kfe058Cfg(projectA), &out, &out); code != 0 {
		t.Fatalf("Part C: re-enable projectA: want 0, got %d\n%s", code, out.String())
	}

	settingsC := kfe058ParseSettings(t, settingsPath)
	if n := kfe058CountHookEntries(settingsC, "Stop", "keeper-stop-hook.sh"); n != 2 {
		t.Errorf("Part C: ON-058a idempotency VIOLATED: Stop hook count after re-enable = %d, want 2", n)
	}
	if n := kfe058CountHookEntries(settingsC, "PreCompact", "keeper-precompact-hook.sh"); n != 2 {
		t.Errorf("Part C: ON-058a idempotency VIOLATED: PreCompact hook count after re-enable = %d, want 2", n)
	}

	var docOut, docErr bytes.Buffer
	runKeeperDoctor(kfe058Doc(projectA), &docOut, &docErr)
	docOutStr := docOut.String()

	if !strings.Contains(docOutStr, "✓ Stop hook") {
		t.Errorf("Part D1: ON-058a(4) VIOLATED: doctor did not greenlight projectA's own Stop hook group: %s", docOutStr)
	}
	if !strings.Contains(docOutStr, "✓ PreCompact hook") {
		t.Errorf("Part D1: ON-058a(4) VIOLATED: doctor did not greenlight projectA's own PreCompact hook group: %s", docOutStr)
	}

	projectC := t.TempDir() // never enabled
	docOut.Reset()
	docErr.Reset()
	code := runKeeperDoctor(kfe058Doc(projectC), &docOut, &docErr)
	docOutStr = docOut.String()
	if code == 0 {
		t.Errorf("Part D2: ON-058a(4) VIOLATED: doctor exited 0 for never-enabled project C — should find hook gaps, not greenlight on peer groups: %s", docOutStr)
	}
	if !strings.Contains(docOutStr, "✗ Stop hook") {
		t.Errorf("Part D2: ON-058a(4) VIOLATED: doctor greenlit project C's Stop hook on a peer project's group: %s", docOutStr)
	}
	if !strings.Contains(docOutStr, "✗ PreCompact hook") {
		t.Errorf("Part D2: ON-058a(4) VIOLATED: doctor greenlit project C's PreCompact hook on a peer project's group: %s", docOutStr)
	}
}
