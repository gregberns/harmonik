package daemon_test

// codexcommit_test.go — Refs:<bead> trailer guarantee tests (codex-harness
// C2/T9, hk-bpxci).
//
// Coverage (all three T9 parts):
//   - INSTRUCT: the codex seed prompt references the bead ID and instructs a
//     "Refs: <bead>" commit trailer.
//   - VERIFY: worktreeHEADHasRefsTrailer detects an exact "Refs: <bead>" line on
//     HEAD (and rejects near-misses like a "Refs: hk-foo.10" prefix collision).
//   - FALLBACK (ensureCodexRefsTrailer, real git in t.TempDir):
//       1. clean no-op when codex already committed WITH the trailer.
//       2. codex-edited-but-not-committed → fallback CREATES a commit carrying
//          the trailer.
//       3. codex-committed-WITHOUT-trailer → fallback AMENDS HEAD to add it
//          (same tree, single commit — the claude posture).
//       4. codex did nothing (clean worktree, HEAD unchanged) → no_change, no
//          commit fabricated.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// test git helpers (real git in t.TempDir)
// ─────────────────────────────────────────────────────────────────────────────

// codexCommitGit runs a git subcommand in dir, failing the test on error.
func codexCommitGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// codexCommitGitOut runs a git subcommand in dir and returns trimmed stdout.
func codexCommitGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// codexCommitRepo creates a temp git repo with one initial commit and returns
// the path and the initial HEAD SHA (the "parent" before a codex turn).
func codexCommitRepo(t *testing.T) (wtPath, parentSHA string) {
	t.Helper()
	dir := t.TempDir()
	codexCommitGit(t, dir, "init", "--initial-branch=main")
	codexCommitGit(t, dir, "config", "user.email", "test@test.com")
	codexCommitGit(t, dir, "config", "user.name", "Test")
	codexCommitGit(t, dir, "config", "commit.gpgsign", "false")
	codexCommitWriteFile(t, dir, "seed.txt", "initial")
	codexCommitGit(t, dir, "add", ".")
	codexCommitGit(t, dir, "commit", "-m", "init")
	return dir, codexCommitGitOut(t, dir, "rev-parse", "HEAD")
}

// codexCommitWriteFile writes name=content under dir.
func codexCommitWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	//nolint:gosec // test fixture file
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// codexCommitHeadBody returns the HEAD commit message body.
func codexCommitHeadBody(t *testing.T, dir string) string {
	t.Helper()
	return codexCommitGitOut(t, dir, "log", "-1", "--format=%B", "HEAD")
}

