package daemon_test

// pasteinject_hk76n5g_test.go — regression tests for the seed-submission
// reliability fixes (hk-76n5g).
//
// # Bugs addressed
//
// Two production incidents on 2026-06-10:
//
//  1. crew-start reseed: mission seed sat unsubmitted in the input bar; operator
//     had to press Enter manually to boot the crew member.
//
//  2. review-loop iter-2 implementer: the combined task+feedback brief was pasted
//     into the resumed pane but the post-paste submit Enter was dropped, wedging
//     the bead for ~37 min.
//
// Both incidents share a root cause: the TUI was still absorbing the bracketed-
// paste content when the submit Enter fired; the Enter raced the paste and was
// swallowed by the absorbing input handler.  The existing retry (hk-ip33d: 3
// attempts over ~800 ms) did not help because all attempts fell within the paste-
// absorption window.
//
// # Fixes
//
//  - pasteInjectImplementerResume and pasteInjectImplementerInitial: add a
//    splashDismissWait AFTER WriteLastPane (before the retry Enters) so the REPL
//    finishes absorbing the paste before any submit keypress arrives.
//
//  - pasteInjectQuitOnCommit: add a one-shot reseed-Enter after
//    implementerReseedGrace (75 s) as a safety net. When all prior Enters were
//    dropped, this Enter submits the pending input and normal flow resumes
//    minutes before the 30-min commitPollTimeout would have killed the run.
//
// # What this file tests
//
// TestPasteInjectQuitOnCommit_ReseedEnterSubmitsPendingInput:
//
//   Verifies that pasteInjectQuitOnCommit fires the reseed-Enter after
//   implementerReseedGrace when no commit has appeared, and that detecting the
//   subsequent commit causes the function to return normally (SendQuitToLastPane
//   called, no noChange kill).
//
//   The stub quitSender also implements enterSender; when SendEnterToLastPane is
//   called (the reseed), it writes a git commit to the worktree synchronously,
//   simulating a resumed implementer that acts as soon as its pending seed is
//   submitted.  The HEAD check on the same tick then detects the commit and the
//   function returns without ever reaching the noChange kill path.
//
// TestPasteInjectQuitOnCommit_ReseedEnterSkippedWhenNoEnterSender:
//
//   Verifies that the reseed-Enter is a no-op when qs does not implement
//   enterSender (i.e. the pasteInjectQuitOnCommit signature is unchanged and the
//   mechanism is safely absent for callers that pass a plain quitSender).
//
// NOTE: tests in this file do NOT call t.Parallel() because they modify
// package-level timing vars via Exported* pointers.  Parallel execution would
// cause a race between tests sharing the same globals.
//
// Bead: hk-76n5g.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk76n5gWorktree creates a minimal git worktree (one commit) and returns the
// path and initial HEAD SHA.
func hk76n5gWorktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	hk76n5gGit(t, dir, "init", "--initial-branch=main")
	hk76n5gGit(t, dir, "config", "user.email", "test@harmonik.local")
	hk76n5gGit(t, dir, "config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	hk76n5gGit(t, dir, "add", ".")
	hk76n5gGit(t, dir, "commit", "-m", "init", "--no-gpg-sign")
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	sha := string(out)
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	return dir, sha
}

func hk76n5gGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	//nolint:gosec // G204: test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), "git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// hk76n5gGitCommit writes a file and commits it synchronously in dir.
// Called from SendEnterToLastPane stubs to simulate an implementer committing
// after the reseed-Enter submits its pending prompt.
func hk76n5gGitCommit(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("done"), 0o600)
	//nolint:gosec // G204: test-internal literals
	_ = exec.CommandContext(context.Background(), "git", "-C", dir, "add", "impl.txt").Run()
	//nolint:gosec // G204: test-internal literals
	_ = exec.CommandContext(context.Background(), "git", "-C", dir,
		"-c", "user.email=test@harmonik.local",
		"-c", "user.name=Harmonik Test",
		"commit", "-m", "impl", "--no-gpg-sign").Run()
}

