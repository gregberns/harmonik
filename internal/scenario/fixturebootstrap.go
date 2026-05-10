package scenario

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/workspace"
)

// ScenarioWorkspacePath returns the canonical path for the per-scenario
// isolated workspace directory per specs/scenario-harness.md §4.4 SH-013.
//
// The harness places each scenario's workspace at
// <fixture-root>/<scenario-name>/workspace/ so that all per-scenario
// artifacts are co-located under the per-suite ephemeral fixture root and
// disjoint across scenarios (given SH-005 name uniqueness).
//
// Spec ref: specs/scenario-harness.md §4.4 SH-013.
func ScenarioWorkspacePath(fixtureRoot, scenarioName string) string {
	return filepath.Join(fixtureRoot, scenarioName, "workspace")
}

// FixtureBootstrapResult is the output of a successful BootstrapFixture call.
// All three sub-steps of SH-012 have completed when this value is returned.
type FixtureBootstrapResult struct {
	// ProjectRoot is the absolute path to the per-scenario synthetic project
	// root (SH-016a), where the daemon's working directory is set and where
	// .harmonik/ artifacts are written.
	ProjectRoot string

	// EventLogDir is the absolute path to the pre-created event-log directory
	// at <project-root>/.harmonik/events/ per SH-014. The harness creates this
	// directory so the daemon can write events.jsonl on first open without
	// needing to create it itself.
	EventLogDir string

	// TwinSearchPaths is the ordered list of resolved twin-binary search paths
	// passed to the harness for sub-step (c). An empty slice is valid when no
	// search paths are configured; absolute-path agent_overrides resolve without
	// a search prefix.
	TwinSearchPaths []string
}

// BootstrapFixture executes the three sub-steps of the fixture-setup phase
// declared in specs/scenario-harness.md §4.4 SH-012:
//
//   - (a) Synthesize the per-scenario synthetic project root at
//     <fixture-root>/<scenario-name>/project/ per SH-016a. The root is
//     initialised as a fresh git repository via SynthesizeProjectRoot, which
//     calls [workspace.CreateWorktree]-compatible git init conforming to
//     workspace-model.md §4.1.WM-001 and §4.2 branching model.
//   - (b) Create the isolated event-log directory at
//     <project-root>/.harmonik/events/ per SH-014 so the per-scenario daemon
//     can write events.jsonl on first open.
//   - (c) Capture the caller-supplied twin-binary search paths; no filesystem
//     access is performed at this layer (resolution against the search paths
//     is deferred to resolve_twin_binary per SH-009).
//
// Failure of any sub-step returns (nil, fixture-setup-failed) per §8.3; the
// caller MUST NOT proceed to orchestration on a non-nil error. On
// partial-success failure the caller MUST run fixture teardown (SH-015)
// best-effort against the partial result before recording the failure; teardown
// errors MUST be appended to error_detail but MUST NOT change the failure class
// from fixture-setup-failed to cleanup-failed per §8.0.
//
// ctx should reflect the scenario-level deadline (SH-025) so that a hung
// sub-step does not block the suite indefinitely.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-012.
func BootstrapFixture(ctx context.Context, fixtureRoot, scenarioName string, twinSearchPaths []string) (*FixtureBootstrapResult, error) {
	// Sub-step (a): synthesize the per-scenario synthetic project root.
	// SynthesizeProjectRoot calls git init, conforming to WM-001/WM-002.
	projectRoot, err := SynthesizeProjectRoot(ctx, fixtureRoot, scenarioName)
	if err != nil {
		return nil, fmt.Errorf("%w: sub-step (a) synthesize project root: %w",
			errFixtureSetupFailed, err)
	}

	// Sub-step (b): create the isolated event-log directory.
	// The daemon writes events.jsonl relative to its working directory; the
	// harness pre-creates the directory so the daemon can open the file
	// without needing to create its parent per SH-014.
	evLogDir := EventLogDir(projectRoot)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(evLogDir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: sub-step (b) create event-log dir %q: %w",
			errFixtureSetupFailed, evLogDir, err)
	}

	// Sub-step (c): capture the twin-binary search paths.
	// The caller (harness CLI or test driver) resolves the search-path
	// precedence (--twin-search-path > HARMONIK_TWIN_SEARCH_PATH > default
	// <repo-root>/twins/) before calling BootstrapFixture; we record the
	// resolved list here so it can be threaded into resolve_twin_binary
	// (SH-009) without re-computing precedence inside the fixture layer.
	// An empty list is valid; absolute agent_override paths resolve directly.
	resolvedPaths := make([]string, len(twinSearchPaths))
	copy(resolvedPaths, twinSearchPaths)

	return &FixtureBootstrapResult{
		ProjectRoot:     projectRoot,
		EventLogDir:     evLogDir,
		TwinSearchPaths: resolvedPaths,
	}, nil
}

// errFixtureSetupFailed is the sentinel error whose string form signals that a
// BootstrapFixture error should be classified as FailureClassFixtureSetupFailed
// per specs/scenario-harness.md §8.3. It is unexported; callers inspect the
// returned FailureClass via BootstrapFixtureFailureClass.
var errFixtureSetupFailed = fmt.Errorf("fixture-setup-failed")

// BootstrapFixtureFailureClass returns the FailureClass for the error returned
// by BootstrapFixture. Per §8.3, any BootstrapFixture error classifies as
// FailureClassFixtureSetupFailed. If err is nil, it returns the zero value
// (empty string).
//
// Spec ref: specs/scenario-harness.md §8.3.
func BootstrapFixtureFailureClass(err error) FailureClass {
	if err == nil {
		return ""
	}
	return FailureClassFixtureSetupFailed
}

// ScenarioWorktreeRootOverride returns a [workspace.WorktreeRootConfig] that
// places the per-scenario worktree at <fixture-root>/<scenario-name>/workspace/
// per SH-013, overriding the default <repo>/.harmonik/worktrees/ path.
//
// This override is used when calling [workspace.CreateWorktree] with the
// synthetic project root as the repoRoot argument so that the worktree lands
// inside the per-suite ephemeral fixture root rather than inside the
// operator's .harmonik/ tree, keeping the operator's working tree untouched
// per SH-014.
//
// Spec refs:
//   - specs/scenario-harness.md §4.4 SH-013 — per-scenario workspace isolation.
//   - specs/scenario-harness.md §4.4 SH-014 — operator .harmonik/ non-mutation.
//   - workspace-model.md §4.7 CP-037 — worktree-root operator override surface.
func ScenarioWorktreeRootOverride(fixtureRoot, scenarioName string) workspace.WorktreeRootConfig {
	return workspace.WithWorktreeRootOverride(ScenarioWorkspacePath(fixtureRoot, scenarioName))
}
