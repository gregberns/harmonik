package daemon_test

// pasteinject_hk7srrd_test.go — unit tests for the heartbeat-staleness kill
// path in pasteInjectQuitOnCommit (hk-7srrd).
//
// Problem addressed: the 10-minute wall-clock kill fired regardless of whether
// the implementer was making progress, destroying productive work on non-trivial
// beads.  The fix replaces the hard wall-clock deadline with a sliding heartbeat-
// staleness check: the kill only fires when heartbeatStalenessThreshold elapses
// without an agent_heartbeat event arriving on the event channel.  A separate
// total-wall-clock backstop (commitPollTimeout, now 30 min) prevents infinite
// hangs when the event source itself dies.
//
// Test matrix:
//   - HeartbeatKeepsSessionAlive: heartbeats arriving before staleness threshold
//     prevent the kill from firing.
//   - StalenessKillsSession: when heartbeats stop, kill fires after threshold.
//   - StalenessClosesNoChangeCh: noChangeTimeoutCh is closed on stale kill.
//   - NilEventChWallClockBackstop: nil eventCh falls back to wall-clock timeout.
//   - NewCommitBeforeStaleness: commit landing before threshold takes normal path.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk7srrdShortTimeouts overrides timing package vars for heartbeat-staleness
// tests.  Returns a restore function; call defer restore() immediately.
//
//   - pollInterval  — git HEAD check cadence
//   - staleness     — heartbeatStalenessThreshold
//   - totalTimeout  — commitPollTimeout (safety backstop)
//   - killDelay     — noChangeKillDelay
//
// postQuitKillGrace is always set to 1 h so the post-commit watchdog never
// fires during these tests.
func hk7srrdShortTimeouts(pollInterval, staleness, totalTimeout, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origTotal := *daemon.ExportedCommitPollTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// hk7srrdHeartbeatEnv builds a minimal core.EventEnvelope with type
// agent_heartbeat, sufficient for pasteInjectQuitOnCommit to recognise it.
func hk7srrdHeartbeatEnv() core.EventEnvelope {
	return core.EventEnvelope{Type: string(core.EventTypeAgentHeartbeat)}
}

// hk7srrdWorktree creates a temp git repo with one commit and returns
// the path and initial HEAD SHA.
func hk7srrdWorktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	hk7srrdGit(t, dir, "init", "--initial-branch=main")
	hk7srrdGit(t, dir, "config", "user.email", "test@test.com")
	hk7srrdGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0600); err != nil {
		t.Fatal(err)
	}
	hk7srrdGit(t, dir, "add", ".")
	hk7srrdGit(t, dir, "commit", "-m", "init")
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	// trim trailing newline
	sha := string(out)
	if len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	return dir, sha
}

// hk7srrdGit runs a git command in dir and fatals on error.
func hk7srrdGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(t.Context(), "git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// hk7srrdGitBg runs a git command in dir with a background context.
// Used from goroutines that may outlive t.Context().
func hk7srrdGitBg(dir string, args ...string) {
	allArgs := append([]string{"-C", dir}, args...)
	_ = exec.CommandContext(context.Background(), "git", allArgs...).Run()
}

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk7srrdQuitSender records SendQuitToLastPane calls.
type hk7srrdQuitSender struct {
	calls atomic.Int64
}

func (q *hk7srrdQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hk7srrdKiller records Kill calls.
type hk7srrdKiller struct {
	calls atomic.Int64
}

func (k *hk7srrdKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnCommit_HeartbeatKeepsSessionAlive verifies that when
// heartbeats arrive at intervals shorter than heartbeatStalenessThreshold, the
// kill path does NOT fire within the test window even if the old wall-clock
// commitPollTimeout would have expired.
//
// Thresholds: staleness = 80 ms; heartbeats every 15 ms.
// Within 150 ms staleness never exceeds 80 ms → kill must NOT fire.
func TestPasteInjectQuitOnCommit_HeartbeatKeepsSessionAlive(t *testing.T) {
	restore := hk7srrdShortTimeouts(
		5*time.Millisecond,  // poll interval
		80*time.Millisecond, // staleness threshold
		2*time.Second,       // total wall-clock backstop (well past test window)
		30*time.Second,      // kill delay (must not fire in test window)
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Pump heartbeats every 15 ms.
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case eventCh <- hk7srrdHeartbeatEnv():
				default:
				}
			}
		}
	}()

	// Run in a goroutine; cancel ctx after 150 ms to exit via ctx.Done().
	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (heartbeats kept session alive), got %d", got)
	}
	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane calls: want 0, got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed; heartbeats should have prevented kill")
	default:
	}
}

