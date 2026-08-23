// Package supervise manages and monitors long-running Harmonik processes.
package supervise

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
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

	// CrashLogPath is the base path for the rotating daemon crash log
	// (e.g., <projectDir>/.harmonik/state/daemon.crash.log). On each revival,
	// stdout and stderr of the spawned daemon are redirected to this file.
	// Previous boots are kept at <CrashLogPath>.1, .2, ..., .{CrashLogKeep-1}.
	// Empty disables capture (output falls to /dev/null as before).
	CrashLogPath string

	// CrashLogKeep is the total number of crash logs to retain (current boot
	// plus this many numbered backups). Default: 5. No effect when
	// CrashLogPath is empty.
	CrashLogKeep int
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
		s.ReviveWindow = 15 * time.Minute
	}
	if s.CrashLogKeep == 0 {
		s.CrashLogKeep = 5
	}
}

// DaemonWatchdog probes daemon liveness on a fixed interval and spawns the
// daemon when it is found dead. This is the supervisor-owned revival path per
// CL-083: the cognition loop (bridge.ts) detects daemon_down and nudges the
// model; this component is the actor that actually restarts the daemon process.
type DaemonWatchdog struct {
	spec       DaemonWatchdogSpec
	log        *slog.Logger
	dialDaemon func(context.Context) (io.Closer, error)
}

// NewDaemonWatchdog creates a DaemonWatchdog. SocketPath and Command must be
// non-empty or Run returns an error immediately.
func NewDaemonWatchdog(spec DaemonWatchdogSpec, log *slog.Logger) *DaemonWatchdog {
	spec.applyDefaults()
	return &DaemonWatchdog{
		spec: spec,
		log:  log,
		dialDaemon: func(ctx context.Context) (io.Closer, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", spec.SocketPath)
		},
	}
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
	activeCommand := dw.spec.Command
	var adoptDeadline time.Time

	ticker := time.NewTicker(dw.spec.CheckInterval)
	defer ticker.Stop()

	dw.log.InfoContext(ctx, "daemon-watchdog: started",
		"socket", dw.spec.SocketPath,
		"check_interval", dw.spec.CheckInterval,
		"max_revives", dw.spec.MaxRevives)

	for {
		select {
		case <-ctx.Done():
			dw.log.InfoContext(ctx, "daemon-watchdog: stopped")
			return ctx.Err()
		case <-ticker.C:
			if dw.isDaemonAlive(ctx) {
				if !adoptDeadline.IsZero() && !time.Now().Before(adoptDeadline) {
					dw.pinLastGood(ctx, activeCommand[0])
					adoptDeadline = time.Time{}
				}
				continue
			}

			crashedInHealthWindow := !adoptDeadline.IsZero()
			adoptDeadline = time.Time{}

			dw.log.WarnContext(ctx, "daemon-watchdog: daemon not reachable",
				"socket", dw.spec.SocketPath, "revives_so_far", revives,
				"crashed_in_health_window", crashedInHealthWindow)

			if dw.spec.MaxRevives >= 0 && revives >= dw.spec.MaxRevives {
				dw.log.ErrorContext(ctx, "daemon-watchdog: revival cap reached — giving up",
					"max_revives", dw.spec.MaxRevives)
				return fmt.Errorf("daemon-watchdog: revival cap reached after %d attempts", dw.spec.MaxRevives)
			}

			if crashedInHealthWindow {
				activeCommand = dw.applyLastGoodFallback(ctx, activeCommand, "crash within health window")
			}

			activeCommand = dw.resolveReviveCommand(ctx, activeCommand)

			revives++
			dw.log.WarnContext(ctx, "daemon-watchdog: spawning daemon",
				"attempt", revives, "cmd", activeCommand)
			spawnErr := dw.reviveWith(ctx, activeCommand)
			if spawnErr != nil {
				dw.log.ErrorContext(ctx, "daemon-watchdog: spawn failed",
					"attempt", revives, "err", spawnErr)
				continue
			}
			dw.log.InfoContext(ctx, "daemon-watchdog: daemon spawned — waiting for socket bind",
				"window", dw.spec.ReviveWindow, "poll_interval", dw.spec.ReviveBackoff)

			if dw.pollUntilAlive(ctx, dw.spec.ReviveWindow, dw.spec.ReviveBackoff) {
				revives = 0
				adoptDeadline = time.Now().Add(dw.spec.CheckInterval)
				dw.log.InfoContext(ctx, "daemon-watchdog: daemon confirmed alive after revival — health window started",
					"adopt_after", adoptDeadline.Format(time.RFC3339))
			}
		}
	}
}

func (dw *DaemonWatchdog) resolveReviveCommand(ctx context.Context, current []string) []string {
	if dw.spec.LedgerPath == "" || len(current) == 0 {
		return current
	}
	hash := commitHashOf(ctx, current[0])
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
		dw.log.WarnContext(ctx, "daemon-watchdog: refused_yank — binary is yanked in ledger",
			"semver", e.Semver,
			"commit", hash,
			"reason", e.YankedReason)
		fmt.Fprintf(os.Stderr,
			"daemon-watchdog: refused_yank: %s %s — %s\n",
			e.Semver, hash[:min(12, len(hash))], e.YankedReason)

		return dw.applyLastGoodFallback(ctx, current, "yanked binary")
	}
	return current
}

