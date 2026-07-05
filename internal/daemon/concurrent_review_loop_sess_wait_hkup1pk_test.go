package daemon_test

// concurrent_review_loop_sess_wait_hkup1pk_test.go — regression test for hk-up1pk.
//
// Root cause: e0b02f77 bounded implSess.Wait (reviewloop.go:741) and revSess.Wait
// (:1474) with agentReadyKillReapTimeout after ErrAgentReadyTimeout, but the fix
// was accidentally reverted by the captain commit dc316cd6.
//
// This file proves BOTH paths are bounded:
//
//   Test 1 (IMPL path): Two beads dispatched concurrently under
//   WorkflowModeReviewLoop with MaxConcurrent=2. Fake tmux adapter where
//   WindowPanePID always returns (0, nil) — pane never closes after Kill —
//   simulating a remote pane that survives SIGKILL. AgentReadyTimeout=150ms
//   fires, then implSess.Wait must return within agentReadyKillReapTimeout
//   (overridden to 50ms) so ReopenBead is called with a live context.
//   Pre-fix: implSess.Wait(ctx) blocks until the 3s deadline cancels ctx;
//   ReopenBead receives a cancelled ctx → bothReopenedCh never closed → test fails.
//
//   Test 2 (REV path): ExportedRunReviewLoop called directly with a counter-based
//   custom substrate: impl session (call 1) emits agent_ready via Stdout pipe and
//   returns from Wait immediately; reviewer session (call 2) has Stdout=nil and
//   Wait blocks until ctx cancelled — simulating the remote-pane hang. A fast
//   hookStore avoids the 3s stopHookGrace on the impl phase. AgentReadyTimeout=150ms
//   fires for the reviewer; revSess.Wait must return within agentReadyKillReapTimeout
//   (50ms). Pre-fix: revSess.Wait(ctx) blocks until the 5s test deadline expires.
//
// Helper prefix: rlSessWaitBounded.

import (
	"context"
	"encoding/json"
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
// Test 1: IMPL path — implSess.Wait bounded in runReviewLoop
// ─────────────────────────────────────────────────────────────────────────────

// TestConcurrentReviewLoop_ImplSessWaitBounded verifies that under
// MaxConcurrent=2 and WorkflowModeReviewLoop, both concurrent runs that hit
// ErrAgentReadyTimeout call ReopenBead with a live (non-cancelled) context,
// even when the remote session's WindowPanePID never returns an error.
//
// This mirrors TestConcurrentRemoteAgentReady_BothBeadsReopened (hk-4hso5) but
// exercises the reviewloop.go implSess.Wait path (line 741) instead of the
// beadRunOne path.
//
// Pre-fix (RED): implSess.Wait(ctx) blocks until the test deadline cancels the
// per-run ctx; ReopenBead receives a cancelled ctx → returns error → channel
// never closed → test fails by timeout.
//
// Post-fix (GREEN): implSess.Wait uses a 50ms bounded context; ReopenBead
// receives context.Background() → succeeds → channel closed within ~300ms.
//
// Bead ref: hk-up1pk.
func TestConcurrentReviewLoop_ImplSessWaitBounded(t *testing.T) {
	// NOT parallel: ExportedSetAgentReadyKillReapTimeout modifies a package global.

	t.Cleanup(daemon.ExportedSetAgentReadyKillReapTimeout(50 * time.Millisecond))

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("rl-impl-sess-wait-A")
		beadB = core.BeadID("rl-impl-sess-wait-B")
	)

	fakeAdapter := &remoteAgentReadyFixtureAdapter{}
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "test-session")

	ledger := &remoteAgentReadyFixtureLedger{
		ready:          []core.BeadID{beadA, beadB},
		bothReopenedCh: make(chan struct{}),
	}
	collector := &stubEventCollector{}

	worktreeFactory := func(_ context.Context, _ string, runID string, _ string) (string, func(), error) {
		wtDir, err := os.MkdirTemp("", "rl-impl-sess-wait-wt-"+runID[:8]+"-")
		if err != nil {
			return "", nil, fmt.Errorf("rlSessWaitBounded: MkdirTemp: %w", err)
		}
		cleanup := func() { os.RemoveAll(wtDir) } //nolint:errcheck
		harmonikDir := filepath.Join(wtDir, ".harmonik")
		//nolint:gosec // G301: test-only temp dir
		if mkErr := os.MkdirAll(harmonikDir, 0o755); mkErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("rlSessWaitBounded: MkdirAll: %w", mkErr)
		}
		taskFile := filepath.Join(harmonikDir, "agent-task.md")
		//nolint:gosec // G306: test-only
		if wErr := os.WriteFile(taskFile, []byte("Please read .harmonik/agent-task.md and begin.\n"), 0o644); wErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("rlSessWaitBounded: WriteFile: %w", wErr)
		}
		return wtDir, cleanup, nil
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:       2,
		Substrate:           substrate,
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:     worktreeFactory,
		AgentReadyTimeout:   150 * time.Millisecond,
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	// 3s total: AgentReadyTimeout (150ms) + bounded Wait (50ms) + overhead << 1s.
	// The 3s deadline is the "test hangs" sentinel.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.bothReopenedCh:
		// GREEN: both ReopenBead calls received context.Background().
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for both beads to be reopened with a live context; " +
			"pre-fix: implSess.Wait(ctx) in runReviewLoop blocks until test deadline, " +
			"ReopenBead then receives a cancelled ctx and returns an error")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: REV path — revSess.Wait bounded in runReviewLoop
