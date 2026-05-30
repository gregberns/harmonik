package supervisecmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ExitCodeDaemonDown is the exit code when the daemon socket is absent or
// unreachable (ECONNREFUSED). Code 17 per PL-008a / ON §8.
const ExitCodeDaemonDown = 17

// ExitCodeSupervisorRunning is the exit code when supervisor.lock is held by
// a live process. Code 25 per PL-INTERIM (PL-019c).
const ExitCodeSupervisorRunning = 25

// RunStart implements `harmonik supervise start`.
//
// Exit codes:
//
//	0   — supervisor launched (tmux session created)
//	1   — argument / I/O error
//	17  — daemon socket absent or ECONNREFUSED
//	25  — supervisor.lock already held
//
// Spec ref: process-lifecycle.md §4.5 PL-019, §4.10 PL-028d.
func RunStart(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	var watchRestart bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, startUsage)
			return 0
		case args[i] == "--watch-restart":
			watchRestart = true
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
			fmt.Fprintf(stderr, "harmonik supervise start: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	// (b) Probe daemon socket — exit 17 if missing / refused.
	sockPath := lifecycle.SocketPath(projectDir)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if code := probeDaemonSocket(ctx, sockPath, stderr); code != 0 {
		return code
	}

	// Read daemon_instance_id from daemon pidfile (PL-019e).
	_, _, instanceID, err := lifecycle.ReadPidfile(projectDir)
	if err != nil {
		// Non-fatal: use "unknown" when pidfile is absent/unreadable.
		instanceID = "unknown"
	}

	// (c) Try to acquire supervisor.lock (flock LOCK_EX|LOCK_NB).
	//nolint:gosec // G304: lockPath derived from operator-controlled projectDir
	lockFd, err := os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		// Ensure cognition dir exists then retry.
		if mkErr := os.MkdirAll(CognitionDir(projectDir), 0o755); mkErr != nil {
			fmt.Fprintf(stderr, "harmonik supervise start: mkdir cognition: %v\n", mkErr)
			return 1
		}
		//nolint:gosec // G304
		lockFd, err = os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise start: open lock: %v\n", err)
			return 1
		}
	}

	flockErr := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	_ = lockFd.Close() // release immediately; shim re-acquires inside tmux pane
	if flockErr != nil {
		if isWouldBlock(flockErr) {
			fmt.Fprintf(stderr, "harmonik supervise start: supervisor already running (lock held: %s)\n",
				PidfilePath(projectDir))
			return ExitCodeSupervisorRunning
		}
		fmt.Fprintf(stderr, "harmonik supervise start: flock error: %v\n", flockErr)
		return 1
	}

	// Write sentinel before launching (PL-006d).
	if err := WriteSentinel(projectDir); err != nil {
		fmt.Fprintf(stderr, "harmonik supervise start: write sentinel: %v\n", err)
		return 1
	}

	// Atomically write config.json snapshot (PL-019e).
	now := time.Now().UTC().Format(time.RFC3339)
	cfg := Config{
		SchemaVersion:    configSchemaVersion,
		RestartPolicy:    "on-failure",
		RestartMax:       5,
		RestartBaseMS:    1000,
		RestartCapMS:     60000,
		StartedAt:        now,
		DaemonInstanceID: instanceID,
	}
	if err := WriteConfigAtomic(projectDir, cfg); err != nil {
		fmt.Fprintf(stderr, "harmonik supervise start: write config: %v\n", err)
		_ = RemoveSentinel(projectDir)
		return 1
	}

	// (f) Create tmux session harmonik-<project_hash>-flywheel.
	sessionName := FlywheelSessionName(projectDir)
	shimArgs := []string{"supervise", "_shim", projectDir}
	if watchRestart {
		shimArgs = append(shimArgs, "--watch-restart")
	}
	// Resolve harmonik binary path for the shim command.
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	shimCmd := exe + " " + strings.Join(shimArgs, " ")

	//nolint:gosec // G204: sessionName and shimCmd are derived from operator-controlled inputs
	createCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName,
		"-c", projectDir, shimCmd)
	if out, err := createCmd.CombinedOutput(); err != nil {
		// "duplicate session" → session already exists; non-fatal.
		if !strings.Contains(string(out), "duplicate session") {
			fmt.Fprintf(stderr, "harmonik supervise start: tmux new-session: %v: %s\n", err, strings.TrimSpace(string(out)))
			_ = RemoveSentinel(projectDir)
			return 1
		}
	}

	// Set remain-on-exit on the flywheel pane (PL-019f).
	//nolint:gosec // G204
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "remain-on-exit", "on").Run()

	fmt.Fprintf(stdout, "harmonik supervise start: supervisor launched (session: %s)\n", sessionName)
	return 0
}

// probeDaemonSocket attempts a TCP connection to sockPath. Returns 17 if the
// socket is absent or connection refused, 0 if the daemon is reachable.
func probeDaemonSocket(ctx context.Context, sockPath string, stderr io.Writer) int {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		if isSocketAbsent(err) || isConnectionRefused(err) {
			fmt.Fprintf(stderr,
				"harmonik supervise start: daemon not running; start with: harmonik daemon\n")
			return ExitCodeDaemonDown
		}
		// Any other error also means we can't proceed.
		fmt.Fprintf(stderr, "harmonik supervise start: dial daemon socket: %v\n", err)
		return ExitCodeDaemonDown
	}
	_ = conn.Close()
	return 0
}

func isSocketAbsent(err error) bool {
	return os.IsNotExist(err)
}

func isConnectionRefused(err error) bool {
	opErr, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
		return sysErr.Err == syscall.ECONNREFUSED
	}
	return opErr.Err == syscall.ECONNREFUSED
}

func isWouldBlock(err error) bool {
	return err == syscall.EAGAIN || err == syscall.EWOULDBLOCK
}

const startUsage = `harmonik supervise start — launch the supervisor (cognition/flywheel) process

USAGE
  harmonik supervise start [--project DIR] [--watch-restart]

FLAGS
  --project DIR    Project directory (default: current working directory)
  --watch-restart  Interpose a restart-shim: supervisor restarts on crash

EXIT CODES
   0  Success — tmux session created
   1  Argument or I/O error
  17  Daemon not running (start with: harmonik daemon)
  25  Supervisor already running (lock held)

NOTES
  Creates tmux session harmonik-<project_hash>-flywheel with remain-on-exit on.
  Reads daemon_instance_id from .harmonik/daemon.pid for config.json.
`
