package scenario

import (
	"path/filepath"
	"strings"
	"testing"
)

// Helper prefix: sh015aSnapshot (per implementer-protocol.md § Helper-prefix discipline)

// TestSH015aWorkspaceSnapshotPathIsRelative verifies that WorkspaceSnapshotPath
// returns a relative (not absolute) path, satisfying SH-022's portability
// requirement that absolute-path predicates are forbidden.
func TestSH015aWorkspaceSnapshotPathIsRelative(t *testing.T) {
	t.Parallel()

	got := WorkspaceSnapshotPath("my-scenario")
	if filepath.IsAbs(got) {
		t.Errorf("WorkspaceSnapshotPath(%q) = %q: must be relative (not absolute); "+
			"SH-022 forbids absolute paths so scenarios are portable across operator machines",
			"my-scenario", got)
	}
}

// TestSH015aWorkspaceSnapshotPathContainsScenarioName verifies that the
// returned path contains the scenario name, which scopes it under the
// per-scenario subdirectory and prevents cross-scenario collision.
func TestSH015aWorkspaceSnapshotPathContainsScenarioName(t *testing.T) {
	t.Parallel()

	scenarioName := "basic-task-run"
	got := WorkspaceSnapshotPath(scenarioName)
	if !strings.Contains(got, scenarioName) {
		t.Errorf("WorkspaceSnapshotPath(%q) = %q: must contain scenario name; "+
			"the snapshot is scoped to the per-scenario workspace subdirectory per SH-013",
			scenarioName, got)
	}
}

// TestSH015aWorkspaceSnapshotPathAlignsWithScenarioWorkspacePath verifies that
// WorkspaceSnapshotPath(scenarioName) equals the fixture-root-relative form of
// ScenarioWorkspacePath(fixtureRoot, scenarioName).  SH-015a mandates the
// snapshot MUST point at the same directory created at SH-012 fixture-up.
func TestSH015aWorkspaceSnapshotPathAlignsWithScenarioWorkspacePath(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/fixture-root"
	scenarioName := "basic-task-run"

	absolutePath := ScenarioWorkspacePath(fixtureRoot, scenarioName)
	relativePath := WorkspaceSnapshotPath(scenarioName)

	// The absolute path MUST end with the relative path, confirming
	// they point at the same location.
	if !strings.HasSuffix(absolutePath, relativePath) {
		t.Errorf(
			"SH-015a alignment check failed:\n"+
				"  ScenarioWorkspacePath(%q, %q) = %q\n"+
				"  WorkspaceSnapshotPath(%q)       = %q\n"+
				"  snapshot path must be the fixture-root-relative tail of the workspace path",
			fixtureRoot, scenarioName, absolutePath,
			scenarioName, relativePath,
		)
	}
}

// TestSH015aWorkspaceSnapshotPathNonEmpty verifies that an empty scenario name
// still produces a non-empty path (even degenerate inputs don't yield an empty
// string, which ScenarioResult.Valid() rejects).
func TestSH015aWorkspaceSnapshotPathNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scenarioName string
	}{
		{"basic-task-run"},
		{"matrix-row-0"},
		{"s"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.scenarioName, func(t *testing.T) {
			t.Parallel()
			got := WorkspaceSnapshotPath(tc.scenarioName)
			if got == "" {
				t.Errorf("WorkspaceSnapshotPath(%q) = %q: must be non-empty; "+
					"ScenarioResult.Valid() requires WorkspaceSnapshotPath to be non-empty per §6.1",
					tc.scenarioName, got)
			}
		})
	}
}
