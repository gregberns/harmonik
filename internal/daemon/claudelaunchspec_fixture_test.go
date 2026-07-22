package daemon_test

// claudelaunchspec_fixture_test.go — the ONE fixture from
// claudelaunchspec_test.go that could not leave with it.
//
// P2 unit E1b moved claudelaunchspec_test.go to
// internal/harness/claude/launchspec_test.go. Six call sites in
// modelpreference_hkxo03m_test.go (lines 61/83/103/123/148/172) STAY in the
// daemon — the 4-tier ResolveModelPreference walk reads project config and
// emits bus events, so it is daemon wiring and did not move — and they need
// this workspace fixture. A Go test helper is not visible across a package
// boundary, so the ~10 lines are duplicated rather than shared: inventing a
// cross-package test-fixture package to save them would be exactly the new
// seam the extraction plan forbids
// (plans/2026-07-21-p2-extraction/E1b-claude.md §1, R4).
//
// Kept byte-identical to the copy that moved. If one changes, change both.

import (
	"os"
	"path/filepath"
	"testing"
)

// claudeLaunchSpecFixtureWorkspace creates a temporary workspace directory
// suitable for MaterializeClaudeSettings and CheckSettingsLocalJSON.
// Returns the workspace path.
func claudeLaunchSpecFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Ensure the .claude/ directory exists so MaterializeClaudeSettings does not
	// need to create it from scratch (it will, but this mirrors real worktree layout).
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("claudeLaunchSpecFixtureWorkspace: MkdirAll .claude/: %v", err)
	}
	return dir
}
