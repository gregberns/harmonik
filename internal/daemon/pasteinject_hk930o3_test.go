package daemon_test

// pasteinject_hk930o3_test.go — unit tests for the brief_delivered gate in
// pasteInjectQuitOnCommit (hk-930o3).
//
// Problem: in run 019e4dfc-0f8c, a stale tmux pane handle from a prior run
// received /exit before the newly-launched claude read agent-task.md.  The
// session log showed 5 lines, all at the same timestamp, all at the /exit
// level — zero assistant turns.
//
// Fix: pasteInjectQuitOnCommit now blocks on a briefDelivered channel before
// entering the commit-poll loop.  pasteInjectOnLaunch closes that channel after
// WriteLastPane completes.
//
// Tests:
//  1. TestPasteInjectQuitOnCommit_NoBriefDeliveredBlocksCommitPoll — when
//     briefDelivered is NOT closed, the commit poll loop must NOT fire /quit
//     within the observation window even if a new commit has already landed.
//  2. TestPasteInjectQuitOnCommit_BriefDeliveredGateOpensOnClose — once
//     briefDelivered is closed, the poll loop proceeds and /quit is sent.
//  3. TestPasteInjectQuitOnCommit_BriefDeliveredTimeoutProceedsWithPoll — if
//     briefDeliveredTimeout elapses without the channel closing, the poll loop
//     starts anyway (liveness: a broken session must not hang forever).
//
// Helper prefix: hk930o3Fixture.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk930o3QuitSender records SendQuitToLastPane calls.
type hk930o3QuitSender struct {
	calls atomic.Int64
}

func (q *hk930o3QuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hk930o3Killer records Kill calls.
type hk930o3Killer struct {
	calls atomic.Int64
}

func (k *hk930o3Killer) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk930o3ShortTimeouts overrides timing vars for tests.  postQuitKillGrace is
// set to 1 hour so the post-commit watchdog does not interfere.
func hk930o3ShortTimeouts(briefTimeout, poll, commitTimeout, killDelay time.Duration) func() {
	origBrief := *daemon.ExportedBriefDeliveredTimeout
	origPoll := *daemon.ExportedCommitPollInterval
	origTimeout := *daemon.ExportedCommitPollTimeout
	origKillDelay := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedBriefDeliveredTimeout = briefTimeout
	*daemon.ExportedCommitPollInterval = poll
	*daemon.ExportedCommitPollTimeout = commitTimeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedBriefDeliveredTimeout = origBrief
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origTimeout
		*daemon.ExportedNoChangeKillDelay = origKillDelay
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// hk930o3Git runs a git command in dir and fatals on error.
func hk930o3Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// hk930o3GitOutput runs a git command and returns trimmed stdout.
func hk930o3GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// hk930o3Worktree creates a temp git repo with one initial commit and returns
// the path and the initial HEAD SHA.
func hk930o3Worktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	hk930o3Git(t, dir, "init", "--initial-branch=main")
	hk930o3Git(t, dir, "config", "user.email", "test@test.com")
	hk930o3Git(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	hk930o3Git(t, dir, "add", ".")
	hk930o3Git(t, dir, "commit", "-m", "init")
	sha := hk930o3GitOutput(t, dir, "rev-parse", "HEAD")
	return dir, sha
}

// hk930o3AddCommit adds a file and commits it in dir using a background context
// so the goroutine is not cancelled when the test's t.Context() expires.
func hk930o3AddCommit(dir string) {
	bgCtx := context.Background()
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "work\n\nRefs: hk-930o3"},
	} {
		cmd := exec.CommandContext(bgCtx, "git", args...)
		cmd.Dir = dir
		_ = cmd.Run()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnCommit_NoBriefDeliveredBlocksCommitPoll verifies that
// when briefDelivered is NOT closed, /quit is not sent even if a new commit
// lands — the gate is blocking.
//
// The test lands a commit in the worktree, then calls
// pasteInjectQuitOnCommit with a briefDelivered channel that is NEVER closed.
// After an observation window (shorter than commitPollTimeout and
// briefDeliveredTimeout), SendQuitToLastPane must still be at zero calls.
func TestPasteInjectQuitOnCommit_NoBriefDeliveredBlocksCommitPoll(t *testing.T) {
	// briefDeliveredTimeout = 2s, commitPollTimeout = 5s, killDelay = 30s.
	// Observation window = 100ms << 2s briefTimeout, so the gate is still
	// blocking when we check.
	restore := hk930o3ShortTimeouts(2*time.Second, 5*time.Millisecond, 5*time.Second, 30*time.Second)
	defer restore()

	wtPath, headSHA := hk930o3Worktree(t)
	qs := &hk930o3QuitSender{}
	noChangeCh := make(chan struct{})

	// Land a new commit BEFORE starting the watcher, so HEAD != initialSHA.
	if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0644); err != nil {
		t.Fatal(err)
	}
	hk930o3AddCommit(wtPath)

	// briefDelivered channel that is never closed — gate must stay blocking.
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run in a goroutine so we can observe it non-blockingly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, nil, wtPath, headSHA, noChangeCh, briefDelivered, nil)
	}()

	// Observe for 100ms — during this window, briefDelivered is never closed,
	// so the poll loop must not have started and /quit must not have been sent.
	time.Sleep(100 * time.Millisecond)

	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 (brief not yet delivered, gate blocking), got %d", got)
	}

	// Cancel ctx to unblock the goroutine (via ctx.Done() in the gate select).
	cancel()
	<-done
}

