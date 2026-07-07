package specaudit_test

// hk-i0tw.41 binding test — SH-INV-005: scenario files are declarative-loadable.
//
// Spec ref: specs/scenario-harness.md §4.9 SH-INV-005.
//
// SH-INV-005 states: "Every scenario file MUST be loadable by a generic YAML
// parser plus the §6.1 schema validator, with no plugin, no eval, no
// code-execution YAML tags. Sensor: a corpus lint at suite-load time runs every
// scenario file through `gopkg.in/yaml.v3` in strict mode with
// `KnownFields(true)` and a forbidden-tag deny-list (rejecting
// `!!python/object`, `!eval`, `!!binary` constructors carrying executable,
// custom-loader directives, anchors that reference unbound aliases); any
// rejected file is a suite-load failure. The parser is pinned to this
// implementation at v0.1; future revisions MUST justify a parser change as a
// foundation amendment per [architecture.md §4.6]."
//
// # What this test verifies
//
// This file encodes three independent audit frames:
//
//  1. Spec-body audit — SH-INV-005 heading is present in
//     specs/scenario-harness.md and the requirement body contains the required
//     enforcement phrases: gopkg.in/yaml.v3, KnownFields, !!python/object,
//     !eval, !!binary, forbidden-tag deny-list.
//
//  2. Implementation audit — internal/scenario/scenariofile.go encodes the
//     enforcement mechanisms mandated by SH-INV-005: KnownFields(true) and the
//     three forbidden-tag constants. A missing phrase means the implementation
//     has drifted from the spec obligation.
//
//  3. Corpus lint — every *.yaml file under scenarios/ (recursive) MUST be
//     parseable by internal/scenario.ParseScenarioFile without error. An empty
//     or absent scenarios/ directory is not a failure (corpus is allowed to be
//     empty at bootstrap); the test logs the corpus size and skips lint if the
//     directory does not exist.
//
// # Helper prefix
//
// All package-level identifiers in this file use the shiNV005Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/scenario"
)

// shiNV005FixtureRepoRoot resolves the repository root from this test file's
// path. The test file lives at internal/specaudit/sh_inv005_declarative_loadable_test.go;
// the repo root is two directories up.
func shiNV005FixtureRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("shiNV005FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/sh_inv005_declarative_loadable_test.go
	// repo root is two directories up.
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// shiNV005FixtureLoadLines opens path and returns all lines.
func shiNV005FixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()

	//nolint:gosec // G304: path is constructed from runtime.Caller repo-root resolution; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("shiNV005FixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("shiNV005FixtureLoadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// shiNV005FixtureSHINV005Heading matches the SH-INV-005 level-4 requirement
// heading line in specs/scenario-harness.md.
var shiNV005FixtureSHINV005Heading = regexp.MustCompile(`^#### SH-INV-005 —`)

// shiNV005FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the SH-INV-005 requirement body window.
var shiNV005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// shiNV005FixtureBodyWindow is the maximum number of lines after the SH-INV-005
// heading to scan for requirement-body content. Matches the 30-line cap used by
// sibling specaudit tests.
const shiNV005FixtureBodyWindow = 30

// shiNV005FixtureBodyLines returns the lines that form the SH-INV-005
// requirement body: all lines after the heading up to (but not including) the
// next Markdown heading or shiNV005FixtureBodyWindow lines, whichever is first.
//
// Returns (nil, 0, reason) if the heading is not found.
func shiNV005FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if shiNV005FixtureSHINV005Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-INV-005 heading not found; expected '#### SH-INV-005 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + shiNV005FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if shiNV005FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// shiNV005FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func shiNV005FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSHINV005SpecBodyAudit verifies that specs/scenario-harness.md correctly
// declares the SH-INV-005 requirement with all mandated enforcement phrases.
//
// Checked phrases:
//
//	(a) SH-INV-005 heading is present.
//	(b) "gopkg.in/yaml.v3" — parser pinning phrase.
//	(c) "KnownFields" — strict-mode enforcement phrase.
//	(d) "!!python/object" — forbidden-tag deny-list entry.
//	(e) "!eval" — forbidden-tag deny-list entry.
//	(f) "!!binary" — forbidden-tag deny-list entry.
//	(g) "suite-load failure" — consequence of a rejected file.
func TestSHINV005SpecBodyAudit(t *testing.T) {
	t.Parallel()

	repoRoot := shiNV005FixtureRepoRoot(t)
	specPath := filepath.Join(repoRoot, "specs", "scenario-harness.md")

	lines := shiNV005FixtureLoadLines(t, specPath)

	body, headingLineNo, reason := shiNV005FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-INV-005 check(a): %s", reason)
	}
	t.Logf("SH-INV-005 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b",
			needle: "gopkg.in/yaml.v3",
			detail: "SH-INV-005 body must name 'gopkg.in/yaml.v3' as the pinned parser " +
				"(the v0.1 parser-pinning guarantee requires the implementation be named explicitly); " +
				"absence means the parser-pinning obligation has been silently removed",
		},
		{
			id:     "c",
			needle: "KnownFields",
			detail: "SH-INV-005 body must reference 'KnownFields' (the strict-mode flag that rejects " +
				"unknown struct fields); absence means the strict-mode enforcement mechanism has drifted",
		},
		{
			id:     "d",
			needle: "!!python/object",
			detail: "SH-INV-005 body must name '!!python/object' in the forbidden-tag deny-list; " +
				"this is the primary code-execution YAML constructor that MUST be rejected at parse time",
		},
		{
			id:     "e",
			needle: "!eval",
			detail: "SH-INV-005 body must name '!eval' in the forbidden-tag deny-list; " +
				"YAML !eval extensions are forbidden and their explicit naming in the spec is normative",
		},
		{
			id:     "f",
			needle: "!!binary",
			detail: "SH-INV-005 body must name '!!binary' in the forbidden-tag deny-list; " +
				"!!binary constructors carrying executable content are forbidden per the deny-list",
		},
		{
			id:     "g",
			needle: "suite-load failure",
			detail: "SH-INV-005 body must state that a rejected file is a 'suite-load failure'; " +
				"the consequence phrase is load-bearing for implementers mapping parse errors to result records",
		},
	}

	for _, c := range checks {
		c := c
		t.Run("check-"+c.id, func(t *testing.T) {
			t.Parallel()
			if !shiNV005FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-INV-005 check(%s) FAILED: phrase %q not found in SH-INV-005 body window\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-INV-005 body)\n"+
						"  detail:  %s",
					c.id, c.needle, headingLineNo, c.detail,
				)
			}
		})
	}
}

