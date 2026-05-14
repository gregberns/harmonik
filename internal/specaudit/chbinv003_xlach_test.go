package specaudit_test

// hk-xlach binding test — CHB-INV-003 mechanism-no-cognition sensor.
//
// Spec ref: specs/claude-hook-bridge.md §5 CHB-INV-003.
//
// CHB-INV-003 states: "Every relay-emitted message derives deterministically
// from (stdin payload, env, on-disk artifacts). No cognition participates.
// The bridge is mechanism-tagged per [architecture.md §4.2]."
//
// # Sensor: two layered checks
//
//  1. Spec-corpus check — confirms that the CHB-INV-003 invariant is correctly
//     declared in specs/claude-hook-bridge.md: the heading is present, the
//     "No cognition" phrase is asserted, the determinism phrase is present, the
//     mechanism-tagged cross-reference is present, and Tags: mechanism appears
//     in the body window.
//
//  2. Code-grep check — confirms that internal/hookrelay/*.go (non-test, non-doc)
//     does NOT import any known LLM SDK package path. This is the normative
//     enforcement surface: if a future change introduces an LLM SDK import into
//     the relay package the sensor fails before the spec can be consulted.
//
// # LLM SDK patterns checked
//
// The sensor checks for import paths matching any of the following prefixes or
// substrings (case-insensitive). The list is intentionally broad to catch any
// plausible Go LLM SDK addition:
//
//   - "anthropic" — github.com/anthropics/*, github.com/anthropic-ai/*
//   - "openai"    — github.com/sashabaranov/go-openai, github.com/openai/*
//   - "bedrock"   — AWS Bedrock SDK segments (bedrock-runtime, etc.)
//   - "vertexai"  — cloud.google.com/go/vertexai
//   - "generative-ai-go" — github.com/google/generative-ai-go
//   - "cohere"    — github.com/cohere-ai/*
//   - "langchain" — github.com/tmc/langchaingo
//   - "mistral"   — any Mistral AI Go SDK
//   - "groq"      — any Groq AI Go SDK
//
// # Failure modes
//
//   - Spec check (1a): CHB-INV-003 heading absent from specs/claude-hook-bridge.md.
//   - Spec check (1b): "No cognition" phrase absent from body window.
//   - Spec check (1c): "deterministically" phrase absent from body window.
//   - Spec check (1d): "mechanism-tagged" cross-reference absent from body window.
//   - Spec check (1e): Tags: mechanism line absent from body window.
//   - Code check (2):  a production .go file in internal/hookrelay/ imports an
//     LLM SDK path matching a forbidden pattern.
//
// # Helper prefix
//
// All package-level identifiers in this file use the chbInv003Fixture prefix
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

// chbInv003FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/chbinv003_xlach_test.go
//
// so the repo root is two directories up.
func chbInv003FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("chbInv003FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/specaudit/chbinv003_xlach_test.go
	// internal/ is one up, repo root is two up.
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// chbInv003FixtureSpecPath returns the absolute path to specs/claude-hook-bridge.md.
func chbInv003FixtureSpecPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, "specs", "claude-hook-bridge.md")
}

// chbInv003FixtureHeading matches the CHB-INV-003 level-4 requirement heading
// line in specs/claude-hook-bridge.md.
var chbInv003FixtureHeading = regexp.MustCompile(`^#### CHB-INV-003 —`)

// chbInv003FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the CHB-INV-003 body window.
var chbInv003FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// chbInv003FixtureTagsMechanism matches a "Tags: mechanism" line.
var chbInv003FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// chbInv003FixtureBodyWindow is the maximum number of lines after the
// CHB-INV-003 heading to scan for requirement-body content.
const chbInv003FixtureBodyWindow = 15

