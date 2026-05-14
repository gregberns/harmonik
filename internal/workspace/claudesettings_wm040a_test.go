package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testDaemonBinaryPath is the fake absolute daemon binary path used by most
// tests (hk-kqdpf.6). Tests that need the real os.Executable() path use it
// directly.
const testDaemonBinaryPath = "/usr/local/bin/harmonik-test"

// claudeSettingsFixturePath returns the canonical settings path for a
// workspace at workspacePath (helper for tests).
func claudeSettingsFixturePath(workspacePath string) string {
	return ClaudeSettingsPath(workspacePath)
}

// claudeSettingsFixtureReadJSON reads the settings.json at path and
// unmarshals it into a map, failing the test on error.
func claudeSettingsFixtureReadJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	//nolint:gosec // G304: path is a test-controlled temp path
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("claudeSettingsFixtureReadJSON: ReadFile %q: %v", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("claudeSettingsFixtureReadJSON: Unmarshal: %v", err)
	}
	return m
}

// claudeSettingsFixtureHooksMap extracts the top-level "hooks" object from m,
// failing the test if absent or wrong type.
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

// claudeSettingsFixtureHookEntries extracts the hooks array for eventKind
// from hooksMap.
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

// claudeSettingsFixtureBridgeGroupPresent reports whether arr contains
// the bridge matcher-group for eventKind per CHB-003, checking that the hook
// "command" field matches wantCommand (hk-kqdpf.6: must be an absolute path).
func claudeSettingsFixtureBridgeGroupPresent(arr []interface{}, eventKind, wantCommand string) bool {
	for _, elem := range arr {
		m, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}
		matcher, _ := m["matcher"].(string)
		if matcher != "" {
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

	// Assert: settings.json exists.
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("WM-040a: settings.json not on disk: %v", err)
	}

	// Assert: no .tmp-* orphan present.
	entries, err := os.ReadDir(filepath.Dir(settingsPath))
	if err != nil {
		t.Fatalf("WM-040a: ReadDir .claude/: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("WM-040a: orphan tmp file after clean materialization: %q", e.Name())
		}
	}

	// Assert: valid JSON with all five bridge event-kinds.
	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a: bridge group missing for event kind %q", kind)
		}
	}

	// Assert: no disableAllHooks key.
	if _, ok := m["disableAllHooks"]; ok {
		t.Errorf("WM-040a: disableAllHooks key present in output; MUST be stripped")
	}

	// Assert: CHB-005 — .claude/settings.json in worktree .gitignore.
	gitignorePath := filepath.Join(workspacePath, ".gitignore")
	//nolint:gosec // G304: controlled test path
	giData, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("WM-040a: .gitignore not created by CHB-005 hygiene: %v", err)
	}
	if !gitignoreLinePresent(string(giData), ClaudeSettingsWorktreeGitignoreLine) {
		t.Errorf("WM-040a: CHB-005: .gitignore missing %q", ClaudeSettingsWorktreeGitignoreLine)
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

	// Write a pre-existing settings.json with user hooks for two of the five
	// event-kinds plus one unrelated key.
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}
	raw, err := json.Marshal(userHooks)
	if err != nil {
		t.Fatalf("WM-040a: marshal user hooks: %v", err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("WM-040a: WriteFile user settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (merge): %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)

	// Assert: bridge groups present for ALL five event-kinds.
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a merge: bridge group missing for %q", kind)
		}
	}

	// Assert: user hook for SessionStart is preserved (len >= 2: user + bridge).
	sessionStartArr := claudeSettingsFixtureHookEntries(t, hooks, "SessionStart")
	if len(sessionStartArr) < 2 {
		t.Errorf("WM-040a merge: SessionStart array len = %d; want >= 2 (user + bridge)", len(sessionStartArr))
	}

	// Assert: user hook for Stop is preserved.
	stopArr := claudeSettingsFixtureHookEntries(t, hooks, "Stop")
	if len(stopArr) < 2 {
		t.Errorf("WM-040a merge: Stop array len = %d; want >= 2 (user + bridge)", len(stopArr))
	}

	// Assert: unrelated key "theme" is preserved.
	if _, ok := m["theme"]; !ok {
		t.Errorf("WM-040a merge: 'theme' key was removed; user settings must be preserved")
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}
	// Write deliberately malformed JSON.
	if err := os.WriteFile(settingsPath, []byte(`{bad json`), 0o644); err != nil {
		t.Fatalf("WM-040a: WriteFile malformed: %v", err)
	}

	// Session log path for warning capture.
	sessionLogPath := filepath.Join(t.TempDir(), "session.log")

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, sessionLogPath); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (malformed): %v", err)
	}

	// Assert: settings.json is now valid JSON with bridge entries.
	m := claudeSettingsFixtureReadJSON(t, settingsPath)
	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a malformed overwrite: bridge group missing for %q after overwrite", kind)
		}
	}

	// Assert: warning line was written to session log.
	//nolint:gosec // G304: controlled test path
	logData, err := os.ReadFile(sessionLogPath)
	if err != nil {
		t.Fatalf("WM-040a: ReadFile session log: %v", err)
	}
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("WM-040a: MkdirAll: %v", err)
	}

	// Write settings with disableAllHooks: true.
	userSettings := map[string]interface{}{
		"disableAllHooks": true,
		"hooks":           map[string]interface{}{},
	}
	raw, _ := json.Marshal(userSettings)
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatalf("WM-040a: WriteFile disableAllHooks settings: %v", err)
	}

	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a: MaterializeClaudeSettings (disableAllHooks): %v", err)
	}

	m := claudeSettingsFixtureReadJSON(t, settingsPath)

	// Assert: disableAllHooks is absent.
	if _, ok := m["disableAllHooks"]; ok {
		t.Errorf("WM-040a: disableAllHooks present in merged result; MUST be stripped")
	}

	// Assert: bridge hooks are present despite disableAllHooks having been set.
	hooks := claudeSettingsFixtureHooksMap(t, m)
	for _, kind := range bridgeEventKinds {
		arr := claudeSettingsFixtureHookEntries(t, hooks, kind)
		if !claudeSettingsFixtureBridgeGroupPresent(arr, kind, testDaemonBinaryPath) {
			t.Errorf("WM-040a: bridge group missing for %q after disableAllHooks strip", kind)
		}
	}
}

