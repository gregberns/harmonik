package supervise

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SupervisorWatchdogSpec configures the supervisor liveness watchdog.
type SupervisorWatchdogSpec struct {
	// PidfilePath is the path to supervisor.pid written by the shim at startup.
	PidfilePath string
	// CheckInterval is how often to probe supervisor liveness. Default: 60s.
	CheckInterval time.Duration
	// ReviveCmd is the argv to spawn when the supervisor is found dead. Nil
	// disables auto-revival; the watchdog only alarms (logs + OnAlarm callback).
	// Example: []string{"harmonik", "supervise", "start", "--watch-restart"}
	ReviveCmd []string
	// WorkDir is the working directory for the revival command.
	WorkDir string
	// MaxRevives caps consecutive revival attempts before the watchdog gives up.
	// The counter resets when the supervisor is confirmed alive after a revival.
	// -1 = unlimited. Default: 3.
	MaxRevives int
	// ReviveBackoff is the polling interval while waiting for the supervisor to
	// appear after a revival. Default: 5s.
	ReviveBackoff time.Duration
	// ReviveWindow is the maximum time to wait for the revived supervisor to
	// write its pidfile. Default: 30s.
	ReviveWindow time.Duration
	// OnAlarm, if non-nil, is called on each check where the supervisor is
	// found dead. Runs synchronously before any revival attempt; must not block.
	OnAlarm func()
}

func (s *SupervisorWatchdogSpec) applyDefaults() {
	if s.CheckInterval == 0 {
		s.CheckInterval = 60 * time.Second
	}
	if s.MaxRevives == 0 {
		s.MaxRevives = 3
	}
	if s.ReviveBackoff == 0 {
		s.ReviveBackoff = 5 * time.Second
	}
	if s.ReviveWindow == 0 {
		s.ReviveWindow = 30 * time.Second
	}
}

// SupervisorWatchdog monitors supervisor liveness from an external vantage
// point — the daemon, a standalone monitor, or any process other than the
// supervisor itself. When the supervisor is found dead, it fires an OnAlarm
// callback and optionally spawns a revival command.
//
// This is the complement of DaemonWatchdog: the supervisor owns DaemonWatchdog
// to revive the daemon; the daemon (or the external ops-monitor) owns
// SupervisorWatchdog to detect when the supervisor has itself gone away. This
// closes the blind spot where both supervisor and daemon die simultaneously
// and nothing in the fleet detects or alarms (hk-pen9: 7h11m undetected gap).
type SupervisorWatchdog struct {
	spec SupervisorWatchdogSpec
	log  *slog.Logger
}

// NewSupervisorWatchdog creates a SupervisorWatchdog. PidfilePath must be
// non-empty or Run returns an error immediately.
func NewSupervisorWatchdog(spec SupervisorWatchdogSpec, log *slog.Logger) *SupervisorWatchdog {
	spec.applyDefaults()
	return &SupervisorWatchdog{spec: spec, log: log}
}

// Run is the main blocking loop. It exits when ctx is cancelled or the revival
// cap is reached. Intended to run as a goroutine alongside the daemon's main
// loop — the daemon is external to the supervisor and can therefore detect
// supervisor death without sharing its fate.
func (sw *SupervisorWatchdog) Run(ctx context.Context) error {
	if sw.spec.PidfilePath == "" {
		return fmt.Errorf("supervisor-watchdog: PidfilePath is required")
	}

	revives := 0

	ticker := time.NewTicker(sw.spec.CheckInterval)
	defer ticker.Stop()

	sw.log.Info("supervisor-watchdog: started",
		"pidfile", sw.spec.PidfilePath,
		"check_interval", sw.spec.CheckInterval,
		"max_revives", sw.spec.MaxRevives)

	for {
		select {
		case <-ctx.Done():
			sw.log.Info("supervisor-watchdog: stopped")
			return ctx.Err()
		case <-ticker.C:
			if sw.isSupervisorAlive() {
				if revives > 0 {
					sw.log.Info("supervisor-watchdog: supervisor confirmed alive after revival — counter reset")
					revives = 0
				}
				continue
			}

			sw.log.Warn("supervisor-watchdog: supervisor not running",
				"pidfile", sw.spec.PidfilePath,
				"revives_so_far", revives)

			if sw.spec.OnAlarm != nil {
				sw.spec.OnAlarm()
			}

			if len(sw.spec.ReviveCmd) == 0 {
				continue
			}

			if sw.spec.MaxRevives >= 0 && revives >= sw.spec.MaxRevives {
				sw.log.Error("supervisor-watchdog: revival cap reached — giving up",
					"max_revives", sw.spec.MaxRevives)
				return fmt.Errorf("supervisor-watchdog: revival cap reached after %d attempts", sw.spec.MaxRevives)
			}

			revives++
			sw.log.Warn("supervisor-watchdog: spawning supervisor",
				"attempt", revives, "cmd", sw.spec.ReviveCmd)

			if err := sw.reviveWith(sw.spec.ReviveCmd); err != nil {
				sw.log.Error("supervisor-watchdog: spawn failed",
					"attempt", revives, "err", err)
				continue
			}

			sw.log.Info("supervisor-watchdog: supervisor spawned — waiting for pidfile",
				"window", sw.spec.ReviveWindow, "poll_interval", sw.spec.ReviveBackoff)

			if sw.pollUntilAlive(ctx, sw.spec.ReviveWindow, sw.spec.ReviveBackoff) {
				sw.log.Info("supervisor-watchdog: supervisor confirmed alive after revival")
				revives = 0
			}
		}
	}
}

// isSupervisorAlive probes supervisor liveness by reading the pidfile and
// sending kill(pid, 0). Returns true only when the pidfile exists, contains a
// valid PID, and the recorded process is alive.
func (sw *SupervisorWatchdog) isSupervisorAlive() bool {
	data, err := os.ReadFile(sw.spec.PidfilePath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// pollUntilAlive probes isSupervisorAlive at interval until the supervisor
// is live or window elapses. Returns true when the supervisor becomes live.
// The final sleep is capped at the remaining window time.
func (sw *SupervisorWatchdog) pollUntilAlive(ctx context.Context, window, interval time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		sleep := interval
		if remaining < sleep {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(sleep):
		}
		if sw.isSupervisorAlive() {
			return true
		}
	}
}

// reviveWith spawns the supervisor revival command as a detached process
// (setsid) so it outlives the caller's process group.
func (sw *SupervisorWatchdog) reviveWith(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("supervisor-watchdog: reviveWith: empty argv")
	}
	//nolint:gosec // G204: argv is operator-controlled config
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if sw.spec.WorkDir != "" {
		cmd.Dir = sw.spec.WorkDir
	}
	return cmd.Start()
}
