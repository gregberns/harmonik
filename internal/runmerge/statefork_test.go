package runmerge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// A forked child can die before the program it was going to run ever started.
// These tests hold that case for the worktree-state and format-gate commands:
// the ones that mean the same thing twice have to run again, the residual
// commit must not, and the format gate must fail rather than report a clean
// tree it never read.
//
// Bead: hk-7neu1.

func stateForkRead(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // G304: the path is one this test made under t.TempDir()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// stateForkRepo makes a one-commit git repository for these tests to work on.
func stateForkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInDir(t, dir, "init", "-q", "-b", "main", ".")
	gitInDir(t, dir, "config", "user.email", "fixture@example.invalid")
	gitInDir(t, dir, "config", "user.name", "Fixture")
	writeFixtureFile(t, dir, "README", "base\n")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// stateForkSubjectCount counts the commits whose subject is subject. It reads
// `git log`, so it counts what a person would see rather than agreeing with the
// production code by construction.
func stateForkSubjectCount(t *testing.T, dir, subject string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(gitInDir(t, dir, "log", "--format=%s"), "\n") {
		if line == subject {
			n++
		}
	}
	return n
}

// TestCommitResidualDelta_CommitStoppedAfterItLandedDoesNotCommitTwice holds
// the half that a blind retry would get wrong. The commit landed, so running it
// again is both unasked-for and wrong: at best the second git finds nothing to
// commit and reports a failure for work that succeeded.
func TestCommitResidualDelta_CommitStoppedAfterItLandedDoesNotCommitTwice(t *testing.T) {
	dir := stateForkRepo(t)
	writeFixtureFile(t, dir, "new.go", "package x\n")
	runID := fixtureRunID(t)
	subject := strings.SplitN(residualDeltaCommitMessage(runID), "\n", 2)[0]
	countPath := stopGitOnceOn(t, "commit", gitStubAnyArgs, "after")

	// A commit that landed before the signal is not a failure, and reporting one
	// would fail a merge over work that is safely committed.
	if err := CommitResidualDelta(context.Background(), dir, runID); err != nil {
		t.Fatalf("a commit stopped AFTER it landed must not report a failure: %v", err)
	}

	if got := stateForkSubjectCount(t, dir, subject); got != 1 {
		t.Errorf("the worktree holds %d residual-delta commits; want exactly 1", got)
	}
	if got := stubCalls(t, countPath); got != 1 {
		t.Errorf("git commit ran %d times; want exactly 1 — a commit that landed must not run again", got)
	}
}

// TestCommitResidualDelta_CommitStoppedBeforeItRanStillLandsTheDelta holds the
// other half. The delta is the bead's own work, and a commit that never ran
// leaves it uncommitted, which fails the rebase that follows.
func TestCommitResidualDelta_CommitStoppedBeforeItRanStillLandsTheDelta(t *testing.T) {
	dir := stateForkRepo(t)
	writeFixtureFile(t, dir, "new.go", "package x\n")
	runID := fixtureRunID(t)
	subject := strings.SplitN(residualDeltaCommitMessage(runID), "\n", 2)[0]
	countPath := stopGitOnceOn(t, "commit", gitStubAnyArgs, "before")

	if err := CommitResidualDelta(context.Background(), dir, runID); err != nil {
		t.Fatalf("the second attempt landed the delta, so no failure is due: %v", err)
	}

	if got := stateForkSubjectCount(t, dir, subject); got != 1 {
		t.Fatalf("the worktree holds %d residual-delta commits; want exactly 1", got)
	}
	if got := stubCalls(t, countPath); got < 2 {
		t.Fatalf("git commit ran %d time(s); the stopped attempt was never run again", got)
	}
	if status := gitInDir(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("the worktree is still dirty after the residual commit:\n%s", status)
	}
}

// TestRefreshMergedPaths_StoppedRestoreGetsTheWholePathListAgain is the stdin
// case. The path list goes to git on standard input, and a reader is spent by
// the attempt that read it. Hand the second attempt the spent reader and git
// gets an empty pathspec, matches no path, and reports success for work it
// never did — a wrong answer that looks right.
func TestRefreshMergedPaths_StoppedRestoreGetsTheWholePathListAgain(t *testing.T) {
	dir := stateForkRepo(t)
	paths := []string{"a.txt", "b.txt", "c.txt"}
	for _, p := range paths {
		writeFixtureFile(t, dir, p, "committed "+p+"\n")
	}
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "merged work")
	mainTip := gitInDir(t, dir, "rev-parse", "HEAD")
	for _, p := range paths {
		writeFixtureFile(t, dir, p, "stale "+p+"\n")
	}

	countPath := stopGitOnceOn(t, "restore", gitStubAnyArgs, "before")

	refreshMergedPaths(context.Background(), dir, fixtureRunID(t), &eventPayloadCapture{},
		core.BeadID("hk-refresh"), mainTip, paths)

	for _, p := range paths {
		if got, want := stateForkRead(t, filepath.Join(dir, p)), "committed "+p+"\n"; got != want {
			t.Errorf("%s = %q after the refresh; want %q — the second attempt did not get the whole path list", p, got, want)
		}
	}
	if got := stubCalls(t, countPath); got < 2 {
		t.Errorf("git restore ran %d time(s); the stopped attempt was never run again", got)
	}
}

