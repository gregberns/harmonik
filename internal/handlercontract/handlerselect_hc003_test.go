package handlercontract_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handlerselect_hc003_test.go — sensor asserting HC-003 (handler selection is
// config-level). The daemon MUST carry zero conditional logic that varies on
// handler-is-twin; real/twin selection is config-level only.
//
// Spec refs: specs/handler-contract.md §4.1.HC-003 and §4.8 (twin parity);
// bead hk-8i31.3.
//
// Helper prefix: handlerselectFixture (per implementer-protocol.md
// §Helper-prefix discipline).

// handlerselectFixtureForbiddenPatterns enumerates the source-code patterns
// that constitute runtime real/twin branching. Each pattern entry is a
// substring search over non-test Go source files in the daemon tree.
//
// The patterns are derived from the normative verification statement in
// specs/handler-contract.md §4.8:
//
//	"Verification: reviewing the daemon codebase yields zero `if isTwin` /
//	`if agent_type == "*-twin"` branches."
//
// Each pattern is chosen to be unambiguous in daemon production code: its
// presence in a non-test .go file indicates conditional logic that varies on
// handler-is-twin, which violates HC-003. Patterns that could appear in
// legitimate string literals (e.g., config-resolution code referencing the
// twin binary name as a resolved value) are intentionally omitted; the
// forbidden set covers only the identifier and runtime-discriminant forms
// named in the spec's verification clause.
var handlerselectFixtureForbiddenPatterns = []string{
	// Identifier forms named directly by the spec's verification statement.
	"isTwin",
	"isTwinHandler",

	// Equality test against the twin agent_type suffix pattern named in the
	// spec's verification statement.
	`agent_type == "*-twin"`,

	// Common Go idiom for twin-suffix discrimination at runtime.
	`HasSuffix(agentType, "-twin")`,
	`HasSuffix(agent_type, "-twin")`,
}

// handlerselectFixtureScannedRoots is the set of source-tree subdirectories
// that constitute the daemon codebase for HC-003 purposes. Test files
// (_test.go) are excluded by the walker.
var handlerselectFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

// handlerselectFixtureRepoRoot resolves the absolute path of the repo root
// (the directory containing `go.mod`) by walking upward from the test file's
// location. Returns t.Fatalf on failure.
func handlerselectFixtureRepoRoot(t *testing.T) string {
	t.Helper()

	// Start from the package directory (determined at build time by the Go
	// toolchain via os.Getwd when tests run). Walk upward to find go.mod.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("handlerselectFixtureRepoRoot: os.Getwd: %v", err)
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("handlerselectFixtureRepoRoot: go.mod not found above %q", cwd)
		}
		dir = parent
	}
}

// handlerselectFixtureCollectGoSources walks root and returns all non-test
// .go files found under it.
func handlerselectFixtureCollectGoSources(t *testing.T, root string) []string {
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
		t.Fatalf("handlerselectFixtureCollectGoSources: WalkDir(%q): %v", root, err)
	}
	return files
}

// TestHandlerSelect_HC003_NoDaemonRuntimeBranching is the HC-003 sensor.
//
// It scans every non-test Go source file under the daemon tree (internal/ and
// cmd/) and asserts that none contain the forbidden real/twin branching
// patterns declared in handlerselectFixtureForbiddenPatterns.
//
// The test is intentionally static (no subprocess invocation): the spec's
// verification statement ("reviewing the daemon codebase yields zero isTwin
// branches") is a source-inspection check, not a runtime check.
//
// A failure here means a daemon source file has introduced conditional logic
// that varies on handler-is-twin, violating HC-003 and HC-INV (§4.8 parity
// invariant). Fix by moving the selection to config resolution at
// workflow-load time per execution-model.md §4.9.
func TestHandlerSelect_HC003_NoDaemonRuntimeBranching(t *testing.T) {
	t.Parallel()

	repoRoot := handlerselectFixtureRepoRoot(t)

	var sourceFiles []string
	for _, rel := range handlerselectFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			// Root doesn't exist yet (e.g., cmd/ before any cmd is added);
			// skip silently — the test is still meaningful for whichever
			// roots do exist.
			continue
		}
		sourceFiles = append(sourceFiles, handlerselectFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Skip("no Go source files found under scanned roots — nothing to check")
	}

	for _, filePath := range sourceFiles {
		filePath := filePath // capture
		for _, pattern := range handlerselectFixtureForbiddenPatterns {
			pattern := pattern // capture
			t.Run(filepath.Base(filePath)+"/"+pattern, func(t *testing.T) {
				t.Parallel()

				//nolint:gosec // G304: path is constructed from repo root resolved by go.mod walk; not user-controlled
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("os.ReadFile(%q): %v", filePath, err)
				}

				if strings.Contains(string(content), pattern) {
					// Report relative path for readability.
					rel, relErr := filepath.Rel(repoRoot, filePath)
					if relErr != nil {
						rel = filePath
					}
					t.Errorf(
						"HC-003 violation: daemon source file %q contains forbidden runtime real/twin branching pattern %q\n"+
							"Handler selection MUST be config-level only (specs/handler-contract.md §4.1.HC-003).\n"+
							"Move the selection to workflow-load-time config resolution (execution-model.md §4.9).",
						rel, pattern,
					)
				}
			})
		}
	}
}

// TestHandlerSelect_HC003_SensorCoverage verifies that the sensor scanned at
// least one file — a meta-test confirming the walker is not silently skipping
// the entire tree.
func TestHandlerSelect_HC003_SensorCoverage(t *testing.T) {
	t.Parallel()

	repoRoot := handlerselectFixtureRepoRoot(t)

	var total int
	for _, rel := range handlerselectFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		total += len(handlerselectFixtureCollectGoSources(t, root))
	}

	if total == 0 {
		t.Error("HC-003 sensor has no coverage: zero Go source files found under scanned roots (internal/, cmd/); sensor is not operating")
	}
}
