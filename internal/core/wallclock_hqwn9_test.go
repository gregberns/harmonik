// Package core — EV-006 timestamp_wall cross-process ordering advisory sensor.
//
// EV-006 (event-model.md §4.2 EV-006): "`timestamp_wall` MUST NOT be used for
// ordering decisions across processes; NTP skew, clock adjustments, and
// container-host time sync make it unreliable."
//
// Consumers that need cross-process ordering MUST use event_id (UUIDv7) per
// EV-002. timestamp_wall is for audit, human-readable display, and external
// correlation.
//
// This file is the documentation/discipline sensor for hk-hqwn.9. The runtime
// enforcement layer (consumers that would reject cross-process comparisons on
// timestamp_wall) is not yet built; that behavioral sensor will land on top of
// the consumer logic in a future bead.
//
// Invariant locked by EV-006:
//
//	timestamp_wall is advisory for cross-process ordering
//	→  MUST NOT be used for ordering decisions across processes
//	→  cross-process ordering MUST use event_id (UUIDv7) per EV-002
//	→  timestamp_wall is for audit, human-readable display, and external correlation
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-006 with canonical required phrases.
//  2. The TimestampWall field godoc in internal/core/event.go carries the
//     cross-process-ordering prohibition and EV-002 redirect.
//
// When a consumer-side enforcement layer is implemented, that bead SHOULD either:
//   - Delete the forward-doc marker (TestWallClock_EV006_ForwardDocSensor) and
//     replace it with concrete assertions against the enforcement layer, OR
//   - Extend it with those assertions, retaining the EV-006 citation and hk-hqwn.9
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.9.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// wallClockSensorSpecContent reads specs/event-model.md, locates the EV-006
// anchor, and returns the paragraph that contains it. It fails the test if
// the file is unreadable or the anchor is missing.
func wallClockSensorSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/core/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	// Confirm the EV-006 section header is present.
	const anchor = "EV-006 — Wall clock is advisory"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-006 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph starting at the anchor (up to the next section
	// boundary) so callers can assert on its contents.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// wallClockSensorEventGoContent reads internal/core/event.go and returns the
// godoc block for the TimestampWall field. It fails the test if the file is
// unreadable or the field declaration is absent.
//
// The search locates the field declaration line (the line where "TimestampWall"
// appears followed by its type, preceded by whitespace), then walks the
// preceding lines to collect the associated comment block.
func wallClockSensorEventGoContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate event.go")
	}
	eventGoPath := filepath.Join(filepath.Dir(thisFile), "event.go")

	raw, err := os.ReadFile(eventGoPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", eventGoPath, err)
	}

	// Split into lines and find the field declaration line: a line whose
	// trimmed form starts with "TimestampWall" followed by whitespace
	// (i.e. not the comment line "// TimestampWall is …").
	lines := strings.Split(string(raw), "\n")
	fieldLineIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "TimestampWall") && !strings.HasPrefix(trimmed, "//") {
			fieldLineIdx = i
			break
		}
	}
	if fieldLineIdx < 0 {
		t.Fatal("event.go does not contain a TimestampWall field declaration; field may have been renamed")
	}

	// Walk backwards from the field declaration line to collect the comment block.
	var commentLines []string
	for i := fieldLineIdx - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "//") {
			commentLines = append([]string{line}, commentLines...)
		} else {
			break
		}
	}
	return strings.Join(commentLines, "\n")
}

