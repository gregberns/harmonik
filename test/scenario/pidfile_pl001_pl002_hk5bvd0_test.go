//go:build scenario

package scenario

// pidfile_pl001_pl002_hk5bvd0_test.go — scenario tests for PL-001 and PL-002
// pidfile lifecycle invariants (single-flywheel supervise lock, hk-li14r).
//
// PL-001: A second daemon started against the same project while the first
//         daemon holds the pidfile lock MUST return lifecycle.ErrPidfileLocked,
//         which the composition root maps to exit code 5 ("pidfile-locked").
//
// PL-002: A stale pidfile (present on disk, PID dead, advisory flock NOT held)
//         MUST be overwritten by the next daemon startup, which proceeds cleanly
//         and emits daemon_started.
//
// # Why this is a scenario test (not a unit test)
//
// The existing unit tests in internal/daemon/daemon_test.go
// (TestDaemonStart_PidfileBlocksSecondInvocation) test the daemon.Start API in
// isolation. These scenario tests cover the same invariants end-to-end:
//   - PL-001 exercises the full lifecycle from "first daemon holds lock" through
//     "second daemon.Start fails" with testify/require assertions.
//   - PL-002 exercises the full recovery cycle: stale file on disk → daemon.Start
//     overwrites it → daemon_started emitted → pidfile readable with current PID.
//
// # Design
//
// Both tests use daemon.Start in-process (established convention in
// test/scenario/; see harness_test.go §Design decisions). The harmonik binary
// requires $TMUX and a real tmux session, making subprocess tests brittle in CI.
// daemon.Start with BrPath="" skips the work loop and returns promptly, which is
// sufficient for these lifecycle-layer assertions.
//
// The exit-code mapping (ErrPidfileLocked → exit code 5 per PL-008a) lives in
// cmd/harmonik/main.go and is separately covered by main_test.go.
//
// Coordinate with (NOT dep on) epic hk-fgy9o (P1 crash-recovery pidfile-lock
// unit tests; this file is the scenario-level complement).
//
// Spec refs: specs/process-lifecycle.md §4.1 PL-001, PL-002, PL-002a, PL-008a,
//            PL-024.
// Bead refs: hk-5bvd0, hk-li14r.
// Helper prefix: pfScenario (per implementer-protocol.md §Helper-prefix).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ─────────────────────────────────────────────────────────────────────────────
// PL-001: second daemon returns ErrPidfileLocked (→ exit code 5)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_PL001_SecondDaemonPidfileLocked verifies that when a daemon
// already holds the advisory pidfile lock for a project, a second daemon.Start
// call against the same ProjectDir returns lifecycle.ErrPidfileLocked
// (unwrapped from the error chain).
//
// This is the scenario-level assertion for PL-001: the single-flywheel
// supervise lock (hk-li14r) guarantees at most one daemon per project directory
// is ever active. Two racing daemons would corrupt the shared queue file.
//
// The exit-code mapping (ErrPidfileLocked → exit code 5 per PL-008a) lives in
// cmd/harmonik/main.go. This test exercises the upstream lifecycle contract at
// the daemon.Start API boundary.
//
// Assertions:
//  1. lifecycle.AcquirePidfile succeeds (simulates first daemon holding lock).
//  2. daemon.Start returns non-nil error.
//  3. errors.Is(err, lifecycle.ErrPidfileLocked) is true.
//  4. daemon.Start returns promptly (< 5 s; flock(LOCK_NB) must not block).
//
// Spec refs: process-lifecycle.md §4.1 PL-001, PL-002a, PL-008a.
// Bead refs: hk-5bvd0, hk-li14r.
func TestScenario_PL001_SecondDaemonPidfileLocked(t *testing.T) {
	t.Parallel()

	proj := pfScenarioProjectDir(t)

	// Acquire the pidfile from this goroutine to simulate a running first daemon
	// holding the advisory lock.
	pid := os.Getpid()
	pgid, err := syscall.Getpgid(pid)
	require.NoError(t, err, "PL-001: Getpgid must not fail for current process")

	pf, err := lifecycle.AcquirePidfile(proj.projectDir, pid, pgid, "pl001-holder-instance")
	require.NoError(t, err, "PL-001: first AcquirePidfile must succeed (no contention)")
	defer func() { _ = pf.Release() }()

	// Second daemon.Start must fail fast with ErrPidfileLocked.
	//
	// flock(LOCK_EX|LOCK_NB) returns EAGAIN/EWOULDBLOCK immediately when another
	// fd holds the lock — there is no blocking wait. Run in a goroutine with a
	// timeout guard so a regression that introduces a blocking flock would not
	// hang the test suite indefinitely.
	cfg := daemon.Config{
		ProjectDir:   proj.projectDir,
		JSONLLogPath: proj.jsonlPath,
	}

	type startResult struct{ err error }
	ch := make(chan startResult, 1)
	go func() {
		ch <- startResult{err: daemon.Start(t.Context(), cfg)}
	}()

	var result startResult
	select {
	case result = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("PL-001: daemon.Start did not return within 5 s; " +
			"LOCK_NB must not block on contested pidfile — regression in flock logic")
	}

	require.Error(t, result.err,
		"PL-001: daemon.Start must return non-nil error when pidfile lock is already held")
	require.True(t, errors.Is(result.err, lifecycle.ErrPidfileLocked),
		"PL-001: errors.Is(err, lifecycle.ErrPidfileLocked) must be true; "+
			"this error maps to exit code 5 per PL-008a; got: %v", result.err)
}

