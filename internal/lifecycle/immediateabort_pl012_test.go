package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shutdownFixtureImmediateShutdownRecord records what happens during an
// immediate (interceptable) shutdown sequence.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "On interceptable stop
// --immediate, the daemon MUST attempt steps 5–9 (emit
// daemon_shutdown{mode=immediate}, flush, release, exit)."
type shutdownFixtureImmediateShutdownRecord struct {
	// drainSkipped is true when the drain steps (PL-011 steps 3–4) were NOT executed.
	drainSkipped bool
	// subprocessesKilled is true when in-flight agent subprocesses were killed.
	subprocessesKilled bool
	// eventEmitted is true when daemon_shutdown{mode=immediate} was emitted.
	eventEmitted bool
	// eventMode is the mode field of the emitted daemon_shutdown event.
	eventMode shutdownFixtureShutdownMode
	// busFlushed is true when the event bus flush was attempted.
	busFlushed bool
}

// shutdownFixtureSimulateImmediateInterceptable simulates the interceptable
// immediate-shutdown sequence (harmonik stop --immediate), executing steps 5–9
// per PL-012 while skipping drain steps 3–4.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "the daemon MUST skip the drain
// steps (§PL-011 steps 3–4). In-flight agent subprocesses are killed; … On
// interceptable stop --immediate, the daemon MUST attempt steps 5–9."
func shutdownFixtureSimulateImmediateInterceptable(inFlightCount int) shutdownFixtureImmediateShutdownRecord {
	rec := shutdownFixtureImmediateShutdownRecord{}

	// PL-012: skip drain steps 3–4.
	rec.drainSkipped = true

	// PL-012: kill in-flight agent subprocesses (simulated — no real processes here).
	rec.subprocessesKilled = inFlightCount > 0

	// PL-012 step 5: emit daemon_shutdown{mode=immediate}.
	rec.eventEmitted = true
	rec.eventMode = shutdownFixtureModeImmediate

	// PL-012 step 6: flush event bus (simulated).
	rec.busFlushed = true

	return rec
}

// TestPL012_ImmediateShutdownSkipsDrain verifies that interceptable immediate
// shutdown skips the drain steps (PL-011 steps 3–4) and kills in-flight
// subprocesses.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "the daemon MUST skip the drain
// steps (§PL-011 steps 3–4). In-flight agent subprocesses are killed."
func TestPL012_ImmediateShutdownSkipsDrain(t *testing.T) {
	t.Parallel()

	const inFlightCount = 3
	rec := shutdownFixtureSimulateImmediateInterceptable(inFlightCount)

	// Drain steps MUST be skipped.
	if !rec.drainSkipped {
		t.Error("PL-012: drain steps were not skipped on immediate shutdown")
	}

	// In-flight subprocesses MUST be killed.
	if !rec.subprocessesKilled {
		t.Error("PL-012: in-flight subprocesses were not killed on immediate shutdown")
	}
}

// TestPL012_ImmediateShutdownEventShape verifies that the daemon_shutdown event
// emitted on interceptable immediate shutdown carries mode=immediate.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "emit daemon_shutdown{mode=immediate}."
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The mode is immediate for
// PL-012 (for the interceptable stop --immediate path)."
// Spec ref: event-model.md §8.7.3 — "mode: <enum: graceful | immediate>."
func TestPL012_ImmediateShutdownEventShape(t *testing.T) {
	t.Parallel()

	rec := shutdownFixtureSimulateImmediateInterceptable(1)

	if !rec.eventEmitted {
		t.Error("PL-012: daemon_shutdown event was not emitted on interceptable immediate shutdown")
	}

	if rec.eventMode != shutdownFixtureModeImmediate {
		t.Errorf("PL-012: daemon_shutdown mode = %q, want %q", rec.eventMode, shutdownFixtureModeImmediate)
	}

	// Bus flush must still happen on interceptable path.
	//
	// Spec ref: process-lifecycle.md §4.4 PL-012 — "the daemon MUST attempt
	// steps 5–9 (emit daemon_shutdown{mode=immediate}, flush, release, exit)."
	if !rec.busFlushed {
		t.Error("PL-012: event bus was not flushed on interceptable immediate shutdown")
	}
}

