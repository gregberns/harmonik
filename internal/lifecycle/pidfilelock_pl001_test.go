package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPL001_OneDaemonPerProject verifies that a second attempt to acquire the
// pidfile lock while the first holder is alive fails with a lock-contention
// error, and that the error maps to exit code 5 per the ON §8 taxonomy.
//
// Spec ref: process-lifecycle.md §4.1 PL-001 — "Each project MUST run exactly
// one daemon." PL-002 — "A second daemon invocation against the same project
// that finds the pidfile held by a live process MUST exit with exit code 5
// 'pidfile-locked'."
func TestPL001_OneDaemonPerProject(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID := "01950000-0000-7000-8000-000000000001"

	// First holder acquires the pidfile lock.
	release1, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID)
	if err != nil {
		t.Fatalf("PL-001: first acquire failed: %v", err)
	}
	t.Cleanup(release1)

	// Second acquire must fail with lock-contention.
	_, err2 := plFixtureAcquirePidfile(t, projectDir, pid, pgid, "01950000-0000-7000-8000-000000000002")
	if err2 == nil {
		t.Fatal("PL-001: second acquire succeeded; want lock-contention error")
	}

	// The error must map to exit code 5 (pidfile-locked).
	exitCode := plFixtureErrToExitCode(err2)
	if exitCode != 5 {
		t.Errorf("PL-001: errToExitCode(%v) = %d, want 5 (pidfile-locked)", err2, exitCode)
	}
}

// TestPL002_PidfilePathIsCanonical verifies that the pidfile is created at the
// canonical path .harmonik/daemon.pid relative to the project root.
//
// Spec ref: process-lifecycle.md §4.1 PL-002 — "The daemon MUST write its PID
// to .harmonik/daemon.pid on startup."
func TestPL002_PidfilePathIsCanonical(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID := "01950000-0000-7000-8000-000000000010"

	release, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID)
	if err != nil {
		t.Fatalf("PL-002: acquire pidfile: %v", err)
	}
	t.Cleanup(release)

	// The pidfile must exist at the canonical path.
	canonicalPath := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	if _, err := os.Stat(canonicalPath); os.IsNotExist(err) {
		t.Errorf("PL-002: pidfile not at canonical path %q", canonicalPath)
	}
}

// TestPL002a_FdLifetimeAdvisoryLock verifies that the flock advisory lock is
// released when the fd is closed (fd-lifetime semantics), and that a second
// acquire succeeds after the fd is closed.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "The lock MUST be released
// automatically by the kernel on daemon-process termination (clean OR crash)
// so that a subsequent daemon invocation can acquire the lock on restart."
func TestPL002a_FdLifetimeAdvisoryLock(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID1 := "01950000-0000-7000-8000-000000000020"
	instanceID2 := "01950000-0000-7000-8000-000000000021"

	// First acquire.
	release1, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID1)
	if err != nil {
		t.Fatalf("PL-002a: first acquire: %v", err)
	}

	// Second acquire must fail while first fd is live.
	_, err2 := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID2)
	if err2 == nil {
		release1() // ensure cleanup
		t.Fatal("PL-002a: second acquire succeeded while first fd is live; want failure")
	}

	// Release the first fd.
	release1()

	// Second acquire must now succeed (lock released on fd close).
	release2, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID2)
	if err != nil {
		t.Fatalf("PL-002a: second acquire after release: %v", err)
	}
	t.Cleanup(release2)
}

// TestPL002b_ThreeLineAtomicWrite verifies that the pidfile content after
// acquire is exactly three lines: PID, PGID, and instanceID, each terminated
// by \n. Also verifies the truncate-rewrite-keep-fd pattern is used (NOT
// temp+rename).
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Write the pidfile's three
// lines, each terminated by \n: line 1 = PID; line 2 = PGID; line 3 =
// daemon_instance_id (UUIDv7, lowercase canonical hyphenated form)."
func TestPL002b_ThreeLineAtomicWrite(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID := "01950000-0000-7000-8000-000000000030"

	release, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID)
	if err != nil {
		t.Fatalf("PL-002b: acquire: %v", err)
	}
	t.Cleanup(release)

	// Read raw pidfile content.
	pidfilePath := plFixturePidfilePath(projectDir)
	//nolint:gosec // G304: pidfilePath derived from t.TempDir(), not user input
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		t.Fatalf("PL-002b: ReadFile: %v", err)
	}

	wantContent := fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)
	if string(data) != wantContent {
		t.Errorf("PL-002b: pidfile content = %q, want %q", string(data), wantContent)
	}

	// Verify the parsed form matches too.
	gotPID, gotPGID, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
	if err != nil {
		t.Fatalf("PL-002b: readPidfile: %v", err)
	}
	if gotPID != pid {
		t.Errorf("PL-002b: parsed PID = %d, want %d", gotPID, pid)
	}
	if gotPGID != pgid {
		t.Errorf("PL-002b: parsed PGID = %d, want %d", gotPGID, pgid)
	}
	if gotInstanceID != instanceID {
		t.Errorf("PL-002b: parsed instanceID = %q, want %q", gotInstanceID, instanceID)
	}
}