// chbInv003FixtureLoadLines opens the file at path and returns all lines.
func chbInv003FixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("chbInv003FixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("chbInv003FixtureLoadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// chbInv003FixtureBodyLines returns the lines comprising the CHB-INV-003
// requirement body: all lines after the CHB-INV-003 heading up to (but not
// including) the next Markdown heading or chbInv003FixtureBodyWindow lines,
// whichever comes first.
//
// Returns (nil, 0, reason) when the heading is not found.
func chbInv003FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if chbInv003FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "CHB-INV-003 heading not found; expected '#### CHB-INV-003 —' in specs/claude-hook-bridge.md"
	}

	limit := headingIdx + 1 + chbInv003FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if chbInv003FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// chbInv003FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive substring match).
func chbInv003FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// chbInv003FixtureLLMPatterns is the set of forbidden import-path substrings
// that identify LLM SDK packages. Matching is case-insensitive.
//
// The relay is mechanism-tagged (CHB-INV-003): it MUST NOT import any LLM SDK.
// This list covers all mainstream Go LLM SDK import paths known as of 2026-05.
var chbInv003FixtureLLMPatterns = []string{
	"anthropic",
	"openai",
	"bedrock",
	"vertexai",
	"generative-ai-go",
	"cohere",
	"langchain",
	"mistral",
	"groq",
}

// chbInv003FixtureImportViolation records a single LLM SDK import violation
// found in the relay package.
type chbInv003FixtureImportViolation struct {
	file       string
	lineNo     int
	importPath string
	pattern    string
}

func (v chbInv003FixtureImportViolation) String() string {
	return fmt.Sprintf("%s:%d import %q (matches pattern %q)", v.file, v.lineNo, v.importPath, v.pattern)
}

// chbInv003FixtureRelayFiles returns the set of production (non-test, non-doc)
// .go files in internal/hookrelay/.
func chbInv003FixtureRelayFiles(t *testing.T, repoRoot string) []string {
	t.Helper()

	relayDir := filepath.Join(repoRoot, "internal", "hookrelay")
	if _, err := os.Stat(relayDir); os.IsNotExist(err) {
		// Package doesn't exist yet; return empty to avoid false-positives.
		t.Logf("chbInv003FixtureRelayFiles: %s does not exist — no files to scan", relayDir)
		return nil
	} else if err != nil {
		t.Fatalf("chbInv003FixtureRelayFiles: stat %s: %v", relayDir, err)
	}

	var files []string
	walkErr := filepath.Walk(relayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Only production .go files; skip test files and doc-only files.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chbInv003FixtureRelayFiles: walk %s: %v", relayDir, walkErr)
	}
	return files
}