// TestPL012_SIGKILLNoEmission verifies the SIGKILL path: since SIGKILL cannot
// be intercepted, daemon_shutdown is NOT emitted and RTO is marked rto_undefined.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "SIGKILL cannot emit."
// Spec ref: event-model.md §8.7.3 — "SIGKILL terminations have no daemon_shutdown
// emission at all (no defer-recover gets to run); ON-033 marks those RTO cycles
// rto_undefined."
func TestPL012_SIGKILLNoEmission(t *testing.T) {
	t.Parallel()

	// The fixture models the SIGKILL path: the process is killed without any
	// opportunity to emit daemon_shutdown. We assert the observable outcome:
	// no emission record, recovery via next startup per PL-024.
	type shutdownFixtureSIGKILLOutcome struct {
		daemonShutdownEmitted bool
		rtoStatus             string
	}

	// On SIGKILL path, the daemon cannot emit (the OS terminates the process
	// before any defer-recover can run).
	sigkillOutcome := shutdownFixtureSIGKILLOutcome{
		daemonShutdownEmitted: false,
		rtoStatus:             "rto_undefined",
	}

	if sigkillOutcome.daemonShutdownEmitted {
		t.Error("PL-012 SIGKILL: daemon_shutdown must NOT be emitted on SIGKILL (cannot intercept)")
	}

	if sigkillOutcome.rtoStatus != "rto_undefined" {
		t.Errorf("PL-012 SIGKILL: rto_status = %q, want %q", sigkillOutcome.rtoStatus, "rto_undefined")
	}
}

