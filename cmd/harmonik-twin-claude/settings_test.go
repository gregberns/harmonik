package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Test helpers use the per-bead prefix declared in implementer-protocol.md:
// twinSettingsFixture (bead hk-e66ht).

// twinSettingsFixtureDir creates a temp dir with a .claude subdirectory and
// returns the "worktree root" path.
func twinSettingsFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("twinSettingsFixtureDir: mkdir: %v", err)
	}
	return root
}

// twinSettingsFixtureWrite writes raw JSON bytes to
// <root>/.claude/settings.json and returns the worktree root.
func twinSettingsFixtureWrite(t *testing.T, root string, content []byte) {
	t.Helper()
	p := filepath.Join(root, ".claude", "settings.json")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("twinSettingsFixtureWrite: write %q: %v", p, err)
	}
}

// TestLoadCloneSettings_Absent verifies that a missing settings.json returns
// a zero-value cloneSettings with no error (both flags false).
func TestLoadCloneSettings_Absent(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	// Do not write a settings.json file.

	cs, err := loadCloneSettings(root)
	if err != nil {
		t.Fatalf("loadCloneSettings absent: unexpected error: %v", err)
	}
	if cs.permissionsPresent {
		t.Error("permissionsPresent = true, want false for absent settings.json")
	}
	if cs.stopHookPresent {
		t.Error("stopHookPresent = true, want false for absent settings.json")
	}
	if cs.stopHookCommand != "" {
		t.Errorf("stopHookCommand = %q, want empty for absent settings.json", cs.stopHookCommand)
	}
}

// TestLoadCloneSettings_MalformedJSON verifies that malformed JSON returns a
// non-nil error (caller emits error wire + exits 1 per bead error policy).
func TestLoadCloneSettings_MalformedJSON(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	twinSettingsFixtureWrite(t, root, []byte("{ this is not json }"))

	_, err := loadCloneSettings(root)
	if err == nil {
		t.Fatal("loadCloneSettings malformed: expected error, got nil")
	}
}

// TestLoadCloneSettings_ValidNoHooks verifies that valid JSON with no hooks
// section returns both flags false but no error.
func TestLoadCloneSettings_ValidNoHooks(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	content, _ := json.Marshal(map[string]any{
		"dangerouslyAllowedPermissions": []string{"Bash(*)"},
	})
	twinSettingsFixtureWrite(t, root, content)

	cs, err := loadCloneSettings(root)
	if err != nil {
		t.Fatalf("loadCloneSettings valid no hooks: unexpected error: %v", err)
	}
	if !cs.permissionsPresent {
		t.Error("permissionsPresent = false, want true (key present in JSON)")
	}
	if cs.stopHookPresent {
		t.Error("stopHookPresent = true, want false (no hooks section)")
	}
}

// TestLoadCloneSettings_ValidWithStopHook verifies that a settings.json
// containing a Stop hook produces stopHookPresent=true and the correct command.
func TestLoadCloneSettings_ValidWithStopHook(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	// Build a minimal settings.json matching CHB-003 shape.
	content, _ := json.Marshal(map[string]any{
		"dangerouslyAllowedPermissions": []string{"Bash(*)", "Read(*)"},
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "harmonik",
							"args":    []string{"hook-relay", "Stop"},
							"timeout": 30,
						},
					},
				},
			},
		},
	})
	twinSettingsFixtureWrite(t, root, content)

	cs, err := loadCloneSettings(root)
	if err != nil {
		t.Fatalf("loadCloneSettings valid with stop hook: unexpected error: %v", err)
	}
	if !cs.permissionsPresent {
		t.Error("permissionsPresent = false, want true")
	}
	if !cs.stopHookPresent {
		t.Error("stopHookPresent = false, want true")
	}
	if cs.stopHookCommand != "harmonik" {
		t.Errorf("stopHookCommand = %q, want %q", cs.stopHookCommand, "harmonik")
	}
}

// TestLoadCloneSettings_MissingStopHook verifies that a settings.json with
// dangerouslyAllowedPermissions but an empty hooks map returns stopHookPresent=false.
func TestLoadCloneSettings_MissingStopHook(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	content, _ := json.Marshal(map[string]any{
		"dangerouslyAllowedPermissions": []any{},
		"hooks":                         map[string]any{},
	})
	twinSettingsFixtureWrite(t, root, content)

	cs, err := loadCloneSettings(root)
	if err != nil {
		t.Fatalf("loadCloneSettings missing stop hook: unexpected error: %v", err)
	}
	if !cs.permissionsPresent {
		t.Error("permissionsPresent = false, want true (key present)")
	}
	if cs.stopHookPresent {
		t.Error("stopHookPresent = true, want false (no Stop entry in hooks)")
	}
}

// TestLoadCloneSettings_PermissionsKeyPresentNull verifies that
// dangerouslyAllowedPermissions: null counts as present.
func TestLoadCloneSettings_PermissionsKeyPresentNull(t *testing.T) {
	root := twinSettingsFixtureDir(t)
	// Write raw JSON with explicit null value for dangerouslyAllowedPermissions.
	twinSettingsFixtureWrite(t, root, []byte(`{"dangerouslyAllowedPermissions": null}`))

	cs, err := loadCloneSettings(root)
	if err != nil {
		t.Fatalf("loadCloneSettings null permissions: unexpected error: %v", err)
	}
	// json.RawMessage is non-nil when key is present even if value is null.
	if !cs.permissionsPresent {
		t.Error("permissionsPresent = false, want true (key present even though value is null)")
	}
}