// TestPasteInjectQuitOnCommit_StalenessKillsSession verifies that when
// heartbeats stop arriving, the kill path fires after heartbeatStalenessThreshold
// has elapsed since the last heartbeat.
//
// Thresholds: staleness = 60 ms; kill delay = 20 ms.
// Heartbeats sent for 20 ms then stopped.
// Kill should fire ~60 ms after last heartbeat.
func TestPasteInjectQuitOnCommit_StalenessKillsSession(t *testing.T) {
	restore := hk7srrdShortTimeouts(
		5*time.Millisecond,  // poll interval
		60*time.Millisecond, // staleness threshold
		5*time.Second,       // total wall-clock backstop
		20*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Send heartbeats for 20 ms, then stop.
	go func() {
		ticker := time.NewTicker(8 * time.Millisecond)
		defer ticker.Stop()
		stop := time.After(20 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				select {
				case eventCh <- hk7srrdHeartbeatEnv():
				default:
				}
			}
		}
	}()

	// Runs synchronously; returns only after kill fires.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (stale heartbeat), got %d", got)
	}
	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
}

// TestPasteInjectQuitOnCommit_StalenessClosesNoChangeCh verifies that the
// staleness kill path closes noChangeTimeoutCh so the workloop detects noChange.
func TestPasteInjectQuitOnCommit_StalenessClosesNoChangeCh(t *testing.T) {
	restore := hk7srrdShortTimeouts(
		5*time.Millisecond,  // poll interval
		40*time.Millisecond, // staleness threshold
		5*time.Second,       // total wall-clock backstop
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})

	// No heartbeats — staleness fires immediately after threshold elapses.
	eventCh := make(chan core.EventEnvelope, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	select {
	case <-noChangeCh:
		// expected — staleness path closed the channel
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after staleness kill")
	}
}

// TestPasteInjectQuitOnCommit_NilEventChWallClockBackstop verifies that when
// eventCh is nil, the function uses the commitPollTimeout wall-clock backstop
// as the sole kill trigger (identical to the pre-hk-7srrd behaviour).
func TestPasteInjectQuitOnCommit_NilEventChWallClockBackstop(t *testing.T) {
	restore := hk7srrdShortTimeouts(
		5*time.Millisecond,  // poll interval
		8*time.Minute,       // staleness (irrelevant — nil eventCh skips it)
		15*time.Millisecond, // total wall-clock timeout
		20*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// nil eventCh — wall-clock backstop governs.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil)

	select {
	case <-noChangeCh:
		// expected — wall-clock timeout fired
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after wall-clock timeout (nil eventCh)")
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (wall-clock timeout), got %d", got)
	}
}

// TestPasteInjectQuitOnCommit_NewCommitBeforeStaleness verifies that when a
// commit lands before the staleness threshold is reached, /quit fires and the
// function returns normally — the kill path is NOT taken.
func TestPasteInjectQuitOnCommit_NewCommitBeforeStaleness(t *testing.T) {
	restore := hk7srrdShortTimeouts(
		5*time.Millisecond,   // poll interval
		500*time.Millisecond, // staleness threshold (must outlast the commit window)
		30*time.Second,       // total wall-clock backstop
		30*time.Second,       // kill delay (long — must not fire in test window)
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Advance HEAD after 20 ms — well before the 500 ms staleness threshold.
	go func() {
		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("done"), 0600); err != nil {
			return
		}
		hk7srrdGitBg(wtPath, "add", ".")
		hk7srrdGitBg(wtPath, "commit", "-m", "work\n\nRefs: hk-7srrd")
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1 (commit path), got %d", got)
	}
	// Kill must NOT have fired (postQuitKillGrace = 1 h per hk7srrdShortTimeouts).
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (commit path, kill grace = 1h), got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh unexpectedly closed on normal commit path")
	default:
	}

	// Cancel so the post-quit watchdog goroutine (hk-5s7tg) can exit.
	cancel()
	time.Sleep(20 * time.Millisecond)
}
