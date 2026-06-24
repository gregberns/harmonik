//go:build linux

package scenario

// postsuiteleaksensor_linux.go — Linux implementation of checkLeakedProcesses
// for SH-INV-002(i).
//
// On Linux, /proc/<pid>/environ is readable for processes owned by the calling
// user. The scan reads the HARMONIK_RUN_ID env var from each /proc entry and
// matches against executedRunIDs.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(i);
//           specs/process-lifecycle.md §4.1 PL-006a.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// checkLeakedProcesses scans /proc for processes whose HARMONIK_RUN_ID
// environment variable matches any executed scenario's run_id.
//
// For each numeric /proc/<pid> entry:
//  1. Read /proc/<pid>/environ (NUL-separated env var list).
//  2. Search for a "HARMONIK_RUN_ID=<uuid>" entry matching executedRunIDs.
//  3. Report matching processes as LeakKindProcess descriptors.
//
// Processes that exit between directory listing and environ read are silently
// skipped (ENOENT / ESRCH); this is inherent to the /proc interface and not
// an error. Zombies (reaped but not yet wait-ed) are tolerated per spec.
//
// Returns nil, nil when executedRunIDs is empty.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(i);
//
//	specs/process-lifecycle.md §4.1 PL-006a.
func checkLeakedProcesses(ctx context.Context, executedRunIDs []core.RunID) ([]LeakDescriptor, error) {
	if len(executedRunIDs) == 0 {
		return nil, nil
	}

	// Build the set of "HARMONIK_RUN_ID=<uuid>" target strings for O(1) lookup.
	runIDEnvSet := make(map[string]bool, len(executedRunIDs))
	for _, rid := range executedRunIDs {
		runIDEnvSet[runIDEnvKey+"="+rid.String()] = true
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("checkLeakedProcesses: ReadDir /proc: %w", err)
	}

	var leaks []LeakDescriptor
	for _, entry := range entries {
		// Check for context cancellation between PIDs.
		select {
		case <-ctx.Done():
			return leaks, ctx.Err()
		default:
		}

		if !entry.IsDir() {
			continue
		}
		if _, numErr := strconv.Atoi(entry.Name()); numErr != nil {
			continue // not a PID directory (e.g. "self", "net")
		}
		pid := entry.Name()

		// Read /proc/<pid>/environ. ENOENT/ESRCH means the process exited
		// between ReadDir and ReadFile — silently skip.
		data, readErr := os.ReadFile(filepath.Join("/proc", pid, "environ"))
		if readErr != nil {
			continue
		}

		// /proc/<pid>/environ is NUL-separated.
		for _, envEntry := range strings.Split(string(data), "\x00") {
			if !runIDEnvSet[envEntry] {
				continue
			}
			runID := strings.TrimPrefix(envEntry, runIDEnvKey+"=")
			leaks = append(leaks, LeakDescriptor{
				Kind:   LeakKindProcess,
				Detail: fmt.Sprintf("pid=%s %s=%s", pid, runIDEnvKey, runID),
			})
			break // one match per process is sufficient
		}
	}
	return leaks, nil
}
