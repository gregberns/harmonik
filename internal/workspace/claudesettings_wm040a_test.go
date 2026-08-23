package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDaemonBinaryPath = "/usr/local/bin/harmonik-test"

func claudeSettingsFixturePath(workspacePath string) string {
	return ClaudeSettingsPath(workspacePath)
}

func claudeSettingsFixtureReadJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw := mustReadFile(t, path)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("claudeSettingsFixtureReadJSON: Unmarshal: %v", err)
	}
	return m
}

func claudeSettingsFixtureHooksMap(t *testing.T, m map[string]interface{}) map[string]interface{} {
	t.Helper()
	hRaw, ok := m["hooks"]
	if !ok {
		t.Fatalf("claudeSettingsFixtureHooksMap: no top-level 'hooks' key")
	}
	h, ok := hRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("claudeSettingsFixtureHooksMap: 'hooks' is not an object, got %T", hRaw)
	}
	return h
}

func claudeSettingsFixtureHookEntries(t *testing.T, hooksMap map[string]interface{}, eventKind string) []interface{} {
	t.Helper()
	raw, ok := hooksMap[eventKind]
	if !ok {
		t.Fatalf("claudeSettingsFixtureHookEntries: no key %q in hooks", eventKind)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("claudeSettingsFixtureHookEntries: %q value is not array, got %T", eventKind, raw)
	}
	return arr
}

func claudeSettingsFixtureCountBridgeHooks(arr []interface{}) (groups, entries int) {
	for _, elem := range arr {
		groupMap, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		hooks, ok := groupMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		found := 0
		for _, entry := range hooks {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			args, ok := entryMap["args"].([]interface{})
			if !ok || len(args) == 0 {
				continue
			}
			if verb, ok := args[0].(string); ok && verb == "hook-relay" {
				found++
			}
		}
		if found > 0 {
			groups++
			entries += found
		}
	}
	return groups, entries
}

func claudeSettingsFixtureHookEntryPresent(arr []interface{}, wantCommand string) bool {
	for _, elem := range arr {
		groupMap, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		hooks, ok := groupMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, entry := range hooks {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, ok := entryMap["command"].(string); ok && cmd == wantCommand {
				return true
			}
		}
	}
	return false
}

func claudeSettingsFixtureMarkerPresent(t *testing.T, arr []interface{}, marker string) bool {
	t.Helper()
	for _, elem := range arr {
		groupMap, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		hooks, ok := groupMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, entry := range hooks {
			raw, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("claudeSettingsFixtureMarkerPresent: marshal hook entry: %v", err)
			}
			if strings.Contains(string(raw), marker) {
				return true
			}
		}
	}
	return false
}

func claudeSettingsFixtureBridgeGroupPresent(arr []interface{}, eventKind, wantCommand string) bool {
	for _, elem := range arr {
		m, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		if matcher, ok := m["matcher"].(string); !ok || matcher != "" {
			continue
		}
		hooks, ok := m["hooks"].([]interface{})
		if !ok || len(hooks) == 0 {
			continue
		}
		h, ok := hooks[0].(map[string]interface{})
		if !ok {
			continue
		}
		if h["command"] != wantCommand {
			continue
		}
		args, ok := h["args"].([]interface{})
		if !ok || len(args) < 2 {
			continue
		}
		if args[0] == "hook-relay" && args[1] == eventKind {
			return true
		}
	}
	return false
}

// TestWM040a_CleanWorkspaceMaterialization verifies that, on a workspace with
// no prior .claude/settings.json, MaterializeClaudeSettings creates the file
// with all five bridge hook entries per CHB-003 and the gitignore hygiene line
// per CHB-005.
//
// Spec ref: workspace-model.md §4.7a WM-040a; claude-hook-bridge.md CHB-001..003, CHB-005.
func TestWM040a_CleanWorkspaceMaterialization(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (clean): %v", err)
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)

	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("WM-040a: settings.json not on disk: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(settingsPath))
	if err != nil {
		t.Fatalf("WM-040a: ReadDir .claude/: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("WM-040a: orphan tmp file after clean materialization: %q", e.Name())
		}
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a: bridge group missing for event kind %q", kind)
		}
	}

	if _, ok := m["disableAllHooks"]; ok {
		t.Errorf("WM-040a: disableAllHooks key present in output; MUST be stripped")
	}

	gitignorePath := filepath.Join(workspacePath, ".gitignore")
	if _, err := os.Stat(gitignorePath); !os.IsNotExist(err) {
		t.Errorf("hk-jvzc2: MaterializeClaudeSettings created .gitignore (stat err=%v); MUST be operator-managed", err)
	}
}

