//go:build darwin

package scenario

// postsuiteleaksensor_darwin.go — Darwin (macOS) stub for checkLeakedProcesses
// in the SH-INV-002 post-suite sensor.
//
// On Darwin, /proc is unavailable so HARMONIK_RUN_ID cannot be read from
// other processes' environments without elevated privileges. The ppid()-walk
// alternative (enumerating descendants of the harness process) is unreliable
// in a test context because it flags the sensor's own child processes (ps,
// lsof) as false-positive leaks — defeating the check.
//
// The process tree check is therefore skipped on Darwin; the lease-registry
// check (SH-INV-002(ii)) and open-fd check (SH-INV-002(iii)) still run and
// provide meaningful coverage on this platform.
//
// Resolution: OQ-PL-008 tracks the Darwin process-environment reading
// limitation. A future implementation may use sysctl/proc_info or a
// PGID-scoped approach similar to the orphan sweep (lifecycle.OSHandlerProcessLister).
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(i);
//           specs/process-lifecycle.md §4.1 PL-006a (OQ-PL-008 Darwin note).

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
)

// checkLeakedProcesses is a no-op on Darwin due to the unavailability of
// /proc/<pid>/environ. See file-level comment for rationale (OQ-PL-008).
// Returns nil, nil (no leaks detected, no error).
func checkLeakedProcesses(_ context.Context, _ []core.RunID) ([]LeakDescriptor, error) {
	return nil, nil
}
