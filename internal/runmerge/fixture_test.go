package runmerge_test

// fixture_test.go — the git-repo fixtures the moved merge-path tests need.
//
// These four helpers are DUPLICATED from internal/daemon/mergetomain_hkftyvo_test.go
// rather than moved. That file cannot follow the merge path into this package: it
// drives the full work loop through daemon.ExportedRunWorkLoop,
// daemon.ExportedWorkLoopDeps, daemon.WorkLoopDepsParams and
// daemon.ExportedProductionWorktreeFactory, and 20 daemon test files consume its
// fixture family (8 of which cannot move either). Importing it from here would
// give this package's external test binary an internal/daemon edge — exactly what
// the P2 E5 RT13 depguard deny rule forbids. Duplicating four ~20-line git-repo
// setup helpers is the cheaper trade.
//
// Origin: internal/daemon/mergetomain_hkftyvo_test.go (hk-ftyvo).
// Bead: P2 unit E5 RT13.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// mergeToMainFixtureGitRepo initialises a git repository in dir with:
//   - git identity set to daemon@harmonik.local
//   - "main" branch with an initial commit
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

// mergeToMainFixtureProjectDir creates the minimal .harmonik/ directory tree.
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

// mergeToMainFixtureHeadSHA resolves the HEAD SHA of branch in repoRoot.
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

// mergeToMainFixtureAdvanceMain creates a diverging commit on main in repoRoot
// so that any run-branch is no longer a fast-forward. The commit touches a
// different file than the agent's work.txt, so a rebase will succeed without
// conflicts.
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

// discardingEmitter is a handlercontract.EventEmitter that drops every event.
// The merge-path tests in this package assert on git state, not on the event
// stream, so the daemon's recording stubEventCollector (146 consumers, cannot
// move) is not needed here.
type discardingEmitter struct{}

func (discardingEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }

func (discardingEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}