// TestSHINV005ImplementationAudit verifies that
// internal/scenario/scenariofile.go encodes the enforcement mechanisms
// mandated by SH-INV-005.
//
// The implementation MUST contain:
//
//	(a) "KnownFields(true)" — the strict-mode call per SH-INV-005.
//	(b) "!!python/object" — the forbidden-tag constant per SH-INV-005.
//	(c) "!eval" — the forbidden-tag constant per SH-INV-005.
//	(d) "!!binary" — the forbidden-tag constant per SH-INV-005.
//	(e) "SH-INV-005" — the spec citation tying the implementation to the requirement.
func TestSHINV005ImplementationAudit(t *testing.T) {
	t.Parallel()

	repoRoot := shiNV005FixtureRepoRoot(t)
	implPath := filepath.Join(repoRoot, "internal", "scenario", "scenariofile.go")

	if _, err := os.Stat(implPath); os.IsNotExist(err) {
		t.Fatalf("SH-INV-005 impl audit: internal/scenario/scenariofile.go not found at %s; "+
			"the ParseScenarioFile implementation must exist before the sensor can bind to it",
			implPath)
	} else if err != nil {
		t.Fatalf("SH-INV-005 impl audit: stat %s: %v", implPath, err)
	}

	implLines := shiNV005FixtureLoadLines(t, implPath)

	type check struct {
		id     string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "a",
			needle: "KnownFields(true)",
			detail: "ParseScenarioFile MUST call decoder.KnownFields(true) per SH-INV-005; " +
				"absence means the strict-mode enforcement is missing and unknown YAML fields " +
				"will be silently accepted instead of rejected as scenario-load-failure",
		},
		{
			id:     "b",
			needle: `"!!python/object"`,
			detail: `ParseScenarioFile MUST include "!!python/object" in its forbidden-tag deny-list ` +
				"per SH-INV-005; this is the primary code-execution YAML constructor",
		},
		{
			id:     "c",
			needle: `"!eval"`,
			detail: `ParseScenarioFile MUST include "!eval" in its forbidden-tag deny-list per SH-INV-005; ` +
				"YAML !eval extensions execute code during parse and are forbidden",
		},
		{
			id:     "d",
			needle: `"!!binary"`,
			detail: `ParseScenarioFile MUST include "!!binary" in its forbidden-tag deny-list per SH-INV-005; ` +
				"!!binary constructors can carry executable payload and are forbidden",
		},
		{
			id:     "e",
			needle: "SH-INV-005",
			detail: "internal/scenario/scenariofile.go MUST cite SH-INV-005 to maintain the " +
				"traceability link between the implementation and the spec requirement; " +
				"absence means the enforcement rationale is lost and the code is untracked",
		},
	}

	implText := strings.Join(implLines, "\n")
	for _, c := range checks {
		c := c
		t.Run("check-"+c.id, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(implText, c.needle) {
				t.Errorf(
					"SH-INV-005 impl audit check(%s) FAILED: phrase %q not found in internal/scenario/scenariofile.go\n"+
						"  file:   internal/scenario/scenariofile.go\n"+
						"  detail: %s",
					c.id, c.needle, c.detail,
				)
			}
		})
	}
}

