//go:build scenario

package main

// scenario_init_pl029_hkoa5_test.go — scenario test: harmonik init from an
// installed binary against a foreign (non-harmonik-source) repo provisions the
// 8 fleet skills and a self-consistent AGENTS.md, and exits 0 (PL-029, hk-oa5).
//
// # What is tested (specs/process-lifecycle.md §4 PL-029)
//
//   PL-029(a) — 8 fleet skills provisioned from binary-embedded assets:
//     captain, crew-launch, keeper, harmonik-dispatch, harmonik-lifecycle,
//     agent-comms, beads-cli, major-issue-fanout.
//
//   PL-029(b) — AGENTS.md rendered from the embedded template (not from disk).
//
//   PL-029(c) — Self-consistent render: no unreplaced $PROJECT_DIR or
//     $TARGET_BRANCH placeholders; scaffold files (AGENT_INDEX.md, STATUS.md,
//     TASKS.md) created; CLAUDE.md → AGENTS.md symlink created.
//
//   PL-029(d) — Runtime directories created: .harmonik/{comms,crew,keeper,queues}.
//
//   Exit 0 — the primary PL-029 success criterion.
//
// # Approach
//
// The test calls runInit directly (package-main internal function), skipping the
// supervisor step via --no-supervise and pre-seeding the .beads/ directory to
// bypass the br-init subprocess (br init is not the behaviour under test here).
// The doctor checks still require br and harmonik on PATH; the test skips when
// either binary is absent so it stays useful on environments without beads_rust
// installed, while failing explicitly on environments where both tools are present
// but init is broken.
//
// Run independently (daemon gate skips //go:build scenario):
//
//	go test -tags scenario -run TestScenario_Init_PL029_HKoa5 ./cmd/harmonik/...
//
// Spec ref: specs/process-lifecycle.md §4 PL-029.
// Bead ref: hk-oa5.

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
	// Skip when prerequisites are absent — this scenario requires real binaries.
	if _, err := exec.LookPath("br"); err != nil {
		t.Skip("TestScenario_Init_PL029_HKoa5: 'br' not on PATH — skipping (install beads_rust to run)")
	}
	if _, err := exec.LookPath("harmonik"); err != nil {
		t.Skip("TestScenario_Init_PL029_HKoa5: 'harmonik' not on PATH — skipping (install harmonik to run)")
	}

	// Create a fresh foreign repository (not the harmonik source tree).
	foreignRepo := t.TempDir()
	if out, err := exec.Command("git", "-C", foreignRepo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init foreign repo: %v\n%s", err, out)
	}
	// git init requires a user config for some operations; set minimal identity.
	_ = exec.Command("git", "-C", foreignRepo, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", foreignRepo, "config", "user.name", "Test").Run()

	// Pre-seed .beads/ so runBrInit is a no-op — br init is not under test here.
	if err := os.MkdirAll(filepath.Join(foreignRepo, ".beads"), 0o755); err != nil {
		t.Fatalf("pre-seed .beads/: %v", err)
	}

	// Run init with --no-supervise (no daemon required) and a non-default branch
	// so we can verify $TARGET_BRANCH substitution.
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

	// ── PL-029(a): 8 fleet skills provisioned ────────────────────────────────────

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
		// Each skill directory must contain at least one file (e.g. SKILL.md).
		entries, readErr := os.ReadDir(skillDir)
		if readErr != nil || len(entries) == 0 {
			t.Errorf("PL-029(a): skill %q directory is empty or unreadable at %s", skill, skillDir)
		}
	}

	// ── PL-029(b,c): AGENTS.md self-consistent render ────────────────────────────

	agentsMDPath := filepath.Join(foreignRepo, "AGENTS.md")
	agentsMDBytes, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("PL-029(b): AGENTS.md not created at %s: %v", agentsMDPath, err)
	}
	agentsMD := string(agentsMDBytes)

	// Template variables must be replaced — no bare placeholder strings left.
	if strings.Contains(agentsMD, "$PROJECT_DIR") {
		t.Errorf("PL-029(c): AGENTS.md contains unreplaced $PROJECT_DIR placeholder (substitution failed)")
	}
	if strings.Contains(agentsMD, "$TARGET_BRANCH") {
		t.Errorf("PL-029(c): AGENTS.md contains unreplaced $TARGET_BRANCH placeholder (substitution failed)")
	}

	// Substituted values must appear.
	if !strings.Contains(agentsMD, foreignRepo) {
		t.Errorf("PL-029(c): AGENTS.md does not contain the project dir %q (PROJECT_DIR substitution wrong)", foreignRepo)
	}
	if !strings.Contains(agentsMD, targetBranch) {
		t.Errorf("PL-029(c): AGENTS.md does not contain target branch %q (TARGET_BRANCH substitution wrong)", targetBranch)
	}

	// ── PL-029(c): scaffold files created ────────────────────────────────────────

	for _, scaffold := range []string{"AGENT_INDEX.md", "STATUS.md", "TASKS.md"} {
		p := filepath.Join(foreignRepo, scaffold)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("PL-029(c): scaffold %s not created at %s: %v", scaffold, p, err)
		}
	}

	// CLAUDE.md → AGENTS.md symlink.
	claudePath := filepath.Join(foreignRepo, "CLAUDE.md")
	linkTarget, err := os.Readlink(claudePath)
	if err != nil {
		t.Errorf("PL-029(c): CLAUDE.md symlink not created at %s: %v", claudePath, err)
	} else if linkTarget != "AGENTS.md" {
		t.Errorf("PL-029(c): CLAUDE.md symlink points to %q, want %q", linkTarget, "AGENTS.md")
	}

	// ── PL-029(d): runtime directories created ────────────────────────────────────

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

	// .harmonik/config.yaml and .harmonik/branching.yaml written.
	for _, cfg := range []string{".harmonik/config.yaml", ".harmonik/branching.yaml", ".harmonik/.gitignore"} {
		p := filepath.Join(foreignRepo, cfg)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("PL-029(d): config file %s not created: %v", cfg, err)
		}
	}

	t.Logf("TestScenario_Init_PL029_HKoa5 PASS: foreign repo at %s fully bootstrapped (exit 0, 8 skills, self-consistent AGENTS.md)", foreignRepo)
}
