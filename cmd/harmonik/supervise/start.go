package supervisecmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

// ExitCodeFlywheelSessionExists is the exit code when the flywheel tmux session
// already exists (lock free but pane still present after shim crash).
// Code 24 per PL-INTERIM (`tmux-session-unavailable`; PL-028b).
const ExitCodeFlywheelSessionExists = 24

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
	var command []string // supervisee argv; populated from --command or -- args

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
		case args[i] == "--command" && i+1 < len(args):
			// --command CMD [ARGS...]: rest of args is the supervisee argv.
			i++
			command = args[i:]
			i = len(args) // consume remaining
		case strings.HasPrefix(args[i], "--command="):
			// --command=CMD (single token, no sub-args).
			command = []string{strings.TrimPrefix(args[i], "--command=")}
		case args[i] == "--":
			// -- CMD [ARGS...]: supervisee argv follows the separator.
			command = args[i+1:]
			i = len(args) // consume remaining
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

	// Ensure cognition dir exists before opening the lock file.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(CognitionDir(projectDir), 0o755); err != nil {
		fmt.Fprintf(stderr, "harmonik supervise start: mkdir cognition: %v\n", err)
		return 1
	}

	// (c) Acquire supervisor.lock (flock LOCK_EX|LOCK_NB).
	//
	// Hold the fd open until AFTER tmux new-session completes. This closes the
	// race window where a concurrent `start` sees a free lock between probe and
	// session-creation: any second `start` invocation will hit EWOULDBLOCK
	// (exit 25) while the first start holds the fd. The shim acquires the lock
	// (blocking) once start exits and releases it.
	//nolint:gosec // G304: lockPath derived from operator-controlled projectDir
	lockFd, err := os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise start: open lock: %v\n", err)
		return 1
	}
	// lockFd is released at the bottom after session creation (or on any error
	// path via the deferred close below).
	lockReleased := false
	defer func() {
		if !lockReleased {
			_ = lockFd.Close()
		}
	}()

	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if isWouldBlock(err) {
			fmt.Fprintf(stderr, "harmonik supervise start: supervisor already running (lock held: %s)\n",
				PidfilePath(projectDir))
			return ExitCodeSupervisorRunning
		}
		fmt.Fprintf(stderr, "harmonik supervise start: flock error: %v\n", err)
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
		Command:          command, // may be nil; shim will error if Command is empty
		APIKey:           resolveAPIKey(projectDir),
	}
	if err := WriteConfigAtomic(projectDir, cfg); err != nil {
		fmt.Fprintf(stderr, "harmonik supervise start: write config: %v\n", err)
		_ = RemoveSentinel(projectDir)
		return 1
	}

	// (f) Create tmux session harmonik-<project_hash>-flywheel with remain-on-exit on.
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
		if strings.Contains(string(out), "duplicate session") {
			// Flywheel session already exists (lock was free but pane survived shim
			// crash with remain-on-exit on). Refuse with 24 so the operator runs
			// `supervise stop` first to reap the stale pane (PL-028b interim).
			fmt.Fprintf(stderr,
				"harmonik supervise start: flywheel session already exists (%s) — run 'harmonik supervise stop' first\n",
				sessionName)
			_ = RemoveSentinel(projectDir)
			return ExitCodeFlywheelSessionExists
		}
		fmt.Fprintf(stderr, "harmonik supervise start: tmux new-session: %v: %s\n", err, strings.TrimSpace(string(out)))
		_ = RemoveSentinel(projectDir)
		return 1
	}

	// Set remain-on-exit on the flywheel session (PL-019f).
	//nolint:gosec // G204
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "remain-on-exit", "on").Run()

	// Release the lock now that the tmux session (and shim) is running.
	// The shim will immediately acquire it (blocking flock). Releasing here
	// rather than via the defer lets the defer no-op cleanly.
	lockReleased = true
	_ = lockFd.Close()

	fmt.Fprintf(stdout, "harmonik supervise start: supervisor launched (session: %s)\n", sessionName)
	return 0
}

// probeDaemonSocket attempts a connection to the Unix socket at sockPath.
// Returns 17 (ExitCodeDaemonDown) if the socket is absent or ECONNREFUSED, 0 if reachable.
func probeDaemonSocket(ctx context.Context, sockPath string, stderr io.Writer) int {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		if isSocketAbsent(err) || isConnectionRefused(err) {
			fmt.Fprintf(stderr,
				"harmonik supervise start: daemon not running; start with: harmonik daemon\n")
			return ExitCodeDaemonDown
		}
		fmt.Fprintf(stderr, "harmonik supervise start: dial daemon socket: %v\n", err)
		return ExitCodeDaemonDown
	}
	_ = conn.Close()
	return 0
}

// resolveAPIKey reads the Pi-scoped ANTHROPIC_API_KEY from the non-committed
// scoped source per specs/credential-isolation.md §4.4 CI-006.
//
// Precedence:
//  1. ANTHROPIC_API_KEY already exported by the operator in the current env.
//  2. A gitignored repo-root .env file (KEY=VALUE lines; comments ignored).
//  3. Empty string — Pi may authenticate via a different mechanism (e.g. OAuth).
//
// The value is stored in config.json (inside .harmonik/cognition/, which is
// gitignored) and injected into Pi's env by the shim at exec time. The daemon
// process MUST NOT read config.APIKey (CI-006).
func resolveAPIKey(projectDir string) string {
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "ANTHROPIC_API_KEY=") {
			return strings.TrimPrefix(line, "ANTHROPIC_API_KEY=")
		}
	}
	return ""
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
  harmonik supervise start [--project DIR] [--watch-restart] [--command CMD [ARGS...]]
  harmonik supervise start [--project DIR] [--watch-restart] -- CMD [ARGS...]

FLAGS
  --project DIR          Project directory (default: current working directory)
  --watch-restart        Interpose a restart-shim: supervisor restarts on crash
  --command CMD [ARGS]   Supervisee argv; all tokens after CMD are sub-args
  -- CMD [ARGS...]       Alternative: supervisee argv after the separator

EXIT CODES
   0  Success — tmux session created
   1  Argument or I/O error
  17  Daemon not running (start with: harmonik daemon)
  25  Supervisor already running (lock held)

NOTES
  Creates tmux session harmonik-<project_hash>-flywheel with remain-on-exit on.
  Reads daemon_instance_id from .harmonik/daemon.pid for config.json.
  The supervisor.lock is held until the tmux session is created, preventing
  concurrent 'start' invocations from writing conflicting config/sentinel files.

EXAMPLES
  harmonik supervise start --watch-restart --command claude --pi
  harmonik supervise start --watch-restart -- claude --pi --project /path/to/project
`
