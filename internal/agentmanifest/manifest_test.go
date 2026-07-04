package agentmanifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/agentmanifest"
)

// --- test fixtures ---

const validManifest = `
type: mytype
cardinality: { min: 0, max: n }
harness: claude

identity:
  soul: soul.md
  parent_intent: captain

context:
  - { ref: operating.md,  as: instruction, presence: injected }
  - { ref: crew-launch,   as: skill,       presence: injected }
  - { ref: beads-cli,     as: skill,       presence: retrieved }

triggers:
  - { id: queue, source: queue, enabled: true }

handoff:
  channel: private

keeper:
  thresholds: default

lifecycle:
  self_restart: true

tools_dir: null

markers:
  never_emits: []
`

const validSoul = `**I am** mytype — a test agent type.

**I do**
- Do something.

**I do NOT**
- Do something else.

**I escalate to** captain.
`

const validOperating = `## On wake
1. Read handoff.

## Loop
1. Do work.

## Skills I use
- skill-a — when needed.

## Bounds
- Do not overstep.
`

// makeTypeFolder creates a minimal valid type folder under agentsDir/typeName.
func makeTypeFolder(t *testing.T, agentsDir, typeName, manifestYAML, soul, operating string) {
	t.Helper()
	dir := filepath.Join(agentsDir, typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	writeFile(t, filepath.Join(dir, "manifest.yaml"), manifestYAML)
	writeFile(t, filepath.Join(dir, "soul.md"), soul)
	writeFile(t, filepath.Join(dir, "operating.md"), operating)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// --- Load tests ---

func TestLoad_ValidType(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	makeTypeFolder(t, agentsDir, "mytype", validManifest, validSoul, validOperating)

	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if tf.Name != "mytype" {
		t.Errorf("Name = %q, want %q", tf.Name, "mytype")
	}
	if tf.Manifest.Type != "mytype" {
		t.Errorf("Manifest.Type = %q, want %q", tf.Manifest.Type, "mytype")
	}
	if tf.Manifest.Harness != "claude" {
		t.Errorf("Manifest.Harness = %q, want %q", tf.Manifest.Harness, "claude")
	}
	if tf.Manifest.Cardinality.Max != agentmanifest.MaxUnlimited {
		t.Errorf("Cardinality.Max = %d, want MaxUnlimited (%d)", tf.Manifest.Cardinality.Max, agentmanifest.MaxUnlimited)
	}
	if tf.Manifest.Cardinality.Min != 0 {
		t.Errorf("Cardinality.Min = %d, want 0", tf.Manifest.Cardinality.Min)
	}
	if tf.Manifest.Identity.Soul != "soul.md" {
		t.Errorf("Identity.Soul = %q, want %q", tf.Manifest.Identity.Soul, "soul.md")
	}
	if tf.Manifest.Identity.ParentIntent != "captain" {
		t.Errorf("Identity.ParentIntent = %q, want %q", tf.Manifest.Identity.ParentIntent, "captain")
	}
	if len(tf.Manifest.Context) != 3 {
		t.Errorf("len(Context) = %d, want 3", len(tf.Manifest.Context))
	}
	if len(tf.Manifest.Triggers) != 1 {
		t.Errorf("len(Triggers) = %d, want 1", len(tf.Manifest.Triggers))
	}
	if tf.Manifest.Triggers[0].Source != "queue" {
		t.Errorf("Triggers[0].Source = %q, want %q", tf.Manifest.Triggers[0].Source, "queue")
	}
	if !tf.Manifest.Triggers[0].Enabled {
		t.Error("Triggers[0].Enabled = false, want true")
	}
	if tf.Manifest.Handoff.Channel != "private" {
		t.Errorf("Handoff.Channel = %q, want %q", tf.Manifest.Handoff.Channel, "private")
	}
	if tf.Manifest.ToolsDir != nil {
		t.Errorf("ToolsDir = %v, want nil", tf.Manifest.ToolsDir)
	}
	if tf.SoulContent != validSoul {
		t.Errorf("SoulContent mismatch: got %q, want %q", tf.SoulContent, validSoul)
	}
	if tf.OperatingContent != validOperating {
		t.Errorf("OperatingContent mismatch: got %q, want %q", tf.OperatingContent, validOperating)
	}
}

func TestLoad_ReservedName(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()

	_, err := agentmanifest.Load(agentsDir, "_skills")
	if !errors.Is(err, agentmanifest.ErrReservedName) {
		t.Errorf("Load(_skills) error = %v, want ErrReservedName", err)
	}
}

func TestLoad_ReservedNameOtherPrefix(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()

	_, err := agentmanifest.Load(agentsDir, "_anything")
	if !errors.Is(err, agentmanifest.ErrReservedName) {
		t.Errorf("Load(_anything) error = %v, want ErrReservedName", err)
	}
}

func TestLoad_NotFound(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()

	_, err := agentmanifest.Load(agentsDir, "doesnotexist")
	if !errors.Is(err, agentmanifest.ErrNotFound) {
		t.Errorf("Load(doesnotexist) error = %v, want ErrNotFound", err)
	}
}

func TestLoad_MissingManifest(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	dir := filepath.Join(agentsDir, "orphan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No manifest.yaml written.

	_, err := agentmanifest.Load(agentsDir, "orphan")
	if !errors.Is(err, agentmanifest.ErrNotFound) {
		t.Errorf("Load(orphan) error = %v, want ErrNotFound", err)
	}
}

func TestLoad_MissingSoul(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	dir := filepath.Join(agentsDir, "mytype")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "manifest.yaml"), validManifest)
	writeFile(t, filepath.Join(dir, "operating.md"), validOperating)
	// soul.md intentionally absent

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load with missing soul.md error = %v, want ErrInvalid", err)
	}
}

