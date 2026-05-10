// Package lifecycle — EV-024 replay-cannot-re-establish-agent-state sensor.
//
// EV-024 (event-model.md §4.5 EV-024): "Neither observational replay nor
// state reconstruction MAY re-establish live agent-process state or re-invoke
// LLMs. Tools that appear to do so are debugging aids; their output is
// non-authoritative."
//
// Replay — whether observational (walking JSONL) or state reconstruction (git
// + Beads walk on startup) — MUST NOT attempt to bring a live agent process
// back to its prior in-memory state or call an LLM on behalf of any replay
// consumer. Tools that surface JSONL content or checkpoint records are
// debugging aids; their output is advisory.
//
// This file is the documentation/discipline sensor for hk-hqwn.33. The
// runtime enforcement layer (a guard that prevents replay paths from
// triggering LLM calls or agent-process revival) is not yet built; that
// behavioral sensor will land in a later bead once the agent-runner layer
// exists.
//
// Invariant locked by EV-024:
//
//	replay output is non-authoritative
//	→  observational replay MUST NOT re-establish live agent-process state
//	→  state reconstruction MUST NOT re-invoke LLMs
//	→  any tool appearing to do so is a debugging aid; output is advisory only
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-024 with the required canonical phrases.
//  2. The ReadJSONLForDivergenceEvidence godoc in jsonldivergence_em031.go
//     carries the EV-024 non-authoritative constraint so it is visible at the
//     call site.
//
// When an agent-runner enforcement layer is implemented, that bead SHOULD
// either:
//   - Delete the forward-doc marker (TestReplayNoAgentState_EV024_ForwardDocSensor)
//     and replace it with concrete assertions against the enforcement layer, OR
//   - Extend it with those assertions, retaining the EV-024 citation and
//     hk-hqwn.33 traceability.
//
// Requirement-traceable bead: hk-hqwn.33.
package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// replayNoAgentStateFixtureSpecContent reads specs/event-model.md, locates
// the EV-024 anchor, and returns the paragraph that contains it. It fails the
// test if the file is unreadable or the anchor is missing.
func replayNoAgentStateFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/lifecycle/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "EV-024 — Replay cannot re-establish agent state or re-invoke LLMs"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-024 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph from the anchor up to the next section boundary.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// replayNoAgentStateFixtureDivergenceGoContent reads
// internal/lifecycle/jsonldivergence_em031.go and returns the godoc block for
// ReadJSONLForDivergenceEvidence. It fails the test if the file is unreadable
// or the function declaration is absent.
func replayNoAgentStateFixtureDivergenceGoContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate jsonldivergence_em031.go")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "jsonldivergence_em031.go")

	raw, err := os.ReadFile(srcPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", srcPath, err)
	}

	// Find the function declaration line.
	lines := strings.Split(string(raw), "\n")
	funcLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "func ReadJSONLForDivergenceEvidence(") {
			funcLineIdx = i
			break
		}
	}
	if funcLineIdx < 0 {
		t.Fatal("jsonldivergence_em031.go does not contain ReadJSONLForDivergenceEvidence declaration")
	}

	// Walk backwards from the function declaration to collect the comment block.
	var commentLines []string
	for i := funcLineIdx - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "//") {
			commentLines = append([]string{line}, commentLines...)
		} else {
			break
		}
	}
	return strings.Join(commentLines, "\n")
}

