package daemon_test

// reviewloop_resume_reseed_hk8oy_test.go — scenario regression test for the
// iter-2 review-loop resume ALL-ENTERS-SWALLOWED scenario (hk-8oy).
//
// # Bug reproduced
//
// In review-loop mode, after the iteration-1 reviewer returns REQUEST_CHANGES,
// the daemon resumes with `claude --resume <id>`, PASTES the combined
// task+feedback prompt (pasteInjectImplementerResume), then sends the post-paste
// submit Enters via sendResumeSubmitEnter (hk-ip33d: 3 total, over ~800 ms).
//
// Observed in production on 2026-06-10, even with the hk-ip33d retry fix in
// place: the TUI was still absorbing the bracketed-paste content when ALL
// submit-retry Enters arrived, so EVERY one was swallowed by the absorbing input
// handler.  The combined task+feedback prompt sat typed-but-unsubmitted in the
// input bar; the resumed implementer stayed IDLE and committed nothing; and after
// the 30-min commitPollTimeout the run was killed with HEAD unchanged.  At the
// START of the next iteration the review loop detected HEAD unchanged (iter ≥ 2,
// prior verdict = REQUEST_CHANGES) and emitted review_fixup_stalled — as if the
// implementer simply refused to address the reviewer's feedback, rather than never
// having seen it (paste-Enter class, relates hk-jzpqo).
//
// # Recovery mechanism tested here (hk-76n5g safety net)
//
// pasteInjectQuitOnCommit fires a one-shot reseed-Enter after
// implementerReseedGrace (75 s in production, shortened here) when no commit has
// appeared.  This Enter submits the pending unsubmitted input and restores normal
// flow: the implementer reads the combined feedback brief, addresses it, commits,
// and the review loop proceeds to APPROVE — no review_fixup_stalled.
//
// # How this test faithfully reproduces the scenario deterministically
//
// rl8oySubstrate is a handler.Substrate that models the ALL-ENTERS-SWALLOWED
// absorption-window race:
//
//   - It implements pasteInjecter, enterSender, and quitSender.
//   - After the resume paste lands (WriteLastPane records it), the first
//     resumeSubmitRetries+1 Enter calls (from sendResumeSubmitEnter, all within
//     the absorption window) are DROPPED.
//   - The NEXT Enter call — the pasteInjectQuitOnCommit reseed-Enter that fires
//     after implementerReseedGrace — is DELIVERED: it writes the submit-ok
//     sentinel into the worktree, releasing the iter-2 handler to commit.
//
// The iter-2 handler script mirrors rlSubmitHandlerScript (hk-ip33d): it polls
// for the sentinel before committing, so without the reseed the sentinel never
// appears → no iter-2 commit → review_fixup_stalled.  With the reseed the
// sentinel appears → iter-2 commits → cycle reaches APPROVE.
//
// Key tunable overrides for fast execution:
//
//   - implementerReseedGrace: 200 ms (production: 75 s).
//   - resumeSubmitRetryDelay: 1 ms (ensures the 3 retries all fire and are dropped
//     before the reseed deadline, regardless of machine speed).
//   - commitPollInterval: 5 ms (fast HEAD detection after sentinel + commit).
//   - splashDismissDelay: 1 ms (suppress real wall-time waits in paste helpers).
//
// NOTE: NOT parallel — mutates package-level timing vars and contends on the
// process-global ~/.claude.json trust lock (see rlIsolateClaudeConfig).
//
// Helper prefix: rl8oy (bead hk-8oy;
// per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-8oy.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// rl8oySentinelName is the file the substrate writes (relative to the worktree)
// once the pasteInjectQuitOnCommit reseed-Enter has been "delivered" (i.e. after
// all of the sendResumeSubmitEnter retries were dropped).  The iter-2 handler
// script polls for it.
const rl8oySentinelName = ".harmonik/rl8oy_submit_ok"

// ─────────────────────────────────────────────────────────────────────────────
// rl8oySubstrate — nil-watcher substrate modelling ALL-ENTERS-SWALLOWED race
// ─────────────────────────────────────────────────────────────────────────────

