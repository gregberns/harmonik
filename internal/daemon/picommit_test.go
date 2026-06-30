package daemon_test

// picommit_test.go — Refs:<bead> trailer guarantee tests for the Pi harness
// (codename:pilot, PI-030/PI-031, hk-mazln).
//
// Coverage (ensurePiRefsTrailer decision table):
//   1. Pi self-committed WITH the trailer → no-op (already_present).
//   2. Pi edited but did NOT commit → fallback CREATES a commit with the trailer.
//   3. Pi committed WITHOUT the trailer → fallback AMENDS HEAD to add it.
//   4. Pi did nothing (clean worktree, HEAD unchanged) → no_change, no fabrication.
//   5. Empty beadID → error (guard).
//
// Mirrors codexcommit_test.go in structure and helper naming.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// test git helpers (real git in t.TempDir)
// ─────────────────────────────────────────────────────────────────────────────

func piCommitGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func piCommitGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// piCommitRepo creates a temp git repo with one initial commit and returns the
// path and the initial HEAD SHA (the "parent" before a Pi turn).
func piCommitRepo(t *testing.T) (wtPath, parentSHA string) {
	t.Helper()
	dir := t.TempDir()
	piCommitGit(t, dir, "init", "--initial-branch=main")
	piCommitGit(t, dir, "config", "user.email", "test@test.com")
	piCommitGit(t, dir, "config", "user.name", "Test")
	piCommitGit(t, dir, "config", "commit.gpgsign", "false")
	piCommitWriteFile(t, dir, "seed.txt", "initial")
	piCommitGit(t, dir, "add", ".")
	piCommitGit(t, dir, "commit", "-m", "init")
	return dir, piCommitGitOut(t, dir, "rev-parse", "HEAD")
}

func piCommitWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	//nolint:gosec // test fixture file
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func piCommitHeadBody(t *testing.T, dir string) string {
	t.Helper()
	return piCommitGitOut(t, dir, "log", "-1", "--format=%B", "HEAD")
}

func piCommitCount(t *testing.T, dir string) int {
	t.Helper()
	out := piCommitGitOut(t, dir, "rev-list", "--count", "HEAD")
	n := 0
	for _, c := range out {
		n = n*10 + int(c-'0')
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// FALLBACK — ensurePiRefsTrailer
// ─────────────────────────────────────────────────────────────────────────────

// TestPiFallback_AlreadyCommittedWithTrailer_NoOp verifies the no-op path: Pi
// already committed WITH the trailer → ensurePiRefsTrailer makes no new commit
// and reports piRefsAlreadyPresent.
func TestPiFallback_AlreadyCommittedWithTrailer_NoOp(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-noop")
	piCommitWriteFile(t, dir, "work.txt", "pi edit")
	piCommitGit(t, dir, "add", ".")
	piCommitGit(t, dir, "commit", "-m", "feat: do the thing\n\nRefs: "+string(beadID))
	headBefore := piCommitGitOut(t, dir, "rev-parse", "HEAD")
	countBefore := piCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensurePiRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedPiRefsAlreadyPresent {
		t.Errorf("outcome = %v; want already_present", outcome)
	}
	if got := piCommitGitOut(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD changed on no-op path: %s -> %s", headBefore, got)
	}
	if got := piCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count changed on no-op path: %d -> %d", countBefore, got)
	}
}

// TestPiFallback_EditedButNotCommitted_CreatesCommit verifies the core fallback:
// Pi edited files (dirty worktree) but produced NO commit → the fallback stages
// everything and CREATES a commit carrying the trailer.
func TestPiFallback_EditedButNotCommitted_CreatesCommit(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-create")
	piCommitWriteFile(t, dir, "seed.txt", "modified by pi")
	piCommitWriteFile(t, dir, "new.txt", "new file from pi")
	countBefore := piCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensurePiRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedPiRefsCommitted {
		t.Errorf("outcome = %v; want committed", outcome)
	}
	if got := piCommitGitOut(t, dir, "rev-parse", "HEAD"); got == parentSHA {
		t.Error("HEAD did not advance past parent after fallback commit")
	}
	if got := piCommitCount(t, dir); got != countBefore+1 {
		t.Errorf("commit count = %d; want %d (one fallback commit)", got, countBefore+1)
	}
	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("verify trailer: %v", err)
	}
	if !has {
		t.Errorf("fallback commit missing 'Refs: %s' trailer; body=%q", beadID, piCommitHeadBody(t, dir))
	}
	if status := piCommitGitOut(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("worktree still dirty after fallback commit: %q", status)
	}
}

