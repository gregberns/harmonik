package daemon_test

// scenario_supervisor_revive_default_selfheal_hku0pz_test.go — reproducing
// scenario tests for hk-9ptu (proactive session keepalive).
//
// # Scenario under test
//
// On supervisor-revive (DaemonWatchdog path), the daemon falls back to the
// deterministic "harmonik-<hash>-default" session (needEnsureSession=true in
// main.go) and marks the substrate with WithSessionKeepalive. daemon.Start then
// starts a background RunSessionKeepalive goroutine that periodically calls
// EnsureSession so the session is recreated if it is killed externally between
// dispatches.
//
// Without hk-9ptu, the keepalive goroutine was never started. If the "-default"
// session was killed between dispatches (e.g. by the operator, a rogue reaper,
// or OS idle-shell timeout), the next SpawnWindow burst would ALL hit ErrNoSession
// simultaneously, causing a fleet-wide launch_initiated outage until the hk-yaj
// retry recovered (one retry per SpawnWindow call, racing each other to recreate
// the session).
//
// # Tests
//
//   - DaemonStart_WiresKeepaliveGoroutine: daemon.Start detects
//     substrateWithKeepalive on cfg.Substrate and launches RunSessionKeepalive
//     as a background goroutine. Verified by asserting EnsureSession is called
//     after daemon.Start returns (with BrPath="" — no work loop).
//
//   - DefaultSession_SelfHealsAfterKill: the keepalive goroutine calls
//     EnsureSession after the session is "killed" (adapter transitions from alive
//     to dead). After EnsureSession is called, the session is alive again and
//     SpawnWindow succeeds without the hk-yaj retry path.
//
// # Bead refs
//
//   - hk-u0pz (this test)
//   - hk-9ptu (proactive session keepalive fix)
//   - hk-yaj (reactive ErrNoSession self-heal in SpawnWindow)

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─── fixture adapters ─────────────────────────────────────────────────────────

// supervisorReviveCountingAdapter counts EnsureSession calls. Implements the full
// tmux.Adapter interface (via embedded noSessionFixtureBase) plus sessionEnsurer.
type supervisorReviveCountingAdapter struct {
	noSessionFixtureBase
	ensureCalls atomic.Int64
}

func (a *supervisorReviveCountingAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Handle: tmux.WindowHandle("revive-test:0")}
}

func (a *supervisorReviveCountingAdapter) EnsureSession(_ context.Context, _, _ string) error {
	a.ensureCalls.Add(1)
	return nil
}

var _ tmux.Adapter = (*supervisorReviveCountingAdapter)(nil)

// selfHealAdapter simulates a session that can be "killed" and recreated.
//
// Before Kill is called: NewWindowIn returns a live window handle.
// After Kill and before EnsureSession: NewWindowIn returns ErrNoSession.
// After EnsureSession: NewWindowIn returns a live window handle again.
//
// This models the supervisor-revive self-heal scenario: the daemon-owned
// "-default" session is killed externally (operator, reaper, idle timeout), the
// keepalive goroutine calls EnsureSession to recreate it, and the next
// SpawnWindow succeeds without the hk-yaj retry path.
type selfHealAdapter struct {
	noSessionFixtureBase
	killed      atomic.Bool
	ensureCalls atomic.Int64
	newWindowIn atomic.Int64
}

func (a *selfHealAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.newWindowIn.Add(1)
	if a.killed.Load() {
		return tmux.Outcome{Err: tmux.ErrNoSession}
	}
	return tmux.Outcome{Handle: tmux.WindowHandle("selfheal-test:0")}
}

func (a *selfHealAdapter) EnsureSession(_ context.Context, _, _ string) error {
	a.ensureCalls.Add(1)
	a.killed.Store(false) // session recreated
	return nil
}

var _ tmux.Adapter = (*selfHealAdapter)(nil)

// ─── fixtures ─────────────────────────────────────────────────────────────────