// TestWM040a_MergeWithExistingUserHooks verifies that, when a
// .claude/settings.json already exists with user-defined hooks, the bridge
// entries are APPENDED to each event-type array and user hooks are preserved
// per CHB-004.
//
// Spec ref: claude-hook-bridge.md CHB-004.
func TestWM040a_MergeWithExistingUserHooks(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	userHooks := map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": []interface{}{
				map[string]interface{}{
					"matcher": "*.go",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "my-tool",
							"args":    []interface{}{"--session-start"},
							"timeout": 10,
						},
					},
				},
			},
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "user-stop-hook",
							"args":    []interface{}{},
							"timeout": 5,
						},
					},
				},
			},
		},
		"theme": "dark",
	}
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}
	raw, err := json.Marshal(userHooks)
	if err != nil {
		t.Fatalf("WM-040a: marshal user hooks: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile user settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (merge): %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)

	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a merge: bridge group missing for %q", kind)
		}
	}

	sessionStartArr := claudeSettingsFixtureHookEntries(t, hooks, "SessionStart")
	if len(sessionStartArr) != 2 {
		t.Errorf("WM-040a merge: SessionStart array len = %d; want exactly 2 (user + one bridge)", len(sessionStartArr))
	}

	stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")
	if len(stopArr) != 2 {
		t.Errorf("WM-040a merge: Stop array len = %d; want exactly 2 (user + one bridge)", len(stopArr))
	}

	if _, ok := m["theme"]; !ok {
		t.Errorf("WM-040a merge: 'theme' key was removed; user settings must be preserved")
	}
}

// TestWM040a_RepeatedLaunchesLeaveOneBridgeGroup is the regression test for
// hk-dknb2. One worktree hosts several launches — implementer, resume,
// reviewer, and every retry — and this function runs on each of them. It used
// to append, so a worktree accumulated one copy of every bridge group per
// launch. Seven of forty live worktrees carried duplicates and the worst held
// four copies of each of the five groups, which makes Claude fire each hook
// four times and report every agent finish four times.
func TestWM040a_RepeatedLaunchesLeaveOneBridgeGroup(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	userSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "user-stop-hook",
							"args":    []interface{}{},
							"timeout": 5,
						},
					},
				},
			},
		},
	}
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}
	raw, err := json.Marshal(userSettings)
	if err != nil {
		t.Fatalf("WM-040a: marshal user settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile user settings: %v", err)
	}

	const launches = 4
	for i := 0; i < launches; i++ {
		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("WM-040a: MaterializeClaudeSettings (launch %d): %v", i+1, err)
		}
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)

	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		bridges, bridgeEntries := claudeSettingsFixtureCountBridgeHooks(arr)
		if bridges != 1 || bridgeEntries != 1 {
			t.Errorf("WM-040a: %q holds %d bridge groups / %d bridge entries after %d launches; want exactly 1 of each — Claude fires a hook once per copy",
				kind, bridges, bridgeEntries, launches)
		}
	}

	stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")
	if len(stopArr) != 2 {
		t.Errorf("WM-040a: Stop array len = %d after %d launches; want exactly 2 (user + one bridge)", len(stopArr), launches)
	}
	if !claudeSettingsFixtureHookEntryPresent(stopArr, "user-stop-hook") {
		t.Errorf("WM-040a: the user's own Stop hook was removed; de-duplication must only touch harmonik's own entries")
	}
}

// TestWM040a_StaleBridgeGroupIsReplacedNotKept proves the replacement is keyed
// on the hook-relay verb and not on the whole group. A binary that moved leaves
// a group naming a path that no longer exists; keeping it beside the new one
// would fire a hook that cannot run.
func TestWM040a_StaleBridgeGroupIsReplacedNotKept(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}

	stale := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/old/path/to/harmonik",
							"args":    []interface{}{"hook-relay", "Stop"},
							"timeout": 15,
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("WM-040a: marshal stale settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile stale settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings: %v", err)
	}

	hooks := claudeSettingsFixtureHooksMap(t, claudeSettingsFixtureReadJSON(t, settingsPath))
	stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")
	if len(stopArr) != 1 {
		t.Fatalf("WM-040a: Stop array len = %d; want exactly 1 — the stale bridge group must be replaced, not joined", len(stopArr))
	}
	if !claudeSettingsFixtureBridgeGroupPresent(stopArr, "Stop", testDaemonBinaryPath) {
		t.Errorf("WM-040a: the surviving Stop group does not name the current binary")
	}
}

