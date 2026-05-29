package daemon_test

// reviewloop_feedback_inject_hk7x7ea_test.go — regression test: after a
// REQUEST_CHANGES verdict, reviewer-feedback.iter-N.md is written before the
// iter-(N+1) implementer-resume launches, enabling the paste-inject to deliver
// reviewer notes and the implementer to produce a different diff (hk-7x7ea).
//
// # Bug reproduced
//
// With hk-lckbv (buffer-name) fixed, the iter-2 implementer-resume no longer
// wedges, but fails differently: reviewer-feedback.iter-1.md is ABSENT when
// pasteInjectImplementerResume looks for it → logs "task file absent" and skips
// feedback inject → implementer-resume has no reviewer notes → reproduces the
// same diff → no_progress_detected at iteration 2 → run_failed.
//
// ROOT: mismatch between where the reviewer writes verdict (review.json) and
// where resume reads it (reviewer-feedback.iter-1.md, which was never written).
//
// # Fix
//
// runReviewLoop now calls workspace.WriteReviewerFeedback in the REQUEST_CHANGES
// routing case before incrementing state.iterationCount. This materializes
// reviewer-feedback.iter-N.md on disk so the iter-(N+1) implementer-resume can
// read it via pasteInjectImplementerResume.
//
// # How this test faithfully reproduces it
//
// The test drives a REQUEST_CHANGES → APPROVE cycle using the nil-watcher
// substrate (same as hk-isq02). The iter-2 implementer-resume script checks
// for reviewer-feedback.iter-1.md:
//   - Pre-fix: file absent → script writes sentinel; the run proceeds but the
//     diff hash is unchanged → no_progress_detected → run_failed.
//   - Post-fix: file present → script writes a new commit → diff advances →
//     run completes with APPROVE.
//
// Assertions:
//  1. result.Success == true (APPROVE at iteration 2).
//  2. reviewer-feedback.iter-1.md exists on disk with non-empty content.
//  3. no no_progress_detected event was emitted (diff DID change at iter 2).
//
// Helper prefix: rlFBInject (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-7x7ea.

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
	"github.com/gregberns/harmonik/internal/workspace"
)

// ─────────────────────────────────────────────────────────────────────────────
// rlFBInjectSubstrate — nil-watcher substrate + relay simulation
// ─────────────────────────────────────────────────────────────────────────────

// rlFBInjectSubstrate is a handler.Substrate whose sessions return Stdout() == nil
// (the production tmux path), forcing runReviewLoop down the nil-watcher path.
// For fresh (`--session-id`) launches it delivers a relay agent_ready after a
// short delay (mimicking SessionStart→agent_ready synthesis); for `--resume`
// launches it delivers nothing (the hk-isq02 gap, patched by the resume-phase
// fallback). This mirrors rlResumeReadySubstrate from reviewloop_resume_ready_hkisq02_test.go.
type rlFBInjectSubstrate struct {
	store *daemon.HookSessionStoreExported
	runID core.RunID

	spawnCount     atomic.Int64
	resumeLaunches atomic.Int64
}

func (s *rlFBInjectSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("rlFBInjectSubstrate: SubstrateSpawn.Argv is empty")
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
	if isResume {
		s.resumeLaunches.Add(1)
	}

	//nolint:gosec // G204: Argv comes from test-internal HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rlFBInjectSubstrate: Start: %w", err)
	}

	// Only fresh (`--session-id`) launches receive a relay agent_ready.
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

	return &rlFBInjectExecSession{cmd: cmd}, nil
}

func (s *rlFBInjectSubstrate) spawnCalls() int    { return int(s.spawnCount.Load()) }
func (s *rlFBInjectSubstrate) resumeCount() int   { return int(s.resumeLaunches.Load()) }

var _ handler.Substrate = (*rlFBInjectSubstrate)(nil)