// TestWM040a_GitignoreIdempotent verifies that calling MaterializeClaudeSettings
// twice does not duplicate the .claude/settings.json line in the worktree
// .gitignore per CHB-005.
//
// Spec ref: claude-hook-bridge.md CHB-005 (idempotency).
func TestWM040a_GitignoreIdempotent(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()

	// First call.
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a gitignore idempotent: first call: %v", err)
	}
	// Second call.
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("WM-040a gitignore idempotent: second call: %v", err)
	}

	gitignorePath := filepath.Join(workspacePath, ".gitignore")
	//nolint:gosec // G304: controlled test path
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("WM-040a gitignore idempotent: ReadFile .gitignore: %v", err)
	}

	// Count occurrences of the line.
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ClaudeSettingsWorktreeGitignoreLine {
			count++
		}
	}
	if count != 1 {
		t.Errorf("WM-040a gitignore idempotent: line %q appears %d times in .gitignore; want exactly 1",
			ClaudeSettingsWorktreeGitignoreLine, count)
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
	// mtime must not be before the start of the call.
	if mtime.Before(before) {
		t.Errorf("WM-040a ordering: settings.json mtime %v is before call start %v; fsync ordering violated", mtime, before)
	}
	_ = after // after is an upper bound; not checked (mtime <= after is trivially true on local fs)

	// Conceptual gate: workspace_leased would emit here.
	// The file MUST be readable at this point.
	//nolint:gosec // G304: controlled test path
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("WM-040a ordering: ReadFile settings.json: %v", err)
	}
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

		// Find the bridge group (matcher == "").
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

			// Validate fields.
			if h["type"] != "command" {
				t.Errorf("CHB-003: %q hook type = %q; want \"command\"", kind, h["type"])
			}
			// hk-kqdpf.6: command MUST be the absolute daemon binary path, not bare "harmonik".
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
			// timeout arrives as float64 from JSON unmarshal.
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

	// Assert all five required event kinds are present in the hooks object.
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

	// The canonical file must exist.
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

	// Use the real test binary path so this test exercises the os.Executable()
	// contract used by production main.go.
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

	// Verify the command is not the bare "harmonik" name — the regression we are fixing.
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
			cmd, _ := h["command"].(string)
			if cmd == "harmonik" {
				t.Errorf("hk-kqdpf.6 regression: hook command for %q is bare \"harmonik\"; must be absolute path", kind)
			}
		}
	}
}

// TestWM040a_PermissionsAllowPresent verifies that MaterializeClaudeSettings
// writes a "permissions.allow" array containing the standard harmonik tool set
// per workspace-model.md §4.7a WM-040a (hk-53y35 amendment).
//
// Per-tool permission dialogs block unattended daemon operation; pre-authorizing
// the standard Claude Code tool set via settings.json is the spec-compliant
// alternative to the deny-listed --dangerously-skip-permissions flag.
//
// Spec ref: workspace-model.md §4.7a WM-040a; claude-hook-bridge.md §4.2 CHB-007.
// Bead: hk-53y35.
func TestWM040a_PermissionsAllowPresent(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	if err := MaterializeClaudeSettings(workspacePath, testDaemonBinaryPath, ""); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPresent: MaterializeClaudeSettings: %v", err)
	}

	settingsPath := claudeSettingsFixturePath(workspacePath)
	m := claudeSettingsFixtureReadJSON(t, settingsPath)

	// Assert: top-level "permissions" key is present and is an object.
	permRaw, ok := m["permissions"]
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPresent: no top-level 'permissions' key in settings.json")
	}
	permMap, ok := permRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPresent: 'permissions' is not an object, got %T", permRaw)
	}

	// Assert: permissions.allow is present and is an array.
	allowRaw, ok := permMap["allow"]
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPresent: 'permissions.allow' key absent")
	}
	allowArr, ok := allowRaw.([]interface{})
	if !ok {
		t.Fatalf("TestWM040a_PermissionsAllowPresent: 'permissions.allow' is not an array, got %T", allowRaw)
	}

	// Assert: each expected tool appears in the array.
	wantTools := harmonikAllowedTools
	allowSet := make(map[string]bool, len(allowArr))
	for _, v := range allowArr {
		if s, ok := v.(string); ok {
			allowSet[s] = true
		}
	}
	for _, want := range wantTools {
		wantStr, ok2 := want.(string)
		if !ok2 {
			continue
		}
		if !allowSet[wantStr] {
			t.Errorf("TestWM040a_PermissionsAllowPresent: tool %q absent from permissions.allow", wantStr)
		}
	}

	// Assert: dangerouslySkipPermissions is NOT present (spec-forbidden, CHB-007).
	if _, ok := m["dangerouslySkipPermissions"]; ok {
		t.Errorf("TestWM040a_PermissionsAllowPresent: dangerouslySkipPermissions present; must not be set (CHB-007 deny-list)")
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("TestWM040a_PermissionsAllowPreservedOnMerge: MkdirAll: %v", err)
	}

	// Write settings with a user-defined permissions.allow list.
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
	//nolint:gosec // G306: 0644 matches existing test fixture conventions in this file
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
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
	// The user's "MyCustomTool" must still be present.
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
