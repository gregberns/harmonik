package agentmanifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/agentmanifest"
)

// A role folder holds a role's soul.md and operating.md so any agent can read and
// follow them with no harmonik process involved. The type folder under
// .harmonik/agents/<type>/ then keeps only the harmonik-side wiring and names the
// role folder with a `role:` key.
//
// Every test here writes DIFFERENT content into the type folder than into the role
// folder. That is deliberate: if Load silently ignored `role:` and kept reading the
// type folder, these tests would still find a soul.md and an operating.md and would
// pass on the wrong content. Asserting on which content came back is the only thing
// that can fail.

const roleManifest = `
type: mytype
cardinality: { min: 0, max: n }
harness: claude
role: roles/mytype

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

const roleSoul = `**I am** mytype — the copy that lives in the role folder.

**I do**
- Come from roles/mytype/soul.md.

**I do NOT**
- Come from the type folder.

**I escalate to** captain.
`

const roleOperating = `## On wake
1. Read the role folder copy.

## Loop
1. Do work.

## Skills I use
- skill-a — when needed.

## Bounds
- Do not overstep.
`

// makeRepo builds a realistic <root>/.harmonik/agents layout and returns the repo
// root and the agents dir. The role path is resolved against the repo root, which
// is the agents dir's grandparent, so a bare t.TempDir() as agentsDir would put the
// role folder outside the temp dir entirely.
func makeRepo(t *testing.T) (repoRoot, agentsDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	agentsDir = filepath.Join(repoRoot, ".harmonik", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", agentsDir, err)
	}
	return repoRoot, agentsDir
}

// makeRoleFolder writes a role folder at <repoRoot>/roles/mytype.
func makeRoleFolder(t *testing.T, repoRoot string) {
	t.Helper()
	dir := filepath.Join(repoRoot, "roles", "mytype")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	writeFile(t, filepath.Join(dir, "soul.md"), roleSoul)
	writeFile(t, filepath.Join(dir, "operating.md"), roleOperating)
}

func TestLoad_RoleFolderSuppliesSoulAndOperating(t *testing.T) {
	t.Parallel()
	repoRoot, agentsDir := makeRepo(t)
	// The type folder carries the OTHER content, so reading the wrong one is visible.
	makeTypeFolder(t, agentsDir, roleManifest)
	makeRoleFolder(t, repoRoot)

	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(tf.SoulContent, "the copy that lives in the role folder") {
		t.Errorf("soul came from the type folder, not the role folder:\n%s", tf.SoulContent)
	}
	if !strings.Contains(tf.OperatingContent, "Read the role folder copy") {
		t.Errorf("operating came from the type folder, not the role folder:\n%s", tf.OperatingContent)
	}
	wantRoleDir := filepath.Join(repoRoot, "roles", "mytype")
	if tf.RoleDir != wantRoleDir {
		t.Errorf("RoleDir = %q, want %q", tf.RoleDir, wantRoleDir)
	}
	// Dir still points at the type folder — the wiring did not move.
	if tf.Dir != filepath.Join(agentsDir, "mytype") {
		t.Errorf("Dir = %q, want the type folder", tf.Dir)
	}
}

func TestLoad_NoRoleReadsTypeFolder(t *testing.T) {
	t.Parallel()
	repoRoot, agentsDir := makeRepo(t)
	makeTypeFolder(t, agentsDir, validManifest)
	// A role folder exists but the manifest does not name it, so it must be ignored.
	makeRoleFolder(t, repoRoot)

	tf, err := agentmanifest.Load(agentsDir, "mytype")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(tf.SoulContent, "the copy that lives in the role folder") {
		t.Error("a manifest with no role: key read the role folder anyway")
	}
	if tf.RoleDir != tf.Dir {
		t.Errorf("RoleDir = %q, want it to equal Dir %q when no role is named", tf.RoleDir, tf.Dir)
	}
}

func TestLoad_RoleFolderMissingNamesTheDirectoryItRead(t *testing.T) {
	t.Parallel()
	_, agentsDir := makeRepo(t)
	// Type folder is complete; the role folder was never created.
	makeTypeFolder(t, agentsDir, roleManifest)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if err == nil {
		t.Fatal("want an error when the role folder is absent, got nil")
	}
	if !errors.Is(err, agentmanifest.ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
	// The message must name the role folder. Pointing a reader at the type folder,
	// which does have the file, is what makes this class of error waste time.
	if !strings.Contains(err.Error(), filepath.Join("roles", "mytype")) {
		t.Errorf("error does not name the role folder it failed to read: %v", err)
	}
}

func TestLoad_RoleAbsolutePathRefused(t *testing.T) {
	t.Parallel()
	_, agentsDir := makeRepo(t)
	abs := strings.Replace(roleManifest, "role: roles/mytype", "role: /etc/roles/mytype", 1)
	makeTypeFolder(t, agentsDir, abs)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if err == nil {
		t.Fatal("want an error for an absolute role path, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should say the path is absolute, got: %v", err)
	}
}

func TestLoad_RoleEscapingRepoRootRefused(t *testing.T) {
	t.Parallel()
	_, agentsDir := makeRepo(t)
	esc := strings.Replace(roleManifest, "role: roles/mytype", "role: ../../../etc", 1)
	makeTypeFolder(t, agentsDir, esc)

	_, err := agentmanifest.Load(agentsDir, "mytype")
	if err == nil {
		t.Fatal("want an error for a role path that climbs out of the repo, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error should say the path escapes the repo root, got: %v", err)
	}
}
