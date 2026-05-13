package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeSettingsPath returns the canonical path for the Claude Code settings
// file materialized into a workspace, per workspace-model.md §4.7a WM-040a
// and claude-hook-bridge.md §4.1 CHB-001.
//
// Path: ${workspace_path}/.claude/settings.json
func ClaudeSettingsPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".claude", "settings.json")
}

// ClaudeSettingsWorktreeGitignoreLine is the gitignore line the workspace
// manager MUST add to the worktree's .gitignore when materializing the
// settings file, per workspace-model.md §4.3 WM-013e and CHB-005.
const ClaudeSettingsWorktreeGitignoreLine = ".claude/settings.json"

// bridgeHookEntry is the canonical shape of a single hook entry as declared
// in claude-hook-bridge.md §4.1 CHB-003.
type bridgeHookEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"`
}

// bridgeMatcherGroup is the wrapper object per event-type array element
// per CHB-003.
type bridgeMatcherGroup struct {
	Matcher string            `json:"matcher"`
	Hooks   []bridgeHookEntry `json:"hooks"`
}

// bridgeEventKinds is the ordered set of Claude hook event-kinds the bridge
// MUST declare per CHB-003.
var bridgeEventKinds = []string{
	"SessionStart",
	"Stop",
	"SessionEnd",
	"StopFailure",
	"Notification",
}

// bridgeMatcherGroupFor returns the single bridge matcher-group for eventKind.
func bridgeMatcherGroupFor(eventKind string) bridgeMatcherGroup {
	return bridgeMatcherGroup{
		Matcher: "",
		Hooks: []bridgeHookEntry{
			{
				Type:    "command",
				Command: "harmonik",
				Args:    []string{"hook-relay", eventKind},
				Timeout: 30,
			},
		},
	}
}

// MaterializeClaudeSettings writes the claude-code hook-bridge settings file
// to ${workspace_path}/.claude/settings.json, applying the merge / overwrite
// / disableAllHooks-strip semantics of CHB-004, and then ensures the worktree's
// .gitignore contains the .claude/settings.json line per CHB-005.
//
// Ordering obligation (CHB-002 / WM-040a): this function MUST be called AFTER
// git worktree add (WM-003) and the parent-dir fsync MUST complete BEFORE
// workspace_leased emits. The caller owns that sequencing.
//
// # Merge semantics (CHB-004)
//
// If ${workspace_path}/.claude/settings.json already exists:
//   - Valid JSON → append bridge matcher-group to each event-type's hooks array.
//   - Malformed JSON → overwrite with bridge-only content; log a warning line
//     to sessionLogPath so the operator knows the file was displaced.
//
// If the file does not exist, a fresh file containing only the bridge-required
// entries is written.
//
// In all cases, a top-level "disableAllHooks": true key is removed from the
// merged result before writing (CHB-004 requirement).
//
// # Gitignore hygiene (CHB-005)
//
// After writing the settings file, MaterializeClaudeSettings ensures
// ".claude/settings.json" is present in the worktree's .gitignore
// (${workspace_path}/.gitignore), adding it if absent. The worktree
// .gitignore write is simple append-only (no git commit required — the
// worktree is a task branch and the daemon owns its commit sequence).
//
// # Atomic write (WM-026 / CHB-002)
//
// The settings.json write follows the WM-026 discipline:
//  1. Write JSON to ${settings_path}.tmp-<pid>
//  2. fsync the temp file
//  3. rename(2) to canonical path (POSIX atomic)
//  4. fsync the parent directory
//
// # Parameters
//
//   - workspacePath: absolute path to the worktree root (${workspace_path}).
//   - sessionLogPath: absolute path to the session-log file for warning lines
//     (used only on malformed-JSON overwrite per CHB-004). May be "" to skip
//     the warning write (tests or callers that have not yet created the log).
//
// Spec refs:
//   - workspace-model.md §4.7a WM-040a — materialization obligation.
//   - claude-hook-bridge.md §4.1 CHB-001..005 — hook entries, merge, gitignore.
//   - workspace-model.md §4.7 WM-026 — atomic-write discipline.
//   - workspace-model.md §4.3 WM-013e — gitignore hygiene (worktree scope).
func MaterializeClaudeSettings(workspacePath, sessionLogPath string) error {
	settingsPath := ClaudeSettingsPath(workspacePath)

	// Ensure the .claude/ parent directory exists.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: MkdirAll .claude/: %w", err)
	}

	// Attempt to read an existing settings.json.
	existing, readErr := os.ReadFile(settingsPath) //nolint:gosec // G304: path constructed from workspacePath + canonical suffix

	var merged map[string]interface{}
	var overwrote bool

	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: ReadFile: %w", readErr)
	}

	if readErr == nil && len(existing) > 0 {
		// File exists — attempt JSON merge.
		var parsed map[string]interface{}
		if jsonErr := json.Unmarshal(existing, &parsed); jsonErr != nil {
			// Malformed JSON — overwrite path per CHB-004.
			merged = buildBridgeOnlySettings()
			overwrote = true
			if sessionLogPath != "" {
				warnLine := fmt.Sprintf("[workspace-manager WARNING] WM-040a/CHB-004: %s was malformed JSON; overwritten with bridge-required content. Original parse error: %v\n",
					settingsPath, jsonErr)
				if logErr := appendToFile(sessionLogPath, warnLine); logErr != nil {
					// Non-fatal; log write failure is observable but must not block materialization.
					_ = logErr
				}
			}
		} else {
			merged = mergeSettingsWithBridge(parsed)
		}
	} else {
		// File absent — write fresh bridge-only content.
		merged = buildBridgeOnlySettings()
	}

	// Strip disableAllHooks: true per CHB-004.
	delete(merged, "disableAllHooks")

	// Serialize the merged result.
	content, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: MarshalIndent: %w", err)
	}
	content = append(content, '\n')

	// Atomic write per WM-026.
	if err := atomicWriteWithParentFsync(settingsPath, content); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: atomic write: %w", err)
	}

	_ = overwrote // consumed via sessionLogPath warning above

	// CHB-005: ensure .claude/settings.json is in the worktree's .gitignore.
	if err := ensureWorktreeGitignore(workspacePath, ClaudeSettingsWorktreeGitignoreLine); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: gitignore hygiene: %w", err)
	}

	return nil
}

