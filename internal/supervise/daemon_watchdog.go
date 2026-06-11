package supervise

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/release"
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
	// MaxRevives is the maximum number of consecutive failed revival attempts
	// before the watchdog gives up. The counter resets to 0 whenever the daemon
	// is confirmed alive after a revival, so isolated clean revivals spread over
	// days do not accumulate toward this cap. -1 = unlimited. Default: 3.
	MaxRevives int
	// ReviveBackoff is the polling interval used while waiting for a just-revived
	// daemon to bind its socket. Default: 10s.
	ReviveBackoff time.Duration
	// ReviveWindow is the maximum time the watchdog waits for a revived daemon to
	// bind its socket before counting the revival as failed. Must cover the
	// daemon's maximum possible boot-backoff delay (restartBackoffCap = 10m).
	// Default: 15m.
	ReviveWindow time.Duration

	// LedgerPath is the path to the release ledger JSON file used to check
	// yanked status before adopting a newly-installed binary. Empty disables
	// the yanked check.
	//
	// Spec ref: specs/release-pipeline.md §7.2 — supervisor yanked-binary guard.
	LedgerPath string

	// LastGoodPath is the path to the last-good-binary state file managed by
	// release.WriteLastGoodBinary / release.ReadLastGoodBinary. Empty disables
	// last-good tracking and yanked fallback.
	//
	// Spec ref: specs/release-pipeline.md §7.2 — "persist path to last
	// known-good binary in a state file".
	LastGoodPath string
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
	if s.ReviveWindow == 0 {
		// 15m covers restartBackoffCap (10m) plus margin for socket-bind latency.
		s.ReviveWindow = 15 * time.Minute
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
//
// Last-good pin: after each successful revival, the watchdog starts a health
// window equal to CheckInterval. On the next alive tick after the window
// expires, the binary is pinned as the last-good binary (spec §7.2).
//
// Yanked-binary guard: before each revival, the watchdog checks the release
// ledger for the binary's commit hash. If the binary is yanked and a last-good
// binary is available, it is used as the revive command instead (spec §7.2).
func (dw *DaemonWatchdog) Run(ctx context.Context) error {
	if dw.spec.SocketPath == "" {
		return fmt.Errorf("daemon-watchdog: SocketPath is required")
	}
	if len(dw.spec.Command) == 0 {
		return fmt.Errorf("daemon-watchdog: Command is required")
	}

	revives := 0
	// activeCommand is the command used for the next revival. It starts as the
	// configured command but may be switched to the last-good binary when the
	// configured binary is yanked.
	activeCommand := dw.spec.Command
	// adoptDeadline is non-zero when we are tracking a health window after a
	// revival. When time.Now() >= adoptDeadline and the daemon is still alive,
	// the binary is pinned as last-good.
	var adoptDeadline time.Time

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
				// Daemon alive: check if the health window has elapsed.
				if !adoptDeadline.IsZero() && !time.Now().Before(adoptDeadline) {
					dw.pinLastGood(activeCommand[0])
					adoptDeadline = time.Time{}
				}
				continue
			}

			// Daemon dead: note whether it crashed within the health window
			// before clearing adoptDeadline.
			crashedInHealthWindow := !adoptDeadline.IsZero()
			adoptDeadline = time.Time{}

			dw.log.Warn("daemon-watchdog: daemon not reachable",
				"socket", dw.spec.SocketPath, "revives_so_far", revives,
				"crashed_in_health_window", crashedInHealthWindow)

			if dw.spec.MaxRevives >= 0 && revives >= dw.spec.MaxRevives {
				dw.log.Error("daemon-watchdog: revival cap reached — giving up",
					"max_revives", dw.spec.MaxRevives)
				return fmt.Errorf("daemon-watchdog: revival cap reached after %d attempts", dw.spec.MaxRevives)
			}

			// §7.2.3: if the daemon crashed before being adopted (within the
			// health window), fall back to the last-good binary for the next
			// revival attempt so a crash-looping non-yanked binary does not
			// keep replacing a known-good one.
			if crashedInHealthWindow {
				activeCommand = dw.applyLastGoodFallback(activeCommand, "crash within health window")
			}

			// §7.2.1: if the active binary is yanked in the ledger, fall back
			// to the last-good binary.
			activeCommand = dw.resolveReviveCommand(activeCommand)

			revives++
			dw.log.Warn("daemon-watchdog: spawning daemon",
				"attempt", revives, "cmd", activeCommand)
			spawnErr := dw.reviveWith(activeCommand)
			if spawnErr != nil {
				dw.log.Error("daemon-watchdog: spawn failed",
					"attempt", revives, "err", spawnErr)
				// Spawn failed — skip the revival window poll; the daemon
				// process was never started so it cannot bind the socket.
				continue
			}
			dw.log.Info("daemon-watchdog: daemon spawned — waiting for socket bind",
				"window", dw.spec.ReviveWindow, "poll_interval", dw.spec.ReviveBackoff)

			// Poll until the daemon binds its socket or ReviveWindow expires.
			// ReviveWindow must be >= restartBackoffCap (10m) so that a daemon
			// sleeping through its boot-backoff delay does not consume a phantom
			// revive slot before it has had a chance to bind.
			// If the daemon comes alive within the window, reset the windowed
			// counter so isolated clean revivals spread over days do not accumulate
			// toward the lifetime cap. Then start the health window.
			if dw.pollUntilAlive(ctx, dw.spec.ReviveWindow, dw.spec.ReviveBackoff) {
				revives = 0
				adoptDeadline = time.Now().Add(dw.spec.CheckInterval)
				dw.log.Info("daemon-watchdog: daemon confirmed alive after revival — health window started",
					"adopt_after", adoptDeadline.Format(time.RFC3339))
			}
		}
	}
}