// TestWallClock_EV006_SpecContainsAdvisoryInvariant verifies that the
// EV-006 section of specs/event-model.md encodes the advisory invariant
// with the required canonical phrases.
//
// Phrases required by the invariant (EV-006, hk-hqwn.9):
//
//   - "MUST NOT be used"        — the prohibition; must be explicit, not advisory
//   - "ordering decisions"      — the forbidden use; must name the decision type
//   - "across processes"        — the scope of the prohibition; must be explicit
//   - "EV-002"                  — the normative redirect for cross-process ordering
//   - "audit"                   — one of the permitted uses; must be named
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestWallClock_EV006_SpecContainsAdvisoryInvariant(t *testing.T) {
	t.Parallel()

	para := wallClockSensorSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "MUST NOT be used",
			hint:   "EV-006 must explicitly prohibit cross-process ordering use; the prohibition must be normative (MUST NOT)",
		},
		{
			phrase: "ordering decisions",
			hint:   "EV-006 must name 'ordering decisions' as the forbidden use of timestamp_wall across processes",
		},
		{
			phrase: "across processes",
			hint:   "EV-006 must scope the prohibition to cross-process comparisons; this is the hk-hqwn.9 invariant boundary",
		},
		{
			phrase: "EV-002",
			hint:   "EV-006 must redirect cross-process ordering consumers to event_id (UUIDv7) per EV-002",
		},
		{
			phrase: "audit",
			hint:   "EV-006 must enumerate audit as a permitted use of timestamp_wall; removing it narrows the spec contract",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-006 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestWallClock_EV006_CodeGodocCarriesAdvisoryConstraint verifies that the
// TimestampWall field godoc in internal/core/event.go carries the EV-006
// cross-process-ordering prohibition so that the constraint is visible to code
// reviewers without leaving the Go type declaration.
//
// The godoc must mention:
//   - "EV-006"                  — spec traceability anchor
//   - "MUST NOT be used"        — the normative canonical prohibition phrase (EV-006);
//     advisory language such as "not recommended" is NOT sufficient
//   - "cross-process ordering"  — must name the forbidden use
//   - "EV-002"                  — must redirect consumers to the correct ordering tool
func TestWallClock_EV006_CodeGodocCarriesAdvisoryConstraint(t *testing.T) {
	t.Parallel()

	godoc := wallClockSensorEventGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "EV-006",
			hint:   "TimestampWall godoc must cite EV-006 so spec traceability is visible at the field declaration",
		},
		{
			phrase: "MUST NOT be used",
			hint:   "TimestampWall godoc must contain the canonical EV-006 prohibition phrase 'MUST NOT be used'; advisory language does not satisfy the normative requirement",
		},
		{
			phrase: "cross-process ordering",
			hint:   "TimestampWall godoc must name 'cross-process ordering' as the forbidden use; this anchors the EV-006 constraint at the type level",
		},
		{
			phrase: "EV-002",
			hint:   "TimestampWall godoc must redirect consumers to EventID (UUIDv7) per EV-002 for cross-process ordering",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(godoc, tc.phrase) {
			t.Errorf(
				"TimestampWall godoc in event.go does not contain %q — %s\nGodoc:\n%s",
				tc.phrase, tc.hint, godoc,
			)
		}
	}
}

// TestWallClock_EV006_ForwardDocSensor is a documentation-marker test for
// event-model.md §4.2 EV-006 (hk-hqwn.9).
//
// EV-006 requires that timestamp_wall MUST NOT be used for ordering decisions
// across processes, and is for audit, human-readable display, and external
// correlation only. Cross-process ordering MUST use event_id (UUIDv7) per EV-002.
//
// This test skips unconditionally because the consumer-side enforcement layer
// (a runtime guard that rejects cross-process ordering on timestamp_wall) is not
// yet implemented. It exists as a discoverable anchor in the test suite. When the
// enforcement layer lands, the implementer SHOULD either:
//
//  1. Replace this marker with concrete assertions against the enforcement layer, OR
//  2. Extend it with those assertions, retaining the EV-006 citation and hk-hqwn.9
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.9.
func TestWallClock_EV006_ForwardDocSensor(t *testing.T) {
	t.Log("EV-006 (hk-hqwn.9): timestamp_wall is advisory for cross-process ordering.")
	t.Log("MUST NOT be used for ordering decisions across processes.")
	t.Log("NTP skew, clock adjustments, and container-host time sync make it unreliable.")
	t.Log("Cross-process ordering MUST use event_id (UUIDv7) per EV-002.")
	t.Log("timestamp_wall is for audit, human-readable display, and external correlation.")
	t.Log("Spec reference: event-model.md §4.2 EV-006.")
	t.Log("")
	t.Log("Consumer-side enforcement layer not yet implemented.")
	t.Log("When that layer lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the enforcement layer.")
	t.SkipNow()
}
