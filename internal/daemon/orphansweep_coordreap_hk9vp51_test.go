package daemon

// orphansweep_coordreap_hk9vp51_test.go — unit tests for the boot-time
// dead-supervisor coordinator-session reaper (hk-9vp51).
//
// Problem: when the auto-revive supervisor dies WITHOUT `tmux kill-session`, its
// flywheel/coordinator session (harmonik-<hash>-flywheel) leaks forever.  The
// generic orphan sweep can fail to classify it as orphaned (the supervisor's
// re-parented bash children keep the first pane PID "alive"), so it survives
// every boot.  The fix force-reaps the coordinator session at boot when the
// sentinel probe confirms the supervisor PID is DEAD (sentinel present,
// kill(pid,0) → ESRCH).
//
// Tests:
//   - reapDeadCoordinatorSession: kills the flywheel session when present.
//   - reapDeadCoordinatorSession: no-op (0) when the session is absent.
//   - reapDeadCoordinatorSession: no-op (0) when adapter is nil.
//   - RunOrphanSweep: dead supervisor → CoordinatorSessionsReaped=1 via adapter.
//   - RunOrphanSweep: live supervisor → coordinator session NOT reaped.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// hk9vp51FakeAdapter is a minimal ltmux.Adapter that records KillSession calls
// and returns a fixed session list.  Only ListSessions and KillSession are
// exercised by the reaper; the other methods are no-op stubs.
type hk9vp51FakeAdapter struct {
	mu       sync.Mutex
	sessions []string
	killed   []string
}

func (a *hk9vp51FakeAdapter) ListSessions(_ context.Context) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessions))
	copy(out, a.sessions)
	return out, nil
}

func (a *hk9vp51FakeAdapter) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	return nil
}

func (a *hk9vp51FakeAdapter) killedSessions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.killed))
	copy(out, a.killed)
	return out
}

// --- no-op stubs to satisfy ltmux.Adapter ---

func (a *hk9vp51FakeAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hk9vp51FakeAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *hk9vp51FakeAdapter) NewWindowIn(_ context.Context, _ ltmux.NewWindowIn) ltmux.Outcome {
	return ltmux.Outcome{}
}
func (a *hk9vp51FakeAdapter) KillWindow(_ context.Context, _ ltmux.WindowHandle) error { return nil }
func (a *hk9vp51FakeAdapter) WindowPanePID(_ context.Context, _ ltmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *hk9vp51FakeAdapter) WindowPaneID(_ context.Context, _ ltmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hk9vp51FakeAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *hk9vp51FakeAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *hk9vp51FakeAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *hk9vp51FakeAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *hk9vp51FakeAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *hk9vp51FakeAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ ltmux.Adapter = (*hk9vp51FakeAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// reapDeadCoordinatorSession unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestHK9vp51_ReapDeadCoordinatorSession_KillsWhenPresent(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")

	adapter := &hk9vp51FakeAdapter{sessions: []string{flywheel, "harmonik-other-default"}}

	reaped := reapDeadCoordinatorSession(t.Context(), projectHash, adapter, nil)
	if reaped != 1 {
		t.Fatalf("reapDeadCoordinatorSession: reaped = %d, want 1", reaped)
	}
	killed := adapter.killedSessions()
	if len(killed) != 1 || killed[0] != flywheel {
		t.Errorf("reapDeadCoordinatorSession: killed = %v, want [%q]", killed, flywheel)
	}
}

func TestHK9vp51_ReapDeadCoordinatorSession_NoopWhenAbsent(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	// Flywheel session NOT in the list.
	adapter := &hk9vp51FakeAdapter{sessions: []string{"harmonik-other-default"}}

	reaped := reapDeadCoordinatorSession(t.Context(), projectHash, adapter, nil)
	if reaped != 0 {
		t.Errorf("reapDeadCoordinatorSession: reaped = %d, want 0 (absent)", reaped)
	}
	if len(adapter.killedSessions()) != 0 {
		t.Errorf("reapDeadCoordinatorSession: killed %v sessions, want none", adapter.killedSessions())
	}
}

func TestHK9vp51_ReapDeadCoordinatorSession_NilAdapter(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	if reaped := reapDeadCoordinatorSession(t.Context(), projectHash, nil, nil); reaped != 0 {
		t.Errorf("reapDeadCoordinatorSession(nil adapter): reaped = %d, want 0", reaped)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RunOrphanSweep integration: boot reaper on dead supervisor
// ─────────────────────────────────────────────────────────────────────────────

// TestHK9vp51_RunOrphanSweep_DeadSupervisor_CoordinatorReaped verifies that a
// dead-supervisor sentinel triggers the boot-time coordinator reaper via the
// adapter path: CoordinatorSessionsReaped=1 and the flywheel session is killed.
func TestHK9vp51_RunOrphanSweep_DeadSupervisor_CoordinatorReaped(t *testing.T) {
	t.Parallel()

	const deadPID = 99971
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("hk-9vp51 dead supervisor: PID %d is live; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, deadPID)

	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")
	adapter := &hk9vp51FakeAdapter{sessions: []string{flywheel}}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter: adapter,
		},
	)
	if err != nil {
		// git worktree prune fails on a non-git temp dir; expected.
		t.Logf("dead supervisor: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsReaped != 1 {
		t.Errorf("dead supervisor: CoordinatorSessionsReaped = %d, want 1", result.CoordinatorSessionsReaped)
	}
	killed := adapter.killedSessions()
	found := false
	for _, k := range killed {
		if k == flywheel {
			found = true
		}
	}
	if !found {
		t.Errorf("dead supervisor: flywheel session %q not killed; killed=%v", flywheel, killed)
	}
	if pl006dSentinelExists(projectDir) {
		t.Error("dead supervisor: stale sentinel still exists; must be removed by probe")
	}
}

// TestHK9vp51_RunOrphanSweep_LiveSupervisor_CoordinatorNotReaped verifies that a
// LIVE supervisor's coordinator session is NOT reaped by the boot reaper.
func TestHK9vp51_RunOrphanSweep_LiveSupervisor_CoordinatorNotReaped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, os.Getpid()) // live (this process)

	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")
	adapter := &hk9vp51FakeAdapter{sessions: []string{flywheel}}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter: adapter,
		},
	)
	if err != nil {
		t.Logf("live supervisor: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsReaped != 0 {
		t.Errorf("live supervisor: CoordinatorSessionsReaped = %d, want 0", result.CoordinatorSessionsReaped)
	}
	if result.CoordinatorSessionsSkipped != 1 {
		t.Errorf("live supervisor: CoordinatorSessionsSkipped = %d, want 1", result.CoordinatorSessionsSkipped)
	}
	for _, k := range adapter.killedSessions() {
		if k == flywheel {
			t.Errorf("live supervisor: flywheel session %q was reaped; must be preserved", flywheel)
		}
	}
}