// supervisorReviveProjectDir creates a minimal project dir for the scenario tests.
// Returns the project dir and JSONL log path.
func supervisorReviveProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil { //nolint:gosec // G301: test fixture
		t.Fatalf("supervisorReviveProjectDir: MkdirAll %s: %v", eventsDir, err)
	}
	return dir, filepath.Join(eventsDir, "events.jsonl")
}

// supervisorReviveStartDaemon starts daemon.Start in a goroutine and returns a
// cancel function and a channel that receives the Start error on exit.
func supervisorReviveStartDaemon(t *testing.T, cfg daemon.Config) (cancel func(), done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() { ch <- daemon.Start(ctx, cfg) }()
	return cancelFn, ch
}

// supervisorReviveWaitDaemon waits for the daemon goroutine to exit within budget.
func supervisorReviveWaitDaemon(t *testing.T, done <-chan error, budget time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("supervisorRevive: daemon.Start returned non-nil after context cancel: %v", err)
		}
	case <-time.After(budget):
		t.Error("supervisorRevive: daemon.Start did not exit within budget after context cancel")
	}
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestScenario_SupervisorRevive_DaemonStart_WiresKeepaliveGoroutine verifies
// that daemon.Start probes cfg.Substrate for the substrateWithKeepalive
// interface and starts RunSessionKeepalive as a background goroutine (hk-9ptu).
//
// This is the integration point that was missing before hk-9ptu: the unit-level
// WithSessionKeepalive option and RunSessionKeepalive method existed, but
// daemon.Start did not call RunSessionKeepalive. This test would have caught
// that regression.
//
// Reproduces the supervisor-revive path:
//  1. Substrate is built with WithSessionKeepalive (main.go sets this when
//     needEnsureSession=true, i.e. live session was flywheel/supervisor/empty).
//  2. daemon.Start detects substrateWithKeepalive on cfg.Substrate.
//  3. daemon.Start launches RunSessionKeepalive as a goroutine.
//  4. RunSessionKeepalive calls EnsureSession at the configured interval.
//
// Assertion: EnsureSession is called ≥1 time within a generous timeout after
// daemon.Start has started. BrPath="" skips the work loop so the test is
// deterministic and does not require a real br binary.
func TestScenario_SupervisorRevive_DaemonStart_WiresKeepaliveGoroutine(t *testing.T) {
	t.Parallel()
	skipRealDaemonE2EInShort(t)

	projectDir, jsonlPath := supervisorReviveProjectDir(t)

	adapter := &supervisorReviveCountingAdapter{}
	substrate := daemon.NewTmuxSubstrate(adapter, "hk-u0pz-keepalive-test",
		daemon.WithSessionKeepalive(5*time.Millisecond), // very short for fast test
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              "", // no work loop — socket + keepalive path only
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           substrate,
	}

	cancel, done := supervisorReviveStartDaemon(t, cfg)
	defer func() {
		cancel()
		supervisorReviveWaitDaemon(t, done, 5*time.Second)
	}()

	// Poll up to 2 s for EnsureSession to be called by the keepalive goroutine
	// that daemon.Start should have started. If daemon.Start did NOT wire
	// RunSessionKeepalive, EnsureSession would never be called and this poll
	// would time out — reproducing the pre-hk-9ptu regression.
	const budget = 2 * time.Second
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if adapter.ensureCalls.Load() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	cancel() // shut down daemon before assertion to avoid leaks

	calls := adapter.ensureCalls.Load()
	if calls < 1 {
		t.Errorf("DaemonStart_WiresKeepaliveGoroutine FAIL: EnsureSession not called within %s; "+
			"daemon.Start must launch RunSessionKeepalive when substrate implements substrateWithKeepalive (hk-9ptu). "+
			"Got %d calls.", budget, calls)
	} else {
		t.Logf("DaemonStart_WiresKeepaliveGoroutine PASS: EnsureSession called %d time(s) within %s "+
			"— daemon.Start correctly wires RunSessionKeepalive on supervisor-revive path", calls, budget)
	}
}

