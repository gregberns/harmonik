package agentmanifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootSkillInBriefOutput(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// captain is the parent of crew
	makeTypeFolder(t, agentsDir, "captain", "operator", "I am captain — I run the fleet.\n", "")

	// boot skill in _skills/
	bootDir := filepath.Join(agentsDir, "_skills", "boot")
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bootContent := "Run `harmonik agent brief` — that IS your complete boot context; no other skill needed to orient.\n"
	if err := os.WriteFile(filepath.Join(bootDir, "SKILL.md"), []byte(bootContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// crew type with boot as first context skill
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
  - { ref: boot,         as: skill,       presence: injected }
  - { ref: operating.md, as: instruction, presence: injected }
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

	// Verify boot skill is in the skills list
	if len(doc.Skills) == 0 {
		t.Fatal("expected at least one skill entry, got none")
	}
	if doc.Skills[0].Name != "boot" {
		t.Errorf("first skill is %q, want %q", doc.Skills[0].Name, "boot")
	}
	if !strings.Contains(doc.Skills[0].ShortDesc, "harmonik agent brief") {
		t.Errorf("boot skill ShortDesc %q does not contain 'harmonik agent brief'", doc.Skills[0].ShortDesc)
	}

	// Verify the rendered markdown mentions harmonik agent brief
	var buf strings.Builder
	RenderMarkdown(doc, &buf)
	out := buf.String()
	if !strings.Contains(out, "harmonik agent brief") {
		t.Errorf("brief output does not mention 'harmonik agent brief':\n%s", out)
	}
}
