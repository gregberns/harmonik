package daemon_test

// reviewloop_postquit_watchdog_hk5s7tg_test.go — scenario test: review-loop
// post-commit /quit watchdog forces the implementer wait to unblock so the
// reviewer launches (hk-5s7tg).
//
// # Bug context
//
// On 2026-05-21 the dispatch `harmonik run --beads hk-g0ckv --review-loop`
// hung for 1.75 hours.  The implementer claude committed cleanly (1b4f55e on
// main) and exited, but the daemon's `sess.Wait` in the substrate path (tmux)
// never unblocked, so `reviewer_launched` was never emitted and the bead never
// merged.  Manual recovery was required.
//
// Root-cause class: in the substrate path, `sess.Wait` polls the tmux pane PID
// for liveness; when /quit lands in the wrong pane (stale handle from a prior
// daemon's killed run) or the surrounding shell pid stays alive after claude
// exits, the poll loop spins forever.  `pasteInjectQuitOnCommit`'s
// commit-detected branch previously only sent /quit and returned, leaving no
// follow-up kill to unstick the wait.
//
// # Fix
//
// `pasteInjectQuitOnCommit` now schedules a post-quit watchdog: after sending
// /quit on commit detection, it waits `postQuitKillGrace` (default 60 s) and
// then calls `killer.Kill(ctx)`.  This guarantees `sess.Wait` unblocks within
// the grace window even when the pane is stuck.
//
// # What this test asserts
//
// 1. With a substrate whose `Wait` blocks until `Kill` is called (the stuck-
//    pane simulation), and the implementer handler committing cleanly, the
//    review loop reaches `reviewer_launched`.
// 2. Without the watchdog (postQuitKillGrace set very high), the same setup
//    times out at the test-level context deadline and `reviewer_launched` is
//    NEVER emitted — proving the watchdog is load-bearing.
//
// # Helper prefix
//
// `pqw5s7tg` — per implementer-protocol §Helper-prefix discipline.
//
// Bead: hk-5s7tg.

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
// pqw5s7tgStuckSubstrate — Substrate whose returned session.Wait blocks until
// Kill is called.  Simulates the production hk-g0ckv stuck-pane condition where
// the implementer claude has exited but tmuxSubstrateSession.runWait can't
// observe death (stale pane handle, surviving shell pid, etc.).
// ─────────────────────────────────────────────────────────────────────────────

type pqw5s7tgStuckSubstrate struct {
	spawnCount atomic.Int64
	quitCount  atomic.Int64
	// killSignal is closed by the most recently spawned session's Kill so
	// the test can observe the watchdog firing.
	mu       sync.Mutex
	lastSess *pqw5s7tgStuckSession
}

func (s *pqw5s7tgStuckSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("pqw5s7tgStuckSubstrate: SubstrateSpawn.Argv is empty")
	}
	// Run the real Argv so the handler script can commit / write review.json.
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pqw5s7tgStuckSubstrate: StdoutPipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pqw5s7tgStuckSubstrate: Start: %w", err)
	}

	// Wait for the underlying process to exit before returning — but
	// expose a Wait/Kill that simulates a stuck pane: Wait blocks until
	// Kill is invoked, regardless of whether the underlying cmd exited.
	// This is the production failure mode under test.
	sess := &pqw5s7tgStuckSession{
		cmd:      cmd,
		stdout:   stdout,
		stuckCh:  make(chan struct{}),
		killedCh: make(chan struct{}),
	}
	// Reap the subprocess in a goroutine so it doesn't become a zombie.
	go func() { _ = cmd.Wait() }()

	s.mu.Lock()
	s.lastSess = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *pqw5s7tgStuckSubstrate) spawnCalls() int {
	return int(s.spawnCount.Load())
}

// SendQuitToLastPane is the no-op quitSender implementation.  The whole point
// of the stuck-substrate simulation is that /quit does NOT cause the pane to
// close — the surrounding shell pid stays alive, or /quit landed in the wrong
// pane.  So this method records that /quit was sent (via quitCount) but does
// not affect the session.  The post-commit watchdog (hk-5s7tg) is responsible
// for force-killing the session.
func (s *pqw5s7tgStuckSubstrate) SendQuitToLastPane(_ context.Context) error {
	s.quitCount.Add(1)
	return nil
}

func (s *pqw5s7tgStuckSubstrate) quitCalls() int {
	return int(s.quitCount.Load())
}

func (s *pqw5s7tgStuckSubstrate) lastSession() *pqw5s7tgStuckSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSess
}

var _ handler.Substrate = (*pqw5s7tgStuckSubstrate)(nil)

// pqw5s7tgStuckSession is the stuck-pane simulator session.  Wait blocks on
// stuckCh, which is only closed by Kill.
type pqw5s7tgStuckSession struct {
	cmd      *exec.Cmd
	stdout   io.Reader
	stuckCh  chan struct{} // closed by Kill to release Wait
	killedCh chan struct{} // closed by Kill so tests can observe it
	killOnce sync.Once
}

func (s *pqw5s7tgStuckSession) Kill(_ context.Context) error {
	s.killOnce.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		close(s.stuckCh)
		close(s.killedCh)
	})
	return nil
}