// TestScenario_SupervisorRevive_DefaultSession_SelfHealsAfterKill verifies the
// end-to-end self-heal scenario (hk-9ptu): when the daemon-owned "-default"
// session is killed externally between dispatches, the keepalive goroutine calls
// EnsureSession to recreate it, and subsequent SpawnWindow calls succeed.
//
// Reproduces the exact failure mode from the 2026-06-12 outage:
//   - Daemon starts on supervisor-revive path → "-default" created at boot.
//   - Session is killed externally (operator, rogue reaper, or OS idle timeout).
//   - Without hk-9ptu: no keepalive goroutine → session stays dead between
//     dispatches → burst SpawnWindow all fail with ErrNoSession → fleet-wide
//     launch_initiated outage.
//   - With hk-9ptu: keepalive goroutine calls EnsureSession → session recreated
//     before next dispatch → SpawnWindow succeeds.
//
// The test uses selfHealAdapter, which tracks the session's alive/dead state
// and transitions it: alive (initial) → dead (Kill()) → alive (EnsureSession).
// The SpawnWindow call after recreation must succeed.
func TestScenario_SupervisorRevive_DefaultSession_SelfHealsAfterKill(t *testing.T) {
	t.Parallel()

	adapter := &selfHealAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-u0pz-selfheal-test",
		daemon.WithSessionKeepalive(5*time.Millisecond), // very short for fast test
		daemon.WithNewWindowTimeout(500*time.Millisecond),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the keepalive goroutine (in production this is done by daemon.Start).
	go sk.RunSessionKeepalive(ctx)

	// Verify initial state: SpawnWindow succeeds before the session is killed.
	initialSess, initialErr := sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Cwd:        t.TempDir(),
		WindowName: "pre-kill",
		Argv:       []string{"/bin/sh"},
	})
	if initialErr != nil {
		t.Fatalf("SelfHealsAfterKill: pre-kill SpawnWindow failed (unexpected): %v", initialErr)
	}
	_ = initialSess

	// Simulate session being killed externally between dispatches.
	// After this point, NewWindowIn returns ErrNoSession until EnsureSession is called.
	adapter.killed.Store(true)
	t.Log("SelfHealsAfterKill: session killed; waiting for keepalive to call EnsureSession")

	// Poll up to 1 s for EnsureSession to be called (keepalive at 5 ms interval).
	const healBudget = 1 * time.Second
	deadline := time.Now().Add(healBudget)
	for time.Now().Before(deadline) {
		if adapter.ensureCalls.Load() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	ensureCalls := adapter.ensureCalls.Load()
	if ensureCalls < 1 {
		t.Fatalf("SelfHealsAfterKill FAIL: EnsureSession not called within %s after session kill; "+
			"keepalive goroutine must call EnsureSession to recreate the session (hk-9ptu). "+
			"Got %d calls.", healBudget, ensureCalls)
	}
	t.Logf("SelfHealsAfterKill: EnsureSession called %d time(s); session should be alive again", ensureCalls)

	// After EnsureSession, the session should be alive (selfHealAdapter resets
	// killed=false in EnsureSession). SpawnWindow should now succeed without retry.
	//
	// This is the key assertion: pre-hk-9ptu, the session would have stayed dead
	// because no keepalive goroutine was running, so this SpawnWindow would fail
	// (or require the hk-yaj retry to fire). Post-hk-9ptu, the session is already
	// recreated proactively, so SpawnWindow succeeds on the first attempt.
	postHealSess, postHealErr := sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Cwd:        t.TempDir(),
		WindowName: "post-heal",
		Argv:       []string{"/bin/sh"},
	})
	if postHealErr != nil {
		t.Errorf("SelfHealsAfterKill FAIL: SpawnWindow failed after keepalive EnsureSession: %v; "+
			"keepalive must recreate the session so SpawnWindow succeeds without hk-yaj retry (hk-9ptu)",
			postHealErr)
	} else {
		_ = postHealSess
		t.Logf("SelfHealsAfterKill PASS: SpawnWindow succeeded after keepalive recreated the session "+
			"(EnsureSession called %d time(s))", ensureCalls)
	}
}
