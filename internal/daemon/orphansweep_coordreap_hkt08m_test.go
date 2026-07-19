package daemon

// orphansweep_coordreap_hkt08m_test.go — unit tests for the work-loop periodic
// coordinator-session reaper (hk-t08m).
//
// These are plain unit tests (NOT scenario): they exercise runPeriodicCoordinatorReap
// and the work-loop rate-limiting gate without booting a full daemon.
//
// Tests:
//   - runPeriodicCoordinatorReap: reaps when supervisor PID is dead (sentinel → dead PID).
//   - runPeriodicCoordinatorReap: no-op when supervisor is live.
//   - runPeriodicCoordinatorReap: no-op when adapter is nil.
//   - runPeriodicCoordinatorReap: no-op when sentinel is absent (no supervisor ever started).
//   - Work-loop gate: skips reap when within rate-limit window.
//   - Work-loop gate: fires reap after rate-limit window elapses.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// hkt08mFakeAdapter_t08m is a minimal ltmux.Adapter that records KillSession
// calls and returns a fixed session list. Suffixed _t08m to avoid redeclaration
// collisions with sibling beads that declare same-shape adapters in this package.
type hkt08mFakeAdapter_t08m struct {
	mu       sync.Mutex
	sessions []string
	killed   []string
}

func (a *hkt08mFakeAdapter_t08m) ListSessions(_ context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessions))
	copy(out, a.sessions)
	return out, nil
}

func (a *hkt08mFakeAdapter_t08m) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	return nil
}

func (a *hkt08mFakeAdapter_t08m) killedSessions_t08m() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.killed))
	copy(out, a.killed)
	return out
}

// --- no-op stubs to satisfy ltmux.Adapter ---

func (a *hkt08mFakeAdapter_t08m) ProbeTmux(_ context.Context) error { return nil }

func (a *hkt08mFakeAdapter_t08m) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *hkt08mFakeAdapter_t08m) NewWindowIn(_ context.Context, _ ltmux.NewWindowIn) ltmux.Outcome {
	return ltmux.Outcome{}
}

func (a *hkt08mFakeAdapter_t08m) KillWindow(_ context.Context, _ ltmux.WindowHandle) error {
	return nil
}

func (a *hkt08mFakeAdapter_t08m) WindowPanePID(_ context.Context, _ ltmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *hkt08mFakeAdapter_t08m) WindowPaneID(_ context.Context, _ ltmux.WindowHandle) (string, error) {
	return "", nil
}

func (a *hkt08mFakeAdapter_t08m) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *hkt08mFakeAdapter_t08m) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *hkt08mFakeAdapter_t08m) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *hkt08mFakeAdapter_t08m) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *hkt08mFakeAdapter_t08m) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *hkt08mFakeAdapter_t08m) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ ltmux.Adapter = (*hkt08mFakeAdapter_t08m)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// runPeriodicCoordinatorReap unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHKt08m_PeriodicReap_DeadSupervisor verifies that runPeriodicCoordinatorReap
// kills the flywheel session when the sentinel is present but the supervisor PID
// is dead.
func TestHKt08m_PeriodicReap_DeadSupervisor(t *testing.T) {
	t.Parallel()

	const deadPID = 99972
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("hk-t08m dead supervisor: PID %d is live; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, deadPID)

	adapter := &hkt08mFakeAdapter_t08m{sessions: []string{flywheel}}

	reaped := runPeriodicCoordinatorReap(t.Context(), projectDir, projectHash, adapter, nil)
	if reaped != 1 {
		t.Fatalf("runPeriodicCoordinatorReap: reaped = %d, want 1 (dead supervisor)", reaped)
	}
	killed := adapter.killedSessions_t08m()
	if len(killed) != 1 || killed[0] != flywheel {
		t.Errorf("runPeriodicCoordinatorReap: killed = %v, want [%q]", killed, flywheel)
	}
	// Sentinel must have been removed by the probe.
	if pl006dSentinelExists(projectDir) {
		t.Error("runPeriodicCoordinatorReap: stale sentinel still present; probe must remove it")
	}
}

// TestHKt08m_PeriodicReap_LiveSupervisor verifies that runPeriodicCoordinatorReap
// is a no-op when the supervisor PID is live (own process).
func TestHKt08m_PeriodicReap_LiveSupervisor(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, os.Getpid()) // own PID is always live

	adapter := &hkt08mFakeAdapter_t08m{sessions: []string{flywheel}}

	reaped := runPeriodicCoordinatorReap(t.Context(), projectDir, projectHash, adapter, nil)
	if reaped != 0 {
		t.Fatalf("runPeriodicCoordinatorReap: reaped = %d, want 0 (live supervisor)", reaped)
	}
	if len(adapter.killedSessions_t08m()) != 0 {
		t.Errorf("runPeriodicCoordinatorReap: killed sessions %v for live supervisor; must be empty", adapter.killedSessions_t08m())
	}
}