// TestWM040a_ForeignEntryInsideBridgeGroupSurvives pins the filter to the hook
// ENTRY and not to the matcher GROUP.
//
// Harmonik writes one entry per group, so nothing on disk needs the distinction
// today. But the bridge group carries the default matcher "", which is the most
// collided-with value there is, and a group-level drop would take any entry a
// later writer put beside harmonik's — with no warning and no log line. This
// test puts a foreign entry inside harmonik's own group and requires it to
// survive a launch.
func TestWM040a_ForeignEntryInsideBridgeGroupSurvives(t *testing.T) {
	t.Parallel()

	const foreignCommand = "third-party-stop-hook"

	workspacePath := t.TempDir()
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}

	shared := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/old/path/to/harmonik",
							"args":    []interface{}{"hook-relay", "Stop"},
							"timeout": 30,
						},
						map[string]interface{}{
							"type":    "command",
							"command": foreignCommand,
							"args":    []interface{}{"--notify"},
							"timeout": 5,
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(shared)
	if err != nil {
		t.Fatalf("WM-040a: marshal shared-group settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile shared-group settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings: %v", err)
	}

	hooks := claudeSettingsFixtureHooksMap(t, claudeSettingsFixtureReadJSON(t, settingsPath))
	stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")

	if !claudeSettingsFixtureHookEntryPresent(stopArr, foreignCommand) {
		t.Errorf("WM-040a: the foreign entry %q was removed with harmonik's own entry; the filter must drop ENTRIES, not the whole group", foreignCommand)
	}
	bridges, bridgeEntries := claudeSettingsFixtureCountBridgeHooks(stopArr)
	if bridges != 1 || bridgeEntries != 1 {
		t.Errorf("WM-040a: Stop holds %d bridge groups / %d bridge entries; want exactly 1 of each — the stale entry must be replaced, not joined",
			bridges, bridgeEntries)
	}
	if !claudeSettingsFixtureBridgeGroupPresent(stopArr, "Stop", testDaemonBinaryPath) {
		t.Errorf("WM-040a: no Stop bridge group names the current binary")
	}
}

// TestWM040a_TwoLaunchesLeaveByteIdenticalSettings asserts the property the fix
// actually claims: launch N and launch N+1 leave the same file.
//
// The encoder is canonical — Go sorts map keys and both write sites go through
// marshalSettings — so comparing the bytes is a direct proof of the claim, not
// a proxy for it. The duplicate count that follows is taken from the parsed
// file with a helper that spells the hook-relay verb literally, so it stays an
// independent oracle: a false negative in the product predicate cannot report
// one group while the file holds two.
func TestWM040a_TwoLaunchesLeaveByteIdenticalSettings(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}

	userSettings := map[string]interface{}{
		"theme": "dark",
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "user-stop-hook",
							"args":    []interface{}{},
							"timeout": 5,
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(userSettings)
	if err != nil {
		t.Fatalf("WM-040a: marshal user settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile user settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (launch 1): %v", err)
	}
	afterFirst := mustReadFile(t, settingsPath)

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (launch 2): %v", err)
	}
	afterSecond := mustReadFile(t, settingsPath)

	if !bytes.Equal(afterFirst, afterSecond) {
		t.Errorf("WM-040a: launch 2 changed the settings file; the merge must leave the same bytes.\n--- after launch 1 ---\n%s\n--- after launch 2 ---\n%s",
			string(afterFirst), string(afterSecond))
	}

	hooks := claudeSettingsFixtureHooksMap(t, claudeSettingsFixtureReadJSON(t, settingsPath))
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		groups, entries := claudeSettingsFixtureCountBridgeHooks(arr)
		if groups != 1 || entries != 1 {
			t.Errorf("WM-040a: %q holds %d bridge groups / %d bridge entries after 2 launches; want exactly 1 of each",
				kind, groups, entries)
		}
	}
}

