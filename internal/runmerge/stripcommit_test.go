package runmerge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The strip commit is the one git command on the merge path that does not mean
// the same thing twice. These tests hold the two halves of that: a git stopped
// BEFORE it committed has to be run again, and a git stopped AFTER it committed
// must not be, or the merge target gets a second commit nobody asked for.
//
// Bead: hk-jbtj6.

func stripFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "fixture@example.invalid")
	run("config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README")
	run("commit", "-q", "-m", "base")

	ctxDir := filepath.Join(dir, RunContextDirPrefix, "run-1")
	if err := os.MkdirAll(ctxDir, 0o750); err != nil {
		t.Fatalf("make the run-context directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "context.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write context.json: %v", err)
	}
	run("add", "-f", "--", RunContextDirPrefix)
	run("commit", "-q", "-m", "add run context")
	return dir
}

// stopGitOnCommit puts a git on PATH that stops itself with a signal on a
// `git commit`, either before it commits or after. Every other git command runs
// as normal. Build the repository BEFORE calling this.
func stopGitOnCommit(t *testing.T, mode string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find the real git: %v", err)
	}
	binDir := t.TempDir()

	// The stub stops the FIRST commit only. A stub that stopped every commit
	// would prove nothing about the retry: the retry would be stopped too, and
	// the test would read as "the fix does not work" whether or not it does.
	countPath := filepath.Join(t.TempDir(), "commits")
	// markerPath, when present, makes rev-parse refuse — the stand-in for a
	// worktree that cannot say whether the commit landed.
	markerPath := filepath.Join(t.TempDir(), "committed")
	var body string
	switch mode {
	case "before":
		body = "kill -SEGV $$\n"
	case "after":
		body = "\"" + realGit + "\" \"$@\"\n      kill -SEGV $$\n"
	case "after-and-head-unreadable":
		body = "\"" + realGit + "\" \"$@\"\n      : > " + markerPath + "\n      kill -SEGV $$\n"
	default:
		t.Fatalf("unknown stub mode %q", mode)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = rev-parse ] && [ -f " + markerPath + " ]; then\n" +
		"    echo 'fatal: fixture refuses rev-parse' >&2\n" +
		"    exit 128\n" +
		"  fi\n" +
		"done\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = commit ]; then\n" +
		"    n=$(cat " + countPath + " 2>/dev/null || echo 0)\n" +
		"    n=$((n+1))\n" +
		"    echo $n > " + countPath + "\n" +
		"    if [ \"$n\" -le 1 ]; then\n" +
		"      " + body +
		"    fi\n" +
		"  fi\n" +
		"done\n" +
		"exec \"" + realGit + "\" \"$@\"\n"

	//nolint:gosec // G306: a fake git has to be executable to stand in for one
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stripSubject is the first line of the strip commit message. The test derives
// it rather than reading a production symbol, so it counts what a person would
// see in `git log` rather than agreeing with the code by construction.
func stripSubject() string {
	return strings.SplitN(stripRunContextCommitMessage, "\n", 2)[0]
}

func stripCommitCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "log", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == stripSubject() {
			n++
		}
	}
	return n
}

func TestStripRunContext_CommitStoppedBeforeItCommittedRunsAgain(t *testing.T) {
	dir := stripFixtureRepo(t)
	stopGitOnCommit(t, "before")

	stripped, err := StripRunContextFromMerge(context.Background(), dir)
	if err != nil {
		t.Fatalf("StripRunContextFromMerge = %v; want the strip to survive one stopped git", err)
	}
	if !stripped {
		t.Error("stripped = false; want true")
	}
	if got := stripCommitCount(t, dir); got != 1 {
		t.Errorf("the worktree holds %d strip commits; want exactly 1", got)
	}
}

func TestStripRunContext_CommitStoppedAfterItCommittedDoesNotCommitTwice(t *testing.T) {
	dir := stripFixtureRepo(t)
	stopGitOnCommit(t, "after")

	stripped, err := StripRunContextFromMerge(context.Background(), dir)
	if err != nil {
		t.Fatalf("StripRunContextFromMerge = %v; want the landed commit to be recognised", err)
	}
	if !stripped {
		t.Error("stripped = false; want true")
	}
	if got := stripCommitCount(t, dir); got != 1 {
		t.Errorf("the worktree holds %d strip commits; want exactly 1 — the second run must not add one", got)
	}
}

// TestStripRunContext_CommitStoppedAndHEADUnreadableFailsRatherThanGuesses is
// the case the first version of this fix got wrong. A worktree that cannot say
// whether the commit landed must not be read as having said yes: reporting
// success there fast-forwards the merge target with the run-context files still
// on it, which is the one outcome this function exists to prevent. Failing the
// merge is the safe answer.
func TestStripRunContext_CommitStoppedAndHEADUnreadableFailsRatherThanGuesses(t *testing.T) {
	dir := stripFixtureRepo(t)
	stopGitOnCommit(t, "after-and-head-unreadable")

	_, err := StripRunContextFromMerge(context.Background(), dir)
	if err == nil {
		t.Fatal("StripRunContextFromMerge reported success though it could not tell whether the commit landed")
	}
	if got := stripCommitCount(t, dir); got != 1 {
		t.Errorf("the worktree holds %d strip commits; want exactly 1 — it must not guess and commit again", got)
	}
}
