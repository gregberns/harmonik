// Settings parser for harmonik-twin-claude (hk-e66ht).
//
// Reads the worktree's .claude/settings.json at startup to extract:
//   - dangerouslyAllowedPermissions presence (Fix 9, §4 audit item 1).
//   - hooks.Stop command for Stop hook execution (Fix 11b, §4 audit item 2).
//
// Cite: docs/twin-parity-audit-2026-05-14.md §4 items 1+2;
// specs/claude-hook-bridge.md §4.1.CHB-001 through CHB-003.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// twinSettingsFixture — per-bead helper prefix for test helpers in this file.
// (Actual test helpers are in settings_test.go; prefix declared here as a
// godoc anchor per implementer-protocol.md §Helper-prefix discipline.)

// cloneSettings holds the minimal subset of .claude/settings.json that the
// twin needs to read. All other fields are ignored.
type cloneSettings struct {
	// permissionsPresent is true when the settings.json contained a
	// dangerouslyAllowedPermissions key (presence is enough; value not interpreted).
	permissionsPresent bool

	// stopHookPresent is true when at least one Stop hook entry with a non-empty
	// command was found.
	stopHookPresent bool

	// stopHookCommand is the executable command string for the first Stop hook
	// found (hooks[0].command per CHB-003). Empty when stopHookPresent is false.
	stopHookCommand string
}

// settingsHookEntry is the inner hook object shape per CHB-003:
//
//	{ "type": "command", "command": "harmonik", "args": [...], "timeout": 30 }
type settingsHookEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"`
}

// settingsMatcherGroup is one entry in the event's hook array per CHB-003:
//
//	{ "matcher": "", "hooks": [...] }
type settingsMatcherGroup struct {
	Matcher string              `json:"matcher"`
	Hooks   []settingsHookEntry `json:"hooks"`
}

// rawSettings is the minimal JSON shape of .claude/settings.json that the
// twin parses. Unknown top-level fields are silently ignored by encoding/json.
type rawSettings struct {
	// DangerouslyAllowedPermissions is tested for presence via a custom
	// json.RawMessage field: when the key is absent json.RawMessage is nil;
	// when present (even if null) it is non-nil.
	DangerouslyAllowedPermissions json.RawMessage `json:"dangerouslyAllowedPermissions"`

	// Hooks is the hooks map: event name → []settingsMatcherGroup.
	Hooks map[string][]settingsMatcherGroup `json:"hooks"`
}

// loadCloneSettings reads .claude/settings.json from worktreePath.
//
// Behaviour matrix (per bead body §Error handling):
//   - File absent: returns valid cloneSettings with both flags false, no error.
//   - Malformed JSON: returns error (caller emits error wire + exits 1).
//   - Valid JSON: parses fields; returns populated cloneSettings.
//
// The caller (main.go) is responsible for emitting the twin_settings_loaded
// wire message after this call.
func loadCloneSettings(worktreePath string) (*cloneSettings, error) {
	settingsPath := filepath.Join(worktreePath, ".claude", "settings.json")

	//nolint:gosec // G304: path is operator-supplied via --worktree-path flag; provenance is the daemon's worktree path
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Absent settings.json is normal: workflows may not need hooks.
			// Return both-false with no error per bead error policy.
			return &cloneSettings{}, nil
		}
		return nil, fmt.Errorf("loadCloneSettings: read %q: %w", settingsPath, err)
	}

	var rs rawSettings
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("loadCloneSettings: parse %q: %w", settingsPath, err)
	}

	cs := &cloneSettings{}

	// Permissions presence: DangerouslyAllowedPermissions is non-nil when the
	// key is present in the JSON object, regardless of value.
	cs.permissionsPresent = rs.DangerouslyAllowedPermissions != nil

	// Stop hook: scan the Stop event's matcher groups for the first valid command.
	if stopGroups, ok := rs.Hooks["Stop"]; ok {
		for _, group := range stopGroups {
			for _, entry := range group.Hooks {
				if entry.Command != "" {
					cs.stopHookPresent = true
					cs.stopHookCommand = entry.Command
					break
				}
			}
			if cs.stopHookPresent {
				break
			}
		}
	}

	return cs, nil
}