// TestWM040a_UnrecognisedHookEntryShapesSurvive pins the predicate to
// FAIL-CLOSED. It is the sensor for the safety property this whole filter rests
// on: harmonik must never delete a hook it did not write.
//
// `isBridgeHookEntry` decides that question, and it has four shape checks that
// each fall through to KEEP — the entry is not an object, `args` is absent or
// is not an array, `args` is empty, `args[0]` is not a string. Nothing asserted
// any of them. Flipping all four to return true left the whole package green,
// so the safety property had no sensor at all. That is the same shape as the
// defect this bead descends from: an assertion that cannot fail on the thing it
// exists to prevent.
//
// Each case puts the odd entry INSIDE harmonik's own default-matcher group,
// beside harmonik's entry. That is the placement that hurts: if the predicate
// claims the odd entry as ours, the group empties, the group is dropped, and
// the entry is gone with no warning and no log line.
func TestWM040a_UnrecognisedHookEntryShapesSurvive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		marker string
		entry  interface{}
	}{
		{
			name:   "entry is not an object",
			marker: "not-an-object-survivor",
			entry:  "not-an-object-survivor",
		},
		{
			name:   "args key absent",
			marker: "args-absent-survivor",
			entry: map[string]interface{}{
				"type":    "command",
				"command": "args-absent-survivor",
				"timeout": 5,
			},
		},
		{
			name:   "args is not an array",
			marker: "args-not-array-survivor",
			entry: map[string]interface{}{
				"type":    "command",
				"command": "args-not-array-survivor",
				"args":    "hook-relay",
				"timeout": 5,
			},
		},
		{
			name:   "args is empty",
			marker: "args-empty-survivor",
			entry: map[string]interface{}{
				"type":    "command",
				"command": "args-empty-survivor",
				"args":    []interface{}{},
				"timeout": 5,
			},
		},
		{
			name:   "args[0] is not a string",
			marker: "args-first-not-string-survivor",
			entry: map[string]interface{}{
				"type":    "command",
				"command": "args-first-not-string-survivor",
				"args":    []interface{}{42, "hook-relay"},
				"timeout": 5,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workspacePath := t.TempDir()
			settingsPath := claudeSettingsFixturePath(workspacePath)
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatalf("WM-040a: MkdirAll: %v", err)
			}

			seed := map[string]interface{}{
				"hooks": map[string]interface{}{
					"Stop": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "/old/path/to/harmonik",
									"args":    []interface{}{"hook-relay", "Stop"},
									"timeout": 30,
								},
								// The odd entry, which MUST survive.
								tc.entry,
							},
						},
					},
				},
			}
			raw, err := json.Marshal(seed)
			if err != nil {
				t.Fatalf("WM-040a: marshal seed settings: %v", err)
			}
			if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
				t.Fatalf("WM-040a: WriteFile seed settings: %v", err)
			}

			if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
				t.Fatalf("WM-040a: MaterializeClaudeSettings: %v", err)
			}

			hooks := claudeSettingsFixtureHooksMap(t, claudeSettingsFixtureReadJSON(t, settingsPath))
			stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")

			if !claudeSettingsFixtureMarkerPresent(t, stopArr, tc.marker) {
				t.Errorf("WM-040a: the entry harmonik cannot recognise (%s) was deleted; the predicate MUST fail closed and keep every shape it does not recognise", tc.name)
			}
			if claudeSettingsFixtureMarkerPresent(t, stopArr, "/old/path/to/harmonik") {
				t.Errorf("WM-040a: harmonik's own stale entry survived; keeping the odd entry must not stop the removal of ours")
			}
			bridges, bridgeEntries := claudeSettingsFixtureCountBridgeHooks(stopArr)
			if bridges != 1 || bridgeEntries != 1 {
				t.Errorf("WM-040a: Stop holds %d bridge groups / %d bridge entries; want exactly 1 of each", bridges, bridgeEntries)
			}
		})
	}
}

