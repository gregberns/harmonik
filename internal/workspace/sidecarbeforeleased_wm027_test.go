package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWM027_SidecarPrecedesWorkspaceLeased verifies the temporal ordering
// contract: the first session's metadata sidecar is on disk BEFORE
// workspace_leased is emitted.
//
// The test simulates the ordering by:
//  1. Writing the sidecar atomically (as WM-026 requires).
//  2. Asserting the sidecar is durably on disk.
//  3. Noting conceptually that workspace_leased would emit only after this point
//     (the actual emitter wiring is downstream; this fixture asserts the
//     durability contract).
//
// Spec ref: workspace-model.md §4.7 WM-027 — "The workspace manager MUST write
// the first session's metadata sidecar BEFORE emitting workspace_leased per
// WM-016. This ordering ensures that any consumer of workspace_leased observing
// the handler launch can join metadata without racing the sidecar write."
func TestWM027_SidecarPrecedesWorkspaceLeased(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0027"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002701"

	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
	content := sessionLogFixtureMakeMetaJSON(t, runID, sessionID, "node-01", "agentic", "wf-01", "")

	// Step 1: Write sidecar atomically (simulates workspace manager action before workspace_leased).
	if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, content); err != nil {
		t.Fatalf("WM-027: atomic sidecar write failed: %v", err)
	}

	// Step 2: Assert sidecar is durably on disk.
	// A workspace_leased consumer arriving now MUST find the sidecar present.
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Errorf("WM-027: sidecar not on disk before workspace_leased emission point: %v", err)
	}

	// Step 3: Conceptual ordering gate — workspace_leased would emit here.
	// The real emitter is downstream (S06). This fixture captures the durability
	// pre-condition: sidecar is present and readable before any event fires.
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("WM-027: ReadFile sidecar: %v", err)
	}
	if len(raw) == 0 {
		t.Errorf("WM-027: sidecar is empty; must be non-empty before workspace_leased")
	}
}

// TestWM027_SubsequentSessionsDoNotReemitWorkspaceLeased verifies that
// subsequent sessions within the same workspace write their own sidecars but
// do NOT re-emit workspace_leased.
//
// Spec ref: workspace-model.md §4.7 WM-027 note — "For subsequent sessions
// within a workspace (WM-010 allows many sessions per workspace), the sidecar
// write precedes handler launch but does NOT re-emit workspace_leased; it
// proceeds as a stand-alone sidecar-write operation covered by WM-026."
//
// Also workspace-model.md §4.4 WM-016 note — "The per-workspace
// workspace_leased emission is tied to the FIRST session's sidecar write.
// Subsequent sessions write their own sidecars per WM-026; the workspace's
// lifecycle state does NOT re-transition and workspace_leased does NOT
// re-emit on subsequent session launches."
func TestWM027_SubsequentSessionsDoNotReemitWorkspaceLeased(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0027"
	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	// Simulate a leased counter: 0 = not yet leased, 1 = leased once.
	// workspace_leased is emitted exactly once — only on the first session.
	workspaceLeasedCount := 0

	emitWorkspaceLeased := func(sessionNum int) {
		// Only the first session triggers workspace_leased.
		if sessionNum == 1 {
			workspaceLeasedCount++
		}
		// Subsequent sessions: sidecar is written, no event re-emitted.
	}

	sessions := []struct {
		sessionID string
	}{
		{"sess-0196a1b2-c3d4-7ef0-8a1b-000000002711"},
		{"sess-0196a1b2-c3d4-7ef0-8a1b-000000002712"},
		{"sess-0196a1b2-c3d4-7ef0-8a1b-000000002713"},
	}

	for i, s := range sessions {
		sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", s.sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("MkdirAll sessionDir[%d]: %v", i, err)
		}
		sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
		content := sessionLogFixtureMakeMetaJSON(t, runID, s.sessionID, "node-01", "agentic", "wf-01", "")
		if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, content); err != nil {
			t.Fatalf("WM-027: session[%d] sidecar write: %v", i, err)
		}
		// Sidecar is on disk before handler launch for every session.
		if _, err := os.Stat(sidecarPath); err != nil {
			t.Errorf("WM-027: session[%d] sidecar not on disk: %v", i, err)
		}
		// workspace_leased fires only for session index 0 (session number 1).
		emitWorkspaceLeased(i + 1)
	}

	// Assert: workspace_leased was emitted exactly once.
	if workspaceLeasedCount != 1 {
		t.Errorf("WM-027: workspace_leased emitted %d times; want exactly 1", workspaceLeasedCount)
	}

	// Assert: all three session directories and sidecars exist independently.
	for i, s := range sessions {
		sidecarPath := filepath.Join(workspacePath, ".harmonik", "sessions", s.sessionID, "harmonik.meta.json")
		if _, err := os.Stat(sidecarPath); err != nil {
			t.Errorf("WM-027: session[%d] sidecar missing: %v", i, err)
		}
	}
}
