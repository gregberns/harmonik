package daemon_test

// reviewloop_resume_submit_hkip33d_test.go — scenario regression test for the
// iter-2 review-loop resume SUBMIT-RELIABILITY bug class (hk-ip33d), which also
// re-guards the iter-2 resume SESSION-ID-TARGETING bug class (hk-za5mz).
//
// # Bug reproduced (hk-ip33d — submit reliability)
//
// In review-loop mode, after the iteration-1 implementer commits and the
// reviewer returns REQUEST_CHANGES, the daemon resumes with
// `claude --resume <real-session-id>`, PASTES the combined task+feedback prompt
// into the freshly-resumed pane (pasteInjectImplementerResume), then sends a
// single post-paste Enter to SUBMIT it.  On a fresh `--resume` the REPL input
// handler is intermittently not yet ready to accept that Enter — the keypress is
// dropped, the prompt sits in the input bar unsubmitted, claude stays idle, and
// the run goes run_stale with no iteration-2 progress.  Confirmed in production:
// a manual `tmux send-keys -t <pane> Enter` submitted the prompt and iteration 2
// began immediately (intermittent; residual of hk-poy7k).
//
// # Bug re-guarded (hk-za5mz — resume targets the REAL session id)
//
// hk-za5mz: the iteration-2 `--resume` MUST target the real minted claude
// session id captured at iteration 1, NOT a freshly-synthesised id.  Resuming a
// synthetic id reattaches to a nonexistent / wrong session and reproduces the
// identical diff → no_progress_detected.
//
// # How this test faithfully reproduces BOTH classes deterministically
//
// rlSubmitSubstrate is a handler.Substrate whose sessions return Stdout() == nil
// (the production tmux path → nil watcher, so the daemon depends solely on the
// relay/fallback agent_ready and the paste-inject submit Enter).  Crucially, the
// substrate ITSELF implements the daemon's paste-inject interfaces
// (pasteInjecter.WriteLastPane, enterSender.SendEnterToLastPane,
// quitSender.SendQuitToLastPane), so pasteInjectOnLaunch exercises the REAL
// submit path (pasteInjectImplementerResume + sendResumeSubmitEnter) rather than
// being a no-op as in the other scenario substrates.
//
// The substrate models the fresh-`--resume` lost-Enter race precisely:
//   - It records the claude session id from every `--session-id` / `--resume`
//     launch (so the test can assert the resume targets the REAL iter-1 id).
//   - On the resume pane, the FIRST post-paste Enter is DROPPED (the not-yet-
//     ready REPL); the SECOND Enter (the bounded retry — the hk-ip33d fix) is
//     "delivered" and writes a submit-ok sentinel into the worktree.
//
// The iter-2 implementer handler script BLOCKS (polls) for that submit-ok
// sentinel before it commits.  Therefore:
//   - With the fix (resumeSubmitRetries ≥ 1): the retry Enter lands → sentinel
//     appears → iter-2 commits → diff advances → cycle proceeds to APPROVE.
//   - Without the fix (resumeSubmitRetries = 0, the pre-fix single-Enter
//     behaviour): only the dropped first Enter is ever sent → sentinel never
//     appears → iter-2 never commits → no_progress_detected / run failure.
//
// Helper prefix: rlSubmit (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-ip33d (submit reliability) + hk-za5mz (resume session-id targeting).

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

// rlSubmitSentinelName is the file the substrate writes (relative to the
// worktree) once the resume submit Enter has been "delivered" (i.e. after the
// dropped first Enter, on the retry).  The iter-2 handler script polls for it.
const rlSubmitSentinelName = ".harmonik/rlsubmit_submit_ok"

// ─────────────────────────────────────────────────────────────────────────────
// rlSubmitSubstrate — nil-watcher substrate that ALSO drives the real paste path
// ─────────────────────────────────────────────────────────────────────────────

// rlSubmitSubstrate forces the nil-watcher (tmux) path AND implements the
// daemon's paste-inject interfaces so the production submit path is exercised.
type rlSubmitSubstrate struct {
	store  *daemon.HookSessionStoreExported
	runID  core.RunID
	wtPath string

	spawnCount     atomic.Int64
	resumeLaunches atomic.Int64

	mu sync.Mutex
	// sessionIDByLaunch records, in launch order, the claude session id observed
	// on each spawn (from --session-id or --resume).  Used to assert the resume
	// targets the same real id captured at iteration 1 (hk-za5mz guard).
	sessionIDs []string
	// resumeBufferNames records the WriteLastPane buffer name(s) used on the
	// resume paste; the buffer name encodes the claude session id, so it is a
	// second, independent witness that the paste targets the real session.
	resumeBufferNames []string
	// resumeID is the claude session id seen on the (first) --resume launch.
	resumeID string
	// pasteSeen flips true once WriteLastPane has delivered the resume paste, so
	// SendEnterToLastPane knows the following Enter(s) are the SUBMIT (and the
	// first of them must be dropped to model the not-ready REPL race).
	pasteSeen bool
	// submitEnterCount counts SendEnterToLastPane calls AFTER the resume paste.
	submitEnterCount int
}

