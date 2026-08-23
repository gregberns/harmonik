package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupTempDir(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove temporary directory %q: %v", dir, err)
		}
	})
}

func fixtureRootFixtureParentDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-test-parent-")
	if err != nil {
		t.Fatalf("fixtureRootFixtureParentDir: MkdirTemp: %v", err)
	}
	cleanupTempDir(t, dir)
	return dir
}

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

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) = %q: os.Stat error = %v", parentDir, got, err)
	}
	if !info.IsDir() {
		t.Errorf("NewFixtureRoot(%q) = %q: exists but is not a directory", parentDir, got)
	}

	cleanupTempDir(t, got)
}

func TestNewFixtureRoot_AbsolutePathUnderParent(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)
	got, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot(%q) error = %v", parentDir, err)
	}
	cleanupTempDir(t, got)

	if !filepath.IsAbs(got) {
		t.Errorf("NewFixtureRoot(%q) = %q: expected absolute path", parentDir, got)
	}

	if !strings.HasPrefix(got, parentDir+string(filepath.Separator)) {
		t.Errorf("NewFixtureRoot(%q) = %q: not a descendant of parentDir", parentDir, got)
	}
}

func TestNewFixtureRoot_UniquePerInvocation(t *testing.T) {
	t.Parallel()

	parentDir := fixtureRootFixtureParentDir(t)

	first, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot first call error = %v", err)
	}
	cleanupTempDir(t, first)

	second, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot second call error = %v", err)
	}
	cleanupTempDir(t, second)

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
	cleanupTempDir(t, first)

	second, err := NewFixtureRoot(parentDir)
	if err != nil {
		t.Fatalf("NewFixtureRoot second call error = %v", err)
	}
	cleanupTempDir(t, second)

	if _, statErr := os.Stat(first); os.IsNotExist(statErr) {
		t.Errorf("NewFixtureRoot deleted prior fixture root %q when creating %q", first, second)
	}
}

func TestNewFixtureRoot_EmptyParentUsesOSTempDir(t *testing.T) {
	t.Parallel()

	got, err := NewFixtureRoot("")
	if err != nil {
		t.Fatalf("NewFixtureRoot(\"\") error = %v", err)
	}
	cleanupTempDir(t, got)

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
	cleanupTempDir(t, got)

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

	if !filepath.IsAbs(got) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: expected absolute path", fixtureRoot, scenarioName, got)
	}

	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: not a descendant of fixtureRoot", fixtureRoot, scenarioName, got)
	}

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

	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("ScenarioProjectRoot(%q, %q) = %q: not under fixture root", fixtureRoot, "some-scenario", got)
	}
}