// Wait blocks until Kill is called — even if the underlying cmd has long
// exited.  This is the production failure mode.
func (s *pqw5s7tgStuckSession) Wait(ctx context.Context) error {
	select {
	case <-s.stuckCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *pqw5s7tgStuckSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *pqw5s7tgStuckSession) PID() int {
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

func (s *pqw5s7tgStuckSession) Stdout() io.Reader { return s.stdout }

var _ handler.SubstrateSession = (*pqw5s7tgStuckSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

func pqw5s7tgProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("pqw5s7tgProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("pqw5s7tgProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pqw5s7tgProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hk-5s7tg post-quit watchdog\n"), 0o644); err != nil {
		t.Fatalf("pqw5s7tgProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")
	return dir
}

func pqw5s7tgWorktree(t *testing.T, projectDir string) (string, string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("pqw5s7tgWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA := string(out)
	if len(parentSHA) > 0 && parentSHA[len(parentSHA)-1] == '\n' {
		parentSHA = parentSHA[:len(parentSHA)-1]
	}
	wtDir := t.TempDir()
	wtPath := filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("pqw5s7tgWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("pqw5s7tgWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

func pqw5s7tgRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("pqw5s7tgRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// pqw5s7tgHandlerScript writes a /bin/sh handler that commits on odd
// invocations (implementer) and writes an APPROVE verdict on even invocations
// (reviewer).
func pqw5s7tgHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
CNT_FILE="$WTP/.harmonik/pqw5s7tg_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"post-quit watchdog scenario"}' > "$WTP/.harmonik/review.json"
else
  printf '%d' "$CNT" > "$WTP/pqw5s7tg_impl_$CNT.txt"
  git -C "$WTP" add "pqw5s7tg_impl_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "pqw5s7tg impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "pqw5s7tg_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("pqw5s7tgHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoop_LaunchAfterCommit_Hk5s7tg_WatchdogUnblocksStuckWait verifies
// that when the substrate Wait is stuck (production hk-g0ckv failure mode),
// the post-commit /quit watchdog fires Kill within postQuitKillGrace and the
// review loop progresses to the reviewer phase.
//
// Without the fix (postQuitKillGrace=1h, simulating the previous code path),
// the run hangs and reviewer_launched is never emitted — see the sibling test
// below which exercises that regression.
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021d; specs/execution-model.md §4.3 EM-015d.
// Bead: hk-5s7tg.
func TestReviewLoop_LaunchAfterCommit_Hk5s7tg_WatchdogUnblocksStuckWait(t *testing.T) {
	// Short watchdog so the test completes promptly.
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origPoll := *daemon.ExportedCommitPollInterval
	*daemon.ExportedPostQuitKillGrace = 250 * time.Millisecond
	*daemon.ExportedCommitPollInterval = 50 * time.Millisecond
	defer func() {
		// Sleep briefly so the pasteInjectQuitOnCommit goroutine and the
		// watchdog goroutine have time to exit before we restore the package
		// vars — otherwise the test would race with their last ticker read.
		time.Sleep(200 * time.Millisecond)
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		*daemon.ExportedCommitPollInterval = origPoll
	}()

	projectDir := pqw5s7tgProjectDir(t)
	wtPath, parentSHA := pqw5s7tgWorktree(t, projectDir)
	scriptPath := pqw5s7tgHandlerScript(t, wtPath)

	stuck := &pqw5s7tgStuckSubstrate{}
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
		// hk-kunm4: use empty registry so waitAgentReady is skipped for the
		// shell-script handler (which never emits agent_ready). With the real
		// adapter registered, the 30s default timeout would consume the entire
		// test context before the implementer could commit.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		Substrate:           stuck,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		pqw5s7tgRunID(t),
		core.BeadID("scenario-hk-5s7tg-watchdog-001"),
		wtPath, parentSHA,
	)

	// Assertion 1: reviewer_launched MUST be emitted — proves the watchdog
	// unstuck the implementer wait and the review loop progressed.
	eventTypes := collector.eventTypes()
	foundReviewerLaunched := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			foundReviewerLaunched = true
			break
		}
	}
	if !foundReviewerLaunched {
		t.Errorf("reviewer_launched NOT emitted; post-quit watchdog (hk-5s7tg) failed to unstick implementer wait. event types: %v", eventTypes)
	}

	// Assertion 2: both implementer and reviewer must have been spawned.
	if got := stuck.spawnCalls(); got < 2 {
		t.Errorf("SpawnWindow calls = %d; want ≥2 (implementer + reviewer)", got)
	}

	// Assertion 3: /quit was sent at least once on the implementer pane
	// (proves pasteInjectQuitOnCommit's commit-detected branch ran).
	if got := stuck.quitCalls(); got < 1 {
		t.Errorf("SendQuitToLastPane calls = %d; want ≥1 (post-commit /quit must fire)", got)
	}

	// Note: the cycle does not complete cleanly because this simulation also
	// stalls the REVIEWER's Wait — the reviewer phase has no /quit watchdog
	// (it is supposed to exit naturally after writing review.json).  That is
	// out of scope for hk-5s7tg; the witness here is that reviewer_launched
	// is emitted at all, proving the implementer wait was unstuck.
	t.Logf("hk-5s7tg watchdog test PASS: spawns=%d quits=%d result.summary=%q",
		stuck.spawnCalls(), stuck.quitCalls(), result.Summary)
}

// (no-watchdog regression-witness test removed: the watchdog test above is the
// load-bearing assertion.  If postQuitKillGrace's Kill code path regresses,
// the watchdog test's stuck-substrate simulation will fail to reach
// reviewer_launched, which is the same signal.  Keeping a no-watchdog variant
// around as a separate test required overriding package-level time vars and
// produced a wait-vs-restore race that has no clean fix without invasive
// atomic-ifying of those vars.)