// hk76n5gShortTimeouts overrides pasteInjectQuitOnCommit timing vars for fast
// test execution.  Returns a restore function; call defer restore() immediately.
//
// NOTE: do NOT call t.Parallel() on tests that use this helper — the globals
// are shared and would race under concurrent test goroutines.
func hk76n5gShortTimeouts(reseedGrace, pollInterval, totalTimeout, hardCeiling, killDelay time.Duration) func() {
	origReseed := *daemon.ExportedImplementerReseedGrace
	origPoll := *daemon.ExportedCommitPollInterval
	origTotal := *daemon.ExportedCommitPollTimeout
	origHard := *daemon.ExportedCommitHardCeiling
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origSplash := daemon.ExportedSplashDismissDelay()
	*daemon.ExportedImplementerReseedGrace = reseedGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedCommitHardCeiling = hardCeiling
	*daemon.ExportedNoChangeKillDelay = killDelay
	// Prevent the post-commit /quit watchdog goroutine from interfering.
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	// Suppress the splash-dismiss wait so paste-inject helpers in the review-
	// loop scenario complete quickly when splashDismissWait is called.
	daemon.ExportedSetSplashDismissDelay(1 * time.Millisecond)
	return func() {
		*daemon.ExportedImplementerReseedGrace = origReseed
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedCommitHardCeiling = origHard
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		daemon.ExportedSetSplashDismissDelay(origSplash)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk76n5gQuitAndEnterSender is a stub that implements both quitSender and
// enterSender. When SendEnterToLastPane is called (the reseed-Enter), it writes
// a git commit synchronously to wtPath so pasteInjectQuitOnCommit can detect it
// in the HEAD check on the same ticker tick.
type hk76n5gQuitAndEnterSender struct {
	wtPath     string
	quitCalls  atomic.Int64
	enterCalls atomic.Int64
}

func (s *hk76n5gQuitAndEnterSender) SendQuitToLastPane(_ context.Context) error {
	s.quitCalls.Add(1)
	return nil
}

func (s *hk76n5gQuitAndEnterSender) SendEnterToLastPane(_ context.Context) error {
	s.enterCalls.Add(1)
	hk76n5gGitCommit(s.wtPath) // synchronous: commit lands before this returns
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnCommit_ReseedEnterSubmitsPendingInput verifies the
// hk-76n5g safety-net mechanism:
//
//   - After implementerReseedGrace with no new commit, pasteInjectQuitOnCommit
//     sends a one-shot Enter via the quitSender's enterSender capability.
//
//   - The Enter causes a synchronous git commit in the worktree (simulating the
//     pending seed being submitted and the implementer committing in response).
//
//   - On the same ticker tick, pasteInjectQuitOnCommit detects the new HEAD,
//     calls SendQuitToLastPane, and returns normally (no noChange kill).
//
// Timing: reseedGrace=100ms, pollInterval=5ms, totalTimeout=5s → the function
// should return in well under 1s; the 5s deadline is a large safety margin.
func TestPasteInjectQuitOnCommit_ReseedEnterSubmitsPendingInput(t *testing.T) {
	// Sequential: modifies package-level timing globals — do NOT call t.Parallel().

	restore := hk76n5gShortTimeouts(
		100*time.Millisecond, // reseedGrace
		5*time.Millisecond,   // pollInterval
		5*time.Second,        // totalTimeout (large gap above reseedGrace)
		1*time.Hour,          // hardCeiling (don't fire)
		5*time.Second,        // killDelay (must not fire in test window)
	)
	defer restore()

	wtPath, headSHA := hk76n5gWorktree(t)
	sender := &hk76n5gQuitAndEnterSender{wtPath: wtPath}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Pass sender as quitSender; the production type assertion inside
		// pasteInjectQuitOnCommit discovers it also satisfies enterSender,
		// enabling the reseed-Enter path.
		daemon.ExportedPasteInjectQuitOnCommit(ctx, sender, nil, wtPath, headSHA, nil, nil, nil)
	}()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("hk-76n5g: pasteInjectQuitOnCommit did not return after reseed-Enter triggered a commit (timed out after 8s)")
	}

	// The reseed-Enter must have fired exactly once.
	if got := sender.enterCalls.Load(); got == 0 {
		t.Errorf("hk-76n5g REGRESSION: reseed Enter was never sent (enterCalls=%d); "+
			"mechanism did not fire after implementerReseedGrace", got)
	}

	// After detecting the commit, /quit must have been sent.
	if got := sender.quitCalls.Load(); got == 0 {
		t.Errorf("hk-76n5g REGRESSION: SendQuitToLastPane was never called (quitCalls=%d); "+
			"commit was not detected after the reseed-Enter", got)
	}
}

// TestPasteInjectQuitOnCommit_ReseedEnterSkippedWhenNoEnterSender verifies that
// the reseed-Enter mechanism is safely absent when qs does not implement
// enterSender. The function must still kill the session via the normal noChange
// path (totalTimeout) without panicking or hanging.
//
// Uses a plain hk7srrdQuitSender (quitSender only, no enterSender). Sets a
// short totalTimeout so the noChange kill fires quickly.
func TestPasteInjectQuitOnCommit_ReseedEnterSkippedWhenNoEnterSender(t *testing.T) {
	// Sequential: modifies package-level timing globals — do NOT call t.Parallel().

	restore := hk76n5gShortTimeouts(
		50*time.Millisecond,  // reseedGrace (elapses, but reseed is disabled — no enterSender)
		5*time.Millisecond,   // pollInterval
		150*time.Millisecond, // totalTimeout: short so noChange kill fires fast
		1*time.Hour,          // hardCeiling (don't fire)
		10*time.Millisecond,  // killDelay
	)
	defer restore()

	wtPath, headSHA := hk76n5gWorktree(t)
	// Plain quitSender — does NOT implement enterSender.
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("hk-76n5g: pasteInjectQuitOnCommit did not return via noChange path (timed out after 5s)")
	}

	// noChange kill must have fired (totalTimeout elapsed without commit).
	if got := kl.calls.Load(); got == 0 {
		t.Errorf("hk-76n5g: Kill was not called; noChange path did not fire (killCalls=%d)", got)
	}

	// noChangeTimeoutCh must be closed.
	select {
	case <-noChangeCh:
	default:
		t.Error("hk-76n5g: noChangeTimeoutCh was not closed after noChange kill")
	}
}
