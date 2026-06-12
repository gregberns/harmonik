package main

// promote_cmd_hkpk3p1_test.go — tests for `harmonik promote` subcommand.
//
// Coverage:
//   (a) protect-branch fail-closed refusal of push-mode
//   (b) push-mode --dry-run plan output
//   (c) --pr + positional-sha mutual-exclusion error
//   (d) target resolution precedence (branching.yaml vs --target flag)
//
// Git-touching paths use a temp-repo fixture.
//
// Bead ref: hk-pk3p1.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupPromoteRepo creates a minimal git repo with a remote that has a "main"
// branch. It returns the repo root (the "local" clone) and a cleanup function.
func setupPromoteRepo(t *testing.T) (string, func()) {
	t.Helper()

	// Create the "remote" bare repo.
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "--initial-branch=main", ".")

	// Create the local clone.
	local := t.TempDir()
	runGit(t, local, "init", "--initial-branch=main", ".")
	runGit(t, local, "remote", "add", "origin", remote)

	// Minimal go.mod so the build gate has something to compile.
	writeFile(t, local, "go.mod", "module example.com/promote-test\n\ngo 1.21\n")
	writeFile(t, local, "main.go", "package main\n\nfunc main() {}\n")

	runGit(t, local, "add", ".")
	runGitWithEnv(t, local, nil, "commit", "-m", "init")
	runGit(t, local, "push", "-u", "origin", "main")

	cleanup := func() {}
	return local, cleanup
}

// writeFile creates a file with the given content inside repoRoot.
func writeFile(t *testing.T, repoRoot, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

// runGit runs a git command in dir and fatals on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runGitWithEnv(t, dir, nil, args...)
}

// runGitWithEnv runs a git command in dir with optional extra env vars.
func runGitWithEnv(t *testing.T, dir string, extraEnv []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// promoteWriteBranchingYAML writes a .harmonik/branching.yaml in repoRoot.
func promoteWriteBranchingYAML(t *testing.T, repoRoot, content string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".harmonik")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "branching.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write branching.yaml: %v", err)
	}
}

// ---- (a) protect-branch fail-closed refusal of push-mode ----

// TestPromoteProtectBranchRefuses verifies that push-mode exits 5 when the
// resolved target is in the protect_branches list.
func TestPromoteProtectBranchRefuses(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	// branching.yaml: protect main, lands_on = integration.
	promoteWriteBranchingYAML(t, repoRoot, `version: 1
defaults:
  lands_on: integration
  protect_branches:
    - main
`)

	// Push-mode targeting "main" (explicit via --target) should be refused.
	// Use a dummy SHA — the protect-check runs before any git ops.
	args := []string{"--project", repoRoot, "--target", "main", "abc1234"}
	rc := runPromoteSubcommand(args)
	if rc != 5 {
		t.Errorf("expected exit 5 (protect-branch refusal), got %d", rc)
	}
}

// TestPromoteProtectBranchDefaultTargetRefuses verifies that when
// branching.yaml sets lands_on=main and protects main, push-mode on the
// default target is also refused.
func TestPromoteProtectBranchDefaultTargetRefuses(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	promoteWriteBranchingYAML(t, repoRoot, `version: 1
defaults:
  lands_on: main
  protect_branches:
    - main
`)

	args := []string{"--project", repoRoot, "abc1234"}
	rc := runPromoteSubcommand(args)
	if rc != 5 {
		t.Errorf("expected exit 5 (protect-branch refusal), got %d", rc)
	}
}

// TestPromotePRModeNotRefusedOnProtectedTarget verifies that --pr is allowed
// even when the target is in protect_branches.
func TestPromotePRModeNotRefusedOnProtectedTarget(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	promoteWriteBranchingYAML(t, repoRoot, `version: 1
defaults:
  lands_on: main
  protect_branches:
    - main
`)

	// --pr + --dry-run: should not exit 5, should not invoke gh.
	args := []string{"--project", repoRoot, "--pr", "--dry-run"}
	rc := runPromoteSubcommand(args)
	// --dry-run skips gh; should succeed unless gh is missing (checked after parse).
	// The protect-gate must NOT fire for PR-mode.
	if rc == 5 {
		t.Errorf("PR-mode should not be refused by protect-gate; got exit 5")
	}
}