// ─────────────────────────────────────────────────────────────────────────────

// rlSessWaitBoundedRevSubstrate is a counter-based handler.Substrate that
// returns different sessions for the impl (call 1) and reviewer (call 2).
//
// Call 1 (impl): Stdout returns a pipe that emits agent_ready then closes;
// Wait returns nil immediately.
//
// Call 2 (reviewer): Stdout returns nil (no watcher, no events);
// Wait blocks until ctx is cancelled — simulating a remote pane that survives Kill.
type rlSessWaitBoundedRevSubstrate struct {
	mu         sync.Mutex
	spawnCount int
}

func (s *rlSessWaitBoundedRevSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.mu.Lock()
	s.spawnCount++
	n := s.spawnCount
	s.mu.Unlock()

	if n == 1 {
		// Impl session: emit agent_ready then EOF so waitAgentReady returns nil,
		// and Wait returns nil immediately (normal completion).
		pr, pw := io.Pipe()
		go func() {
			_, _ = pw.Write([]byte(`{"type":"agent_ready"}` + "\n"))
			pw.Close()
		}()
		return &rlSessWaitBoundedRevImplSession{stdout: pr}, nil
	}
	// Reviewer session: no stdout (no watcher, no events), Wait blocks.
	return &rlSessWaitBoundedRevRevSession{}, nil
}

var _ handler.Substrate = (*rlSessWaitBoundedRevSubstrate)(nil)

// rlSessWaitBoundedRevImplSession is the impl SubstrateSession for Test 2.
// Stdout returns a non-nil pipe so SpawnWatcher is wired and agent_ready is
// delivered. Wait returns nil immediately to simulate fast impl completion.
type rlSessWaitBoundedRevImplSession struct {
	stdout io.Reader
}

func (s *rlSessWaitBoundedRevImplSession) Kill(_ context.Context) error  { return nil }
func (s *rlSessWaitBoundedRevImplSession) Wait(_ context.Context) error  { return nil }
func (s *rlSessWaitBoundedRevImplSession) Outcome() handler.Outcome      { return handler.Outcome{} }
func (s *rlSessWaitBoundedRevImplSession) PID() int                      { return 0 }
func (s *rlSessWaitBoundedRevImplSession) Stdout() io.Reader             { return s.stdout }

var _ handler.SubstrateSession = (*rlSessWaitBoundedRevImplSession)(nil)

// rlSessWaitBoundedRevRevSession is the reviewer SubstrateSession for Test 2.
// Stdout returns nil (no watcher, so no agent_ready events reach revTapCh).
// Wait blocks until ctx is cancelled — this is the regression path.
// Pre-fix: Wait(ctx) blocks indefinitely when the pane never closes.
// Post-fix: Wait(revWaitCtx) returns after agentReadyKillReapTimeout.
type rlSessWaitBoundedRevRevSession struct{}

func (s *rlSessWaitBoundedRevRevSession) Kill(_ context.Context) error { return nil }
func (s *rlSessWaitBoundedRevRevSession) Wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
func (s *rlSessWaitBoundedRevRevSession) Outcome() handler.Outcome { return handler.Outcome{} }
func (s *rlSessWaitBoundedRevRevSession) PID() int                 { return 0 }
func (s *rlSessWaitBoundedRevRevSession) Stdout() io.Reader        { return nil }

var _ handler.SubstrateSession = (*rlSessWaitBoundedRevRevSession)(nil)

// rlSessWaitBoundedHookStore is a hookStore stub that fast-paths LatestOutcome
// so waitWithSocketGrace for the impl does not block on the 3s stopHookGrace
// window. All other methods are no-ops.
type rlSessWaitBoundedHookStore struct{}