func (s *rlSubmitSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("rlSubmitSubstrate: SubstrateSpawn.Argv is empty")
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
	if isResume {
		s.resumeID = claudeSessionID
	}
	s.mu.Unlock()
	if isResume {
		s.resumeLaunches.Add(1)
	}

	//nolint:gosec // G204: Argv comes from test-internal HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rlSubmitSubstrate: Start: %w", err)
	}

	// Only fresh (`--session-id`) launches receive a relay agent_ready (the
	// SessionStart→agent_ready synthesis).  `--resume` launches get none — the
	// resume-phase fallback (resumeReadyFallbackGrace) provides ready there.
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

	return &rlSubmitExecSession{cmd: cmd}, nil
}

// WriteLastPane records the resume paste and arms the submit-Enter race.
// Implements the daemon's (unexported) pasteInjecter interface structurally.
func (s *rlSubmitSubstrate) WriteLastPane(_ context.Context, bufferName string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The resume paste is recognisable because a --resume launch has occurred.
	if s.resumeLaunches.Load() >= 1 {
		s.resumeBufferNames = append(s.resumeBufferNames, bufferName)
		s.pasteSeen = true
	}
	return nil
}

// SendEnterToLastPane models the fresh-`--resume` lost-Enter race: after the
// resume paste, the FIRST Enter is dropped (REPL not yet input-ready); the
// SECOND (the bounded retry — hk-ip33d fix) is delivered and writes the
// submit-ok sentinel that releases the iter-2 handler to commit.
// Implements the daemon's (unexported) enterSender interface structurally.
func (s *rlSubmitSubstrate) SendEnterToLastPane(_ context.Context) error {
	s.mu.Lock()
	if !s.pasteSeen {
		// Splash-dismiss Enter (before the paste) — irrelevant to the race.
		s.mu.Unlock()
		return nil
	}
	s.submitEnterCount++
	n := s.submitEnterCount
	s.mu.Unlock()

	if n <= 1 {
		// Drop the first submit Enter: the freshly-resumed REPL was not ready.
		return nil
	}
	// Retry Enter lands: deliver the submit by writing the sentinel.
	sentinel := filepath.Join(s.wtPath, rlSubmitSentinelName)
	//nolint:gosec // G306: test-only sentinel file; not production.
	return os.WriteFile(sentinel, []byte("ok"), 0o644)
}

// SendQuitToLastPane is a no-op for this substrate (commit detection drives the
// daemon's quit-on-commit path; the script exits on its own).
// Implements the daemon's (unexported) quitSender interface structurally.
func (s *rlSubmitSubstrate) SendQuitToLastPane(_ context.Context) error { return nil }

func (s *rlSubmitSubstrate) resumeCount() int { return int(s.resumeLaunches.Load()) }

func (s *rlSubmitSubstrate) snapshot() (sessionIDs []string, resumeID string, resumeBufs []string, submitEnters int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, len(s.sessionIDs))
	copy(ids, s.sessionIDs)
	bufs := make([]string, len(s.resumeBufferNames))
	copy(bufs, s.resumeBufferNames)
	return ids, s.resumeID, bufs, s.submitEnterCount
}

var _ handler.Substrate = (*rlSubmitSubstrate)(nil)

