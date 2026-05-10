package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWM025_SessionLogDirPath locks the canonical session-log directory path
// shape required by workspace-model.md §4.7 WM-025 and §6.2:
//
//	${workspace_path}/.harmonik/sessions/${session_id}/
func TestWM025_SessionLogDirPath(t *testing.T) {
	t.Parallel()

	t.Run("canonical-path-shape", func(t *testing.T) {
		t.Parallel()

		workspacePath := "/srv/harmonik/.harmonik/worktrees/0196a1b2-c3d4-7025-8a1b-2c3d4e5f0025"
		sessionID := "sess-0196a1b2-c3d4-7025-8a1b-000000002501"

		wantPath := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
		gotPath := SessionLogDirPath(workspacePath, sessionID)

		if gotPath != wantPath {
			t.Errorf("WM-025: SessionLogDirPath = %q, want %q", gotPath, wantPath)
		}
	})

	t.Run("session-id-is-final-segment", func(t *testing.T) {
		t.Parallel()

		workspacePath := "/srv/harmonik/.harmonik/worktrees/0196a1b2-c3d4-7025-8a1b-2c3d4e5f0026"
		sessionID := "sess-0196a1b2-c3d4-7025-8a1b-000000002502"

		gotPath := SessionLogDirPath(workspacePath, sessionID)
		if filepath.Base(gotPath) != sessionID {
			t.Errorf("WM-025: SessionLogDirPath base = %q, want sessionID %q as final segment",
				filepath.Base(gotPath), sessionID)
		}
	})

	t.Run("root-path-shape", func(t *testing.T) {
		t.Parallel()

		workspacePath := "/srv/harmonik/.harmonik/worktrees/0196a1b2-c3d4-7025-8a1b-2c3d4e5f0027"
		sessionID := "sess-0196a1b2-c3d4-7025-8a1b-000000002503"

		wantRoot := filepath.Join(workspacePath, ".harmonik", "sessions")
		gotRoot := SessionLogRootPath(workspacePath)

		if gotRoot != wantRoot {
			t.Errorf("WM-025: SessionLogRootPath = %q, want %q", gotRoot, wantRoot)
		}

		// SessionLogDirPath must start with the root.
		gotDir := SessionLogDirPath(workspacePath, sessionID)
		if filepath.Dir(gotDir) != wantRoot {
			t.Errorf("WM-025: SessionLogDirPath parent = %q, want %q", filepath.Dir(gotDir), wantRoot)
		}
	})

	t.Run("distinct-sessions-no-collision", func(t *testing.T) {
		t.Parallel()

		// WM-025: session-log directories for distinct sessions within a workspace
		// never collide because session_id is unique per launch.
		workspacePath := "/srv/harmonik/.harmonik/worktrees/0196a1b2-c3d4-7025-8a1b-2c3d4e5f0028"
		sessionID1 := "sess-0196a1b2-c3d4-7025-8a1b-000000002504"
		sessionID2 := "sess-0196a1b2-c3d4-7025-8a1b-000000002505"

		path1 := SessionLogDirPath(workspacePath, sessionID1)
		path2 := SessionLogDirPath(workspacePath, sessionID2)

		if path1 == path2 {
			t.Errorf("WM-025: distinct session IDs produced the same path %q", path1)
		}
	})
}

// TestWM025_CreateSessionLogDir verifies that the workspace manager's session-log
// directory creation helper materialises the canonical directory per WM-025.
func TestWM025_CreateSessionLogDir(t *testing.T) {
	t.Parallel()

	t.Run("creates-directory-at-canonical-path", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		runID := "0196a1b2-c3d4-7025-8a1b-2c3d4e5f0029"
		sessionID := "sess-0196a1b2-c3d4-7025-8a1b-000000002506"

		workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		// Ensure the worktree dir exists (no git worktree add needed for path tests).
		if err := os.MkdirAll(workspacePath, 0o755); err != nil {
			t.Fatalf("MkdirAll worktree: %v", err)
		}

		if err := CreateSessionLogDir(workspacePath, sessionID); err != nil {
			t.Fatalf("WM-025: CreateSessionLogDir: %v", err)
		}

		wantDir := SessionLogDirPath(workspacePath, sessionID)
		info, err := os.Stat(wantDir)
		if err != nil {
			t.Fatalf("WM-025: session-log directory not found at %q: %v", wantDir, err)
		}
		if !info.IsDir() {
			t.Errorf("WM-025: %q exists but is not a directory", wantDir)
		}
	})

	t.Run("idempotent-create", func(t *testing.T) {
		t.Parallel()

		// Creating the same session-log dir twice must succeed without error.
		repo, _ := tempRepo(t)
		runID := "0196a1b2-c3d4-7025-8a1b-2c3d4e5f002a"
		sessionID := "sess-0196a1b2-c3d4-7025-8a1b-000000002507"

		workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		if err := os.MkdirAll(workspacePath, 0o755); err != nil {
			t.Fatalf("MkdirAll worktree: %v", err)
		}

		if err := CreateSessionLogDir(workspacePath, sessionID); err != nil {
			t.Fatalf("WM-025: first CreateSessionLogDir: %v", err)
		}
		if err := CreateSessionLogDir(workspacePath, sessionID); err != nil {
			t.Errorf("WM-025: second CreateSessionLogDir (idempotent): %v", err)
		}
	})
}