// rl8oySubstrate implements handler.Substrate, pasteInjecter, enterSender, and
// quitSender.  It drives the nil-watcher (tmux) path so the daemon depends
// solely on the paste-inject submit path.  After the resume paste it drops ALL
// sendResumeSubmitEnter Enters (simulating the absorption window), delivering
// the sentinel only on the (resumeSubmitRetries+1)th+ call — the reseed-Enter
// from pasteInjectQuitOnCommit.
type rl8oySubstrate struct {
	store  *daemon.HookSessionStoreExported
	runID  core.RunID
	wtPath string

	spawnCount     atomic.Int64
	resumeLaunches atomic.Int64

	mu sync.Mutex
	// pasteSeen flips true once WriteLastPane delivered the resume paste.
	pasteSeen bool
	// submitEnterCount counts SendEnterToLastPane calls AFTER the resume paste.
	submitEnterCount int
	// droppedCount is the number of post-paste Enters to drop before delivering.
	// Set to resumeSubmitRetries+1 to model all sendResumeSubmitEnter calls being
	// swallowed; the (droppedCount+1)th Enter is the reseed and is delivered.
	droppedCount int
	// sessionIDs records, in launch order, the claude session id from each spawn.
	sessionIDs []string
}

func (s *rl8oySubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("rl8oySubstrate: SubstrateSpawn.Argv is empty")
	}

	isResume := false
	claudeSessionID := ""
	for i, a := range in.Argv {
		if a == "--resume" && i+1 < len(in.Argv) {
			isResume = true
			claudeSessionID = in.Argv[i+1]
		}
		if a == "--session-id" && i+1 < len(in.Argv) {
			claudeSessionID = in.Argv[i+1]
		}
	}
	s.mu.Lock()
	s.sessionIDs = append(s.sessionIDs, claudeSessionID)
	s.mu.Unlock()
	if isResume {
		s.resumeLaunches.Add(1)
	}

	//nolint:gosec // G204: Argv comes from test-internal HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rl8oySubstrate: Start: %w", err)
	}

	// Only fresh (--session-id) launches get the relay agent_ready; --resume
	// launches use the resumeReadyFallbackGrace inside the review loop.
	if !isResume && claudeSessionID != "" {
		go func(csid string) {
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				return
			}
			daemon.ExportedHookDispatch(s.store, daemon.HookRelayEnvelopeExported{
				Type:            "agent_ready",
				RunID:           s.runID.String(),
				ClaudeSessionID: csid,
			})
		}(claudeSessionID)
	}

	return &rl8oyExecSession{cmd: cmd}, nil
}

// WriteLastPane records the resume paste and arms the submit-Enter drop logic.
// Implements the daemon's (unexported) pasteInjecter interface structurally.
func (s *rl8oySubstrate) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resumeLaunches.Load() >= 1 {
		s.pasteSeen = true
	}
	return nil
}

// SendEnterToLastPane models the ALL-ENTERS-SWALLOWED absorption-window race:
//
//   - Enter calls 1..droppedCount (from sendResumeSubmitEnter): DROPPED.
//   - Enter call droppedCount+1 (from pasteInjectQuitOnCommit reseed): DELIVERED,
//     writes the submit-ok sentinel that releases the iter-2 handler to commit.
//
// Implements the daemon's (unexported) enterSender interface structurally.
func (s *rl8oySubstrate) SendEnterToLastPane(_ context.Context) error {
	s.mu.Lock()
	if !s.pasteSeen {
		// Splash-dismiss Enter (before the paste) — irrelevant to the race.
		s.mu.Unlock()
		return nil
	}
	s.submitEnterCount++
	n := s.submitEnterCount
	drop := s.droppedCount
	s.mu.Unlock()

	if n <= drop {
		// Within the absorption window: drop this Enter (all retries swallowed).
		return nil
	}
	// The reseed-Enter: deliver by writing the sentinel.
	sentinel := filepath.Join(s.wtPath, rl8oySentinelName)
	//nolint:gosec // G306: test-only sentinel file; not production
	return os.WriteFile(sentinel, []byte("ok"), 0o644)
}

// SendQuitToLastPane is a no-op; the handler script exits on its own.
// Implements the daemon's (unexported) quitSender interface structurally.
func (s *rl8oySubstrate) SendQuitToLastPane(_ context.Context) error { return nil }

func (s *rl8oySubstrate) resumeCount() int { return int(s.resumeLaunches.Load()) }

func (s *rl8oySubstrate) snapshot() (sessionIDs []string, submitEnters int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, len(s.sessionIDs))
	copy(ids, s.sessionIDs)
	return ids, s.submitEnterCount
}

var _ handler.Substrate = (*rl8oySubstrate)(nil)

