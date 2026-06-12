package daemon_test

// reviewloop_resume_ready_hkisq02_test.go — regression test: the iteration-2
// (implementer-resume) implementer reaches agent_ready under the tmux substrate
// even though `claude --resume` does not re-fire a SessionStart hook (hk-isq02).
//
// # Bug reproduced
//
// In review-loop mode, when iteration 1's implementer commits and the reviewer
// returns REQUEST_CHANGES, the daemon launches the iteration-2 implementer with
// `claude --resume <uuid>`. Under the tmux substrate (Stdout() == nil → nil
// watcher), the daemon's ONLY ready signal is the relay-synthesized agent_ready,
// which the hook-relay produces solely on receipt of a SessionStart hook
// (internal/hookrelay/hookrelay.go buildSessionStartMessage). A `--resume`
// reattach does not reliably re-fire SessionStart, so the relay never delivers
// agent_ready, waitAgentReady never observes it, and the run fails
// `implementer agent_ready_timeout at iteration 2`. Iteration-1 (`--session-id`,
// a fresh session) fires SessionStart and so readies fine — which is why beads
// that APPROVE at iteration 1 never hit this (latent), but any bead needing a
// fix-up cycle fails. Reproduced 3× in production (hk-69asi, hk-xp9j7).
//
// # How this test faithfully reproduces it
//
// rlResumeReadySubstrate is a substrate whose sessions return Stdout() == nil
// (the production tmux path; see internal/handler/handler.go:301-304), so
// runReviewLoop gets a NIL implWatcher and depends solely on the hook-relay
// agent_ready callback. The substrate plays the role of the hook-relay: it
// delivers an agent_ready envelope (via ExportedHookDispatch, exactly as the
// real socket path would) ONLY for `--session-id` launches (fresh sessions:
// implementer-initial + every reviewer), and NEVER for `--resume` launches
// (implementer-resume) — modelling the SessionStart-on-resume gap.
//
// Pre-fix: iteration 2 hangs until ErrAgentReadyTimeout → result.Success=false,
// completion_reason=error, summary contains "agent_ready_timeout at iteration 2".
// Post-fix: the resume-phase fallback (resumeReadyFallbackGrace) synthesizes
// agent_ready so iteration 2 readies, the cycle proceeds to the iter-2 reviewer,
// and the APPROVE verdict completes the run successfully.
//
// Helper prefix: rlResumeReady (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-isq02.

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

// ─────────────────────────────────────────────────────────────────────────────
// rlResumeReadySubstrate — nil-watcher substrate + relay simulation
// ─────────────────────────────────────────────────────────────────────────────

// rlResumeReadySubstrate is a handler.Substrate whose sessions return
// Stdout() == nil, forcing runReviewLoop down the nil-watcher (tmux) path. It
// stands in for the hook-relay: for `--session-id` launches it delivers an
// agent_ready envelope into the hook store after a short delay (mimicking the
// SessionStart→agent_ready synthesis of a fresh claude session); for `--resume`
// launches it delivers NOTHING (mimicking claude not re-firing SessionStart on
// resume — the hk-isq02 root cause).
type rlResumeReadySubstrate struct {
	store *daemon.HookSessionStoreExported
	runID core.RunID

	spawnCount atomic.Int64

	mu             sync.Mutex
	resumeLaunches int // count of --resume launches observed
}

// SpawnWindow runs the handler script via exec (so commits + review.json land),
// returns a session with a nil Stdout() (nil watcher), and — for fresh
// (`--session-id`) launches only — delivers a relay agent_ready after a delay.
func (s *rlResumeReadySubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("rlResumeReadySubstrate: SubstrateSpawn.Argv is empty")
	}

	// Determine launch kind from argv: `--resume` (implementer-resume) vs
	// `--session-id` (fresh: implementer-initial / reviewer).
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
		s.mu.Lock()
		s.resumeLaunches++
		s.mu.Unlock()
	}

	//nolint:gosec // G204: Argv comes from test-internal HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rlResumeReadySubstrate: Start: %w", err)
	}

	// Relay simulation: only a fresh (`--session-id`) launch fires a SessionStart
	// hook, so only those get a relay-synthesized agent_ready. `--resume` launches
	// get nothing — exactly the hk-isq02 production gap. The agent_ready is
	// delivered asynchronously (after a short delay) so it lands after the daemon
	// has called SetAgentReadyCallback (which happens after Launch returns).
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

	return &rlResumeReadyExecSession{cmd: cmd}, nil
}

func (s *rlResumeReadySubstrate) spawnCalls() int { return int(s.spawnCount.Load()) }

func (s *rlResumeReadySubstrate) resumeLaunchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeLaunches
}

var _ handler.Substrate = (*rlResumeReadySubstrate)(nil)