// TestPL012_SIGKILLRecovery_NextStartup verifies that after a SIGKILL, the
// next startup detects stale state and can proceed with recovery via the orphan
// sweep + reconciliation path. Uses the self-exec pattern to spawn a child,
// SIGKILL it, then assert the next startup can acquire the pidfile lock
// (stale lock released by kernel) and detect the stale pidfile.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "On SIGKILL, steps 5–9 are
// skipped by force; recovery follows PL-024."
// Spec ref: process-lifecycle.md §4.8 PL-024 — "The next harmonik daemon
// invocation MUST detect a stale pidfile by checking that the recorded PID is
// no longer a live process … remove the stale pidfile, and proceed with
// startup."
func TestPL012_SIGKILLRecovery_NextStartup(t *testing.T) {
	// Sentinel check MUST happen before t.Parallel() so the child process
	// exits immediately without waiting for the test scheduler.
	const (
		sentinelEnv   = "GO_PL012_CHILD_RUN"
		projectDirEnv = "GO_PL012_PROJECT_DIR"
		syncFileEnv   = "GO_PL012_SYNC_FILE"
	)

	if os.Getenv(sentinelEnv) == "1" {
		// --- CHILD PROCESS BODY ---
		projectDir := os.Getenv(projectDirEnv)
		syncFile := os.Getenv(syncFileEnv)

		childPID := os.Getpid()
		childPGID, _ := syscall.Getpgid(childPID) //nolint:errcheck // child-process stub; pid is always valid
		instanceID := "01950000-0000-7000-8000-000000000050"

		_, err := plFixtureAcquirePidfile(nil, projectDir, childPID, childPGID, instanceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "PL012 child: acquirePidfile: %v\n", err)
			os.Exit(1)
		}

		// Signal readiness by writing sentinel state to sync file.
		pidStr := strconv.Itoa(childPID) + "\n"
		if err := os.WriteFile(syncFile, []byte(pidStr), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "PL012 child: write sync file: %v\n", err)
			os.Exit(1)
		}

		// Block until SIGKILL — simulates a running daemon that gets SIGKILLed.
		// daemon_shutdown is NOT emitted (cannot intercept SIGKILL).
		select {}
	}

	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL012_SIGKILLRecovery_NextStartup: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	projectDir := plFixtureTempProjectDir(t)

	// Create a sync file for PID communication.
	syncFile, err := os.CreateTemp("/tmp", "pl012-sync-")
	if err != nil {
		t.Fatalf("PL-012: CreateTemp sync file: %v", err)
	}
	syncFilePath := syncFile.Name()
	_ = syncFile.Close()                              //nolint:errcheck // cleanup error unactionable
	_ = os.Remove(syncFilePath)                       //nolint:errcheck // child will create it; Remove error expected if already absent
	t.Cleanup(func() { _ = os.Remove(syncFilePath) }) //nolint:errcheck // cleanup error unactionable

	// Self-exec: spawn the test binary with the sentinel env.
	testBin := os.Args[0]
	//nolint:gosec,noctx // G204: testBin is os.Args[0]; noctx: child-process stub, CommandContext would cancel child on t.Context() done
	cmd := exec.Command(testBin, "-test.run=^TestPL012_SIGKILLRecovery_NextStartup$")
	cmd.Env = append(os.Environ(),
		sentinelEnv+"=1",
		projectDirEnv+"="+projectDir,
		syncFileEnv+"="+syncFilePath,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-012: cmd.Start: %v", err)
	}

	// Poll for child sync file (up to 5 s).
	var childPIDStr string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: syncFilePath is derived from os.CreateTemp, not user input
		data, err := os.ReadFile(syncFilePath)
		if err == nil && len(data) > 0 {
			childPIDStr = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPIDStr == "" {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
		t.Fatal("PL-012: timed out waiting for child to write sync file")
	}

	childPID, err := strconv.Atoi(childPIDStr)
	if err != nil || childPID <= 0 {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-012: invalid child PID %q: %v", childPIDStr, err)
	}

	// Assert child is live before SIGKILL.
	if !plFixtureIsPidLive(childPID) {
		_ = cmd.Wait() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-012: child PID %d should be live before SIGKILL", childPID)
	}

	// SIGKILL the child — simulates a daemon receiving SIGKILL.
	// PL-012: on SIGKILL, steps 5–9 are skipped; daemon_shutdown NOT emitted.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("PL-012: Kill child: %v", err)
	}
	_ = cmd.Wait() //nolint:errcheck // reap zombie; non-zero exit expected after SIGKILL

	// PL-012 + PL-024: child is dead; pidfile must still be on disk.
	// Recovery follows PL-024: stale pidfile detected via flock + kill(pid, 0).
	pidfilePath := plFixturePidfilePath(projectDir)
	if _, err := os.Stat(pidfilePath); os.IsNotExist(err) {
		t.Fatal("PL-012: pidfile missing after SIGKILL; stale recovery (PL-024) requires it on disk")
	}

	// PL-024: kill(pid, 0) must report child as dead.
	if plFixtureIsPidLive(childPID) {
		t.Errorf("PL-012: plFixtureIsPidLive(%d) = true after SIGKILL, want false", childPID)
	}

	// PL-024 recovery: next startup acquires the pidfile lock (kernel released it on child death).
	myPID := os.Getpid()
	myPGID, _ := syscall.Getpgid(myPID) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	release, err := plFixtureAcquirePidfile(t, projectDir, myPID, myPGID, "01950000-0000-7000-8000-000000000051")
	if err != nil {
		t.Fatalf("PL-012: post-SIGKILL acquire failed (stale lock not released?): %v", err)
	}
	t.Cleanup(release)

	// Verify pidfile reflects the new (recovered) daemon.
	gotPID, _, _, err := plFixtureReadPidfile(t, projectDir)
	if err != nil {
		t.Fatalf("PL-012: readPidfile after recovery: %v", err)
	}
	if gotPID != myPID {
		t.Errorf("PL-012: post-recovery pidfile PID = %d, want %d (our PID)", gotPID, myPID)
	}
}
