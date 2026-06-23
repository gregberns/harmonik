package main

// captain_boot_assets_hk2nmbq_test.go — RED-then-GREEN: start captain on a
// never-initialised foreign project provisions the boot assets the agent needs.
//
// Without the hk-2nmbq fix, .claude/skills/ was absent unless the operator
// separately ran `harmonik init` first. The captain's STARTUP.md (which the
// captain reads immediately on boot) silently did not exist. This test is the
// regression guard: it asserts that after runCaptainLaunchWithOps returns, the
// sentinel skill file .claude/skills/captain/STARTUP.md exists in the project.
//
// Analogous to TestKeeperScriptsEmbedInSync (hk-ybmqp) for the keeper scripts —
// same embed-and-extract pattern, same create-if-missing semantics.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCaptainLaunch_ProvisionesSkillsOnForeignProject_hk2nmbq asserts that
// `start captain` provisions .claude/skills/captain/STARTUP.md before launching
// the agent, even when the project has never had `harmonik init` run.
func TestCaptainLaunch_ProvisionesSkillsOnForeignProject_hk2nmbq(t *testing.T) {
	run, _ := captureRunHkly0n()
	proj := t.TempDir() // foreign project: no .beads, no .harmonik, no .claude

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, &fakeCaptainOps{})
	if code != 0 {
		t.Fatalf("runCaptainLaunchWithOps exit = %d, want 0", code)
	}

	// The sentinel file the captain reads first on boot (STARTUP.md).
	skillPath := filepath.Join(proj, ".claude", "skills", "captain", "STARTUP.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf(".claude/skills/captain/STARTUP.md not provisioned on foreign project: %v", err)
	}
}

// TestCaptainLaunch_SkillsIdempotentOnInitedProject_hk2nmbq asserts that
// ensureBootAssets does NOT overwrite an existing STARTUP.md (create-if-missing).
func TestCaptainLaunch_SkillsIdempotentOnInitedProject_hk2nmbq(t *testing.T) {
	run, _ := captureRunHkly0n()
	proj := t.TempDir()

	// Pre-plant a custom STARTUP.md to verify it is not overwritten.
	skillDir := filepath.Join(proj, ".claude", "skills", "captain")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", skillDir, err)
	}
	const sentinel = "# custom captain skill — must not be overwritten\n"
	skillPath := filepath.Join(skillDir, "STARTUP.md")
	if err := os.WriteFile(skillPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("write %s: %v", skillPath, err)
	}

	code := runCaptainLaunchWithOps([]string{"--project", proj}, run, noopKeeperHkly0n, &fakeCaptainOps{})
	if code != 0 {
		t.Fatalf("runCaptainLaunchWithOps exit = %d, want 0", code)
	}

	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read %s after launch: %v", skillPath, err)
	}
	if string(got) != sentinel {
		t.Errorf("STARTUP.md was overwritten; got %q, want %q", string(got), sentinel)
	}
}
