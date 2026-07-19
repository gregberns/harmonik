package lifecycle

// orphansweep_w4_pidreuse_test.go — Wave-4 mega-review §c regressions for the
// lifecycle sweeps:
//
//  1. PID-reuse guard: the SIGTERM→SIGKILL escalation in SweepOrphanBr,
//     SweepOrphanHandlers, and ReapPriorAgentFollowWatchers must re-verify a
//     candidate's identity (fresh enumeration) before SIGKILL, so a recycled
//     PID belonging to an unrelated process is never SIGKILLed. Simulated with
//     a sequenced fake lister whose second enumeration no longer contains the
//     PID, plus a real SIGTERM-ignoring child process standing in for the
//     "innocent process that inherited the PID".
//
//  2. Stale reconciliation-lock sweep skips (does NOT remove) a lock file
//     whose creator_pid cannot be parsed — staleness requires proving the
//     recorded creator PID dead, which is impossible without a PID (PL-006).
//
//  3. EnumerateStaleIntents counts only `*.json` intent files — directories,
//     atomic-write temp files (`*.json.tmp-*`), and other stray entries are
//     excluded (BI-030 intent files are `<key>.json`).

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// w4SeqBrLister returns a different PID list per ListOrphanBrPIDs call.
type w4SeqBrLister struct {
	seq   [][]int
	calls int
}

func (l *w4SeqBrLister) ListOrphanBrPIDs(_ context.Context) ([]int, error) {
	i := l.calls
	l.calls++
	if i >= len(l.seq) {
		return l.seq[len(l.seq)-1], nil
	}
	return l.seq[i], nil
}

// w4SeqHandlerPIDLister returns a different PID list per
// ListOrphanHandlerPIDs call.
type w4SeqHandlerPIDLister struct {
	seq   [][]int
	calls int
}

func (l *w4SeqHandlerPIDLister) ListOrphanHandlerPIDs(_ context.Context, _ core.ProjectHash) ([]int, error) {
	i := l.calls
	l.calls++
	if i >= len(l.seq) {
		return l.seq[len(l.seq)-1], nil
	}
	return l.seq[i], nil
}

// w4SeqWatcherLister returns a different PID list per
// ListAgentFollowWatcherPIDs call.
type w4SeqWatcherLister struct {
	seq   [][]int
	calls int
}

func (l *w4SeqWatcherLister) ListAgentFollowWatcherPIDs(_ context.Context, _ string) ([]int, error) {
	i := l.calls
	l.calls++
	if i >= len(l.seq) {
		return l.seq[len(l.seq)-1], nil
	}
	return l.seq[i], nil
}

// w4StartTermIgnoringProc spawns a shell that ignores SIGTERM, waits until the
// trap is installed (via a "ready" line on stdout), and registers cleanup that
// SIGKILLs it. It stands in for an unrelated process that inherited a recycled
// PID: it survives the SIGTERM phase, so only the SIGKILL phase distinguishes
// a guarded sweep (process survives) from an unguarded one (process killed).
func w4StartTermIgnoringProc(t *testing.T) int {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `trap '' TERM; echo ready; sleep 60`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start term-ignoring proc: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //nolint:errcheck // best-effort cleanup of test subprocess
		_ = cmd.Wait()         //nolint:errcheck // best-effort cleanup of test subprocess
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatalf("term-ignoring proc did not report ready")
	}
	return cmd.Process.Pid
}

