//go:build specaudit

package specaudit_test

// hk-sx9r.58 binding test — ON-042 multi-tenancy is explicitly deferred post-MVH.
//
// Spec ref: specs/operator-nfr.md §4.10 ON-042.
//
// ON-042 states: per-project daemon isolation (one daemon per project) is the MVH
// answer to multi-tenancy. Shared LLM budgets, shared operator identity/auth, and
// shared skill registries are acknowledged as real concerns and explicitly deferred
// post-MVH — not dismissed.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that ON-042 is correctly declared
// in the spec so that:
//
//  1. ON-042 heading is present in specs/operator-nfr.md.
//  2. Per-project daemon isolation is declared as the MVH answer to multi-tenancy.
//  3. "Deferred" wording is present (concerns acknowledged, not dismissed).
//  4. Shared operator LLM budgets concern is named.
//  5. Shared operator identity/auth concern is named.
//  6. Shared skill registries concern is named.
//  7. "post-MVH" scoping is declared (deferral is bounded, not open-ended).
//  8. Tags: mechanism is present in the ON-042 body window.
//
// # Failure modes
//
//   - ON-042 heading missing.
//   - Per-project daemon isolation absent.
//   - Deferral wording absent.
//   - Shared LLM budgets absent.
//   - Shared operator identity/auth absent.
//   - Shared skill registries absent.
//   - Post-MVH scoping absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on042Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

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

// on042FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on042FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on042FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on042FixtureON042Heading matches the ON-042 level-4 requirement heading line.
var on042FixtureON042Heading = regexp.MustCompile(`^#### ON-042 —`)

// on042FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on042FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on042FixtureTagsMechanism matches a "Tags: mechanism" line.
var on042FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on042FixtureBodyWindow is the maximum number of lines after the ON-042
// heading to scan for requirement-body content.
const on042FixtureBodyWindow = 30

// on042FixtureLoadLines opens specFile and returns all lines.
func on042FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on042FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on042FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on042FixtureON042BodyLines returns the lines comprising the ON-042 body.
func on042FixtureON042BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on042FixtureON042Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-042 heading not found; expected '#### ON-042 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on042FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on042FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on042FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on042FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON042MultiTenancyDeferredPostMVH is the binding test for hk-sx9r.58.
func TestON042MultiTenancyDeferredPostMVH(t *testing.T) {
	t.Parallel()

	specFile := on042FixtureOperatorNFRPath(t)
	lines := on042FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on042FixtureON042BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-042 check(1): %s", reason)
	}
	t.Logf("ON-042 heading found at specs/operator-nfr.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "2",
			label:  "per-project-daemon-isolation-mvh-answer",
			needle: "per-project daemon isolation",
			detail: "ON-042 body must declare per-project daemon isolation as the MVH answer to " +
				"multi-tenancy (expected phrase 'per-project daemon isolation'); this is the " +
				"normative baseline: one daemon per project is the extent of MVH isolation",
		},
		{
			id:     "3",
			label:  "deferred-not-dismissed",
			needle: "deferred",
			detail: "ON-042 body must use the word 'deferred' (expected phrase 'deferred'); " +
				"the deferral wording distinguishes acknowledged-and-deferred concerns from " +
				"dismissed ones, which is the normative stance of this requirement",
		},
		{
			id:     "4",
			label:  "shared-llm-budgets-named",
			needle: "budget",
			detail: "ON-042 body must name shared operator LLM budgets as a deferred concern " +
				"(expected phrase 'budget'); Anthropic quota is per-account and running N daemons " +
				"does not create N quotas — this is a real post-MVH concern",
		},
		{
			id:     "5",
			label:  "shared-operator-identity-auth-named",
			needle: "identity",
			detail: "ON-042 body must name shared operator identity and auth as a deferred concern " +
				"(expected phrase 'identity'); harmonik attach across N daemons is the same human " +
				"with the same skills — global install conflicts are a shared concern",
		},
		{
			id:     "6",
			label:  "shared-skill-registries-named",
			needle: "skill",
			detail: "ON-042 body must name shared skill registries as a deferred concern " +
				"(expected phrase 'skill'); skills installed machine-wide mean a provisioning " +
				"failure in one project is a global failure surface",
		},
		{
			id:     "7",
			label:  "post-mvh-scoping",
			needle: "post-MVH",
			detail: "ON-042 body must carry 'post-MVH' scoping (expected phrase 'post-MVH'); " +
				"this bounds the deferral and signals that these concerns are addressed in a " +
				"foundation amendment rather than left permanently unresolved",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on042FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-042 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-042 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-042 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on042FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-042 check(8) FAILED: Tags: mechanism not found in ON-042 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-042 body)\n"+
					"  detail: ON-042 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.58 audit complete — ON-042 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
