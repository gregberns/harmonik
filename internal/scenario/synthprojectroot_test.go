package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticRootFixtureParentDir creates a temporary directory to act as the
// per-suite fixture root in synthetic-project-root tests. The directory is
// cleaned up by t.Cleanup.
func syntheticRootFixtureParentDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-test-fixture-")
	if err != nil {
		t.Fatalf("syntheticRootFixtureParentDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestSynthesizeProjectRoot_CreatesDirectory(t *testing.T) {
	t.Parallel()

	fixtureRoot := syntheticRootFixtureParentDir(t)
	got, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "alpha")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot(%q, %q) error = %v", fixtureRoot, "alpha", err)
	}

	// The returned path must exist and be a directory.
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot returned path %q: os.Stat error = %v", got, err)
	}
	if !info.IsDir() {
		t.Errorf("SynthesizeProjectRoot returned %q: exists but is not a directory", got)
	}
}

func TestSynthesizeProjectRoot_PathShape(t *testing.T) {
	t.Parallel()

	fixtureRoot := syntheticRootFixtureParentDir(t)
	got, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "my-scenario")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot(%q, %q) error = %v", fixtureRoot, "my-scenario", err)
	}

	// Result must be an absolute path.
	if !filepath.IsAbs(got) {
		t.Errorf("SynthesizeProjectRoot returned %q: expected absolute path", got)
	}

	// Result must be a descendant of fixtureRoot.
	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("SynthesizeProjectRoot returned %q: not a descendant of fixtureRoot %q", got, fixtureRoot)
	}

	// Result must end with /<scenarioName>/project per SH-016a.
	wantSuffix := filepath.Join("my-scenario", "project")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("SynthesizeProjectRoot returned %q: does not end with %q", got, wantSuffix)
	}
}

func TestSynthesizeProjectRoot_PathMatchesScenarioProjectRoot(t *testing.T) {
	t.Parallel()

	fixtureRoot := syntheticRootFixtureParentDir(t)
	scenarioName := "suite-scenario"

	got, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, scenarioName)
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot error = %v", err)
	}

	// The returned path MUST equal ScenarioProjectRoot(fixtureRoot, scenarioName).
	// This invariant guarantees that callers who compute the path ahead of time
	// (e.g., to pass it as the daemon CWD) get the same path that was created.
	want := ScenarioProjectRoot(fixtureRoot, scenarioName)
	if got != want {
		t.Errorf("SynthesizeProjectRoot returned %q, want ScenarioProjectRoot result %q", got, want)
	}
}

func TestSynthesizeProjectRoot_ContainsGitRepo(t *testing.T) {
	t.Parallel()

	fixtureRoot := syntheticRootFixtureParentDir(t)
	projectRoot, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "git-check")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot error = %v", err)
	}

	// A fresh git repository must be present: .git/ directory must exist.
	gitDir := filepath.Join(projectRoot, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot: expected .git directory at %q: %v", gitDir, err)
	}
	if !info.IsDir() {
		t.Errorf("SynthesizeProjectRoot: %q exists but is not a directory", gitDir)
	}
}

func TestSynthesizeProjectRoot_NoHarmonikDir(t *testing.T) {
	t.Parallel()

	// The harness MUST NOT pre-create .harmonik/ — the daemon mints it at
	// PL-005 step 0. Verify that SynthesizeProjectRoot does not create it.
	fixtureRoot := syntheticRootFixtureParentDir(t)
	projectRoot, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "no-harmonik")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot error = %v", err)
	}

	harmonikDir := filepath.Join(projectRoot, ".harmonik")
	if _, statErr := os.Stat(harmonikDir); !os.IsNotExist(statErr) {
		t.Errorf("SynthesizeProjectRoot pre-created .harmonik/ at %q; the daemon must mint it at PL-005 step 0", harmonikDir)
	}
}

func TestSynthesizeProjectRoot_DisjointScenarios(t *testing.T) {
	t.Parallel()

	// Two different scenario names MUST produce disjoint project roots so that
	// each scenario IS a different project from the daemon's perspective (SH-016a,
	// PL-001 one-daemon-per-project).
	fixtureRoot := syntheticRootFixtureParentDir(t)

	alpha, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "alpha")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot alpha error = %v", err)
	}

	beta, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "beta")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot beta error = %v", err)
	}

	if alpha == beta {
		t.Errorf("SynthesizeProjectRoot returned same path %q for different scenario names", alpha)
	}

	if strings.HasPrefix(alpha, beta+string(filepath.Separator)) {
		t.Errorf("SynthesizeProjectRoot: alpha %q is under beta %q", alpha, beta)
	}
	if strings.HasPrefix(beta, alpha+string(filepath.Separator)) {
		t.Errorf("SynthesizeProjectRoot: beta %q is under alpha %q", beta, alpha)
	}
}

func TestSynthesizeProjectRoot_UnderFixtureRoot(t *testing.T) {
	t.Parallel()

	// The synthetic project root MUST be under the per-suite fixture root so
	// that SH-016's "not auto-deleted" rule covers the per-scenario data by
	// construction. Concurrent harness invocations create independent fixture
	// roots and MUST NOT contend for any shared resource (SH-031).
	fixtureRoot := syntheticRootFixtureParentDir(t)
	got, err := SynthesizeProjectRoot(t.Context(), fixtureRoot, "under-root-check")
	if err != nil {
		t.Fatalf("SynthesizeProjectRoot error = %v", err)
	}

	if !strings.HasPrefix(got, fixtureRoot+string(filepath.Separator)) {
		t.Errorf("SynthesizeProjectRoot returned %q: not under fixture root %q", got, fixtureRoot)
	}
}
