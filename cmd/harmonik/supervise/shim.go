package supervisecmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/release"
	"github.com/gregberns/harmonik/internal/supervise"
)

// RunShim implements the internal `harmonik supervise _shim <projectDir>` command.
//
// This runs inside the flywheel tmux pane. It:
//  1. Acquires the supervisor.lock flock (fd-lifetime, released on exit).
//  2. Writes its own PID to supervisor.pid.
//  3. Reads config.json for supervisor parameters.
//  4. If config.Command is empty: watchdog-only mode — runs DaemonWatchdog,
//     no supervisee started (hk-5gdqu).
//  5. If --watch-restart: runs internal/supervise.Supervisor with the configured command.
//  6. Otherwise: exec-replaces itself with the configured command directly.
//  7. On exit: removes sentinel and pidfile.
//
// This is an internal subcommand not meant for direct operator use.
//
// Spec ref: process-lifecycle.md §4.5 PL-019c-f, §4.10 PL-028d.
func RunShim(args []string, stdout, stderr io.Writer) (exitCode int) {
	if len(args) == 0 {
		if shimWritef(stderr, "harmonik supervise _shim: missing project directory argument\n") != nil {
			return 1
		}
		return 1
	}

	projectDir := args[0]
	watchRestart := false
	for _, a := range args[1:] {
		if a == "--watch-restart" {
			watchRestart = true
		}
	}

	lockFd, err := os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if shimWritef(stderr, "harmonik supervise _shim: open lock: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX); err != nil {
		if closeErr := lockFd.Close(); closeErr != nil {
			return 1
		}
		if shimWritef(stderr, "harmonik supervise _shim: flock: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	defer func() {
		if closeErr := lockFd.Close(); closeErr != nil && exitCode == 0 {
			exitCode = 1
		}
		if cleanupErr := cleanup(projectDir); cleanupErr != nil && exitCode == 0 {
			exitCode = 1
		}
	}()

	if err := WritePidfile(projectDir, os.Getpid()); err != nil {
		if shimWritef(stderr, "harmonik supervise _shim: write pidfile: %v\n", err) != nil {
			return 1
		}
		return 1
	}

	cfg, err := ReadConfig(projectDir)
	if err != nil {
		if shimWritef(stderr, "harmonik supervise _shim: read config: %v\n", err) != nil {
			return 1
		}
		return 1
	}

	if len(cfg.Command) == 0 {
		return runWatchdogOnly(cfg, projectDir, stdout, stderr)
	}

	if !watchRestart {
		return runDirect(cfg, stderr)
	}

	return runWithSupervisor(cfg, projectDir, stderr)
}

func runDirect(cfg Config, stderr io.Writer) int {
	bin := cfg.Command[0]
	resolved, err := exec.LookPath(bin)
	if err != nil {
		if shimWritef(stderr, "harmonik supervise _shim: command not found %q: %v\n", bin, err) != nil {
			return 1
		}
		return 1
	}
	//nolint:gosec // G204: cfg.Command is the operator-provided supervisee argv from the trusted project config.
	if execErr := syscall.Exec(resolved, cfg.Command, buildPiEnv(cfg.APIKey)); execErr != nil {
		if shimWritef(stderr, "harmonik supervise _shim: exec %q: %v\n", resolved, execErr) != nil {
			return 1
		}
		return 1
	}
	return 0 // never reached
}

func buildPiEnv(apiKey string) []string {
	ambient := os.Environ()
	env := make([]string, 0, len(ambient)+1)
	for _, kv := range ambient {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if handler.IsCredentialDenyListKey(key) {
			continue
		}
		env = append(env, kv)
	}
	if apiKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+apiKey)
	}
	return env
}

