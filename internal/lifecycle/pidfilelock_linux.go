package lifecycle

import (
	"fmt"
	"os"
	"strings"
)

// probePidCmdline reads /proc/<pid>/cmdline on Linux and returns the first
// argv[0] token and true if the file is readable. Returns ("", false) if the
// file is absent or unreadable (the PID may have already exited between
// kill(pid, 0) and this read; treat as inconclusive).
//
// The /proc/<pid>/cmdline file contains argv tokens separated by NUL bytes.
// We extract argv[0] as the executable path for corroboration.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "The daemon MAY corroborate
// via /proc/<pid>/cmdline (Linux) to disambiguate recycled PIDs."
func probePidCmdline(pid int) (string, bool) {
	path := fmt.Sprintf("/proc/%d/cmdline", pid)
	//nolint:gosec // G304: path constructed from pid obtained via kill(pid,0) probe; not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	// cmdline is NUL-delimited; argv[0] is the first token.
	argv0 := strings.SplitN(string(data), "\x00", 2)[0]
	if argv0 == "" {
		return "", false
	}
	return argv0, true
}
