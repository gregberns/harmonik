package scenario

// WorkspaceSnapshotPath returns the fixture-root-relative path that the harness
// MUST record in ScenarioResult.WorkspaceSnapshotPath per SH-015a.
//
// SH-015a mandates that the snapshot path MUST point at the per-scenario
// worktree directory in-place — the same directory created during fixture-up
// at SH-012 (i.e. ScenarioWorkspacePath(fixtureRoot, scenarioName)). The
// harness MUST NOT copy, archive, or otherwise relocate the worktree at
// teardown; SH-016's "fixture root is not auto-deleted" rule preserves the
// directory for post-hoc operator inspection.
//
// The returned path is fixture-root-relative so ScenarioResult serialises
// portably across operator machines (absolute-path predicates are forbidden per
// SH-022).
//
// The snapshot MUST be captured AFTER sub-steps (a)-(d) of SH-015 (subprocess
// termination, lease release, event-log close, daemon stop). None of those
// sub-steps modify the worktree files or refs — any merge-back to integration
// occurred during orchestration per workspace-model.md §4.5.WM-019, BEFORE
// teardown — so the in-place directory faithfully represents the post-run
// workspace state.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-015a.
func WorkspaceSnapshotPath(scenarioName string) string {
	// ScenarioWorkspacePath returns <fixture-root>/<scenario-name>/workspace/.
	// The fixture-root-relative form strips the leading <fixture-root>/ prefix,
	// yielding <scenario-name>/workspace — a portable relative path.
	//
	// Implementation note: ScenarioWorkspacePath takes (fixtureRoot, scenarioName)
	// and joins them, so the relative portion is always:
	//   <scenarioName>/workspace
	// We construct the relative path directly to keep this function pure
	// (no filesystem access required) and avoid coupling to fixtureRoot
	// at record-time.
	return scenarioName + "/workspace"
}