// TestWM040a_MalformedJSONOverwrite verifies that a malformed existing
// settings.json is overwritten with bridge-required content, and a warning
// line is logged to the session log per CHB-004.
//
// Spec ref: claude-hook-bridge.md CHB-004 (malformed branch).
func TestWM040a_MalformedJSONOverwrite(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{bad json`), 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile malformed: %v", err)
	}

	sessionLogPath := filepath.Join(t.TempDir(), "session.log")

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, sessionLogPath); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (malformed): %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a malformed overwrite: bridge group missing for %q after overwrite", kind)
		}
	}

	logData := mustReadFile(t, sessionLogPath)
	if !strings.Contains(string(logData), "malformed") && !strings.Contains(string(logData), "overwritten") {
		t.Errorf("WM-040a: session log missing expected warning; got: %q", logData)
	}
}

// TestWM040a_DisableAllHooksStripped verifies that a "disableAllHooks": true
// key in the user's existing settings.json is removed from the merged result
// per CHB-004.
//
// Spec ref: claude-hook-bridge.md CHB-004 (disableAllHooks strip).
func TestWM040a_DisableAllHooksStripped(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}

	userSettings := map[string]interface{}{
		"disableAllHooks": true,
		"hooks":           map[string]interface{}{},
	}
	raw, err := json.Marshal(userSettings)
	if err != nil {
		t.Fatalf("WM-040a: marshal disableAllHooks settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("WM-040a: WriteFile disableAllHooks settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (disableAllHooks): %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)

	if _, ok := m["disableAllHooks"]; ok {
		t.Errorf("WM-040a: disableAllHooks present in merged result; MUST be stripped")
	}

	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a: bridge group missing for %q after disableAllHooks strip", kind)
		}
	}
}

// TestHkJvzc2_MaterializeClaudeSettingsDoesNotTouchGitignore verifies that
// MaterializeClaudeSettings does NOT create or modify any .gitignore (the
// hk-jvzc2 contract). CHB-005 hygiene is an operator-setup obligation.
//
// A pre-existing operator-managed .gitignore at the workspace path MUST be
// left byte-for-byte identical across multiple Materialize calls.
func TestHkJvzc2_MaterializeClaudeSettingsDoesNotTouchGitignore(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	gitignorePath := filepath.Join(workspacePath, ".gitignore")

	preExisting := "# operator setup\n.harmonik/\n.claude/settings.json\n"
	if err := os.WriteFile(gitignorePath, []byte(preExisting), 0o600); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
	preStat, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("stat seeded .gitignore: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("hk-jvzc2: MaterializeClaudeSettings #1: %v", err)
	}
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("hk-jvzc2: MaterializeClaudeSettings #2: %v", err)
	}

	postData := mustReadFile(t, gitignorePath)
	if string(postData) != preExisting {
		t.Errorf("hk-jvzc2: .gitignore was mutated by MaterializeClaudeSettings:\nwant:\n%q\ngot:\n%q",
			preExisting, string(postData))
	}

	postStat, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("stat post-call .gitignore: %v", err)
	}
	if preStat.Size() != postStat.Size() {
		t.Errorf("hk-jvzc2: .gitignore size changed from %d to %d", preStat.Size(), postStat.Size())
	}
}

// TestWM040a_OrderingSettingsBeforeWorkspaceLeased verifies the temporal
// ordering invariant: settings.json is fsynced to disk BEFORE the conceptual
// workspace_leased emission point (CHB-002, WM-040a).
//
// The test approximates this by recording a timestamp before materialization
// and confirming the file's mtime is not before the start time.
//
// Spec ref: claude-hook-bridge.md CHB-002; workspace-model.md §4.7a WM-040a.
func TestWM040a_OrderingSettingsBeforeWorkspaceLeased(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	before := time.Now()
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a ordering: MaterializeClaudeSettings: %v", err)
	}
	after := time.Now()

	settingsPath := claudeSettingsFixturePath(workspacePath)
	fi, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("WM-040a ordering: Stat settings.json: %v", err)
	}

	mtime := fi.ModTime()
	if mtime.Before(before) {
		t.Errorf("WM-040a ordering: settings.json mtime %v is before call start %v; fsync ordering violated", mtime, before)
	}
	_ = after // after is an upper bound; not checked (mtime <= after is trivially true on local fs)

	raw := mustReadFile(t, settingsPath)
	if len(raw) == 0 {
		t.Errorf("WM-040a ordering: settings.json is empty; must contain bridge content before workspace_leased")
	}
}

// TestWM040a_CHB003HookShape verifies the exact shape of bridge hook entries
// per CHB-003: type=command, command=<absolute binary path>, args=["hook-relay","<kind>"],
// timeout=30, matcher="". The command field MUST be the absolute path to the
// running daemon binary (hk-kqdpf.6 — not a bare "harmonik" name).
//
// Spec ref: claude-hook-bridge.md CHB-003.
func TestWM040a_CHB003HookShape(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a CHB-003: MaterializeClaudeSettings: %v", err)
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)
	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)

	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if len(arr) == 0 {
			t.Errorf("CHB-003: no entries for event kind %q", kind)
			continue
		}

		var found bool
		for _, elem := range arr {
			group, ok := elem.(map[string]interface{})
			if !ok {
				continue
			}
			if group["matcher"] != "" {
				continue
			}
			hookArr, ok := group["hooks"].([]interface{})
			if !ok || len(hookArr) == 0 {
				t.Errorf("CHB-003: %q bridge group has no hooks array", kind)
				break
			}
			h, ok := hookArr[0].(map[string]interface{})
			if !ok {
				t.Errorf("CHB-003: %q bridge group hook[0] is not object", kind)
				break
			}

			if h["type"] != "command" {
				t.Errorf("CHB-003: %q hook type = %q; want \"command\"", kind, h["type"])
			}
			if h["command"] != testDaemonBinaryPath {
				t.Errorf("CHB-003: %q hook command = %q; want absolute path %q", kind, h["command"], testDaemonBinaryPath)
			}
			args, ok := h["args"].([]interface{})
			if !ok || len(args) != 2 {
				t.Errorf("CHB-003: %q hook args = %v; want [\"hook-relay\", %q]", kind, h["args"], kind)
			} else {
				if args[0] != "hook-relay" {
					t.Errorf("CHB-003: %q hook args[0] = %q; want \"hook-relay\"", kind, args[0])
				}
				if args[1] != kind {
					t.Errorf("CHB-003: %q hook args[1] = %q; want %q", kind, args[1], kind)
				}
			}
			timeoutVal, ok := h["timeout"].(float64)
			if !ok || int(timeoutVal) != 30 {
				t.Errorf("CHB-003: %q hook timeout = %v; want 30", kind, h["timeout"])
			}

			found = true
			break
		}
		if !found {
			t.Errorf("CHB-003: bridge group (matcher=\"\") not found for event kind %q", kind)
		}
	}

	wantKinds := []string{"SessionStart", "Stop", "SessionEnd", "StopFailure", "Notification"}
	for _, kind := range wantKinds {
		if _, ok := hooks[kind]; !ok {
			t.Errorf("CHB-003: event kind %q absent from hooks object; all five are required", kind)
		}
	}
}

// TestWM040a_AtomicWriteNoOrphan verifies the WM-026 atomic-write discipline:
// after a successful MaterializeClaudeSettings, no .tmp-* file is left behind.
//
// Spec ref: workspace-model.md §4.7 WM-026 (atomic-write discipline).
func TestWM040a_AtomicWriteNoOrphan(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a atomic: MaterializeClaudeSettings: %v", err)
	}

	claudeDir := filepath.Join(workspacePath, ".claude")
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		t.Fatalf("WM-040a atomic: ReadDir .claude/: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), fmt.Sprintf(".tmp-%d", os.Getpid())) {
			t.Errorf("WM-040a atomic: orphan tmp file remains after successful write: %q", e.Name())
		}
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("WM-040a atomic: canonical settings.json missing: %v", err)
	}
}

// TestWM040a_HookCommandIsAbsolutePath verifies that the hook "command" field
// in the materialized settings.json is exactly the daemonBinaryPath passed in,
// not the bare name "harmonik" (hk-kqdpf.6 acceptance criterion).
//
// This test uses os.Executable() to get the actual test binary path, ensuring
// the absolute-path contract holds for a real path rather than a constant.
//
// Spec ref: claude-hook-bridge.md CHB-003 (hook command field); hk-kqdpf.6.
func TestWM040a_HookCommandIsAbsolutePath(t *testing.T) {
	t.Parallel()

	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("TestWM040a_HookCommandIsAbsolutePath: os.Executable(): %v", err)
	}

	workspacePath := t.TempDir()
	if err := MaterializeClaudeSettings(workspacePath, execPath, ""); err != nil {
		t.Fatalf("TestWM040a_HookCommandIsAbsolutePath: MaterializeClaudeSettings: %v", err)
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)
	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)

	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, execPath) {
			t.Errorf("hk-kqdpf.6: hook command for %q is not the absolute path %q", kind, execPath)
		}
	}

	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		for _, elem := range arr {
			m2, ok := elem.(map[string]interface{})
			if !ok {
				continue
			}
			hooksArr, ok := m2["hooks"].([]interface{})
			if !ok || len(hooksArr) == 0 {
				continue
			}
			h, ok := hooksArr[0].(map[string]interface{})
			if !ok {
				continue
			}
			cmd, ok := h["command"].(string)
			if ok && cmd == "harmonik" {
				t.Errorf("hk-kqdpf.6 regression: hook command for %q is bare \"harmonik\"; must be absolute path", kind)
			}
		}
	}
}

// TestWM040a_PermissionsAllowAbsent verifies that a freshly-materialized
// settings.json does NOT write a harmonik-default "permissions.allow" block
// (hk trust-modal fix, 2026-07-06).
//
// Rationale: in a git-worktree context Claude Code >= 2.1.201 fires an interactive
// "This folder pre-approves N tool permissions in .claude/settings.json" consent
// modal whenever project-local settings declare permissions.allow. That modal is
// NOT suppressed by the ~/.claude.json trust keys (hasTrustDialogAccepted /
// hasCompletedProjectOnboarding) NOR by --dangerously-skip-permissions, so a
// daemon-spawned pane wedges at it and times out at agent_ready (HC-056). Every
// harmonik worktree launch already passes --dangerously-skip-permissions
// (HC-055b), which makes the allow-list redundant; omitting it removes the modal.
// This inverts the former TestWM040a_PermissionsAllowPresent (hk-53y35).
//
// Spec ref: workspace-model.md §4.7a WM-040a (permissions-block removal note).
func TestWM040a_PermissionsAllowAbsent(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowAbsent: MaterializeClaudeSettings: %v", err)
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)
	m := claudeSettingsFixtureReadJSON(t, settingsPath)

	if _, ok := m["permissions"]; ok {
		t.Errorf("TestWM040a_PermissionsAllowAbsent: 'permissions' key present; harmonik must not write a permissions.allow block (trust-modal fix)")
	}

	if _, ok := m["dangerouslySkipPermissions"]; ok {
		t.Errorf("TestWM040a_PermissionsAllowAbsent: dangerouslySkipPermissions present; must not be set (CHB-007 deny-list)")
	}
}

// TestWM040a_PermissionsAllowPreservedOnMerge verifies that when an existing
// settings.json already has a permissions.allow key, MaterializeClaudeSettings
// does not overwrite it (user wins).
//
// Spec ref: workspace-model.md §4.7a WM-040a; claude-hook-bridge.md CHB-004.
// Bead: hk-53y35.
func TestWM040a_PermissionsAllowPreservedOnMerge(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	settingsPath := claudeSettingsFixturePath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: MkdirAll: %v", err)
	}

	userAllow := []interface{}{"MyCustomTool"}
	userSettings := map[string]interface{}{
		"hooks": map[string]interface{}{},
		"permissions": map[string]interface{}{
			"allow": userAllow,
		},
	}
	raw, err := json.Marshal(userSettings)
	if err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: json.Marshal: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: WriteFile: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: MaterializeClaudeSettings: %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	permRaw, ok := m["permissions"]
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: 'permissions' key absent after merge")
	}
	permMap, ok := permRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: 'permissions' not an object")
	}
	allowRaw, ok := permMap["allow"]
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: 'permissions.allow' absent after merge")
	}
	allowArr, ok := allowRaw.([]interface{})
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: 'permissions.allow' not an array")
	}
	found := false
	for _, v := range allowArr {
		if v == "MyCustomTool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TestWM040a_PermissionsAllowPreservedOnMerge: user tool 'MyCustomTool' was overwritten; must be preserved")
	}
}

// TestWM040a_AutoLoadedSkillsDisabled verifies that MaterializeClaudeSettings
// always writes "autoLoadedSkillsDirectories": [] into the materialized
// settings.json, both on fresh write and on merge, so fleet orchestration skills
// in .claude/skills/ cannot auto-load into worker agent panes.
//
// This is a hard invariant (always-overwrite, not user-overridable): harmonik
// controls skill scoping via required_skills[] + manifest context[]; ambient
// autoload would bypass that per-agent scoping.
//
// Spec ref: T6/hk-j79ny; agent-manifest SPEC.md §6.
func TestWM040a_AutoLoadedSkillsDisabled(t *testing.T) {
	t.Parallel()

	t.Run("fresh workspace", func(t *testing.T) {
		t.Parallel()
		workspacePath := t.TempDir()
		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("MaterializeClaudeSettings (fresh): %v", err)
		}
		settingsPath := claudeSettingsFixturePath(workspacePath)
		m := claudeSettingsFixtureReadJSON(t, settingsPath)
		assertAutoLoadedSkillsDisabled(t, m, "fresh workspace")
	})

	t.Run("merge preserves hard override", func(t *testing.T) {
		t.Parallel()
		workspacePath := t.TempDir()
		settingsPath := claudeSettingsFixturePath(workspacePath)
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		existing := map[string]interface{}{
			"hooks": map[string]interface{}{},
			"theme": "dark",
		}
		raw, err := json.Marshal(existing)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("MaterializeClaudeSettings (merge): %v", err)
		}
		m := claudeSettingsFixtureReadJSON(t, settingsPath)
		assertAutoLoadedSkillsDisabled(t, m, "merge over existing without key")
		if _, ok := m["theme"]; !ok {
			t.Errorf("merge: 'theme' key was removed; user settings must be preserved")
		}
	})

	t.Run("user value overwritten", func(t *testing.T) {
		t.Parallel()
		workspacePath := t.TempDir()
		settingsPath := claudeSettingsFixturePath(workspacePath)
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		existing := map[string]interface{}{
			"hooks":                       map[string]interface{}{},
			"autoLoadedSkillsDirectories": []interface{}{"/some/skills/dir"},
		}
		raw, err := json.Marshal(existing)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("MaterializeClaudeSettings (override): %v", err)
		}
		m := claudeSettingsFixtureReadJSON(t, settingsPath)
		assertAutoLoadedSkillsDisabled(t, m, "user non-empty value overwritten")
	})
}

func assertAutoLoadedSkillsDisabled(t *testing.T, m map[string]interface{}, ctx string) {
	t.Helper()
	raw, ok := m["autoLoadedSkillsDirectories"]
	if !ok {
		t.Errorf("%s: 'autoLoadedSkillsDirectories' key absent; MUST be present and empty", ctx)
		return
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Errorf("%s: 'autoLoadedSkillsDirectories' is not an array, got %T", ctx, raw)
		return
	}
	if len(arr) != 0 {
		t.Errorf("%s: 'autoLoadedSkillsDirectories' = %v; want empty []", ctx, arr)
	}
}

// TestDispatchConsentFix_NoPermissionsAllowInjected is the deterministic E2E
// canary for the dispatch-consent-fix (hk-5gmkd / HC-056). It materializes
// worktree settings through the real MaterializeClaudeSettings path — exactly
// as a bead run does — and asserts the on-disk settings.json contains NO
// harmonik-injected top-level permissions.allow block. A pre-approved
// permissions.allow in a git-worktree project settings is a consent gate that
// Claude Code >= 2.1.204 fires an interactive modal for, which
// --dangerously-skip-permissions does NOT bypass; SessionStart then never fires
// and the launch times out at agent_ready. The two sub-cases together cover
// both write sites: buildBridgeOnlySettings (fresh) and mergeSettingsWithBridge
// (existing user settings).
//
// Refs: hk-5gmkd, HC-056, #25.
func TestDispatchConsentFix_NoPermissionsAllowInjected(t *testing.T) {
	t.Parallel()

	t.Run("fresh_worktree_no_injected_allow", func(t *testing.T) {
		t.Parallel()
		workspacePath := t.TempDir()
		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("MaterializeClaudeSettings: %v", err)
		}
		m := claudeSettingsFixtureReadJSON(t, claudeSettingsFixturePath(workspacePath))
		if perm, ok := m["permissions"]; ok {
			t.Errorf("consent-gate canary: fresh settings.json has top-level 'permissions' = %v; "+
				"harmonik must not inject permissions.allow (would trip the consent modal, HC-056)", perm)
		}
	})

	t.Run("user_allow_preserved_not_augmented", func(t *testing.T) {
		t.Parallel()
		workspacePath := t.TempDir()

		userSettings := map[string]interface{}{
			"permissions": map[string]interface{}{
				"allow": []interface{}{"Read"},
			},
		}
		settingsPath := claudeSettingsFixturePath(workspacePath)
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		raw, err := json.Marshal(userSettings)
		if err != nil {
			t.Fatalf("marshal user settings: %v", err)
		}
		if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
			t.Fatalf("WriteFile user settings: %v", err)
		}

		if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
			t.Fatalf("MaterializeClaudeSettings (merge): %v", err)
		}

		m := claudeSettingsFixtureReadJSON(t, settingsPath)
		permMap, ok := m["permissions"].(map[string]interface{})
		if !ok {
			t.Fatalf("consent-gate canary: user 'permissions' object was dropped, got %T", m["permissions"])
		}
		allowArr, ok := permMap["allow"].([]interface{})
		if !ok {
			t.Fatalf("consent-gate canary: user 'permissions.allow' was dropped, got %T", permMap["allow"])
		}
		if len(allowArr) != 1 || allowArr[0] != "Read" {
			t.Errorf("consent-gate canary: user permissions.allow = %v; want [\"Read\"] preserved verbatim "+
				"(harmonik must not augment it)", allowArr)
		}
	})
}