// TestPiFallback_CommittedWithoutTrailer_Amends verifies the amend posture: Pi
// committed WITHOUT the trailer → the fallback AMENDS HEAD to append the trailer,
// keeping the same tree and a single commit (no empty follow-up).
func TestPiFallback_CommittedWithoutTrailer_Amends(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-amend")
	piCommitWriteFile(t, dir, "work.txt", "pi edit")
	piCommitGit(t, dir, "add", ".")
	piCommitGit(t, dir, "commit", "-m", "feat: pi did work but forgot the trailer")
	treeBefore := piCommitGitOut(t, dir, "rev-parse", "HEAD^{tree}")
	countBefore := piCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensurePiRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedPiRefsAmended {
		t.Errorf("outcome = %v; want amended", outcome)
	}
	if got := piCommitGitOut(t, dir, "rev-parse", "HEAD^{tree}"); got != treeBefore {
		t.Errorf("amend changed the tree: %s -> %s", treeBefore, got)
	}
	if got := piCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count = %d; want %d (amend, not follow-up)", got, countBefore)
	}
	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("verify trailer: %v", err)
	}
	if !has {
		t.Errorf("amended commit missing 'Refs: %s' trailer; body=%q", beadID, piCommitHeadBody(t, dir))
	}
	if !strings.Contains(piCommitHeadBody(t, dir), "feat: pi did work but forgot the trailer") {
		t.Errorf("amend dropped the original message; body=%q", piCommitHeadBody(t, dir))
	}
}

// TestPiFallback_NoWork_NoCommitFabricated verifies the no_change path: Pi did
// nothing (HEAD unchanged, clean worktree) → ensurePiRefsTrailer reports
// piRefsNoChange and does NOT fabricate a commit.
func TestPiFallback_NoWork_NoCommitFabricated(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-idle")
	countBefore := piCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensurePiRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedPiRefsNoChange {
		t.Errorf("outcome = %v; want no_change", outcome)
	}
	if got := piCommitGitOut(t, dir, "rev-parse", "HEAD"); got != parentSHA {
		t.Errorf("HEAD advanced on idle path (commit fabricated): %s -> %s", parentSHA, got)
	}
	if got := piCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count changed on idle path: %d -> %d", countBefore, got)
	}
}

// TestPiFallback_EmptyBeadIDErrors verifies the guard: an empty beadID returns
// an error (never silently commits).
func TestPiFallback_EmptyBeadIDErrors(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	if _, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, ""); err == nil {
		t.Error("ensurePiRefsTrailer with empty beadID: want error, got nil")
	}
}

// TestPiFallback_RunnerRoutedAmend verifies that ensurePiRefsTrailer routes all
// git operations through the runner when one is provided (PI-031 remote-safe
// path, PI-100 runner-routed coverage). Uses RecordingRunner with nil CmdFunc
// so real git runs while every call is recorded.
func TestPiFallback_RunnerRoutedAmend(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-runner-amend")
	piCommitWriteFile(t, dir, "work.txt", "pi edit via runner")
	piCommitGit(t, dir, "add", ".")
	piCommitGit(t, dir, "commit", "-m", "feat: pi did work but forgot the trailer")

	rr := &tmux.RecordingRunner{}
	outcome, err := daemon.ExportedEnsurePiRefsTrailerViaRunner(context.Background(), rr, dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensurePiRefsTrailerViaRunner: %v", err)
	}
	if outcome != daemon.ExportedPiRefsAmended {
		t.Errorf("outcome = %v; want amended", outcome)
	}
	if len(rr.Calls) == 0 {
		t.Error("runner recorded zero calls — git commands must route through runner (PI-031)")
	}
	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("verify trailer: %v", err)
	}
	if !has {
		t.Errorf("amended commit missing 'Refs: %s' trailer", beadID)
	}
}

// TestPiFallback_CommitMessageContainsPiPrefix verifies the fallback commit
// message uses the "feat(pi):" prefix, not "feat(codex):". Tests distinguish
// the two harnesses' fallback commits at the message level.
func TestPiFallback_CommitMessageContainsPiPrefix(t *testing.T) {
	t.Parallel()

	dir, parentSHA := piCommitRepo(t)
	beadID := core.BeadID("hk-pi-msgcheck")
	piCommitWriteFile(t, dir, "seed.txt", "pi output")

	if _, err := daemon.ExportedEnsurePiRefsTrailer(context.Background(), dir, parentSHA, beadID); err != nil {
		t.Fatalf("ensurePiRefsTrailer: %v", err)
	}
	body := piCommitHeadBody(t, dir)
	if !strings.Contains(body, "feat(pi):") {
		t.Errorf("fallback commit message does not contain 'feat(pi):'; body=%q", body)
	}
	if strings.Contains(body, "feat(codex):") {
		t.Errorf("fallback commit message contains 'feat(codex):' (wrong harness prefix); body=%q", body)
	}
}
