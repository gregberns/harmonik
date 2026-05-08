// Package core — EV-007 monotonic time orders intra-process events sensor.
//
// EV-007 (event-model.md §4.2 EV-007): "Within a single emitter process,
// `timestamp_mono_nsec` (when present) MUST be non-decreasing across emissions
// in emission order."
//
// This file is the documentation/discipline sensor for hk-hqwn.10.  The runtime
// enforcement layer (an emitter that reads time.Now().Monotonic and stamps each
// Event, plus a validation path that rejects decreasing readings) is not yet
// built; that behavioral sensor will land on top of the emitter logic in a
// future bead.
//
// Invariant locked by EV-007:
//
//	timestamp_mono_nsec is non-decreasing within a single emitter process
//	→  MUST be non-decreasing across emissions in emission order
//	→  meaningful ONLY for intra-process ordering within the emitter's lifetime
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-007 with canonical required phrases.
//  2. The TimestampMonoNsec field godoc in internal/core/event.go carries the
//     EV-007 non-decreasing invariant.
//  3. A sequence of Event values with non-decreasing TimestampMonoNsec values
//     satisfies the EV-007 ordering predicate; a decreasing sequence does not.
//
// When an emitter-side enforcement layer is implemented, that bead SHOULD either:
//   - Delete the forward-doc marker (TestMonoTsMono_EV007_ForwardDocSensor) and
//     replace it with concrete assertions against the enforcement layer, OR
//   - Extend it with those assertions, retaining the EV-007 citation and hk-hqwn.10
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.10.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// hqwn10SpecContent reads specs/event-model.md, locates the EV-007 anchor, and
// returns the paragraph that contains it.  It fails the test if the file is
// unreadable or the anchor is missing.
func hqwn10SpecContent(t *testing.T) string {
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

	// Confirm the EV-007 section header is present.
	const anchor = "EV-007"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-007 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph starting at the anchor (up to the next section
	// boundary) so callers can assert on its contents.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// hqwn10EventGoContent reads internal/core/event.go and returns the godoc block
// for the TimestampMonoNsec field.  It fails the test if the file is unreadable
// or the field declaration is absent.
//
// The search locates the field declaration line (the line where
// "TimestampMonoNsec" appears followed by its type, preceded by whitespace),
// then walks the preceding lines to collect the associated comment block.
func hqwn10EventGoContent(t *testing.T) string {
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
	// trimmed form starts with "TimestampMonoNsec" followed by whitespace
	// (i.e. not the comment line "// TimestampMonoNsec is …").
	lines := strings.Split(string(raw), "\n")
	fieldLineIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "TimestampMonoNsec") && !strings.HasPrefix(trimmed, "//") {
			fieldLineIdx = i
			break
		}
	}
	if fieldLineIdx < 0 {
		t.Fatal("event.go does not contain a TimestampMonoNsec field declaration; field may have been renamed")
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

// hqwn10MakeEvent returns a minimal valid Event with the given mono timestamp
// value.  When monoNsec is 0 the TimestampMonoNsec field is left nil so the
// helper can produce both nil and set variants.
func hqwn10MakeEvent(t *testing.T, monoNsec int64) Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7 failed: %v", err)
	}
	payload := []byte(`{}`)
	e := Event{
		EventID:         EventID(id),
		SchemaVersion:   1,
		Type:            "run_started",
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         payload,
	}
	if monoNsec > 0 {
		v := monoNsec
		e.TimestampMonoNsec = &v
	}
	return e
}

// hqwn10IsNonDecreasing reports whether the sequence of TimestampMonoNsec values
// across the given events satisfies EV-007: non-decreasing across emissions in
// emission order.  Events with nil TimestampMonoNsec are skipped (EV-007 only
// applies "when present").
func hqwn10IsNonDecreasing(events []Event) bool {
	var last *int64
	for i := range events {
		m := events[i].TimestampMonoNsec
		if m == nil {
			continue
		}
		if last != nil && *m < *last {
			return false
		}
		last = m
	}
	return true
}

// TestMonoTsMono_EV007_SpecContainsNonDecreasingInvariant verifies that the
// EV-007 section of specs/event-model.md encodes the non-decreasing ordering
// invariant with the required canonical phrases.
//
// Phrases required by the invariant (EV-007, hk-hqwn.10):
//
//   - "non-decreasing"      — the ordering constraint; must not be weakened to
//     "increasing" (strict) or softened to "monotonically increasing"
//   - "emission order"      — the ordering reference; must be explicit
//   - "single emitter process" — the process scope; must not be elided
//   - "when present"        — the optionality qualifier; EV-007 only fires when
//     timestamp_mono_nsec is populated
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestMonoTsMono_EV007_SpecContainsNonDecreasingInvariant(t *testing.T) {
	t.Parallel()

	para := hqwn10SpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "non-decreasing",
			hint:   "EV-007 must declare the ordering constraint as non-decreasing; renaming this breaks the hk-hqwn.10 invariant",
		},
		{
			phrase: "emission order",
			hint:   "EV-007 must bound the ordering reference to emission order; the field is meaningful for intra-process ordering only",
		},
		{
			phrase: "single emitter process",
			hint:   "EV-007 must scope the invariant to a single emitter process; without this scope the rule has unbounded cross-process implications",
		},
		{
			phrase: "when present",
			hint:   "EV-007 must qualify the invariant with 'when present' because timestamp_mono_nsec is optional; dropping the qualifier changes the rule from conditional to unconditional",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-007 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestMonoTsMono_EV007_CodeGodocCarriesNonDecreasingInvariant verifies that the
// TimestampMonoNsec field godoc in internal/core/event.go carries the EV-007
// non-decreasing ordering invariant so that the constraint is visible to code
// reviewers without leaving the Go type declaration.
//
// The godoc must mention:
//   - "EV-007"          — spec traceability anchor
//   - "non-decreasing"  — the ordering constraint canonical phrase (EV-007)
func TestMonoTsMono_EV007_CodeGodocCarriesNonDecreasingInvariant(t *testing.T) {
	t.Parallel()

	godoc := hqwn10EventGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "EV-007",
			hint:   "TimestampMonoNsec godoc must cite EV-007 so spec traceability for the non-decreasing invariant is visible at the field declaration",
		},
		{
			phrase: "non-decreasing",
			hint:   "TimestampMonoNsec godoc must contain the canonical EV-007 phrase 'non-decreasing'; advisory or weaker language does not satisfy the normative requirement",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(godoc, tc.phrase) {
			t.Errorf(
				"TimestampMonoNsec godoc in event.go does not contain %q — %s\nGodoc:\n%s",
				tc.phrase, tc.hint, godoc,
			)
		}
	}
}

// TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsValidSequence verifies that
// the hqwn10IsNonDecreasing predicate accepts a sequence of events whose
// timestamp_mono_nsec values are non-decreasing, covering:
//   - strictly increasing values
//   - equal (same tick) values
//   - nil values interspersed (EV-007: "when present")
func TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsValidSequence(t *testing.T) {
	t.Parallel()

	events := []Event{
		hqwn10MakeEvent(t, 100),
		hqwn10MakeEvent(t, 200),
		hqwn10MakeEvent(t, 200), // equal tick — non-decreasing allows equal
		hqwn10MakeEvent(t, 300),
	}

	if !hqwn10IsNonDecreasing(events) {
		t.Error("EV-007: non-decreasing predicate rejected a valid strictly-non-decreasing sequence")
	}
}

// TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsNilInterspersed verifies
// that nil (absent) timestamp_mono_nsec values are skipped by the predicate;
// EV-007 applies only "when present".
func TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsNilInterspersed(t *testing.T) {
	t.Parallel()

	// Events with nil mono are produced by hqwn10MakeEvent(t, 0).
	noMono := hqwn10MakeEvent(t, 0) // TimestampMonoNsec will be nil

	events := []Event{
		hqwn10MakeEvent(t, 100),
		noMono,
		hqwn10MakeEvent(t, 200),
		noMono,
		hqwn10MakeEvent(t, 300),
	}

	if !hqwn10IsNonDecreasing(events) {
		t.Error("EV-007: non-decreasing predicate rejected a sequence with interspersed nil mono values; nil must be skipped per EV-007 'when present' qualifier")
	}
}

// TestMonoTsMono_EV007_NonDecreasingPredicateRejectsDecreasingSequence verifies
// that the predicate correctly rejects a sequence where timestamp_mono_nsec
// decreases between two consecutive present values — the canonical EV-007
// violation.
func TestMonoTsMono_EV007_NonDecreasingPredicateRejectsDecreasingSequence(t *testing.T) {
	t.Parallel()

	events := []Event{
		hqwn10MakeEvent(t, 300),
		hqwn10MakeEvent(t, 100), // decreasing — EV-007 violation
	}

	if hqwn10IsNonDecreasing(events) {
		t.Error("EV-007: non-decreasing predicate accepted a decreasing sequence; it must reject sequences where timestamp_mono_nsec decreases between emissions")
	}
}

// TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsAllNil verifies that a
// sequence containing only events with nil timestamp_mono_nsec is accepted;
// EV-007 constrains only present values.
func TestMonoTsMono_EV007_NonDecreasingPredicateAcceptsAllNil(t *testing.T) {
	t.Parallel()

	events := []Event{
		hqwn10MakeEvent(t, 0),
		hqwn10MakeEvent(t, 0),
		hqwn10MakeEvent(t, 0),
	}

	if !hqwn10IsNonDecreasing(events) {
		t.Error("EV-007: non-decreasing predicate rejected an all-nil sequence; nil values must be treated as absent per EV-007 'when present' qualifier")
	}
}

// TestMonoTsMono_EV007_ForwardDocSensor is a documentation-marker test for
// event-model.md §4.2 EV-007 (hk-hqwn.10).
//
// EV-007 requires that within a single emitter process, timestamp_mono_nsec
// (when present) MUST be non-decreasing across emissions in emission order.
//
// This test skips unconditionally because the emitter-side enforcement layer
// (a runtime path that reads time.Now() monotonic, stamps each Event, and
// rejects decreasing readings) is not yet implemented.
// It exists as a discoverable anchor in the test suite.  When the enforcement
// layer lands, the implementer SHOULD either:
//
//  1. Replace this marker with concrete assertions against the enforcement layer, OR
//  2. Extend it with those assertions, retaining the EV-007 citation and hk-hqwn.10
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.10.
func TestMonoTsMono_EV007_ForwardDocSensor(t *testing.T) {
	t.Log("EV-007 (hk-hqwn.10): within a single emitter process, timestamp_mono_nsec (when present) MUST be non-decreasing across emissions in emission order.")
	t.Log("Spec reference: event-model.md §4.2 EV-007.")
	t.Log("")
	t.Log("Emitter-side enforcement layer not yet implemented.")
	t.Log("When that layer lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the enforcement layer.")
	t.SkipNow()
}
