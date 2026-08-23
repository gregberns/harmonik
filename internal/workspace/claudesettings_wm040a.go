package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

type bridgeHookEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Timeout int      `json:"timeout"`
}

type bridgeMatcherGroup struct {
	Matcher string            `json:"matcher"`
	Hooks   []bridgeHookEntry `json:"hooks"`
}

var bridgeEventKinds = []string{
	"SessionStart",
	"Stop",
	"SessionEnd",
	"StopFailure",
	"Notification",
}

const hookRelayVerb = "hook-relay"

func bridgeMatcherGroupFor(eventKind, daemonBinaryPath string) bridgeMatcherGroup {
	return bridgeMatcherGroup{
		Matcher: "",
		Hooks: []bridgeHookEntry{
			{
				Type:    "command",
				Command: daemonBinaryPath,
				Args:    []string{hookRelayVerb, eventKind},
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
//   - Valid JSON → remove harmonik's own hook entries from each event-type's
//     hooks array, then add the current bridge matcher-group. Hook entries
//     harmonik did not write stay where they are and continue to fire.
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
// MaterializeClaudeSettings does NOT mutate any .gitignore (hk-jvzc2). The
// CHB-005 hygiene rule is now an operator-setup obligation: the parent repo's
// root .gitignore MUST cover .claude/settings.json before the daemon runs.
// Earlier revisions appended the entry to the worktree .gitignore per launch;
// that silent edit surfaced as uncommitted churn in the parent repo's working
// tree across dogfood runs (hk-cd92e, hk-jvzc2).
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
//   - daemonBinaryPath: absolute path to the running harmonik binary, resolved
//     via os.Executable() at daemon startup. Used as the hook "command" field so
//     the relay subprocess can be found regardless of the tmux window's $PATH
//     (hk-kqdpf.6). MUST be non-empty; callers MUST fail fast at daemon start if
//     os.Executable() errors.
//   - sessionLogPath: absolute path to the session-log file for warning lines
//     (used only on malformed-JSON overwrite per CHB-004). May be "" to skip
//     the warning write (tests or callers that have not yet created the log).
//
// Spec refs:
//   - workspace-model.md §4.7a WM-040a — materialization obligation.
//   - claude-hook-bridge.md §4.1 CHB-001..005 — hook entries, merge, gitignore.
//   - workspace-model.md §4.7 WM-026 — atomic-write discipline.
//   - workspace-model.md §4.3 WM-013e — gitignore hygiene (worktree scope).
func MaterializeClaudeSettings(workspacePath, daemonBinaryPath, sessionLogPath string) error {
	settingsPath := ClaudeSettingsPath(workspacePath)

	// Ensure the .claude/ parent directory exists. This is the .claude/ tree,
	// NOT .harmonik/, so it deliberately does not use core.HarmonikDirMode:
	// `harmonik init`'s provisionSkills and sync-assets' writeFileEnsureDir both
	// create .claude/ at 0o755, and tightening only this creator would open the
	// same first-creator-wins split on the .claude side that the constant exists
	// to close on the .harmonik side.
	//nolint:gosec // G301: 0755 matches the .claude/ dir conventions
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil { //dirmode:allow not a .harmonik state dir: .claude/ is owned by the claude asset tree at 0o755
		return fmt.Errorf("workspace: MaterializeClaudeSettings: MkdirAll .claude/: %w", err)
	}

	existing, readErr := os.ReadFile(settingsPath) //nolint:gosec // G304: path constructed from workspacePath + canonical suffix

	var merged map[string]interface{}
	var overwrote bool

	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: ReadFile: %w", readErr)
	}

	if readErr == nil && len(existing) > 0 {
		var parsed map[string]interface{}
		if jsonErr := json.Unmarshal(existing, &parsed); jsonErr != nil {
			merged = buildBridgeOnlySettings(daemonBinaryPath)
			overwrote = true
			if sessionLogPath != "" {
				warnLine := fmt.Sprintf("[workspace-manager WARNING] WM-040a/CHB-004: %s was malformed JSON; overwritten with bridge-required content. Original parse error: %v\n",
					settingsPath, jsonErr)
				if logErr := appendToFile(sessionLogPath, warnLine); logErr != nil {
					_ = logErr
				}
			}
		} else {
			merged = mergeSettingsWithBridge(parsed, daemonBinaryPath)
		}
	} else {
		merged = buildBridgeOnlySettings(daemonBinaryPath)
	}

	delete(merged, "disableAllHooks")

	content, err := marshalSettings(merged)
	if err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: MarshalIndent: %w", err)
	}

	if err := atomicWriteWithParentFsync(settingsPath, content); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: atomic write: %w", err)
	}

	_ = overwrote // consumed via sessionLogPath warning above

	return nil
}

func marshalSettings(merged map[string]interface{}) ([]byte, error) {
	content, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func buildBridgeOnlySettings(daemonBinaryPath string) map[string]interface{} {
	hooks := make(map[string]interface{}, len(bridgeEventKinds))
	for _, kind := range bridgeEventKinds {
		hooks[kind] = []interface{}{groupToInterface(bridgeMatcherGroupFor(kind, daemonBinaryPath))}
	}
	return map[string]interface{}{
		"hooks": hooks,
		// Disable Claude Code's default skill autoload from ancestor .claude/skills/
		// directories so worker agents only see skills explicitly requested via their
		// manifest context[]. Fleet orchestration skills must not leak into implementer
		// or reviewer panes. Empty array = zero auto-loaded directories (T6/hk-j79ny).
		"autoLoadedSkillsDirectories": []interface{}{},
	}
}

func mergeSettingsWithBridge(existing map[string]interface{}, daemonBinaryPath string) map[string]interface{} {
	merged := make(map[string]interface{}, len(existing))
	for k, v := range existing {
		merged[k] = v
	}

	hooksRaw, ok := merged["hooks"]
	if !ok || hooksRaw == nil {
		hooksRaw = map[string]interface{}{}
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		hooksMap = make(map[string]interface{})
	}

	for _, kind := range bridgeEventKinds {
		bridgeGroup := groupToInterface(bridgeMatcherGroupFor(kind, daemonBinaryPath))
		existing, exists := hooksMap[kind]
		if !exists || existing == nil {
			hooksMap[kind] = []interface{}{bridgeGroup}
			continue
		}
		arr, ok := existing.([]interface{})
		if !ok {
			hooksMap[kind] = []interface{}{bridgeGroup}
			continue
		}
		kept := make([]interface{}, 0, len(arr)+1)
		for _, group := range arr {
			stripped, dropGroup := stripBridgeHookEntries(group)
			if dropGroup {
				continue
			}
			kept = append(kept, stripped)
		}
		hooksMap[kind] = append(kept, bridgeGroup)
	}

	merged["hooks"] = hooksMap

	merged["autoLoadedSkillsDirectories"] = []interface{}{}

	return merged
}

func stripBridgeHookEntries(group interface{}) (interface{}, bool) {
	groupMap, ok := group.(map[string]interface{})
	if !ok {
		return group, false
	}
	entries, ok := groupMap["hooks"].([]interface{})
	if !ok {
		return group, false
	}
	kept := make([]interface{}, 0, len(entries))
	for _, entry := range entries {
		if isBridgeHookEntry(entry) {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == len(entries) {
		return group, false
	}
	if len(kept) == 0 {
		return nil, true
	}
	copied := make(map[string]interface{}, len(groupMap))
	for k, v := range groupMap {
		copied[k] = v
	}
	copied["hooks"] = kept
	return copied, false
}

func isBridgeHookEntry(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	args, ok := entryMap["args"].([]interface{})
	if !ok || len(args) == 0 {
		return false
	}
	verb, ok := args[0].(string)
	return ok && verb == hookRelayVerb
}

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

func stringsToInterface(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func atomicWriteWithParentFsync(path string, content []byte) error {
	pid := os.Getpid()
	tmpPath := fmt.Sprintf("%s.tmp-%d", path, pid)

	// (1) Write to temp file.
	//nolint:gosec // G304: tmpPath is derived from the caller-selected workspace settings path
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		return withCleanupErrs(fmt.Errorf("write tmp: %w", err), f.Close(), os.Remove(tmpPath))
	}
	if err := f.Sync(); err != nil {
		return withCleanupErrs(fmt.Errorf("fsync tmp: %w", err), f.Close(), os.Remove(tmpPath))
	}
	if err := f.Close(); err != nil {
		return withCleanupErrs(fmt.Errorf("close tmp: %w", err), os.Remove(tmpPath))
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return withCleanupErrs(fmt.Errorf("rename: %w", err), os.Remove(tmpPath))
	}

	parentDir := filepath.Dir(path)
	d, err := os.Open(parentDir) //nolint:gosec // G304: parentDir is derived from caller-provided path, not user input
	if err != nil {
		return fmt.Errorf("open parent dir: %w", err)
	}
	if err := d.Sync(); err != nil {
		return withCleanupErrs(fmt.Errorf("fsync parent dir: %w", err), d.Close())
	}
	return d.Close()
}

func appendToFile(path, text string) error {
	//nolint:gosec // G304: path is the caller-selected workspace session log
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("appendToFile OpenFile %q: %w", path, err)
	}
	if _, err := f.WriteString(text); err != nil {
		return withCleanupErrs(fmt.Errorf("appendToFile WriteString %q: %w", path, err), f.Close())
	}
	return f.Close()
}
