// Package core — EV-003 timestamp_mono_nsec process-scope sensor.
//
// EV-003 (event-model.md §4.1 EV-003): "`timestamp_mono_nsec` is process-scoped
// and NOT cross-process-comparable."
//
// When present, `timestamp_mono_nsec` MUST be a monotonic nanosecond reading from
// the emitter's process.  It MUST NOT be compared across daemon restarts or across
// processes; it is meaningful ONLY for intra-process ordering within the emitter's
// lifetime.
//
// This file is the documentation/discipline sensor for hk-hqwn.6.  The runtime
// enforcement layer (consumers that would reject cross-process comparisons) is not
// yet built; that behavioral sensor will land on top of the consumer logic in a
// future bead.
//
// Invariant locked by EV-003:
//
//	timestamp_mono_nsec is process-scoped
//	→  MUST NOT be compared across daemon restarts or across processes
//	→  meaningful ONLY for intra-process ordering within the emitter's lifetime
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-003 with canonical required phrases.
//  2. The TimestampMonoNsec field godoc in internal/core/event.go carries the
//     cross-process-comparison prohibition.
//
// When a consumer-side enforcement layer is implemented, that bead SHOULD either:
//   - Delete the forward-doc marker (TestMonoTsMono_EV003_ForwardDocSensor) and
//     replace it with concrete assertions against the enforcement layer, OR
//   - Extend it with those assertions, retaining the EV-003 citation and hk-hqwn.6
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.6.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// monoNsecSensorSpecContent reads specs/event-model.md, locates the EV-003
// anchor, and returns the paragraph that contains it.  It fails the test if
// the file is unreadable or the anchor is missing.
func monoNsecSensorSpecContent(t *testing.T) string {
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

	// Confirm the EV-003 section header is present.
	const anchor = "EV-003"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-003 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph starting at the anchor (up to the next section
	// boundary) so callers can assert on its contents.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// monoNsecSensorEventGoContent reads internal/core/event.go and returns the
// godoc block for the TimestampMonoNsec field.  It fails the test if the file
// is unreadable or the field declaration is absent.
//
// The search locates the field declaration line (the line where
// "TimestampMonoNsec" appears followed by its type, preceded by whitespace),
// then walks the preceding lines to collect the associated comment block.
func monoNsecSensorEventGoContent(t *testing.T) string {
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

// TestMonoTsMono_EV003_SpecContainsProcessScopeInvariant verifies that the
// EV-003 section of specs/event-model.md encodes the process-scope invariant
// with the required canonical phrases.
//
// Phrases required by the invariant (EV-003, hk-hqwn.6):
//
//   - "process-scoped"        — the primary classification; MUST NOT become "host-scoped" etc.
//   - "MUST NOT be compared"  — the prohibition; must be explicit, not advisory
//   - "intra-process ordering"— the permitted use; scope is within a single process lifetime
//   - "emitter's lifetime"    — the temporal bound; clarifies what "intra-process" means
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestMonoTsMono_EV003_SpecContainsProcessScopeInvariant(t *testing.T) {
	t.Parallel()

	para := monoNsecSensorSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "process-scoped",
			hint:   "EV-003 must declare timestamp_mono_nsec as process-scoped; renaming this breaks the hk-hqwn.6 invariant",
		},
		{
			phrase: "MUST NOT be compared",
			hint:   "EV-003 must explicitly prohibit cross-process comparison; the prohibition must be normative (MUST NOT)",
		},
		{
			phrase: "intra-process ordering",
			hint:   "EV-003 must restrict the permitted use to intra-process ordering; this is the only valid scope",
		},
		{
			phrase: "emitter's lifetime",
			hint:   "EV-003 must bound the valid scope to the emitter's lifetime; 'emitter's lifetime' anchors the temporal constraint",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-003 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestMonoTsMono_EV003_SpecExplicitCrossProcessDaemonRestartProhibition verifies
// that the EV-003 paragraph combines the cross-process prohibition with the
// daemon-restart prohibition in the same sentence.  This double-proximity check
// guards against a spec edit that mentions the terms but in unrelated sentences.
func TestMonoTsMono_EV003_SpecExplicitCrossProcessDaemonRestartProhibition(t *testing.T) {
	t.Parallel()

	para := monoNsecSensorSpecContent(t)

	// "across daemon restarts or across processes" is the canonical phrasing in EV-003.
	const canonicalPhrase = "across daemon restarts or across processes"
	if !strings.Contains(para, canonicalPhrase) {
		t.Errorf(
			"EV-003 spec paragraph does not contain %q; the cross-process prohibition "+
				"(hk-hqwn.6) requires this exact phrase to lock in both daemon-restart "+
				"and cross-process boundaries\nParagraph:\n%s",
			canonicalPhrase, para,
		)
	}
}

// TestMonoTsMono_EV003_CodeGodocCarriesProcessScopeProhibition verifies that the
// TimestampMonoNsec field godoc in internal/core/event.go carries the EV-003
// cross-process-comparison prohibition so that the constraint is visible to code
// reviewers without leaving the Go type declaration.
//
// The godoc must mention:
//   - "EV-003" — spec traceability anchor
//   - "process" (in any form) — the scope constraint
func TestMonoTsMono_EV003_CodeGodocCarriesProcessScopeProhibition(t *testing.T) {
	t.Parallel()

	// Use the raw-text walk rather than the AST approach here; the AST
	// parser's field.Doc vs field.Comment distinction depends on comment
	// placement within multi-field structs and is brittle across go/ast
	// versions.  The raw walk is authoritative for human-visible godoc.
	godoc := monoNsecSensorEventGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "EV-003",
			hint:   "TimestampMonoNsec godoc must cite EV-003 so spec traceability is visible at the field declaration",
		},
		{
			phrase: "process",
			hint:   "TimestampMonoNsec godoc must contain 'process' to document the process-scope constraint",
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

// TestMonoTsMono_EV003_ForwardDocSensor is a documentation-marker test for
// event-model.md §4.1 EV-003 (hk-hqwn.6).
//
// EV-003 requires that timestamp_mono_nsec MUST NOT be compared across daemon
// restarts or across processes, and is meaningful ONLY for intra-process ordering
// within the emitter's lifetime.
//
// This test skips unconditionally because the consumer-side enforcement layer
// (a runtime guard that rejects cross-process comparisons) is not yet implemented.
// It exists as a discoverable anchor in the test suite.  When the enforcement
// layer lands, the implementer SHOULD either:
//
//  1. Replace this marker with concrete assertions against the enforcement layer, OR
//  2. Extend it with those assertions, retaining the EV-003 citation and hk-hqwn.6
//     traceability.
//
// Requirement-traceable bead: hk-hqwn.6.
func TestMonoTsMono_EV003_ForwardDocSensor(t *testing.T) {
	t.Log("EV-003 (hk-hqwn.6): timestamp_mono_nsec is process-scoped and NOT cross-process-comparable.")
	t.Log("MUST NOT be compared across daemon restarts or across processes.")
	t.Log("Meaningful ONLY for intra-process ordering within the emitter's lifetime.")
	t.Log("Spec reference: event-model.md §4.1 EV-003.")
	t.Log("")
	t.Log("Consumer-side enforcement layer not yet implemented.")
	t.Log("When that layer lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the enforcement layer.")
	t.SkipNow()
}
