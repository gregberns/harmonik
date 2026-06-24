package supervisecmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// RunStop implements `harmonik supervise stop`.
//
// Reads the supervisor PID from supervisor.pid, sends SIGTERM, waits up to
// stopTimeout, then sends SIGKILL if needed (PL-011). On clean shutdown,
// removes the sentinel file.
//
// Exit codes:
//
//	0  — supervisor stopped (or was already stopped)
//	1  — argument or I/O error
//
// Spec ref: process-lifecycle.md §4.5 PL-019, §4.10 PL-028d; PL-011 SIGTERM→SIGKILL.
func RunStop(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	stopTimeout := 10 * time.Second

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, stopUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise stop: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	pid, err := ReadPidfile(projectDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(stdout, "harmonik supervise stop: supervisor not running (no pidfile)")
			return 0
		}
		fmt.Fprintf(stderr, "harmonik supervise stop: read pidfile: %v\n", err)
		return 1
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise stop: find process %d: %v\n", pid, err)
		_ = cleanup(projectDir)
		return 1
	}

	// PL-011: SIGTERM → bounded wait → SIGKILL.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			fmt.Fprintln(stdout, "harmonik supervise stop: supervisor already exited")
			_ = cleanup(projectDir)
			return 0
		}
		fmt.Fprintf(stderr, "harmonik supervise stop: SIGTERM: %v\n", err)
	}

	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process no longer exists.
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If still alive, SIGKILL.
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		fmt.Fprintf(stderr, "harmonik supervise stop: SIGTERM timeout — sending SIGKILL\n")
		_ = proc.Signal(syscall.SIGKILL)
		// Brief wait for kernel reaping.
		time.Sleep(500 * time.Millisecond)
	}

	// Reap the flywheel tmux session (child tree). This kills the pane and any
	// processes still running under it even if the shim already exited.
	// Ignore errors: session may not exist if the shim already cleaned up.
	sessionName := FlywheelSessionName(projectDir)
	//nolint:gosec // G204: sessionName derived from operator-controlled projectDir
	_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	_ = cleanup(projectDir)
	fmt.Fprintln(stdout, "harmonik supervise stop: supervisor stopped")
	return 0
}

// cleanup removes pidfile and sentinel on supervisor exit.
func cleanup(projectDir string) error {
	_ = os.Remove(PidfilePath(projectDir))
	return RemoveSentinel(projectDir)
}

const stopUsage = `harmonik supervise stop — terminate the supervisor process

USAGE
  harmonik supervise stop [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
  0  Success (or supervisor was already stopped)
  1  Argument or I/O error

NOTES
  Sends SIGTERM, waits 10s, then SIGKILL (PL-011).
  Removes supervisor.pid and supervisor.sentinel on exit.
`