func TestLoad_MissingOperating(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	dir := filepath.Join(agentsDir, "mytype")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "manifest.yaml"), validManifest)
	writeFile(t, filepath.Join(dir, "soul.md"), validSoul)
	// operating.md intentionally absent

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load with missing operating.md error = %v, want ErrInvalid", err)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	makeTypeFolder(t, agentsDir, "mytype", ":::not valid yaml:::", validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(malformed yaml) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_TypeMismatch(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	mismatch := `
type: wrongname
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", mismatch, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(type mismatch) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_MissingTypeField(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	noType := `
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", noType, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(missing type) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_MissingHarness(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	noHarness := `
type: mytype
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", noHarness, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(missing harness) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_InvalidContextAs(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	badAs := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: crew-launch, as: INVALID, presence: injected }
`
	makeTypeFolder(t, agentsDir, "mytype", badAs, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(invalid context.as) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_InvalidContextPresence(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	badPresence := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: crew-launch, as: skill, presence: INVALID }
`
	makeTypeFolder(t, agentsDir, "mytype", badPresence, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(invalid context.presence) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_InvalidTriggerSource(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	badSource := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
triggers:
  - { id: mytrigger, source: INVALID, enabled: true }
`
	makeTypeFolder(t, agentsDir, "mytype", badSource, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(invalid trigger.source) error = %v, want ErrInvalid", err)
	}
}

func TestLoad_MissingTriggerID(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	noID := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
triggers:
  - { source: queue, enabled: true }
`
	makeTypeFolder(t, agentsDir, "mytype", noID, validSoul, validOperating)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("Load(missing trigger.id) error = %v, want ErrInvalid", err)
	}
}

// --- MaxCardinality unmarshaling ---

func TestLoad_MaxCardinality_Unlimited(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	m := `
type: mytype
cardinality: { min: 0, max: n }
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)
	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tf.Manifest.Cardinality.Max != agentmanifest.MaxUnlimited {
		t.Errorf("Cardinality.Max = %d, want MaxUnlimited (%d)", tf.Manifest.Cardinality.Max, agentmanifest.MaxUnlimited)
	}
}

func TestLoad_MaxCardinality_Singleton(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	m := `
type: mytype
cardinality: { min: 0, max: 1 }
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)
	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tf.Manifest.Cardinality.Max != 1 {
		t.Errorf("Cardinality.Max = %d, want 1", tf.Manifest.Cardinality.Max)
	}
}

