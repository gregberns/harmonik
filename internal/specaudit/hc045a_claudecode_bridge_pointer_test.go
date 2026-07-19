//go:build specaudit

package specaudit_test

// hk-ezo2f binding test — HC-045a: claude-code agent type governed by
// claude-hook-bridge spec.
//
// Spec ref: specs/handler-contract.md §4.10 HC-045a.
//
// HC-045a states: for `agent_type = "claude-code"`, the launch mechanism,
// `.claude/settings.json` materialization, hook-event-to-progress-message
// translation, env-var schema, and failure-mode classification are normatively
// defined by [claude-hook-bridge.md]. This spec (handler-contract) defines the
// cross-handler invariants; the bridge spec defines the claude-code-specific
// realization.
//
// # What this test verifies
//
// Two layered checks:
//
//  1. Spec-corpus check — confirms HC-045a is correctly declared in
//     specs/handler-contract.md:
//     (a) HC-045a heading is present.
//     (b) "claude-code" appears in the body (the scoped agent_type).
//     (c) "claude-hook-bridge.md" appears in the body (the normative pointer).
//     (d) "cross-handler invariants" appears in the body (the delegation
//         rationale — handler-contract owns invariants; bridge owns the
//         claude-code realization).
//     (e) "bridge spec" appears in the body (the realization delegation phrase).
//     (f) Tags: mechanism is present in the body window.
//
//  2. Code-traceability check — confirms that the ClaudeHarness implementation
//     file (internal/daemon/claudeharness.go) contains a cite back to HC-045a.
//     This ensures the normative pointer is anchored in the code that realizes
//     the claude-code agent type, so future readers can trace from the harness
//     implementation to the governing spec requirement.
//
// # Failure modes
//
//   - Spec check (a): HC-045a heading absent from specs/handler-contract.md.
//   - Spec check (b): "claude-code" absent from HC-045a body.
//   - Spec check (c): "claude-hook-bridge.md" absent from HC-045a body.
//   - Spec check (d): "cross-handler invariants" absent from HC-045a body.
//   - Spec check (e): "bridge spec" absent from HC-045a body.
//   - Spec check (f): Tags: mechanism absent from HC-045a body window.
//   - Code check (2): "HC-045a" not cited in internal/daemon/claudeharness.go.
//
// # Helper prefix
//
// All package-level identifiers use the hc045aFixture prefix per the
// implementer-protocol.md helper-prefix discipline (bead hk-ezo2f).

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

// hc045aFixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/hc045a_claudecode_bridge_pointer_test.go
//
// so the repo root is two directories up.
func hc045aFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc045aFixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/specaudit/hc045a_claudecode_bridge_pointer_test.go
	// internal/ is one up; repo root is two up.
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// hc045aFixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md.
func hc045aFixtureHandlerContractPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc045aFixtureClaudeHarnessPath returns the absolute path to
// internal/daemon/claudeharness.go.
func hc045aFixtureClaudeHarnessPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, "internal", "daemon", "claudeharness.go")
}

// hc045aFixtureHC045aHeading matches the HC-045a level-4 requirement heading
// line in specs/handler-contract.md.
var hc045aFixtureHC045aHeading = regexp.MustCompile(`^#### HC-045a —`)

// hc045aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-045a body window.
var hc045aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc045aFixtureTagsMechanism matches a "Tags: mechanism" line.
var hc045aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc045aFixtureBodyWindow is the maximum number of lines after the HC-045a
// heading to scan for requirement-body content.
const hc045aFixtureBodyWindow = 15

