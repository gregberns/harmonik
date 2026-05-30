package supervise

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// RestartPolicy controls when the supervisor respawns the child process.
type RestartPolicy string

const (
	// PolicyNever means a crash leaves the pane stopped; shim exits.
	PolicyNever RestartPolicy = "never"
	// PolicyOnFailure means restart iff exit code != 0.
	PolicyOnFailure RestartPolicy = "on-failure"
)

// BackoffConfig mirrors handlercontract.ProvisioningBackoffConfig for the
// supervisor's restart-delay envelope.
type BackoffConfig struct {
	// Base is the initial restart delay. Default: 1s.
	Base time.Duration
	// Cap is the maximum restart delay. Default: 60s.
	Cap time.Duration
	// Jitter is a fraction [0,1] of random jitter added to each delay. Default: 0.2.
	Jitter float64
	// MaxRestarts is the restart cap; -1 means unlimited. Default: 5.
	MaxRestarts int
}

// DefaultBackoffConfig is the backoff configuration used when none is supplied.
var DefaultBackoffConfig = BackoffConfig{
	Base:        1 * time.Second,
	Cap:         60 * time.Second,
	Jitter:      0.2,
	MaxRestarts: 5,
}

// Spec is the immutable configuration for a single supervised process.
type Spec struct {
	// Command is argv to exec; Command[0] is the binary.
	Command []string
	// Env is the supplemental environment; nil inherits the parent's env.
	Env []string
	// WorkDir is the working directory; "" inherits the shim's CWD.
	WorkDir string
	// HeartbeatPath is the path to .harmonik/cognition/heartbeat.json written
	// by the supervisee. Empty disables the heartbeat probe.
	HeartbeatPath string
	// HeartbeatTTL is how stale the heartbeat file may be before the supervisor
	// marks the process unhealthy. Default: 90s.
	HeartbeatTTL time.Duration
	// HealthProbeInterval is how often the health probe runs (liveness +
	// heartbeat-freshness check). Default: 15s.
	HealthProbeInterval time.Duration
	// StopTimeout is the bounded SIGTERM→SIGKILL window used by Stop() when the
	// caller passes a zero timeout. It also caps the run-loop's stop path.
	// Default: 10s.
	StopTimeout time.Duration
	// Policy controls restart behaviour. Default: PolicyOnFailure.
	Policy RestartPolicy
	// Backoff is the restart-delay envelope.
	Backoff BackoffConfig
	// StartTimeout is the assume-running gate: after this duration the
	// supervisor transitions "starting" → "running" if no exit has occurred.
	// Default: 30s.
	StartTimeout time.Duration
	// CrashLoopWindow is the sliding window used for crash-loop detection.
	// Default: 60s.
	CrashLoopWindow time.Duration
}

func (s *Spec) applyDefaults() {
	if s.HeartbeatTTL == 0 {
		s.HeartbeatTTL = 90 * time.Second
	}
	if s.Policy == "" {
		s.Policy = PolicyOnFailure
	}
	if s.Backoff.Base == 0 {
		s.Backoff = DefaultBackoffConfig
	}
	if s.StartTimeout == 0 {
		s.StartTimeout = 30 * time.Second
	}
	if s.CrashLoopWindow == 0 {
		s.CrashLoopWindow = 60 * time.Second
	}
	if s.HealthProbeInterval == 0 {
		s.HealthProbeInterval = 15 * time.Second
	}
	if s.StopTimeout == 0 {
		s.StopTimeout = 10 * time.Second
	}
}

// Status represents the observable lifecycle state of the supervised process.
type Status string

const (
	StatusIdle      Status = "idle"
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusUnhealthy Status = "unhealthy"
	StatusStopped   Status = "stopped"
	StatusCrashLoop Status = "crashloop"
)

// State is a point-in-time snapshot of the supervisor's observable state.
type State struct {
	PID          int
	Status       Status
	StartedAt    time.Time
	RestartCount int
	LastExitCode int
}

// Supervisor manages the lifecycle of a single child process, applying restart
// policy, exponential backoff, and crash-loop detection per PL-019(f).
type Supervisor struct {
	spec   Spec
	state  atomic.Pointer[State]
	log    *slog.Logger
	stopCh chan struct{} // closed by Stop() to signal the run loop
	// stopTimeoutNanos holds the SIGTERM→SIGKILL deadline (in ns) requested by
	// the most recent Stop() call. Read by terminateChild via atomics so the
	// run-loop goroutine honours the caller's timeout, not a hardcoded one.
	stopTimeoutNanos atomic.Int64
}

// New creates a new Supervisor. The caller is responsible for acquiring the
// advisory lock at .harmonik/cognition/supervisor.lock (PL-019(c)) before
// calling Run.
func New(spec Spec, log *slog.Logger) *Supervisor {
	spec.applyDefaults()
	s := &Supervisor{
		spec:   spec,
		log:    log,
		stopCh: make(chan struct{}),
	}
	s.state.Store(&State{Status: StatusIdle})
	return s
}

