package daemon_test

// mergetomain_hk1u4wp_test.go — regression test for the local FF-check retry
// introduced by hk-1u4wp (commit 0866a2f0).
//
// Before the fix, mergeRunBranchToMain's local fast-forward check (`git
// merge-base --is-ancestor mainTip runTip`) returned non_ff_merge TERMINALLY
// on first failure — with no rebase-retry, unlike the push-time non-FF path
// (hk-svieq, mergetomain_hksvieq_test.go) which retries up to
// maxPushAttempts. When targetBranch advances between the step-2 rebase and
// the FF-check (a concurrent merge landing on the same target), the run
// failed and its bead had to reopen and re-run from scratch.
//
// Reproducing the exact race deterministically: mainTip is re-resolved via a
// live `git rev-parse` immediately after the step-2 `git rebase` call
// returns, so the only way the FF-check can observe a stale-vs-live mismatch
// is if targetBranch advances DURING that `git rebase` subprocess's own
// execution — after it resolves its upstream commit but before our code's
// post-rebase rev-parse. This test hits that window deterministically (no
// sleep/timing dependence) via a one-shot `pre-rebase` git hook: the hook
// fires exactly once, on the first real rebase, and immediately advances
// refs/heads/main to a new sibling commit — simulating a second run's merge
// landing on target at the worst possible instant. `git rebase` has already
// resolved its own upstream by the time the hook runs, so the rebase itself
// still completes onto the PRE-hook tip; only the subsequent rev-parse in
// mergeRunBranchToMain observes the advanced (sibling) tip, producing a
// genuine non-FF on the first FF-check attempt. The hook disarms itself via a
// marker file so the retry's own rebase (attempt 2) is clean and converges.
//
// Assertions (mirrors mergetomain_hksvieq_test.go's non-FF push-retry test):
//
//	(A) CloseBead called exactly once (bead succeeds despite the first
//	    FF-check failure).
//	(B) ReopenBead never called.
//	(C) refs/heads/main advances past mainSHABefore.
//	(D) The final main tree contains BOTH the agent's work AND the
//	    concurrently-landed sibling commit — proving the retry incorporated
//	    the concurrent advance rather than losing it.
//	(E) The final push reached the bare remote.
//	(F) run_completed{success:true} is present in the event stream.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 3 (FF-check),
// mirroring the EM-052 push-retry mechanics (hk-svieq).
// Bead: hk-1u4wp.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// mergeToMainFixtureHookFiredMarker is the marker file the installed
// pre-rebase hook touches the first (and only) time it runs, so the test can
// assert the hook actually fired — distinguishing "the race was reproduced
// and the retry logic recovered" from "the hook silently never ran" (e.g. a
// future --no-verify on the production rebase call, or a global
// core.hooksPath override), which would otherwise surface as a confusing
// failure deep in the main-tree-contents assertions instead of at the source.
const mergeToMainFixtureHookFiredMarker = "hk-1u4wp-test-fired"

// mergeToMainFixtureInstallConcurrentAdvanceHook installs a one-shot
// pre-rebase hook in repoRoot that, the FIRST time any `git rebase` runs
// (against the shared .git/hooks, which linked worktrees inherit from the
// common dir), advances refs/heads/main to a new commit sibling to whatever
// main currently is — simulating a concurrent merge landing on target
// between this run's step-2 rebase and its FF-check (hk-1u4wp). The hook
// disarms itself via a marker file in the git common dir so it fires exactly
// once across the whole test (including any retry-path rebase).
//
// The advance logic runs in a subshell and always exits 0 regardless of its
// outcome: a pre-rebase hook that exits non-zero VETOES the rebase entirely,
// which would silently redirect the test down the unrelated rebase_conflict
// path instead of the non-FF-retry path under test. A failure inside the
// subshell only means the simulated concurrent advance didn't happen — the
// test's own hook-fired / main-tree assertions catch that distinctly, rather
// than the whole rebase aborting opaquely.
func mergeToMainFixtureInstallConcurrentAdvanceHook(t *testing.T, repoRoot string) {
	t.Helper()
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	//nolint:gosec // G301: 0755 matches existing .git/hooks conventions
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureInstallConcurrentAdvanceHook: mkdir hooks: %v", err)
	}
	script := `#!/bin/sh
COMMON_DIR="$(git rev-parse --git-common-dir)"
MARKER="$COMMON_DIR/` + mergeToMainFixtureHookFiredMarker + `"
if [ -f "$MARKER" ]; then
  exit 0
fi
touch "$MARKER"
(
  set -e
  TMPFILE="$(mktemp)"
  echo "sibling-advance" > "$TMPFILE"
  BLOB=$(git hash-object -w "$TMPFILE")
  rm -f "$TMPFILE"
  # git mktree auto-sorts tree entries (verified against the git version this
  # test was written against); appending one entry to a live ls-tree is safe.
  NEWTREE=$( (git ls-tree main; printf '100644 blob %s\tsibling.txt\n' "$BLOB") | git mktree)
  NEWCOMMIT=$(echo "sibling: concurrent advance (hk-1u4wp test)" | git commit-tree "$NEWTREE" -p main)
  git update-ref refs/heads/main "$NEWCOMMIT"
) || echo "hk-1u4wp test hook: concurrent-advance simulation failed" >&2
exit 0
`
	hookPath := filepath.Join(hooksDir, "pre-rebase")
	//nolint:gosec // G306: hooks must be executable
	if err := os.WriteFile(hookPath, []byte(script), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureInstallConcurrentAdvanceHook: WriteFile: %v", err)
	}
}