// chbInv003FixtureScanImports scans a single .go source file and returns
// violations for any import path that matches a forbidden LLM SDK pattern.
//
// The scanner uses a simple state machine to detect import blocks and single-
// import declarations, then tests each quoted import path against the pattern
// list. This avoids go/parser to keep the sensor free of complex dependencies.
func chbInv003FixtureScanImports(t *testing.T, absPath string) []chbInv003FixtureImportViolation {
	t.Helper()

	//nolint:gosec // G304: path constructed by filepath.Walk over repo-relative internal/hookrelay/; not user input.
	f, err := os.Open(absPath)
	if err != nil {
		t.Fatalf("chbInv003FixtureScanImports: open %s: %v", absPath, err)
	}
	defer func() { _ = f.Close() }()

	var violations []chbInv003FixtureImportViolation
	scanner := bufio.NewScanner(f)
	lineNo := 0
	inImportBlock := false

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Detect start of import block: `import (`.
		if trimmed == "import (" {
			inImportBlock = true
			continue
		}
		// Detect end of import block.
		if inImportBlock && trimmed == ")" {
			inImportBlock = false
			continue
		}

		// Detect single-line import: `import "..."`.
		isSingleImport := strings.HasPrefix(trimmed, `import "`) || strings.HasPrefix(trimmed, "import `")

		if !inImportBlock && !isSingleImport {
			continue
		}

		// Extract the quoted import path from this line.
		importPath := chbInv003FixtureExtractImportPath(trimmed)
		if importPath == "" {
			continue
		}

		// Test against each forbidden LLM SDK pattern.
		lowerPath := strings.ToLower(importPath)
		for _, pattern := range chbInv003FixtureLLMPatterns {
			if strings.Contains(lowerPath, strings.ToLower(pattern)) {
				violations = append(violations, chbInv003FixtureImportViolation{
					file:       absPath,
					lineNo:     lineNo,
					importPath: importPath,
					pattern:    pattern,
				})
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("chbInv003FixtureScanImports: scan %s: %v", absPath, scanErr)
	}
	return violations
}

// chbInv003FixtureExtractImportPath extracts the bare import path (without
// quotes or alias prefix) from a trimmed Go import line.
//
// Handles these forms:
//
//	"github.com/foo/bar"              → github.com/foo/bar
//	alias "github.com/foo/bar"        → github.com/foo/bar
//	import "github.com/foo/bar"       → github.com/foo/bar
//	_ "github.com/foo/bar"            → github.com/foo/bar
func chbInv003FixtureExtractImportPath(trimmed string) string {
	// Strip leading `import ` if present.
	if strings.HasPrefix(trimmed, "import ") {
		trimmed = strings.TrimPrefix(trimmed, "import ")
		trimmed = strings.TrimSpace(trimmed)
	}

	// Strip alias or blank identifier prefix (e.g., `_ `, `alias `, `. `).
	// If the remaining string doesn't start with `"` or `` ` ``, strip the
	// first token and whitespace.
	if len(trimmed) > 0 && trimmed[0] != '"' && trimmed[0] != '`' {
		idx := strings.IndexAny(trimmed, " \t")
		if idx < 0 {
			return ""
		}
		trimmed = strings.TrimSpace(trimmed[idx:])
	}

	if len(trimmed) == 0 {
		return ""
	}

	// Extract between double-quotes or backticks.
	quote := trimmed[0]
	if quote != '"' && quote != '`' {
		return ""
	}
	end := strings.IndexByte(trimmed[1:], quote)
	if end < 0 {
		return ""
	}
	return trimmed[1 : end+1]
}

// TestCHBINV003MechanismNoCognition is the binding test for hk-xlach (CHB-INV-003).
//
// It runs two checks in parallel sub-tests:
//
//  1. Spec-corpus check: opens specs/claude-hook-bridge.md, locates the
//     CHB-INV-003 heading, and validates the required phrases + Tags: mechanism.
//
//  2. Code-grep check: walks internal/hookrelay/*.go (non-test), scans import
//     blocks for LLM SDK patterns, and fails if any match is found.
func TestCHBINV003MechanismNoCognition(t *testing.T) {
	t.Parallel()

	repoRoot := chbInv003FixtureRepoRoot(t)

	// ── Check 1: spec-corpus ───────────────────────────────────────────────────
	t.Run("spec-corpus", func(t *testing.T) {
		t.Parallel()

		specFile := chbInv003FixtureSpecPath(t, repoRoot)
		lines := chbInv003FixtureLoadLines(t, specFile)

		body, headingLineNo, reason := chbInv003FixtureBodyLines(lines)
		if reason != "" {
			t.Fatalf("CHB-INV-003 check(1a): %s", reason)
		}
		t.Logf("CHB-INV-003 heading found at specs/claude-hook-bridge.md line %d; body window = %d lines",
			headingLineNo, len(body))

		type check struct {
			id     string
			label  string
			needle string
			detail string
		}

		checks := []check{
			{
				id:     "1b",
				label:  "no-cognition-phrase",
				needle: "No cognition",
				detail: "CHB-INV-003 body must assert 'No cognition participates' " +
					"(expected phrase 'No cognition'); this is the primary invariant " +
					"— the relay MUST NOT perform any semantic judgment or LLM invocation",
			},
			{
				id:     "1c",
				label:  "deterministically-phrase",
				needle: "deterministically",
				detail: "CHB-INV-003 body must declare that relay outputs derive " +
					"'deterministically' from (stdin payload, env, on-disk artifacts) " +
					"(expected phrase 'deterministically'); this is the positive " +
					"formulation of the mechanism invariant that pairs with 'No cognition'",
			},
			{
				id:     "1d",
				label:  "mechanism-tagged-reference",
				needle: "mechanism-tagged",
				detail: "CHB-INV-003 body must include the 'mechanism-tagged' cross-reference " +
					"to architecture.md §4.2 (expected phrase 'mechanism-tagged'); " +
					"this anchors the relay's ZFC classification to the architecture spec's " +
					"AR-005 evaluation-point tagging requirement",
			},
		}

		for _, c := range checks {
			c := c
			t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
				t.Parallel()
				if !chbInv003FixtureBodyContains(body, c.needle) {
					t.Errorf(
						"CHB-INV-003 check(%s) FAILED: %s\n"+
							"  spec:    specs/claude-hook-bridge.md line ~%d (CHB-INV-003 body)\n"+
							"  missing: %q\n"+
							"  detail:  %s",
						c.id, c.label, headingLineNo, c.needle, c.detail,
					)
				}
			})
		}

		// Check (1e): Tags: mechanism.
		t.Run("check-1e-tags-mechanism", func(t *testing.T) {
			t.Parallel()
			found := false
			for _, line := range body {
				if chbInv003FixtureTagsMechanism.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(
					"CHB-INV-003 check(1e) FAILED: Tags: mechanism not found in CHB-INV-003 body window\n"+
						"  spec:   specs/claude-hook-bridge.md line ~%d (CHB-INV-003 body)\n"+
						"  detail: CHB-INV-003 carries tag 'mechanism'; its absence indicates the "+
						"requirement body has been truncated or the Tags: line removed",
					headingLineNo,
				)
			}
		})

		t.Logf("CHB-INV-003 spec-corpus check complete — heading at line %d, body = %d lines",
			headingLineNo, len(body))
	})

	// ── Check 2: code-grep — relay package imports no LLM SDK ─────────────────
	t.Run("code-grep-no-llm-sdk", func(t *testing.T) {
		t.Parallel()

		relayFiles := chbInv003FixtureRelayFiles(t, repoRoot)
		if len(relayFiles) == 0 {
			t.Log("CHB-INV-003 code-grep: internal/hookrelay/ is empty — nothing to scan")
			return
		}

		var allViolations []chbInv003FixtureImportViolation
		for _, f := range relayFiles {
			vs := chbInv003FixtureScanImports(t, f)
			allViolations = append(allViolations, vs...)
		}

		if len(allViolations) == 0 {
			t.Logf("CHB-INV-003 code-grep PASS: scanned %d relay file(s) — zero LLM SDK imports found "+
				"(specs/claude-hook-bridge.md §5 CHB-INV-003)", len(relayFiles))
			return
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(
			"CHB-INV-003 code-grep FAIL: %d LLM SDK import(s) found in %d relay file(s)\n",
			len(allViolations), len(relayFiles),
		))
		sb.WriteString("(specs/claude-hook-bridge.md §5 CHB-INV-003: the relay is mechanism-tagged;\n")
		sb.WriteString("it MUST NOT import any LLM SDK package)\n\n")
		sb.WriteString("Forbidden patterns: ")
		sb.WriteString(strings.Join(chbInv003FixtureLLMPatterns, ", "))
		sb.WriteString("\n\nViolations:\n")
		for _, v := range allViolations {
			rel, err := filepath.Rel(repoRoot, v.file)
			if err != nil {
				rel = v.file
			}
			sb.WriteString(fmt.Sprintf("  %s:%d import %q (matches pattern %q)\n",
				rel, v.lineNo, v.importPath, v.pattern))
		}
		t.Error(sb.String())
	})
}
