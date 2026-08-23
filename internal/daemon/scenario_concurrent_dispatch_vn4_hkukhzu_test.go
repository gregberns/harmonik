//go:build scenario

package daemon_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
)

func vn4BootForTesting(t *testing.T) func(ctx context.Context, cfg daemon.Config) <-chan error {
	t.Helper()
	mergeQ := mergeq.New(nil)
	mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
	mergeQ.Start(mergeQCtx)
	t.Cleanup(mergeQCancel)
	return func(ctx context.Context, cfg daemon.Config) <-chan error {
		done := make(chan error, 1)
		go func() {
			done <- daemon.StartForTesting(ctx, cfg,
				daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
				daemon.WithMergeQueue(mergeQ),
			)
		}()
		return done
	}
}

// TestScenario_ConcurrentDispatch_VN4_AllReachMerge is the flagship regression
// guard: N>=3 distinct beads dispatched concurrently through the real spawn/
// heartbeat/merge path all reach run_completed + merge + close, the cap is
// honored, and there is no terminal run_stale / launch_stall wedge.
//
// It uses single-happy-path (which emits the full agent lifecycle but makes no
// commit; the empty-commit worktree factory provides the HEAD advance that
// satisfies the no-commit guard — the hkumemp determinism recipe). N>=3 of these
// running concurrently exercises real concurrent spawn timing through the work
// loop, merge mutex, and review loop.
//
// Bead: hk-ukhzu.
func TestScenario_ConcurrentDispatch_VN4_AllReachMerge(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N: 3,
		// commit-on-cue-startup-delay, not single-happy-path: dot checks HEAD
		// advance per node, and single-happy-path leans on the fixture's
		// pre-committed empty commit rather than landing one of its own.
		TwinScenario:      "commit-on-cue-startup-delay",
		Boot:              vn4BootForTesting(t),
		ExpectAllComplete: true,
		AgentReadyTimeout: 5 * time.Second,
		BeadPrefix:        "vn4",
	})

	if res.Completed < len(res.BeadIDs) {
		t.Errorf("VN4: only %d/%d runs completed (concurrent-dispatch wedge signature)",
			res.Completed, len(res.BeadIDs))
	}
	t.Logf("VN4 AllReachMerge PASS: N=%d completed=%d closed=%d maxConcurrent=%d stale=%d launchStall=%d",
		len(res.BeadIDs), res.Completed, res.ClosedBeads, res.MaxConcurrent, res.Stale, res.LaunchStall)
}

type vn4PaneFixtureAdapter struct {
	mu sync.Mutex
	// paneCounter assigns sequential pane IDs.
	paneCounter int
	// spawnedAt records the wall time each window's pane was spawned, keyed by
	// pane ID, so WindowPanePID can report "gone" once paneAliveWindow elapses.
	spawnedAt map[string]time.Time
	// paneAliveWindow is how long a pane reports an active PID before reporting
	// gone. Tuned to comfortably exceed the (shrunk) launchSuppressionCeiling so
	// the reverted path has time to wedge-and-kill, while the fixed path advances.
	paneAliveWindow time.Duration
}

func newVN4PaneFixtureAdapter(aliveWindow time.Duration) *vn4PaneFixtureAdapter {
	return &vn4PaneFixtureAdapter{
		spawnedAt:       make(map[string]time.Time),
		paneAliveWindow: aliveWindow,
	}
}

func (a *vn4PaneFixtureAdapter) ProbeTmux(context.Context) error { return nil }
func (a *vn4PaneFixtureAdapter) ListSessions(context.Context) ([]string, error) {
	return nil, nil
}

func (a *vn4PaneFixtureAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *vn4PaneFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter)
	a.spawnedAt[paneID] = time.Now()
	a.mu.Unlock()
	return tmux.Outcome{
		Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName),
		PaneID: paneID,
	}
}

func (a *vn4PaneFixtureAdapter) KillWindow(context.Context, tmux.WindowHandle) error { return nil }