func TestLoad_AllTriggerSources(t *testing.T) {
	t.Parallel()
	sources := []string{"queue", "cron", "interval", "event", "comms", "manual", "operator"}
	for _, src := range sources {
		src := src
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			agentsDir := t.TempDir()
			m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
triggers:
  - { id: t1, source: ` + src + `, enabled: true }
`
			makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)
			if _, err := agentmanifest.Load(agentsDir, "mytype"); err != nil {
				t.Errorf("Load with source %q: unexpected error: %v", src, err)
			}
		})
	}
}

func TestLoad_CronTrigger(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	m := `
type: mytype
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
triggers:
  - id: priorities-report
    source: cron
    every: 6h
    enabled: true
    deliver: comms
    message: "Post a priorities update."
`
	makeTypeFolder(t, agentsDir, "mytype", m, validSoul, validOperating)
	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tr := tf.Manifest.Triggers[0]
	if tr.Every != "6h" {
		t.Errorf("Triggers[0].Every = %q, want %q", tr.Every, "6h")
	}
	if tr.Deliver != "comms" {
		t.Errorf("Triggers[0].Deliver = %q, want %q", tr.Deliver, "comms")
	}
	if tr.Message == "" {
		t.Error("Triggers[0].Message is empty, want non-empty")
	}
}

// --- ResolveRef tests ---

func TestResolveRef_PathBearingIsLiteral(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	ref := "docs/orchestration-protocol-v2.md"
	got, err := agentmanifest.ResolveRef(agentsDir, "crew", ref)
	if err != nil {
		t.Fatalf("ResolveRef path-bearing: unexpected error: %v", err)
	}
	if got != ref {
		t.Errorf("ResolveRef = %q, want %q (literal)", got, ref)
	}
}

func TestResolveRef_BareRef_SharedFirst(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	// Create shared skill and per-type skill with the same name.
	sharedSkill := filepath.Join(agentsDir, "_skills", "crew-launch")
	if err := os.MkdirAll(sharedSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	typeSkill := filepath.Join(agentsDir, "crew", "crew-launch")
	if err := os.MkdirAll(typeSkill, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := agentmanifest.ResolveRef(agentsDir, "crew", "crew-launch")
	if err != nil {
		t.Fatalf("ResolveRef bare ref: %v", err)
	}
	if got != sharedSkill {
		t.Errorf("ResolveRef = %q, want shared path %q", got, sharedSkill)
	}
}

func TestResolveRef_BareRef_TypeFolderFallback(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	// Only the per-type skill exists (not in _skills/).
	typeSkill := filepath.Join(agentsDir, "crew", "private-skill")
	if err := os.MkdirAll(typeSkill, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := agentmanifest.ResolveRef(agentsDir, "crew", "private-skill")
	if err != nil {
		t.Fatalf("ResolveRef fallback: %v", err)
	}
	if got != typeSkill {
		t.Errorf("ResolveRef = %q, want type-folder path %q", got, typeSkill)
	}
}

func TestResolveRef_BareRef_NotFound(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()

	_, err := agentmanifest.ResolveRef(agentsDir, "crew", "nonexistent-skill")
	if !errors.Is(err, agentmanifest.ErrNotFound) {
		t.Errorf("ResolveRef(nonexistent) error = %v, want ErrNotFound", err)
	}
}

func TestResolveRef_BareRef_FileInTypeFolder(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	// operating.md is a file in the type folder, not a directory.
	typeDir := filepath.Join(agentsDir, "crew")
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(typeDir, "operating.md"), "content")

	got, err := agentmanifest.ResolveRef(agentsDir, "crew", "operating.md")
	if err != nil {
		t.Fatalf("ResolveRef(operating.md): %v", err)
	}
	want := filepath.Join(typeDir, "operating.md")
	if got != want {
		t.Errorf("ResolveRef = %q, want %q", got, want)
	}
}
