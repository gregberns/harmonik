//go:build scenario

package daemon_test

// scenario_reviewloop_verdict_absent_salvage_hknhmbk_test.go — //go:build scenario
// coverage for the review-loop verdict-absent salvage path (hk-nhmbk).
//
// # The transient being pinned
//
// Known transient (ref memory + reviewer-stall): the reviewer claude exits
// WITHOUT writing a parseable verdict (review.json absent) even though the
// implementer already committed real work on the run worktree. Today this
// surfaces as a verdict-absent failure (reviewloop.go ~L1201) that historically
// was salvaged by a MANUAL cherry-pick of the committed worktree commit. This
// scenario pins the daemon-side guarantee so the manual cherry-pick is not
// silently load-bearing: verdict-absent fails the run with needs-attention BUT
// does NOT strand or discard the implementer's commit — the committed work is
// still on the worktree HEAD, recoverable by the documented operator path.
//
// # Why a scenario test (and not just dot_reviewer_no_verdict_hkbqf1q_test.go)
//
// dot_reviewer_no_verdict_hkbqf1q_test.go (untagged, per-commit gate) covers the
// DOT-cascade verdict-absent RETRY path via ExportedDriveDotWorkflow. This file
// covers the standalone review-loop path (runReviewLoop / reviewloop.go), where
// verdict-absent has NO retry — it fails the run — and asserts the SALVAGE
// invariant the bead names: the committed worktree commit is not lost.
//
// It is //go:build scenario tagged (the per-bead commit gate SKIPS these), and
// uses the nil-stdout substrate so the reviewer subprocess actually LAUNCHES
// (firing reviewer_launched) but produces no verdict — the precise verdict-absent
// shape, exercised without a live Claude session.
//
// # Substrate
//
// vasFixtureNilStdoutSubstrate is a handler.Substrate whose SpawnWindow runs the
// real handler script as a subprocess but returns Stdout()==nil, so
// handler.launchViaSubstrate returns watcher=nil — the tmux-hosted path (same
// mechanism as nilwatcherFixtureNilStdoutSubstrate / em015FixtureNilStdoutSubstrate).
// This makes the reviewer phase actually launch (reviewer_launched fires) before
// the daemon reads the absent verdict.
//
// # What this asserts (verdict-absent → SALVAGE, commit preserved)
//
//  1. result.Success == false  (verdict-absent is a failure)
//  2. result.CompletionReason == "error"
//  3. result.NeedsAttention == true  (operator-visible recovery path)
//  4. reviewer_launched DID fire (the reviewer ran — this is verdict-ABSENT, not
//     no-commit/no-launch) and review_loop_cycle_complete is the terminal event.
//  5. SALVAGE CORE: the implementer's commit is PRESERVED — the worktree HEAD
//     still points past parentSHA at the impl commit, and the committed marker
//     file is still present in the tree. The verdict-absent failure did NOT
//     reset/revert/strand the committed work; it remains recoverable.
//
// # Helper prefix: vasFixture (bead hk-nhmbk; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/execution-model.md §4.3 EM-015e (no-progress / reviewer stall)
//   - specs/event-model.md §8.1a (reviewer_verdict / review_loop_cycle_complete)
//   - specs/scenario-harness.md §4 (nil-stdout substrate spawn-path coverage)
//
// Bead: hk-nhmbk. Refs: reviewloop.go (verdict-absent path).

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// vasFixtureNilStdoutSubstrate — handler.Substrate stub with nil Stdout
// ─────────────────────────────────────────────────────────────────────────────

// vasFixtureNilStdoutSubstrate runs the real Argv as a subprocess but wraps it in
// a session whose Stdout() returns nil, so handler.launchViaSubstrate returns
// watcher=nil — the tmux-hosted substrate path. The reviewer subprocess executes
// (firing reviewer_launched) and exits; the daemon then reads the verdict file
// it never wrote → verdict-absent.
type vasFixtureNilStdoutSubstrate struct{}

// SpawnWindow runs the real command from in.Argv and returns a session with
// Stdout()==nil. Completion is detected via sess.Wait() (nil-watcher branch).
func (s *vasFixtureNilStdoutSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("vasFixtureNilStdoutSubstrate: Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	// Do NOT set cmd.Stdout — subprocess stdout goes to /dev/null (like tmux).
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("vasFixtureNilStdoutSubstrate: Start: %w", err)
	}
	return &vasFixtureNilStdoutSession{cmd: cmd}, nil
}

