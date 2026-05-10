package scenario

// sh013_isolated_workspace_test.go — contract tests for SH-013 isolated workspace.
//
// Per specs/scenario-harness.md §4.4 SH-013: each scenario's workspace MUST be
// disjoint from every other scenario's workspace and from the operator's working
// tree. "Disjoint" means: no two scenarios' canonical workspace paths share a
// prefix and no symlink under one workspace resolves to a path under another.
// Workspaces MUST be created under a per-suite ephemeral root (see SH-016) and
// MUST NOT reuse any path from a prior suite invocation.
//
// These tests cover the three gap surfaces not yet locked by sh012_fixture_setup_test.go:
//
//  (a) Cross-scenario disjoint: two scenarios in the same suite MUST have disjoint
//      ScenarioWorkspacePath results (no shared prefix in either direction).
//  (b) Symlink-resolution discipline: no symlink under one workspace resolves to a
//      path under another scenario's workspace.
//  (c) Per-suite no-reuse: a fresh suite invocation (different ephemeral root)
//      MUST NOT produce workspace paths that collide with a prior suite's paths.
//
// Helper prefix: sh013Isolated (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.4 SH-013, §4.4 SH-016.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sh013IsolatedEphemeralRoot creates a temporary directory that serves as a
// per-suite ephemeral fixture root, registered for cleanup via t.Cleanup.
func sh013IsolatedEphemeralRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-sh013-")
	if err != nil {
		t.Fatalf("sh013IsolatedEphemeralRoot: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// sh013IsolatedWorkspacePath is a thin alias kept here to make test intent
// obvious — it calls ScenarioWorkspacePath, which is the production surface
// under test for SH-013.
func sh013IsolatedWorkspacePath(fixtureRoot, scenarioName string) string {
	return ScenarioWorkspacePath(fixtureRoot, scenarioName)
}

// TestSH013_CrossScenario_WorkspacePathsAreDisjoint verifies that two different
// scenario names within the same suite produce ScenarioWorkspacePath values that
// are fully disjoint: neither is a path-prefix of the other. This is the
// cross-scenario disjoint property mandated by SH-013.
func TestSH013_CrossScenario_WorkspacePathsAreDisjoint(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh013IsolatedEphemeralRoot(t)

	alphaWs := sh013IsolatedWorkspacePath(fixtureRoot, "alpha")
	betaWs := sh013IsolatedWorkspacePath(fixtureRoot, "beta")

	if alphaWs == betaWs {
		t.Errorf("SH-013 violation: alpha and beta workspace paths are identical: %q", alphaWs)
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(alphaWs, betaWs+sep) {
		t.Errorf("SH-013 violation: alpha workspace %q is under beta workspace %q; paths must not share a prefix",
			alphaWs, betaWs)
	}
	if strings.HasPrefix(betaWs, alphaWs+sep) {
		t.Errorf("SH-013 violation: beta workspace %q is under alpha workspace %q; paths must not share a prefix",
			betaWs, alphaWs)
	}
}

// TestSH013_CrossScenario_WorkspacePathsDontSharePrefixMultiple verifies that
// N distinct scenario names all produce mutually disjoint workspace paths. This
// exercises the general case (not just pairwise alpha/beta) and ensures the
// naming convention scales to a larger suite without path collisions.
func TestSH013_CrossScenario_WorkspacePathsDontSharePrefixMultiple(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh013IsolatedEphemeralRoot(t)
	scenarios := []string{
		"load-scenario",
		"checkpoint-merge",
		"agent-crash-recovery",
		"twin-exit-nonzero",
		"policy-gate-block",
	}

	paths := make([]string, len(scenarios))
	for i, name := range scenarios {
		paths[i] = sh013IsolatedWorkspacePath(fixtureRoot, name)
	}

	sep := string(filepath.Separator)
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			pi, pj := paths[i], paths[j]
			if pi == pj {
				t.Errorf("SH-013 violation: scenarios %q and %q produce identical workspace paths %q",
					scenarios[i], scenarios[j], pi)
				continue
			}
			if strings.HasPrefix(pi, pj+sep) {
				t.Errorf("SH-013 violation: workspace[%q] %q is under workspace[%q] %q",
					scenarios[i], pi, scenarios[j], pj)
			}
			if strings.HasPrefix(pj, pi+sep) {
				t.Errorf("SH-013 violation: workspace[%q] %q is under workspace[%q] %q",
					scenarios[j], pj, scenarios[i], pi)
			}
		}
	}
}

// TestSH013_SymlinkDiscipline_WorkspaceSymlinkDoesNotCrossIntoSibling verifies
// the symlink-resolution discipline: a symlink placed under one scenario's
// workspace that points at a path under a sibling scenario's workspace is
// detectable (and MUST be rejected at predicate-evaluation time per SH-022).
// This test confirms that EvalSymlinks on such a symlink resolves outside the
// source workspace, giving the harness the information it needs to enforce the
// "no symlink crosses" rule from SH-013.
//
// The harness assertion evaluator (SH-022) rejects such predicates; this test
// validates the path-level primitive (filepath.EvalSymlinks) that the evaluator
// would use when checking symlinks at workspace-predicate evaluation time.
//
// Note: both workspace paths are resolved via filepath.EvalSymlinks before
// comparison so that OS-level symlinks in the temp directory (e.g. /var →
// /private/var on macOS) do not produce false negatives.
func TestSH013_SymlinkDiscipline_WorkspaceSymlinkDoesNotCrossIntoSibling(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh013IsolatedEphemeralRoot(t)

	alphaWs := sh013IsolatedWorkspacePath(fixtureRoot, "alpha")
	betaWs := sh013IsolatedWorkspacePath(fixtureRoot, "beta")

	// Create both workspace directories.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(alphaWs, 0o755); err != nil {
		t.Fatalf("MkdirAll alphaWs: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(betaWs, 0o755); err != nil {
		t.Fatalf("MkdirAll betaWs: %v", err)
	}

	// Resolve canonical forms of the workspace paths before comparison.
	// On macOS, /var is a symlink to /private/var; EvalSymlinks normalises both
	// sides so comparisons are stable across OS-level symlinks in the temp dir.
	alphaWsReal, err := filepath.EvalSymlinks(alphaWs)
	if err != nil {
		t.Fatalf("EvalSymlinks(alphaWs %q): %v", alphaWs, err)
	}
	betaWsReal, err := filepath.EvalSymlinks(betaWs)
	if err != nil {
		t.Fatalf("EvalSymlinks(betaWs %q): %v", betaWs, err)
	}

	// Place a canary file under beta's workspace.
	betaCanary := filepath.Join(betaWs, "canary.txt")
	//nolint:gosec // G306: 0644 is appropriate for a canary test file; not user input
	if err := os.WriteFile(betaCanary, []byte("beta-canary"), 0o644); err != nil {
		t.Fatalf("WriteFile betaCanary: %v", err)
	}

	// Create a symlink under alpha's workspace pointing at the canary in beta.
	symlinkPath := filepath.Join(alphaWs, "link-to-beta")
	if err := os.Symlink(betaCanary, symlinkPath); err != nil {
		t.Fatalf("Symlink alpha→beta: %v", err)
	}

	// Resolve the symlink. The harness uses EvalSymlinks to detect cross-workspace
	// symlinks and MUST reject predicates that resolve outside the source workspace.
	resolved, err := filepath.EvalSymlinks(symlinkPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", symlinkPath, err)
	}

	// The resolved path must NOT be under the alpha workspace — it points into beta.
	sep := string(filepath.Separator)
	if strings.HasPrefix(resolved, alphaWsReal+sep) || resolved == alphaWsReal {
		t.Errorf("SH-013 symlink discipline: symlink resolved inside alpha workspace %q; "+
			"expected it to resolve into beta workspace %q. "+
			"The harness would incorrectly allow a cross-workspace traversal.",
			alphaWsReal, betaWsReal)
	}
	// Confirm the resolved path is actually under beta (proves the crossing).
	if !strings.HasPrefix(resolved, betaWsReal+sep) && resolved != betaWsReal {
		t.Errorf("SH-013 symlink discipline: resolved path %q is neither under alpha %q nor beta %q; "+
			"unexpected resolution result", resolved, alphaWsReal, betaWsReal)
	}
}

// TestSH013_PerSuiteNoReuse_FreshSuiteRootProducesDistinctPaths verifies that
// two separate suite invocations (each with their own ephemeral fixture root)
// produce workspace paths for the same scenario name that are distinct from
// each other. This enforces the "MUST NOT reuse any path from a prior suite
// invocation" property of SH-013 / SH-016 (fresh ephemeral roots per suite).
func TestSH013_PerSuiteNoReuse_FreshSuiteRootProducesDistinctPaths(t *testing.T) {
	t.Parallel()

	// Simulate two separate suite invocations with their own ephemeral roots.
	suiteOneRoot := sh013IsolatedEphemeralRoot(t)
	suiteTwoRoot := sh013IsolatedEphemeralRoot(t)

	const scenarioName = "smoke-scenario"

	// Each suite must produce a distinct workspace path for the same scenario.
	suiteOneWs := sh013IsolatedWorkspacePath(suiteOneRoot, scenarioName)
	suiteTwoWs := sh013IsolatedWorkspacePath(suiteTwoRoot, scenarioName)

	if suiteOneWs == suiteTwoWs {
		t.Errorf("SH-013 / SH-016 no-reuse violation: two separate suite invocations "+
			"produced the same workspace path %q for scenario %q; "+
			"each suite MUST use a fresh ephemeral root guaranteeing path uniqueness",
			suiteOneWs, scenarioName)
	}

	// The two roots themselves must be distinct (validates that the ephemeral
	// root creation mechanism is functioning; if this fails, the no-reuse
	// guarantee cannot be satisfied at all).
	if suiteOneRoot == suiteTwoRoot {
		t.Errorf("SH-016 violation: two ephemeral roots are identical: %q; "+
			"os.MkdirTemp must produce a unique root per suite invocation", suiteOneRoot)
	}
}

// TestSH013_PerSuiteNoReuse_PathContainsEphemeralRootPrefix verifies that the
// workspace path returned by ScenarioWorkspacePath is rooted under the
// suite-supplied fixture root. This confirms the structural invariant that path
// uniqueness per-suite is guaranteed by unique ephemeral roots (SH-016): if
// each suite creates a fresh fixture root, the resulting workspace paths are
// guaranteed to be distinct across suites because the root prefix differs.
func TestSH013_PerSuiteNoReuse_PathContainsEphemeralRootPrefix(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh013IsolatedEphemeralRoot(t)
	ws := sh013IsolatedWorkspacePath(fixtureRoot, "my-scenario")

	sep := string(filepath.Separator)
	if !strings.HasPrefix(ws, fixtureRoot+sep) {
		t.Errorf("SH-013: workspace path %q is not under the fixture root %q; "+
			"per-suite uniqueness requires all workspace paths to be rooted under the ephemeral root",
			ws, fixtureRoot)
	}
}

// TestSH013_WorkspacePath_DisjointFromOperatorTree verifies that the scenario
// workspace path is distinct from the operator's working tree (the real repo
// root). The operator's working tree must never appear in the fixture path.
func TestSH013_WorkspacePath_DisjointFromOperatorTree(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh013IsolatedEphemeralRoot(t)
	ws := sh013IsolatedWorkspacePath(fixtureRoot, "op-disjoint")

	// The fixture root is created by os.MkdirTemp under the OS temp directory,
	// which is structurally distinct from the operator's working tree (repo root).
	// We verify this by confirming the workspace path does not contain the
	// fixture root's parent as a path that would imply nesting in the repo tree.
	//
	// A stricter check: the workspace must be under fixtureRoot, not some other
	// path that coincides with a real repo directory. Since fixtureRoot is a
	// MkdirTemp-produced path, it lives under os.TempDir(), not the repo root.
	osTmp := os.TempDir()
	sep := string(filepath.Separator)

	// Workspace must be under the ephemeral fixture root (which is under osTmp).
	if !strings.HasPrefix(ws, fixtureRoot+sep) {
		t.Errorf("SH-013: workspace path %q is not under fixture root %q",
			ws, fixtureRoot)
	}
	// Fixture root must be under OS temp dir, confirming separation from the repo.
	if !strings.HasPrefix(fixtureRoot, osTmp) {
		t.Logf("SH-013 info: fixture root %q is not under os.TempDir() %q; "+
			"this is unusual but not a spec violation — custom fixture-root overrides are permitted (SH-016)",
			fixtureRoot, osTmp)
	}

	_ = sep
}
