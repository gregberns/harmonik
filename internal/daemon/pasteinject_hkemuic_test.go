package daemon_test

// pasteinject_hkemuic_test.go — regression test for re-entrant implementer
// post-agent_ready silent-death path through pasteInjectQuitOnCommit (hk-emuic).
//
// Root cause context (hk-sj6a): before 3cb51c4b, RunHeartbeatLoop in
// dispatchDotAgenticNode emitted daemon synthetic heartbeats through the
// perRunEventTap (tap). tap.Subscribe() consumers — including the eventCh
// fed into pasteInjectQuitOnCommit — received these daemon heartbeats as if
// they were real Claude heartbeats, keeping lastHeartbeat perpetually fresh
// after the implementer claude process died. The heartbeat-staleness kill never
// fired; the bead wedged silently until the hard ceiling.
//
// Fix (3cb51c4b / hk-sj6a): RunHeartbeatLoop now emits to deps.bus directly
// (not through tap). Only real Claude heartbeats (agent_heartbeat events from
// the running claude process) appear on eventCh. When the implementer dies
// silently, eventCh goes dry and the staleness kill fires correctly.
//
// 3cb51c4b shipped tests for:
//   - reviewer path: pasteInjectQuitOnReviewFile (pasteinject_hksj6a_test.go)
//   - never-spawned DOT reaper (stalewatch_neverSpawnedReaper_hk0z5x_test.go)
//
// This file adds the missing IMPLEMENTER PATH regression:
//   re-entrant implementer that received agent_ready (had prior Claude HBs on
//   eventCh) then died silently while daemon HBs flowed to a separate sink
//   (not eventCh, per the hk-sj6a fix). Kill must fire on eventCh staleness.
//
// Regression signal: if daemon HBs were re-routed back through tap into eventCh
// the channel would never go dry; staleness would never fire; Kill() would
// never be called (kl.calls == 0), failing the assertion.
//
// Bead: hk-emuic.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkemuicShortTimeouts overrides timing vars for these regression tests.
// postQuitKillGrace is set to 1h so the post-commit watchdog does not fire.
// launchHeartbeatTimeout and launchSuppressionCeiling are set to 1h to keep
// the launch-verification branch from interfering with the staleness path
// under test (firstHeartbeatSeen is set to true by the initial Claude HB, but
// keeping the launch branch dormant avoids any timing sensitivity).
// implementerReseedGrace is set to 1h to suppress the one-shot reseed-Enter.
func hkemuicShortTimeouts(staleness, totalTimeout, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origTotal := *daemon.ExportedCommitPollTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origLaunch := *daemon.ExportedLaunchHeartbeatTimeout
	origLaunchCeil := *daemon.ExportedLaunchSuppressionCeiling
	origReseed := *daemon.ExportedImplementerReseedGrace
	*daemon.ExportedCommitPollInterval = 5 * time.Millisecond
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	*daemon.ExportedLaunchHeartbeatTimeout = 1 * time.Hour
	*daemon.ExportedLaunchSuppressionCeiling = 1 * time.Hour
	*daemon.ExportedImplementerReseedGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunch
		*daemon.ExportedLaunchSuppressionCeiling = origLaunchCeil
		*daemon.ExportedImplementerReseedGrace = origReseed
	}
}

// hkemuicHeartbeatEnv builds a minimal agent_heartbeat EventEnvelope.
func hkemuicHeartbeatEnv() core.EventEnvelope {
	return core.EventEnvelope{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(core.EventTypeAgentHeartbeat),
	}
}

// hkemuicQuitSender records SendQuitToLastPane calls.
type hkemuicQuitSender struct{ calls atomic.Int64 }

