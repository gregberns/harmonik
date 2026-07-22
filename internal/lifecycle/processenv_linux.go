package lifecycle

// processenv_linux.go — per-process environment lookup on Linux, backed by
// /proc/<pid>/environ.
//
// Used by the launch-path watcher reap (agentwatcherreap.go) to prove a kill
// candidate belongs to THIS project before signalling it. See processEnvValue
// for the fail-closed contract.

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// processEnvValue returns the value environment variable key held at exec time
// for pid.
//
// ok is false whenever the value cannot be established for ANY reason — the
// process is gone, the read is denied, or the key is absent. Callers MUST read
// !ok as "cannot prove this process is ours" and leave the process alone; this
// function never guesses and never partially matches.
func processEnvValue(_ context.Context, pid int, key string) (value string, ok bool) {
	path := fmt.Sprintf("/proc/%d/environ", pid)
	//nolint:gosec // G304: path is /proc/<pid>/environ, built from an integer pid
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	prefix := key + "="
	for _, entry := range splitNul(data) {
		if after, found := strings.CutPrefix(entry, prefix); found {
			return after, true
		}
	}
	return "", false
}