// hc045aFixtureLoadLines opens the file at path and returns all lines.
func hc045aFixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known project paths; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("hc045aFixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc045aFixtureLoadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// hc045aFixtureBodyLines returns the lines comprising the HC-045a requirement
// body: all lines after the HC-045a heading up to (but not including) the next
// Markdown heading or hc045aFixtureBodyWindow lines, whichever comes first.
//
// Returns (nil, 0, reason) when the heading is not found.
func hc045aFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc045aFixtureHC045aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0,
			"HC-045a heading not found; expected '#### HC-045a —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc045aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc045aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc045aFixtureBodyContains reports whether any line in body contains substr
// (case-insensitive substring match).
func hc045aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC045aClaudeCodeBridgePointer is the binding test for hk-ezo2f (HC-045a).
//
// It runs two checks:
//
//  1. Spec-corpus check: opens specs/handler-contract.md, locates the HC-045a
//     heading, and validates the required phrases and Tags: mechanism.
//
//  2. Code-traceability check: verifies that internal/daemon/claudeharness.go
//     contains a "HC-045a" citation so the harness implementation is traceable
//     back to its governing spec requirement.
func TestHC045aClaudeCodeBridgePointer(t *testing.T) {
	t.Parallel()

	repoRoot := hc045aFixtureRepoRoot(t)

	// ── Check 1: spec-corpus ───────────────────────────────────────────────────
	t.Run("spec-corpus", func(t *testing.T) {
		t.Parallel()

		specFile := hc045aFixtureHandlerContractPath(t, repoRoot)
		lines := hc045aFixtureLoadLines(t, specFile)

		body, headingLineNo, reason := hc045aFixtureBodyLines(lines)
		if reason != "" {
			t.Fatalf("HC-045a check(a): %s", reason)
		}
		t.Logf("HC-045a heading found at specs/handler-contract.md line %d; body window = %d lines",
			headingLineNo, len(body))

		type check struct {
			id     string
			label  string
			needle string
			detail string
		}

		checks := []check{
			{
				id:     "b",
				label:  "claude-code-agent-type-scoping",
				needle: "claude-code",
				detail: "HC-045a body must name 'claude-code' as the scoped agent type " +
					"(expected substring 'claude-code'); this scoping clause is the load-bearing " +
					"gate that restricts the bridge spec's authority to the claude-code realization " +
					"— other agent types implement their own bridge specs post-MVH",
			},
			{
				id:     "c",
				label:  "claude-hook-bridge-reference",
				needle: "claude-hook-bridge.md",
				detail: "HC-045a body must reference 'claude-hook-bridge.md' as the normative " +
					"authority (expected substring 'claude-hook-bridge.md'); this is the pointer " +
					"from handler-contract to the bridge spec — without it HC-045a is not a " +
					"functional cross-reference and implementers cannot find the claude-code-specific rules",
			},
			{
				id:     "d",
				label:  "cross-handler-invariants-delegation",
				needle: "cross-handler invariants",
				detail: "HC-045a body must state that handler-contract defines 'cross-handler invariants' " +
					"(expected phrase 'cross-handler invariants'); this distinguishes the handler-contract " +
					"scope (cross-cutting invariants applicable to all agent types) from the bridge spec " +
					"scope (claude-code-specific realization details)",
			},
			{
				id:     "e",
				label:  "bridge-spec-realization-delegation",
				needle: "bridge spec",
				detail: "HC-045a body must delegate the claude-code-specific realization to the 'bridge spec' " +
					"(expected phrase 'bridge spec'); this is the positive formulation of the delegation " +
					"that pairs with the 'cross-handler invariants' clause — together they define the " +
					"authority boundary between handler-contract and claude-hook-bridge.md",
			},
		}

		for _, c := range checks {
			c := c
			t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
				t.Parallel()
				if !hc045aFixtureBodyContains(body, c.needle) {
					t.Errorf(
						"HC-045a check(%s) FAILED: %s\n"+
							"  spec:    specs/handler-contract.md line ~%d (HC-045a body)\n"+
							"  missing: %q\n"+
							"  detail:  %s",
						c.id, c.label, headingLineNo, c.needle, c.detail,
					)
				}
			})
		}

		// Check (f): Tags: mechanism
		t.Run("check-f-tags-mechanism", func(t *testing.T) {
			t.Parallel()
			found := false
			for _, line := range body {
				if hc045aFixtureTagsMechanism.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(
					"HC-045a check(f) FAILED: Tags: mechanism not found in HC-045a body window\n"+
						"  spec:   specs/handler-contract.md line ~%d (HC-045a body)\n"+
						"  detail: HC-045a carries tag 'mechanism'; its absence indicates the "+
						"requirement body has been truncated or the Tags: line removed",
					headingLineNo,
				)
			}
		})

		t.Logf("HC-045a spec-corpus check complete — heading at line %d, body = %d lines",
			headingLineNo, len(body))
	})

	// ── Check 2: code-traceability — claudeharness.go cites HC-045a ────────────
	t.Run("code-traceability-claudeharness", func(t *testing.T) {
		t.Parallel()

		harnessFile := hc045aFixtureClaudeHarnessPath(t, repoRoot)
		lines := hc045aFixtureLoadLines(t, harnessFile)

		found := false
		for _, line := range lines {
			if strings.Contains(line, "HC-045a") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-045a code-traceability FAILED: 'HC-045a' not cited in %s\n"+
					"  detail: internal/daemon/claudeharness.go is the ClaudeHarness implementation — "+
					"the concrete realization of agent_type='claude-code' in the daemon. "+
					"It MUST cite HC-045a so that a future reader can trace from the code "+
					"to the governing spec requirement. "+
					"Add 'HC-045a' to the file-level or type-level doc comment's Spec: line "+
					"(e.g. '// Spec: specs/handler-contract.md §4.10 HC-045a; ...').",
				harnessFile,
			)
		} else {
			t.Logf("HC-045a code-traceability PASS: 'HC-045a' found in internal/daemon/claudeharness.go")
		}
	})
}
