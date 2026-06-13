//go:build scenario

package main

// scenario_keeper_enable_on058_hkf5z_test.go — scenario test: two projects'
// keeper-enable Stop/PreCompact hook groups coexist as siblings in
// ~/.claude/settings.json plus a single project-agnostic statusLine stanza
// (ON-058a/b, bead hk-f5z).
//
// # What is tested (specs/operator-nfr.md §4.12 ON-058a, ON-058b)
//
//   ON-058a — Project-keyed dedup (four sub-rules):
//     (1) dedup on (script-basename, HARMONIK_PROJECT=<projectDir>) pair, not on
//         basename alone;
//     (2) two distinct projects produce two sibling groups in hooks.Stop and
//         hooks.PreCompact;
//     (3) enable for project B MUST NOT rewrite project A's group command or env;
//     (4) doctor validates the presence of THIS project's group only — it MUST NOT
//         greenlight merely because a peer project's group exists.
//
//   ON-058b — Single project-agnostic statusLine:
//     (1–5) statusLine.command carries no HARMONIK_PROJECT= prefix; all projects
//     converge on the same bare script-path stanza; the merge after the first enable
//     is a no-op.
//
// # Approach — runKeeperEnable / runKeeperDoctor with injectable config
//
// `runKeeperEnableEntry` (the CLI flag parser) derives the settings path from the
// user's home directory and does not expose a --settings-path flag, so the test
// uses the testable runKeeperEnable / runKeeperDoctor functions directly with
// enableConfig / doctorConfig structs pointing at temp directories.  This is the
// same level of code coverage as the real operator path because runKeeperEnableEntry
// does nothing but parse flags and call runKeeperEnable — the dedup, merge, and I/O
// logic under test lives entirely in runKeeperEnable and the helpers it calls.
//
// No daemon is involved; keeper-enable is purely file-system I/O.
//
// # Scenario structure (four parts)
//
//   Part A — enable project A: 1 Stop group, 1 PreCompact group, 1 statusLine.
//   Part B — enable project B: 2 Stop groups, 2 PreCompact groups, still 1 statusLine.
//   Part C — re-enable project A: group count stays at 2 (idempotency).
//   Part D — doctor scope: doctor for project A passes; doctor for an unknown project
//             reports a gap even though other projects' groups exist.
//
// # Helper prefix
//
// All package-level identifiers use the "kfe058" prefix (bead hk-f5z, ON-058).
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_KeeperEnableOn058_HKF5Z ./cmd/harmonik/...
//
// Spec ref: specs/operator-nfr.md §4.12 ON-058a, ON-058b.
// Bead ref: hk-f5z.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kfe058MakeScripts creates a fake scripts directory with placeholder keeper scripts.
func kfe058MakeScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{
		"keeper-statusline.sh",
		"keeper-stop-hook.sh",
		"keeper-precompact-hook.sh",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("kfe058MakeScripts: write %s: %v", name, err)
		}
	}
	return dir
}

// kfe058ParseSettings reads and JSON-parses a settings.json file.
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

