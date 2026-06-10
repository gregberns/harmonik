package daemon_test

// pasteinject_hk3gq0b_test.go — unit tests for the launch-verification window
// in pasteInjectQuitOnCommit (hk-3gq0b).
//
// Problem addressed: when the paste-inject message is written to a dead or
// empty tmux pane, Claude Code never starts — no heartbeats, no commits, and
// the session sits idle until the 8-minute heartbeat-staleness threshold fires.
// hk-3gq0b adds a shorter "launch window" (60s in production): if no
// agent_heartbeat arrives within that window after brief delivery, the session
// is killed immediately so the workloop reopens the bead for retry.
//
// Test matrix:
//   - NoFirstHeartbeat_KillsAfterLaunchWindow: no heartbeats arrive → kill
//     fires after launchHeartbeatTimeout and noChangeTimeoutCh is closed.
//   - HeartbeatArrivesInWindow_NoKill: heartbeat arrives before the launch
//     window expires → launch kill must NOT fire during the window.
//   - CommitBeforeLaunchWindow_NoKill: commit lands before launch timeout →
//     normal /quit-on-commit path taken, kill path NOT triggered.
//   - NilEventCh_LaunchWindowSkipped: nil eventCh → launch window not active,
//     function falls back to wall-clock commitPollTimeout.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk3gq0bShortTimeouts overrides timing package vars for launch-verification
// tests.  Returns a restore function; call defer restore() immediately.
//
//   - pollInterval   — git HEAD check cadence
//   - launchWindow   — launchHeartbeatTimeout
//   - staleness      — heartbeatStalenessThreshold (kept large so it never fires first)
//   - totalTimeout   — commitPollTimeout (kept large so it never fires first)
//   - killDelay      — noChangeKillDelay
//
// postQuitKillGrace is always set to 1 h so the post-commit watchdog never
// fires during these tests.
func hk3gq0bShortTimeouts(pollInterval, launchWindow, staleness, totalTimeout, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origLaunch := *daemon.ExportedLaunchHeartbeatTimeout
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origTotal := *daemon.ExportedCommitPollTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedLaunchHeartbeatTimeout = launchWindow
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunch
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// hk3gq0bWorktree creates a temp git repo with one commit and returns
// the path and initial HEAD SHA.
func hk3gq0bWorktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	hk3gq0bGit(t, dir, "init", "--initial-branch=main")
	hk3gq0bGit(t, dir, "config", "user.email", "test@test.com")
	hk3gq0bGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	hk3gq0bGit(t, dir, "add", ".")
	hk3gq0bGit(t, dir, "commit", "-m", "init")
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	sha := string(out)
	if len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	return dir, sha
}

