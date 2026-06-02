package supervise

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"syscall"
	"time"
)

// DaemonWatchdogSpec configures the daemon revival watchdog.
type DaemonWatchdogSpec struct {
	// SocketPath is the Unix socket to probe for daemon liveness.
	SocketPath string
	// Command is the argv to spawn when the daemon is found dead. Command[0]
	// is the binary. The process is spawned detached (setsid) so it outlives
	// the supervisor pane.
	Command []string
	// WorkDir is the working directory for the revived daemon process.
	WorkDir string
	// CheckInterval is how often to probe daemon liveness. Default: 30s.
	CheckInterval time.Duration
	// DialTimeout caps the per-probe connection attempt. Default: 3s.
	DialTimeout time.Duration
	// MaxRevives is the maximum number of revival attempts before the watchdog
	// gives up. -1 = unlimited. Default: 3.
	MaxRevives int
	// ReviveBackoff is the grace period after spawning before the next liveness
	// probe, giving the daemon time to bind its socket. Default: 10s.
	ReviveBackoff time.Duration
}

func (s *DaemonWatchdogSpec) applyDefaults() {
	if s.CheckInterval == 0 {
		s.CheckInterval = 30 * time.Second
	}
	if s.DialTimeout == 0 {
		s.DialTimeout = 3 * time.Second
	}
	if s.MaxRevives == 0 {
		s.MaxRevives = 3
	}
	if s.ReviveBackoff == 0 {
		s.ReviveBackoff = 10 * time.Second
	}
}

// DaemonWatchdog probes daemon liveness on a fixed interval and spawns the
// daemon when it is found dead. This is the supervisor-owned revival path per
// CL-083: the cognition loop (bridge.ts) detects daemon_down and nudges the
// model; this component is the actor that actually restarts the daemon process.
type DaemonWatchdog struct {
	spec DaemonWatchdogSpec
	log  *slog.Logger
}

// NewDaemonWatchdog creates a DaemonWatchdog. SocketPath and Command must be
// non-empty or Run returns an error immediately.
func NewDaemonWatchdog(spec DaemonWatchdogSpec, log *slog.Logger) *DaemonWatchdog {
	spec.applyDefaults()
	return &DaemonWatchdog{spec: spec, log: log}
}

// Run is the main blocking loop. It exits when ctx is cancelled or the revival
// cap is reached. Safe to run concurrently with a Supervisor.Run on the same
// context — when the supervisor stops, ctx cancellation terminates the watchdog.
func (dw *DaemonWatchdog) Run(ctx context.Context) error {
	if dw.spec.SocketPath == "" {
		return fmt.Errorf("daemon-watchdog: SocketPath is required")
	}
	if len(dw.spec.Command) == 0 {
		return fmt.Errorf("daemon-watchdog: Command is required")
	}

	revives := 0
	ticker := time.NewTicker(dw.spec.CheckInterval)
	defer ticker.Stop()

	dw.log.Info("daemon-watchdog: started",
		"socket", dw.spec.SocketPath,
		"check_interval", dw.spec.CheckInterval,
		"max_revives", dw.spec.MaxRevives)

	for {
		select {
		case <-ctx.Done():
			dw.log.Info("daemon-watchdog: stopped")
			return ctx.Err()
		case <-ticker.C:
			if dw.isDaemonAlive(ctx) {
				continue
			}
			dw.log.Warn("daemon-watchdog: daemon not reachable",
				"socket", dw.spec.SocketPath, "revives_so_far", revives)

			if dw.spec.MaxRevives >= 0 && revives >= dw.spec.MaxRevives {
				dw.log.Error("daemon-watchdog: revival cap reached — giving up",
					"max_revives", dw.spec.MaxRevives)
				return fmt.Errorf("daemon-watchdog: revival cap reached after %d attempts", dw.spec.MaxRevives)
			}

			revives++
			dw.log.Warn("daemon-watchdog: spawning daemon",
				"attempt", revives, "cmd", dw.spec.Command)
			if err := dw.revive(); err != nil {
				dw.log.Error("daemon-watchdog: spawn failed",
					"attempt", revives, "err", err)
			} else {
				dw.log.Info("daemon-watchdog: daemon spawned — waiting for socket bind",
					"backoff", dw.spec.ReviveBackoff)
			}

			// Grace period: let the daemon bind its socket before the next probe.
			select {
			case <-ctx.Done():
				dw.log.Info("daemon-watchdog: stopped during revival backoff")
				return ctx.Err()
			case <-time.After(dw.spec.ReviveBackoff):
			}
		}
	}
}

// isDaemonAlive probes the daemon Unix socket. Returns true when the daemon is
// reachable; false on any error (absent, ECONNREFUSED, timeout, etc.).
func (dw *DaemonWatchdog) isDaemonAlive(ctx context.Context) bool {
	dialCtx, cancel := context.WithTimeout(ctx, dw.spec.DialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", dw.spec.SocketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// revive spawns the daemon as a detached process (setsid) so it can outlive
// the supervisor/shim pane. Stdout and stderr are not inherited (connected to
// os.DevNull); the daemon writes its own events to .harmonik/events/events.jsonl.
func (dw *DaemonWatchdog) revive() error {
	//nolint:gosec // command comes from operator-controlled config
	cmd := exec.Command(dw.spec.Command[0], dw.spec.Command[1:]...)
	// Detach from shim's process group and session so SIGTERM to the flywheel
	// pane does not cascade to the revived daemon.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if dw.spec.WorkDir != "" {
		cmd.Dir = dw.spec.WorkDir
	}
	return cmd.Start()
}
