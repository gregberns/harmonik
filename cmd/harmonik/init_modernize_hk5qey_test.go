package main

// init_modernize_hk5qey_test.go — unit test for the three-kinds init scaffold
// (hk-5qey). Asserts that `harmonik init` modernizes onto the new instruction
// model: .harmonik/context/ tier files are rendered, TASKS.md is NOT scaffolded
// (retired), and the rendered AGENTS.md is the managed router.
//
// Runs on the default build (no //go:build tag) so it gates `go test
// ./cmd/harmonik/`. Skips when br/harmonik are absent on PATH, since runInit's
// doctor checks require both binaries.
//
// Bead ref: hk-5qey.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitModernize_ContextTiers_HK5qey(t *testing.T) {
	if _, err := exec.LookPath("br"); err != nil {
		t.Skip("TestInitModernize_ContextTiers_HK5qey: 'br' not on PATH — skipping")
	}
	if _, err := exec.LookPath("harmonik"); err != nil {
		t.Skip("TestInitModernize_ContextTiers_HK5qey: 'harmonik' not on PATH — skipping")
	}

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_ = exec.Command("git", "-C", repo, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", repo, "config", "user.name", "Test").Run()

	// Pre-seed .beads/ so runBrInit is a no-op (br init is not under test).
	if err := os.MkdirAll(filepath.Join(repo, ".beads"), 0o755); err != nil {
		t.Fatalf("pre-seed .beads/: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--project", repo, "--no-supervise"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runInit exit %d (want 0); stderr=%q", code, stderr.String())
	}

	// .harmonik/context/ tier files + seed HANDOFF.md must exist.
	for _, f := range []string{
		".harmonik/context/project.yaml",
		".harmonik/context/captain-lanes.md",
		".harmonik/context/roadmap.md",
		"HANDOFF.md",
	} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Errorf("hk-5qey: expected %s to be scaffolded: %v", f, err)
		}
	}

	// TASKS.md is retired — must NOT be scaffolded.
	if _, err := os.Stat(filepath.Join(repo, "TASKS.md")); err == nil {
		t.Errorf("hk-5qey: TASKS.md was scaffolded but is retired")
	}

	// Kept scaffolds.
	for _, f := range []string{"AGENT_INDEX.md", "STATUS.md"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Errorf("hk-5qey: expected %s to still be scaffolded: %v", f, err)
		}
	}

	// AGENTS.md must be the managed three-kinds router.
	agentsMD, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsMD), "harmonik:managed agents-router") {
		t.Errorf("hk-5qey: AGENTS.md missing 'harmonik:managed agents-router' marker (not the router structure)")
	}
	if !strings.Contains(string(agentsMD), "<!-- END harmonik:managed -->") {
		t.Errorf("hk-5qey: AGENTS.md missing the END harmonik:managed marker")
	}
	if strings.Contains(string(agentsMD), "$PROJECT_DIR") || strings.Contains(string(agentsMD), "$TARGET_BRANCH") {
		t.Errorf("hk-5qey: AGENTS.md has unreplaced template placeholders")
	}
}
