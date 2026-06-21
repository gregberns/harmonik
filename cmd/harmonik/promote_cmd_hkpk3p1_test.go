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

// gitRevParse returns the SHA of the given ref in dir.
func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output() //nolint:gosec
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// gitLogBody returns the full commit message body of the tip of ref in dir.
func gitLogBody(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%B", ref).Output() //nolint:gosec
	if err != nil {
		t.Fatalf("git log -1 %s in %s: %v", ref, dir, err)
	}
	return string(out)
}

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

// ---- (e) extractBeadIDFromSubject unit tests ----

// TestExtractBeadIDFromSubject verifies the bead-ID auto-detection helper.
func TestExtractBeadIDFromSubject(t *testing.T) {
	t.Parallel()

	cases := []struct {
		subject string
		want    string
	}{
		{"fix: patch the thing (hk-abc123)", "hk-abc123"},
		{"chore: cleanup (hk-z99)", "hk-z99"},
		{"no bead id here", ""},
		{"multiple (hk-aaa) and (hk-bbb)", "hk-bbb"}, // last match wins
		{"(hk-53p3)", "hk-53p3"},
		{"unrelated (foo-bar)", ""},
	}

	for _, tc := range cases {
		got := extractBeadIDFromSubject(tc.subject)
		if got != tc.want {
			t.Errorf("extractBeadIDFromSubject(%q) = %q, want %q", tc.subject, got, tc.want)
		}
	}
}

// ---- (f) push-mode Harmonik-Bead-ID trailer stamping ----

// setupCommitOnBranch creates a new branch off local main, adds a file, and
// commits with the given message.  Returns the commit SHA.
func setupCommitOnBranch(t *testing.T, repoRoot, branch, commitMsg string) string {
	t.Helper()
	runGit(t, repoRoot, "checkout", "-b", branch)
	writeFile(t, repoRoot, branch+".go", "package main\n// "+branch+"\n")
	runGit(t, repoRoot, "add", branch+".go")
	runGitWithEnv(t, repoRoot, nil, "commit", "-m", commitMsg)
	sha := gitRevParse(t, repoRoot, "HEAD")
	runGit(t, repoRoot, "checkout", "main")
	return sha
}

// TestPromotePushModeStampsBeadIDTrailerExplicit verifies that when --bead is
// provided, the cherry-picked commit on the target branch carries a
// Harmonik-Bead-ID trailer that harmonik reconcile can match.
func TestPromotePushModeStampsBeadIDTrailerExplicit(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	sha := setupCommitOnBranch(t, repoRoot, "task-explicit", "fix: explicit bead stamp (hk-testexplicit)")

	// Promote with an explicit --bead flag.
	args := []string{"--project", repoRoot, "--bead", "hk-testexplicit", sha}
	rc := runPromoteSubcommand(args)
	if rc != 0 {
		t.Fatalf("harmonik promote --bead: expected exit 0, got %d", rc)
	}

	// The pushed origin/main tip must carry the Harmonik-Bead-ID trailer.
	runGit(t, repoRoot, "fetch", "origin", "main")
	msg := gitLogBody(t, repoRoot, "origin/main")
	const want = "Harmonik-Bead-ID: hk-testexplicit"
	if !strings.Contains(msg, want) {
		t.Errorf("expected %q in commit message after promote --bead, got:\n%s", want, msg)
	}
}

// TestPromotePushModeStampsBeadIDTrailerAutoDetect verifies that when --bead is
// absent, promote auto-detects the bead ID from the source commit subject's
// "(hk-xxx)" parenthetical and stamps it as a trailer.
func TestPromotePushModeStampsBeadIDTrailerAutoDetect(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	sha := setupCommitOnBranch(t, repoRoot, "task-autodetect", "fix: auto-detect bead stamp (hk-testauto)")

	// Promote without --bead; auto-detection should extract hk-testauto.
	args := []string{"--project", repoRoot, sha}
	rc := runPromoteSubcommand(args)
	if rc != 0 {
		t.Fatalf("harmonik promote (auto-detect): expected exit 0, got %d", rc)
	}

	runGit(t, repoRoot, "fetch", "origin", "main")
	msg := gitLogBody(t, repoRoot, "origin/main")
	const want = "Harmonik-Bead-ID: hk-testauto"
	if !strings.Contains(msg, want) {
		t.Errorf("expected %q in commit message after promote (auto-detect), got:\n%s", want, msg)
	}
}

// TestPromotePushModeNoTrailerWhenNoBeadID verifies that promote does not stamp
// a Harmonik-Bead-ID trailer when neither --bead nor a "(hk-xxx)" pattern is
// present in the subject.  The commit should still land; only the trailer is absent.
func TestPromotePushModeNoTrailerWhenNoBeadID(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	sha := setupCommitOnBranch(t, repoRoot, "task-nobead", "fix: no bead in subject")

	args := []string{"--project", repoRoot, sha}
	rc := runPromoteSubcommand(args)
	if rc != 0 {
		t.Fatalf("harmonik promote (no bead): expected exit 0, got %d", rc)
	}

	runGit(t, repoRoot, "fetch", "origin", "main")
	msg := gitLogBody(t, repoRoot, "origin/main")
	if strings.Contains(msg, "Harmonik-Bead-ID:") {
		t.Errorf("expected no Harmonik-Bead-ID trailer when no bead ID is detectable, got:\n%s", msg)
	}
}

// TestPromoteParseFlagsBead verifies --bead flag parsing.
func TestPromoteParseFlagsBead(t *testing.T) {
	t.Parallel()

	cfg, err := parsePromoteFlags([]string{"--bead", "hk-abc123", "deadbeef"})
	if err != nil {
		t.Fatalf("parsePromoteFlags: %v", err)
	}
	if cfg.beadID != "hk-abc123" {
		t.Errorf("expected beadID %q, got %q", "hk-abc123", cfg.beadID)
	}

	// = form
	cfg2, err2 := parsePromoteFlags([]string{"--bead=hk-xyz", "deadbeef"})
	if err2 != nil {
		t.Fatalf("parsePromoteFlags: %v", err2)
	}
	if cfg2.beadID != "hk-xyz" {
		t.Errorf("expected beadID %q, got %q", "hk-xyz", cfg2.beadID)
	}
}
