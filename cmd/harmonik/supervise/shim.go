package supervisecmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
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
//  4. If --watch-restart: runs internal/supervise.Supervisor with the configured command.
//  5. Otherwise: exec-replaces itself with the configured command directly.
//  6. On exit: removes sentinel and pidfile.
//
// This is an internal subcommand not meant for direct operator use.
//
// Spec ref: process-lifecycle.md §4.5 PL-019c-f, §4.10 PL-028d.
func RunShim(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "harmonik supervise _shim: missing project directory argument")
		return 1
	}

	projectDir := args[0]
	watchRestart := false
	for _, a := range args[1:] {
		if a == "--watch-restart" {
			watchRestart = true
		}
	}

	// Acquire supervisor.lock (fd-lifetime; kernel releases on shim exit).
	//nolint:gosec // G304
	lockFd, err := os.OpenFile(LockPath(projectDir), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise _shim: open lock: %v\n", err)
		return 1
	}
	// Blocking flock: wait until any prior holder releases (brief race window
	// after start exits).
	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFd.Close()
		fmt.Fprintf(stderr, "harmonik supervise _shim: flock: %v\n", err)
		return 1
	}
	// lockFd is intentionally kept open for the shim's lifetime.
	// nolint:gocritic — intentional leak; lockFd must outlive this func stack.
	defer func() {
		_ = lockFd.Close()
		_ = cleanup(projectDir)
	}()

	// Write own PID (PL-019d).
	if err := WritePidfile(projectDir, os.Getpid()); err != nil {
		fmt.Fprintf(stderr, "harmonik supervise _shim: write pidfile: %v\n", err)
		return 1
	}

	// Read config.json (PL-019e): supervisor re-reads config at startup, must
	// NOT hot-reload.
	cfg, err := ReadConfig(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise _shim: read config: %v\n", err)
		return 1
	}

	if len(cfg.Command) == 0 {
		fmt.Fprintf(stderr,
			"harmonik supervise _shim: config.json missing 'command' field — nothing to run\n"+
				"  Set config.command to the supervisee argv (e.g. [\"claude\", \"--pi\"])\n")
		return 1
	}

	if !watchRestart {
		// Non-restart mode: exec-replace with the supervisee directly.
		return runDirect(cfg, stderr)
	}

	// Watch-restart mode: use internal/supervise.Supervisor.
	return runWithSupervisor(cfg, projectDir, stdout, stderr)
}

// runDirect exec-replaces the shim with the supervisee command.
// Uses a scoped Pi env (CI-005) rather than blanket os.Environ().
func runDirect(cfg Config, stderr io.Writer) int {
	bin := cfg.Command[0]
	// Use exec.LookPath for correct PATH resolution including exec-bit check.
	resolved, err := exec.LookPath(bin)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise _shim: command not found %q: %v\n", bin, err)
		return 1
	}
	if execErr := syscall.Exec(resolved, cfg.Command, buildPiEnv(cfg.APIKey)); execErr != nil {
		fmt.Fprintf(stderr, "harmonik supervise _shim: exec %q: %v\n", resolved, execErr)
		return 1
	}
	return 0 // never reached
}

// buildPiEnv constructs the Pi process environment per specs/credential-isolation.md
// §4.3 CI-005: strips all credential deny-list keys from the ambient env, then
// injects apiKey as ANTHROPIC_API_KEY when non-empty.
//
// This is the scoped-injection builder: it never passes os.Environ() directly
// to the Pi process, ensuring no ambient credential leaks through inheritance.
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

// runWithSupervisor runs the supervisee under internal/supervise.Supervisor
// with restart policy from config.json, and concurrently runs a DaemonWatchdog
// that revives the harmonik daemon if it dies (supervisor-owned revival, CL-083).
func runWithSupervisor(cfg Config, projectDir string, stdout, stderr io.Writer) int {
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
		Command:         cfg.Command,
		Policy:          policy,
		StartTimeout:    30 * time.Second,
		CrashLoopWindow: 60 * time.Second,
		StopTimeout:     10 * time.Second,
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

	// Boot-time asset version-skew check (hk-yqx9): compare the RUNNING binary's
	// embedded asset bundle against what this project last installed
	// (.harmonik/assets.lock) and, on skew, notify the captain to run sync-assets.
	// Best-effort, detection+notify only — never writes project files; runs ONCE at
	// boot (a binary swap requires a supervisor restart, which re-runs this).
	RunAssetSkewCheck(projectDir, cfg, log, stderr)

	// Daemon watchdog: runs alongside the Pi supervisor, reviving the harmonik
	// daemon when it is detected dead via socket probe (CL-083 supervisor-owned
	// revival). The daemon command is built from the current executable and the
	// project directory known to the shim.
	if daemonCmd := buildDaemonCmd(projectDir, cfg.MaxConcurrent); len(daemonCmd) > 0 {
		dw := supervise.NewDaemonWatchdog(supervise.DaemonWatchdogSpec{
			SocketPath:   lifecycle.SocketPath(projectDir),
			Command:      daemonCmd,
			WorkDir:      projectDir,
			LedgerPath:   release.LedgerPath(projectDir),
			LastGoodPath: release.LastGoodStatePath(projectDir),
		}, log)
		go func() {
			if err := dw.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(stderr, "daemon-watchdog: exited: %v\n", err)
			}
		}()
	}

	if err := sv.Run(ctx); err != nil {
		state := sv.Snapshot()
		if state.Status == supervise.StatusCrashLoop {
			fmt.Fprintf(stderr, "harmonik supervise: crash-loop detected after %d restarts\n",
				state.RestartCount)
		} else {
			fmt.Fprintf(stderr, "harmonik supervise: supervisor exited: %v\n", err)
		}
		return 1
	}
	return 0
}

// buildDaemonCmd constructs the harmonik daemon revival argv from the current
// executable and the project parameters. The daemon is restarted with
// --no-auto-pull (queue-only, safe default per process-lifecycle.md §"Start the
// daemon once") and --max-concurrent when cfg.MaxConcurrent is set.
// Returns nil when the executable path cannot be resolved.
func buildDaemonCmd(projectDir string, maxConcurrent int) []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	cmd := []string{exe, "--project", projectDir, "--no-auto-pull"}
	if maxConcurrent > 0 {
		cmd = append(cmd, "--max-concurrent", fmt.Sprintf("%d", maxConcurrent))
	}
	return cmd
}

// setupSignals returns a context cancelled on SIGINT/SIGTERM and a stop func.
// Imported via os/signal to avoid pulling in signal package at package level.
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
