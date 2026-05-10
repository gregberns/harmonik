package lifecycle

import (
	"fmt"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// KillSubprocesses sends SIGKILL to each PID in the given list. Errors from
// individual kills (e.g., the process has already exited) are collected and
// returned as a combined error. A nil return means all signals were delivered.
//
// This is the subprocess-kill step of PL-012 immediate shutdown: in-flight
// agent subprocesses are killed by the daemon before it proceeds to steps 5–9.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "In-flight agent subprocesses
// are killed; in-flight state is recoverable via the next startup's orphan
// sweep (§PL-006) + reconciliation."
func KillSubprocesses(pids []int) error {
	var errs []error
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			// ESRCH means the process has already exited — not an error.
			if err == syscall.ESRCH {
				continue
			}
			errs = append(errs, fmt.Errorf("lifecycle: KillSubprocesses: kill(%d, SIGKILL): %w", pid, err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("lifecycle: KillSubprocesses: %d kill errors: %v", len(errs), errs)
}

// BuildImmediateShutdownPayload constructs a core.DaemonShutdownPayload for
// the interceptable immediate-shutdown path (harmonik stop --immediate).
//
// The payload carries:
//   - ShutdownAt: RFC 3339 wall-clock timestamp at shutdown emission.
//   - ShutdownAtNsSinceBoot: monotonic-clock companion via MonotonicNsSinceBoot().
//   - Mode: core.ShutdownModeImmediate.
//
// On monotonic-clock failure the returned error wraps the underlying errno.
// The caller SHOULD abort the shutdown emission and log the error; the daemon
// MUST still proceed to exit.
//
// SIGKILL paths cannot call this function (the kernel terminates the process
// before any defer/recover runs). Only the interceptable stop --immediate path
// calls BuildImmediateShutdownPayload.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "On interceptable stop
// --immediate, the daemon MUST attempt steps 5–9 (emit
// daemon_shutdown{mode=immediate}, flush, release, exit)."
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The mode is immediate for
// PL-012 (for the interceptable stop --immediate path; SIGKILL cannot emit)."
//
// Spec ref: operator-nfr.md §4.8 ON-033 — monotonic-companion field required
// for RTO measurement.
func BuildImmediateShutdownPayload() (core.DaemonShutdownPayload, error) {
	now := time.Now().UTC()
	monoNs, err := MonotonicNsSinceBoot()
	if err != nil {
		return core.DaemonShutdownPayload{}, fmt.Errorf("lifecycle: BuildImmediateShutdownPayload: %w", err)
	}
	return core.DaemonShutdownPayload{
		ShutdownAt:            now.Format(time.RFC3339Nano),
		ShutdownAtNsSinceBoot: monoNs,
		Mode:                  core.ShutdownModeImmediate,
	}, nil
}
