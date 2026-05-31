package daemon

// orphansweep_pl006d_test.go — unit tests for the PL-006d coordinator sentinel
// exclusion: the orphan sweep MUST skip the flywheel tmux session when the
// supervisor sentinel is present AND the supervisor PID is live.
//
// # What is tested
//
//   - probeCoordinatorSentinel: absent sentinel → not live, no removal.
//   - probeCoordinatorSentinel: sentinel present + live PID → Live=true, no removal.
//   - probeCoordinatorSentinel: sentinel present + dead PID → Live=false, sentinel removed.
//   - probeCoordinatorSentinel: sentinel present + unreadable pidfile → Live=false, sentinel removed.
//   - RunOrphanSweep: when coordinator is live, CoordinatorSessionsSkipped=1 and
//     the flywheel session is NOT killed.
//   - RunOrphanSweep: when no sentinel exists, CoordinatorSessionsSkipped stays 0.
//   - RunOrphanSweep: when sentinel is stale (dead PID), flywheel session IS killed
//     and sentinel is removed.
//   - OrphanSweepResult.ToPayload: coordinator_sessions_skipped mapped correctly.
//
// Spec refs:
//   - process-lifecycle.md §4.2 PL-006d — coordinator sentinel exclusion.
//
// Bead: hk-9eury.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

func pl006dWriteSentinel(t *testing.T, projectDir string) {
	t.Helper()
	cognitionDir := filepath.Join(projectDir, ".harmonik", "cognition")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("pl006dWriteSentinel: MkdirAll: %v", err)
	}
	sentinelPath := filepath.Join(cognitionDir, "supervisor.sentinel")
	if err := os.WriteFile(sentinelPath, []byte("schema_version=1\n"), 0o644); err != nil {
		t.Fatalf("pl006dWriteSentinel: WriteFile sentinel: %v", err)
	}
}

func pl006dWritePidfile(t *testing.T, projectDir string, pid int) {
	t.Helper()
	cognitionDir := filepath.Join(projectDir, ".harmonik", "cognition")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("pl006dWritePidfile: MkdirAll: %v", err)
	}
	pidfilePath := filepath.Join(cognitionDir, "supervisor.pid")
	if err := os.WriteFile(pidfilePath, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatalf("pl006dWritePidfile: WriteFile: %v", err)
	}
}

func pl006dSentinelExists(projectDir string) bool {
	sentinelPath := filepath.Join(projectDir, ".harmonik", "cognition", "supervisor.sentinel")
	_, err := os.Stat(sentinelPath)
	return err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// probeCoordinatorSentinel unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPL006d_ProbeCoordinatorSentinel_Absent verifies that when no sentinel
// file exists, probeCoordinatorSentinel returns Live=false without error.
func TestPL006d_ProbeCoordinatorSentinel_Absent(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)

	result, err := probeCoordinatorSentinel(projectDir, nil)
	if err != nil {
		t.Fatalf("absent sentinel: unexpected error: %v", err)
	}
	if result.Live {
		t.Error("absent sentinel: Live = true, want false")
	}
	if result.SentinelRemoved {
		t.Error("absent sentinel: SentinelRemoved = true, want false (nothing to remove)")
	}
}

// TestPL006d_ProbeCoordinatorSentinel_LivePID verifies that when the sentinel
// is present AND the PID in supervisor.pid is the test process itself (always
// live), probeCoordinatorSentinel returns Live=true and does not remove the
// sentinel.
func TestPL006d_ProbeCoordinatorSentinel_LivePID(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, os.Getpid()) // own PID is always live

	result, err := probeCoordinatorSentinel(projectDir, nil)
	if err != nil {
		t.Fatalf("live PID: unexpected error: %v", err)
	}
	if !result.Live {
		t.Error("live PID: Live = false, want true")
	}
	if result.SentinelRemoved {
		t.Error("live PID: SentinelRemoved = true, want false (live sentinel must be kept)")
	}
	if !pl006dSentinelExists(projectDir) {
		t.Error("live PID: sentinel file was removed; must be kept when PID is live")
	}
}