// kfe058CountHookEntries counts hook entry commands in hooks[eventName] that
// contain scriptBasename.
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
	// ── Shared fixtures ─────────────────────────────────────────────────────────
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	scriptsDir := kfe058MakeScripts(t)
	projectA := t.TempDir()
	projectB := t.TempDir()

	// kfe058Cfg builds an enableConfig wired to the shared settingsPath.
	kfe058Cfg := func(projectDir string) enableConfig {
		return enableConfig{
			agentName:    "orchestrator",
			projectDir:   projectDir,
			scriptsDir:   scriptsDir,
			settingsPath: settingsPath,
		}
	}
	// kfe058Doc builds a doctorConfig for the shared settingsPath.
	kfe058Doc := func(projectDir string) doctorConfig {
		return doctorConfig{
			agentName:    "orchestrator",
			projectDir:   projectDir,
			settingsPath: settingsPath,
		}
	}

	// ── Part A — enable project A ───────────────────────────────────────────────
	var out bytes.Buffer
	if code := runKeeperEnable(kfe058Cfg(projectA), &out, &out); code != 0 {
		t.Fatalf("Part A: enable projectA: want 0, got %d\n%s", code, out.String())
	}

	settingsA := kfe058ParseSettings(t, settingsPath)

	// One Stop group, one PreCompact group.
	if n := kfe058CountHookEntries(settingsA, "Stop", "keeper-stop-hook.sh"); n != 1 {
		t.Errorf("Part A: Stop hook count: want 1, got %d", n)
	}
	if n := kfe058CountHookEntries(settingsA, "PreCompact", "keeper-precompact-hook.sh"); n != 1 {
		t.Errorf("Part A: PreCompact hook count: want 1, got %d", n)
	}

	// ON-058b: statusLine is project-agnostic.
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
	// hk-hs1: type must be "command".
	if tp, _ := sl["type"].(string); tp != "command" {
		t.Errorf("Part A: statusLine.type = %q, want \"command\" (hk-hs1)", tp)
	}

	// ── Part B — enable project B ───────────────────────────────────────────────
	out.Reset()
	if code := runKeeperEnable(kfe058Cfg(projectB), &out, &out); code != 0 {
		t.Fatalf("Part B: enable projectB: want 0, got %d\n%s", code, out.String())
	}

	settingsB := kfe058ParseSettings(t, settingsPath)

	// ON-058a(1–2): two sibling groups — one per project.
	if n := kfe058CountHookEntries(settingsB, "Stop", "keeper-stop-hook.sh"); n != 2 {
		t.Errorf("Part B: ON-058a(2) VIOLATED: Stop hook count: want 2 (one per project), got %d", n)
	}
	if n := kfe058CountHookEntries(settingsB, "PreCompact", "keeper-precompact-hook.sh"); n != 2 {
		t.Errorf("Part B: ON-058a(2) VIOLATED: PreCompact hook count: want 2 (one per project), got %d", n)
	}

	// ON-058a(3): project A's group must still be intact.
	foundA, cmdA := findHookForScript(settingsB, "Stop", "keeper-stop-hook.sh", projectA)
	if !foundA {
		t.Error("Part B: ON-058a(3) VIOLATED: project A's Stop hook group is gone after project B enable")
	}
	if !strings.Contains(cmdA, "HARMONIK_PROJECT="+projectA) {
		t.Errorf("Part B: ON-058a(3) VIOLATED: project A's Stop hook command perturbed: %q", cmdA)
	}

	// Project B's group exists.
	if found, _ := findHookForScript(settingsB, "Stop", "keeper-stop-hook.sh", projectB); !found {
		t.Error("Part B: project B's Stop hook group missing after its own enable")
	}

	// ON-058b: statusLine still project-agnostic and unchanged.
	sl2, ok := settingsB["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("Part B: statusLine missing after projectB enable")
	}
	if cmd2, _ := sl2["command"].(string); strings.Contains(cmd2, "HARMONIK_PROJECT=") {
		t.Errorf("Part B: ON-058b VIOLATED: statusLine.command has HARMONIK_PROJECT= after projectB enable: %q", cmd2)
	}

	// ── Part C — idempotency: re-enable project A must not add a third group ────
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

	// ── Part D — doctor scope (ON-058a(4)) ──────────────────────────────────────
	// D1: doctor for project A passes its hook checks.
	var docOut, docErr bytes.Buffer
	runKeeperDoctor(kfe058Doc(projectA), &docOut, &docErr)
	docOutStr := docOut.String()
	if strings.Contains(docOutStr, "✗ Stop hook") {
		t.Errorf("Part D1: ON-058a(4) VIOLATED: doctor reported Stop hook gap for projectA (should find its own group): %s", docOutStr)
	}
	if strings.Contains(docOutStr, "✗ PreCompact hook") {
		t.Errorf("Part D1: ON-058a(4) VIOLATED: doctor reported PreCompact gap for projectA (should find its own group): %s", docOutStr)
	}

	// D2: doctor for an unknown third project must report hook gaps —
	// it MUST NOT greenlight on project A's or B's groups.
	projectC := t.TempDir() // never enabled
	docOut.Reset()
	docErr.Reset()
	code := runKeeperDoctor(kfe058Doc(projectC), &docOut, &docErr)
	docOutStr = docOut.String()
	if code == 0 {
		t.Errorf("Part D2: ON-058a(4) VIOLATED: doctor exited 0 for never-enabled project C — should find hook gaps, not greenlight on peer groups: %s", docOutStr)
	}
	if !strings.Contains(docOutStr, "✗ Stop hook") && !strings.Contains(docOutStr, "✗") {
		t.Errorf("Part D2: ON-058a(4): doctor for project C emitted no ✗ markers (expected hook gaps): %s", docOutStr)
	}
}
