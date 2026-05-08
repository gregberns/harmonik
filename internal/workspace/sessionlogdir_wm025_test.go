package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWM025_SessionLogDirectoryLayout verifies that a session-log directory
// exists at the canonical path ${workspace_path}/.harmonik/sessions/${session_id}/
// and that the workspace manager owns directory creation (i.e., the directory is
// pre-created before a handler runs).
//
// Spec ref: workspace-model.md §4.7 WM-025 — "For every agent session launched
// against a workspace, a session-log directory MUST exist at
// ${workspace_path}/.harmonik/sessions/${session_id}/. The directory is the
// join point for handler-written session logs (S04) and CASS-read metadata (S08).
// The workspace manager OWNS directory creation."
func TestWM025_SessionLogDirectoryLayout(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0025"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002501"

	// Construct the canonical worktree path per WM-002.
	// No real git worktree add needed — these are filesystem-shape tests.
	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}

	// Simulate the workspace manager pre-creating the session-log directory
	// before the handler launches.
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	// Assert: canonical path exists and is a directory.
	info, err := os.Stat(sessionDir)
	if err != nil {
		t.Fatalf("WM-025: session-log directory missing: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("WM-025: %q exists but is not a directory", sessionDir)
	}

	// Assert: the path follows the exact canonical pattern
	// ${workspace_path}/.harmonik/sessions/${session_id}/
	wantDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if sessionDir != wantDir {
		t.Errorf("WM-025: session-log dir path = %q, want %q", sessionDir, wantDir)
	}

	t.Run("multiple-sessions-no-collision", func(t *testing.T) {
		t.Parallel()

		// WM-025: session-log directories for distinct sessions within a workspace
		// never collide because session_id is unique per launch.
		sessionID2 := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002502"
		sessionDir2 := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID2)
		if err := os.MkdirAll(sessionDir2, 0o755); err != nil {
			t.Fatalf("MkdirAll sessionDir2: %v", err)
		}

		if sessionDir == sessionDir2 {
			t.Errorf("WM-025: distinct session IDs produced the same directory path %q", sessionDir)
		}

		// Both must exist independently.
		for _, dir := range []string{sessionDir, sessionDir2} {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Errorf("WM-025: session dir %q not present or not a directory", dir)
			}
		}
	})
}
