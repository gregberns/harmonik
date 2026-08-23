//go:build scenario

package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type llslFixtureAdapter struct {
	mu          sync.Mutex
	paneCounter int
	// spawnedAt records when each pane's window was created, keyed by pane ID,
	// so WindowPanePID can report "gone" once paneAliveWindow elapses.
	spawnedAt map[string]time.Time
	// paneAliveWindow is how long a pane reports alive (pid=0, no error) before
	// reporting gone (ErrNoSession). Tuned to comfortably cover run_started +
	// launch_initiated + the short agent_ready wait, then let runWait unblock.
	paneAliveWindow time.Duration
}

func newLLSLFixtureAdapter(aliveWindow time.Duration) *llslFixtureAdapter {
	return &llslFixtureAdapter{
		spawnedAt:       make(map[string]time.Time),
		paneAliveWindow: aliveWindow,
	}
}

func (a *llslFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *llslFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *llslFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *llslFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter) // slash-free "%N" pane ID
	a.spawnedAt[paneID] = time.Now()
	a.mu.Unlock()
	return tmux.Outcome{
		Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName),
		PaneID: paneID,
	}
}

func (a *llslFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }

// WindowPanePID returns (pid=0, nil) while the pane is within its alive window,
// then (0, ErrNoSession) once paneAliveWindow elapses (pane gone). pid is ALWAYS
// 0 — never a real PID — so the kill path's killProcessWithGrace (which only
// signals when pid>0) can never SIGTERM a live process; the test process is never
// at risk. Returning os.Getpid() (as the VN4 pane fixture does for a different
// purpose) would be UNSAFE here. The "gone after window" behaviour lets the
// substrate session's runWait slow-path (s.pid==0) observe the error and unblock
// sess.Wait so the run reaches a terminal event promptly. The handle may be the
// "%N" pane ID (from runWait's panePIDTarget) or the "session:window" handle; we
// treat any in-window pane as alive, using the most-recent spawn when the handle
// is not a recorded pane ID.
func (a *llslFixtureAdapter) WindowPanePID(_ context.Context, handle tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := string(handle)
	var spawned time.Time
	if ts, ok := a.spawnedAt[h]; ok {
		spawned = ts
	} else {
		for _, ts := range a.spawnedAt {
			if ts.After(spawned) {
				spawned = ts
			}
		}
	}
	if spawned.IsZero() || time.Since(spawned) > a.paneAliveWindow {
		return 0, tmux.ErrNoSession
	}
	return 0, nil // pane alive, pid unavailable (never a real PID)
}

func (a *llslFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil // PaneID is set via NewWindowIn outcome
}
func (a *llslFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *llslFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *llslFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *llslFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *llslFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *llslFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *llslFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*llslFixtureAdapter)(nil)

func llslEventCount(t *testing.T, jsonlPath, eventType string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("llslEventCount: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil {
			t.Logf("llslEventCount: close: %v", cErr)
		}
	}()
	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &env) != nil {
			continue
		}
		if env.Type == eventType {
			count++
		}
	}
	return count
}

// TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y is the live-mechanism regression
// guard for concurrent launch-liveness + spawn-semaphore no-leak.
//
// It boots the full daemon at MaxConcurrent=N with a fake-adapter *tmuxSubstrate
// carrying a spawn cap of N, dispatches N distinct beads from one wave queue, and
// after the wave drains asserts: (a) all N launched (N run_started, no
// launch_stall_detected / run_stale), (b) the spawn semaphore returned to
// baseline (SpawnSlotsInUse()==0 against cap N — no leaked slot), (c) the
// concurrent-run cap was honored. See the file docstring for why the runs reach
// run_failed (agent_ready_timeout on the fake-substrate path) and why that still
// exercises the slot-release / launch-liveness mechanism deterministically.
//
// Not parallel: RunConcurrentMerge redirects HARMONIK_CLAUDE_CONFIG_PATH through
// t.Setenv, which panics under t.Parallel. It used to set and UNSET the variable,
// which deleted the package-wide isolation for every test that ran afterwards
// (hk-85pqo). The conclusion here was always right; the mechanism named was not.
//
// Bead: hk-40c3y.
func TestScenario_LaunchLiveness_SlotNoLeak_HK40C3Y(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	const n = 3

	adapter := newLLSLFixtureAdapter(4 * time.Second)
	const sessionName = "hk40c3y-llsl-isolated-session" // unique; never a live session
	substrate := daemon.NewTmuxSubstrate(adapter, sessionName,
		daemon.WithSpawnCap(n),
		daemon.WithSpawnAcquireTimeout(10*time.Second),
	)

	if got := daemon.ExportedSpawnSlotsInUse(substrate); got != 0 {
		t.Fatalf("precondition: SpawnSlotsInUse()=%d, want 0 before any dispatch", got)
	}

	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N:            n,
		TwinScenario: "single-happy-path",
		Boot:         vn4BootForTesting(t), // binds StartForTesting + determinism options
		Substrate:    substrate,
		// ExpectAllComplete=false: the fake-substrate path cannot reach
		// run_completed (no hook-bridge socket relay → agent_ready_timeout). The
		// fixture then asserts only that all N reach SOME terminal event + the cap
		// is honored — exactly the drain condition the no-leak assertion needs.
		ExpectAllComplete: false,
		AgentReadyTimeout: 2 * time.Second, // short → each run fails fast on the fake path
		TerminalBudget:    90 * time.Second,
		BeadPrefix:        "llsl",
	})

	nStarted := llslEventCount(t, res.JSONLPath, string(core.EventTypeRunStarted))
	if nStarted < n {
		t.Errorf("(a) launch-liveness: %d/%d run_started events; want all N "+
			"(a shortfall is the launch-stall/no-spawn wedge signature — a run stuck "+
			"at launch_initiated never reaches run_started)", nStarted, n)
	}
	if res.LaunchStall > 0 {
		t.Errorf("(a) launch-liveness: %d launch_stall_detected event(s); want 0 "+
			"(launch_stall_detected is the spawn-wedge terminal signature, hk-4l7zs)", res.LaunchStall)
	}
	if res.Stale > 0 {
		t.Errorf("(a) launch-liveness: %d run_stale event(s); want 0 "+
			"(run_stale is the launch-wedge terminal signature)", res.Stale)
	}

	if got := daemon.ExportedSpawnSlotsInUse(substrate); got != 0 {
		t.Errorf("(b) spawn-semaphore LEAK: SpawnSlotsInUse()=%d after the wave drained; "+
			"want 0 — a slot was acquired by SpawnWindow but never returned by Kill "+
			"(hk-4l7zs slot leak). Subsequent dispatch would be starved.", got)
	}

	if res.MaxConcurrent > n {
		t.Errorf("(c) cap violated: max concurrent runs = %d, want <= %d", res.MaxConcurrent, n)
	}

	t.Logf("hk-40c3y PASS: N=%d run_started=%d (launched all), launchStall=%d stale=%d "+
		"maxConcurrent=%d slotsInUseAfterDrain=%d (cap=%d) — launch-liveness OK, no slot leak",
		n, nStarted, res.LaunchStall, res.Stale, res.MaxConcurrent,
		daemon.ExportedSpawnSlotsInUse(substrate), n)
}