func (dw *DaemonWatchdog) applyLastGoodFallback(ctx context.Context, current []string, reason string) []string {
	if dw.spec.LastGoodPath == "" || len(current) == 0 {
		fmt.Fprintf(os.Stderr, "daemon-watchdog: last-good fallback unavailable (%s): no LastGoodPath configured\n", reason)
		return current
	}
	lastGood, err := release.ReadLastGoodBinary(dw.spec.LastGoodPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon-watchdog: last-good fallback unavailable (%s): %v\n", reason, err)
		return current
	}
	if current[0] == lastGood {
		dw.log.WarnContext(ctx, "daemon-watchdog: already running last-good binary; staying with it",
			"reason", reason, "bin", lastGood)
		return current
	}
	dw.log.InfoContext(ctx, "daemon-watchdog: falling back to last-good binary",
		"reason", reason, "last_good", lastGood, "was", current[0])
	result := make([]string, len(current))
	copy(result, current)
	result[0] = lastGood
	return result
}

func (dw *DaemonWatchdog) pinLastGood(ctx context.Context, binPath string) {
	if dw.spec.LastGoodPath == "" {
		return
	}

	if dw.spec.LedgerPath != "" {
		hash := commitHashOf(ctx, binPath)
		if hash != "" {
			if entries, err := release.LoadLedgerFile(dw.spec.LedgerPath); err == nil {
				for _, e := range entries {
					if e.CommitHash == hash && e.Prerelease {
						dw.log.InfoContext(ctx, "daemon-watchdog: skipping last-good pin — binary is pre-release",
							"bin", binPath, "semver", e.Semver, "commit", hash)
						return
					}
				}
			}
		}
	}

	if err := release.PinLastGoodBinary(dw.spec.LastGoodPath, binPath); err != nil {
		dw.log.WarnContext(ctx, "daemon-watchdog: failed to pin last-good binary", "bin", binPath, "err", err)
		return
	}
	dw.log.InfoContext(ctx, "daemon-watchdog: pinned last-good binary",
		"bin", binPath,
		"last_good", binPath+".last-good",
		"state", dw.spec.LastGoodPath)
}

func commitHashOf(ctx context.Context, binPath string) string {
	out, err := exec.CommandContext(ctx, binPath, "version").Output()
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

func (dw *DaemonWatchdog) isDaemonAlive(ctx context.Context) bool {
	dialCtx, cancel := context.WithTimeout(ctx, dw.spec.DialTimeout)
	defer cancel()
	conn, err := dw.dialDaemon(dialCtx)
	if err != nil {
		return false
	}
	if closeErr := conn.Close(); closeErr != nil {
		dw.log.DebugContext(ctx, "daemon-watchdog: probe connection close failed", "err", closeErr)
	}
	return true
}

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

func (dw *DaemonWatchdog) reviveWith(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("daemon-watchdog: reviveWith: empty argv")
	}
	//nolint:gosec // G204: argv comes from operator-controlled config or last-good state
	// This process is deliberately detached so it survives cancellation of the
	// watchdog that revived it; CommandContext would violate that contract.
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:noctx // detached child must survive watchdog cancellation
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if dw.spec.WorkDir != "" {
		cmd.Dir = dw.spec.WorkDir
	}
	cmd.Env = append(os.Environ(), "GOTRACEBACK=all")
	if dw.spec.CrashLogPath != "" {
		f, err := openCrashLog(dw.spec.CrashLogPath, dw.spec.CrashLogKeep, argv)
		if err != nil {
			dw.log.WarnContext(ctx, "daemon-watchdog: failed to open crash log; daemon output will be lost",
				"path", dw.spec.CrashLogPath, "err", err)
		} else {
			cmd.Stdout = f
			cmd.Stderr = f
			defer func() {
				if closeErr := f.Close(); closeErr != nil {
					dw.log.WarnContext(ctx, "daemon-watchdog: close crash log", "err", closeErr, "path", dw.spec.CrashLogPath)
				}
			}()
		}
	}
	return cmd.Start()
}

func openCrashLog(path string, keep int, argv []string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), core.HarmonikDirMode); err != nil {
		return nil, fmt.Errorf("crash log dir: %w", err)
	}
	if err := os.Remove(path + "." + strconv.Itoa(keep-1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove oldest crash log: %w", err)
	}
	for i := keep - 2; i >= 1; i-- {
		if err := renameIfExists(path+"."+strconv.Itoa(i), path+"."+strconv.Itoa(i+1)); err != nil {
			return nil, err
		}
	}
	if err := renameIfExists(path, path+".1"); err != nil {
		return nil, err
	}
	f, err := os.Create(path) //nolint:gosec // G304: operator-configured path
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "=== daemon boot cmd=%s\n", strings.Join(argv, " ")); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, fmt.Errorf("write crash log header: %w", err)
	}
	return f, nil
}

func renameIfExists(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rotate crash log %q to %q: %w", oldPath, newPath, err)
	}
	return nil
}