// WindowPanePID returns the test-process PID while the pane is within its alive
// window, then ErrNoSession (pane gone). The handle may be the "%N" pane ID
// (from PaneHasActiveProcess / pidTarget) or the "session:window" handle (from
// runWait's slow path before pid is set); we treat any non-empty, in-window pane
// as alive by checking the most-recent spawn when the handle is not a pane ID.
func (a *vn4PaneFixtureAdapter) WindowPanePID(_ context.Context, handle tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := string(handle)
	var spawned time.Time
	if t, ok := a.spawnedAt[h]; ok {
		spawned = t
	} else {
		for _, t := range a.spawnedAt {
			if t.After(spawned) {
				spawned = t
			}
		}
	}
	if spawned.IsZero() || time.Since(spawned) > a.paneAliveWindow {
		return 0, tmux.ErrNoSession
	}
	return os.Getpid(), nil
}

func (a *vn4PaneFixtureAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil // PaneID is set via NewWindowIn outcome
}
func (a *vn4PaneFixtureAdapter) KillSession(context.Context, string) error { return nil }
func (a *vn4PaneFixtureAdapter) LoadBuffer(context.Context, string, []byte) error {
	return nil
}
func (a *vn4PaneFixtureAdapter) PasteBuffer(context.Context, string, string) error { return nil }
func (a *vn4PaneFixtureAdapter) SendKeysLiteral(context.Context, string, string) error {
	return nil
}
func (a *vn4PaneFixtureAdapter) SendKeysEnter(context.Context, string) error { return nil }
func (a *vn4PaneFixtureAdapter) SendKeysQuit(context.Context, string) error  { return nil }
func (a *vn4PaneFixtureAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

var _ tmux.Adapter = (*vn4PaneFixtureAdapter)(nil)

// TestScenario_ConcurrentDispatch_VN4_WatchdogContention is the KEYSTONE
// reproduction: it engages a fake-adapter *tmuxSubstrate so the
// pasteInjectQuitOnCommit watchdog launches as the SECOND per-run-tap consumer,
// then dispatches N>=3 heartbeat-then-hold beads concurrently. This is the only
// path that exercises the hk-37giq competing-consumer race (the exec path has a
// single tap consumer and cannot reproduce the wedge — see the AllReachMerge
// docstring).
//
// On current main (fan-out tap, 53ead2aa) the watchdog gets its own copy of the
// immediate startup heartbeat, advances, and all N runs reach a terminal event
// without a launch wedge → this asserts all N reach run_completed and there is no
// terminal run_stale / launch_stall_detected.
//
// Reverting 53ead2aa (restore the single-shared-channel tap) is expected to make
// THIS test fail: waitAgentReady's drainer steals the heartbeat, the watchdog
// wedges in the launch-suppression branch until launchSuppressionCeiling, then
// kills the run (run_failed / launch_stall_detected). Run the keystone manually
// fixed-vs-reverted; the worktree branch report records both results and any
// timing caveats (the race window is environment-dependent — the assertion is the
// terminal lifecycle, never a suppression-line count).
//
// The launch/suppression timeouts are shrunk via the export seams so the test
// runs in seconds rather than the production 180s/12min.
//
// Bead: hk-ukhzu. Refs: hk-37giq.
//
// ── ALTITUDE BLOCKER (empirically established; do not re-enable without solving)
//
// This test is SKIPPED. Engaging a fake-adapter *tmuxSubstrate does make the
// pasteInjectQuitOnCommit watchdog the second per-run-tap consumer (so the race
// CAN occur), but the substrate path cannot be driven to a terminal run state by
// a fake adapter alone, because on the substrate path:
//
//   - The substrate session's Stdout() is nil (handler.launchViaSubstrate), so
//     there is NO stdout watcher. agent_ready and the run outcome arrive over the
//     hook-bridge UNIX socket (HookSessionStore.SetAgentReadyCallback /
//     WaitForOutcome), NOT the twin's stdout. A fake adapter does not run the
//     socket relay, so waitAgentReady ALWAYS times out (agent_ready_timeout,
//     HC-056) and the run is killed BEFORE the watchdog/launch phase — i.e. the
//     wedge condition is never reached. (Verified: the run fails with
//     "agent_ready timeout: no agent_ready event within deadline" in ~4s.)
//   - Returning a real live PID (e.g. os.Getpid()) from the fake adapter's
//     WindowPanePID to satisfy PaneHasActiveProcess is unsafe — the daemon's kill
//     paths may signal that PID.
//
// Driving this to completion requires the full hook-bridge socket wiring (a real
// relay or a socket stub feeding agent_ready + outcome) AND a tmux-or-equivalent
// substrate that reports a pane-active child it is safe to kill. That is real
// tmux + real socket altitude, which (a) the validation-net brief forbids
// touching on the shared box and (b) is far heavier than a regression-guard
// test. The shipped channel-level fan-out unit test
// (workloopeventsource_hk37giq_test.go) IS the deterministic keystone for the
// tap mechanism (it FAILS on the reverted single-shared-channel design, PASSES on
// the fan-out); the exec-path TestScenario_ConcurrentDispatch_VN4_AllReachMerge
// above is the end-to-end concurrent-dispatch+merge guard. This skipped test
// documents the path and is the scaffold to finish once a socket stub lands
// (follow-up: a hook-bridge socket fake for substrate-path scenario tests).
func TestScenario_ConcurrentDispatch_VN4_WatchdogContention(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Skip("VN4 keystone: substrate-path run cannot reach a terminal state without " +
		"hook-bridge socket wiring (agent_ready/outcome arrive over the socket, not " +
		"stdout); see the test docstring's ALTITUDE BLOCKER. The channel-level fan-out " +
		"unit test (workloopeventsource_hk37giq_test.go) is the deterministic tap-mechanism " +
		"keystone; AllReachMerge is the end-to-end concurrent-merge guard.")

	restore := vn4ShrinkWatchdogTimers(t,
		2*time.Second,  // launchHeartbeatTimeout
		6*time.Second,  // launchSuppressionCeiling (the reverted wedge kills here)
		1*time.Second,  // noChangeKillDelay
		2*time.Second,  // postQuitKillGrace
		20*time.Second, // commitPollTimeout
	)
	defer restore()

	fakeAdapter := newVN4PaneFixtureAdapter(10 * time.Second)
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "vn4-keystone-session")

	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N:                 3,
		TwinScenario:      "heartbeat-then-hold",
		Boot:              vn4BootForTesting(t),
		Substrate:         substrate,
		ExpectAllComplete: true,
		AgentReadyTimeout: 3 * time.Second,
		TerminalBudget:    90 * time.Second,
		BeadPrefix:        "vn4k",
	})

	t.Logf("VN4 WatchdogContention: N=%d completed=%d failed=%d closed=%d maxConcurrent=%d stale=%d launchStall=%d",
		len(res.BeadIDs), res.Completed, res.Failed, res.ClosedBeads, res.MaxConcurrent, res.Stale, res.LaunchStall)
}

func vn4ShrinkWatchdogTimers(t *testing.T, launchHB, launchSuppress, noChangeKill, postQuit, commitPoll time.Duration) func() {
	t.Helper()
	origLaunchHB := *daemon.ExportedLaunchHeartbeatTimeout
	origLaunchSuppress := *daemon.ExportedLaunchSuppressionCeiling
	origNoChangeKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origCommitPoll := *daemon.ExportedCommitPollTimeout

	*daemon.ExportedLaunchHeartbeatTimeout = launchHB
	*daemon.ExportedLaunchSuppressionCeiling = launchSuppress
	*daemon.ExportedNoChangeKillDelay = noChangeKill
	*daemon.ExportedPostQuitKillGrace = postQuit
	*daemon.ExportedCommitPollTimeout = commitPoll

	return func() {
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunchHB
		*daemon.ExportedLaunchSuppressionCeiling = origLaunchSuppress
		*daemon.ExportedNoChangeKillDelay = origNoChangeKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		*daemon.ExportedCommitPollTimeout = origCommitPoll
	}
}