// TestSweepOrphanBr_PIDReuseGuard_NoSIGKILLWhenIdentityGone verifies that a
// candidate PID that no longer appears in a fresh enumeration at SIGKILL time
// (its PID was recycled by a non-matching process, simulated by the sequenced
// lister dropping it) is NOT SIGKILLed and not reported as a survivor.
//
// Not parallel: mutates the package-level grace/poll vars.
func TestSweepOrphanBr_PIDReuseGuard_NoSIGKILLWhenIdentityGone(t *testing.T) {
	oldGrace, oldPoll := orphanSweepGracePeriod, orphanSweepPollInterval
	orphanSweepGracePeriod, orphanSweepPollInterval = 300*time.Millisecond, 20*time.Millisecond
	defer func() { orphanSweepGracePeriod, orphanSweepPollInterval = oldGrace, oldPoll }()

	pid := w4StartTermIgnoringProc(t)
	lister := &w4SeqBrLister{seq: [][]int{{pid}, {}}} // gone on re-enumeration

	survived, err := SweepOrphanBr(t.Context(), lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("SweepOrphanBr: unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("survived = %v, want [] (dropped candidate must not be a survivor)", survived)
	}
	if lister.calls < 2 {
		t.Errorf("lister called %d time(s); want a fresh re-enumeration before SIGKILL", lister.calls)
	}
	if !orphanSweepIsPidLive(pid) {
		t.Error("PID-reuse regression: process absent from fresh enumeration was SIGKILLed")
	}
}

// TestSweepOrphanHandlers_PIDReuseGuard_NoSIGKILLWhenIdentityGone is the
// SweepOrphanHandlers analogue of the br-sweep PID-reuse guard test.
//
// Not parallel: mutates the package-level grace/poll vars.
func TestSweepOrphanHandlers_PIDReuseGuard_NoSIGKILLWhenIdentityGone(t *testing.T) {
	oldGrace, oldPoll := handlerSweepGracePeriod, handlerSweepPollInterval
	handlerSweepGracePeriod, handlerSweepPollInterval = 300*time.Millisecond, 20*time.Millisecond
	defer func() { handlerSweepGracePeriod, handlerSweepPollInterval = oldGrace, oldPoll }()

	pid := w4StartTermIgnoringProc(t)
	lister := &w4SeqHandlerPIDLister{seq: [][]int{{pid}, {}}}

	killed, err := SweepOrphanHandlers(t.Context(), "deadbeef", lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("SweepOrphanHandlers: unexpected error: %v", err)
	}
	_ = killed
	if lister.calls < 2 {
		t.Errorf("lister called %d time(s); want a fresh re-enumeration before SIGKILL", lister.calls)
	}
	if !orphanSweepIsPidLive(pid) {
		t.Error("PID-reuse regression: process absent from fresh enumeration was SIGKILLed")
	}
}

// TestReapPriorAgentFollowWatchers_PIDReuseGuard_NoSIGKILLWhenIdentityGone is
// the watcher-reap analogue of the PID-reuse guard test.
//
// Not parallel: mutates the package-level grace/poll vars.
func TestReapPriorAgentFollowWatchers_PIDReuseGuard_NoSIGKILLWhenIdentityGone(t *testing.T) {
	oldGrace, oldPoll := agentWatcherReapGracePeriod, agentWatcherReapPollInterval
	agentWatcherReapGracePeriod, agentWatcherReapPollInterval = 300*time.Millisecond, 20*time.Millisecond
	defer func() { agentWatcherReapGracePeriod, agentWatcherReapPollInterval = oldGrace, oldPoll }()

	pid := w4StartTermIgnoringProc(t)
	lister := &w4SeqWatcherLister{seq: [][]int{{pid}, {}}}

	survived, err := ReapPriorAgentFollowWatchers(t.Context(), lister, "captain", 0, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("ReapPriorAgentFollowWatchers: unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("survived = %v, want []", survived)
	}
	if lister.calls < 2 {
		t.Errorf("lister called %d time(s); want a fresh re-enumeration before SIGKILL", lister.calls)
	}
	if !orphanSweepIsPidLive(pid) {
		t.Error("PID-reuse regression: process absent from fresh enumeration was SIGKILLed")
	}
}

// TestSweepStaleReconciliationLocks_MalformedCreatorPIDIsSkipped verifies the
// sweep-level behavior for a lock file whose creator_pid cannot be parsed: the
// file is skipped (left on disk), not removed — staleness requires proving the
// recorded creator PID dead (PL-006), which is impossible without a PID.
func TestSweepStaleReconciliationLocks_MalformedCreatorPIDIsSkipped(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil { //nolint:gosec // G301: test fixture dir in t.TempDir(), perms not security-relevant
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockDir, "run-garbled.lock")
	if err := os.WriteFile(lockPath, []byte("creator_pid=not-a-number\nrun_id=run-garbled\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := SweepStaleReconciliationLocks(projectDir, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("SweepStaleReconciliationLocks: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0 (unparseable creator_pid must be skipped)", result.Removed)
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("lock file was removed despite unparseable creator_pid: %v", statErr)
	}
}

// TestSweepStaleReconciliationLocks_HoldsFlockUntilUnlink verifies the RC-002a
// end-to-end behavior via the sweep: a genuinely stale lock (dead creator PID,
// flock acquirable) is removed. The hold-across-unlink property itself is
// asserted at the unit level in TestReconLockProbeStale_DeadPIDIsStaleAndHoldsFlock.
func TestSweepStaleReconciliationLocks_HoldsFlockUntilUnlink(t *testing.T) {
	t.Parallel()

	deadPID := reconLockUpliftFindDeadPID(t)
	projectDir := t.TempDir()
	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil { //nolint:gosec // G301: test fixture dir in t.TempDir(), perms not security-relevant
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockDir, "run-stale.lock")
	content := fmt.Sprintf("creator_pid=%d\nrun_id=run-stale\n", deadPID)
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := SweepStaleReconciliationLocks(projectDir, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("SweepStaleReconciliationLocks: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Errorf("stale lock still present: statErr=%v", statErr)
	}
	// No verdict-executed line: run must be routed Cat 3b.
	if len(result.Cat3bRunIDs) != 1 || result.Cat3bRunIDs[0] != "run-stale" {
		t.Errorf("Cat3bRunIDs = %v, want [run-stale]", result.Cat3bRunIDs)
	}
}

// TestEnumerateStaleIntents_FiltersNonIntentEntries verifies that only
// `*.json` files are counted: directories, `*.json.tmp-*` atomic-write temp
// files, and other stray entries are excluded even when older than the daemon
// start time.
func TestEnumerateStaleIntents_FiltersNonIntentEntries(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	if err := os.MkdirAll(filepath.Join(intentsDir, "subdir"), 0o755); err != nil { //nolint:gosec // G301: test fixture dir in t.TempDir(), perms not security-relevant
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, name := range []string{"real-intent.json", "inflight.json.tmp-deadbeef", "stray.txt"} {
		p := filepath.Join(intentsDir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(intentsDir, "subdir"), old, old); err != nil {
		t.Fatal(err)
	}

	count, err := EnumerateStaleIntents(projectDir, time.Now())
	if err != nil {
		t.Fatalf("EnumerateStaleIntents: unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only real-intent.json)", count)
	}
}