// ─────────────────────────────────────────────────────────────────────────────
// PL-002: stale pidfile is overwritten; daemon starts cleanly
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_PL002_StalePidfileRecovery verifies that a stale pidfile — a
// file present on disk from a crashed prior daemon with a dead PID recorded and
// no advisory flock held — is detected and overwritten by the next daemon.Start,
// which proceeds cleanly and emits daemon_started.
//
// "Stale" is defined by the two-condition probe in PL-002a / PL-024:
//   - flock(LOCK_EX|LOCK_NB) succeeds (no live lock holder), AND
//   - kill(recorded_pid, 0) returns ESRCH (recorded PID is dead).
//
// AcquirePidfile handles this naturally: it acquires the flock (succeeds because
// no one holds it), truncates the stale content, and writes fresh three-line
// content (PID/PGID/instanceID). RemoveStalePidfile is the explicit cleanup
// helper; AcquirePidfile's truncate-rewrite achieves the same observable effect.
//
// Assertions:
//  1. Stale pidfile written before daemon.Start with a dead PID (no flock).
//  2. daemon.Start returns nil (no ErrPidfileLocked or other startup error).
//  3. daemon_started event present in the JSONL log.
//  4. Pidfile readable via ReadPidfile with PID == os.Getpid() (stale PID gone).
//
// Spec refs: process-lifecycle.md §4.1 PL-002, PL-002a, PL-024, hk-li14r.
// Bead refs: hk-5bvd0, hk-li14r.
func TestScenario_PL002_StalePidfileRecovery(t *testing.T) {
	t.Parallel()

	proj := pfScenarioProjectDir(t)

	// Obtain a reliably dead PID by spawning a subprocess that exits immediately
	// and waiting for kill(pid, 0) to return ESRCH.
	deadPID := pfScenarioDeadPID(t)

	// Write a stale pidfile: three-line format (PL-002b), dead PID, no flock.
	// The file must exist on disk before daemon.Start so that AcquirePidfile
	// opens an existing inode rather than creating a fresh one.
	pidfilePath := filepath.Join(proj.projectDir, ".harmonik", "daemon.pid")
	staleContent := fmt.Sprintf("%d\n%d\nstale-instance-pl002\n", deadPID, deadPID)
	require.NoError(t,
		os.WriteFile(pidfilePath, []byte(staleContent), 0o600),
		"PL-002: writing stale pidfile must succeed")

	// Confirm the stale PID is dead before proceeding.
	killErr := syscall.Kill(deadPID, 0)
	require.True(t, errors.Is(killErr, syscall.ESRCH),
		"PL-002: dead PID %d must return ESRCH from kill(0); got %v — "+
			"cannot safely test stale-pidfile path with a live PID", deadPID, killErr)

	// daemon.Start with BrPath="" skips the work loop and returns promptly.
	cfg := daemon.Config{
		ProjectDir:   proj.projectDir,
		JSONLLogPath: proj.jsonlPath,
	}
	startErr := daemon.Start(t.Context(), cfg)

	require.NoError(t, startErr,
		"PL-002: daemon.Start must return nil when recovering from a stale pidfile; "+
			"got error — stale-pidfile recovery must not be mistaken for an active lock")

	// Assert: daemon_started event is present in the JSONL log.
	lines := scenarioFixtureReadJSONLLines(t, proj.jsonlPath)
	found := false
	for _, line := range lines {
		if strings.Contains(line, "daemon_started") {
			found = true
			break
		}
	}
	require.True(t, found,
		"PL-002: daemon_started event must be emitted after stale-pidfile recovery; "+
			"JSONL lines: %v", lines)

	// Assert: pidfile now contains the current process's PID (stale PID gone).
	gotPID, _, _, readErr := lifecycle.ReadPidfile(proj.projectDir)
	require.NoError(t, readErr,
		"PL-002: ReadPidfile must succeed after stale-pidfile recovery")
	require.Equal(t, os.Getpid(), gotPID,
		"PL-002: pidfile PID must equal os.Getpid() after recovery; "+
			"stale PID was %d — AcquirePidfile must have overwritten stale content", deadPID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (pfScenario prefix per implementer-protocol.md §Helper-prefix)
// ─────────────────────────────────────────────────────────────────────────────

// pfScenarioProjectPaths holds the temp-dir paths for a single scenario.
type pfScenarioProjectPaths struct {
	projectDir string
	jsonlPath  string
}

// pfScenarioProjectDir creates a minimal harmonik project directory:
//   - .harmonik/events/       (JSONL event log location)
//   - .harmonik/beads-intents/ (intent-log protocol)
//
// Uses a short /tmp path when t.TempDir() would exceed the macOS 104-byte
// sun_path limit for Unix domain sockets (sockaddr_un.sun_path).
func pfScenarioProjectDir(t *testing.T) pfScenarioProjectPaths {
	t.Helper()

	const sunPathMax = 104
	const sockRelPath = "/.harmonik/daemon.sock"

	candidate := t.TempDir()
	var projectDir string
	if len(candidate)+len(sockRelPath) <= sunPathMax {
		projectDir = candidate
	} else {
		dir, mkErr := os.MkdirTemp("/tmp", "pf-pl-")
		if mkErr != nil {
			t.Fatalf("pfScenarioProjectDir: MkdirTemp: %v", mkErr)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		projectDir = dir
	}

	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); mkErr != nil {
			t.Fatalf("pfScenarioProjectDir: MkdirAll %s: %v", sub, mkErr)
		}
	}

	return pfScenarioProjectPaths{
		projectDir: projectDir,
		jsonlPath:  filepath.Join(projectDir, ".harmonik", "events", "events.jsonl"),
	}
}

// pfScenarioDeadPID spawns a subprocess that exits immediately and returns its
// PID. The function polls kill(pid, 0) until ESRCH is returned, confirming the
// PID is no longer in the kernel process table.
//
// This gives the caller a reliably dead PID for constructing stale pidfiles.
// Using a subprocess avoids hard-coded PID magic numbers that may be live on
// some systems.
func pfScenarioDeadPID(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("/bin/sh", "-c", "exit 0") //nolint:gosec // G204: args are hard-coded constants
	require.NoError(t, cmd.Run(), "pfScenarioDeadPID: subprocess must exit 0")

	pid := cmd.ProcessState.Pid()

	// Poll until kill(pid, 0) returns ESRCH. cmd.Run() calls Wait() internally
	// so the zombie is reaped; ESRCH is expected almost immediately. The 2-second
	// deadline guards against PID recycling on a heavily loaded CI host.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(pid, 0); errors.Is(killErr, syscall.ESRCH) {
			return pid
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("pfScenarioDeadPID: PID %d still responds to kill(0) after 2 s; "+
		"cannot construct a reliably dead PID for PL-002 stale-pidfile test", pid)
	return 0 // unreachable
}
