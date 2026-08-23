//go:build linux

package scenario

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

const runIDEnvKey = "HARMONIK_RUN_ID"

func checkLeakedProcesses(ctx context.Context, executedRunIDs []core.RunID) ([]LeakDescriptor, error) {
	if len(executedRunIDs) == 0 {
		return nil, nil
	}

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

		data, readErr := os.ReadFile(filepath.Join("/proc", pid, "environ"))
		if readErr != nil {
			continue
		}

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