func (h *rlSessWaitBoundedHookStore) RegisterHookSession(_, _ string) {}
func (h *rlSessWaitBoundedHookStore) CloseHookSession(_, _ string)    {}
func (h *rlSessWaitBoundedHookStore) LatestOutcome(_, _ string) *json.RawMessage {
	// Return a valid ExportedOutcomeEmittedPayload JSON so parseLatestOutcome
	// returns a non-nil pointer and waitWithSocketGrace fast-paths.
	raw := json.RawMessage(`{"kind":"complete"}`)
	return &raw
}
func (h *rlSessWaitBoundedHookStore) WaitForOutcome(_ context.Context, _, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"complete"}`), nil
}
func (h *rlSessWaitBoundedHookStore) SetAgentReadyCallback(_, _ string, _ func()) {}

// rlSessWaitBoundedRevGitRevParse runs `git -C dir rev-parse ref` and returns
// the trimmed SHA.
func rlSessWaitBoundedRevGitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	//nolint:gosec // G204: test-only git args
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rlSessWaitBoundedRevGitRevParse: git rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// rlSessWaitBoundedRevMakeCommit adds a file and commits in dir.
func rlSessWaitBoundedRevMakeCommit(t *testing.T, dir, filename, msg string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: test-only git args
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rlSessWaitBoundedRevMakeCommit: git %v: %v\n%s", args, err, out)
		}
	}
	fpath := filepath.Join(dir, filename)
	//nolint:gosec // G306: test-only
	if err := os.WriteFile(fpath, []byte("test\n"), 0o644); err != nil {
		t.Fatalf("rlSessWaitBoundedRevMakeCommit: WriteFile: %v", err)
	}
	run("add", filename)
	run("commit", "-m", msg)
}

// TestReviewLoop_RevSessWaitBounded verifies that when the reviewer session's
// Wait blocks indefinitely (remote pane survives Kill), runReviewLoop still
// returns within agentReadyKillReapTimeout + AgentReadyTimeout + margin.
//
// Setup: impl session completes immediately (agent_ready from Stdout pipe,
// Wait returns nil), passing the no-commit guard (HEAD != parentSHA).
// Reviewer session has no Stdout (no watcher, no events) and blocks in Wait —
// simulating a remote pane that survives Kill after ErrAgentReadyTimeout.
//
// Pre-fix (RED): revSess.Wait(ctx) blocks until the 5s test deadline; the
// function returns only after ctx is cancelled — well past the 1.5s assertion.
//
// Post-fix (GREEN): revSess.Wait uses a 50ms bounded context; the function
// returns in ~200ms total.
//
// Bead ref: hk-up1pk.
func TestReviewLoop_RevSessWaitBounded(t *testing.T) {
	// NOT parallel: ExportedSetAgentReadyKillReapTimeout modifies a package global.

	t.Cleanup(daemon.ExportedSetAgentReadyKillReapTimeout(50 * time.Millisecond))

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// parentSHA = initial commit; HEAD will be advanced to pass the no-commit guard.
	parentSHA := rlSessWaitBoundedRevGitRevParse(t, projectDir, "HEAD")

	// Advance HEAD past parentSHA so the no-commit guard passes (HEAD != parentSHA).
	rlSessWaitBoundedRevMakeCommit(t, projectDir, "impl_work.txt", "impl: add work")

	substrate := &rlSessWaitBoundedRevSubstrate{}
	collector := &stubEventCollector{}

	runID := rlFixtureRunID(t)
	beadID := core.BeadID("rl-rev-sess-wait-bounded")

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         &remoteAgentReadyFixtureLedger{bothReopenedCh: make(chan struct{})},
		Bus:               collector,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		Substrate:         substrate,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		HookStore:         &rlSessWaitBoundedHookStore{},
		AgentReadyTimeout: 150 * time.Millisecond,
	})

	// 5s deadline — the "test hangs" sentinel for the pre-fix case.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan daemon.ReviewLoopResultExported, 1)
	go func() {
		result := daemon.ExportedRunReviewLoop(ctx, deps, runID, beadID, projectDir, parentSHA)
		done <- result
	}()

	// With fix: returns in ~200ms (150ms agent_ready_timeout + 50ms bounded Wait).
	// Without fix: revSess.Wait(ctx) blocks until ctx cancelled (5s).
	select {
	case <-done:
		// GREEN: review loop returned before the 1.5s mark.
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("ExportedRunReviewLoop did not return within 1.5s; " +
			"pre-fix: revSess.Wait(ctx) in runReviewLoop blocks until the test " +
			"deadline expires when the remote pane survives Kill after ErrAgentReadyTimeout")
	}
}
