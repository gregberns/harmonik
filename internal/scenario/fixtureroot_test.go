package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRootFixtureParentDir creates a temporary directory to act as the
// parent (operator-supplied --fixture-root override) in fixture-root tests.
// The directory is cleaned up by t.Cleanup; this simulates the operator's
// chosen parent location, NOT the fixture root itself.
func fixtureRootFixtureParentDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-test-parent-")
	if err != nil {
		t.Fatalf("fixtureRootFixtureParentDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fixtureRootFixtureOSTempSubdir returns a prefix that NewFixtureRoot uses
// when no override is given — used to confirm the returned path is under
// os.TempDir().
func fixtureRootFixtureOSTempSubdir() string {
	return filepath.Join(os.TempDir(), "harmonik-harness-")
}

func TestNewFixtureRoot_CreatesDirectory(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)
	got, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) error = %v", parentDir, err)
	}

	// The returned path must exist and be a directory.
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) = %q: os.Stat error = %v", parentDir, got, err)
	}
	if !info.IsDir() {
		t.Errorf("NewFixtureRoot(%q) = %q: exists but is not a directory", parentDir, got)
	}

	// Clean up the fixture root itself (test isolation).
	t.Cleanup(func() { _ = os.RemoveAll(got) })
}

func TestNewFixtureRoot_AbsolutePathUnderParent(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)
	got, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) error = %v", parentDir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(got) })

	// Result must be an absolute path.
	if !filepath.IsAbs(got) {
		t.Errorf("NewFixtureRoot(%q) = %q: expected absolute path", parentDir, got)
	}

	// Result must be a descendant of parentDir.
	if !strings.HasPrefix(got, parentDir+string(filepath.Separator)) {
		t.Errorf("NewFixtureRoot(%q) = %q: not a descendant of parentDir", parentDir, got)
	}
}

func TestNewFixtureRoot_UniquePerInvocation(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)

	// Two successive invocations MUST produce distinct paths so that prior
	// fixture roots accumulate (SH-016: "prior fixture roots accumulate until
	// the operator deletes them").
	first, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot first call error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(first) })

	second, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot second call error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(second) })

	if first == second {
		t.Errorf("NewFixtureRoot returned the same path on two successive calls: %q", first)
	}
}

func TestNewFixtureRoot_PriorRootAccumulates(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)

	first, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot first call error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(first) })

	// Create the second root — simulates a new suite invocation.
	second, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot second call error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(second) })

	// The first fixture root MUST still exist after the second is created.
	// NewFixtureRoot MUST NOT delete prior fixture roots (SH-016).
	if _, statErr := os.Stat(first); os.IsNotExist(statErr) {
		t.Errorf("NewFixtureRoot deleted prior fixture root %q when creating %q", first, second)
	}
}

func TestNewFixtureRoot_EmptyParentUsesOSTempDir(t *testing.T) {
	t.Parallel()

	// When parentDir is empty, NewFixtureRoot MUST use os.TempDir().
	got, err := NewFixtureRoot("")
	if err != nil {
		t.Fatalf("NewFixtureRoot(\"\") error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(got) })

	// Path must be under os.TempDir().
	tmpPrefix := fixtureRootFixtureOSTempSubdir()
	if !strings.HasPrefix(got, tmpPrefix) {
		t.Errorf("NewFixtureRoot(\"\") = %q: expected path under os.TempDir() with harmonik-harness- prefix (prefix %q)", got, tmpPrefix)
	}
}

func TestNewFixtureRoot_HasHarnessPrefix(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)
	got, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) error = %v", parentDir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(got) })

	// The directory name must carry the "harmonik-harness-" prefix so
	// operators can identify harness fixture roots by visual inspection.
	base := filepath.Base(got)
	const wantPrefix = "harmonik-harness-"
	if !strings.HasPrefix(base, wantPrefix) {
		t.Errorf("NewFixtureRoot(%q) = %q: base %q does not have prefix %q", parentDir, got, base, wantPrefix)
	}
}

func TestScenarioProjectRoot_PathShape(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/harmonik-harness-abc123"
	scenarioName := "my-scenario"
	got := ScenarioProjectRoot(fixtureRoot, scenarioName)

	// Must be an absolute path.
	if !filepath.IsAbs(got) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: expected absolute path", fixtureRoot, scenarioName, got)
	}

	// Must be a descendant of fixtureRoot.
	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: not a descendant of fixtureRoot", fixtureRoot, scenarioName, got)
	}

	// Must end with /<scenarioName>/project per SH-016a.
	wantSuffix := filepath.Join(scenarioName, "project")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: does not end with %q", fixtureRoot, scenarioName, got, wantSuffix)
	}
}

func TestScenarioProjectRoot_DisjointScenarios(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/harmonik-harness-abc123"
	alpha := ScenarioProjectRoot(fixtureRoot, "alpha")
	beta := ScenarioProjectRoot(fixtureRoot, "beta")

	// Two scenarios MUST produce disjoint paths (no shared prefix beyond the
	// fixture root itself).
	if alpha == beta {
		t.Errorf("ScenarioProjectRoot returned same path for different scenario names: %q", alpha)
	}

	if strings.HasPrefix(alpha, beta+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot: alpha %q is under beta %q", alpha, beta)
	}
	if strings.HasPrefix(beta, alpha+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot: beta %q is under alpha %q", beta, alpha)
	}
}

func TestScenarioProjectRoot_UnderFixtureRoot(t *testing.T) {
	t.Parallel()

	fixtureRoot := "/tmp/harmonik-harness-abc123"
	got := ScenarioProjectRoot(fixtureRoot, "some-scenario")

	// The synthetic project root MUST be under the per-suite fixture root.
	// This guarantees that SH-016's "not auto-deleted" rule covers the
	// per-scenario synthetic project root by construction.
	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: not under fixture root", fixtureRoot, "some-scenario", got)
	}
}