// TestPasteInjectQuitOnCommit_BriefDeliveredGateOpensOnClose verifies that once
// briefDelivered is closed, the commit poll loop proceeds and /quit is sent
// when a new commit is detected.
func TestPasteInjectQuitOnCommit_BriefDeliveredGateOpensOnClose(t *testing.T) {
	// Short poll interval; long commitPollTimeout so noChange path doesn't fire.
	restore := hk930o3ShortTimeouts(5*time.Second, 5*time.Millisecond, 30*time.Second, 30*time.Second)
	defer restore()

	wtPath, headSHA := hk930o3Worktree(t)
	qs := &hk930o3QuitSender{}
	noChangeCh := make(chan struct{})

	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Land a new commit in the background after a short delay.
	go func() {
		time.Sleep(30 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0644); err != nil {
			return
		}
		hk930o3AddCommit(wtPath)
	}()

	// Close briefDelivered after 20ms — before the commit lands.
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(briefDelivered)
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, nil, wtPath, headSHA, noChangeCh, briefDelivered, nil)

	// After the function returns (commit detected + /quit sent), assert.
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (brief delivered, commit detected), got %d", got)
	}
	// noChangeTimeoutCh must NOT be closed (normal commit path, not timeout path).
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed on normal commit path")
	default:
	}
	cancel()
}

// TestPasteInjectQuitOnCommit_BriefDeliveredTimeoutProceedsWithPoll verifies
// that if briefDeliveredTimeout elapses before the channel is closed, the
// commit poll loop starts anyway.
//
// This ensures liveness: a broken session (e.g. paste-inject failed silently)
// must not cause the daemon to hang forever waiting for a brief-delivery signal
// that will never arrive.
func TestPasteInjectQuitOnCommit_BriefDeliveredTimeoutProceedsWithPoll(t *testing.T) {
	// briefDeliveredTimeout = 30ms (short), commitPollTimeout = 150ms,
	// killDelay = 30ms.
	restore := hk930o3ShortTimeouts(30*time.Millisecond, 5*time.Millisecond, 150*time.Millisecond, 30*time.Millisecond)
	defer restore()

	wtPath, headSHA := hk930o3Worktree(t)
	qs := &hk930o3QuitSender{}
	kl := &hk930o3Killer{}
	noChangeCh := make(chan struct{})

	// briefDelivered channel that is NEVER closed — timeout must fire.
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// After the briefDeliveredTimeout (30ms) elapses, the commit poll loop
	// starts.  After commitPollTimeout (300ms) with no commit, /quit + Kill + close
	// noChangeCh fires.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, briefDelivered, nil)

	// noChangeTimeoutCh must be closed (noChange-timeout path executed).
	select {
	case <-noChangeCh:
	default:
		t.Fatal("noChangeTimeoutCh not closed: commit poll did not start after brief-delivered timeout")
	}
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (unconditional on noChange-timeout), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (after noChangeKillDelay), got %d", got)
	}
}
