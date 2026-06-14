package handlercontract_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// handlerselect_hc003a_test.go — sensor asserting HC-003a (workflow-mode is
// dispatch-level, not a handler-selector). Handler selection MUST be
// mode-agnostic: the harness resolver MUST NOT inspect workflow_mode, and
// adapter implementations MUST NOT branch on workflow_mode.
//
// Spec refs: specs/handler-contract.md §4.1.HC-003a and §6.1;
// bead hk-c6idw.
//
// Helper prefix: hc003aFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// # Invariants enforced
//
//  1. HC-003a spec section exists — the requirement is normatively declared
//     in specs/handler-contract.md.
//
//  2. Harness resolver is mode-agnostic — internal/daemon/harnessresolve.go
//     MUST NOT reference WorkflowMode in any non-comment line. Handler
//     selection inputs are bead labels, queue default, node default, and
//     global default; workflow_mode is not an input.
//
//  3. Adapter implementations are mode-agnostic — the adapter files
//     (adapter_claudecode.go, adapter_codex.go, and any future adapters
//     matching adapter_*.go under internal/handler/) MUST NOT branch on
//     WorkflowMode. The adapter surface (§4.3.HC-013) MUST NOT expand to
//     accommodate workflow-mode dispatch per HC-003a.
//
// # Failure modes
//
//   - HC-003a heading missing: HC-003a section not declared in the spec.
//   - harnessresolve.go references WorkflowMode: the harness resolver has
//     started using workflow_mode for handler selection, violating HC-003a.
//   - adapter file references WorkflowMode: an adapter has branched on
//     workflow_mode, expanding the adapter surface for mode dispatch.

// hc003aFixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md from the repo root derived via go.mod walk.
func hc003aFixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(hc003aFixtureRepoRoot(t), "specs", "handler-contract.md")
}

// hc003aFixtureRepoRoot resolves the absolute repo root by walking upward
// from the test package directory until go.mod is found.
func hc003aFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("hc003aFixtureRepoRoot: os.Getwd: %v", err)
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("hc003aFixtureRepoRoot: go.mod not found above %q", cwd)
		}
		dir = parent
	}
}

// hc003aFixtureLoadLines opens path and returns all lines.
func hc003aFixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from go.mod walk + known relative paths; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("hc003aFixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("hc003aFixtureLoadLines: scan %s: %v", path, err)
	}
	return lines
}

// hc003aFixtureHC003aHeading matches the HC-003a level-4 requirement heading.
var hc003aFixtureHC003aHeading = regexp.MustCompile(`^#### HC-003a`)

// hc003aFixtureModeAgnosticClause matches the "mode-agnostic" prose fragment
// that must appear in the HC-003a spec section.
var hc003aFixtureModeAgnosticClause = regexp.MustCompile(`mode-agnostic`)

// TestHC003a_SpecSectionExists verifies that handler-contract.md declares the
// HC-003a requirement and includes the observational-only constraint prose.
func TestHC003a_SpecSectionExists(t *testing.T) {
	t.Parallel()

	specPath := hc003aFixtureHandlerContractPath(t)
	lines := hc003aFixtureLoadLines(t, specPath)

	const windowLines = 30
	foundHeading := false
	foundObservational := false

	for i, line := range lines {
		if hc003aFixtureHC003aHeading.MatchString(line) {
			foundHeading = true
			// Scan the body window for the observational clause.
			end := i + 1 + windowLines
			if end > len(lines) {
				end = len(lines)
			}
			for _, bodyLine := range lines[i+1 : end] {
				if hc003aFixtureModeAgnosticClause.MatchString(bodyLine) {
					foundObservational = true
					break
				}
			}
			break
		}
	}

	if !foundHeading {
		t.Errorf("HC-003a: specs/handler-contract.md does not contain '#### HC-003a' heading; " +
			"the workflow-mode dispatch-level invariant MUST be declared normatively")
	}
	if !foundObservational {
		t.Errorf("HC-003a: HC-003a body window (first %d lines after heading) does not contain "+
			"'mode-agnostic'; the spec MUST declare that watcher behavior is mode-agnostic", windowLines)
	}
}

// hc003aFixtureHarnessResolvePath returns the absolute path to
// internal/daemon/harnessresolve.go.
func hc003aFixtureHarnessResolvePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(hc003aFixtureRepoRoot(t), "internal", "daemon", "harnessresolve.go")
}

// hc003aFixtureGoCommentRE matches Go comment lines (//-style).
var hc003aFixtureGoCommentRE = regexp.MustCompile(`^\s*//`)

// hc003aFixtureWorkflowModeRE matches any occurrence of the WorkflowMode
// identifier or the HARMONIK_WORKFLOW_MODE env-var string — both constitute
// handler-selection awareness of workflow_mode that is forbidden in the
// harness resolver.
var hc003aFixtureWorkflowModeRE = regexp.MustCompile(`WorkflowMode|workflow_mode|HARMONIK_WORKFLOW_MODE`)

