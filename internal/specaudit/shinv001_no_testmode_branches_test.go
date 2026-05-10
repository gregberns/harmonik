package specaudit_test

// hk-i0tw.37 binding test — no test-mode branches in production-imported code.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-001.
//
// SH-INV-001 states: for every package imported by the harness from the
// production tree (daemon, orchestrator, agent-runner, workspace manager, hook
// system, policy engine, event bus, handler implementations), there MUST be
// ZERO conditional branches keyed off "is this a test?" / "is this scenario
// mode?" / "is this a twin?" / "is this a harness invocation?". The harness
// configures the production stack via two explicit surface mutations only:
// handler-config override (SH-008) and working-directory assignment to the
// per-scenario synthetic project root (SH-016a).
//
// # Sensor: 4 layered checks
//
// Applied to every non-test Go source file in the production-imported package
// set (internal/**, cmd/**, tools/**), excluding unit-test packages
// (*_test.go), the testhelpers package (HC-035 in-process fakes carve-out),
// the specaudit package (this package), and the harness's own packages
// (internal/scenario/**, test/**):
//
//  1. Token-set grep (case-insensitive): any of `scenarioMode`, `isTest`,
//     `isTwin`, `harnessMode`, `isFakeRunner`, `useStub`, `cfg.TestMode`
//     triggers a fail.
//
//  2. Regex pattern: `if\s+.*[Tt]est|[Tt]win|[Ss]cenario|[Hh]arness.*Mode`
//     triggers a fail.
//
//  3. Suffix-test patterns: `HasSuffix(<expr>, "-twin")` and
//     `agent_type\s*==\s*"*?-twin"` trigger a fail.
//
//  4. Environment-variable pattern: any identifier matching
//     `HARMONIK_[A-Z_]+_MODE` in production code triggers a fail.
//
// # Failure modes
//
//   - token-set: a forbidden token (`scenarioMode`, `isTest`, etc.) found.
//   - regex-branch: an if-branch pattern matching test/twin/scenario/harness
//     mode was found.
//   - suffix-test: a HasSuffix "-twin" or agent_type == "*-twin" was found.
//   - env-var-mode: a HARMONIK_*_MODE env-var name was found in production code.
//
// # Helper prefix
//
// All package-level identifiers in this file use the shinv001Fixture prefix
// per the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// shinv001FixtureForbiddenTokens is the canonical token-set from SH-INV-001
// check (1). Matching is case-insensitive.
var shinv001FixtureForbiddenTokens = []string{
	"scenarioMode",
	"isTest",
	"isTwin",
	"harnessMode",
	"isFakeRunner",
	"useStub",
	"cfg.TestMode",
}

// shinv001FixtureRegexBranch is SH-INV-001 check (2): an if-branch that
// discriminates on test/twin/scenario/harness mode.
var shinv001FixtureRegexBranch = regexp.MustCompile(`if\s+.*(?:[Tt]est|[Tt]win|[Ss]cenario|[Hh]arness).*[Mm]ode`)

// shinv001FixtureSuffixTwin is SH-INV-001 check (3a): HasSuffix(expr, "-twin").
var shinv001FixtureSuffixTwin = regexp.MustCompile(`HasSuffix\([^,]+,\s*"-twin"\)`)

// shinv001FixtureAgentTypeTwin is SH-INV-001 check (3b): agent_type == "*-twin".
var shinv001FixtureAgentTypeTwin = regexp.MustCompile(`agent_type\s*==\s*"\*?-twin"`)

// shinv001FixtureEnvVarMode is SH-INV-001 check (4): HARMONIK_*_MODE env-var
// name in production code.
var shinv001FixtureEnvVarMode = regexp.MustCompile(`HARMONIK_[A-Z_]+_MODE`)

// shinv001FixtureViolation records a single SH-INV-001 violation.
type shinv001FixtureViolation struct {
	file   string
	lineNo int    // 1-based
	check  string // "token-set", "regex-branch", "suffix-test", "env-var-mode"
	token  string // the matched text
}

func (v shinv001FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d [%s] %q", v.file, v.lineNo, v.check, v.token)
}

// shinv001FixtureRepoRoot resolves the repository root from the test file's
// path. The test file lives at internal/specaudit/shinv001_no_testmode_branches_test.go;
// the repo root is two directories up.
func shinv001FixtureRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("shinv001FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/shinv001_no_testmode_branches_test.go
	// repo root is two directories up
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// shinv001FixtureIsExcluded reports whether a file path is excluded from the
// corpus scan per SH-INV-001's exclusion rules:
//   - unit-test files (*_test.go) — these may use test helpers
//   - internal/testhelpers/** — HC-035 in-process fakes carve-out
//   - internal/specaudit/** — this sensor package itself
//   - internal/scenario/** — harness's own production package
//   - test/** — harness stub/integration/crash packages
func shinv001FixtureIsExcluded(repoRoot, absPath string) bool {
	// Must be a .go file (defensive; callers already filter).
	if !strings.HasSuffix(absPath, ".go") {
		return true
	}

	// Exclude test files.
	if strings.HasSuffix(absPath, "_test.go") {
		return true
	}

	// Compute path relative to repo root for prefix checks.
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		// If we cannot relativize, exclude conservatively.
		return true
	}
	// Normalize to forward slashes for consistent matching.
	rel = filepath.ToSlash(rel)

	// Exclude harness packages.
	exclusionPrefixes := []string{
		"internal/testhelpers/",
		"internal/specaudit/",
		"internal/scenario/",
		"test/",
	}
	for _, pfx := range exclusionPrefixes {
		if strings.HasPrefix(rel, pfx) {
			return true
		}
	}

	return false
}

