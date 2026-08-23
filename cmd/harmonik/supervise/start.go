package supervisecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
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
	var requireAPIKey bool
	var command []string // supervisee argv; populated from --command or -- args

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if _, err := fmt.Fprint(stdout, startUsage); err != nil {
				return 1
			}
			return 0
		case args[i] == "--watch-restart":
			watchRestart = true
		case args[i] == "--require-api-key":
			requireAPIKey = true
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--command" && i+1 < len(args):
			i++
			command = args[i:]
			i = len(args) // consume remaining
		case strings.HasPrefix(args[i], "--command="):
			command = []string{strings.TrimPrefix(args[i], "--command=")}
		case args[i] == "--":
			command = args[i+1:]
			i = len(args) // consume remaining
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}

	projectCfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: load .harmonik/config.yaml: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	sockPath := lifecycle.SocketPath(projectDir)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if code := probeDaemonSocket(ctx, sockPath, stderr); code != 0 {
		return code
	}

	_, _, instanceID, err := lifecycle.ReadPidfile(projectDir)
	if err != nil {
		instanceID = "unknown"
	}

	if err := os.MkdirAll(CognitionDir(projectDir), core.HarmonikDirMode); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: mkdir cognition: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	lockFd, err := os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: open lock: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	lockReleased := false
	defer func() {
		if !lockReleased {
			if closeErr := lockFd.Close(); closeErr != nil {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: close lock: %v\n", closeErr); writeErr != nil {
					return
				}
			}
		}
	}()

	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if isWouldBlock(err) {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: supervisor already running (lock held: %s)\n",
				PidfilePath(projectDir)); writeErr != nil {
				return 1
			}
			return ExitCodeSupervisorRunning
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: flock error: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	sessionName := FlywheelSessionName(projectDir)
	// #nosec G204 -- sessionName is passed as a discrete tmux argument, never through a shell.
	if err := exec.CommandContext(ctx, "tmux", "has-session", "-t", sessionName).Run(); err == nil {
		if _, writeErr := fmt.Fprintf(stderr,
			"harmonik supervise start: flywheel session already exists (%s) — run 'harmonik supervise stop' first\n",
			sessionName); writeErr != nil {
			return 1
		}
		return ExitCodeFlywheelSessionExists
	}

	if err := WriteSentinel(projectDir); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: write sentinel: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	apiKey, err := resolveAPIKey(projectDir, requireAPIKey)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: %v\n", err); writeErr != nil {
			return 1
		}
		if removeErr := RemoveSentinel(projectDir); removeErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: remove sentinel after API-key failure: %v\n", removeErr); writeErr != nil {
				return 1
			}
		}
		return 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	cfg := Config{
		SchemaVersion:    configSchemaVersion,
		RestartPolicy:    "on-failure",
		RestartMax:       5,
		RestartBaseMS:    1000,
		RestartCapMS:     60000,
		StartedAt:        now,
		DaemonInstanceID: instanceID,
		Command:          command, // may be nil; empty Command triggers watchdog-only mode in the shim (hk-5gdqu)
		APIKey:           apiKey,
	}
	applySuperviseProjectConfig(&cfg, projectCfg.Supervise)
	if err := WriteConfigAtomic(projectDir, cfg); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: write config: %v\n", err); writeErr != nil {
			return 1
		}
		if removeErr := RemoveSentinel(projectDir); removeErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: remove sentinel after config failure: %v\n", removeErr); writeErr != nil {
				return 1
			}
		}
		return 1
	}

	shimArgs := []string{"supervise", "_shim", projectDir}
	if watchRestart {
		shimArgs = append(shimArgs, "--watch-restart")
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	shimCmd := exe + " " + strings.Join(shimArgs, " ")

	// #nosec G204 -- sessionName and shimCmd are passed as direct tmux arguments, never through a shell.
	createCmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", sessionName,
		"-c", projectDir, shimCmd)
	if out, err := createCmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "duplicate session") {
			if _, writeErr := fmt.Fprintf(stderr,
				"harmonik supervise start: flywheel session already exists (%s) — run 'harmonik supervise stop' first\n",
				sessionName); writeErr != nil {
				return 1
			}
			if removeErr := RemoveSentinel(projectDir); removeErr != nil {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: remove sentinel after duplicate session: %v\n", removeErr); writeErr != nil {
					return 1
				}
			}
			return ExitCodeFlywheelSessionExists
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: tmux new-session: %v: %s\n", err, strings.TrimSpace(string(out))); writeErr != nil {
			return 1
		}
		if removeErr := RemoveSentinel(projectDir); removeErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: remove sentinel after tmux failure: %v\n", removeErr); writeErr != nil {
				return 1
			}
		}
		return 1
	}

	// Set remain-on-exit on the flywheel session (PL-019f).
	// #nosec G204 -- sessionName is passed as a discrete tmux argument, never through a shell.
	setOptionCmd := exec.CommandContext(ctx, "tmux", "set-option", "-t", sessionName, "remain-on-exit", "on")
	if setOptionErr := setOptionCmd.Run(); setOptionErr != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: tmux set remain-on-exit: %v\n", setOptionErr); writeErr != nil {
			return 1
		}
		return 1
	}

	bootReapOrphanFlywheels(projectDir, sessionName)

	lockReleased = true
	if closeErr := lockFd.Close(); closeErr != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: close lock: %v\n", closeErr); writeErr != nil {
			return 1
		}
		return 1
	}

	if _, writeErr := fmt.Fprintf(stdout, "harmonik supervise start: supervisor launched (session: %s)\n", sessionName); writeErr != nil {
		return 1
	}
	return 0
}