// TestHC003a_HarnessResolverIsModeAgnostic verifies that harnessresolve.go
// contains no WorkflowMode reference outside of comments.
//
// Handler selection inputs are bead labels, queue default, node default, and
// global Config.DefaultHarness; workflow_mode MUST NOT influence handler
// selection per §4.1.HC-003a.
func TestHC003a_HarnessResolverIsModeAgnostic(t *testing.T) {
	t.Parallel()

	resolverPath := hc003aFixtureHarnessResolvePath(t)
	if _, err := os.Stat(resolverPath); os.IsNotExist(err) {
		t.Skip("harnessresolve.go not yet present — no check needed")
	}

	lines := hc003aFixtureLoadLines(t, resolverPath)

	for i, line := range lines {
		// Skip pure comment lines.
		if hc003aFixtureGoCommentRE.MatchString(line) {
			continue
		}
		if hc003aFixtureWorkflowModeRE.MatchString(line) {
			t.Errorf(
				"HC-003a violation: harnessresolve.go line %d references WorkflowMode/workflow_mode:\n\t%s\n"+
					"Handler selection MUST be workflow-mode-agnostic "+
					"(specs/handler-contract.md §4.1.HC-003a). "+
					"Handler selection inputs are bead labels, queue, node, and global defaults only.",
				i+1, strings.TrimSpace(line),
			)
		}
	}
}

// hc003aFixtureAdapterFiles returns the set of non-test Go source files whose
// names match adapter_*.go under internal/handler/. These are the adapter
// implementation files that MUST NOT branch on WorkflowMode.
func hc003aFixtureAdapterFiles(t *testing.T) []string {
	t.Helper()
	handlerDir := filepath.Join(hc003aFixtureRepoRoot(t), "internal", "handler")
	entries, err := os.ReadDir(handlerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("hc003aFixtureAdapterFiles: ReadDir(%q): %v", handlerDir, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "adapter_") &&
			strings.HasSuffix(name, ".go") &&
			!strings.HasSuffix(name, "_test.go") {
			files = append(files, filepath.Join(handlerDir, name))
		}
	}
	return files
}

// TestHC003a_AdaptersAreModeAgnostic verifies that no adapter_*.go file in
// internal/handler/ references WorkflowMode outside of comments.
//
// The adapter surface (§4.3.HC-013) MUST NOT expand to accommodate
// workflow-mode dispatch per HC-003a. Adapters receive events and sessions;
// they MUST remain oblivious to the dispatched workflow_mode.
func TestHC003a_AdaptersAreModeAgnostic(t *testing.T) {
	t.Parallel()

	adapterFiles := hc003aFixtureAdapterFiles(t)
	if len(adapterFiles) == 0 {
		t.Skip("no adapter_*.go files found under internal/handler/ — nothing to check")
	}

	for _, adapterPath := range adapterFiles {
		adapterPath := adapterPath // capture
		t.Run(filepath.Base(adapterPath), func(t *testing.T) {
			t.Parallel()

			lines := hc003aFixtureLoadLines(t, adapterPath)

			for i, line := range lines {
				// Skip pure comment lines.
				if hc003aFixtureGoCommentRE.MatchString(line) {
					continue
				}
				if hc003aFixtureWorkflowModeRE.MatchString(line) {
					t.Errorf(
						"HC-003a violation: adapter file %q line %d references WorkflowMode/workflow_mode:\n\t%s\n"+
							"The adapter surface MUST NOT expand to accommodate workflow-mode dispatch "+
							"(specs/handler-contract.md §4.1.HC-003a, §4.3.HC-013). "+
							"Adapter methods receive EventEnvelope/Session; they must be mode-agnostic.",
						filepath.Base(adapterPath), i+1, strings.TrimSpace(line),
					)
				}
			}
		})
	}
}

// TestHC003a_SensorCoverage verifies that the harness resolver and at least
// one adapter file exist on disk, confirming the sensors are not silently
// skipping all checks.
func TestHC003a_SensorCoverage(t *testing.T) {
	t.Parallel()

	repoRoot := hc003aFixtureRepoRoot(t)

	resolverPath := filepath.Join(repoRoot, "internal", "daemon", "harnessresolve.go")
	if _, err := os.Stat(resolverPath); err != nil {
		t.Errorf("HC-003a sensor: harnessresolve.go not found at expected path %q; "+
			"sensor cannot assert the mode-agnostic invariant for handler selection", resolverPath)
	}

	adapterFiles := hc003aFixtureAdapterFiles(t)
	if len(adapterFiles) == 0 {
		t.Errorf("HC-003a sensor: no adapter_*.go files found under internal/handler/; " +
			"sensor cannot assert the adapter mode-agnostic invariant")
	}
}