// shinv001FixtureProductionFiles returns the set of production Go source files
// to scan.  It walks internal/, cmd/, and tools/ under the repo root, applying
// shinv001FixtureIsExcluded to each file.
func shinv001FixtureProductionFiles(t *testing.T, repoRoot string) []string {
	t.Helper()

	scanRoots := []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "cmd"),
		filepath.Join(repoRoot, "tools"),
	}

	var files []string
	for _, root := range scanRoots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			// Root doesn't exist yet; skip silently (future subsystems land here).
			continue
		} else if err != nil {
			t.Fatalf("shinv001FixtureProductionFiles: stat %s: %v", root, err)
		}
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if shinv001FixtureIsExcluded(repoRoot, path) {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("shinv001FixtureProductionFiles: walk %s: %v", root, walkErr)
		}
	}

	if len(files) == 0 {
		t.Logf("SH-INV-001: no production files found to scan (corpus is empty — expected at early bootstrap)")
	}
	return files
}

// shinv001FixtureScanFile scans a single production Go source file and returns
// all SH-INV-001 violations found within it.
func shinv001FixtureScanFile(t *testing.T, absPath string) []shinv001FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path is constructed by filepath.Walk over repo-relative scan roots; not user input.
	f, err := os.Open(absPath)
	if err != nil {
		t.Fatalf("shinv001FixtureScanFile: open %s: %v", absPath, err)
	}
	defer func() { _ = f.Close() }()

	var violations []shinv001FixtureViolation
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		lower := strings.ToLower(line)

		// Check 1: token-set grep (case-insensitive).
		for _, tok := range shinv001FixtureForbiddenTokens {
			if strings.Contains(lower, strings.ToLower(tok)) {
				violations = append(violations, shinv001FixtureViolation{
					file:   absPath,
					lineNo: lineNo,
					check:  "token-set",
					token:  tok,
				})
			}
		}

		// Check 2: regex pattern.
		if m := shinv001FixtureRegexBranch.FindString(line); m != "" {
			violations = append(violations, shinv001FixtureViolation{
				file:   absPath,
				lineNo: lineNo,
				check:  "regex-branch",
				token:  m,
			})
		}

		// Check 3a: HasSuffix(expr, "-twin").
		if m := shinv001FixtureSuffixTwin.FindString(line); m != "" {
			violations = append(violations, shinv001FixtureViolation{
				file:   absPath,
				lineNo: lineNo,
				check:  "suffix-test",
				token:  m,
			})
		}

		// Check 3b: agent_type == "*-twin".
		if m := shinv001FixtureAgentTypeTwin.FindString(line); m != "" {
			violations = append(violations, shinv001FixtureViolation{
				file:   absPath,
				lineNo: lineNo,
				check:  "suffix-test",
				token:  m,
			})
		}

		// Check 4: HARMONIK_*_MODE env-var name.
		if m := shinv001FixtureEnvVarMode.FindString(line); m != "" {
			violations = append(violations, shinv001FixtureViolation{
				file:   absPath,
				lineNo: lineNo,
				check:  "env-var-mode",
				token:  m,
			})
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("shinv001FixtureScanFile: scan %s: %v", absPath, scanErr)
	}
	return violations
}

// TestSHINV001NoTestModeBranches is the binding test for SH-INV-001.
//
// It walks the production-imported package set, applies the four layered
// checks from SH-INV-001, and fails if any forbidden token, branch pattern,
// suffix test, or env-var mode read is found.
//
// The test reports ALL violations found (not just the first) so the full
// failure surface is visible in a single run.
func TestSHINV001NoTestModeBranches(t *testing.T) {
	repoRoot := shinv001FixtureRepoRoot(t)
	files := shinv001FixtureProductionFiles(t, repoRoot)

	var allViolations []shinv001FixtureViolation
	for _, f := range files {
		vs := shinv001FixtureScanFile(t, f)
		allViolations = append(allViolations, vs...)
	}

	if len(allViolations) == 0 {
		t.Logf("SH-INV-001 PASS: scanned %d production file(s) — zero test-mode branches found "+
			"(specs/scenario-harness.md §5 SH-INV-001)", len(files))
		return
	}

	// Relativize paths in violations for readable output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"SH-INV-001 FAIL: %d violation(s) found across %d production file(s)\n",
		len(allViolations), len(files),
	))
	sb.WriteString("(specs/scenario-harness.md §5 SH-INV-001: production-imported packages MUST NOT\n")
	sb.WriteString("contain conditional branches keyed off test/scenario/twin/harness mode)\n\n")
	sb.WriteString("Checks applied:\n")
	sb.WriteString("  1. token-set: scenarioMode, isTest, isTwin, harnessMode, isFakeRunner, useStub, cfg.TestMode\n")
	sb.WriteString("  2. regex-branch: if\\s+.*[Tt]est|[Tt]win|[Ss]cenario|[Hh]arness.*Mode\n")
	sb.WriteString("  3. suffix-test: HasSuffix(expr, \"-twin\") / agent_type == \"*-twin\"\n")
	sb.WriteString("  4. env-var-mode: HARMONIK_*_MODE identifier in production code\n\n")
	sb.WriteString("Violations:\n")
	for _, v := range allViolations {
		// Relativize for cleaner output.
		rel, err := filepath.Rel(repoRoot, v.file)
		if err != nil {
			rel = v.file
		}
		sb.WriteString(fmt.Sprintf("  %s:%d [%s] %q\n", rel, v.lineNo, v.check, v.token))
	}
	t.Error(sb.String())
}
