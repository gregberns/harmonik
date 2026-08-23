package hermetic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetup_GlobalGitConfigTurnsSigningOff is the direct guard on the defect that
// failed 440 tests across six core packages: an operator with commit.gpgsign=true
// in their global config. The package doc carries the method behind that count.
//
// It does not read the generated file — it runs the real git and asks what git
// resolved, so the test still holds if git changes how it merges config sources.
func TestSetup_GlobalGitConfigTurnsSigningOff(t *testing.T) {
	cleanup := Setup()
	defer cleanup()

	for _, key := range []string{"commit.gpgsign", "tag.gpgsign"} {
		//nolint:gosec // G204: key comes from the literal list above, not from input.
		out, err := exec.CommandContext(t.Context(), "git", "config", "--get", key).Output()
		if err != nil {
			t.Fatalf("git config --get %s: %v", key, err)
		}
		if got := strings.TrimSpace(string(out)); got != "false" {
			t.Errorf("git resolved %s = %q, want \"false\"; a host with signing on would fail every git fixture", key, got)
		}
	}
}

// TestSetup_GitCommitSucceedsInAFreshRepo is the end-to-end version of the test
// above: the thing every fixture in the tree actually does. A machine with
// signing on, or with no user identity at all, fails this without Setup.
func TestSetup_GitCommitSucceedsInAFreshRepo(t *testing.T) {
	cleanup := Setup()
	defer cleanup()

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "initial")
}

// TestSetup_ClaudeStatePointsAwayFromHome guards the two seams that keep the
// tests off the operator's real Claude state: the config file the trust writer
// rewrites under a lock, and the transcript directory the work loop reads.
func TestSetup_ClaudeStatePointsAwayFromHome(t *testing.T) {
	cleanup := Setup()
	defer cleanup()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	for _, key := range []string{"HARMONIK_CLAUDE_CONFIG_PATH", "HARMONIK_CLAUDE_PROJECTS_DIR"} {
		got := os.Getenv(key)
		if got == "" {
			t.Errorf("%s is empty; production code then falls back to the operator's home", key)
			continue
		}
		if strings.HasPrefix(got, home+string(os.PathSeparator)) {
			t.Errorf("%s = %q, which is inside the operator's home %q", key, got, home)
		}
	}
}

// TestSetup_ClearsInheritedHarmonikEnv guards clearedEnv. Each of those variables
// is exported in a live harmonik agent pane and absent on an assessor's machine,
// so inheriting one makes the same test ask two different questions.
func TestSetup_ClearsInheritedHarmonikEnv(t *testing.T) {
	for _, key := range clearedEnv {
		t.Setenv(key, "inherited-value")
	}

	cleanup := Setup()
	defer cleanup()

	for _, key := range clearedEnv {
		if got, present := os.LookupEnv(key); present {
			t.Errorf("%s survived Setup with value %q; it must be cleared", key, got)
		}
	}
}

// TestSetup_RespectsAnOuterValue is the counterpart: a harness that has already
// chosen a Claude path keeps it. Without this, a CI runner or an operator
// reproducing a failure cannot point the suite anywhere.
func TestSetup_RespectsAnOuterValue(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "outer.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", outer)

	cleanup := Setup()
	defer cleanup()

	if got := os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH"); got != outer {
		t.Errorf("HARMONIK_CLAUDE_CONFIG_PATH = %q, want the outer value %q", got, outer)
	}
}

// TestSetup_OverridesAnOuterGitConfig is the case where deferring to the outer
// value would be wrong. A CI runner that exports GIT_CONFIG_GLOBAL for its own
// reasons must not thereby switch git isolation off for the whole tree — the
// failures would then read as product regressions rather than as the host.
func TestSetup_OverridesAnOuterGitConfig(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "outer-gitconfig")
	if err := os.WriteFile(outer, []byte("[commit]\n\tgpgsign = true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", outer)

	cleanup := Setup()
	defer cleanup()

	if got := os.Getenv("GIT_CONFIG_GLOBAL"); got == outer {
		t.Fatalf("GIT_CONFIG_GLOBAL still points at the outer file %q; the fixtures read the host", got)
	}
	out, err := exec.CommandContext(t.Context(), "git", "config", "--get", "commit.gpgsign").Output()
	if err != nil {
		t.Fatalf("git config --get commit.gpgsign: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "false" {
		t.Errorf("git resolved commit.gpgsign = %q through the outer config, want \"false\"", got)
	}
}

// TestSetup_CleanupRemovesTheRoot keeps the helper from leaving a directory
// behind on every test binary in the tree.
//
// The root is read back out of GIT_CONFIG_GLOBAL, which is safe only because
// Setup writes that one unconditionally. The prefix check below is the assertion
// that keeps it safe: if the value were ever an inherited path, this test would
// fail rather than assert that somebody else's directory was deleted.
func TestSetup_CleanupRemovesTheRoot(t *testing.T) {
	cleanup := Setup()
	root := filepath.Dir(os.Getenv("GIT_CONFIG_GLOBAL"))
	if !strings.HasPrefix(filepath.Base(root), "harmonik-hermetic-") {
		t.Fatalf("root %q is not a directory Setup created", root)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("stat %s after Setup: %v", root, err)
	}

	cleanup()

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("root %s still present after cleanup (err=%v)", root, err)
	}
}
