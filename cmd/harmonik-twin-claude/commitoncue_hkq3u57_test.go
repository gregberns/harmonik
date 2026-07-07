package main

// Tests for hk-q3u57: ST3 — verify/extend commit_on_cue for the deterministic
// two-twin same-file race.
//
// The two-twin same-file race is the scenario where two twins each commit the
// same file from the same base commit, then one "wins" the merge race (its
// branch fast-forwards to main), leaving the other as the non_ff_merge loser
// (its branch cannot be fast-forwarded and the daemon emits a rebase_conflict /
// non_ff_merge rejection).
//
// commit_on_cue now accepts an optional sentinel_name payload key that pins the
// sentinel file basename. Setting the same value in two commits to separate
// worktrees sharing the same repo creates two diverging commits that both touch
// the same file — the exact condition that triggers a rebase conflict when the
// daemon tries to merge the loser's branch.
//
// Helper prefix: twinCocFixture (re-used from commitoncue_hk8ys88_test.go;
// this file adds tests for the hk-q3u57 sentinel_name extension).
//
// Cite: hk-q3u57; docs/twin-parity-audit-2026-05-14.md §4 item 3 (hk-8ys88).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Unit: sentinel_name payload override
// ────────────────────────────────────────────────────────────────────────────

// TestRunCommitOnCue_SentinelNamePayload verifies that when the commit_on_cue
// step carries a sentinel_name payload key, runScript uses that basename for
// the sentinel file (not the default .harmonik-twin-commit-<unix-ns> pattern).
//
// This is the end-to-end path: YAML payload → runScript → cfg.sentinelName →
// runCommitOnCue → file on disk.
func TestRunCommitOnCue_SentinelNamePayload(t *testing.T) {
	dir := twinCocFixtureWorktree(t)
	e, buf := twinCocFixtureEmitter(t)
	ctx := context.Background()

	const customName = "my-custom-sentinel.txt"
	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type:    commitOnCueStep,
				Payload: map[string]any{"sentinel_name": customName},
			},
		},
	}

	if err := runScript(ctx, e, sf, scriptRunConfig{worktreePath: dir}); err != nil {
		t.Fatalf("runScript: %v", err)
	}

	msgs := twinCocFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got, _ := msgs[0]["type"].(string); got != "twin_committed" {
		t.Errorf("type = %q, want twin_committed", got)
	}
	if code, ok := msgs[0]["exit_code"].(float64); !ok || int(code) != 0 {
		t.Errorf("exit_code = %v, want 0", msgs[0]["exit_code"])
	}

	// Custom sentinel file must exist in the worktree.
	if _, err := os.Stat(filepath.Join(dir, customName)); err != nil {
		t.Errorf("sentinel file %q not found in worktree: %v", customName, err)
	}

	// No default-pattern file should exist alongside it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".harmonik-twin-commit-") {
			t.Errorf("found default-pattern sentinel file %q; expected only custom %q",
				ent.Name(), customName)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Two-twin same-file race
// ────────────────────────────────────────────────────────────────────────────

