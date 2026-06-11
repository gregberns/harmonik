package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStandardBeadDotLoadFailureReturnsError pins the EM-012a-FLOOR review-floor
// guarantee: loadStandardGraph MUST return a non-nil error when the embedded
// bytes are invalid so that the pre-switch block in workloop.go (lines ~1985-1992)
// can demote workflowMode from dot to review-loop, NEVER to single.
//
// This test verifies the failure half of the safety contract: when
// standardBeadDotSrc is corrupt, loadStandardGraph fails.  The positive half
// (the embedded graph is always valid in production) is covered by
// TestStandardBeadDotEmbedValidAndInSync above.
//
// The workloop pre-switch block that catches this error and sets
//
//	workflowMode = core.WorkflowModeReviewLoop
//
// is at internal/daemon/workloop.go (look for "Safety floor (hk-30vlb §REVIEW
// FLOOR item b)").  The two tests together pin the full chain:
//
//	valid embedded bytes   → loadStandardGraph succeeds → dot dispatch
//	invalid embedded bytes → loadStandardGraph fails   → workloop → review-loop (NEVER single)
func TestStandardBeadDotLoadFailureReturnsError(t *testing.T) {
	// Save the real embedded bytes so we can restore them after the test.
	orig := standardBeadDotSrc
	t.Cleanup(func() { standardBeadDotSrc = orig })

	// Corrupt the embedded bytes — any invalid DOT content is sufficient.
	standardBeadDotSrc = []byte("this is not valid DOT graph syntax #@!")

	_, err := loadStandardGraph(nil)
	if err == nil {
		t.Fatal("loadStandardGraph: expected an error for invalid DOT bytes; got nil — " +
			"the EM-012a-FLOOR review-floor in workloop.go relies on this error to " +
			"demote workflowMode from dot to review-loop")
	}
}

// TestStandardBeadDotEmbedValidAndInSync guards the invariant documented in
// standardgraph.go: the embedded internal/daemon/standard-bead.dot — the bytes the
// daemon actually //go:embed's and runs in DOT mode — must (1) parse+validate and
// (2) be byte-identical to the canonical specs/examples/standard-bead.dot.
//
// Without (2) a node edit (e.g. the commit_gate rewire in hk-u830m) applied to only
// one copy silently no-ops at runtime: the golden test loads the specs/examples copy
// and stays green while the daemon keeps running the stale embedded copy. That exact
// drift was caught in hk-u830m review; this test makes it impossible to reintroduce.
func TestStandardBeadDotEmbedValidAndInSync(t *testing.T) {
	// (1) The embedded graph must parse and validate — it is what DOT-mode dispatch runs.
	if _, err := loadStandardGraph(nil); err != nil {
		t.Fatalf("embedded standard-bead.dot failed to parse/validate: %v", err)
	}

	// (2) Embedded copy must match the canonical spec byte-for-byte.
	specPath := filepath.Join("..", "..", "specs", "examples", "standard-bead.dot")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read canonical spec %s: %v", specPath, err)
	}
	if string(standardBeadDotSrc) != string(specBytes) {
		t.Fatalf("embedded internal/daemon/standard-bead.dot is OUT OF SYNC with %s.\n"+
			"The daemon embeds the internal/daemon copy; an edit to only one copy silently "+
			"no-ops at runtime. Re-sync with: cp %s internal/daemon/standard-bead.dot",
			specPath, specPath)
	}
}