// Compile-time assertion.
var _ handler.Substrate = (*vasFixtureNilStdoutSubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// vasFixtureNilStdoutSession — SubstrateSession with Stdout() == nil
// ─────────────────────────────────────────────────────────────────────────────

// vasFixtureNilStdoutSession is a handler.SubstrateSession backed by a real
// *exec.Cmd whose Stdout() returns nil, simulating a tmux-hosted session.
type vasFixtureNilStdoutSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// Kill terminates the subprocess.
func (s *vasFixtureNilStdoutSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the subprocess to exit; exit-code errors are suppressed so the
// nil-watcher branch in waitWithSocketGrace completes without error.
func (s *vasFixtureNilStdoutSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

// Outcome returns a zero-value Outcome (exit info not surfaced in this stub).
func (s *vasFixtureNilStdoutSession) Outcome() handler.Outcome {
	return handler.Outcome{}
}

// PID returns the subprocess PID.
func (s *vasFixtureNilStdoutSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — causes handler.launchViaSubstrate to return watcher=nil.
func (s *vasFixtureNilStdoutSession) Stdout() io.Reader { return nil }

// Compile-time assertion.
var _ handler.SubstrateSession = (*vasFixtureNilStdoutSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// vasFixtureHandlerScript — implementer commits; reviewer writes NO verdict
// ─────────────────────────────────────────────────────────────────────────────

// vasMarkerFile is the file the implementer commits — its presence on the
// worktree HEAD after the run is the salvage proof.
const vasMarkerFile = "vas_salvage_marker.txt"

// vasFixtureHandlerScript writes a /bin/sh handler keyed on an invocation counter:
//
//	Invocation 1 (implementer, iter 1): commit vasMarkerFile → HEAD advances
//	    past parentSHA. THIS is the work that must be salvaged.
//	Invocation 2 (reviewer, iter 1): exits 0 WITHOUT writing review.json →
//	    the daemon reads an absent verdict → verdict-absent failure.
//
// The reviewer's cwd ($PWD) is the daemon-created isolated reviewer worktree
// (revWtPath); writing nothing there is exactly the verdict-absent shape the
// daemon reads via ReadReviewVerdict(revWtPath).
func vasFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/vas_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ "$CNT" -eq 1 ]; then
  # Implementer iter 1: commit real work -> HEAD advances past parentSHA.
  # This commit is what the salvage invariant must preserve.
  printf 'vas salvage work' > "$WS/%s"
  git -C "$WS" add "%s" >/dev/null 2>&1
  git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "vas-nhmbk impl iter1 (must be salvaged)" --no-gpg-sign >/dev/null 2>&1
else
  # Reviewer iter 1: exit WITHOUT writing review.json -> verdict absent.
  # (No review.json in $PWD/.harmonik nor $WS/.harmonik.)
  :
fi
exit 0
`, wtpEsc, vasMarkerFile, vasMarkerFile)
	scriptPath := filepath.Join(t.TempDir(), "vas_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("vasFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// vasGitOut runs `git <args>` in dir and returns trimmed stdout, failing the test
// on error.
func vasGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("vasGitOut: git %v in %q: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// vasGitOK runs `git <args>` in dir and reports whether it exited zero (used for
// predicate commands like `merge-base --is-ancestor`).
func vasGitOK(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	//nolint:gosec // G204: git args are test-internal literals; not user input
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_ReviewLoop_VerdictAbsent_SalvagesCommit_hknhmbk
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_VerdictAbsent_SalvagesCommit_hknhmbk pins the
// daemon-side verdict-absent salvage guarantee (hk-nhmbk):
//
//	impl iter 1 commits real work → reviewer launches but writes NO verdict →
//	run fails (error / needs-attention) WITHOUT discarding the committed work.
//
// The salvage proof: the worktree HEAD still points at the impl commit (advanced
// past parentSHA) and the committed marker file is still present — the commit was
// never reset/reverted/stranded.
//
// Spec refs: specs/execution-model.md §4.3 EM-015e; reviewloop.go verdict-absent path.
// Bead: hk-nhmbk.
func TestScenario_ReviewLoop_VerdictAbsent_SalvagesCommit_hknhmbk(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := vasFixtureHandlerScript(t, wtPath)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     ledger,
		Bus:           collector,
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{scriptPath},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// nil-stdout substrate: the reviewer subprocess launches (reviewer_launched
		// fires) but watcher=nil — the tmux-hosted path. The reviewer writes no
		// verdict → verdict-absent.
		Substrate:           &vasFixtureNilStdoutSubstrate{},
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		// Empty sealed registry: ForAgent(claude-code) errors so waitAgentReady is
		// skipped (the shell fixture never delivers agent_ready via hook relay).
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	runID := rlFixtureRunID(t)
	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("vas-nhmbk-verdict-absent-001"),
		wtPath, parentSHA,
	)

	eventTypes := collector.eventTypes()
	t.Logf("verdict-absent-salvage: result=%+v events=%v", result, eventTypes)

	// ── Assertion 1: verdict-absent is a FAILURE ─────────────────────────────
	if result.Success {
		t.Errorf("hk-nhmbk: expected success=false (reviewer wrote no parseable verdict); summary=%q events=%v",
			result.Summary, eventTypes)
	}

	// ── Assertion 2: completion_reason == "error" ────────────────────────────
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("hk-nhmbk: completion_reason=%q; want %q. summary=%q",
			result.CompletionReason, core.ReviewLoopCompletionReasonError, result.Summary)
	}

	// ── Assertion 3: needs-attention (operator-visible recovery path) ─────────
	if !result.NeedsAttention {
		t.Errorf("hk-nhmbk: NeedsAttention=false; want true (verdict-absent must surface for recovery). summary=%q",
			result.Summary)
	}

	// ── Assertion 4a: the reviewer DID launch ────────────────────────────────
	// This distinguishes verdict-ABSENT (reviewer ran, produced nothing) from the
	// no-commit/no-launch path (hk-9c1v4). The reviewer must have launched.
	reviewerLaunched := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			reviewerLaunched = true
			break
		}
	}
	if !reviewerLaunched {
		t.Errorf("hk-nhmbk: reviewer_launched NOT emitted; verdict-absent requires the reviewer to have run. events=%v",
			eventTypes)
	}

	// ── Assertion 4b: no reviewer_verdict (none was ever written) ─────────────
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerVerdict) {
			t.Errorf("hk-nhmbk: reviewer_verdict emitted but the reviewer wrote no verdict; events=%v", eventTypes)
		}
	}

	// ── Assertion 4c: review_loop_cycle_complete is emitted (terminal) ────────
	foundCycleComplete := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			foundCycleComplete = true
			break
		}
	}
	if !foundCycleComplete {
		t.Errorf("hk-nhmbk: review_loop_cycle_complete not emitted; events=%v", eventTypes)
	}

	// ── Assertion 5 (SALVAGE CORE): the committed work is PRESERVED ───────────
	//
	// The verdict-absent failure must NOT reset/revert/strand the implementer's
	// commit. Prove it on the run worktree:
	//   (a) HEAD advanced past parentSHA (the impl commit is still HEAD), and
	//   (b) the committed marker file is still tracked at HEAD.
	headSHA := vasGitOut(t, wtPath, "rev-parse", "HEAD")
	if headSHA == parentSHA {
		t.Fatalf("hk-nhmbk SALVAGE FAIL: worktree HEAD == parentSHA (%s) — the implementer commit was lost/reset by the verdict-absent path",
			parentSHA)
	}

	// parentSHA must still be an ancestor of HEAD — the impl commit was added on
	// top of the worktree base, not orphaned by a detach/reset elsewhere.
	if !vasGitOK(t, wtPath, "merge-base", "--is-ancestor", parentSHA, "HEAD") {
		t.Errorf("hk-nhmbk SALVAGE FAIL: parentSHA %s is not an ancestor of HEAD %s — the worktree history was rewritten, not preserved",
			parentSHA, headSHA)
	}

	// The committed marker file must still be present in the working tree.
	markerPath := filepath.Join(wtPath, vasMarkerFile)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("hk-nhmbk SALVAGE FAIL: committed marker %q missing after verdict-absent: %v", markerPath, err)
	}

	// And it must be tracked at HEAD (committed, not just an untracked leftover).
	tracked := vasGitOut(t, wtPath, "ls-files", vasMarkerFile)
	if tracked != vasMarkerFile {
		t.Errorf("hk-nhmbk SALVAGE FAIL: %q not tracked at HEAD (ls-files=%q) — the committed work was stranded/discarded",
			vasMarkerFile, tracked)
	}

	// Strongest salvage proof: the marker's content is reachable from the HEAD
	// commit itself (git show HEAD:<file>), confirming the implementer's commit is
	// the worktree HEAD and was not discarded/reset by the verdict-absent path.
	committedContent := vasGitOut(t, wtPath, "show", "HEAD:"+vasMarkerFile)
	if committedContent != "vas salvage work" {
		t.Errorf("hk-nhmbk SALVAGE FAIL: HEAD:%s content=%q; want %q — the salvageable commit was replaced or lost",
			vasMarkerFile, committedContent, "vas salvage work")
	}

	t.Logf("hk-nhmbk PASS: verdict-absent failed the run (error/needs-attention) but the impl commit %s (file=%q, content reachable from HEAD) is PRESERVED on the worktree HEAD — recoverable, not stranded.",
		headSHA, vasMarkerFile)
}
