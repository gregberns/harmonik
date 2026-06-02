package daemon_test

// pasteinject_hktrjef_test.go — unit tests for the noChange-timeout recovery
// path in pasteInjectQuitOnCommit (hk-trjef).
//
// Observed failure (2026-05-20 batch 1, bead hk-2m3bq): Claude detected
// nothing-to-do and self-quit.  commitPollTimeout fired without a new commit,
// but the goroutine only logged and returned.  waitWithSocketGrace then blocked
// on sess.Wait indefinitely because the tmux pane never closed.
//
// Fix: on timeout, (1) send /quit unconditionally, (2) wait noChangeKillDelay,
// (3) call killer.Kill, (4) close noChangeTimeoutCh to signal the workloop.
//
// These tests exercise the four-step path with overridden (short) timeouts.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hktrjefQuitSender records SendQuitToLastPane calls.
type hktrjefQuitSender struct {
	calls atomic.Int64
}

func (q *hktrjefQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hktrjefKiller records Kill calls.
type hktrjefKiller struct {
	calls atomic.Int64
}

func (k *hktrjefKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hktrjefShortTimeouts overrides the three timing vars and returns a restore
// function.  Call defer restore() immediately.  postQuitKillGrace is set to a
// long value (1 hour) so the post-commit watchdog (hk-5s7tg) does not fire
// during tests that only exercise the noChange-timeout path.  Tests that need
// to exercise the post-commit watchdog override it explicitly.
func hktrjefShortTimeouts(poll, timeout, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origTimeout := *daemon.ExportedCommitPollTimeout
	origKillDelay := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = poll
	*daemon.ExportedCommitPollTimeout = timeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origTimeout
		*daemon.ExportedNoChangeKillDelay = origKillDelay
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// hktrjefGit runs a git command in dir and fatals on error.
func hktrjefGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// hktrjefGitOutput runs a git command and returns trimmed stdout.
func hktrjefGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// hktrjefWorktree creates a temp git repo with one initial commit and returns
// the path and the initial HEAD SHA.
func hktrjefWorktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	hktrjefGit(t, dir, "init", "--initial-branch=main")
	hktrjefGit(t, dir, "config", "user.email", "test@test.com")
	hktrjefGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	hktrjefGit(t, dir, "add", ".")
	hktrjefGit(t, dir, "commit", "-m", "init")
	sha := hktrjefGitOutput(t, dir, "rev-parse", "HEAD")
	return dir, sha
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnCommit_TimeoutSendsQuitAndKills verifies that when
// commitPollTimeout fires without a new commit:
//  1. SendQuitToLastPane is called exactly once.
//  2. killer.Kill is called exactly once (after noChangeKillDelay).
//  3. noChangeTimeoutCh is closed.
func TestPasteInjectQuitOnCommit_TimeoutSendsQuitAndKills(t *testing.T) {
	restore := hktrjefShortTimeouts(5*time.Millisecond, 10*time.Millisecond, 20*time.Millisecond)
	defer restore()

	wtPath, headSHA := hktrjefWorktree(t)
	qs := &hktrjefQuitSender{}
	kl := &hktrjefKiller{}
	noChangeCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)

	select {
	case <-noChangeCh:
	default:
		t.Fatal("noChangeTimeoutCh not closed after timeout recovery")
	}
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1, got %d", got)
	}
}

// TestPasteInjectQuitOnCommit_NewCommitNoKill verifies that when a new commit
// lands before commitPollTimeout, SendQuitToLastPane fires and (with a long
// postQuitKillGrace) no Kill fires within the test window.
func TestPasteInjectQuitOnCommit_NewCommitNoKill(t *testing.T) {
	restore := hktrjefShortTimeouts(5*time.Millisecond, 5*time.Second, 30*time.Second)
	defer restore()

	wtPath, headSHA := hktrjefWorktree(t)
	qs := &hktrjefQuitSender{}
	kl := &hktrjefKiller{}
	noChangeCh := make(chan struct{})

	// Advance HEAD after a short delay so the poller detects it.
	// Use background context — t.Context() cancels when the test ends, which
	// would cause the goroutine's git calls to fail after test completion.
	go func() {
		time.Sleep(30 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0644); err != nil {
			return
		}
		bgCtx := context.Background()
		for _, args := range [][]string{
			{"add", "."},
			{"commit", "-m", "work\n\nRefs: hk-test"},
		} {
			cmd := exec.CommandContext(bgCtx, "git", args...)
			cmd.Dir = wtPath
			_ = cmd.Run()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)

	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed on normal commit path")
	default:
	}
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
	// hk-5s7tg: with postQuitKillGrace overridden to 1 hour by
	// hktrjefShortTimeouts, the post-commit watchdog must not fire within
	// the test's 3-second window.
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (postQuitKillGrace=1h is well past test window), got %d", got)
	}
	// Cancel the parent ctx so the watchdog goroutine exits before the test
	// returns and the Kill counter is observed.
	cancel()
	// Give the goroutine a tick to exit cleanly.
	time.Sleep(50 * time.Millisecond)
}

// TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace verifies the
// hk-5s7tg fix: when a commit lands and /quit is sent, but the session has
// not exited within postQuitKillGrace, killer.Kill is called to unblock the
// workloop's sess.Wait.
func TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace(t *testing.T) {
	// Override package vars: short pollInterval + long pollTimeout so the
	// noChange-timeout path does NOT fire; short postQuitKillGrace so the
	// watchdog fires quickly.
	origPoll := *daemon.ExportedCommitPollInterval
	origTimeout := *daemon.ExportedCommitPollTimeout
	origKillDelay := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = 5 * time.Millisecond
	*daemon.ExportedCommitPollTimeout = 30 * time.Second
	*daemon.ExportedNoChangeKillDelay = 30 * time.Second
	*daemon.ExportedPostQuitKillGrace = 50 * time.Millisecond
	defer func() {
		// Sleep first so the watchdog goroutine has exited before we restore.
		time.Sleep(200 * time.Millisecond)
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origTimeout
		*daemon.ExportedNoChangeKillDelay = origKillDelay
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}()

	wtPath, headSHA := hktrjefWorktree(t)
	qs := &hktrjefQuitSender{}
	kl := &hktrjefKiller{}
	noChangeCh := make(chan struct{})

	// Advance HEAD after a short delay so the poller detects it and the
	// post-commit branch is hit (which schedules the watchdog).
	go func() {
		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0644); err != nil {
			return
		}
		bgCtx := context.Background()
		for _, args := range [][]string{
			{"add", "."},
			{"commit", "-m", "work\n\nRefs: hk-5s7tg"},
		} {
			cmd := exec.CommandContext(bgCtx, "git", args...)
			cmd.Dir = wtPath
			_ = cmd.Run()
		}
	}()

	// Use a parent context that outlives postQuitKillGrace so the watchdog
	// can fire before ctx-cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)

	// pasteInjectQuitOnCommit returned after sending /quit and launching the
	// watchdog goroutine.  Wait long enough for the watchdog grace to elapse
	// and Kill to be invoked.
	time.Sleep(150 * time.Millisecond)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls (post-commit watchdog): want 1 after grace elapsed, got %d (hk-5s7tg watchdog did not fire)", got)
	}
	// noChangeTimeoutCh must NOT be closed (the no-progress path did not fire).
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed; only the post-commit watchdog should have fired")
	default:
	}
}

// TestBeadAlreadySubsumedInMain verifies the git-log scan for a "Refs: <id>"
// trailer in main's recent commits.
func TestBeadAlreadySubsumedInMain(t *testing.T) {
	dir := t.TempDir()
	hktrjefGit(t, dir, "init", "--initial-branch=main")
	hktrjefGit(t, dir, "config", "user.email", "test@test.com")
	hktrjefGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	hktrjefGit(t, dir, "add", ".")
	hktrjefGit(t, dir, "commit", "-m", "fix something\n\nRefs: hk-target01\nCo-Authored-By: Test")

	ctx := context.Background()

	if !daemon.ExportedBeadAlreadySubsumedInMain(ctx, dir, core.BeadID("hk-target01")) {
		t.Error("expected bead hk-target01 to be found in main commits")
	}
	if daemon.ExportedBeadAlreadySubsumedInMain(ctx, dir, core.BeadID("hk-absent01")) {
		t.Error("expected bead hk-absent01 to NOT be found in main commits")
	}
}

// TestBeadAlreadySubsumedInMainPrefixRegression is a regression test for
// hk-kizwo: a Refs trailer "hk-tigaf.10" must NOT satisfy a query for
// "hk-tigaf.1" (which is a strict prefix of the longer ID).
func TestBeadAlreadySubsumedInMainPrefixRegression(t *testing.T) {
	dir := t.TempDir()
	hktrjefGit(t, dir, "init", "--initial-branch=main")
	hktrjefGit(t, dir, "config", "user.email", "test@test.com")
	hktrjefGit(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	hktrjefGit(t, dir, "add", ".")
	// Commit carries hk-tigaf.10, NOT hk-tigaf.1.
	hktrjefGit(t, dir, "commit", "-m", "fix something\n\nRefs: hk-tigaf.10\nCo-Authored-By: Test")

	ctx := context.Background()

	if !daemon.ExportedBeadAlreadySubsumedInMain(ctx, dir, core.BeadID("hk-tigaf.10")) {
		t.Error("expected hk-tigaf.10 to be found in main commits")
	}
	// Bug: old strings.Contains returned true here because "Refs: hk-tigaf.1"
	// is a substring of "Refs: hk-tigaf.10".
	if daemon.ExportedBeadAlreadySubsumedInMain(ctx, dir, core.BeadID("hk-tigaf.1")) {
		t.Error("hk-tigaf.1 must NOT match a commit that only carries 'Refs: hk-tigaf.10'")
	}
}