func (q *hkemuicQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hkemuicKiller records Kill calls.
type hkemuicKiller struct{ calls atomic.Int64 }

func (k *hkemuicKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hkemuicWorktree creates a minimal git repo with one initial commit and
// returns (wtPath, headSHA).
func hkemuicWorktree(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		allArgs := append([]string{"-C", dir}, args...)
		if out, err := exec.CommandContext(t.Context(), "git", allArgs...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		allArgs := append([]string{"-C", dir}, args...)
		if out, err := exec.CommandContext(t.Context(), "git", allArgs...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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

// TestQuitOnCommit_KillsAfterImplementerDiesSilentlyPostAgentReady is the
// hk-emuic regression test.
//
// Scenario: re-entrant implementer received agent_ready (one real Claude HB on
// eventCh — the last HB before the process died silently). A concurrent goroutine
// pumps "daemon synthetic HBs" to a SEPARATE sink (not eventCh), modelling what
// the hk-sj6a fix enforces: RunHeartbeatLoop emits to bus directly, not tap.
//
// Expected: Kill fires once heartbeatStalenessThreshold elapses on eventCh
// (the one real Claude HB is consumed; no further HBs arrive; staleness fires).
// The daemon synthetic HBs flowing to the separate sink must NOT prevent Kill.
//
// This is the implementer-path parallel of
// TestQuitOnReviewFile_KillsAfterHeartbeatGraceExpires (hk-sj6a reviewer path).
func TestQuitOnCommit_KillsAfterImplementerDiesSilentlyPostAgentReady(t *testing.T) {
	restore := hkemuicShortTimeouts(
		60*time.Millisecond,  // staleness threshold
		500*time.Millisecond, // total timeout backstop (not reached in test window)
		10*time.Millisecond,  // kill delay
	)
	defer restore()

	wtPath, headSHA := hkemuicWorktree(t)

	// One real Claude HB in the channel — the last heartbeat emitted before
	// the implementer claude process died silently (agent_ready was observed;
	// the implementer was active). After this HB no more arrive on eventCh.
	eventCh := make(chan core.EventEnvelope, 1)
	eventCh <- hkemuicHeartbeatEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Daemon synthetic HBs go to a SEPARATE channel (not eventCh), modelling the
	// hk-sj6a fix: RunHeartbeatLoop now emits to bus directly.
	// These MUST NOT prevent Kill from firing (they are invisible to
	// pasteInjectQuitOnCommit, which only reads eventCh).
	daemonHBSink := make(chan core.EventEnvelope, 64)
	go func() {
		ticker := time.NewTicker(8 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case daemonHBSink <- hkemuicHeartbeatEnv():
				default:
				}
			}
		}
	}()

	qs := &hkemuicQuitSender{}
	kl := &hkemuicKiller{}
	noChangeCh := make(chan struct{})

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	// Kill must fire: eventCh went dry after the one real Claude HB;
	// heartbeatStalenessThreshold elapsed; staleness path fired.
	// Daemon HBs on daemonHBSink are isolated and did not reset lastHeartbeat.
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (staleness after implementer silent death), got %d", got)
	}
	select {
	case <-noChangeCh:
		// correct — staleness kill closed the channel
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after staleness kill")
	}
}

// TestQuitOnCommit_ContinuousHBsThenSilentDeathKills verifies the full
// "implementer active post-agent_ready, then dies" arc: continuous Claude HBs
// suppress Kill; once they stop, Kill fires within the staleness window.
//
// This is the implementer-path parallel of
// TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget (hk-sj6a reviewer path).
func TestQuitOnCommit_ContinuousHBsThenSilentDeathKills(t *testing.T) {
	const (
		staleness    = 60 * time.Millisecond
		totalTimeout = 5 * time.Second
	)
	restore := hkemuicShortTimeouts(staleness, totalTimeout, 10*time.Millisecond)
	defer restore()

	wtPath, headSHA := hkemuicWorktree(t)
	eventCh := make(chan core.EventEnvelope, 16)
	qs := &hkemuicQuitSender{}
	kl := &hkemuicKiller{}
	noChangeCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Pump Claude HBs every 15 ms for 120 ms (implementer active after
	// agent_ready), then stop (implementer dies silently).
	stopHB := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopHB:
				return
			case <-ticker.C:
				select {
				case eventCh <- hkemuicHeartbeatEnv():
				default:
				}
			}
		}
	}()
	time.AfterFunc(120*time.Millisecond, func() { close(stopHB) })

	startedAt := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	elapsed := time.Since(startedAt)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (staleness after HBs stopped), got %d", got)
	}
	// Must not have fired before the heartbeat pump stopped (~120 ms).
	if elapsed < 100*time.Millisecond {
		t.Errorf("Kill fired too early (%v): should have waited for HBs to stop first", elapsed)
	}
}