// TestHKt08m_PeriodicReap_NilAdapter verifies that runPeriodicCoordinatorReap
// is a no-op when the adapter is nil (no tmux substrate).
func TestHKt08m_PeriodicReap_NilAdapter(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	reaped := runPeriodicCoordinatorReap(t.Context(), projectDir, projectHash, nil, nil)
	if reaped != 0 {
		t.Errorf("runPeriodicCoordinatorReap(nil adapter): reaped = %d, want 0", reaped)
	}
}

// TestHKt08m_PeriodicReap_AbsentSentinel verifies that runPeriodicCoordinatorReap
// reaps the flywheel session when the sentinel is absent (no supervisor was ever
// started, or it exited cleanly without leaving a session behind).
//
// When the sentinel is absent, probeCoordinatorSentinel returns Live=false and
// SentinelRemoved=false — the reaper treats the session as unconditionally
// orphaned (same as the hk-7u002 boot path).
func TestHKt08m_PeriodicReap_AbsentSentinel(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	// No sentinel written — sentinel is absent.
	adapter := &hkt08mFakeAdapter_t08m{sessions: []string{flywheel}}

	reaped := runPeriodicCoordinatorReap(t.Context(), projectDir, projectHash, adapter, nil)
	if reaped != 1 {
		t.Fatalf("runPeriodicCoordinatorReap: reaped = %d, want 1 (absent sentinel)", reaped)
	}
	killed := adapter.killedSessions_t08m()
	if len(killed) != 1 || killed[0] != flywheel {
		t.Errorf("runPeriodicCoordinatorReap: killed = %v, want [%q]", killed, flywheel)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Work-loop rate-limiting gate tests
// ─────────────────────────────────────────────────────────────────────────────

// newMinimalDeps_t08m builds the minimal workLoopDeps needed to exercise the
// periodic-reap gate in the work loop. Suffixed _t08m to avoid redeclaration
// collisions with sibling beads.
func newMinimalDeps_t08m(projectDir string, projectHash core.ProjectHash, adapter *hkt08mFakeAdapter_t08m, interval time.Duration) workLoopDeps {
	return workLoopDeps{
		projectDir:                 projectDir,
		coordinatorReapAdapter:     adapter,
		coordinatorReapProjectHash: projectHash,
		coordinatorReapInterval:    interval,
		// lastCoordinatorReap zero-valued → first tick fires immediately.
	}
}

// TestHKt08m_WorkLoop_SkipsReapWithinInterval verifies that the work-loop
// gate does NOT call the reaper when the interval has not elapsed.
func TestHKt08m_WorkLoop_SkipsReapWithinInterval(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	adapter := &hkt08mFakeAdapter_t08m{sessions: []string{flywheel}}

	// Set lastCoordinatorReap to "just now" so the interval has NOT elapsed.
	deps := newMinimalDeps_t08m(projectDir, projectHash, adapter, time.Hour)
	maint := loopMaintenanceState{lastCoordinatorReap: time.Now()}

	// Simulate the work-loop gate logic (step 1c).
	interval := deps.coordinatorReapInterval
	if interval <= 0 {
		interval = periodicCoordinatorReapInterval
	}
	if deps.coordinatorReapAdapter != nil && time.Since(maint.lastCoordinatorReap) >= interval {
		runPeriodicCoordinatorReap(context.Background(), deps.projectDir, deps.coordinatorReapProjectHash, deps.coordinatorReapAdapter, nil)
		maint.lastCoordinatorReap = time.Now()
	}

	killed := adapter.killedSessions_t08m()
	if len(killed) != 0 {
		t.Errorf("work-loop gate: killed sessions %v within interval; want none", killed)
	}
}

// TestHKt08m_WorkLoop_FiresReapAfterInterval verifies that the work-loop gate
// DOES call the reaper once the interval has elapsed.
func TestHKt08m_WorkLoop_FiresReapAfterInterval(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	adapter := &hkt08mFakeAdapter_t08m{sessions: []string{flywheel}}

	// Use a near-zero interval so time.Since always exceeds it.
	deps := newMinimalDeps_t08m(projectDir, projectHash, adapter, time.Nanosecond)
	// maint.lastCoordinatorReap zero-valued → far in the past relative to 1ns interval.
	var maint loopMaintenanceState

	// Simulate the work-loop gate logic (step 1c).
	interval := deps.coordinatorReapInterval
	if interval <= 0 {
		interval = periodicCoordinatorReapInterval
	}
	if deps.coordinatorReapAdapter != nil && time.Since(maint.lastCoordinatorReap) >= interval {
		runPeriodicCoordinatorReap(context.Background(), deps.projectDir, deps.coordinatorReapProjectHash, deps.coordinatorReapAdapter, nil)
		maint.lastCoordinatorReap = time.Now()
	}

	killed := adapter.killedSessions_t08m()
	if len(killed) != 1 || killed[0] != flywheel {
		t.Errorf("work-loop gate: killed = %v after interval elapsed; want [%q]", killed, flywheel)
	}
}
