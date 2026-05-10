package scenario

// sh012_fixture_setup_test.go — contract tests for the SH-012 fixture-setup phase.
//
// Per specs/scenario-harness.md §4.4 SH-012: for every scenario, the harness
// MUST execute a fixture-setup phase BEFORE invoking the orchestration drive
// of §4.5. Fixture setup MUST:
//   (a) create a fresh workspace conforming to WM-001 / WM-002;
//   (b) create an isolated event-log directory;
//   (c) prepare the twin-binary search path.
//
// Failure of any sub-step → failure_class=fixture-setup-failed; MUST NOT
// proceed to orchestration.
//
// These tests exercise BootstrapFixture, ScenarioWorkspacePath, and
// ScenarioWorktreeRootOverride at the sub-step contract boundary. They do not
// drive orchestration; that is out of scope for the scenario package layer.
//
// Helper prefix: sh012Fixture (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.4 SH-012, §4.4 SH-013, §4.4 SH-014.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sh012FixtureEphemeralRoot creates a temporary directory as the per-suite
// fixture root, registered for cleanup via t.Cleanup.
func sh012FixtureEphemeralRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-sh012-")
	if err != nil {
		t.Fatalf("sh012FixtureEphemeralRoot: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestSH012_BootstrapFixture_SubStepA_ProjectRootCreated verifies that
// BootstrapFixture sub-step (a) creates the per-scenario synthetic project root
// and initialises it as a git repository (SH-016a, WM-001).
func TestSH012_BootstrapFixture_SubStepA_ProjectRootCreated(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "alpha", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	// The project root must exist and be a directory.
	info, statErr := os.Stat(result.ProjectRoot)
	if statErr != nil {
		t.Fatalf("BootstrapFixture: ProjectRoot %q: stat error: %v", result.ProjectRoot, statErr)
	}
	if !info.IsDir() {
		t.Errorf("BootstrapFixture: ProjectRoot %q: exists but is not a directory", result.ProjectRoot)
	}
}

// TestSH012_BootstrapFixture_SubStepA_ProjectRootIsGitRepo verifies that the
// synthetic project root produced by sub-step (a) contains a fresh git
// repository (.git/ directory present) per WM-001.
func TestSH012_BootstrapFixture_SubStepA_ProjectRootIsGitRepo(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "git-repo-check", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	gitDir := filepath.Join(result.ProjectRoot, ".git")
	info, statErr := os.Stat(gitDir)
	if statErr != nil {
		t.Fatalf("BootstrapFixture: ProjectRoot %q: .git/ not found: %v", result.ProjectRoot, statErr)
	}
	if !info.IsDir() {
		t.Errorf("BootstrapFixture: ProjectRoot %q: .git exists but is not a directory", result.ProjectRoot)
	}
}

// TestSH012_BootstrapFixture_SubStepA_PathShape verifies that the ProjectRoot
// follows the <fixture-root>/<scenario-name>/project/ naming convention per
// SH-016a and ScenarioProjectRoot.
func TestSH012_BootstrapFixture_SubStepA_PathShape(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	scenarioName := "my-scenario"
	result, err := BootstrapFixture(t.Context(), fixtureRoot, scenarioName, nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	// ProjectRoot must equal ScenarioProjectRoot(fixtureRoot, scenarioName).
	wantRoot := ScenarioProjectRoot(fixtureRoot, scenarioName)
	if result.ProjectRoot != wantRoot {
		t.Errorf("BootstrapFixture: ProjectRoot = %q, want %q", result.ProjectRoot, wantRoot)
	}
}

// TestSH012_BootstrapFixture_SubStepB_EventLogDirCreated verifies that
// BootstrapFixture sub-step (b) creates the isolated event-log directory at
// <project-root>/.harmonik/events/ per SH-014.
func TestSH012_BootstrapFixture_SubStepB_EventLogDirCreated(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "evlog-check", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	// The event-log directory must exist and be a directory.
	info, statErr := os.Stat(result.EventLogDir)
	if statErr != nil {
		t.Fatalf("BootstrapFixture: EventLogDir %q: stat error: %v", result.EventLogDir, statErr)
	}
	if !info.IsDir() {
		t.Errorf("BootstrapFixture: EventLogDir %q: exists but is not a directory", result.EventLogDir)
	}
}

// TestSH012_BootstrapFixture_SubStepB_EventLogDirMatchesHelper verifies that
// the EventLogDir in the result equals EventLogDir(ProjectRoot), keeping the
// result consistent with the EventLogDir path helper.
func TestSH012_BootstrapFixture_SubStepB_EventLogDirMatchesHelper(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "evlog-helper", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	want := EventLogDir(result.ProjectRoot)
	if result.EventLogDir != want {
		t.Errorf("BootstrapFixture: EventLogDir = %q, want EventLogDir(ProjectRoot) = %q",
			result.EventLogDir, want)
	}
}

// TestSH012_BootstrapFixture_SubStepB_EventLogDirUnderProjectRoot verifies
// that the event-log directory is under the synthetic project root per SH-014
// (so the operator's .harmonik/ is untouched).
func TestSH012_BootstrapFixture_SubStepB_EventLogDirUnderProjectRoot(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "evlog-under-root", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	if !strings.HasPrefix(result.EventLogDir, result.ProjectRoot+string(filepath.Separator)) {
		t.Errorf("BootstrapFixture: EventLogDir %q is not under ProjectRoot %q; "+
			"operator .harmonik/ would be contaminated (SH-014 violation)",
			result.EventLogDir, result.ProjectRoot)
	}
}

// TestSH012_BootstrapFixture_SubStepC_TwinSearchPathsPreserved verifies that
// BootstrapFixture sub-step (c) copies the caller-supplied twin-binary search
// paths into the result without mutation.
func TestSH012_BootstrapFixture_SubStepC_TwinSearchPathsPreserved(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	inputPaths := []string{"/opt/twins", "/usr/local/twins"}
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "twin-paths", inputPaths)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error: %v", err)
	}

	if len(result.TwinSearchPaths) != len(inputPaths) {
		t.Fatalf("BootstrapFixture: TwinSearchPaths len = %d, want %d",
			len(result.TwinSearchPaths), len(inputPaths))
	}
	for i, want := range inputPaths {
		if result.TwinSearchPaths[i] != want {
			t.Errorf("BootstrapFixture: TwinSearchPaths[%d] = %q, want %q",
				i, result.TwinSearchPaths[i], want)
		}
	}
}

// TestSH012_BootstrapFixture_SubStepC_NilSearchPathsValid verifies that a nil
// twin-binary search path slice is a valid (no-search-path) configuration per
// SH-009 (absolute-path overrides resolve without a prefix).
func TestSH012_BootstrapFixture_SubStepC_NilSearchPathsValid(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)
	result, err := BootstrapFixture(t.Context(), fixtureRoot, "nil-paths", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture: unexpected error on nil twinSearchPaths: %v", err)
	}

	// A nil input may produce an empty non-nil slice; both are valid.
	if result.TwinSearchPaths == nil {
		// nil is also acceptable — caller can range over it safely.
		return
	}
	if len(result.TwinSearchPaths) != 0 {
		t.Errorf("BootstrapFixture: TwinSearchPaths with nil input = %v, want empty or nil",
			result.TwinSearchPaths)
	}
}

