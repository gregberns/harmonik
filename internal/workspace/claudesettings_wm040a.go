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
// daemonBinaryPath MUST be the absolute path to the running harmonik binary
// (resolved at daemon start via os.Executable) so that the hook command can
// be found regardless of the tmux window's $PATH (hk-kqdpf.6 fix).
func bridgeMatcherGroupFor(eventKind, daemonBinaryPath string) bridgeMatcherGroup {
	return bridgeMatcherGroup{
		Matcher: "",
		Hooks: []bridgeHookEntry{
			{
				Type:    "command",
				Command: daemonBinaryPath,
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
			merged = buildBridgeOnlySettings(daemonBinaryPath)
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
			merged = mergeSettingsWithBridge(parsed, daemonBinaryPath)
		}
	} else {
		// File absent — write fresh bridge-only content.
		merged = buildBridgeOnlySettings(daemonBinaryPath)
	}

	// Strip disableAllHooks: true per CHB-004.
	delete(merged, "disableAllHooks")

	// Serialize the merged result.
	content, err := marshalSettings(merged)
	if err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: MarshalIndent: %w", err)
	}

	// Atomic write per WM-026.
	if err := atomicWriteWithParentFsync(settingsPath, content); err != nil {
		return fmt.Errorf("workspace: MaterializeClaudeSettings: atomic write: %w", err)
	}

	_ = overwrote // consumed via sessionLogPath warning above

	// CHB-005 gitignore hygiene is now an operator-setup obligation (hk-jvzc2):
	// the parent repo's root .gitignore MUST cover .claude/settings.json before
	// the daemon runs. The daemon no longer mutates the worktree .gitignore
	// per-launch — silent edits surfaced as uncommitted churn in the parent
	// repo's working tree across dogfood runs (hk-cd92e, hk-jvzc2).
	return nil
}

// marshalSettings serializes a merged settings map to the canonical on-disk
// byte form (indented JSON + trailing newline). Shared by the local
// MaterializeClaudeSettings write and the remote MaterializeClaudeSettingsVia
// write so both produce byte-identical settings.json content (hk-z8ek).
func marshalSettings(merged map[string]interface{}) ([]byte, error) {
	content, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// buildBridgeOnlySettings returns a settings map containing only the
// bridge-required hook entries per CHB-003, plus the skill-autoload
// disable setting per T6 (hk-j79ny).
// daemonBinaryPath is used as the hook "command" field per hk-kqdpf.6.
//
// NOTE (hk trust-modal fix, 2026-07-06): the permissions.allow pre-authorization
// array (formerly written here per WM-040a/hk-53y35) is DELIBERATELY NOT written.
// In a git-worktree context Claude Code >= 2.1.201 fires an interactive
// "This folder pre-approves N tool permissions in .claude/settings.json" consent
// modal whenever a project-local settings.json declares permissions.allow. That
// modal is NOT suppressed by the ~/.claude.json trust keys (hasTrustDialogAccepted
// / hasCompletedProjectOnboarding) NOR by --dangerously-skip-permissions, so a
// daemon-spawned pane wedges at it and times out at agent_ready (HC-056). Since
// every harmonik worktree launch already passes --dangerously-skip-permissions
// (HC-055b), the allow-list is redundant — omitting it removes the modal. See
// the workspace-model.md §4.7a note. Confirmed empirically: identical settings
// boot clean in a non-git dir but wedge in a git worktree; stripping the block
// boots the git worktree clean.
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

// mergeSettingsWithBridge appends bridge matcher-groups to each event-type
// array in existing, per CHB-004: user hooks continue to fire alongside.
// daemonBinaryPath is used as the hook "command" field per hk-kqdpf.6.
func mergeSettingsWithBridge(existing map[string]interface{}, daemonBinaryPath string) map[string]interface{} {
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
		bridgeGroup := groupToInterface(bridgeMatcherGroupFor(kind, daemonBinaryPath))
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

	// NOTE (hk trust-modal fix, 2026-07-06): harmonik NO LONGER injects a
	// permissions.allow block. Formerly (WM-040a/hk-53y35) it wrote the harmonik
	// default allow-list when the merged settings lacked one. But in a git-worktree
	// context Claude Code >= 2.1.201 fires an interactive "pre-approves N tool
	// permissions in .claude/settings.json" consent modal whenever project-local
	// settings declare permissions.allow — a modal NOT suppressed by the
	// ~/.claude.json trust keys or by --dangerously-skip-permissions, wedging the
	// daemon-spawned pane at agent_ready (HC-056). Since every worktree launch
	// already passes --dangerously-skip-permissions (HC-055b), the allow-list is
	// redundant. Any permissions block the user committed in their own settings.json
	// is left exactly as-is (harmonik neither adds nor edits it) — if a user opts to
	// declare permissions.allow themselves, that is their choice. See
	// buildBridgeOnlySettings for the full rationale.

	// Force-set autoLoadedSkillsDirectories to [] so worker agent panes never
	// see fleet orchestration skills auto-loaded from ancestor .claude/skills/
	// directories. This is a hard invariant, not a user-overridable default:
	// harmonik controls which skills reach each agent via required_skills[] in
	// the LaunchSpec + manifest context[]; ambient autoload would bypass that
	// scoping. Always overwrite — similar to how disableAllHooks is stripped.
	//
	// Spec ref: T6/hk-j79ny; agent-manifest SPEC.md §6.
	merged["autoLoadedSkillsDirectories"] = []interface{}{}

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

// gitignoreLinePresent reports whether line appears on its own line in content.
// Retained as a test helper after hk-jvzc2 removed the per-launch worktree
// .gitignore-append path; the function continues to back assertion-only callers.
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
