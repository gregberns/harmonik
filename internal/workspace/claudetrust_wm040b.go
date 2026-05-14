package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// claudeGlobalConfigPath returns the path to Claude Code's user-level JSON
// config file (~/.claude.json). This is the file in which Claude Code persists
// per-project trust state (hasTrustDialogAccepted) after the operator accepts
// the interactive "Trust this directory?" dialog.
//
// Exposed as a var so tests may redirect to a temp file.
var claudeGlobalConfigPath = defaultClaudeGlobalConfigPath

func defaultClaudeGlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Unreachable on all supported platforms under normal operation.
		panic(fmt.Sprintf("workspace: claudeGlobalConfigPath: UserHomeDir: %v", err))
	}
	return filepath.Join(home, ".claude.json")
}

// EnsureWorktreeTrust pre-seeds Claude Code's user-level config (~/.claude.json)
// with a trust entry for worktreePath so that no interactive "Trust this
// directory?" prompt appears when Claude Code starts inside a daemon-spawned
// tmux pane (per workspace-model.md §4.7b WM-040b and claude-hook-bridge.md
// §4.12 CHB-029).
//
// # Mechanism
//
// Claude Code stores per-project trust state in ~/.claude.json under a
// top-level "projects" map keyed by absolute directory path. When the key is
// absent, or present but hasTrustDialogAccepted is false/missing, Claude Code
// shows an interactive trust prompt on startup. With no human at the terminal
// (daemon-spawned pane), that prompt blocks indefinitely and HC-056 fires.
//
// This function upserts the entry:
//
//	~/.claude.json["projects"][worktreePath]["hasTrustDialogAccepted"] = true
//
// It is idempotent: a second call for the same worktreePath is a no-op.
//
// # Ordering obligation (CHB-029 / WM-040b)
//
// MUST be called AFTER WM-003 (worktree creation) and WM-040a
// (settings.json materialization) and BEFORE exec'ing Claude via the tmux
// substrate (SubstrateSpawn). The ~/.claude.json write is NOT an atomic WM-026
// rename because the file must be stable across concurrent daemon activity; the
// function uses a PID-keyed temp file + rename for atomicity.
//
// # Failure semantics
//
// On any error (read, parse, marshal, write), EnsureWorktreeTrust returns a
// wrapped error. The caller MUST propagate this as a structural error and MUST
// NOT exec Claude — an un-trusted session would block rather than hang silently.
//
// # Parameters
//
//   - worktreePath: absolute path to the workspace root (worktree directory).
//     MUST be the same path Claude Code will be launched with as its working
//     directory (cmd.Dir / tmux start-directory).
func EnsureWorktreeTrust(worktreePath string) error {
	cfgPath := claudeGlobalConfigPath()
	return ensureWorktreeTrustAt(worktreePath, cfgPath)
}

// ensureWorktreeTrustAt is the testable inner implementation; cfgPath is the
// ~/.claude.json override, allowing unit tests to redirect to a temp file.
func ensureWorktreeTrustAt(worktreePath, cfgPath string) error {
	// Read existing config, or start from an empty map.
	var cfg map[string]interface{}
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is the user's own config file
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
			// Malformed ~/.claude.json: fail rather than silently corrupt.
			return fmt.Errorf("workspace: EnsureWorktreeTrust: parse %s: %w", cfgPath, jsonErr)
		}
	case os.IsNotExist(err):
		cfg = make(map[string]interface{})
	default:
		return fmt.Errorf("workspace: EnsureWorktreeTrust: read %s: %w", cfgPath, err)
	}

	// Navigate to cfg["projects"] map.
	var projects map[string]interface{}
	if raw, ok := cfg["projects"]; ok && raw != nil {
		projects, ok = raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("workspace: EnsureWorktreeTrust: ~/.claude.json projects field has unexpected type %T", raw)
		}
	} else {
		projects = make(map[string]interface{})
		cfg["projects"] = projects
	}

	// Upsert the per-project entry for worktreePath.
	var projectEntry map[string]interface{}
	if raw, ok := projects[worktreePath]; ok && raw != nil {
		projectEntry, ok = raw.(map[string]interface{})
		if !ok {
			// Unexpected shape — replace with a minimal entry.
			projectEntry = make(map[string]interface{})
		}
	} else {
		projectEntry = make(map[string]interface{})
	}

	// Check if already trusted (idempotent path).
	if trusted, ok := projectEntry["hasTrustDialogAccepted"].(bool); ok && trusted {
		return nil
	}

	projectEntry["hasTrustDialogAccepted"] = true
	projects[worktreePath] = projectEntry
	cfg["projects"] = projects

	// Marshal and atomically write back.
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: marshal: %w", err)
	}
	out = append(out, '\n')

	if err := atomicWriteWithParentFsync(cfgPath, out); err != nil {
		return fmt.Errorf("workspace: EnsureWorktreeTrust: write %s: %w", cfgPath, err)
	}

	return nil
}