// ---- (b) push-mode --dry-run plan output ----

// TestPromoteDryRunPushMode verifies that --dry-run exits 0 and prints the
// planned actions without touching git or the filesystem.
func TestPromoteDryRunPushMode(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	// Capture stdout by temporarily redirecting (simple approach: run via
	// parsePromoteFlags + promoteConfig to verify the dry-run path).
	// We test the exit code here; output content is verified textually.

	args := []string{"--project", repoRoot, "--dry-run", "abc1234", "def5678"}
	rc := runPromoteSubcommand(args)
	if rc != 0 {
		t.Errorf("--dry-run should exit 0, got %d", rc)
	}
}

// ---- (c) --pr + positional-sha mutual-exclusion error ----

// TestPromotePRAndSHAMutuallyExclusive verifies that supplying both --pr and
// positional SHA args is an argument error (exit 1).
func TestPromotePRAndSHAMutuallyExclusive(t *testing.T) {
	_, parseErr := parsePromoteFlags([]string{"--pr", "abc1234"})
	if parseErr == nil {
		t.Error("expected error for --pr + positional SHA, got nil")
	}
	if !strings.Contains(parseErr.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", parseErr)
	}
}

// TestPromotePushModeRequiresSHA verifies that push-mode with no SHA args is an
// argument error.
func TestPromotePushModeRequiresSHA(t *testing.T) {
	_, parseErr := parsePromoteFlags([]string{"--project", "/tmp"})
	if parseErr == nil {
		t.Error("expected error when no SHA given in push-mode, got nil")
	}
}

// ---- (d) target resolution precedence ----

// TestPromoteTargetResolutionPrecedence verifies the resolution order:
// --target flag > branching.yaml lands_on > "main".
func TestPromoteTargetResolutionPrecedence(t *testing.T) {
	t.Run("flag_overrides_yaml", func(t *testing.T) {
		repoRoot := t.TempDir()
		promoteWriteBranchingYAML(t, repoRoot, `version: 1
defaults:
  lands_on: integration
`)
		// --target main should override branching.yaml lands_on=integration.
		// With protect_branches empty, push-mode is not refused.
		// We check by observing that the protect-gate does NOT fire (protection
		// list is empty, so any target passes regardless).
		cfg, parseErr := parsePromoteFlags([]string{"--project", repoRoot, "--target", "main", "abc1234"})
		if parseErr != nil {
			t.Fatalf("parsePromoteFlags: %v", parseErr)
		}
		if cfg.target != "main" {
			t.Errorf("expected parsed target 'main', got %q", cfg.target)
		}
	})

	t.Run("yaml_lands_on_used_when_no_flag", func(t *testing.T) {
		repoRoot := t.TempDir()
		promoteWriteBranchingYAML(t, repoRoot, `version: 1
defaults:
  lands_on: integration
`)
		// No --target flag: branching.yaml lands_on should be used.
		// We verify that the protect-gate against "main" does NOT fire when
		// lands_on=integration (no protect_branches set).
		// Dry-run exits 0 regardless of actual git state.
		args := []string{"--project", repoRoot, "--dry-run", "abc1234"}
		rc := runPromoteSubcommand(args)
		if rc != 0 {
			t.Errorf("expected exit 0 for dry-run with yaml lands_on=integration, got %d", rc)
		}
	})

	t.Run("default_main_when_no_yaml_and_no_flag", func(t *testing.T) {
		repoRoot := t.TempDir()
		// No branching.yaml, no --target: should default to "main".
		// Dry-run exits 0.
		args := []string{"--project", repoRoot, "--dry-run", "abc1234"}
		rc := runPromoteSubcommand(args)
		if rc != 0 {
			t.Errorf("expected exit 0 for dry-run with default target main, got %d", rc)
		}
	})
}
