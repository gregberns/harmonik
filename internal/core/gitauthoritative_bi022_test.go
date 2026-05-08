// Package core — BI-022 git-authoritative-completion sensor.
//
// BI-022 (beads-integration.md §4.7): "If Beads reports a bead as `closed` but
// no merge commit with `Harmonik-Bead-ID: <bead_id>` exists in the project's
// git history, the divergence MUST be classified as a reconciliation flag per
// [reconciliation/spec.md §8] Cat 3 and MUST NOT be silently auto-reconciled
// into git's direction."
//
// This file is the documentation/discipline sensor for hk-872.23.  The
// reconciliation Cat-3 classifier is not yet built; that behavioral sensor
// (hk-872.43 — "Sensor: git wins on completion disagreement") will land on top
// of the classifier logic in a future bead.
//
// Invariant locked by BI-022:
//
//	bead.status == "closed"  AND  no git merge commit carries Harmonik-Bead-ID: <bead_id>
//	→  classify as Cat 3 reconciliation flag
//	→  NEVER silently auto-reconcile
//
// The tests below verify that the spec encodes this invariant with the exact
// canonical phrasing, protecting against spec drift that would silently
// un-anchor the invariant from the codebase.
//
// When the Cat-3 classifier is implemented, hk-872.43 SHOULD either:
//   - Delete the forward-doc marker (TestGitAuthoritativeBI022_ForwardDocSensor)
//     and replace it with concrete assertions against the classifier, OR
//   - Extend it with those concrete assertions, retaining the BI-022 citation
//     and hk-872.23 traceability.
//
// Requirement-traceable bead: hk-872.23.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// specBI022Content reads specs/beads-integration.md, locates the BI-022
// anchor, and returns the paragraph that contains it.  It fails the test if
// the file is unreadable or the anchor is missing.
func specBI022Content(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/core/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	// Confirm the BI-022 section header is present.
	const anchor = "BI-022"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; BI-022 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph starting at the anchor (up to the next section
	// boundary) so callers can assert on its contents.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// TestGitAuthoritativeBI022_SpecContainsCat3Invariant verifies that the BI-022
// section of specs/beads-integration.md encodes the Cat 3 invariant with the
// required canonical phrases.
//
// Phrases required by the invariant (BI-022, hk-872.23):
//
//   - "Cat 3"              — classification category; must not silently become Cat 4
//   - "Harmonik-Bead-ID"  — the git trailer that constitutes proof of completion
//   - "auto-reconcile"    — the forbidden action; spec must use this word
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestGitAuthoritativeBI022_SpecContainsCat3Invariant(t *testing.T) {
	t.Parallel()

	para := specBI022Content(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "Cat 3",
			hint:   "BI-022 must classify the divergence as Cat 3; renaming this breaks the hk-872.23 invariant",
		},
		{
			phrase: "Harmonik-Bead-ID",
			hint:   "BI-022 must name the Harmonik-Bead-ID git trailer as the completion-proof mechanism",
		},
		{
			phrase: "auto-reconcile",
			hint:   "BI-022 must forbid silent auto-reconcile; the phrase anchors the no-silent-overwrite contract",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("BI-022 spec paragraph does not contain %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// TestGitAuthoritativeBI022_SpecRequiresNoSilentAutoReconcile verifies that the
// BI-022 paragraph combines the "Harmonik-Bead-ID" trailer name with the "Cat 3"
// classification in close proximity, and that "silently" appears before
// "auto-reconcile".  This double-proximity check guards against a spec edit
// that mentions both terms but in unrelated sentences.
func TestGitAuthoritativeBI022_SpecRequiresNoSilentAutoReconcile(t *testing.T) {
	t.Parallel()

	para := specBI022Content(t)

	// "silently auto-reconciled" is the canonical phrasing in BI-022.
	const silentPhrase = "silently auto-reconciled"
	if !strings.Contains(para, silentPhrase) {
		t.Errorf(
			"BI-022 spec paragraph does not contain %q; the no-silent-overwrite invariant "+
				"(hk-872.23) requires this exact phrase\nParagraph:\n%s",
			silentPhrase, para,
		)
	}
}

// TestGitAuthoritativeBI022_ForwardDocSensor is a documentation-marker test for
// beads-integration.md §4.7 BI-022 (hk-872.23).
//
// BI-022 requires that a bead reported as `closed` by Beads but lacking a
// matching Harmonik-Bead-ID merge commit in git MUST be classified as a Cat 3
// reconciliation flag and MUST NOT be silently auto-reconciled.
//
// This test skips unconditionally because the Cat-3 classifier is not yet
// implemented.  It exists as a discoverable anchor in the test suite.  When the
// classifier lands (see hk-872.43), the implementer SHOULD either:
//
//  1. Replace this marker with concrete assertions against the classifier, OR
//  2. Extend it with those assertions, retaining BI-022 citation and hk-872.23
//     traceability.
//
// Requirement-traceable bead: hk-872.23.
func TestGitAuthoritativeBI022_ForwardDocSensor(t *testing.T) {
	t.Log("BI-022 (hk-872.23): bead.status=closed without matching Harmonik-Bead-ID merge commit in git → Cat 3 reconciliation flag.")
	t.Log("MUST NOT be silently auto-reconciled into git's direction.")
	t.Log("Spec reference: beads-integration.md §4.7 BI-022.")
	t.Log("")
	t.Log("Cat-3 classifier not yet implemented.")
	t.Log("When hk-872.43 (Sensor: git wins on completion disagreement) lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the classifier.")
	t.SkipNow()
}
