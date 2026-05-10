package handlercontract_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ctxvalrestrict_hc019_test.go — sensor asserting HC-019 (context values are
// restricted to observability metadata). Production code MUST NOT pass business
// data (run fields, outcomes, bead IDs) via context values; only observability
// metadata (trace IDs, correlation IDs, operator-identity tokens) is permitted.
//
// Spec refs: specs/handler-contract.md §4.4.HC-019;
// bead hk-8i31.23.
//
// Helper prefix: ctxValRestrictFixture (per implementer-protocol.md
// §Helper-prefix discipline).

// ctxValRestrictFixtureForbiddenPatterns enumerates source-code patterns that
// constitute business-data carriage via context values. Each pattern is a
// substring matched against non-test Go source files under the daemon tree.
//
// The patterns are derived from the normative statement in
// specs/handler-contract.md §4.4.HC-019:
//
//	"Context values MUST NOT carry business data (run fields, outcomes,
//	bead IDs). Context values MAY carry observability metadata only:
//	trace IDs, correlation IDs, operator-identity tokens."
//
// Each pattern targets a context.WithValue call whose key argument names a
// business-data concept. The patterns are intentionally narrow — they match
// only the identifier forms most likely to appear in production code — so that
// legitimate observability metadata keys (e.g. "traceID", "correlationID")
// are not flagged.
//
// Pattern rationale:
//
//   - "RunID" / "runID" / "run_id": run identifier — business data per HC-019.
//   - "BeadID" / "beadID" / "bead_id": bead identifier — business data per HC-019.
//   - "WorkflowID" / "workflowID" / "workflow_id": workflow identifier — a run field.
//   - "NodeID" / "nodeID" / "node_id": node identifier — a run field.
//   - "LaunchSpec" / "launchSpec": the full launch specification — business data.
//   - "Outcome" / "outcome": run outcome — business data per HC-019.
//
// Each pattern is combined with "context.WithValue" via the
// ctxValRestrictFixtureContainsBothParts check to avoid false positives: a
// file that merely declares a RunID type does not violate HC-019; only a file
// that passes such a value into context.WithValue does.
var ctxValRestrictFixtureForbiddenKeyPatterns = []string{
	// Run identifier forms named directly by HC-019.
	"RunID",
	"runID",
	"run_id",

	// Bead identifier forms named directly by HC-019.
	"BeadID",
	"beadID",
	"bead_id",

	// Workflow and node identifiers — run fields per execution-model.md §6.1.
	"WorkflowID",
	"workflowID",
	"workflow_id",
	"NodeID",
	"nodeID",
	"node_id",

	// Full launch specification struct — business data per handler-contract.md §6.1.
	"LaunchSpec",
	"launchSpec",

	// Run outcome — business data per HC-019 ("outcomes" are explicitly named).
	"Outcome",
	"outcome",
}

// ctxValRestrictFixtureWithValueCall is the production API that MUST NOT be
// used with business-data keys. Presence of this substring in the same file as
// a forbidden key pattern constitutes an HC-019 violation candidate; the
// per-file line-level check in ctxValRestrictFixtureFilePairsViolated confirms
// they appear on the same logical call.
const ctxValRestrictFixtureWithValueCall = "context.WithValue"

// ctxValRestrictFixtureScannedRoots is the set of source-tree subdirectories
// that constitute the daemon codebase for HC-019 purposes. Test files
// (_test.go) are excluded by the walker.
var ctxValRestrictFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

// ctxValRestrictFixtureRepoRoot resolves the absolute path of the repo root
// (the directory containing `go.mod`) by walking upward from the test file's
// location. Calls t.Fatalf on failure.
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

// ctxValRestrictFixtureCollectGoSources walks root and returns all non-test
// .go files found under it.
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

// ctxValRestrictFixtureViolatingLines returns any lines in content that
// contain both ctxValRestrictFixtureWithValueCall and keyPattern. A line
// containing both is a HC-019 violation candidate: it passes a business-data
// key into context.WithValue.
//
// Returning per-line violations (rather than a file-level bool) gives the
// failure message enough precision to navigate to the offending call without a
// full-tree grep.
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
			// Root doesn't exist yet (e.g., cmd/ before any cmd is added);
			// skip silently — the test is still meaningful for whichever
			// roots do exist.
			continue
		}
		sourceFiles = append(sourceFiles, ctxValRestrictFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Skip("no Go source files found under scanned roots — nothing to check")
	}

	for _, filePath := range sourceFiles {
		filePath := filePath // capture
		for _, keyPattern := range ctxValRestrictFixtureForbiddenKeyPatterns {
			keyPattern := keyPattern // capture
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
