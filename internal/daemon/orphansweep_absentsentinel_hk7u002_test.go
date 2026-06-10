package daemon

// orphansweep_absentsentinel_hk7u002_test.go — unit tests for the boot-time
// orphan reap when the coordinator sentinel is ABSENT (hk-7u002).
//
// Problem: when the daemon crashes without a live supervisor (sentinel never
// written, or supervisor stopped cleanly and removed its own sentinel), the
// flywheel session (harmonik-<hash>-flywheel) is NOT reapable by the generic
// sessionIsOrphaned check: sessions whose first pane PID is live (shells from
// prior implementer launches that outlived the crash) return false.  Without an
// explicit reap branch for the sentinel-absent case the sessions accumulate
// across daemon restarts until tmux exhaustion blocks new spawns (50 leaked in
// production).
//
// Fix: add an else branch in RunOrphanSweep after the probe.SentinelRemoved
// check that calls reapDeadCoordinatorSession when the sentinel is absent.
//
// Tests:
//   - RunOrphanSweep: sentinel absent + flywheel present → CoordinatorSessionsReaped=1.
//   - RunOrphanSweep: sentinel absent + flywheel absent → CoordinatorSessionsReaped=0 (no-op).
//   - RunOrphanSweep: sentinel absent → does NOT reap spawn-target session.
//   - RunOrphanSweep: CoordinatorSessionsReaped reflected in ToPayload().

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestHK7u002_AbsentSentinel_FlywheelReaped verifies that when no sentinel
// file exists and the flywheel session is present, RunOrphanSweep
// force-reaps it (CoordinatorSessionsReaped=1).
func TestHK7u002_AbsentSentinel_FlywheelReaped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	// No sentinel written — absent case.
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")
	adapter := &hk9vp51FakeAdapter{sessions: []string{flywheel, "harmonik-other-default"}}

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
		t.Logf("absent sentinel: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsReaped != 1 {
		t.Errorf("absent sentinel: CoordinatorSessionsReaped = %d, want 1", result.CoordinatorSessionsReaped)
	}
	killed := adapter.killedSessions()
	found := false
	for _, k := range killed {
		if k == flywheel {
			found = true
		}
	}
	if !found {
		t.Errorf("absent sentinel: flywheel session %q not killed; killed=%v", flywheel, killed)
	}
}

// TestHK7u002_AbsentSentinel_NoFlywheel_Noop verifies that when no sentinel
// exists and the flywheel session is also absent, RunOrphanSweep does not
// reap anything (CoordinatorSessionsReaped=0).
func TestHK7u002_AbsentSentinel_NoFlywheel_Noop(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	// No sentinel, no flywheel session.
	adapter := &hk9vp51FakeAdapter{sessions: []string{"harmonik-other-default"}}

	result, err := RunOrphanSweep(
		context.Background(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter: adapter,
		},
	)
	if err != nil {
		t.Logf("absent sentinel no-flywheel: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsReaped != 0 {
		t.Errorf("absent sentinel no-flywheel: CoordinatorSessionsReaped = %d, want 0", result.CoordinatorSessionsReaped)
	}
	for _, k := range adapter.killedSessions() {
		if k != "harmonik-other-default" {
			t.Errorf("absent sentinel no-flywheel: unexpected session killed: %q", k)
		}
	}
}

// TestHK7u002_AbsentSentinel_DoesNotReapSpawnTarget verifies that the
// sentinel-absent reap path never kills the daemon's own spawn-target session,
// even when it is present alongside the flywheel session.
func TestHK7u002_AbsentSentinel_DoesNotReapSpawnTarget(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")
	spawnTarget := lifecycle.TmuxSessionName(projectHash, "default")

	// No sentinel. Both flywheel and spawn-target are present.
	adapter := &hk9vp51FakeAdapter{sessions: []string{spawnTarget, flywheel}}

	_, err := RunOrphanSweep(
		context.Background(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:        adapter,
			DaemonSpawnSession: spawnTarget,
		},
	)
	if err != nil {
		t.Logf("absent sentinel spawn-guard: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	for _, k := range adapter.killedSessions() {
		if k == spawnTarget {
			t.Fatalf("absent sentinel: spawn-target session %q was reaped — MUST be excluded (hk-7u002 regression guard)", spawnTarget)
		}
	}
}

// TestHK7u002_AbsentSentinel_ToPayload_CoordinatorSessionsReaped verifies that
// CoordinatorSessionsReaped is forwarded by ToPayload so the
// daemon_orphan_sweep_completed event carries an accurate count.
func TestHK7u002_AbsentSentinel_ToPayload_CoordinatorSessionsReaped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)
	flywheel := lifecycle.TmuxSessionName(projectHash, "flywheel")
	adapter := &hk9vp51FakeAdapter{sessions: []string{flywheel}}

	result, _ := RunOrphanSweep(
		context.Background(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxAdapter: adapter,
		},
	)

	payload := result.ToPayload()
	if payload.CoordinatorSessionsReaped != result.CoordinatorSessionsReaped {
		t.Errorf("ToPayload: CoordinatorSessionsReaped = %d, want %d",
			payload.CoordinatorSessionsReaped, result.CoordinatorSessionsReaped)
	}
	if !payload.Valid() {
		t.Errorf("ToPayload: Valid() = false for payload %+v", payload)
	}

	var _ ltmux.Adapter = (*hk9vp51FakeAdapter)(nil) // compile-time interface check
}