func probeDaemonSocket(ctx context.Context, sockPath string, stderr io.Writer) int {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		if isSocketAbsent(err) || isConnectionRefused(err) {
			if _, writeErr := fmt.Fprintf(stderr,
				"harmonik supervise start: daemon not running; start with: harmonik daemon\n"); writeErr != nil {
				return 1
			}
			return ExitCodeDaemonDown
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: dial daemon socket: %v\n", err); writeErr != nil {
			return 1
		}
		return ExitCodeDaemonDown
	}
	if closeErr := conn.Close(); closeErr != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise start: close daemon socket: %v\n", closeErr); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func resolveAPIKey(projectDir string, require bool) (string, error) {
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v, nil
	}
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "ANTHROPIC_API_KEY=") {
				return strings.TrimPrefix(line, "ANTHROPIC_API_KEY="), nil
			}
		}
	}
	if require {
		return "", fmt.Errorf("no ANTHROPIC_API_KEY source resolved: neither operator env nor .env file contains the key; set the key or omit --require-api-key for OAuth auth")
	}
	return "", nil
}

func applySuperviseProjectConfig(cfg *Config, sc projectconfig.SuperviseConfig) {
	if sc.HeartbeatTTL > 0 {
		cfg.HeartbeatTTLMS = durationMS(sc.HeartbeatTTL)
	}
	if sc.StartTimeout > 0 {
		cfg.StartTimeoutMS = durationMS(sc.StartTimeout)
	}
	if sc.CrashLoopWindow > 0 {
		cfg.CrashLoopWindowMS = durationMS(sc.CrashLoopWindow)
	}
	if sc.HealthProbeInterval > 0 {
		cfg.HealthProbeMS = durationMS(sc.HealthProbeInterval)
	}
	if sc.StopTimeout > 0 {
		cfg.StopTimeoutMS = durationMS(sc.StopTimeout)
	}
	if sc.RestartBackoffBase > 0 {
		cfg.RestartBaseMS = durationMS(sc.RestartBackoffBase)
	}
	if sc.RestartBackoffCap > 0 {
		cfg.RestartCapMS = durationMS(sc.RestartBackoffCap)
	}
	if sc.DaemonWatchdog.CheckInterval > 0 {
		cfg.DWCheckIntervalMS = durationMS(sc.DaemonWatchdog.CheckInterval)
	}
	if sc.DaemonWatchdog.DialTimeout > 0 {
		cfg.DWDialTimeoutMS = durationMS(sc.DaemonWatchdog.DialTimeout)
	}
	if sc.DaemonWatchdog.ReviveBackoff > 0 {
		cfg.DWReviveBackoffMS = durationMS(sc.DaemonWatchdog.ReviveBackoff)
	}
	if sc.DaemonWatchdog.ReviveWindow > 0 {
		cfg.DWReviveWindowMS = durationMS(sc.DaemonWatchdog.ReviveWindow)
	}
}

func durationMS(d time.Duration) int {
	return int(d / time.Millisecond)
}

func isSocketAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func isWouldBlock(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

const startUsage = `harmonik supervise start — launch the supervisor (cognition/flywheel) process

USAGE
  harmonik supervise start [--project DIR] [--watch-restart] [--require-api-key] [--command CMD [ARGS...]]
  harmonik supervise start [--project DIR] [--watch-restart] [--require-api-key] -- CMD [ARGS...]

FLAGS
  --project DIR          Project directory (default: current working directory)
  --watch-restart        Interpose a restart-shim: supervisor restarts on crash
  --require-api-key      Fail-closed (exit 1) when no ANTHROPIC_API_KEY source resolves
                         (operator env or .env file). Without this flag an empty key
                         is allowed so the holder process may authenticate via OAuth.
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
  Credential source precedence: operator env > .env file > fail-closed (CI-006).

EXAMPLES
  harmonik supervise start --watch-restart --command claude --pi
  harmonik supervise start --watch-restart -- claude --pi --project /path/to/project
  harmonik supervise start --require-api-key --watch-restart -- claude --pi
`
