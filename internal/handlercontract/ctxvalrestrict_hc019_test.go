package handlercontract_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ctxValRestrictFixtureForbiddenKeyPatterns = []string{
	"RunID",
	"runID",
	"run_id",

	"BeadID",
	"beadID",
	"bead_id",

	"WorkflowID",
	"workflowID",
	"workflow_id",
	"NodeID",
	"nodeID",
	"node_id",

	"LaunchSpec",
	"launchSpec",

	"Outcome",
	"outcome",
}

const ctxValRestrictFixtureWithValueCall = "context.WithValue"

var ctxValRestrictFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

func ctxValRestrictFixtureRepoRoot(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("ctxValRestrictFixtureRepoRoot: os.Getwd: %v", err)
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("ctxValRestrictFixtureRepoRoot: go.mod not found above %q", cwd)
		}
		dir = parent
	}
}

func ctxValRestrictFixtureCollectGoSources(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ctxValRestrictFixtureCollectGoSources: WalkDir(%q): %v", root, err)
	}
	return files
}

func ctxValRestrictFixtureViolatingLines(content, keyPattern string) []string {
	var violations []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, ctxValRestrictFixtureWithValueCall) &&
			strings.Contains(line, keyPattern) {
			trimmed := strings.TrimSpace(line)
			violations = append(violations, trimmed)
		}
	}
	return violations
}

// TestCtxValRestrict_HC019_NoBusinessDataInContext is the HC-019 sensor.
//
// It scans every non-test Go source file under the daemon tree (internal/ and
// cmd/) and asserts that no line contains both context.WithValue and a
// business-data key pattern declared in ctxValRestrictFixtureForbiddenKeyPatterns.
//
// The test is intentionally static (no subprocess invocation): the spec's
// verification statement ("context-value lint asserting no business data is
// carried via ctx values", §10.2.HC-017–HC-019) is a source-inspection check.
//
// A failure here means a production file is threading business data (run
// fields, outcomes, bead IDs) through a context value, violating HC-019.
// Fix by removing the context.WithValue call and passing the business datum
// via an explicit parameter, LaunchSpec field, or event payload
// (specs/handler-contract.md §4.4.HC-019).
func TestCtxValRestrict_HC019_NoBusinessDataInContext(t *testing.T) {
	t.Parallel()

	repoRoot := ctxValRestrictFixtureRepoRoot(t)

	var sourceFiles []string
	for _, rel := range ctxValRestrictFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		sourceFiles = append(sourceFiles, ctxValRestrictFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Skip("no Go source files found under scanned roots — nothing to check")
	}

	for _, filePath := range sourceFiles {
		for _, keyPattern := range ctxValRestrictFixtureForbiddenKeyPatterns {
			t.Run(filepath.Base(filePath)+"/"+keyPattern, func(t *testing.T) {
				t.Parallel()

				//nolint:gosec // G304: path is constructed from repo root resolved by go.mod walk; not user-controlled
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("os.ReadFile(%q): %v", filePath, err)
				}

				violations := ctxValRestrictFixtureViolatingLines(string(content), keyPattern)
				if len(violations) == 0 {
					return
				}

				rel, relErr := filepath.Rel(repoRoot, filePath)
				if relErr != nil {
					rel = filePath
				}
				for _, line := range violations {
					t.Errorf(
						"HC-019 violation: %q passes business-data key %q via context.WithValue\n"+
							"  offending line: %s\n"+
							"Context values MUST NOT carry business data (run fields, outcomes, bead IDs).\n"+
							"Pass the datum via an explicit parameter, LaunchSpec field, or event payload\n"+
							"(specs/handler-contract.md §4.4.HC-019).",
						rel, keyPattern, line,
					)
				}
			})
		}
	}
}

// TestCtxValRestrict_HC019_SensorCoverage verifies that the sensor scanned at
// least one file — a meta-test confirming the walker is not silently skipping
// the entire tree.
func TestCtxValRestrict_HC019_SensorCoverage(t *testing.T) {
	t.Parallel()

	repoRoot := ctxValRestrictFixtureRepoRoot(t)

	var total int
	for _, rel := range ctxValRestrictFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		total += len(ctxValRestrictFixtureCollectGoSources(t, root))
	}

	if total == 0 {
		t.Error("HC-019 sensor has no coverage: zero Go source files found under scanned roots (internal/, cmd/); sensor is not operating")
	}
}