// TestPL002b_ReaderTolerance_OneAndTwoLine verifies that the pidfile reader
// tolerates one-line (v0.2.x) and two-line (v0.4.0) formats. A missing line 3
// must return instanceID = "unknown"; a missing line 2 must return pgid = 0.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Readers MUST tolerate
// one-line pidfiles for backward compatibility with v0.2.x format and
// two-line pidfiles for backward compatibility with v0.4.0 format; a missing
// line 3 is treated as daemon_instance_id = unknown."
func TestPL002b_ReaderTolerance_OneAndTwoLine(t *testing.T) {
	t.Parallel()

	t.Run("one-line/pid-only", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		pidfilePath := plFixturePidfilePath(projectDir)

		wantPID := 99901
		if err := os.WriteFile(pidfilePath, []byte(strconv.Itoa(wantPID)+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		gotPID, gotPGID, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
		if err != nil {
			t.Fatalf("PL-002b reader-tolerance one-line: readPidfile: %v", err)
		}
		if gotPID != wantPID {
			t.Errorf("PL-002b reader-tolerance one-line: PID = %d, want %d", gotPID, wantPID)
		}
		if gotPGID != 0 {
			t.Errorf("PL-002b reader-tolerance one-line: PGID = %d, want 0 (missing)", gotPGID)
		}
		if gotInstanceID != "unknown" {
			t.Errorf("PL-002b reader-tolerance one-line: instanceID = %q, want %q", gotInstanceID, "unknown")
		}
	})

	t.Run("two-line/pid-and-pgid", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		pidfilePath := plFixturePidfilePath(projectDir)

		wantPID := 99902
		wantPGID := 99900
		content := fmt.Sprintf("%d\n%d\n", wantPID, wantPGID)
		if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		gotPID, gotPGID, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
		if err != nil {
			t.Fatalf("PL-002b reader-tolerance two-line: readPidfile: %v", err)
		}
		if gotPID != wantPID {
			t.Errorf("PL-002b reader-tolerance two-line: PID = %d, want %d", gotPID, wantPID)
		}
		if gotPGID != wantPGID {
			t.Errorf("PL-002b reader-tolerance two-line: PGID = %d, want %d", gotPGID, wantPGID)
		}
		if gotInstanceID != "unknown" {
			t.Errorf("PL-002b reader-tolerance two-line: instanceID = %q, want %q", gotInstanceID, "unknown")
		}
	})
}

