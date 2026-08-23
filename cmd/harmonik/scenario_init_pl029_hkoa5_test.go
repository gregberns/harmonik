//go:build scenario

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenario_Init_PL029_HKoa5 verifies that runInit against a fresh foreign
// git repository exits 0 and produces a bootable, self-consistent project.
func TestScenario_Init_PL029_HKoa5(t *testing.T) {
	if _, err := exec.LookPath("br"); err != nil {
		t.Skip("TestScenario_Init_PL029_HKoa5: 'br' not on PATH — skipping (install beads_rust to run)")
	}
	if _, err := exec.LookPath("harmonik"); err != nil {
		t.Skip("TestScenario_Init_PL029_HKoa5: 'harmonik' not on PATH — skipping (install harmonik to run)")
	}

	foreignRepo := t.TempDir()
	if out, err := exec.Command("git", "-C", foreignRepo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init foreign repo: %v\n%s", err, out)
	}
	_ = exec.Command("git", "-C", foreignRepo, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", foreignRepo, "config", "user.name", "Test").Run()

	if err := os.MkdirAll(filepath.Join(foreignRepo, ".beads"), 0o755); err != nil {
		t.Fatalf("pre-seed .beads/: %v", err)
	}

	var stdout, stderr bytes.Buffer
	const targetBranch = "integration"
	code := runInit([]string{
		"--project", foreignRepo,
		"--target-branch", targetBranch,
		"--no-supervise",
	}, &stdout, &stderr)

	outStr := stdout.String()
	errStr := stderr.String()
	t.Logf("stdout:\n%s", outStr)
	if errStr != "" {
		t.Logf("stderr:\n%s", errStr)
	}

	if code != 0 {
		t.Fatalf("PL-029: runInit returned exit code %d (want 0); stderr=%q stdout=%q",
			code, errStr, outStr)
	}

	wantSkills := []string{
		"captain",
		"crew-launch",
		"keeper",
		"harmonik-dispatch",
		"harmonik-lifecycle",
		"agent-comms",
		"beads-cli",
		"major-issue-fanout",
	}
	skillsRoot := filepath.Join(foreignRepo, ".claude", "skills")
	for _, skill := range wantSkills {
		skillDir := filepath.Join(skillsRoot, skill)
		info, err := os.Stat(skillDir)
		if err != nil {
			t.Errorf("PL-029(a): skill %q not provisioned at %s: %v", skill, skillDir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("PL-029(a): skill %q is not a directory at %s", skill, skillDir)
			continue
		}
		entries, readErr := os.ReadDir(skillDir)
		if readErr != nil || len(entries) == 0 {
			t.Errorf("PL-029(a): skill %q directory is empty or unreadable at %s", skill, skillDir)
		}
	}

	agentsMDPath := filepath.Join(foreignRepo, "AGENTS.md")
	agentsMDBytes, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("PL-029(b): AGENTS.md not created at %s: %v", agentsMDPath, err)
	}
	agentsMD := string(agentsMDBytes)

	if strings.Contains(agentsMD, "$PROJECT_DIR") {
		t.Errorf("PL-029(c): AGENTS.md contains unreplaced $PROJECT_DIR placeholder (substitution failed)")
	}
	if strings.Contains(agentsMD, "$TARGET_BRANCH") {
		t.Errorf("PL-029(c): AGENTS.md contains unreplaced $TARGET_BRANCH placeholder (substitution failed)")
	}

	if !strings.Contains(agentsMD, foreignRepo) {
		t.Errorf("PL-029(c): AGENTS.md does not contain the project dir %q (PROJECT_DIR substitution wrong)", foreignRepo)
	}
	if !strings.Contains(agentsMD, targetBranch) {
		t.Errorf("PL-029(c): AGENTS.md does not contain target branch %q (TARGET_BRANCH substitution wrong)", targetBranch)
	}

	for _, scaffold := range []string{"AGENT_INDEX.md", "STATUS.md"} {
		p := filepath.Join(foreignRepo, scaffold)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("PL-029(c): scaffold %s not created at %s: %v", scaffold, p, err)
		}
	}

	if _, err := os.Stat(filepath.Join(foreignRepo, "TASKS.md")); err == nil {
		t.Errorf("hk-5qey: TASKS.md was scaffolded but is retired in the three-kinds model")
	}

	if !strings.Contains(agentsMD, "harmonik:managed agents-router") {
		t.Errorf("hk-5qey: AGENTS.md missing the 'harmonik:managed agents-router' marker (not the router structure)")
	}

	for _, tier := range []string{
		".harmonik/context/project.yaml",
		".harmonik/context/captain-lanes.md",
		".harmonik/context/roadmap.md",
		"HANDOFF.md",
	} {
		p := filepath.Join(foreignRepo, tier)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("hk-5qey: context tier file %s not created at %s: %v", tier, p, err)
		}
	}

	claudePath := filepath.Join(foreignRepo, "CLAUDE.md")
	linkTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Errorf("PL-029(c): CLAUDE.md symlink not created at %s: %v", claudePath, err)
	} else if linkTarget != "AGENTS.md" {
		t.Errorf("PL-029(c): CLAUDE.md symlink points to %q, want %q", linkTarget, "AGENTS.md")
	}

	wantDirs := []string{
		".harmonik",
		".harmonik/events",
		".harmonik/worktrees",
		".harmonik/beads-intents",
		".harmonik/comms",
		".harmonik/crew",
		".harmonik/keeper",
		".harmonik/queues",
	}
	for _, dir := range wantDirs {
		p := filepath.Join(foreignRepo, dir)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("PL-029(d): runtime dir %s not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("PL-029(d): %s exists but is not a directory", dir)
		}
	}

	for _, cfg := range []string{".harmonik/config.yaml", ".harmonik/branching.yaml", ".harmonik/.gitignore"} {
		p := filepath.Join(foreignRepo, cfg)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("PL-029(d): config file %s not created: %v", cfg, err)
		}
	}

	t.Logf("TestScenario_Init_PL029_HKoa5 PASS: foreign repo at %s fully bootstrapped (exit 0, 8 skills, self-consistent AGENTS.md)", foreignRepo)
}