func hk3gq0bGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(t.Context(), "git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func hk3gq0bGitBg(dir string, args ...string) {
	allArgs := append([]string{"-C", dir}, args...)
	_ = exec.CommandContext(context.Background(), "git", allArgs...).Run()
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectLaunchVerification_NoFirstHeartbeat_KillsAfterLaunchWindow
// verifies that when no heartbeats arrive after brief delivery, the kill path
// fires after launchHeartbeatTimeout and noChangeTimeoutCh is closed.
func TestPasteInjectLaunchVerification_NoFirstHeartbeat_KillsAfterLaunchWindow(t *testing.T) {
	restore := hk3gq0bShortTimeouts(
		5*time.Millisecond,  // poll interval
		60*time.Millisecond, // launch window
		5*time.Second,       // staleness (must not fire first)
		30*time.Second,      // total timeout (must not fire first)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk3gq0bWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	// eventCh provided but no heartbeats sent.
	eventCh := make(chan core.EventEnvelope, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	select {
	case <-noChangeCh:
		// expected: launch-verification kill closed the channel
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after launch-heartbeat-timeout")
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (launch kill), got %d", got)
	}
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
}

// TestPasteInjectLaunchVerification_HeartbeatArrivesInWindow_NoKill verifies
// that when the first heartbeat arrives before launchHeartbeatTimeout, the
// launch kill path does NOT fire during the window.
func TestPasteInjectLaunchVerification_HeartbeatArrivesInWindow_NoKill(t *testing.T) {
	restore := hk3gq0bShortTimeouts(
		5*time.Millisecond,   // poll interval
		150*time.Millisecond, // launch window
		3*time.Second,        // staleness (must not fire during test)
		30*time.Second,       // total timeout
		30*time.Second,       // kill delay (must not complete during test)
	)
	defer restore()

	wtPath, headSHA := hk3gq0bWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send one heartbeat well before the 150ms launch window expires.
	go func() {
		time.Sleep(30 * time.Millisecond)
		select {
		case eventCh <- core.EventEnvelope{Type: string(core.EventTypeAgentHeartbeat)}:
		default:
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	}()

	// Wait past the launch window (150ms) + buffer, then cancel.
	time.Sleep(220 * time.Millisecond)
	cancel()
	<-done

	// Kill must NOT have fired — heartbeat arrived in time.
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (heartbeat arrived before launch window), got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed; heartbeat should have prevented launch kill")
	default:
	}
}

// TestPasteInjectLaunchVerification_CommitBeforeLaunchWindow_NoKill verifies
// that when a commit lands before the launch window expires, the normal
// /quit-on-commit path is taken and the launch kill is NOT triggered.
func TestPasteInjectLaunchVerification_CommitBeforeLaunchWindow_NoKill(t *testing.T) {
	restore := hk3gq0bShortTimeouts(
		5*time.Millisecond,   // poll interval
		200*time.Millisecond, // launch window (commit lands before this)
		5*time.Second,        // staleness
		30*time.Second,       // total timeout
		30*time.Second,       // kill delay
	)
	defer restore()

	wtPath, headSHA := hk3gq0bWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Advance HEAD after 40 ms — well before the 200ms launch window.
	go func() {
		time.Sleep(40 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0o600); err != nil {
			return
		}
		hk3gq0bGitBg(wtPath, "add", ".")
		hk3gq0bGitBg(wtPath, "commit", "-m", "work\n\nRefs: hk-3gq0b")
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	// /quit must have fired (commit path).
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1 (commit path), got %d", got)
	}
	// Launch kill must NOT have fired (postQuitKillGrace = 1h).
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (commit path, postQuitKillGrace = 1h), got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed on normal commit path")
	default:
	}

	cancel()
	time.Sleep(20 * time.Millisecond)
}

// TestPasteInjectLaunchVerification_NilEventCh_LaunchWindowSkipped verifies
// that when eventCh is nil, the launch-verification window is inactive and
// the function falls back to the wall-clock commitPollTimeout.  The elapsed
// time must be at least the total timeout (not the shorter launch window),
// confirming the launch check was skipped.
func TestPasteInjectLaunchVerification_NilEventCh_LaunchWindowSkipped(t *testing.T) {
	restore := hk3gq0bShortTimeouts(
		5*time.Millisecond,  // poll interval
		10*time.Millisecond, // launch window (would fire instantly if active)
		5*time.Second,       // staleness (irrelevant — nil eventCh)
		40*time.Millisecond, // total wall-clock timeout (governs instead)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk3gq0bWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// nil eventCh — launch verification skipped; wall-clock governs.
	start := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)
	elapsed := time.Since(start)

	// noChangeTimeoutCh must be closed (wall-clock fired).
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after wall-clock timeout (nil eventCh)")
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1, got %d", got)
	}

	// Should have taken at least the total timeout (40ms) to fire, not the
	// shorter launch window (10ms) — confirms launch window was NOT active.
	if elapsed < 35*time.Millisecond {
		t.Errorf("elapsed %v < 35ms; launch window may have fired instead of wall-clock timeout", elapsed)
	}
}