// TestSHINV005CorpusLint walks the scenarios/ directory (if present) and
// verifies that every *.yaml file is loadable without error by
// internal/scenario.ParseScenarioFile.
//
// An absent or empty scenarios/ directory is not a test failure — the corpus is
// allowed to be empty at bootstrap; the test logs the corpus size and returns
// early when no scenario files are found.
//
// A scenarios/ directory that contains *.yaml files where any file fails
// ParseScenarioFile IS a test failure: SH-INV-005 requires every scenario file
// to be declarative-loadable, and a load failure is a suite-load failure.
func TestSHINV005CorpusLint(t *testing.T) {
	t.Parallel()

	repoRoot := shiNV005FixtureRepoRoot(t)
	scenariosDir := filepath.Join(repoRoot, "scenarios")

	if _, err := os.Stat(scenariosDir); os.IsNotExist(err) {
		t.Logf("SH-INV-005 corpus lint: scenarios/ directory does not exist at %s; "+
			"corpus is empty at bootstrap — no files to lint",
			scenariosDir)
		return
	} else if err != nil {
		t.Fatalf("SH-INV-005 corpus lint: stat %s: %v", scenariosDir, err)
	}

	// Collect all *.yaml files under scenarios/ recursively, skipping twin-scripts/
	// subdirectories. Files under twin-scripts/ are handler-protocol message streams
	// (twin binary configuration), not ScenarioFile structs; including them in the
	// corpus lint would misidentify them as scenario files and fail KnownFields(true)
	// strict-mode parsing on their heartbeat_mode / messages fields.
	var yamlFiles []string
	walkErr := filepath.Walk(scenariosDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "twin-scripts" {
			return filepath.SkipDir
		}
		if !info.IsDir() && filepath.Ext(path) == ".yaml" {
			yamlFiles = append(yamlFiles, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("SH-INV-005 corpus lint: walk %s: %v", scenariosDir, walkErr)
	}

	if len(yamlFiles) == 0 {
		t.Logf("SH-INV-005 corpus lint: scenarios/ exists but contains no *.yaml files; corpus is empty — no files to lint")
		return
	}

	t.Logf("SH-INV-005 corpus lint: found %d scenario file(s) under scenarios/; running declarative-load check on each", len(yamlFiles))

	for _, yamlPath := range yamlFiles {
		yamlPath := yamlPath
		// Derive a test name from the path relative to scenariosDir.
		relPath, relErr := filepath.Rel(scenariosDir, yamlPath)
		if relErr != nil {
			relPath = yamlPath
		}
		t.Run(relPath, func(t *testing.T) {
			t.Parallel()
			_, parseErr := scenario.ParseScenarioFile(yamlPath)
			if parseErr != nil {
				t.Errorf(
					"SH-INV-005 corpus lint FAILED: scenarios/%s is not declarative-loadable\n"+
						"  error: %v\n"+
						"  SH-INV-005 requires every scenario file to be parseable by gopkg.in/yaml.v3 "+
						"in strict mode with KnownFields(true) and the forbidden-tag deny-list; "+
						"any rejected file is a suite-load failure (specs/scenario-harness.md §4.9)",
					relPath, parseErr,
				)
			}
		})
	}
}
