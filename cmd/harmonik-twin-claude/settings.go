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

type settingsHookEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"`
}

type settingsMatcherGroup struct {
	Matcher string              `json:"matcher"`
	Hooks   []settingsHookEntry `json:"hooks"`
}

type rawSettings struct {
	// DangerouslyAllowedPermissions is tested for presence via a custom
	// json.RawMessage field: when the key is absent json.RawMessage is nil;
	// when present (even if null) it is non-nil.
	DangerouslyAllowedPermissions json.RawMessage `json:"dangerouslyAllowedPermissions"`

	// Hooks is the hooks map: event name → []settingsMatcherGroup.
	Hooks map[string][]settingsMatcherGroup `json:"hooks"`
}

func loadCloneSettings(worktreePath string) (*cloneSettings, error) {
	settingsPath := filepath.Join(worktreePath, ".claude", "settings.json")

	//nolint:gosec // G304: path is operator-supplied via --worktree-path flag; provenance is the daemon's worktree path
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &cloneSettings{}, nil
		}
		return nil, fmt.Errorf("loadCloneSettings: read %q: %w", settingsPath, err)
	}

	var rs rawSettings
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("loadCloneSettings: parse %q: %w", settingsPath, err)
	}

	cs := &cloneSettings{}

	cs.permissionsPresent = rs.DangerouslyAllowedPermissions != nil

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