// codexCommitCount returns the number of commits reachable from HEAD.
func codexCommitCount(t *testing.T, dir string) int {
	t.Helper()
	out := codexCommitGitOut(t, dir, "rev-list", "--count", "HEAD")
	n := 0
	for _, c := range out {
		n = n*10 + int(c-'0')
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// INSTRUCT
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexInstruct_SeedPromptCarriesRefsTrailer verifies the codex seed prompt
// references the bead ID and instructs a "Refs: <bead>" commit trailer (the
// INSTRUCT half of the T9 guarantee).
func TestCodexInstruct_SeedPromptCarriesRefsTrailer(t *testing.T) {
	t.Parallel()

	beadID := core.BeadID("hk-bpxci-instruct")
	prompt := daemon.ExportedCodexSeedPromptInstruction(beadID)

	if !strings.Contains(prompt, string(beadID)) {
		t.Errorf("seed prompt does not reference bead ID %q: %q", beadID, prompt)
	}
	if !strings.Contains(prompt, "Refs: "+string(beadID)) {
		t.Errorf("seed prompt does not instruct the exact 'Refs: %s' trailer: %q", beadID, prompt)
	}
	if !strings.Contains(strings.ToLower(prompt), "commit") {
		t.Errorf("seed prompt does not mention committing: %q", prompt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// VERIFY
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexVerify_TrailerPresentDetected verifies worktreeHEADHasRefsTrailer
// returns true when HEAD carries an exact "Refs: <bead>" line.
func TestCodexVerify_TrailerPresentDetected(t *testing.T) {
	t.Parallel()

	dir, _ := codexCommitRepo(t)
	beadID := core.BeadID("hk-verify-present")
	codexCommitWriteFile(t, dir, "work.txt", "codex edit")
	codexCommitGit(t, dir, "add", ".")
	codexCommitGit(t, dir, "commit", "-m", "work\n\nRefs: "+string(beadID))

	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("worktreeHEADHasRefsTrailer: %v", err)
	}
	if !has {
		t.Error("trailer present on HEAD but worktreeHEADHasRefsTrailer = false")
	}
}

// TestCodexVerify_TrailerAbsentNotDetected verifies a commit WITHOUT the trailer
// is not falsely detected, and that a prefix-collision trailer
// ("Refs: hk-foo.10" must not satisfy "Refs: hk-foo.1") is rejected by the
// line-exact match (mirrors beadAlreadySubsumedInMain semantics).
func TestCodexVerify_TrailerAbsentNotDetected(t *testing.T) {
	t.Parallel()

	dir, _ := codexCommitRepo(t)
	codexCommitWriteFile(t, dir, "work.txt", "codex edit")
	codexCommitGit(t, dir, "add", ".")
	// Commit carries Refs: hk-foo.10 only.
	codexCommitGit(t, dir, "commit", "-m", "work\n\nRefs: hk-foo.10")

	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, core.BeadID("hk-foo.1"))
	if err != nil {
		t.Fatalf("worktreeHEADHasRefsTrailer: %v", err)
	}
	if has {
		t.Error("hk-foo.1 must NOT match a commit whose only trailer is 'Refs: hk-foo.10'")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// FALLBACK — ensureCodexRefsTrailer
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexFallback_AlreadyCommittedWithTrailer_NoOp verifies the clean no-op
// path: codex already committed WITH the trailer → ensureCodexRefsTrailer makes
// no new commit and reports codexRefsAlreadyPresent.
func TestCodexFallback_AlreadyCommittedWithTrailer_NoOp(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-noop")
	codexCommitWriteFile(t, dir, "work.txt", "codex edit")
	codexCommitGit(t, dir, "add", ".")
	codexCommitGit(t, dir, "commit", "-m", "feat: do the thing\n\nRefs: "+string(beadID))
	headBefore := codexCommitGitOut(t, dir, "rev-parse", "HEAD")
	countBefore := codexCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedCodexRefsAlreadyPresent {
		t.Errorf("outcome = %v; want already_present", outcome)
	}
	if got := codexCommitGitOut(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD changed on no-op path: %s -> %s", headBefore, got)
	}
	if got := codexCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count changed on no-op path: %d -> %d", countBefore, got)
	}
}

// TestCodexFallback_EditedButNotCommitted_CreatesCommit verifies the core T9
// fallback: codex edited files (dirty worktree) but produced NO commit → the
// fallback stages everything and CREATES a commit carrying the trailer.
func TestCodexFallback_EditedButNotCommitted_CreatesCommit(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-create")
	// codex edited a tracked file AND added an untracked one, but did not commit.
	codexCommitWriteFile(t, dir, "seed.txt", "modified by codex")
	codexCommitWriteFile(t, dir, "new.txt", "new file from codex")
	countBefore := codexCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedCodexRefsCommitted {
		t.Errorf("outcome = %v; want committed", outcome)
	}
	// A new commit must exist past parent, carrying the trailer.
	if got := codexCommitGitOut(t, dir, "rev-parse", "HEAD"); got == parentSHA {
		t.Error("HEAD did not advance past parent after fallback commit")
	}
	if got := codexCommitCount(t, dir); got != countBefore+1 {
		t.Errorf("commit count = %d; want %d (one fallback commit)", got, countBefore+1)
	}
	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("verify trailer: %v", err)
	}
	if !has {
		t.Errorf("fallback commit missing 'Refs: %s' trailer; body=%q", beadID, codexCommitHeadBody(t, dir))
	}
	// The codex edits (tracked + untracked) must be in the commit, not left dirty.
	if status := codexCommitGitOut(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("worktree still dirty after fallback commit: %q", status)
	}
}

// TestCodexFallback_CommittedWithoutTrailer_Amends verifies the amend posture:
// codex committed but WITHOUT the trailer → the fallback AMENDS HEAD to append
// the trailer, keeping the same tree and a single commit (no empty follow-up).
func TestCodexFallback_CommittedWithoutTrailer_Amends(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-amend")
	codexCommitWriteFile(t, dir, "work.txt", "codex edit")
	codexCommitGit(t, dir, "add", ".")
	codexCommitGit(t, dir, "commit", "-m", "feat: codex did work but forgot the trailer")
	treeBefore := codexCommitGitOut(t, dir, "rev-parse", "HEAD^{tree}")
	countBefore := codexCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedCodexRefsAmended {
		t.Errorf("outcome = %v; want amended", outcome)
	}
	// Same tree (amend preserves the codex edits exactly).
	if got := codexCommitGitOut(t, dir, "rev-parse", "HEAD^{tree}"); got != treeBefore {
		t.Errorf("amend changed the tree: %s -> %s", treeBefore, got)
	}
	// Single commit — amend, not a follow-up.
	if got := codexCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count = %d; want %d (amend, not follow-up)", got, countBefore)
	}
	// Trailer now present.
	has, err := daemon.ExportedWorktreeHEADHasRefsTrailer(context.Background(), dir, beadID)
	if err != nil {
		t.Fatalf("verify trailer: %v", err)
	}
	if !has {
		t.Errorf("amended commit missing 'Refs: %s' trailer; body=%q", beadID, codexCommitHeadBody(t, dir))
	}
	// Original message body preserved.
	if !strings.Contains(codexCommitHeadBody(t, dir), "feat: codex did work but forgot the trailer") {
		t.Errorf("amend dropped the original message; body=%q", codexCommitHeadBody(t, dir))
	}
}

// TestCodexFallback_NoWork_NoCommitFabricated verifies the no_change path: codex
// did nothing (HEAD unchanged, clean worktree) → ensureCodexRefsTrailer reports
// codexRefsNoChange and does NOT fabricate a commit, so the caller can route the
// run to the standard no_commit failure path.
func TestCodexFallback_NoWork_NoCommitFabricated(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-idle")
	countBefore := codexCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedCodexRefsNoChange {
		t.Errorf("outcome = %v; want no_change", outcome)
	}
	if got := codexCommitGitOut(t, dir, "rev-parse", "HEAD"); got != parentSHA {
		t.Errorf("HEAD advanced on idle path (commit fabricated): %s -> %s", parentSHA, got)
	}
	if got := codexCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count changed on idle path: %d -> %d", countBefore, got)
	}
}

// TestCodexFallback_EmptyBeadIDErrors verifies the guard: an empty bead ID is a
// programmer error and returns an error (never silently commits).
func TestCodexFallback_EmptyBeadIDErrors(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	if _, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, ""); err == nil {
		t.Error("ensureCodexRefsTrailer with empty beadID: want error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-jcrzn — the fallback must measure AGENT work, never daemon scaffolding
// ─────────────────────────────────────────────────────────────────────────────

// codexCommitWriteDaemonScaffolding writes the files the DAEMON itself drops
// into a run worktree. None of these are the agent's work product.
//
// In a project whose gitignore does not cover them — `harmonik init` used to
// scaffold an ENUMERATED .harmonik/.gitignore that omitted agent-task.md, and a
// gitignore inside .harmonik/ cannot un-untrack its own directory — a bare
// `git status --porcelain` reports `?? .harmonik/` even for a run in which the
// agent did nothing at all.
func codexCommitWriteDaemonScaffolding(t *testing.T, dir string) {
	t.Helper()
	for _, d := range []string{".harmonik", ".claude"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	codexCommitWriteFile(t, dir, filepath.Join(".harmonik", "agent-task.md"),
		"title: do the thing\n\n## Task Description\nimplement X\n")
	codexCommitWriteFile(t, dir, filepath.Join(".harmonik", "commit-gate.log"), "gate ran\n")
	codexCommitWriteFile(t, dir, filepath.Join(".claude", "settings.json"), "{}\n")
}

// TestCodexFallback_DaemonScaffoldingOnly_NoCommit is the hk-jcrzn regression
// guard, and it is the false-green that mattered most: an implementer that did
// NOTHING must not acquire a commit.
//
// Observed in production (lima, 3 sandboxed codex samples): exit 0 in 3-5s with
// commit_landed=false, yet the daemon logged "ensureCodexRefsTrailer: committed"
// and the run advanced through commit_gate to review. The synthesized commit
// carried a changed tree, real insertions and a valid Refs trailer — so the gate
// passed it LEGITIMATELY. By every check the gate makes, it was a real commit.
// The commit contained only the daemon's own scaffolding.
//
// ensureCodexRefsTrailer was never at fault: it explicitly refuses to fabricate
// on a clean worktree. The dirty check was being answered by the daemon's own
// files, so the "did the agent work?" question was reading the daemon's writes.
func TestCodexFallback_DaemonScaffoldingOnly_NoCommit(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-jcrzn-idle")

	// The daemon writes its scaffolding; the agent then does nothing at all.
	codexCommitWriteDaemonScaffolding(t, dir)

	headBefore := codexCommitGitOut(t, dir, "rev-parse", "HEAD")
	countBefore := codexCommitCount(t, dir)

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}

	// MUTATION ORACLE: neutralise the agentWorkPathspec exclusions in
	// codexcommit.go and this assertion fails — the worktree reads dirty on the
	// daemon's own files and the fallback fabricates a commit.
	if outcome != daemon.ExportedCodexRefsNoChange {
		t.Errorf("outcome = %v; want no_change — the agent did nothing, so daemon scaffolding "+
			"must not be read as uncommitted work (hk-jcrzn)", outcome)
	}
	if got := codexCommitGitOut(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD advanced on an idle run: %s -> %s — a commit was fabricated from daemon "+
			"scaffolding and would pass commit_gate as real work (hk-jcrzn)", headBefore, got)
	}
	if got := codexCommitCount(t, dir); got != countBefore {
		t.Errorf("commit count changed on an idle run: %d -> %d", countBefore, got)
	}
}

// TestCodexFallback_AgentWorkWithScaffolding_ExcludesDaemonPaths verifies the
// other half: when the agent DID work, the fallback still commits — but the
// commit contains only agent files. Daemon scaffolding present in the same
// worktree must never be swept in.
//
// The .claude/ exclusion is not cosmetic: that directory is only partially
// gitignored, so a blanket `git add -A` can stage credential-adjacent files and
// push them to origin (hk-igq3). commitResidualDelta has excluded both
// directories since GH #7 / hk-znou; this committer was its unhardened twin.
func TestCodexFallback_AgentWorkWithScaffolding_ExcludesDaemonPaths(t *testing.T) {
	t.Parallel()

	dir, parentSHA := codexCommitRepo(t)
	beadID := core.BeadID("hk-jcrzn-real")

	codexCommitWriteDaemonScaffolding(t, dir)
	// Real agent work, uncommitted — the genuine fallback case.
	codexCommitWriteFile(t, dir, "feature.go", "package main\n")

	outcome, err := daemon.ExportedEnsureCodexRefsTrailer(context.Background(), dir, parentSHA, beadID)
	if err != nil {
		t.Fatalf("ensureCodexRefsTrailer: %v", err)
	}
	if outcome != daemon.ExportedCodexRefsCommitted {
		t.Fatalf("outcome = %v; want committed — real agent work must still be committed", outcome)
	}

	committed := codexCommitGitOut(t, dir, "diff", "--name-only", parentSHA, "HEAD")
	if !strings.Contains(committed, "feature.go") {
		t.Errorf("agent work missing from the commit; files = %q", committed)
	}
	for _, forbidden := range []string{".harmonik/", ".claude/"} {
		if strings.Contains(committed, forbidden) {
			t.Errorf("daemon-owned path %q was committed as agent work; files = %q", forbidden, committed)
		}
	}
	// The worktree keeps its scaffolding — excluded from the commit, not deleted.
	if _, statErr := os.Stat(filepath.Join(dir, ".harmonik", "agent-task.md")); statErr != nil {
		t.Errorf("scaffolding was removed from the worktree: %v", statErr)
	}
}