// resolveReviveCommand returns the command to use for the next revival. If the
// configured binary is yanked in the ledger, it falls back to the last-good
// binary. Returns current unchanged if no yank is detected or no fallback is
// available.
//
// Spec ref: specs/release-pipeline.md §7.2.1 — supervisor yanked-binary guard.
func (dw *DaemonWatchdog) resolveReviveCommand(current []string) []string {
	if dw.spec.LedgerPath == "" || len(current) == 0 {
		return current
	}
	hash := commitHashOf(current[0])
	if hash == "" {
		return current
	}
	entries, err := release.LoadLedgerFile(dw.spec.LedgerPath)
	if err != nil {
		return current
	}
	for _, e := range entries {
		if e.CommitHash != hash || !e.Yanked {
			continue
		}
		dw.log.Warn("daemon-watchdog: refused_yank — binary is yanked in ledger",
			"semver", e.Semver,
			"commit", hash,
			"reason", e.YankedReason)
		fmt.Fprintf(os.Stderr,
			"daemon-watchdog: refused_yank: %s %s — %s\n",
			e.Semver, hash[:min(12, len(hash))], e.YankedReason)

		return dw.applyLastGoodFallback(current, "yanked binary")
	}
	return current
}

// applyLastGoodFallback replaces current[0] with the last-good binary path
// when one is available. Returns current unchanged when LastGoodPath is unset
// or no last-good binary has been recorded. reason is used for log messages.
//
// Spec ref: specs/release-pipeline.md §7.2.3 (crash fallback) and §7.2.1
// (yanked fallback).
func (dw *DaemonWatchdog) applyLastGoodFallback(current []string, reason string) []string {
	if dw.spec.LastGoodPath == "" || len(current) == 0 {
		fmt.Fprintf(os.Stderr, "daemon-watchdog: last-good fallback unavailable (%s): no LastGoodPath configured\n", reason)
		return current
	}
	lastGood, err := release.ReadLastGoodBinary(dw.spec.LastGoodPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon-watchdog: last-good fallback unavailable (%s): %v\n", reason, err)
		return current
	}
	// Don't switch if we are already running the last-good binary.
	if current[0] == lastGood {
		dw.log.Warn("daemon-watchdog: already running last-good binary; staying with it",
			"reason", reason, "bin", lastGood)
		return current
	}
	dw.log.Info("daemon-watchdog: falling back to last-good binary",
		"reason", reason, "last_good", lastGood, "was", current[0])
	result := make([]string, len(current))
	copy(result, current)
	result[0] = lastGood
	return result
}

// pinLastGood copies binPath to binPath+".last-good" and updates the state
// file. Logs at Warn on failure (non-fatal: the daemon is still running).
//
// §2.4 MUST NOT: pre-release binaries are never adopted as last-good.
func (dw *DaemonWatchdog) pinLastGood(binPath string) {
	if dw.spec.LastGoodPath == "" {
		return
	}

	// §2.4: refuse to adopt pre-release binaries as last-good.
	if dw.spec.LedgerPath != "" {
		hash := commitHashOf(binPath)
		if hash != "" {
			if entries, err := release.LoadLedgerFile(dw.spec.LedgerPath); err == nil {
				for _, e := range entries {
					if e.CommitHash == hash && e.Prerelease {
						dw.log.Info("daemon-watchdog: skipping last-good pin — binary is pre-release",
							"bin", binPath, "semver", e.Semver, "commit", hash)
						return
					}
				}
			}
		}
	}

	if err := release.PinLastGoodBinary(dw.spec.LastGoodPath, binPath); err != nil {
		dw.log.Warn("daemon-watchdog: failed to pin last-good binary", "bin", binPath, "err", err)
		return
	}
	dw.log.Info("daemon-watchdog: pinned last-good binary",
		"bin", binPath,
		"last_good", binPath+".last-good",
		"state", dw.spec.LastGoodPath)
}

// commitHashOf runs "$bin version" and extracts the commit hash from the
// output. Output format (normative): "harmonik v0.y.z (commit: <sha>)".
// Returns empty string on any error.
func commitHashOf(binPath string) string {
	out, err := exec.Command(binPath, "version").Output() //nolint:gosec // G204: binPath is operator-controlled
	if err != nil {
		return ""
	}
	s := string(out)
	const prefix = "(commit: "
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	j := strings.Index(rest, ")")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
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

// pollUntilAlive probes isDaemonAlive at interval until the daemon is reachable
// or window elapses. Returns true when the daemon becomes reachable, false when
// the window expires or ctx is cancelled. The final sleep is capped at the
// remaining window time so the function does not overshoot the deadline by a
// full interval.
func (dw *DaemonWatchdog) pollUntilAlive(ctx context.Context, window, interval time.Duration) bool {
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
		if dw.isDaemonAlive(ctx) {
			return true
		}
	}
}

// reviveWith spawns the daemon using argv as a detached process (setsid) so
// it can outlive the supervisor/shim pane. Stdout and stderr are not inherited
// (connected to os.DevNull); the daemon writes its own events to
// .harmonik/events/events.jsonl.
func (dw *DaemonWatchdog) reviveWith(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("daemon-watchdog: reviveWith: empty argv")
	}
	//nolint:gosec // G204: argv comes from operator-controlled config or last-good state
	cmd := exec.Command(argv[0], argv[1:]...)
	// Detach from shim's process group and session so SIGTERM to the flywheel
	// pane does not cascade to the revived daemon.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if dw.spec.WorkDir != "" {
		cmd.Dir = dw.spec.WorkDir
	}
	return cmd.Start()
}