// rlResumeReadyExecSession wraps an exec.Cmd as a handler.SubstrateSession whose
// Stdout() returns nil — forcing the nil-watcher (tmux) path in handler.Launch.
type rlResumeReadyExecSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func (s *rlResumeReadyExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *rlResumeReadyExecSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

func (s *rlResumeReadyExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *rlResumeReadyExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — the production tmux path (handler.go:301-304 returns a
// nil watcher), which is the path that exhibits the hk-isq02 bug.
func (s *rlResumeReadyExecSession) Stdout() io.Reader { return nil }

var _ handler.SubstrateSession = (*rlResumeReadyExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func rlResumeReadyProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlResumeReadyProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlResumeReadyProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rlResumeReadyProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("resume-ready scenario\n"), 0o644); err != nil {
		t.Fatalf("rlResumeReadyProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

func rlResumeReadyWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlResumeReadyWorktree: git rev-parse HEAD: %v", err)
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
		t.Fatalf("rlResumeReadyWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlResumeReadyWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

func rlResumeReadyRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlResumeReadyRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlResumeReadyHandlerScript writes a /bin/sh handler that, on each invocation,
// alternates implementer (commit a unique file so the diff hash advances) and
// reviewer (write a verdict). The first reviewer returns REQUEST_CHANGES (forcing
// an iteration-2 implementer-resume — the buggy path); the second returns APPROVE.
func rlResumeReadyHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rlresumeready_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter1 implementer: commit.
    printf '1' > "$WS/rlrr_impl_1.txt"
    git -C "$WS" add rlrr_impl_1.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter1 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # iter1 reviewer: REQUEST_CHANGES (forces iter2 implementer-resume).
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":[],"notes":"please address X"}' > "$WS/.harmonik/review.json"
    ;;
  3)
    # iter2 implementer-resume: commit again (diff hash advances).
    printf '2' > "$WS/rlrr_impl_2.txt"
    git -C "$WS" add rlrr_impl_2.txt >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name=Test commit -m "iter2 impl" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter2 reviewer: APPROVE.
    mkdir -p "$WS/.harmonik"
    printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"looks good"}' > "$WS/.harmonik/review.json"
    ;;
esac
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "rlresumeready_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlResumeReadyHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_ResumeImplementerReachesReady is the regression test
// for hk-isq02. It drives a REQUEST_CHANGES → APPROVE cycle under a nil-watcher
// substrate where only `--session-id` (fresh) launches receive a relay
// agent_ready and `--resume` launches receive none.
//
// Pre-fix: the iteration-2 implementer-resume launch never observes agent_ready
// and the run fails with `implementer agent_ready_timeout at iteration 2`.
// Post-fix: the resume-phase fallback synthesizes agent_ready so iteration 2
// readies, the iter-2 reviewer runs, and the APPROVE verdict completes the run.
//
// agentReadyTimeout is set to 8s — comfortably larger than the 2s resume
// fallback grace so the fallback wins post-fix, yet small enough that the pre-fix
// timeout fires well inside the test's outer deadline (proving a timeout, not a
// hang).
//
// Bead: hk-isq02.
func TestScenario_ReviewLoop_ResumeImplementerReachesReady(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	// NOT parallel (hk-1o0cc de-flake): isolates the process-global
	// ~/.claude.json trust config so EnsureWorktreeTrust does not contend on the
	// real config's lock under load. See rlIsolateClaudeConfig.
	rlIsolateClaudeConfig(t)

	projectDir := rlResumeReadyProjectDir(t)
	wtPath, parentSHA := rlResumeReadyWorktree(t, projectDir)
	scriptPath := rlResumeReadyHandlerScript(t, wtPath)

	store := daemon.ExportedNewHookSessionStore()
	runID := rlResumeReadyRunID(t)
	sub := &rlResumeReadySubstrate{store: store, runID: runID}

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
		// 8s > 2s resume fallback grace: the fallback wins post-fix; pre-fix the
		// timeout fires at 8s, well inside the 60s outer deadline.
		AgentReadyTimeout: 8 * time.Second,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("scenario-rl-resume-ready-001"),
		wtPath, parentSHA,
	)

	t.Logf("result=%+v resumeLaunches=%d spawns=%d events=%v",
		result, sub.resumeLaunchCount(), sub.spawnCalls(), collector.eventTypes())

	// Sanity: the cycle must actually have reached an implementer-resume launch,
	// otherwise the test is not exercising the buggy path.
	if sub.resumeLaunchCount() < 1 {
		t.Fatalf("test did not reach an implementer-resume (--resume) launch; "+
			"resumeLaunches=%d — fixture/handler did not produce REQUEST_CHANGES→iter2", sub.resumeLaunchCount())
	}

	// Core assertion: the run must NOT fail with an iteration-2 agent_ready_timeout.
	// Pre-fix this is exactly the failure; post-fix the fallback prevents it.
	if !result.Success {
		t.Errorf("hk-isq02 REGRESSION: expected success=true (RC→iter2→APPROVE); "+
			"got success=false, completion_reason=%q, summary=%q",
			result.CompletionReason, result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("hk-isq02 REGRESSION: completion_reason=%q; want %q (summary=%q)",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved, result.Summary)
	}

	// The iteration-2 implementer must NOT have produced an agent_ready_timeout.
	eventTypes := collector.eventTypes()
	for _, et := range eventTypes {
		if et == string(core.EventTypeAgentReadyTimeout) {
			t.Errorf("hk-isq02 REGRESSION: agent_ready_timeout event emitted — the "+
				"resume-phase implementer never reached ready; events=%v", eventTypes)
			break
		}
	}

	// The cycle must have advanced to a second reviewer (iter-2) and completed.
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
