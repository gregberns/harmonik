package specaudit_test

// hk-hqwn.32 binding test — EV-023 + EV-023a divergence-evidence read with
// post-crash-window guardrail and inconclusive classification (coalesced per §2.3).
//
// Spec ref: specs/event-model.md §4.5 EV-023, EV-023a.
//
// EV-023 states: reconciliation detectors MAY read JSONL tail to detect inconsistency
// between three stores. A divergence-evidence read MUST produce a store_divergence_detected
// event (§8.6.8). The detector MUST determine whether the read covers the post-crash window,
// set post_crash_window: true on divergence events in that window, and corroborate against
// git and Beads before flagging.
//
// EV-023a states: a detector MUST classify evidence as git-corroborated, beads-corroborated,
// or inconclusive. MUST emit store_divergence_detected ONLY for corroborated evidence; for
// inconclusive evidence MUST emit divergence_inconclusive (§8.6.10).
//
// # Audit frame
//
// This test is a spec-corpus sensor. Both EV-023 and EV-023a are verified together
// (coalesced per §2.3). The sensor verifies that both are correctly declared so that:
//
//  1. EV-023 heading is present in specs/event-model.md.
//  2. "store_divergence_detected" is the required output event.
//  3. "post-crash window" guardrail is declared.
//  4. "post_crash_window: true" flag is declared.
//  5. Corroboration against git and Beads before flagging is declared.
//  6. EV-023a heading is present in specs/event-model.md.
//  7. "git-corroborated" classification is named.
//  8. "inconclusive" classification is named.
//  9. "divergence_inconclusive" is the required output for inconclusive evidence.
// 10. Tags: mechanism is present in the EV-023 body window.
//
// # Failure modes
//
//   - EV-023 heading missing.
//   - store_divergence_detected absent.
//   - post-crash window absent.
//   - post_crash_window: true absent.
//   - corroboration requirement absent.
//   - EV-023a heading missing.
//   - git-corroborated classification absent.
//   - inconclusive classification absent.
//   - divergence_inconclusive absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ev023Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// ev023FixtureEventModelPath returns the absolute path to specs/event-model.md.
func ev023FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ev023FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// ev023FixtureEV023Heading matches the EV-023 level-4 requirement heading line.
var ev023FixtureEV023Heading = regexp.MustCompile(`^#### EV-023 —`)

// ev023FixtureEV023aHeading matches the EV-023a level-4 requirement heading line.
var ev023FixtureEV023aHeading = regexp.MustCompile(`^#### EV-023a —`)

// ev023FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ev023FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ev023FixtureTagsMechanism matches a "Tags: mechanism" line.
var ev023FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ev023FixtureBodyWindow is the maximum number of lines to scan after a heading.
const ev023FixtureBodyWindow = 30

// ev023FixtureLoadLines opens specFile and returns all lines.
func ev023FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ev023FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ev023FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ev023FixtureBodyLines returns the body lines for the given heading pattern.
func ev023FixtureBodyLines(lines []string, headingPattern *regexp.Regexp, reqID string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if headingPattern.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, reqID + " heading not found in specs/event-model.md"
	}

	limit := headingIdx + 1 + ev023FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ev023FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ev023FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func ev023FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEV023DivergenceEvidenceReadPostCrashGuardrail is the binding test for hk-hqwn.32.