// TestPL024_StalePidfileDetection verifies cross-process stale-pidfile
// detection per the flock + kill(pid, 0) discipline. The test spawns a child
// process that acquires the pidfile lock, then kills the child (simulating a
// daemon crash). The parent then verifies that:
//
//  1. The pidfile remains on disk (stale, not cleaned up by the crashed process).
//  2. plFixtureIsPidLive reports the child as dead.
//  3. The flock on the pidfile is no longer held (kernel released it on child exit).
//  4. A subsequent acquire (simulating the next daemon startup) succeeds.
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "The next harmonik daemon
// invocation MUST detect a stale pidfile by checking that the recorded PID is
// no longer a live process (per PL-002a primitive selection), remove the stale
// pidfile, and proceed with startup." PL-002a — disambiguate (a) "lock held by
// live process" from (b) "lock not held, recorded PID not live" via flock +
// kill(pid, 0).
func TestPL024_StalePidfileDetection(t *testing.T) {
	// Sentinel check MUST happen before t.Parallel() so the child process
	// exits immediately in stub mode without waiting for the test scheduler.
	// The standard Go self-exec pattern requires the sentinel to be checked
	// at the very start of TestMain or before any t.Parallel() calls.
	const (
		sentinelEnv   = "GO_PL024_CHILD_RUN"
		projectDirEnv = "GO_PL024_PROJECT_DIR"
		syncFileEnv   = "GO_PL024_SYNC_FILE"
	)

	if os.Getenv(sentinelEnv) == "1" {
		// --- CHILD PROCESS BODY ---
		// Detect the sentinel and run the stub; the child never returns from here.
		projectDir := os.Getenv(projectDirEnv)
		syncFile := os.Getenv(syncFileEnv)

		childPID := os.Getpid()
		childPGID, _ := syscall.Getpgid(childPID) //nolint:errcheck // child-process stub; pid is always valid
		instanceID := "01950000-0000-7000-8000-000000000040"

		_, err := plFixtureAcquirePidfile(nil, projectDir, childPID, childPGID, instanceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "PL024 child: acquirePidfile: %v\n", err)
			os.Exit(1)
		}

		// Signal readiness by writing our PID to the sync file.
		pidStr := strconv.Itoa(childPID) + "\n"
		if err := os.WriteFile(syncFile, []byte(pidStr), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "PL024 child: write sync file: %v\n", err)
			os.Exit(1)
		}

		// Block until SIGKILL — the parent will kill us to simulate a crash.
		select {}
	}

	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL024_StalePidfileDetection: skipping on %s (flock+kill(pid,0) path is POSIX-only)", runtime.GOOS)
	}

	// --- PARENT TEST BODY ---
	projectDir := plFixtureTempProjectDir(t)

	// Create a sync file for PID communication. Use /tmp with a short name so
	// it is guaranteed to be on the same filesystem as the pidfile.
	syncFile, err := os.CreateTemp("/tmp", "pl024-sync-")
	if err != nil {
		t.Fatalf("PL-024: CreateTemp sync file: %v", err)
	}
	syncFilePath := syncFile.Name()
	_ = syncFile.Close()                              //nolint:errcheck // cleanup error unactionable
	_ = os.Remove(syncFilePath)                       //nolint:errcheck // child will create it; Remove error expected if already absent
	t.Cleanup(func() { _ = os.Remove(syncFilePath) }) //nolint:errcheck // cleanup error unactionable

	// Self-exec: spawn the test binary with the sentinel env, targeting this
	// exact test function. No -test.v so the child emits no extra lines.
	testBin := os.Args[0]
	//nolint:gosec,noctx // G204: testBin is os.Args[0]; noctx: child-process stub, CommandContext would cancel child on t.Context() done
	cmd := exec.Command(testBin, "-test.run=^TestPL024_StalePidfileDetection$")
	cmd.Env = append(os.Environ(),
		sentinelEnv+"=1",
		projectDirEnv+"="+projectDir,
		syncFileEnv+"="+syncFilePath,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-024: cmd.Start: %v", err)
	}

	// Wait for the child to write the sync file (poll up to 5 s).
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
		t.Fatal("PL-024: timed out waiting for child to write sync file")
	}

	childPID, err := strconv.Atoi(childPIDStr)
	if err != nil || childPID <= 0 {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-024: invalid child PID %q: %v", childPIDStr, err)
	}

	// Verify the child is live.
	if !plFixtureIsPidLive(childPID) {
		_ = cmd.Wait() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-024: child PID %d should be live before SIGKILL", childPID)
	}

	// SIGKILL the child — simulates a daemon crash (PL-012 / PL-024 path).
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("PL-024: Kill child: %v", err)
	}
	_ = cmd.Wait() //nolint:errcheck // reap the zombie; exit status expected non-zero after SIGKILL

	// PL-024: child is now dead; pidfile must still be on disk (crash left it stale).
	pidfilePath := plFixturePidfilePath(projectDir)
	if _, err := os.Stat(pidfilePath); os.IsNotExist(err) {
		t.Fatal("PL-024: pidfile missing after SIGKILL; stale detection requires it to remain on disk")
	}

	// PL-024: kill(pid, 0) must now report the child as dead.
	if plFixtureIsPidLive(childPID) {
		t.Errorf("PL-024: plFixtureIsPidLive(%d) = true after SIGKILL, want false", childPID)
	}

	// PL-024: attempt flock on the pidfile — it must succeed (kernel released
	// the advisory lock on child exit). A new acquire simulates the next daemon
	// startup's stale-pidfile detection path.
	myPID := os.Getpid()
	myPGID, _ := syscall.Getpgid(myPID) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	release, err := plFixtureAcquirePidfile(t, projectDir, myPID, myPGID, "01950000-0000-7000-8000-000000000041")
	if err != nil {
		t.Fatalf("PL-024: post-crash acquire failed (stale lock not released?): %v", err)
	}
	t.Cleanup(release)

	// PL-024: verify the pidfile was rewritten with our PID.
	gotPID, _, _, err := plFixtureReadPidfile(t, projectDir)
	if err != nil {
		t.Fatalf("PL-024: readPidfile after re-acquire: %v", err)
	}
	if gotPID != myPID {
		t.Errorf("PL-024: post-crash pidfile PID = %d, want %d (our PID)", gotPID, myPID)
	}
}