// TestRunCommitOnCue_TwoTwinSameFileRace is the proof-of-expressibility test
// for the hk-q3u57 scenario. It confirms that commit_on_cue, with the
// sentinel_name override, can express two twins deterministically committing
// the same file so the merge topology makes exactly one the non_ff_merge loser.
//
// Setup:
//  1. A shared git repo (mainDir) on branch "main" with a baseline commit.
//  2. Two linked git worktrees — twin-a and twin-b — both branching from the
//     same main HEAD (M0). Each worktree is on its own task branch.
//  3. Twin A calls runCommitOnCue with sentinel_name="shared-sentinel.txt".
//  4. Twin B calls runCommitOnCue with the same sentinel_name (different content
//     because the timestamp differs), making twin-b's branch diverge from M0
//     with a conflicting change to the same file.
//  5. main fast-forwards to twin-a's HEAD (twin A wins the merge race).
//
// Assertions:
//   - Both twins emitted twin_committed with exit_code=0 and a non-empty SHA.
//   - main == twin-a's SHA (winner landed on main).
//   - twin-a's SHA is NOT an ancestor of twin-b's SHA in the diverged topology,
//     confirming twin-b cannot be fast-forward merged — it is the loser.
//   - Both worktrees contain the shared sentinel file on disk.
//
// The git merge-base --is-ancestor check mirrors the daemon's own fast-forward
// check (workloop.go: `git merge-base --is-ancestor <mainTip> <runTip>`) that
// emits non_ff_merge / rebase_conflict when the check fails.
func TestRunCommitOnCue_TwoTwinSameFileRace(t *testing.T) {
	// 1. Shared git repo: main branch with baseline commit.
	mainDir := twinCocFixtureWorktree(t)

	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.local",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.local",
	)

	// runGit runs a git command in mainDir and returns trimmed stdout.
	runGit := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204: test helper with controlled args
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = mainDir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// 2. Create two linked worktrees (branches twin-a, twin-b) from M0.
	// git worktree add creates the directory; supply a path inside a temp dir.
	parentDir := t.TempDir()
	wtAPath := filepath.Join(parentDir, "wt-twin-a")
	wtBPath := filepath.Join(parentDir, "wt-twin-b")
	runGit("worktree", "add", "-b", "twin-a", wtAPath)
	runGit("worktree", "add", "-b", "twin-b", wtBPath)

	const sharedFile = "shared-sentinel.txt"
	ctx := context.Background()

	// 3. Twin A commits shared-sentinel.txt (deterministic winner).
	eA, bufA := twinCocFixtureEmitter(t)
	cfgA := scriptRunConfig{worktreePath: wtAPath, sentinelName: sharedFile}
	if err := runCommitOnCue(ctx, eA, cfgA); err != nil {
		t.Fatalf("runCommitOnCue twin-a: %v", err)
	}
	msgsA := twinCocFixtureDecodeAll(t, bufA)
	if len(msgsA) != 1 {
		t.Fatalf("twin-a: expected 1 message, got %d", len(msgsA))
	}
	if got, _ := msgsA[0]["type"].(string); got != "twin_committed" {
		t.Errorf("twin-a: type = %q, want twin_committed", got)
	}
	if code, ok := msgsA[0]["exit_code"].(float64); !ok || int(code) != 0 {
		t.Errorf("twin-a: exit_code = %v, want 0", msgsA[0]["exit_code"])
	}
	shaA, _ := msgsA[0]["commit_sha"].(string)
	if shaA == "" {
		t.Fatal("twin-a: commit_sha is empty, want non-empty SHA")
	}

	// 4. Twin B commits the same file (deterministic loser).
	// Sleep 1ms to guarantee a different nanosecond timestamp → different file
	// content, which is what creates the rebase conflict on the daemon side.
	time.Sleep(time.Millisecond)
	eB, bufB := twinCocFixtureEmitter(t)
	cfgB := scriptRunConfig{worktreePath: wtBPath, sentinelName: sharedFile}
	if err := runCommitOnCue(ctx, eB, cfgB); err != nil {
		t.Fatalf("runCommitOnCue twin-b: %v", err)
	}
	msgsB := twinCocFixtureDecodeAll(t, bufB)
	if len(msgsB) != 1 {
		t.Fatalf("twin-b: expected 1 message, got %d", len(msgsB))
	}
	if got, _ := msgsB[0]["type"].(string); got != "twin_committed" {
		t.Errorf("twin-b: type = %q, want twin_committed", got)
	}
	if code, ok := msgsB[0]["exit_code"].(float64); !ok || int(code) != 0 {
		t.Errorf("twin-b: exit_code = %v, want 0", msgsB[0]["exit_code"])
	}
	shaB, _ := msgsB[0]["commit_sha"].(string)
	if shaB == "" {
		t.Fatal("twin-b: commit_sha is empty, want non-empty SHA")
	}

	// shaA and shaB must be distinct commits.
	if shaA == shaB {
		t.Errorf("twin-a and twin-b produced the same commit SHA %q; expected diverged commits", shaA)
	}

	// 5. Twin A wins the merge race: fast-forward main to twin-a's tip.
	runGit("update-ref", "refs/heads/main", shaA)
	mainHEAD := runGit("rev-parse", "refs/heads/main")
	if mainHEAD != shaA {
		t.Errorf("main HEAD = %q, want twin-a SHA %q", mainHEAD, shaA)
	}

	// 6. Twin B is the non_ff loser: main (shaA) must NOT be an ancestor of
	// twin-b's commit (shaB). If main were an ancestor of shaB, twin-b's branch
	// could be fast-forward merged — but they diverged at M0 so shaA cannot be
	// an ancestor of shaB.
	//
	// git merge-base --is-ancestor A B exits 0 iff A is ancestor of B.
	// We expect exit non-zero (main is NOT ancestor of twin-b).
	//
	//nolint:gosec // G204: constant git args; test only
	isAncCmd := exec.CommandContext(t.Context(), "git", "merge-base", "--is-ancestor", shaA, shaB)
	isAncCmd.Dir = mainDir
	if err := isAncCmd.Run(); err == nil {
		t.Error("expected twin-a's SHA to NOT be an ancestor of twin-b's SHA " +
			"(twin-b should be the non_ff loser), but it is")
	}

	// 7. Both worktrees must have the shared sentinel file on disk.
	for _, wt := range []struct{ name, path string }{
		{"twin-a", wtAPath},
		{"twin-b", wtBPath},
	} {
		if _, err := os.Stat(filepath.Join(wt.path, sharedFile)); err != nil {
			t.Errorf("%s: sentinel file %q not found in worktree: %v", wt.name, sharedFile, err)
		}
	}

	// 8. Verify the sentinel file content differs between the two commits (each
	// twin wrote a unique nanosecond timestamp as the file body). This confirms
	// that the rebase the daemon would attempt would encounter a real content
	// conflict, not a no-op.
	contentA, errA := os.ReadFile(filepath.Join(wtAPath, sharedFile))
	contentB, errB := os.ReadFile(filepath.Join(wtBPath, sharedFile))
	if errA != nil || errB != nil {
		t.Fatalf("ReadFile sentinel: twin-a err=%v twin-b err=%v", errA, errB)
	}
	if string(contentA) == string(contentB) {
		t.Errorf("sentinel file content is identical in both worktrees; "+
			"expected different timestamps to create a rebase conflict: %q", string(contentA))
	}
}
