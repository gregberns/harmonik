package lifecycle

import (
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestPL_INV001_PidfileLockExclusivity exercises the PL-INV-001 sensor:
// only one process-fd may hold the pidfile lock at a time; the lock content
// must be parseable and its PID must match the holder.
//
// Sensor definition (spec): "pidfile lock held by the daemon's fd AND pidfile
// content parseable AND parsed PID equals getpid() AND parsed PGID equals
// getpgrp() AND parsed daemon_instance_id equals the in-memory
// daemon_instance_id minted at PL-005 step 0."
//
// Spec ref: process-lifecycle.md §5 PL-INV-001 — "For each project directory
// at any instant, at most one daemon process MUST hold the pidfile lock at
// .harmonik/daemon.pid."
func TestPL_INV001_PidfileLockExclusivity(t *testing.T) {
	t.Parallel()

	t.Run("sensor/lock-held-by-exactly-one-fd", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		pid := os.Getpid()
		pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
		wantInstanceID := "01950000-0000-7000-8000-000000000060"

		// Acquire the lock.
		release, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, wantInstanceID)
		if err != nil {
			t.Fatalf("PL-INV-001 sensor: acquire: %v", err)
		}
		t.Cleanup(release)

		// Sensor check 1: pidfile is parseable.
		gotPID, gotPGID, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
		if err != nil {
			t.Fatalf("PL-INV-001 sensor: readPidfile: %v", err)
		}

		// Sensor check 2: parsed PID equals the holder's PID.
		if gotPID != pid {
			t.Errorf("PL-INV-001 sensor: pidfile PID = %d, want %d (getpid)", gotPID, pid)
		}

		// Sensor check 3: parsed PGID equals the holder's PGID.
		if gotPGID != pgid {
			t.Errorf("PL-INV-001 sensor: pidfile PGID = %d, want %d (getpgid)", gotPGID, pgid)
		}

		// Sensor check 4: parsed daemon_instance_id equals the in-memory value.
		if gotInstanceID != wantInstanceID {
			t.Errorf("PL-INV-001 sensor: pidfile instanceID = %q, want %q", gotInstanceID, wantInstanceID)
		}

		// Sensor check 5: no second holder can acquire the lock.
		_, err2 := plFixtureAcquirePidfile(t, projectDir, pid, pgid, "01950000-0000-7000-8000-000000000061")
		if err2 == nil {
			t.Error("PL-INV-001 sensor: second acquire succeeded; invariant violated — only one lock holder allowed")
		}
	})

	t.Run("sensor/lock-released-on-fd-close", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		pid := os.Getpid()
		pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
		instanceID1 := "01950000-0000-7000-8000-000000000062"
		instanceID2 := "01950000-0000-7000-8000-000000000063"

		// Acquire and immediately release.
		release1, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID1)
		if err != nil {
			t.Fatalf("PL-INV-001 lock-released: first acquire: %v", err)
		}
		release1()

		// After release, a second acquire must succeed (invariant is
		// re-satisfiable). Retry briefly: a concurrent exec.Command fork(2)
		// elsewhere in this parallel test binary can transiently keep the
		// just-released flock alive via an inherited fd copy until that
		// child's exec(2) closes it — see plFixtureEventuallyNoErr.
		var release2 func()
		err = plFixtureEventuallyNoErr(t, 2*time.Second, func() error {
			r, acquireErr := plFixtureAcquirePidfile(t, projectDir, pid, pgid, instanceID2)
			release2 = r
			return acquireErr
		})
		if err != nil {
			t.Fatalf("PL-INV-001 lock-released: second acquire after release: %v", err)
		}
		t.Cleanup(release2)

		// Verify the pidfile reflects the new holder.
		gotPID, _, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
		if err != nil {
			t.Fatalf("PL-INV-001 lock-released: readPidfile: %v", err)
		}
		if gotPID != pid {
			t.Errorf("PL-INV-001 lock-released: pidfile PID = %d, want %d", gotPID, pid)
		}
		if gotInstanceID != instanceID2 {
			t.Errorf("PL-INV-001 lock-released: pidfile instanceID = %q, want %q", gotInstanceID, instanceID2)
		}
	})
}

// TestPL_INV004_SocketPathExclusivity exercises the PL-INV-004 sensor: at
// most one bound Unix socket at .harmonik/daemon.sock may serve daemon
// requests for a given project directory at any instant.
//
// Sensor definition (spec): "The daemon that holds the pidfile lock (PL-002)
// is the exclusive owner of the bound socket fd. A second daemon observing
// EADDRINUSE on bind MUST exit with ON §8 code 6 (socket-bind-failed)."
//
// Spec ref: process-lifecycle.md §5 PL-INV-004 — "For each project directory
// at any instant, at most one bound Unix socket at .harmonik/daemon.sock MUST
// be serving daemon requests."
func TestPL_INV004_SocketPathExclusivity(t *testing.T) {
	t.Parallel()

	t.Run("sensor/exactly-one-listener-per-project", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)

		// First bind — invariant: this is the exclusive listener.
		ln1, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-INV-004 sensor: first bind: %v", err)
		}
		t.Cleanup(func() { _ = ln1.Close() }) //nolint:errcheck // cleanup error unactionable

		// Second bind — does NOT call Remove (so it hits the live socket).
		// Using ListenConfig directly to observe the raw EADDRINUSE.
		sockPath := plFixtureSocketPath(projectDir)
		ln2, err2 := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
		if err2 == nil {
			_ = ln2.Close() //nolint:errcheck // cleanup error unactionable
			t.Fatal("PL-INV-004 sensor: second bind succeeded; invariant violated — only one listener allowed per project")
		}

		// The error must be EADDRINUSE.
		errno := plFixtureExtractErrno(err2)
		if errno != syscall.EADDRINUSE {
			t.Errorf("PL-INV-004 sensor: bind error errno = %v, want EADDRINUSE", errno)
		}

		// EADDRINUSE must map to exit code 6.
		exitCode := plFixtureErrToExitCode(errno)
		if exitCode != 6 {
			t.Errorf("PL-INV-004 sensor: errToExitCode(EADDRINUSE) = %d, want 6 (socket-bind-failed)", exitCode)
		}
	})

	t.Run("sensor/listener-reassignable-after-close", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)

		// Bind, then close.
		ln1, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-INV-004 reassign: first bind: %v", err)
		}
		_ = ln1.Close() //nolint:errcheck // cleanup error unactionable

		// After close, another bind must succeed (new daemon starts up).
		ln2, err := plFixtureBindSocket(t, projectDir)
		if err != nil {
			t.Fatalf("PL-INV-004 reassign: second bind after close: %v", err)
		}
		t.Cleanup(func() { _ = ln2.Close() }) //nolint:errcheck // cleanup error unactionable
	})
}