// TestFmtGofumptPass_ListStoppedForGoodFailsTheGate covers the fail-open. The
// gate read a failed gofumpt as "every file is formatted", so a child that
// never ran let unformatted code through. The caller has already checked that
// gofumpt is there, so a failure here is real.
// TestFmtToolWords_LeadsWithTheDiagnostic: a format tool that fails can write to
// both streams at once — the file that broke on standard error, and the files
// that merely need formatting on standard output. The reason has to lead with
// the one that says why the tool failed. Reporting only the drift list names a
// file that is not the problem and hides the file that is, which reads as an
// answer rather than as a missing answer.
func TestFmtToolWords_LeadsWithTheDiagnostic(t *testing.T) {
	failed := &exec.ExitError{Stderr: []byte("zbroken.go:3:9: expected ')', found '{'\n")}

	got := string(fmtToolWords(failed, []byte("drift.go\n")))
	if !strings.HasPrefix(got, "zbroken.go:3:9:") {
		t.Errorf("the words are %q; want the parse error first", got)
	}
	if !strings.Contains(got, "drift.go") {
		t.Errorf("the words are %q; want the drift list kept as well", got)
	}

	if got := string(fmtToolWords(failed, nil)); !strings.Contains(got, "zbroken.go") {
		t.Errorf("with no standard output the words are %q; want the diagnostic", got)
	}
	if got := string(fmtToolWords(errors.New("fork/exec: resource temporarily unavailable"), nil)); got != "" {
		t.Errorf("a child that never ran left words %q; want none — the error itself says it", got)
	}
}

func TestFmtGofumptPass_ListStoppedForGoodFailsTheGate(t *testing.T) {
	dir := stateForkRepo(t)
	bin := filepath.Join(t.TempDir(), "gofumpt")
	// This stub stops on EVERY call: the case is a child that never gets to run
	// at all, not one that recovers on the next attempt.
	//nolint:gosec // G306: a fake gofumpt has to be executable to stand in for one
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nkill -SEGV $$\n"), 0o700); err != nil {
		t.Fatalf("write the fake gofumpt: %v", err)
	}
	emitter := &eventPayloadCapture{}

	dirty, outcome := fmtGofumptPass(context.Background(), dir, bin, true,
		fixtureRunID(t), core.BeadID("hk-fmt"), emitter)

	if outcome == nil {
		t.Fatal("the format gate passed though gofumpt never ran")
	}
	if outcome.Success {
		t.Error("the outcome reports success for a gofumpt that never ran")
	}
	if !strings.HasPrefix(outcome.Reason, "merge_fmt_failed (gofumpt -l): ") {
		t.Errorf("reason = %q; want it to start with the gofumpt -l failure prefix", outcome.Reason)
	}
	if dirty {
		t.Error("dirty = true; a gofumpt that never ran formatted nothing")
	}
	if emitter.eventType != core.EventTypeMergeBuildFailed {
		t.Errorf("emitted %q; want %q so the failure reaches the operator", emitter.eventType, core.EventTypeMergeBuildFailed)
	}
}

// TestCleanUntrackedFiles_StoppedCleanRunsAgain holds the repeatable half: a
// second clean removes whatever the first one did not, and an untracked file
// left behind aborts the rebase that follows.
func TestCleanUntrackedFiles_StoppedCleanRunsAgain(t *testing.T) {
	dir := stateForkRepo(t)
	const junkName = "artifact.bin"
	junk := filepath.Join(dir, junkName)
	writeFixtureFile(t, dir, junkName, "build output\n")
	countPath := stopGitOnceOn(t, "clean", gitStubAnyArgs, "before")

	CleanUntrackedFiles(context.Background(), dir, t.TempDir(), fixtureRunID(t), &eventPayloadCapture{}, core.BeadID("hk-clean"))

	if _, err := os.Stat(junk); err == nil {
		t.Error("the untracked artifact survived the clean; the rebase that follows would abort on it")
	}
	if got := stubCalls(t, countPath); got < 2 {
		t.Errorf("git clean ran %d time(s); the stopped attempt was never run again", got)
	}
}

// TestDiscardDirtyChurn_StoppedCheckoutRunsAgain holds the other repeatable
// half: a checkout of a pathspec puts the committed content back, so it means
// the same thing every time. Churn left dirty aborts the rebase.
func TestDiscardDirtyChurn_StoppedCheckoutRunsAgain(t *testing.T) {
	dir := stateForkRepo(t)
	ledgerPath := filepath.Join(".beads", "issues.jsonl")
	ledger := filepath.Join(dir, ledgerPath)
	writeFixtureFile(t, dir, ledgerPath, "committed\n")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "add the ledger")
	writeFixtureFile(t, dir, ledgerPath, "churn\n")
	countPath := stopGitOnceOn(t, "checkout", gitStubAnyArgs, "before")

	DiscardDirtyChurn(context.Background(), dir, t.TempDir(), fixtureRunID(t), &eventPayloadCapture{}, core.BeadID("hk-churn"))

	if got := stateForkRead(t, ledger); got != "committed\n" {
		t.Errorf("the ledger holds %q; want the committed content back", got)
	}
	if got := stubCalls(t, countPath); got < 2 {
		t.Errorf("git checkout ran %d time(s); the stopped attempt was never run again", got)
	}
}
