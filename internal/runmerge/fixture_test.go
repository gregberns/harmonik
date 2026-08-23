package runmerge_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func mergeToMainFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	initPath := filepath.Join(dir, "README")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(initPath, []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")
}

func mergeToMainFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

func mergeToMainFixtureHeadSHA(t *testing.T, repoRoot, branch string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mergeToMainFixtureHeadSHA: git rev-parse refs/heads/%s: %v", branch, err)
	}
	return strings.TrimRight(string(out), "\n")
}

func mergeToMainFixtureAdvanceMain(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureAdvanceMain: git %v: %v\n%s", args, err, out)
		}
	}
	divergePath := filepath.Join(repoRoot, "DIVERGE")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(divergePath, []byte("diverge\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureAdvanceMain: WriteFile: %v", err)
	}
	run("add", "DIVERGE")
	run("commit", "-m", "diverging commit on main")
}

type discardingEmitter struct{}

func (discardingEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }

func (discardingEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}