func runWithSupervisor(cfg Config, projectDir string, stderr io.Writer) int {
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	policy := supervise.PolicyOnFailure
	if cfg.RestartPolicy == string(supervise.PolicyNever) {
		policy = supervise.PolicyNever
	}

	restartMax := cfg.RestartMax
	if restartMax == 0 {
		restartMax = 5
	}
	baseMS := cfg.RestartBaseMS
	if baseMS == 0 {
		baseMS = 1000
	}
	capMS := cfg.RestartCapMS
	if capMS == 0 {
		capMS = 60000
	}

	spec := supervise.Spec{
		Command:             cfg.Command,
		Policy:              policy,
		HeartbeatTTL:        durationFromMS(cfg.HeartbeatTTLMS),
		StartTimeout:        durationFromMS(cfg.StartTimeoutMS),
		CrashLoopWindow:     durationFromMS(cfg.CrashLoopWindowMS),
		HealthProbeInterval: durationFromMS(cfg.HealthProbeMS),
		StopTimeout:         durationFromMS(cfg.StopTimeoutMS),
		Backoff: supervise.BackoffConfig{
			Base:        time.Duration(baseMS) * time.Millisecond,
			Cap:         time.Duration(capMS) * time.Millisecond,
			Jitter:      0.2,
			MaxRestarts: restartMax,
		},
		// BaseEnv is the pre-filtered Pi env (CI-005): deny-list keys stripped
		// from ambient + Pi-scoped ANTHROPIC_API_KEY injected if configured.
		BaseEnv: buildPiEnv(cfg.APIKey),
	}

	sv := supervise.New(spec, log)

	ctx, stop := setupSignals()
	defer stop()

	RunAssetSkewCheck(projectDir, cfg, log, stderr)

	if daemonCmd := buildDaemonCmd(projectDir, cfg.MaxConcurrent); len(daemonCmd) > 0 {
		dwSpec := daemonWatchdogSpecFromConfig(cfg, projectDir, daemonCmd)
		dwSpec.CrashLogPath = filepath.Join(projectDir, ".harmonik", "state", "daemon.crash.log")
		dw := supervise.NewDaemonWatchdog(dwSpec, log)
		go func() {
			if err := dw.Run(ctx); err != nil && ctx.Err() == nil {
				if writeErr := shimWritef(stderr, "daemon-watchdog: exited: %v\n", err); writeErr != nil {
					log.ErrorContext(ctx, "write daemon-watchdog failure", "err", writeErr)
				}
			}
		}()
	}

	if err := sv.Run(ctx); err != nil {
		state := sv.Snapshot()
		if state.Status == supervise.StatusCrashLoop {
			if writeErr := shimWritef(stderr, "harmonik supervise: crash-loop detected after %d restarts\n",
				state.RestartCount); writeErr != nil {
				return 1
			}
		} else {
			if writeErr := shimWritef(stderr, "harmonik supervise: supervisor exited: %v\n", err); writeErr != nil {
				return 1
			}
		}
		return 1
	}
	return 0
}

func runWatchdogOnly(cfg Config, projectDir string, stdout, stderr io.Writer) int {
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := setupSignals()
	defer stop()

	RunAssetSkewCheck(projectDir, cfg, log, stderr)

	daemonCmd := buildDaemonCmd(projectDir, cfg.MaxConcurrent)
	if len(daemonCmd) == 0 {
		if shimWritef(stderr, "harmonik supervise _shim: watchdog-only: cannot resolve daemon binary\n") != nil {
			return 1
		}
		return 1
	}

	dwSpec := daemonWatchdogSpecFromConfig(cfg, projectDir, daemonCmd)
	dwSpec.CrashLogPath = filepath.Join(projectDir, ".harmonik", "state", "daemon.crash.log")
	dw := supervise.NewDaemonWatchdog(dwSpec, log)

	if shimWritef(stdout, "harmonik supervise: watchdog-only mode (no supervisee configured)\n") != nil {
		return 1
	}
	if err := dw.Run(ctx); err != nil && ctx.Err() == nil {
		if shimWritef(stderr, "daemon-watchdog: exited: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	return 0
}

func shimWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func buildDaemonCmd(projectDir string, maxConcurrent int) []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	cmd := []string{exe, "start", "daemon", "--project", projectDir, "--no-auto-pull"}
	if maxConcurrent > 0 {
		cmd = append(cmd, "--max-concurrent", fmt.Sprintf("%d", maxConcurrent))
	}
	if dh := os.Getenv("HARMONIK_DEFAULT_HARNESS"); dh != "" {
		cmd = append(cmd, "--default-harness", dh)
	}
	return cmd
}

func daemonWatchdogSpecFromConfig(cfg Config, projectDir string, daemonCmd []string) supervise.DaemonWatchdogSpec {
	return supervise.DaemonWatchdogSpec{
		SocketPath:    lifecycle.SocketPath(projectDir),
		Command:       daemonCmd,
		WorkDir:       projectDir,
		CheckInterval: durationFromMS(cfg.DWCheckIntervalMS),
		DialTimeout:   durationFromMS(cfg.DWDialTimeoutMS),
		ReviveBackoff: durationFromMS(cfg.DWReviveBackoffMS),
		ReviveWindow:  durationFromMS(cfg.DWReviveWindowMS),
		LedgerPath:    release.LedgerPath(projectDir),
		LastGoodPath:  release.LastGoodStatePath(projectDir),
	}
}

func durationFromMS(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func setupSignals() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal, 1)
		signalNotify(ch, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-ctx.Done():
		case <-ch:
			cancel()
		}
	}()
	return ctx, cancel
}