// TestPL006d_ProbeCoordinatorSentinel_DeadPID verifies that when the sentinel
// is present but the PID is dead, probeCoordinatorSentinel returns Live=false
// and removes the stale sentinel.
func TestPL006d_ProbeCoordinatorSentinel_DeadPID(t *testing.T) {
	t.Parallel()

	const deadPID = 99989
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("PL-006d dead-pid: PID %d is live on this host; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, deadPID)

	result, err := probeCoordinatorSentinel(projectDir, nil)
	if err != nil {
		t.Fatalf("dead PID: unexpected error: %v", err)
	}
	if result.Live {
		t.Error("dead PID: Live = true, want false")
	}
	if !result.SentinelRemoved {
		t.Error("dead PID: SentinelRemoved = false, want true (stale sentinel must be removed)")
	}
	if pl006dSentinelExists(projectDir) {
		t.Error("dead PID: sentinel file still exists; stale sentinel must be removed")
	}
}

// TestPL006d_ProbeCoordinatorSentinel_UnreadablePidfile verifies that when
// the sentinel is present but the pidfile is missing, the probe treats it as
// stale and removes the sentinel.
func TestPL006d_ProbeCoordinatorSentinel_UnreadablePidfile(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	pl006dWriteSentinel(t, projectDir)
	// No pidfile — probeCoordinatorSentinel cannot read the PID.

	result, err := probeCoordinatorSentinel(projectDir, nil)
	if err != nil {
		t.Fatalf("unreadable pidfile: unexpected error: %v", err)
	}
	if result.Live {
		t.Error("unreadable pidfile: Live = true, want false")
	}
	if !result.SentinelRemoved {
		t.Error("unreadable pidfile: SentinelRemoved = false, want true (stale sentinel must be removed)")
	}
	if pl006dSentinelExists(projectDir) {
		t.Error("unreadable pidfile: sentinel file still exists after removal")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RunOrphanSweep PL-006d integration tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPL006d_RunOrphanSweep_LiveCoordinatorSkipped verifies that when the
// supervisor sentinel is present and the PID is live, RunOrphanSweep sets
// CoordinatorSessionsSkipped=1 and does NOT kill the flywheel session.
func TestPL006d_RunOrphanSweep_LiveCoordinatorSkipped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	// Write sentinel + own PID (always live).
	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, os.Getpid())

	// Lister presents the flywheel session.
	flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")
	killer := &daemonOrphanSweepFakeTmuxKiller{}
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{flywheelSession}}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxLister: lister,
			TmuxKiller: killer,
		},
	)
	if err != nil {
		// git worktree prune fails on a non-git temp dir; that is expected.
		t.Logf("live coordinator: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsSkipped != 1 {
		t.Errorf("live coordinator: CoordinatorSessionsSkipped = %d, want 1", result.CoordinatorSessionsSkipped)
	}
	for _, killed := range killer.killed {
		if killed == flywheelSession {
			t.Errorf("live coordinator: flywheel session %q was killed; must be skipped (PL-006d)", flywheelSession)
		}
	}
	if result.TmuxSessionsKilled != 0 {
		t.Errorf("live coordinator: TmuxSessionsKilled = %d, want 0", result.TmuxSessionsKilled)
	}
}

// TestPL006d_RunOrphanSweep_NoSentinel_ZeroSkipped verifies that when no
// sentinel exists, CoordinatorSessionsSkipped remains 0.
func TestPL006d_RunOrphanSweep_NoSentinel_ZeroSkipped(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	killer := &daemonOrphanSweepFakeTmuxKiller{}
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: nil}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxLister: lister,
			TmuxKiller: killer,
		},
	)
	if err != nil {
		// git worktree prune fails on a non-git temp dir; that is expected.
		t.Logf("no sentinel: RunOrphanSweep error (possibly worktree prune): %v", err)
	}
	if result.CoordinatorSessionsSkipped != 0 {
		t.Errorf("no sentinel: CoordinatorSessionsSkipped = %d, want 0", result.CoordinatorSessionsSkipped)
	}
}

// TestPL006d_RunOrphanSweep_StaleCoordinator_SessionKilled verifies that when
// the sentinel is present but the PID is dead, the flywheel session IS killed
// (treated as an ordinary orphan) and the stale sentinel is removed.
func TestPL006d_RunOrphanSweep_StaleCoordinator_SessionKilled(t *testing.T) {
	t.Parallel()

	const deadPID = 99987
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("PL-006d stale coordinator: PID %d is live; skipping", deadPID)
	}

	projectDir := daemonOrphanSweepTempProjectDir(t)
	projectHash := lifecycle.ComputeProjectHash(projectDir)

	// Write sentinel + dead PID.
	pl006dWriteSentinel(t, projectDir)
	pl006dWritePidfile(t, projectDir, deadPID)

	flywheelSession := lifecycle.TmuxSessionName(projectHash, "flywheel")
	killer := &daemonOrphanSweepFakeTmuxKiller{}
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{flywheelSession}}

	result, err := RunOrphanSweep(
		t.Context(),
		projectDir,
		projectHash,
		time.Now(),
		OrphanSweepConfig{
			TmuxLister: lister,
			TmuxKiller: killer,
		},
	)
	if err != nil {
		// git worktree prune fails on a non-git temp dir; that is expected.
		t.Logf("stale coordinator: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.CoordinatorSessionsSkipped != 0 {
		t.Errorf("stale coordinator: CoordinatorSessionsSkipped = %d, want 0", result.CoordinatorSessionsSkipped)
	}
	if result.TmuxSessionsKilled != 1 {
		t.Errorf("stale coordinator: TmuxSessionsKilled = %d, want 1 (stale flywheel must be killed)", result.TmuxSessionsKilled)
	}
	if pl006dSentinelExists(projectDir) {
		t.Error("stale coordinator: sentinel still exists; stale sentinel must be removed")
	}
	// Payload must carry coordinator_sessions_skipped = 0.
	payload := result.ToPayload()
	if payload.CoordinatorSessionsSkipped != 0 {
		t.Errorf("stale coordinator: payload.CoordinatorSessionsSkipped = %d, want 0", payload.CoordinatorSessionsSkipped)
	}
}

// TestPL006d_ToPayload_CoordinatorSessionsSkipped verifies that the
// coordinator_sessions_skipped field is correctly mapped by ToPayload.
func TestPL006d_ToPayload_CoordinatorSessionsSkipped(t *testing.T) {
	t.Parallel()

	result := OrphanSweepResult{
		CoordinatorSessionsSkipped: 3,
		SweptAt:                    time.Now(),
	}
	payload := result.ToPayload()
	if payload.CoordinatorSessionsSkipped != 3 {
		t.Errorf("ToPayload: CoordinatorSessionsSkipped = %d, want 3", payload.CoordinatorSessionsSkipped)
	}
}
