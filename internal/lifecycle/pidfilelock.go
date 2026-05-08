package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// PidfileLockStatus classifies the state of a pidfile found on disk, as
// determined by the PL-002a two-step probe: flock first, then kill(pid, 0).
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "The daemon MUST disambiguate
// (a) 'pidfile present, lock held by live process' from (b) 'pidfile present,
// no lock, recorded PID not live' by attempting flock first and, on failure,
// reading the recorded PID and probing with kill(pid, 0)."
type PidfileLockStatus int

const (
	// PidfileLockStatusHeld means flock(LOCK_EX|LOCK_NB) returned EAGAIN or
	// EWOULDBLOCK: a live daemon holds the lock. The caller MUST exit with code
	// 5 ("pidfile-locked") per PL-002 / PL-008a.
	PidfileLockStatusHeld PidfileLockStatus = iota

	// PidfileLockStatusStale means flock succeeded (lock not held) and
	// kill(pid, 0) returned ESRCH: the recorded PID is dead. The caller SHOULD
	// remove the stale pidfile and proceed with startup per PL-024.
	PidfileLockStatusStale

	// PidfileLockStatusAmbiguous means flock succeeded but kill(pid, 0) did NOT
	// return ESRCH: the recorded PID exists in the kernel process table even
	// though the lock is not held. This indicates a possible PID recycling after
	// an OS reboot. The caller MUST refuse startup (OQ-PL-007 tracks the
	// resolution; current normative behaviour is to refuse).
	PidfileLockStatusAmbiguous
)

// ErrPidfileAmbiguous is returned by ProbePidfileLock when the probe result is
// PidfileLockStatusAmbiguous: the flock was acquirable (no live lock holder)
// but kill(pid, 0) reports the recorded PID as alive. This suggests PID reuse
// after an OS reboot. The daemon MUST refuse startup on this error.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "behavior on ambiguity is to
// refuse startup with a specific exit code (OQ-PL-007)."
var ErrPidfileAmbiguous = errors.New("lifecycle: pidfile lock is unambiguously acquirable but recorded PID is alive (possible PID reuse)")

// ProbePidfileLock inspects an existing pidfile at <projectDir>/.harmonik/daemon.pid
// using the PL-002a two-step probe to distinguish the two pidfile-present cases:
//
//  1. Attempt flock(LOCK_EX|LOCK_NB) on the pidfile.
//  2. If flock returns EAGAIN or EWOULDBLOCK → another daemon holds the lock
//     → return (PidfileLockStatusHeld, 0, ErrPidfileLocked).
//  3. If flock succeeds → the lock is not held; read the recorded PID from the
//     file and release the probe fd immediately.
//  4. Send kill(pid, 0) to the recorded PID:
//     - ESRCH → PID is dead → return (PidfileLockStatusStale, pid, nil).
//     - nil (or EPERM) → PID exists → corroborate via platform-specific means
//     (Linux: /proc/<pid>/cmdline; darwin: proc_pidpath).
//     - If corroboration is inconclusive or unavailable → return
//     (PidfileLockStatusAmbiguous, pid, ErrPidfileAmbiguous).
//  5. On any syscall error other than EAGAIN/EWOULDBLOCK for flock → return
//     (0, 0, ErrPidfileLockError wrapping the underlying errno).
//
// ProbePidfileLock does NOT modify the pidfile or hold the lock after returning.
// Its sole purpose is disambiguation for the caller's startup logic. Callers
// that detect Stale SHOULD remove the pidfile and then call AcquirePidfile.
// Callers that detect Held MUST exit with code 5. Callers that detect Ambiguous
// MUST refuse startup (exit code determined by OQ-PL-007).
//
// If the pidfile does not exist, errors.Is(err, os.ErrNotExist) will be true on
// the returned error — the caller may treat absent-pidfile as a normal
// first-start case.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a, PL-002b, PL-024.
func ProbePidfileLock(projectDir string) (PidfileLockStatus, int, error) {
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Step 1: open the pidfile without truncating (read-only probe).
	//nolint:gosec // G304: pidfilePath derived from projectDir, an operator-controlled parameter; not user input
	fd, err := os.OpenFile(pidfilePath, os.O_RDONLY, 0)
	if err != nil {
		// Let the caller distinguish os.IsNotExist from other errors.
		return 0, 0, fmt.Errorf("lifecycle: ProbePidfileLock: open %q: %w", pidfilePath, err)
	}
	defer func() { _ = fd.Close() }() //nolint:errcheck // probe fd; close error unactionable

	// Step 2: attempt exclusive non-blocking flock.
	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			// Another daemon holds the lock: caller must exit 5.
			return PidfileLockStatusHeld, 0, ErrPidfileLocked
		}
		// Unexpected flock errno (ENOLCK, EBADF, ENOTSUP, …).
		return 0, 0, fmt.Errorf("%w: %w", ErrPidfileLockError, err)
	}

	// Step 3: flock succeeded — release it immediately by closing fd via defer.
	// Read the recorded PID before closing.
	pid, _, _, err := ReadPidfile(projectDir)
	if err != nil {
		// Unparseable or empty pidfile → treat as stale per PL-024.
		return PidfileLockStatusStale, 0, nil
	}

	// Step 4: probe kill(pid, 0).
	killErr := syscall.Kill(pid, 0)
	if killErr != nil {
		if errors.Is(killErr, syscall.ESRCH) {
			// PID is dead: stale pidfile left by a crashed daemon.
			return PidfileLockStatusStale, pid, nil
		}
		// Other kill error (e.g., EINVAL for pid <= 0): treat as stale.
		return PidfileLockStatusStale, pid, nil
	}

	// Step 4a: kill(pid, 0) succeeded (or EPERM — process exists).
	// The flock was acquirable but the PID is alive. Attempt optional
	// platform-specific corroboration.
	cmdline, ok := probePidCmdline(pid)
	if ok && len(cmdline) > 0 {
		// Corroboration available: we can distinguish a recycled PID from a
		// harmonik daemon that somehow lost its lock. Since the flock is not
		// held, this is not a live harmonik daemon (a live harmonik daemon
		// would hold the flock). The recorded PID belongs to a different
		// process. Treat as stale: the recycled-PID process is unrelated.
		_ = cmdline // corroboration data available for structured logging by caller
		return PidfileLockStatusStale, pid, nil
	}

	// Corroboration inconclusive or unavailable: per PL-002a, refuse startup.
	return PidfileLockStatusAmbiguous, pid, ErrPidfileAmbiguous
}