// Snapshot returns a copy of the current supervisor state.
func (s *Supervisor) Snapshot() State {
	return *s.state.Load()
}

// Stop signals the supervisor to terminate. It sends SIGTERM to the child,
// waits up to timeout, then sends SIGKILL, per PL-011. A zero (or negative)
// timeout falls back to Spec.StopTimeout (default 10s).
//
// Stop may be called from any goroutine concurrently with Run. The timeout is
// recorded before the stop signal is delivered so the run-loop's terminateChild
// observes it.
func (s *Supervisor) Stop(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = s.spec.StopTimeout
	}
	s.stopTimeoutNanos.Store(int64(timeout))
	select {
	case <-s.stopCh:
		// already stopped
	default:
		close(s.stopCh)
	}
	return nil
}

// Run is the main blocking loop. It spawns the child and handles exit →
// policy evaluation → backoff → respawn. Run returns when:
//   - ctx is cancelled,
//   - Stop is called,
//   - the restart cap is reached (crashloop),
//   - the policy is PolicyNever and the child exits.
func (s *Supervisor) Run(ctx context.Context) error {
	restartTimes := make([]time.Time, 0, s.spec.Backoff.MaxRestarts+1)
	backoff := s.spec.Backoff.Base

	for {
		cmd := s.buildCmd()
		s.setState(State{
			Status:       StatusStarting,
			StartedAt:    time.Now(),
			RestartCount: len(restartTimes),
		})

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("supervise: start: %w", err)
		}

		pid := cmd.Process.Pid
		s.log.Info("supervise: child started", "pid", pid, "restarts", len(restartTimes))
		s.setState(State{
			PID:          pid,
			Status:       StatusStarting,
			StartedAt:    time.Now(),
			RestartCount: len(restartTimes),
		})

		// Assume-running gate: transition starting → running after StartTimeout
		// if the child hasn't exited yet.
		assumeTimer := time.AfterFunc(s.spec.StartTimeout, func() {
			cur := s.state.Load()
			if cur.Status == StatusStarting {
				next := *cur
				next.Status = StatusRunning
				s.state.Store(&next)
				s.log.Info("supervise: assume-running gate fired", "pid", pid)
			}
		})

		// Health probe goroutine (runs for the lifetime of this child).
		healthDone := make(chan struct{})
		var stopHealthOnce sync.Once
		stopHealth := func() { stopHealthOnce.Do(func() { close(healthDone) }) }
		if s.spec.HeartbeatPath != "" {
			go s.runHealthProbe(pid, healthDone)
		}

		// Wait for child exit in a goroutine so we can also select on ctx/stop.
		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()

		var exitErr error
		select {
		case <-ctx.Done():
			assumeTimer.Stop()
			s.forwardSignal(cmd, syscall.SIGTERM)
			<-waitCh
			stopHealth()
			s.setStatus(StatusStopped)
			return ctx.Err()

		case <-s.stopCh:
			assumeTimer.Stop()
			s.terminateChild(cmd, waitCh)
			stopHealth()
			s.setStatus(StatusStopped)
			return nil

		case exitErr = <-waitCh:
			assumeTimer.Stop()
			stopHealth()
		}

		exitCode := exitCodeFrom(exitErr)
		s.log.Info("supervise: child exited", "pid", pid, "code", exitCode)

		cur := s.state.Load()
		next := *cur
		next.Status = StatusStopped
		next.LastExitCode = exitCode
		s.state.Store(&next)

		// Policy evaluation.
		if s.spec.Policy == PolicyNever {
			return nil
		}
		// PolicyOnFailure: only restart on non-zero exit.
		if exitCode == 0 {
			s.log.Info("supervise: clean exit with on-failure policy — not restarting")
			return nil
		}

		// Restart cap / crash-loop guard.
		now := time.Now()
		restartTimes = append(restartTimes, now)

		if s.spec.Backoff.MaxRestarts >= 0 && len(restartTimes) > s.spec.Backoff.MaxRestarts {
			s.setCrashLoopState(len(restartTimes), exitCode)
			s.log.Error("supervise: crash-loop detected — restart cap reached",
				"max_restarts", s.spec.Backoff.MaxRestarts)
			return fmt.Errorf("supervise: crash-loop: exceeded %d restarts", s.spec.Backoff.MaxRestarts)
		}

		// Sliding-window crash-loop check: if MaxRestarts restarts all happened
		// within CrashLoopWindow, declare crash-loop.
		if s.spec.Backoff.MaxRestarts > 0 && len(restartTimes) >= s.spec.Backoff.MaxRestarts {
			windowStart := now.Add(-s.spec.CrashLoopWindow)
			count := 0
			for _, t := range restartTimes {
				if t.After(windowStart) {
					count++
				}
			}
			if count >= s.spec.Backoff.MaxRestarts {
				s.setCrashLoopState(len(restartTimes), exitCode)
				s.log.Error("supervise: crash-loop detected — sliding window exceeded",
					"restarts_in_window", count,
					"window", s.spec.CrashLoopWindow)
				return fmt.Errorf("supervise: crash-loop: %d restarts in %s",
					count, s.spec.CrashLoopWindow)
			}
		}

		// Backoff with jitter before respawn.
		delay := backoffWithJitter(backoff, s.spec.Backoff.Jitter)
		s.log.Info("supervise: backoff before restart", "delay", delay, "restart", len(restartTimes))

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			s.setStatus(StatusStopped)
			return ctx.Err()
		case <-s.stopCh:
			s.setStatus(StatusStopped)
			return nil
		}

		// Advance backoff for next iteration (double, cap).
		backoff *= 2
		if backoff > s.spec.Backoff.Cap {
			backoff = s.spec.Backoff.Cap
		}
	}
}