// TestReplayNoAgentState_EV024_SpecContainsProhibitionInvariant verifies that
// the EV-024 section of specs/event-model.md encodes the re-establishment
// prohibition with the required canonical phrases.
//
// Phrases required by the invariant (EV-024, hk-hqwn.33):
//
//   - "non-authoritative"           — the output classification; must be explicit
//   - "re-establish"                — the prohibited action for observational replay
//   - "re-invoke"                   — the prohibited action for state reconstruction
//   - "debugging aids"              — the correct classification of tools that appear to do so
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestReplayNoAgentState_EV024_SpecContainsProhibitionInvariant(t *testing.T) {
	t.Parallel()

	para := replayNoAgentStateFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "non-authoritative",
			hint:   "EV-024 must explicitly classify replay output as non-authoritative; removing this phrase weakens the EV-024 contract",
		},
		{
			phrase: "re-establish",
			hint:   "EV-024 must name 're-establish' as the prohibited action for observational replay; this is the hk-hqwn.33 invariant boundary for the agent-state side",
		},
		{
			phrase: "re-invoke",
			hint:   "EV-024 must name 're-invoke' as the prohibited action for LLM calls; the prohibition covers both agent-state re-establishment and LLM re-invocation",
		},
		{
			phrase: "debugging aids",
			hint:   "EV-024 must classify tools that appear to re-establish state as 'debugging aids'; this is the correct framing per hk-hqwn.33",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-024 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestReplayNoAgentState_EV024_CodeGodocCarriesNonAuthoritativeConstraint
// verifies that the ReadJSONLForDivergenceEvidence godoc in
// jsonldivergence_em031.go carries the EV-024 non-authoritative constraint so
// the prohibition is visible at the call site without requiring a spec lookup.
//
// The godoc must mention:
//   - "non-authoritative"           — the output classification per EV-024
//   - "re-establish"                — the forbidden action (agent-state revival)
//   - "re-invoke"                   — the forbidden action (LLM calls)
//   - "debugging aid"               — the correct framing for any tool that appears to do so
func TestReplayNoAgentState_EV024_CodeGodocCarriesNonAuthoritativeConstraint(t *testing.T) {
	t.Parallel()

	godoc := replayNoAgentStateFixtureDivergenceGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "non-authoritative",
			hint:   "ReadJSONLForDivergenceEvidence godoc must carry the EV-024 non-authoritative classification; this anchors the constraint at the call site",
		},
		{
			phrase: "re-establish",
			hint:   "ReadJSONLForDivergenceEvidence godoc must name 're-establish' as the forbidden agent-state action per EV-024; the prohibition must be explicit",
		},
		{
			phrase: "re-invoke",
			hint:   "ReadJSONLForDivergenceEvidence godoc must name 're-invoke' as the forbidden LLM-call action per EV-024; both prohibited actions must appear",
		},
		{
			phrase: "debugging aid",
			hint:   "ReadJSONLForDivergenceEvidence godoc must classify itself as a 'debugging aid' per EV-024; this framing prevents callers from treating output as authoritative",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(godoc, tc.phrase) {
			t.Errorf(
				"ReadJSONLForDivergenceEvidence godoc does not contain %q — %s\nGodoc:\n%s",
				tc.phrase, tc.hint, godoc,
			)
		}
	}
}

// TestReplayNoAgentState_EV024_ForwardDocSensor is a documentation-marker
// test for event-model.md §4.5 EV-024 (hk-hqwn.33).
//
// EV-024 requires that neither observational replay nor state reconstruction
// may re-establish live agent-process state or re-invoke LLMs. Tools that
// appear to do so are debugging aids; their output is non-authoritative.
//
// This test skips unconditionally because the agent-runner enforcement layer
// (a runtime guard that prevents replay paths from triggering LLM calls or
// agent-process revival) is not yet implemented. It exists as a discoverable
// anchor in the test suite. When the enforcement layer lands, the implementer
// SHOULD either:
//
//  1. Replace this marker with concrete assertions against the enforcement
//     layer, OR
//  2. Extend it with those assertions, retaining the EV-024 citation and
//     hk-hqwn.33 traceability.
//
// Requirement-traceable bead: hk-hqwn.33.
func TestReplayNoAgentState_EV024_ForwardDocSensor(t *testing.T) {
	t.Log("EV-024 (hk-hqwn.33): replay cannot re-establish agent state or re-invoke LLMs.")
	t.Log("Neither observational replay nor state reconstruction MAY re-establish live agent-process state.")
	t.Log("Neither observational replay nor state reconstruction MAY re-invoke LLMs.")
	t.Log("Tools that appear to do so are debugging aids; their output is non-authoritative.")
	t.Log("Spec reference: event-model.md §4.5 EV-024.")
	t.Log("")
	t.Log("Agent-runner enforcement layer not yet implemented.")
	t.Log("When that layer lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the enforcement layer.")
	t.SkipNow()
}