// rlFBInjectExecSession wraps exec.Cmd with nil Stdout() — forces nil-watcher path.
type rlFBInjectExecSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (s *rlFBInjectExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *rlFBInjectExecSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

func (s *rlFBInjectExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *rlFBInjectExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — forces nil-watcher (tmux) path.
func (s *rlFBInjectExecSession) Stdout() io.Reader { return nil }

var _ handler.SubstrateSession = (*rlFBInjectExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func rlFBInjectProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlFBInjectProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlFBInjectProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rlFBInjectProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("feedback-inject scenario\n"), 0o644); err != nil {
		t.Fatalf("rlFBInjectProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

func rlFBInjectWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlFBInjectWorktree: git rev-parse HEAD: %v", err)
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
		t.Fatalf("rlFBInjectWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlFBInjectWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

func rlFBInjectRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlFBInjectRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlFBInjectHandlerScript writes a /bin/sh handler that drives the 4-phase
// REQUEST_CHANGES → APPROVE cycle. The iter-2 implementer-resume step checks
// for the presence of reviewer-feedback.iter-1.md:
//
//   - If the file is absent (pre-fix): the script still commits (to avoid
//     no_progress_detected masking the real failure), but writes a "MISSING"
//     sentinel file so the test can detect the pre-fix state.
//   - If the file is present (post-fix): the script commits normally and the
//     cycle completes with APPROVE.
func rlFBInjectHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	// The feedback file path the daemon MUST write before iter-2 implementer-resume.
	feedbackFile := workspace.ReviewerFeedbackPath(wtPath, 1)
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
FEEDBACK_FILE='` + feedbackFile + `'
CNT_FILE="$WTP/.harmonik/rlfbinject_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter1 implementer-initial: commit a file.
    printf 'v1' > "$WTP/rlfbi_impl_1.txt"
    git -C "$WTP" add rlfbi_impl_1.txt >/dev/null 2>&1
    git -C "$WTP" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter1 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # iter1 reviewer: REQUEST_CHANGES.
    printf '{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["test-flag"],"notes":"please address the test flag before proceeding"}' \
      > "$WTP/.harmonik/review.json"
    ;;
  3)
    # iter2 implementer-resume: check feedback file then commit a DIFFERENT file.
    # Record whether the feedback file was present — test inspects this sentinel.
    if [ -f "$FEEDBACK_FILE" ]; then
      printf 'present' > "$WTP/.harmonik/rlfbi_feedback_status.txt"
    else
      printf 'absent' > "$WTP/.harmonik/rlfbi_feedback_status.txt"
    fi
    # Always commit a new file so the diff hash advances (avoids no_progress_detected
    # masking the test result regardless of feedback-file presence).
    printf 'v2' > "$WTP/rlfbi_impl_2.txt"
    git -C "$WTP" add rlfbi_impl_2.txt rlfbi_feedback_status.txt 2>/dev/null || true
    git -C "$WTP" add rlfbi_impl_2.txt >/dev/null 2>&1
    git -C "$WTP" add .harmonik/rlfbi_feedback_status.txt >/dev/null 2>&1
    git -C "$WTP" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter2 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter2 reviewer: APPROVE.
    printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"looks good now"}' \
      > "$WTP/.harmonik/review.json"
    ;;
esac
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "rlfbinject_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlFBInjectHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_FeedbackFileInjected is the regression test for
// hk-7x7ea. It verifies that:
//
//  1. After the iter-1 reviewer returns REQUEST_CHANGES, the daemon writes
//     reviewer-feedback.iter-1.md before launching the iter-2 implementer.
//  2. The run completes with APPROVE (not no_progress_detected).
//  3. No no_progress_detected event is emitted (the diff DID advance at iter 2).
//
// Pre-fix: reviewer-feedback.iter-1.md is absent; the run either hits
// no_progress_detected (same diff) or succeeds despite missing feedback.
// Post-fix: the file is present; the iter-2 implementer can read it.
//
// Bead: hk-7x7ea.
func TestScenario_ReviewLoop_FeedbackFileInjected(t *testing.T) {
	t.Parallel()

	projectDir := rlFBInjectProjectDir(t)
	wtPath, parentSHA := rlFBInjectWorktree(t, projectDir)
	scriptPath := rlFBInjectHandlerScript(t, wtPath)

	store := daemon.ExportedNewHookSessionStore()
	runID := rlFBInjectRunID(t)
	sub := &rlFBInjectSubstrate{store: store, runID: runID}

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
		// 8s > 2s resume fallback grace so fallback wins; pre-fix timeout fires at 8s.
		AgentReadyTimeout: 8 * time.Second,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("scenario-rl-fb-inject-hk7x7ea"),
		wtPath, parentSHA,
	)

	t.Logf("result=%+v resumeCount=%d spawns=%d events=%v",
		result, sub.resumeCount(), sub.spawnCalls(), collector.eventTypes())

	// The cycle must have reached an implementer-resume launch.
	if sub.resumeCount() < 1 {
		t.Fatalf("test did not reach an implementer-resume (--resume) launch; "+
			"resumeCount=%d — fixture/handler did not produce REQUEST_CHANGES→iter2", sub.resumeCount())
	}

	// Core assertion 1: run must succeed (APPROVE at iteration 2).
	if !result.Success {
		t.Errorf("hk-7x7ea REGRESSION: expected success=true (RC→iter2→APPROVE); "+
			"got success=false, completion_reason=%q, summary=%q",
			result.CompletionReason, result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("hk-7x7ea REGRESSION: completion_reason=%q; want %q (summary=%q)",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved, result.Summary)
	}

	// Core assertion 2: reviewer-feedback.iter-1.md must exist and be non-empty.
	feedbackPath := workspace.ReviewerFeedbackPath(wtPath, 1)
	fi, err := os.Stat(feedbackPath)
	if err != nil {
		t.Errorf("hk-7x7ea REGRESSION: reviewer-feedback.iter-1.md absent after REQUEST_CHANGES: %v", err)
	} else if fi.Size() == 0 {
		t.Errorf("hk-7x7ea REGRESSION: reviewer-feedback.iter-1.md is empty (WriteReviewerFeedback produced no content)")
	}

	// Core assertion 3: the feedback file must have been PRESENT when iter-2
	// implementer ran (sentinel written by the handler script).
	statusPath := filepath.Join(wtPath, ".harmonik", "rlfbi_feedback_status.txt")
	statusBytes, statusErr := os.ReadFile(statusPath)
	if statusErr != nil {
		t.Errorf("hk-7x7ea: feedback-status sentinel file not found at %s: %v", statusPath, statusErr)
	} else if string(statusBytes) != "present" {
		t.Errorf("hk-7x7ea REGRESSION: feedback file was %q at iter-2 implementer launch; want \"present\"",
			string(statusBytes))
	}

	// Core assertion 4: no no_progress_detected event (diff DID advance at iter 2).
	eventTypes := collector.eventTypes()
	for _, et := range eventTypes {
		if et == string(core.EventTypeNoProgressDetected) {
			t.Errorf("hk-7x7ea REGRESSION: no_progress_detected event emitted — "+
				"the iter-2 diff is unchanged (implementer did not address reviewer notes); events=%v", eventTypes)
			break
		}
	}

	// Sequence sanity: verify the full RC→iter2→APPROVE event chain.
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
