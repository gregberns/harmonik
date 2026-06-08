package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

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