// rlSubmitExecSession wraps exec.Cmd with nil Stdout() — forces the nil-watcher
// (tmux) path in handler.Launch.
type rlSubmitExecSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (s *rlSubmitExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *rlSubmitExecSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

func (s *rlSubmitExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *rlSubmitExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

func (s *rlSubmitExecSession) Stdout() io.Reader { return nil }

var _ handler.SubstrateSession = (*rlSubmitExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func rlSubmitProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlSubmitProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlSubmitProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rlSubmitProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("resume-submit scenario\n"), 0o644); err != nil {
		t.Fatalf("rlSubmitProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

func rlSubmitWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlSubmitWorktree: git rev-parse HEAD: %v", err)
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
		t.Fatalf("rlSubmitWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlSubmitWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

func rlSubmitRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlSubmitRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlSubmitHandlerScript writes a /bin/sh handler driving the 4-phase
// REQUEST_CHANGES → APPROVE cycle.  The iter-2 implementer-resume step BLOCKS
// (polls) for the submit-ok sentinel before committing — modelling a real
// resumed claude that only acts once its prompt is actually SUBMITTED.  If the
// sentinel never appears (pre-fix: submit Enter dropped, never retried) the
// iter-2 step times out without committing → no_progress_detected.
func rlSubmitHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	sentinel := filepath.Join(wtPath, rlSubmitSentinelName)
	// Implementer phases write/commit in the implementer worktree ($WTP);
	// reviewer phases run in an isolated worktree exposed via
	// HARMONIK_WORKSPACE_PATH ($WS) and must write review.json there (mirrors
	// rlResumeReadyHandlerScript).  The submit sentinel is keyed to $WTP because
	// the substrate writes it relative to the implementer worktree.
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
SENTINEL='` + sentinel + `'
CNT_FILE="$WTP/.harmonik/rlsubmit_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter1 implementer-initial: commit a file.
    printf 'v1' > "$WS/rlsubmit_impl_1.txt"
    git -C "$WS" add rlsubmit_impl_1.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter1 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # iter1 reviewer: REQUEST_CHANGES (forces iter2 implementer-resume).
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["test-flag"],"notes":"please address the test flag"}' \
      > "$WS/.harmonik/review.json"
    ;;
  3)
    # iter2 implementer-resume: a resumed claude only does work once its pasted
    # prompt is SUBMITTED.  Block until the substrate signals the submit Enter
    # landed (the hk-ip33d retry).  Bounded wait: if it never lands, do NOT
    # commit — the daemon then sees an unchanged diff (no_progress_detected).
    i=0
    while [ ! -f "$SENTINEL" ]; do
      i=$((i + 1))
      if [ "$i" -gt 100 ]; then
        # Submit never delivered (pre-fix path): exit WITHOUT committing.
        exit 0
      fi
      sleep 0.1
    done
    printf 'v2' > "$WS/rlsubmit_impl_2.txt"
    git -C "$WS" add rlsubmit_impl_2.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter2 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter2 reviewer: APPROVE.
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"looks good now"}' \
      > "$WS/.harmonik/review.json"
    ;;
esac
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "rlsubmit_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlSubmitHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_ResumeSubmitReliable is the scenario regression test
// for hk-ip33d (iter-2 resume submit reliability), which simultaneously guards
// hk-za5mz (iter-2 resume targets the REAL session id).
//
// It drives a full REQUEST_CHANGES → APPROVE cycle through the production
// paste-inject submit path under a nil-watcher substrate that drops the first
// post-paste submit Enter (the freshly-resumed-REPL race) and only honours the
// bounded retry.  The iter-2 implementer commits ONLY after the submit lands.
//
// Asserts:
//
//	(a) [hk-za5mz] the iter-2 --resume targets the REAL session id captured at
//	    iteration 1 (same id on the --session-id and --resume launches; the
//	    resume paste buffer name encodes that same id).
//	(b) [hk-ip33d] the feedback prompt is actually SUBMITTED (a SECOND submit
//	    Enter — the retry — was sent after the dropped first) and iteration 2
//	    produces progress: the run completes APPROVE with no no_progress_detected
//	    and no agent_ready_timeout.
func TestScenario_ReviewLoop_ResumeSubmitReliable(t *testing.T) {
	t.Parallel()

	// Shrink the submit-retry delay so the retry fires quickly (the fix path).
	// resumeSubmitRetries stays at its default (≥1) so the retry actually runs;
	// this is what distinguishes the fix from the pre-fix single-Enter behaviour.
	origDelay := *daemon.ExportedResumeSubmitRetryDelay
	origRetries := *daemon.ExportedResumeSubmitRetries
	*daemon.ExportedResumeSubmitRetryDelay = 50 * time.Millisecond
	t.Cleanup(func() {
		*daemon.ExportedResumeSubmitRetryDelay = origDelay
		*daemon.ExportedResumeSubmitRetries = origRetries
	})
	if *daemon.ExportedResumeSubmitRetries < 1 {
		t.Fatalf("precondition: resumeSubmitRetries must be ≥1 for the fix to retry; got %d",
			*daemon.ExportedResumeSubmitRetries)
	}

	projectDir := rlSubmitProjectDir(t)
	wtPath, parentSHA := rlSubmitWorktree(t, projectDir)
	scriptPath := rlSubmitHandlerScript(t, wtPath)

	store := daemon.ExportedNewHookSessionStore()
	runID := rlSubmitRunID(t)
	sub := &rlSubmitSubstrate{store: store, runID: runID, wtPath: wtPath}

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
		core.BeadID("scenario-rl-resume-submit-hkip33d"),
		wtPath, parentSHA,
	)

	sessionIDs, resumeID, resumeBufs, submitEnters := sub.snapshot()
	t.Logf("result=%+v resumeCount=%d sessionIDs=%v resumeID=%q resumeBufs=%v submitEnters=%d events=%v",
		result, sub.resumeCount(), sessionIDs, resumeID, resumeBufs, submitEnters, collector.eventTypes())

	// Sanity: the cycle must have reached an implementer-resume launch.
	if sub.resumeCount() < 1 {
		t.Fatalf("test did not reach an implementer-resume (--resume) launch; resumeCount=%d", sub.resumeCount())
	}

	// ── (a) hk-za5mz guard: resume targets the REAL iter-1 session id ──────────
	//
	// The iteration-1 implementer launches `--session-id <id1>`; iteration 2 must
	// `--resume <id1>` (NOT a freshly-synthesised id).  sessionIDs[0] is the
	// iter-1 implementer id; resumeID is the id on the --resume launch.
	if len(sessionIDs) == 0 || sessionIDs[0] == "" {
		t.Fatalf("hk-za5mz: no iteration-1 --session-id captured; sessionIDs=%v", sessionIDs)
	}
	iter1ID := sessionIDs[0]
	if resumeID == "" {
		t.Fatalf("hk-za5mz: no --resume session id captured (resume launch did not pass --resume <id>)")
	}
	if resumeID != iter1ID {
		t.Errorf("hk-za5mz REGRESSION: iter-2 --resume targets %q but iter-1 minted %q "+
			"(resuming a synthetic/wrong id reattaches to the wrong session)", resumeID, iter1ID)
	}
	// Independent witness: the resume paste buffer name encodes the same id.
	wantBuf := daemon.ExportedBufferName(iter1ID, "task")
	foundBuf := false
	for _, b := range resumeBufs {
		if b == wantBuf {
			foundBuf = true
			break
		}
	}
	if !foundBuf {
		t.Errorf("hk-za5mz REGRESSION: resume paste buffer names %v do not include %q "+
			"(combined feedback paste did not target the real iter-1 session)", resumeBufs, wantBuf)
	}

	// ── (b) hk-ip33d guard: the feedback prompt was actually SUBMITTED ─────────
	//
	// The first submit Enter was dropped (not-ready REPL); progress was made only
	// because the bounded retry (the fix) sent a SECOND submit Enter, which the
	// substrate honoured (wrote the sentinel) and which released the iter-2
	// implementer to commit.  A single submit Enter (the pre-fix behaviour) would
	// leave submitEnters == 1, the sentinel absent, and the iter-2 commit never
	// landing → no_progress_detected.
	if submitEnters < 2 {
		t.Errorf("hk-ip33d REGRESSION: only %d submit Enter(s) sent after the resume paste; "+
			"the bounded retry did not fire, so a dropped first Enter would leave the prompt unsubmitted", submitEnters)
	}

	// The run must complete with APPROVE — proving iteration 2 made progress.
	if !result.Success {
		t.Errorf("hk-ip33d REGRESSION: expected success=true (RC→iter2→APPROVE); "+
			"got success=false, completion_reason=%q, summary=%q",
			result.CompletionReason, result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("hk-ip33d REGRESSION: completion_reason=%q; want %q (summary=%q)",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved, result.Summary)
	}

	eventTypes := collector.eventTypes()
	for _, et := range eventTypes {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-ip33d REGRESSION: no_progress_detected emitted — iter-2 diff unchanged "+
				"(the feedback prompt was never submitted); events=%v", eventTypes)
			break
		}
		if et == string(core.EventTypeAgentReadyTimeout) {
			t.Errorf("hk-ip33d/hk-isq02 REGRESSION: agent_ready_timeout emitted — the resume "+
				"implementer never reached ready; events=%v", eventTypes)
			break
		}
	}

	// Sequence sanity: the full RC→iter2→APPROVE chain must be present.
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeImplementerResumed))
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),   // iter1
		string(core.EventTypeReviewerVerdict),    // iter1 RC
		string(core.EventTypeImplementerResumed), // iter2 dispatch
		string(core.EventTypeReviewerLaunched),   // iter2
		string(core.EventTypeReviewerVerdict),    // iter2 APPROVE
		string(core.EventTypeReviewLoopCycleComplete),
	})
}
