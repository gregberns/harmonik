package specaudit_test

// hk-872.35 binding test — Beads-CLI skill present in every agent's launch context.
//
// Spec ref: specs/beads-integration.md §4.9 BI-028.
//
// BI-028 states: "Per [handler-contract.md §4.11], every agent operating in a
// harmonik run MUST have the Beads-CLI skill available in its launch context
// unless a role-specific permission set explicitly excludes it (an unusual
// policy decision logged in the node's YAML policy per [control-points.md §6.3])."
//
// # Audit frame
//
// This test verifies three conditions that together constitute the "present in
// every agent's launch context" obligation at the current bootstrap stage (the
// handler-contract skill-injection mechanism is pending bootstrap; its concrete
// Go implementation lands in a later phase):
//
//  1. Skill file exists — `.claude/skills/beads-cli/SKILL.md` MUST be present
//     in the repository. BI-028 cannot be satisfied if the skill package is
//     absent.
//
//  2. Front-matter name binding — the skill's YAML front-matter MUST declare
//     `name: beads-cli`. A name mismatch breaks handler-contract skill
//     resolution (HC-047) which is deterministic against `required_skills[]`.
//
//  3. BI-028 self-citation — the skill body MUST reference `BI-028`. This
//     ensures the skill is aware of and documents the "every launch context"
//     obligation; skills that drop this citation drift from the requirement
//     they are meant to satisfy.
//
//  4. Agent-configuration listing — `docs/foundation/project-level/
//     agent-configuration.md` MUST list `beads-cli` as a load-bearing skill
//     in its Skills table. This is the authoritative human-readable record of
//     what skills are injected into MVH-required agent nodes per CP-031.
//
// # Failure modes
//
//   - Skill file missing: `.claude/skills/beads-cli/SKILL.md` does not exist.
//   - Front-matter mismatch: the `name:` field is absent or not `beads-cli`.
//   - BI-028 citation missing: the skill body does not mention `BI-028`.
//   - Agent-configuration omission: `agent-configuration.md` does not list
//     `beads-cli` in its Skills section.
//
// # Helper prefix
//
// All package-level identifiers in this file use the b87235Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// b87235FixtureRepoRoot resolves the repository root from the test file's path.
// The test file lives at internal/specaudit/bi028_skill_launch_context_test.go;
// the repo root is two directories up.
func b87235FixtureRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("b87235FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/bi028_skill_launch_context_test.go
	// repo root is two directories up
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// b87235FixtureReadLines reads a file and returns its lines.
// Path must be absolute; the nolint annotation is required per lint rule §5.
func b87235FixtureReadLines(t *testing.T, path string) []string {
	t.Helper()

	//nolint:gosec // G304: path is constructed from runtime.Caller repo-root resolution; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("b87235FixtureReadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("b87235FixtureReadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// TestBI028SkillPresentInLaunchContext is the binding test for BI-028.
//
// It verifies that the Beads-CLI skill package exists, carries the correct
// `name:` front-matter binding, self-documents the BI-028 requirement, and is
// listed in the agent-configuration doc as a load-bearing (MVH-required) skill.
func TestBI028SkillPresentInLaunchContext(t *testing.T) {
	repoRoot := b87235FixtureRepoRoot(t)

	// -----------------------------------------------------------------------
	// Check 1: skill file exists.
	// -----------------------------------------------------------------------
	skillPath := filepath.Join(repoRoot, ".claude", "skills", "beads-cli", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("BI-028 FAIL: Beads-CLI skill file missing — %s does not exist; "+
			"BI-028 requires the skill to be present in every agent's launch context "+
			"(specs/beads-integration.md §4.9)", skillPath)
	} else if err != nil {
		t.Fatalf("BI-028 FAIL: stat %s: %v", skillPath, err)
	}

	skillLines := b87235FixtureReadLines(t, skillPath)

	// -----------------------------------------------------------------------
	// Check 2: front-matter name binding.
	//
	// The SKILL.md front-matter block opens with "---" and closes with "---".
	// We scan lines until the closing delimiter and look for "name: beads-cli".
	// HC-047 requires skill resolution to be deterministic against
	// required_skills[]; the name must match exactly.
	// -----------------------------------------------------------------------
	const expectedName = "name: beads-cli"
	var foundName bool
	inFrontMatter := false
	for _, line := range skillLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if !inFrontMatter {
				inFrontMatter = true
				continue
			}
			// Closing delimiter — stop scanning front matter.
			break
		}
		if inFrontMatter && strings.HasPrefix(trimmed, "name:") {
			if trimmed == expectedName {
				foundName = true
			} else {
				t.Errorf("BI-028 FAIL: skill front-matter name mismatch — got %q, want %q; "+
					"HC-047 requires deterministic name-based resolution", trimmed, expectedName)
			}
			break
		}
	}
	if !foundName {
		t.Errorf("BI-028 FAIL: skill front-matter does not contain %q — "+
			"handler-contract §4.11 HC-047 resolves skills by name; missing or wrong name "+
			"breaks skill provisioning", expectedName)
	}

	// -----------------------------------------------------------------------
	// Check 3: BI-028 self-citation in skill body.
	//
	// The skill MUST reference "BI-028" to document the "every agent launch
	// context by default" obligation. A skill that drops this citation drifts
	// silently from the requirement it serves.
	// -----------------------------------------------------------------------
	var foundBI028 bool
	for _, line := range skillLines {
		if strings.Contains(line, "BI-028") {
			foundBI028 = true
			break
		}
	}
	if !foundBI028 {
		t.Errorf("BI-028 FAIL: .claude/skills/beads-cli/SKILL.md does not mention BI-028; " +
			"the skill must cite specs/beads-integration.md §4.9 BI-028 (every agent launch " +
			"context by default) to avoid silent requirement drift")
	}

	// -----------------------------------------------------------------------
	// Check 4: agent-configuration.md lists beads-cli.
	//
	// docs/foundation/project-level/agent-configuration.md is the authoritative
	// human-readable record of load-bearing (MVH-required) skills. CP-031 binds
	// the Beads-CLI skill into every MVH-required role's default_skills; the
	// configuration doc must reflect this binding.
	// -----------------------------------------------------------------------
	agentConfigPath := filepath.Join(repoRoot, "docs", "foundation", "project-level", "agent-configuration.md")
	if _, err := os.Stat(agentConfigPath); os.IsNotExist(err) {
		t.Fatalf("BI-028 FAIL: agent-configuration.md missing — %s does not exist; "+
			"cannot verify CP-031 listing", agentConfigPath)
	} else if err != nil {
		t.Fatalf("BI-028 FAIL: stat %s: %v", agentConfigPath, err)
	}

	configLines := b87235FixtureReadLines(t, agentConfigPath)
	var foundBeadsCLIEntry bool
	for _, line := range configLines {
		// The Skills table row format is "| `beads-cli` | ... |".
		// Accept any line that contains the backtick-quoted skill name.
		if strings.Contains(line, "`beads-cli`") {
			foundBeadsCLIEntry = true
			break
		}
	}
	if !foundBeadsCLIEntry {
		t.Errorf("BI-028 FAIL: docs/foundation/project-level/agent-configuration.md does not " +
			"list `beads-cli` as a skill; CP-031 requires the Beads-CLI skill to appear in " +
			"every MVH-required role's default_skills and agent-configuration.md is the " +
			"authoritative listing of load-bearing skills")
	}

	if !t.Failed() {
		t.Logf("BI-028 PASS: Beads-CLI skill present at %s, name binding correct, "+
			"BI-028 cited, agent-configuration.md lists beads-cli",
			filepath.Join(".claude", "skills", "beads-cli", "SKILL.md"))
	}
}
