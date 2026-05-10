package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// SessionLogDirPath returns the canonical session-log directory path for a
// given workspace path and session ID, per workspace-model.md §4.7 WM-025
// and §6.2:
//
//	${workspace_path}/.harmonik/sessions/${session_id}/
//
// The directory is the join point for handler-written session logs (S04) and
// CASS-read metadata (S08). The workspace manager OWNS directory creation per
// WM-025; handlers OWN log-file contents.
//
// The caller is responsible for ensuring sessionID is a valid per-session UUID
// minted by the workspace manager at session start per [handler-contract.md
// §4.2]. SessionLogDirPath does NOT validate sessionID.
//
// Spec refs:
//   - workspace-model.md §4.7 WM-025 — session-log directory ownership rule.
//   - workspace-model.md §6.2 — canonical on-disk paths table.
func SessionLogDirPath(workspacePath, sessionID string) string {
	return filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
}

// SessionLogRootPath returns the canonical path to the sessions root directory
// for a given workspace, per workspace-model.md §6.2:
//
//	${workspace_path}/.harmonik/sessions/
//
// This is the parent under which each session's subdirectory lives.
// WM-003a uses this path to determine whether any session has ever been
// started against a workspace.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-003a — "no session-log directory" detection.
//   - workspace-model.md §6.2 — canonical on-disk paths table.
func SessionLogRootPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "sessions")
}

// CreateSessionLogDir creates the session-log directory at the canonical path
// for the given workspace and session ID per workspace-model.md §4.7 WM-025.
//
// The workspace manager MUST call CreateSessionLogDir BEFORE the handler
// launches for each session. The handler's `session_log_location` progress-
// stream emission (per [handler-contract.md §4.2 HC-010]) announces this
// pre-existing path to the watcher; the directory MUST pre-exist by the time
// the handler process starts.
//
// Per WM-016 ordering: for the first session, the sidecar write and
// CreateSessionLogDir MUST complete before `workspace_leased` is emitted.
// For subsequent sessions, the call precedes handler launch but does NOT
// re-emit `workspace_leased` (WM-026).
//
// CreateSessionLogDir uses [os.MkdirAll] so the call is idempotent — if the
// directory already exists the function succeeds without error.
//
// Returns an error if the directory cannot be created.
//
// Spec refs:
//   - workspace-model.md §4.7 WM-025 — session-log directory ownership rule.
//   - workspace-model.md §4.4 WM-016 — ordering gate: sidecar + dir before
//     workspace_leased.
//   - workspace-model.md §6.2 — canonical on-disk paths table.
func CreateSessionLogDir(workspacePath, sessionID string) error {
	dirPath := SessionLogDirPath(workspacePath, sessionID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("workspace: CreateSessionLogDir: MkdirAll %q: %w", dirPath, err)
	}
	return nil
}