// rl8oyExecSession wraps exec.Cmd with nil Stdout() to force the nil-watcher
// (tmux) path in handler.Launch.
type rl8oyExecSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (s *rl8oyExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *rl8oyExecSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

func (s *rl8oyExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *rl8oyExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

func (s *rl8oyExecSession) Stdout() io.Reader { return nil }

var _ handler.SubstrateSession = (*rl8oyExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func rl8oyProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rl8oyProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rl8oyProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rl8oyProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("reseed scenario\n"), 0o644); err != nil {
		t.Fatalf("rl8oyProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

func rl8oyWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rl8oyWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = string(out)
	for len(parentSHA) > 0 && parentSHA[len(parentSHA)-1] == '\n' {
		parentSHA = parentSHA[:len(parentSHA)-1]
	}

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("rl8oyWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rl8oyWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

func rl8oyRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rl8oyRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rl8oyHandlerScript writes a /bin/sh handler driving the 4-phase
// REQUEST_CHANGES → APPROVE cycle.  The iter-2 implementer-resume BLOCKS
// (polls) for the submit-ok sentinel before committing — modelling a real
// resumed claude that only acts once its prompt is SUBMITTED.
//
// Without the reseed-Enter (all sendResumeSubmitEnter Enters swallowed): the
// sentinel never appears → iter-2 exits WITHOUT committing → HEAD unchanged →
// review_fixup_stalled.
//
// With the reseed-Enter (pasteInjectQuitOnCommit safety net): the sentinel
// appears → iter-2 commits → cycle proceeds to APPROVE.
func rl8oyHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	sentinel := filepath.Join(wtPath, rl8oySentinelName)
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
SENTINEL='` + sentinel + `'
CNT_FILE="$WTP/.harmonik/rl8oy_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter1 implementer-initial: commit a file.
    printf 'v1' > "$WS/rl8oy_impl_1.txt"
    git -C "$WS" add rl8oy_impl_1.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "rl8oy iter1 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # iter1 reviewer: REQUEST_CHANGES (forces iter2 implementer-resume).
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["test-flag"],"notes":"please address the test flag"}' \
      > "$WS/.harmonik/review.json"
    ;;
  3)
    # iter2 implementer-resume: BLOCKS until the submit-ok sentinel appears.
    # The sentinel is written by the reseed-Enter from pasteInjectQuitOnCommit
    # (after implementerReseedGrace elapses with no commit), NOT by the
    # sendResumeSubmitEnter retries (all dropped in the substrate).
    i=0
    while [ ! -f "$SENTINEL" ]; do
      i=$((i + 1))
      if [ "$i" -gt 200 ]; then
        # Reseed never fired (pre-fix path): exit WITHOUT committing → HEAD
        # unchanged → review_fixup_stalled on the next iteration.
        exit 0
      fi
      sleep 0.05
    done
    printf 'v2' > "$WS/rl8oy_impl_2.txt"
    git -C "$WS" add rl8oy_impl_2.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "rl8oy iter2 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter2 reviewer: APPROVE.
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"looks good"}' \
      > "$WS/.harmonik/review.json"
    ;;
esac
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "rl8oy_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rl8oyHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_ResumeReseedSavesIdleImplementer is the scenario
// regression test for hk-8oy (iter-2 resume ALL-ENTERS-SWALLOWED → reseed
// saves the run).
//
// It drives a full REQUEST_CHANGES → APPROVE cycle where EVERY sendResumeSubmitEnter
// Enter (all resumeSubmitRetries+1 = 3 total) is dropped by the substrate — modelling
// the hk-76n5g production incident where ALL Enters landed within the paste-absorption
// window.  The run is saved only by the pasteInjectQuitOnCommit reseed-Enter that fires
// after implementerReseedGrace, which the substrate honours (writes the sentinel).
//
// Asserts:
//
//	(a) [hk-8oy] the reseed-Enter fired: at least resumeSubmitRetries+2 post-paste
//	    Enters were sent (the first resumeSubmitRetries+1 dropped + at least one
//	    reseed delivered), proving the safety-net mechanism engaged.
//	(b) [hk-8oy] the run completed APPROVE — proving the iter-2 implementer
//	    eventually committed after the reseed-Enter submitted the pending prompt.
//	(c) [hk-8oy] review_fixup_stalled was NOT emitted — the reseed prevented the
//	    HEAD-unchanged mis-classification.
func TestScenario_ReviewLoop_ResumeReseedSavesIdleImplementer(t *testing.T) {
	// NOT parallel: mutates package-level timing vars + contends on the
	// process-global ~/.claude.json trust lock (see rlIsolateClaudeConfig).
	rlIsolateClaudeConfig(t)

	// ── Timing overrides ────────────────────────────────────────────────────
	//
	// Shrink the reseed grace so the reseed fires quickly in the test.
	// Keep resumeSubmitRetries at its default (≥1) so sendResumeSubmitEnter
	// sends all its retries — and all of them are dropped by the substrate.
	// Short splash-dismiss delay suppresses real wall-time waits in paste helpers.
	// Short poll interval speeds up HEAD detection after the commit lands.
	origReseed := *daemon.ExportedImplementerReseedGrace
	origRetryDelay := *daemon.ExportedResumeSubmitRetryDelay
	origRetries := *daemon.ExportedResumeSubmitRetries
	origSplash := *daemon.ExportedSplashDismissDelay
	origPoll := *daemon.ExportedCommitPollInterval
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedImplementerReseedGrace = 200 * time.Millisecond
	*daemon.ExportedResumeSubmitRetryDelay = 1 * time.Millisecond
	*daemon.ExportedSplashDismissDelay = 1 * time.Millisecond
	*daemon.ExportedCommitPollInterval = 5 * time.Millisecond
	// Prevent the post-commit /quit watchdog goroutine from interfering with
	// the test by setting an extremely long grace (well beyond the test timeout).
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	t.Cleanup(func() {
		*daemon.ExportedImplementerReseedGrace = origReseed
		*daemon.ExportedResumeSubmitRetryDelay = origRetryDelay
		*daemon.ExportedResumeSubmitRetries = origRetries
		*daemon.ExportedSplashDismissDelay = origSplash
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	})

	// The substrate drops the first (resumeSubmitRetries+1) post-paste Enters.
	// These correspond to the entire sendResumeSubmitEnter burst.  The next Enter
	// — from the pasteInjectQuitOnCommit reseed — is delivered.
	droppedCount := *daemon.ExportedResumeSubmitRetries + 1

	projectDir := rl8oyProjectDir(t)
	wtPath, parentSHA := rl8oyWorktree(t, projectDir)
	scriptPath := rl8oyHandlerScript(t, wtPath)

	store := daemon.ExportedNewHookSessionStore()
	runID := rl8oyRunID(t)
	sub := &rl8oySubstrate{
		store:        store,
		runID:        runID,
		wtPath:       wtPath,
		droppedCount: droppedCount,
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		Substrate:           sub,
		HookStore:           store,
		// 8s > 2s resume fallback grace so the fallback readies the resume launch.
		AgentReadyTimeout: 8 * time.Second,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("scenario-rl-resume-reseed-hk8oy"),
		wtPath, parentSHA,
	)

	sessionIDs, submitEnters := sub.snapshot()
	eventTypes := collector.eventTypes()
	t.Logf("result=%+v resumeCount=%d sessionIDs=%v submitEnters=%d events=%v",
		result, sub.resumeCount(), sessionIDs, submitEnters, eventTypes)

	// Sanity: the cycle must have launched an implementer-resume.
	if sub.resumeCount() < 1 {
		t.Fatalf("test did not reach an implementer-resume (--resume) launch; resumeCount=%d", sub.resumeCount())
	}

	// ── (a) hk-8oy guard: the reseed-Enter fired ──────────────────────────
	//
	// droppedCount Enters were swallowed; at least one more (the reseed) must
	// have been delivered.  submitEnters > droppedCount proves the reseed fired.
	if submitEnters <= droppedCount {
		t.Errorf("hk-8oy REGRESSION: only %d post-paste Enter(s) sent, but %d are dropped by the substrate; "+
			"the pasteInjectQuitOnCommit reseed-Enter did not fire, so the pending input was never submitted",
			submitEnters, droppedCount)
	}

	// ── (b) hk-8oy guard: the run completed APPROVE ───────────────────────
	//
	// The iter-2 implementer must have committed (sentinel appeared after the
	// reseed) and the iter-2 reviewer must have APPROVEd.
	if !result.Success {
		t.Errorf("hk-8oy REGRESSION: expected success=true (RC→iter2→APPROVE via reseed); "+
			"got success=false, completion_reason=%q, summary=%q",
			result.CompletionReason, result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("hk-8oy REGRESSION: completion_reason=%q; want %q (summary=%q)",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved, result.Summary)
	}

	// ── (c) hk-8oy guard: review_fixup_stalled must NOT have fired ────────
	//
	// The reseed ensures the iter-2 implementer commits; HEAD advances; no
	// HEAD-unchanged mis-classification should occur.
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("hk-8oy REGRESSION: review_fixup_stalled emitted — iter-2 HEAD unchanged, "+
				"meaning the reseed-Enter did not submit the pending prompt; events=%v summary=%q",
				eventTypes, result.Summary)
			break
		}
	}

	// Sequence sanity: the full RC→reseed→iter2→APPROVE chain must be present.
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeImplementerResumed))
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),   // iter1
		string(core.EventTypeReviewerVerdict),    // iter1 RC
		string(core.EventTypeImplementerResumed), // iter2 dispatch (after reseed)
		string(core.EventTypeReviewerLaunched),   // iter2
		string(core.EventTypeReviewerVerdict),    // iter2 APPROVE
		string(core.EventTypeReviewLoopCycleComplete),
	})
}