// buildBridgeOnlySettings returns a settings map containing only the
// bridge-required hook entries per CHB-003.
func buildBridgeOnlySettings() map[string]interface{} {
	hooks := make(map[string]interface{}, len(bridgeEventKinds))
	for _, kind := range bridgeEventKinds {
		hooks[kind] = []interface{}{groupToInterface(bridgeMatcherGroupFor(kind))}
	}
	return map[string]interface{}{
		"hooks": hooks,
	}
}

// mergeSettingsWithBridge appends bridge matcher-groups to each event-type
// array in existing, per CHB-004: user hooks continue to fire alongside.
func mergeSettingsWithBridge(existing map[string]interface{}) map[string]interface{} {
	// Clone top-level so we don't mutate the caller's map.
	merged := make(map[string]interface{}, len(existing))
	for k, v := range existing {
		merged[k] = v
	}

	// Ensure a "hooks" top-level key exists.
	hooksRaw, ok := merged["hooks"]
	if !ok || hooksRaw == nil {
		hooksRaw = map[string]interface{}{}
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		// Unexpected shape — treat as absent; replace with bridge-only.
		hooksMap = make(map[string]interface{})
	}

	// Append the bridge matcher-group to each event-type array.
	for _, kind := range bridgeEventKinds {
		bridgeGroup := groupToInterface(bridgeMatcherGroupFor(kind))
		existing, exists := hooksMap[kind]
		if !exists || existing == nil {
			hooksMap[kind] = []interface{}{bridgeGroup}
			continue
		}
		arr, ok := existing.([]interface{})
		if !ok {
			// Unexpected element type — start fresh with bridge-only for this kind.
			hooksMap[kind] = []interface{}{bridgeGroup}
			continue
		}
		hooksMap[kind] = append(arr, bridgeGroup)
	}

	merged["hooks"] = hooksMap
	return merged
}

// groupToInterface converts a bridgeMatcherGroup to the interface{} shape
// that json.MarshalIndent will encode correctly.
func groupToInterface(g bridgeMatcherGroup) interface{} {
	entries := make([]interface{}, len(g.Hooks))
	for i, h := range g.Hooks {
		entries[i] = map[string]interface{}{
			"type":    h.Type,
			"command": h.Command,
			"args":    stringsToInterface(h.Args),
			"timeout": h.Timeout,
		}
	}
	return map[string]interface{}{
		"matcher": g.Matcher,
		"hooks":   entries,
	}
}

// stringsToInterface converts []string to []interface{} for JSON encoding.
func stringsToInterface(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// atomicWriteWithParentFsync implements the WM-026 atomic-write discipline:
//
//  1. Write content to ${path}.tmp-<pid>
//  2. fsync the temp file
//  3. rename(2) to canonical path
//  4. fsync the parent directory
func atomicWriteWithParentFsync(path string, content []byte) error {
	pid := os.Getpid()
	tmpPath := fmt.Sprintf("%s.tmp-%d", path, pid)

	// (1) Write to temp file.
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write tmp: %w", err)
	}
	// (2) fsync temp file.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close tmp: %w", err)
	}

	// (3) Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	// (4) fsync the parent directory.
	parentDir := filepath.Dir(path)
	d, err := os.Open(parentDir) //nolint:gosec // G304: parentDir is derived from caller-provided path, not user input
	if err != nil {
		return fmt.Errorf("open parent dir: %w", err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("fsync parent dir: %w", err)
	}
	return d.Close()
}

// ensureWorktreeGitignore ensures that line appears in
// ${workspacePath}/.gitignore, appending it if absent.
// The worktree .gitignore (not the repo root .gitignore) is a plain file
// owned by git inside the task branch; we append without a git commit because
// the daemon manages task-branch commits through its own checkpoint sequence.
func ensureWorktreeGitignore(workspacePath, line string) error {
	gitignorePath := filepath.Join(workspacePath, ".gitignore")

	existing := ""
	//nolint:gosec // G304: path is constructed from workspacePath + ".gitignore", not user input
	data, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ReadFile worktree .gitignore: %w", err)
	}
	if err == nil {
		existing = string(data)
	}

	if gitignoreLinePresent(existing, line) {
		return nil // idempotent
	}

	// Append the line.
	var sb strings.Builder
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(line)
	sb.WriteString("\n")

	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("OpenFile worktree .gitignore: %w", err)
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("WriteString worktree .gitignore: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("Sync worktree .gitignore: %w", err)
	}
	return f.Close()
}

// gitignoreLinePresent reports whether line appears on its own line in content.
func gitignoreLinePresent(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// appendToFile appends text to path, creating the file if absent.
// Used for the CHB-004 overwrite-warning write to the session log.
func appendToFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("appendToFile OpenFile %q: %w", path, err)
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return fmt.Errorf("appendToFile WriteString %q: %w", path, err)
	}
	return f.Close()
}