// TestSH012_BootstrapFixture_FailFast_BadFixtureRoot verifies that
// BootstrapFixture fails fast when sub-step (a) cannot create the synthetic
// project root (e.g., the fixture root does not exist and cannot be created).
// The failure class MUST be fixture-setup-failed per §8.3.
func TestSH012_BootstrapFixture_FailFast_BadFixtureRoot(t *testing.T) {
	t.Parallel()

	// Use a path that cannot exist (a file acting as a directory component).
	tmpFile, err := os.CreateTemp("", "harmonik-sh012-not-a-dir-")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	// Use the temp file's path as the fixtureRoot — MkdirAll will fail because
	// it is a file, not a directory.
	badFixtureRoot := tmpFile.Name()

	result, bootstrapErr := BootstrapFixture(t.Context(), badFixtureRoot, "bad-root", nil)
	if bootstrapErr == nil {
		t.Errorf("BootstrapFixture: expected error for non-directory fixture root, got result=%+v", result)
		return
	}

	// The failure class must be fixture-setup-failed.
	fc := BootstrapFixtureFailureClass(bootstrapErr)
	if fc != FailureClassFixtureSetupFailed {
		t.Errorf("BootstrapFixtureFailureClass = %q, want %q", fc, FailureClassFixtureSetupFailed)
	}

	// The error must wrap the sentinel so callers can detect it.
	if !errors.Is(bootstrapErr, errFixtureSetupFailed) {
		t.Errorf("BootstrapFixture error does not wrap errFixtureSetupFailed; got: %v", bootstrapErr)
	}
}