// TestMergeToMain_NonFFCheckRetry is the hk-1u4wp regression: a concurrent
// advance of targetBranch landing between the step-2 rebase and the FF-check
// must be retried (re-resolve, rebase, recheck) rather than terminally
// failing the bead on the first non-FF observation.
func TestMergeToMain_NonFFCheckRetry(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-nonff-checkretry-bead-1u4wp")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Bare remote so the eventual push (after a successful retry) succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	addRemoteCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addRemoteCmd.Dir = projectDir
	if out, err := addRemoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (prime): %v\n%s", err, out)
	}

	// Arm the one-shot concurrent-advance hook BEFORE any rebase happens.
	mergeToMainFixtureInstallConcurrentAdvanceHook(t, projectDir)

	// Custom factory: commit agent work onto the run-branch (forked from the
	// CURRENT main tip), then advance main with a non-conflicting commit so
	// the step-2 rebase in mergeRunBranchToMain is a REAL rebase (a no-op
	// rebase never invokes pre-rebase — git short-circuits "already up to
	// date"). The hook then fires during that real rebase and advances main
	// a SECOND time, producing the stale-FF-check window under test.
	factory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := mergeToMainCommittingFactory(t)(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		mergeToMainFixtureAdvanceMain(t, projectDir)
		return wtPath, cleanup, nil
	}

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  factory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Diagnostic: the concurrent-advance hook actually fired. If it didn't
	// (e.g. a future --no-verify on the production rebase call, or a global
	// core.hooksPath override), the assertions below would still fail, but
	// with a confusing "main tree missing sibling.txt" message rather than
	// pointing at the real cause — check this first so a regression here is
	// immediately legible. projectDir IS the common .git dir's parent (it is
	// the primary checkout, not a linked worktree), so the marker path is
	// resolved directly rather than by re-running `git rev-parse
	// --git-common-dir` from the test process (whose cwd is unrelated to
	// projectDir and would resolve a relative common-dir wrongly). ──────────
	markerPath := filepath.Join(projectDir, ".git", mergeToMainFixtureHookFiredMarker)
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Fatalf("concurrent-advance pre-rebase hook never fired (marker %s absent): %v — "+
			"the FF-check retry path was not exercised at all", markerPath, statErr)
	}

	// ── Assertion (A): CloseBead called exactly once (no reopen). ────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (FF-check retry should succeed)", got)
	}

	// ── Assertion (B): ReopenBead never called. ───────────────────────────────
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0; reason = %q", got, ledger.getReopenReason())
	}

	// ── Assertion (C): refs/heads/main advanced. ─────────────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main HEAD unchanged after FF-check retry: still %s", mainSHABefore)
	}

	// ── Assertion (D): final tree has BOTH the agent's work AND the
	// concurrently-landed sibling commit — the retry rebased onto the
	// concurrent advance rather than clobbering or losing it. ────────────────
	for _, f := range []string{"work.txt", "sibling.txt", "DIVERGE"} {
		showCmd := exec.CommandContext(t.Context(), "git", "show", "main:"+f)
		showCmd.Dir = projectDir
		if out, err := showCmd.CombinedOutput(); err != nil {
			t.Errorf("git show main:%s: %v\n%s (main should contain the concurrent advance + agent work)", f, err, out)
		}
	}

	// ── Assertion (E): remote's main matches the new local main. ─────────────
	remoteRevCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "main")
	remoteRevCmd.Dir = originDir
	remoteRevOut, remoteRevErr := remoteRevCmd.Output()
	if remoteRevErr != nil {
		t.Fatalf("git rev-parse main (origin): %v", remoteRevErr)
	}
	remoteSHA := strings.TrimRight(string(remoteRevOut), "\n")
	if remoteSHA != mainSHAAfter {
		t.Errorf("origin main = %s; want %s (local main after FF-check retry)", remoteSHA[:8], mainSHAAfter[:8])
	}

	// ── Assertion (F): run_completed{success:true} present. ──────────────────
	runCompletedEvs := mergeToMainFindEvents(collector, "run_completed")
	if len(runCompletedEvs) == 0 {
		t.Error("no run_completed events found")
	} else {
		var m map[string]interface{}
		if err := json.Unmarshal(runCompletedEvs[0].Payload, &m); err != nil {
			t.Fatalf("run_completed payload unmarshal: %v", err)
		}
		if success, _ := m["success"].(bool); !success {
			t.Errorf("run_completed success = false; want true (FF-check retry should succeed)")
		}
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-1u4wp FF-check retry OK: main %s → %s, remote %s, events: %v",
		mainSHABefore[:8], mainSHAAfter[:8], remoteSHA[:8], types)
}