// buildCmd constructs an exec.Cmd from the spec. stdout/stderr are inherited
// from the shim so the tmux pane absorbs them (PL-028d).
func (s *Supervisor) buildCmd() *exec.Cmd {
	//nolint:gosec // command comes from operator-controlled config
	cmd := exec.Command(s.spec.Command[0], s.spec.Command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(s.spec.Env) > 0 {
		cmd.Env = append(os.Environ(), s.spec.Env...)
	}
	if s.spec.WorkDir != "" {
		cmd.Dir = s.spec.WorkDir
	}
	// Place child in its own process group so SIGTERM can be forwarded cleanly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// terminateChild implements PL-011: SIGTERM → bounded wait → SIGKILL. The
// bounded wait honours the timeout passed to Stop() (recorded in
// stopTimeoutNanos); a zero value falls back to Spec.StopTimeout.
func (s *Supervisor) terminateChild(cmd *exec.Cmd, waitCh <-chan error) {
	killTimeout := time.Duration(s.stopTimeoutNanos.Load())
	if killTimeout <= 0 {
		killTimeout = s.spec.StopTimeout
	}
	s.forwardSignal(cmd, syscall.SIGTERM)
	timer := time.NewTimer(killTimeout)
	defer timer.Stop()
	select {
	case <-waitCh:
	case <-timer.C:
		s.log.Warn("supervise: SIGTERM timeout — sending SIGKILL", "timeout", killTimeout)
		s.forwardSignal(cmd, syscall.SIGKILL)
		<-waitCh
	}
}

func (s *Supervisor) forwardSignal(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(sig)
}

// runHealthProbe periodically checks process liveness (kill(pid,0)) and
// heartbeat-file freshness. Health failures update state.Status but do NOT
// trigger a restart — only process exit does (matches TS behaviour).
func (s *Supervisor) runHealthProbe(pid int, done <-chan struct{}) {
	ticker := time.NewTicker(s.spec.HealthProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			healthy := s.checkHealth(pid)
			cur := s.state.Load()
			if cur.Status == StatusRunning || cur.Status == StatusUnhealthy {
				next := *cur
				if healthy {
					next.Status = StatusRunning
				} else {
					next.Status = StatusUnhealthy
				}
				s.state.Store(&next)
				if !healthy {
					s.log.Warn("supervise: health probe failed", "pid", pid)
				}
			}
		}
	}
}

func (s *Supervisor) checkHealth(pid int) bool {
	// Process liveness via kill(pid, 0).
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	// Heartbeat-file freshness.
	if s.spec.HeartbeatPath != "" {
		info, err := os.Stat(s.spec.HeartbeatPath)
		if err != nil {
			return false
		}
		if time.Since(info.ModTime()) > s.spec.HeartbeatTTL {
			return false
		}
	}
	return true
}

func (s *Supervisor) setState(st State) {
	s.state.Store(&st)
}

func (s *Supervisor) setStatus(status Status) {
	cur := s.state.Load()
	next := *cur
	next.Status = status
	s.state.Store(&next)
}

func (s *Supervisor) setCrashLoopState(restartCount, exitCode int) {
	cur := s.state.Load()
	next := *cur
	next.Status = StatusCrashLoop
	next.RestartCount = restartCount
	next.LastExitCode = exitCode
	s.state.Store(&next)
}

// exitCodeFrom extracts the integer exit code from cmd.Wait()'s error.
func exitCodeFrom(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := isExitError(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func isExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// backoffWithJitter adds uniform random jitter of ±(jitter*d)/2.
func backoffWithJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	//nolint:gosec // non-cryptographic jitter
	delta := float64(d) * jitter
	return d + time.Duration((rand.Float64()-0.5)*delta)
}