// TestSH012_BootstrapFixture_DisjointScenarios verifies that two different
// scenario names produce disjoint results with no shared paths (SH-013
// isolation guarantee).
func TestSH012_BootstrapFixture_DisjointScenarios(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh012FixtureEphemeralRoot(t)

	alpha, err := BootstrapFixture(t.Context(), fixtureRoot, "alpha", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture(alpha): %v", err)
	}

	beta, err := BootstrapFixture(t.Context(), fixtureRoot, "beta", nil)
	if err != nil {
		t.Fatalf("BootstrapFixture(beta): %v", err)
	}

	// Project roots must be disjoint.
	if alpha.ProjectRoot == beta.ProjectRoot {
		t.Errorf("BootstrapFixture: both scenarios share ProjectRoot %q", alpha.ProjectRoot)
	}
	if strings.HasPrefix(alpha.ProjectRoot, beta.ProjectRoot+string(filepath.Separator)) {
		t.Errorf("BootstrapFixture: alpha ProjectRoot %q is under beta %q", alpha.ProjectRoot, beta.ProjectRoot)
	}

	// Event-log dirs must be disjoint.
	if alpha.EventLogDir == beta.EventLogDir {
		t.Errorf("BootstrapFixture: both scenarios share EventLogDir %q", alpha.EventLogDir)
	}
}

// TestSH012_ScenarioWorkspacePath_Shape verifies that ScenarioWorkspacePath
// returns <fixture-root>/<scenario-name>/workspace/ per SH-013.
func TestSH012_ScenarioWorkspacePath_Shape(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/harmonik-harness-abc123"
	scenarioName := "my-scenario"
	got := ScenarioWorkspacePath(fixtureRoot, scenarioName)

	wantSuffix := filepath.Join(scenarioName, "workspace")
	if !strings.HasSuffix(got, string(filepath.Separator)+wantSuffix) &&
		got != filepath.Join(fixtureRoot, wantSuffix) {
		t.Errorf("ScenarioWorkspacePath = %q, want suffix %q", got, wantSuffix)
	}
	want := filepath.Join(fixtureRoot, scenarioName, "workspace")
	if got != want {
		t.Errorf("ScenarioWorkspacePath = %q, want %q", got, want)
	}
}

// TestSH012_ScenarioWorkspacePath_DisjointFromProjectRoot verifies that the
// workspace path and project root path are disjoint (SH-013 requires no
// symlink under one workspace resolves to a path under another; the workspace
// and project root are different sub-trees within the scenario directory).
func TestSH012_ScenarioWorkspacePath_DisjointFromProjectRoot(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/harmonik-harness-abc123"
	scenarioName := "test-scenario"
	workspacePath := ScenarioWorkspacePath(fixtureRoot, scenarioName)
	projectRoot := ScenarioProjectRoot(fixtureRoot, scenarioName)

	if workspacePath == projectRoot {
		t.Errorf("ScenarioWorkspacePath and ScenarioProjectRoot are identical: %q", workspacePath)
	}
	if strings.HasPrefix(workspacePath, projectRoot+string(filepath.Separator)) {
		t.Errorf("workspace path %q is under project root %q; they must be disjoint", workspacePath, projectRoot)
	}
	if strings.HasPrefix(projectRoot, workspacePath+string(filepath.Separator)) {
		t.Errorf("project root %q is under workspace path %q; they must be disjoint", projectRoot, workspacePath)
	}
}

// TestSH012_BootstrapFixtureFailureClass_NilReturnsEmpty verifies that
// BootstrapFixtureFailureClass returns the zero FailureClass for a nil error,
// keeping the failure-class taxonomy closed.
func TestSH012_BootstrapFixtureFailureClass_NilReturnsEmpty(t *testing.T) {
	t.Parallel()

	fc := BootstrapFixtureFailureClass(nil)
	if fc != "" {
		t.Errorf("BootstrapFixtureFailureClass(nil) = %q, want empty string", fc)
	}
}

// TestSH012_BootstrapFixtureFailureClass_ErrorReturnsFixtureSetupFailed
// verifies that any non-nil error from BootstrapFixture produces the
// fixture-setup-failed failure class per §8.3.
func TestSH012_BootstrapFixtureFailureClass_ErrorReturnsFixtureSetupFailed(t *testing.T) {
	t.Parallel()

	// Force a failure by using a non-directory path as the fixture root.
	tmpFile, err := os.CreateTemp("", "harmonik-sh012-fc-check-")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	_, bootstrapErr := BootstrapFixture(t.Context(), tmpFile.Name(), "check", nil)
	if bootstrapErr == nil {
		t.Fatal("BootstrapFixture: expected error, got nil")
	}

	fc := BootstrapFixtureFailureClass(bootstrapErr)
	if fc != FailureClassFixtureSetupFailed {
		t.Errorf("BootstrapFixtureFailureClass(err) = %q, want %q",
			fc, FailureClassFixtureSetupFailed)
	}
	if !fc.Valid() {
		t.Errorf("BootstrapFixtureFailureClass(err).Valid() = false; failure class is not in taxonomy")
	}
}
