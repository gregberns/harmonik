package agentmanifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/agentmanifest"
)

// makeSkillDir creates a skill directory (bare name) under agentsDir/_skills/.
func makeSkillDir(t *testing.T, agentsDir, skillName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(agentsDir, "_skills", skillName), 0o755); err != nil {
		t.Fatalf("mkdir skill %q: %v", skillName, err)
	}
}

// makeParentTypeFolder creates a minimal parent type folder with just a soul.md.
func makeParentTypeFolder(t *testing.T, agentsDir, typeName string) {
	t.Helper()
	dir := filepath.Join(agentsDir, typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir parent type %q: %v", typeName, err)
	}
	writeFile(t, filepath.Join(dir, "soul.md"), "**I am** "+typeName+".\n")
}

// makePathBearingRef creates a file at repoRoot/<path>.
func makePathBearingRef(t *testing.T, repoRoot, ref string) {
	t.Helper()
	full := filepath.Join(repoRoot, ref)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for ref %q: %v", ref, err)
	}
	writeFile(t, full, "content\n")
}

// --- TestCheck_WellFormed ---

func TestCheck_WellFormed_NoContext(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// Parent type "captain" must have a soul.md.
	makeParentTypeFolder(t, agentsDir, "captain")

	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
markers:
  never_emits: []
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) != 0 {
		t.Errorf("expected no defects, got %d: %v", len(defects), defects)
	}
}

func TestCheck_WellFormed_BareSkillRef(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	makeParentTypeFolder(t, agentsDir, "captain")
	makeSkillDir(t, agentsDir, "crew-launch")

	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: crew-launch, as: skill, presence: injected }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) != 0 {
		t.Errorf("expected no defects, got %d: %v", len(defects), defects)
	}
}

func TestCheck_WellFormed_PathBearingRef(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	makeParentTypeFolder(t, agentsDir, "captain")
	makePathBearingRef(t, repoRoot, "docs/orchestration-protocol-v2.md")

	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: docs/orchestration-protocol-v2.md, as: doc, presence: retrieved }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) != 0 {
		t.Errorf("expected no defects, got %d: %v", len(defects), defects)
	}
}

func TestCheck_WellFormed_OperatorTerminal(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// "operator" is the reserved terminal — no folder needed.
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) != 0 {
		t.Errorf("expected no defects for operator terminal, got %d: %v", len(defects), defects)
	}
}

// --- TestCheck_LoadFailures ---

func TestCheck_LoadError_MissingManifest(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	defects := agentmanifest.Check(agentsDir, "ghost", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects for missing type folder, got none")
	}
	if !strings.Contains(defects[0].Message, "ghost") {
		t.Errorf("defect message should name the type, got: %q", defects[0].Message)
	}
}

func TestCheck_LoadError_MissingSoulFile(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// Write manifest + operating.md, omit soul.md.
	dir := filepath.Join(agentsDir, "mytype")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
`
	writeFile(t, filepath.Join(dir, "manifest.yaml"), m)
	writeFile(t, filepath.Join(dir, "operating.md"), validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects for missing soul.md, got none")
	}
}

// --- TestCheck_ParentIntent ---

func TestCheck_ParentIntent_DanglingType(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// "captain" type folder does NOT exist.
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects for dangling parent_intent, got none")
	}
	found := false
	for _, d := range defects {
		if strings.Contains(d.Field, "parent_intent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a defect with field containing 'parent_intent', got: %v", defects)
	}
}

func TestCheck_ParentIntent_ParentFolderExistsButNoSoul(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// Create parent type folder WITHOUT soul.md.
	if err := os.MkdirAll(filepath.Join(agentsDir, "captain"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects when parent soul.md missing, got none")
	}
	found := false
	for _, d := range defects {
		if strings.Contains(d.Field, "parent_intent") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected defect field 'parent_intent', got: %v", defects)
	}
}

// --- TestCheck_ContextRef ---

func TestCheck_ContextRef_UnknownBareRef(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	makeParentTypeFolder(t, agentsDir, "captain")

	// context ref "nonexistent-skill" is not in _skills/ or type folder.
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: nonexistent-skill, as: skill, presence: retrieved }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects for unknown bare ref, got none")
	}
	found := false
	for _, d := range defects {
		if strings.Contains(d.Field, "context") && strings.Contains(d.Field, "ref") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected defect for context ref, got: %v", defects)
	}
}

func TestCheck_ContextRef_PathBearingMissing(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	makeParentTypeFolder(t, agentsDir, "captain")

	// path-bearing ref that doesn't exist under repoRoot.
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: docs/missing-doc.md, as: doc, presence: retrieved }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) == 0 {
		t.Fatal("expected defects for missing path-bearing ref, got none")
	}
	found := false
	for _, d := range defects {
		if strings.Contains(d.Field, "context") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected defect for context ref, got: %v", defects)
	}
}

func TestCheck_ContextRef_MultipleDefectsCollected(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	// Both a bad ref and a dangling parent — all defects collected.
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: missing-skill-a, as: skill, presence: retrieved }
  - { ref: missing-skill-b, as: skill, presence: retrieved }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	// At minimum: 1 parent_intent + 2 context refs = 3 defects.
	if len(defects) < 3 {
		t.Errorf("expected >= 3 defects, got %d: %v", len(defects), defects)
	}
}

func TestCheck_ContextRef_BareRefInTypeFolder(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	repoRoot := t.TempDir()

	makeParentTypeFolder(t, agentsDir, "captain")

	// skill lives in the type's OWN folder (not _skills/).
	typeDir := filepath.Join(agentsDir, "mytype")
	if err := os.MkdirAll(filepath.Join(typeDir, "my-private-skill"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: my-private-skill, as: skill, presence: injected }
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)

	defects := agentmanifest.Check(agentsDir, "mytype", repoRoot)
	if len(defects) != 0 {
		t.Errorf("expected no defects for per-type skill ref, got %d: %v", len(defects), defects)
	}
}
