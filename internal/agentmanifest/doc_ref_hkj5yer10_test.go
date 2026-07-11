package agentmanifest

// doc_ref_hkj5yer10_test.go — covers as:doc/retrieved refs rendering with explicit
// paths + frontmatter description parsing.
// Bead: hk-j5yer.10.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBootDoc_DocRefWithFrontmatterDescription(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	makeTypeFolder(t, agentsDir, "captain", "operator", "I am captain — I run the fleet.\n", "")

	// A path-bearing doc ref with YAML frontmatter carrying a description.
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docContent := "---\n" +
		"name: some-rules\n" +
		"description: The canonical rules doc.\n" +
		"---\n" +
		"# Some Rules\n"
	if err := os.WriteFile(filepath.Join(docsDir, "rules.md"), []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// A path-bearing doc ref with NO frontmatter.
	if err := os.WriteFile(filepath.Join(docsDir, "plain.md"), []byte("# Plain\nNo frontmatter here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	crewDir := filepath.Join(agentsDir, "crew")
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `type: crew
cardinality: { min: 0, max: n }
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: docs/rules.md, as: doc, presence: retrieved }
  - { ref: docs/plain.md, as: doc, presence: retrieved }
triggers:
  - { id: queue, source: queue, enabled: true }
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
markers:
  never_emits: []
`
	if err := os.WriteFile(filepath.Join(crewDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "soul.md"), []byte("I am crew — I work beads.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "operating.md"), []byte("## Loop\n1. Claim bead.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := BuildBootDoc(agentsDir, tmpDir, "crew", "crew", "fresh")
	if err != nil {
		t.Fatalf("BuildBootDoc: %v", err)
	}

	if len(doc.Docs) != 2 {
		t.Fatalf("Docs count = %d, want 2", len(doc.Docs))
	}

	wantPath := filepath.Join(tmpDir, "docs", "rules.md")
	if doc.Docs[0].Pointer != wantPath {
		t.Errorf("Docs[0].Pointer = %q, want %q", doc.Docs[0].Pointer, wantPath)
	}
	if doc.Docs[0].ShortDesc != "The canonical rules doc." {
		t.Errorf("Docs[0].ShortDesc = %q, want %q", doc.Docs[0].ShortDesc, "The canonical rules doc.")
	}

	if doc.Docs[1].ShortDesc != "" {
		t.Errorf("Docs[1].ShortDesc = %q, want empty (no frontmatter)", doc.Docs[1].ShortDesc)
	}
	if doc.Docs[1].Pointer == "" {
		t.Error("Docs[1].Pointer is empty, want explicit path")
	}

	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()
	if !strings.Contains(out, "### Docs") {
		t.Errorf("rendered markdown missing Docs section:\n%s", out)
	}
	if !strings.Contains(out, wantPath) {
		t.Errorf("rendered markdown missing explicit doc path %q:\n%s", wantPath, out)
	}
	if !strings.Contains(out, "The canonical rules doc.") {
		t.Errorf("rendered markdown missing frontmatter description:\n%s", out)
	}
}