func TestEV023DivergenceEvidenceReadPostCrashGuardrail(t *testing.T) {
	t.Parallel()

	specFile := ev023FixtureEventModelPath(t)
	lines := ev023FixtureLoadLines(t, specFile)

	ev023Body, ev023LineNo, ev023Reason := ev023FixtureBodyLines(lines, ev023FixtureEV023Heading, "EV-023")
	if ev023Reason != "" {
		t.Fatalf("EV-023 check(1): %s", ev023Reason)
	}
	t.Logf("EV-023 heading found at specs/event-model.md line %d; body window = %d lines",
		ev023LineNo, len(ev023Body))

	ev023aBody, ev023aLineNo, ev023aReason := ev023FixtureBodyLines(lines, ev023FixtureEV023aHeading, "EV-023a")
	if ev023aReason != "" {
		t.Fatalf("EV-023a check(6): %s", ev023aReason)
	}
	t.Logf("EV-023a heading found at specs/event-model.md line %d; body window = %d lines",
		ev023aLineNo, len(ev023aBody))

	// EV-023 body checks.
	t.Run("check-2-store-divergence-detected-event", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023Body, "store_divergence_detected") {
			t.Errorf("EV-023 check(2) FAILED: store_divergence_detected absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023 body)\n"+
				"  detail: EV-023 MUST declare store_divergence_detected (§8.6.8) as the "+
				"required output event for divergence-evidence reads", ev023LineNo)
		}
	})
	t.Run("check-3-post-crash-window-guardrail", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023Body, "post-crash window") {
			t.Errorf("EV-023 check(3) FAILED: post-crash window absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023 body)\n"+
				"  detail: EV-023 MUST declare the post-crash window guardrail to prevent "+
				"false positives from lossy-tail event loss near startup", ev023LineNo)
		}
	})
	t.Run("check-4-post-crash-window-true-flag", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023Body, "post_crash_window: true") {
			t.Errorf("EV-023 check(4) FAILED: post_crash_window: true absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023 body)\n"+
				"  detail: EV-023 MUST declare the post_crash_window: true flag on "+
				"divergence events whose evidence falls inside the post-crash window", ev023LineNo)
		}
	})
	t.Run("check-5-corroboration-against-git-and-beads", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023Body, "git") || !ev023FixtureBodyContains(ev023Body, "Beads") {
			t.Errorf("EV-023 check(5) FAILED: corroboration against git and Beads absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023 body)\n"+
				"  detail: EV-023 MUST require corroboration against git and Beads before "+
				"flagging divergence — authoritative stores must also disagree", ev023LineNo)
		}
	})

	// EV-023a body checks.
	t.Run("check-7-git-corroborated-classification", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023aBody, "git-corroborated") {
			t.Errorf("EV-023a check(7) FAILED: git-corroborated classification absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023a body)\n"+
				"  detail: EV-023a MUST name git-corroborated as one of the three classification "+
				"values — evidence with a commit_hash testable against the git DAG", ev023aLineNo)
		}
	})
	t.Run("check-8-inconclusive-classification", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023aBody, "inconclusive") {
			t.Errorf("EV-023a check(8) FAILED: inconclusive classification absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023a body)\n"+
				"  detail: EV-023a MUST name inconclusive as a classification for evidence "+
				"that cannot be tested against git or Beads", ev023aLineNo)
		}
	})
	t.Run("check-9-divergence-inconclusive-event", func(t *testing.T) {
		t.Parallel()
		if !ev023FixtureBodyContains(ev023aBody, "divergence_inconclusive") {
			t.Errorf("EV-023a check(9) FAILED: divergence_inconclusive absent\n"+
				"  spec: specs/event-model.md line ~%d (EV-023a body)\n"+
				"  detail: EV-023a MUST declare divergence_inconclusive (§8.6.10) as the "+
				"required output for inconclusive evidence — distinguishable from confirmed divergence", ev023aLineNo)
		}
	})

	// Check (10): Tags: mechanism in EV-023 body.
	t.Run("check-10-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range ev023Body {
			if ev023FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EV-023 check(10) FAILED: Tags: mechanism not found in EV-023 body window\n"+
				"  spec: specs/event-model.md line ~%d\n"+
				"  detail: EV-023 carries tag 'mechanism'; absence indicates body was truncated",
				ev023LineNo)
		}
	})

	t.Logf("hk-hqwn.32 audit complete — EV-023 at line %d, EV-023a at line %d",
		ev023LineNo, ev023aLineNo)
}
