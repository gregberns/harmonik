package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workspace"
)

var splashDismissDelayNs atomic.Int64

func init() { splashDismissDelayNs.Store(int64(750 * time.Millisecond)) }

func splashDismissDelayDur() time.Duration {
	return time.Duration(splashDismissDelayNs.Load())
}

var resumeSubmitRetries = 2

var resumeSubmitRetryDelayNs atomic.Int64

func init() { resumeSubmitRetryDelayNs.Store(int64(400 * time.Millisecond)) }

func resumeSubmitRetryDelayDur() time.Duration {
	return time.Duration(resumeSubmitRetryDelayNs.Load())
}

var (
	pasteVerifyAttempts   = 3
	pasteVerifyScrollback = 200
)

var pasteVerifyBackoffNs atomic.Int64

func init() { pasteVerifyBackoffNs.Store(int64(1500 * time.Millisecond)) }

func pasteVerifyBackoffDur() time.Duration {
	return time.Duration(pasteVerifyBackoffNs.Load())
}

type enterSender interface {
	// SendEnterToLastPane sends a bare "Enter" key to the most recently
	// spawned window's first pane.  Returns a non-nil error if no window
	// has been spawned yet or if the underlying send-keys call fails.
	SendEnterToLastPane(ctx context.Context) error
}

type paneCapturer interface {
	// CaptureLastPane returns the rendered pane text plus scrollback lines of
	// history tail for the most recently spawned window's first pane.  Returns a
	// non-nil error if no window has been spawned yet or the capture fails.
	// Returns an error wrapping errPaneCaptureUnsupported when the underlying
	// adapter cannot capture panes at all (a minimal/test substrate).
	CaptureLastPane(ctx context.Context, scrollback int) (string, error)
}

var errPaneCaptureUnsupported = errors.New("daemon: pane capture unsupported by adapter")

type quitSender interface {
	// SendQuitToLastPane sends `/quit` followed by Enter to the most recently
	// spawned window's first pane.  Returns a non-nil error if no window has
	// been spawned yet or if the underlying send-keys call fails.
	SendQuitToLastPane(ctx context.Context) error
}

type paneOutputSizer interface {
	// PaneOutputFingerprint returns a string that changes as the pane
	// produces visible output (history size grows, cursor advances).
	// Returns ("", false) on any error (conservative: treat unknown as
	// no growth — the ceiling kill is allowed to proceed).
	PaneOutputFingerprint(ctx context.Context) (string, bool)
}

type paneLivenessChecker interface {
	// PaneHasActiveProcess returns true when the tmux pane shell has at least
	// one child process (i.e. the hosted claude process is still running).
	// Returns false on any error (conservative: treat unknown as dead).
	PaneHasActiveProcess(ctx context.Context) bool
}

var livePaneCommandSubstrings = []string{"claude", "node"}

func agentCommandFragmentsFor(binary string) []string {
	if binary == "" {
		return livePaneCommandSubstrings
	}
	base := filepath.Base(binary)
	if base == "claude" {
		return livePaneCommandSubstrings
	}
	return []string{base}
}

func hasChildProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	if hasAnyDirectChild(pid) {
		return true
	}
	return commandMatchesLiveAgent(pid, livePaneCommandSubstrings)
}

func hasAnyDirectChild(pid int) bool {
	return exec.Command("pgrep", "-P", fmt.Sprintf("%d", pid)).Run() == nil
}

func hasAnyDirectChildVia(ctx context.Context, runner tmux.CommandRunner, pid int) bool {
	return runner.Command(ctx, "pgrep", "-P", fmt.Sprintf("%d", pid)).Run() == nil
}

func commandMatchesLiveAgent(pid int, fragments []string) bool {
	out, err := exec.Command("ps", "-o", "comm=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false
	}
	comm := strings.ToLower(strings.TrimSpace(string(out)))
	if comm == "" {
		return false
	}
	for _, frag := range fragments {
		if strings.Contains(comm, frag) {
			return true
		}
	}
	return false
}

func commandMatchesLiveAgentVia(ctx context.Context, runner tmux.CommandRunner, pid int, fragments []string) bool {
	out, err := runner.Command(ctx, "ps", "-o", "comm=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false
	}
	comm := strings.ToLower(strings.TrimSpace(string(out)))
	if comm == "" {
		return false
	}
	for _, frag := range fragments {
		if strings.Contains(comm, frag) {
			return true
		}
	}
	return false
}

func hasAnyDirectChildOrSSHFail(ctx context.Context, runner tmux.CommandRunner, pid int) (alive bool, connFailed bool) {
	err := runner.Command(ctx, "pgrep", "-P", fmt.Sprintf("%d", pid)).Run()
	if err == nil {
		return true, false
	}
	return false, tmux.IsSSHConnectionFailure(err)
}

func commandMatchesLiveAgentOrSSHFail(ctx context.Context, runner tmux.CommandRunner, pid int, fragments []string) (alive bool, connFailed bool) {
	out, err := runner.Command(ctx, "ps", "-o", "comm=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false, tmux.IsSSHConnectionFailure(err)
	}
	comm := strings.ToLower(strings.TrimSpace(string(out)))
	if comm == "" {
		return false, false
	}
	for _, frag := range fragments {
		if strings.Contains(comm, frag) {
			return true, false
		}
	}
	return false, false
}

func probeLivenessOrSSHFail(ctx context.Context, runner tmux.CommandRunner, pid int, fragments []string) (alive bool, connFailed bool) {
	alive1, cf1 := hasAnyDirectChildOrSSHFail(ctx, runner, pid)
	if cf1 {
		return false, true
	}
	if alive1 {
		return true, false
	}
	return commandMatchesLiveAgentOrSSHFail(ctx, runner, pid, fragments)
}

type commandRunnerProvider interface {
	commandRunner() tmux.CommandRunner
}

func worktreeActivityFingerprintVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, bool) {
	head, err := gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
	if err != nil {
		return "", false
	}
	out, err := runner.Command(ctx, "git", "-C", wtPath, "status", "--porcelain=v1").Output()
	if err != nil {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteByte(0)
	sb.Write(out)
	if gitprobe.RunnerIsLocalFS(runner) {
		for _, line := range strings.Split(string(out), "\n") {
			if len(line) < 4 {
				continue
			}
			path := line[3:]
			if strings.Contains(path, " -> ") || strings.HasPrefix(path, "\"") {
				continue
			}
			if fi, statErr := os.Stat(filepath.Join(wtPath, path)); statErr == nil {
				fmt.Fprintf(&sb, "\x00%s:%d:%d", path, fi.Size(), fi.ModTime().UnixNano())
			}
		}
	}
	return sb.String(), true
}

var briefDeliveredTimeout = 2 * time.Minute

var commitPollInterval = 500 * time.Millisecond

var commitPollTimeout = 30 * time.Minute

var commitHardCeiling = 90 * time.Minute

var heartbeatStalenessThreshold = 8 * time.Minute

var launchHeartbeatTimeout = 180 * time.Second

var launchSuppressionCeiling = 12 * time.Minute

var noChangeKillDelay = 30 * time.Second

var implementerReseedGrace = 75 * time.Second

var postQuitKillGrace = 60 * time.Second

type sessionKiller interface {
	Kill(ctx context.Context) error
}

func pasteInjectQuitOnCommit(
	ctx context.Context,
	clk substrate.ClockPort,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) {
	if clk == nil {
		clk = substrate.SystemClock{}
	}

	briefDeliveredFired := false
	if briefDelivered != nil {
		bdTimeout := briefDeliveredTimeout // snapshot before blocking
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
			briefDeliveredFired = true
		case <-substrate.After(clk, bdTimeout): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-commit: brief_delivered timeout after %v for %s; proceeding with commit poll (session may be broken)\n",
				bdTimeout, wtPath)
		}
	}

	if briefDeliveredFired && eventCh != nil {
		if lc, ok := qs.(paneLivenessChecker); ok && !lc.PaneHasActiveProcess(ctx) {
			stalePaneKillDelay := noChangeKillDelay // snapshot before the main snapshot block
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-commit: stale-pane: pane dead at brief delivery in %s; "+
					"firing noChange immediately to reopen bead for retry (hk-1too)\n", wtPath)
			if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: stale-pane: SendQuitToLastPane: %v\n", qErr)
			}
			select {
			case <-ctx.Done():
				return
			case <-substrate.After(clk, stalePaneKillDelay): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
			}
			if killer != nil {
				if kErr := killer.Kill(ctx); kErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: stale-pane: Kill: %v\n", kErr)
				}
			}
			if noChangeTimeoutCh != nil {
				close(noChangeTimeoutCh)
			}
			return
		}
	}

	pollTimeout := commitPollTimeout
	pollInterval := commitPollInterval
	killDelay := noChangeKillDelay
	stalenessThreshold := heartbeatStalenessThreshold
	launchWindow := launchHeartbeatTimeout
	hardCeiling := commitHardCeiling
	launchSuppressCeil := launchSuppressionCeiling
	reseedGrace := implementerReseedGrace

	loopStart := clk.Now()
	totalDeadline := loopStart.Add(pollTimeout)
	hardDeadline := loopStart.Add(hardCeiling)
	lastHeartbeat := clk.Now() // initialised to now; first real beat resets it
	lastProgress := loopStart
	heartbeatProvided := eventCh != nil
	launchDeadline := clk.Now().Add(launchWindow)
	launchSuppressDeadline := loopStart.Add(launchSuppressCeil)
	firstHeartbeatSeen := false
	var lastLaunchSuppressLog time.Time
	var lastStalenessLog time.Time

	probeRunner := tmux.CommandRunner(tmux.LocalRunner{})
	if crp, ok := qs.(commandRunnerProvider); ok {
		probeRunner = crp.commandRunner()
	}

	lastActivityFingerprint, _ := worktreeActivityFingerprintVia(ctx, probeRunner, wtPath)

	var outputSizer paneOutputSizer
	if sizer, ok := qs.(paneOutputSizer); ok {
		outputSizer = sizer
	}
	lastPaneOutputFP := ""
	if outputSizer != nil {
		lastPaneOutputFP, _ = outputSizer.PaneOutputFingerprint(ctx)
	}

	var livenessChecker paneLivenessChecker
	if lc, ok := qs.(paneLivenessChecker); ok {
		livenessChecker = lc
	}

	var reseedES enterSender
	if es, ok := qs.(enterSender); ok {
		reseedES = es
	}
	reseedEnterDeadline := loopStart.Add(reseedGrace)
	reseedEnterFired := reseedES == nil

	ticker := clk.NewTicker(pollInterval)
	defer ticker.Stop()

	fireNoChangePath := func(reason, reasonTag string, budgetExceeded bool) {
		fmt.Fprintf(os.Stderr,
			"daemon: pasteinject: quit-on-commit: %s in %s (initial=%s); sending /quit unconditionally\n",
			reason, wtPath, initialSHA)
		if budgetExceeded {
			now := clk.Now()
			emitImplementerBudgetExceeded(ctx, bus, runID,
				now.Sub(loopStart), now.Sub(lastProgress), reasonTag)
		}
		if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-commit: noChange SendQuitToLastPane: %v\n", qErr)
		}
		select {
		case <-ctx.Done():
			return
		case <-substrate.After(clk, killDelay): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
		}
		if killer != nil {
			if kErr := killer.Kill(ctx); kErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: noChange Kill: %v\n", kErr)
			}
		}
		if noChangeTimeoutCh != nil {
			close(noChangeTimeoutCh)
		}
	}

	noteHeartbeat := func(at time.Time) {
		lastHeartbeat = at
		firstHeartbeatSeen = true
	}

	for {
		select {
		case <-ctx.Done():
			return

		case env, ok := <-eventCh:
			if !ok {
				eventCh = nil
				continue
			}
			if env.Type == core.EventTypeAgentHeartbeat {
				noteHeartbeat(clk.Now())
			}

		case <-ticker.C():
			now := clk.Now()

			if eventCh != nil {
			drainHeartbeats:
				for {
					select {
					case env, ok := <-eventCh:
						if !ok {
							eventCh = nil
							break drainHeartbeats
						}
						if env.Type == core.EventTypeAgentHeartbeat {
							noteHeartbeat(clk.Now())
						}
					default:
						break drainHeartbeats
					}
				}
			}

			if now.After(hardDeadline) {
				fireNoChangePath(
					fmt.Sprintf("hard-ceiling %v reached without a new commit", hardCeiling),
					"hard-ceiling", true)
				return
			}

			if now.After(totalDeadline) {
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					budgetPaneOutputProgressed := false
					if outputSizer != nil {
						if fp, ok := outputSizer.PaneOutputFingerprint(ctx); ok && fp != lastPaneOutputFP {
							budgetPaneOutputProgressed = true
							lastPaneOutputFP = fp
						}
					}

					budgetWorktreeProgressed := false
					if !budgetPaneOutputProgressed && outputSizer != nil {
						wtFP, wtOK := worktreeActivityFingerprintVia(ctx, probeRunner, wtPath)
						if wtOK && wtFP != lastActivityFingerprint {
							budgetWorktreeProgressed = true
							lastActivityFingerprint = wtFP
						}
					}

					if budgetWorktreeProgressed || budgetPaneOutputProgressed {
						signal := "pane output growth"
						if budgetWorktreeProgressed {
							signal = "changed working tree"
						}
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-commit: commit-budget %v elapsed but pane is making progress (%s) in %s; extending budget (hard ceiling %v)\n",
							pollTimeout, signal, wtPath, hardCeiling)
						lastProgress = now
						totalDeadline = now.Add(pollTimeout)
					} else {
						fireNoChangePath("total-timeout waiting for new commit (pane active but no observable progress)",
							"total-budget-stale-active", true)
						return
					}
				} else {
					fireNoChangePath("total-timeout waiting for new commit (pane inactive)",
						"total-budget-stale", true)
					return
				}
			}

			if heartbeatProvided && !firstHeartbeatSeen && now.After(launchDeadline) {
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					wtFP, wtOK := worktreeActivityFingerprintVia(ctx, probeRunner, wtPath)
					worktreeProgressed := wtOK && wtFP != lastActivityFingerprint

					paneOutputProgressed := false
					if outputSizer != nil {
						if fp, ok := outputSizer.PaneOutputFingerprint(ctx); ok && fp != lastPaneOutputFP {
							paneOutputProgressed = true
							lastPaneOutputFP = fp
						}
					}

					if worktreeProgressed || paneOutputProgressed {
						signal := "changed working tree"
						if paneOutputProgressed && !worktreeProgressed {
							signal = "pane output growth"
						} else if paneOutputProgressed {
							signal = "changed working tree + pane output growth"
						}
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-commit: launch-heartbeat-timeout: worktree progressing (active pane + %s) in %s; treating as heartbeat, deferring to commit budget (hard ceiling %v)\n",
							signal, wtPath, hardCeiling)
						if worktreeProgressed {
							lastActivityFingerprint = wtFP
						}
						firstHeartbeatSeen = true
						lastHeartbeat = now
						lastProgress = now
						totalDeadline = now.Add(pollTimeout)
						continue
					}
				}

				if now.Before(launchSuppressDeadline) &&
					livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					_ = lastLaunchSuppressLog
					lastHeartbeat = now
					launchDeadline = now.Add(launchWindow)
				} else {
					reason := "launch-heartbeat-timeout: no heartbeat within launch window after brief delivery"
					if !now.Before(launchSuppressDeadline) {
						reason = fmt.Sprintf(
							"launch-heartbeat-timeout: no heartbeat within launch-suppression ceiling %v (pane active but never progressed)",
							launchSuppressCeil)
					}
					fireNoChangePath(reason, "launch-heartbeat-timeout", false)
					return
				}
			}

			if heartbeatProvided && now.Sub(lastHeartbeat) > stalenessThreshold {
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					_ = lastStalenessLog
					lastHeartbeat = now
				} else {
					fireNoChangePath(fmt.Sprintf(
						"heartbeat stale for >%v (no agent_heartbeat received)",
						stalenessThreshold,
					), "heartbeat-stale", true)
					return
				}
			}

			if !reseedEnterFired && now.After(reseedEnterDeadline) {
				reseedEnterFired = true
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: reseed-enter: %v elapsed in %s; sending Enter to submit any pending input\n",
					reseedGrace, wtPath)
				if reseedErr := reseedES.SendEnterToLastPane(ctx); reseedErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: reseed-enter: SendEnterToLastPane: %v\n", reseedErr)
				}
			}

			headSHA, err := gitprobe.ResolveWorktreeHEADVia(ctx, probeRunner, wtPath)
			if err != nil {
				continue
			}
			if headSHA != initialSHA {
				if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: SendQuitToLastPane: %v\n", qErr)
				}
				if killer != nil {
					grace := postQuitKillGrace
					go func() {
						<-substrate.After(clk, grace)
						if kErr := killer.Kill(context.Background()); kErr != nil {
							fmt.Fprintf(os.Stderr,
								"daemon: pasteinject: quit-on-commit: post-quit Kill: %v\n", kErr)
						}
					}()
				}
				return
			}
		}
	}
}

func pasteInjectOnLaunch(
	ctx context.Context,
	clk substrate.ClockPort,
	subst handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) <-chan struct{} {
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		if subst == nil {
			return
		}
		inj, ok := subst.(pasteInjecter)
		if !ok {
			return
		}

		var runner tmux.CommandRunner
		if crp, ok2 := subst.(commandRunnerProvider); ok2 {
			if r := crp.commandRunner(); !gitprobe.RunnerIsLocalFS(r) {
				runner = r
			}
		}

		var failReason string
		switch phase {
		case handlercontract.ReviewLoopPhaseReviewer:
			failReason = pasteInjectReviewer(ctx, clk, inj, claudeSessID, wtPath, runner)

		case handlercontract.ReviewLoopPhaseImplementerResume:
			failReason = pasteInjectImplementerResume(ctx, clk, inj, claudeSessID, iterCount, wtPath, runner)

		default:
			failReason = pasteInjectImplementerInitial(ctx, clk, inj, claudeSessID, wtPath, runner)
		}

		if failReason != "" && bus != nil {
			emitPasteInjectFailed(ctx, bus, runID, string(phase), failReason)
		}
	}()
	return ch
}

func emitPasteInjectFailed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, phase, reason string) {
	pl := core.PasteInjectFailedPayload{
		RunID:  runID.String(),
		Phase:  phase,
		Reason: reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: emitPasteInjectFailed: marshal: %v\n", err)
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypePasteInjectFailed, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: emit paste_inject_failed: %v\n", emitErr)
	}
}

func emitImplementerBudgetExceeded(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, elapsed, sinceProgress time.Duration, reason string) {
	if bus == nil {
		return
	}
	elapsedMS := elapsed.Milliseconds()
	if elapsedMS <= 0 {
		elapsedMS = 1
	}
	sinceMS := sinceProgress.Milliseconds()
	if sinceMS < 0 {
		sinceMS = 0
	}
	if reason == "" {
		reason = "budget-exceeded"
	}
	pl := core.ImplementerBudgetExceededPayload{
		RunID:               runID.String(),
		ElapsedMS:           elapsedMS,
		SinceLastProgressMS: sinceMS,
		Reason:              reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: emitImplementerBudgetExceeded: marshal: %v\n", err)
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerBudgetExceeded, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: emit implementer_budget_exceeded: %v\n", emitErr)
	}
}

func splashDismissWait(ctx context.Context, clk substrate.ClockPort) {
	select {
	case <-ctx.Done():
	case <-substrate.After(clk, splashDismissDelayDur()): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
	}
}

func pasteInjectImplementerInitial(ctx context.Context, clk substrate.ClockPort, inj pasteInjecter, claudeSessID, wtPath string, runner tmux.CommandRunner) string {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFileVia(ctx, runner, taskFile); err != nil {
		reason := fmt.Sprintf("implementer-initial: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx, clk)
	}

	bufName := bufferName(claudeSessID, "task")
	msg := "Please read .harmonik/agent-task.md and begin.\n"
	if reason := injectAndVerifySeed(ctx, clk, inj, bufName, []byte(msg), "agent-task.md", "implementer-initial"); reason != "" {
		return reason
	}
	splashDismissWait(ctx, clk)
	if es, ok := inj.(enterSender); ok {
		sendSubmitEnterWithRetry(ctx, clk, es, "implementer-initial")
	}
	return ""
}

func pasteInjectImplementerResume(ctx context.Context, clk substrate.ClockPort, inj pasteInjecter, claudeSessID string, iterCount int, wtPath string, runner tmux.CommandRunner) string {
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx, clk)
	}

	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFileVia(ctx, runner, taskFile); err != nil {
		reason := fmt.Sprintf("implementer-resume task: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	priorIter := iterCount - 1
	feedbackFile := filepath.Join(wtPath, ".harmonik", fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIter))
	feedbackExists := statTaskFileVia(ctx, runner, feedbackFile) == nil

	var msg string
	if feedbackExists {
		msg = fmt.Sprintf(
			"Please read .harmonik/agent-task.md and begin.\n\n"+
				"Before continuing, also read .harmonik/reviewer-feedback.iter-%d.md in your worktree."+
				" It says what routed iteration %d back to you, and its first line says who produced it —"+
				" a reviewer, or the daemon because the commit gate went red or no commit landed."+
				" Address every point it raises before proceeding.\n",
			priorIter, priorIter,
		)
	} else {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume feedback iter %d: not found (delivering task-only message)\n", priorIter)
		msg = "Please read .harmonik/agent-task.md and begin.\n"
	}

	bufName := bufferName(claudeSessID, "task")
	if reason := injectAndVerifySeed(ctx, clk, inj, bufName, []byte(msg), "agent-task.md", "implementer-resume"); reason != "" {
		return reason
	}
	splashDismissWait(ctx, clk)
	if es, ok := inj.(enterSender); ok {
		sendResumeSubmitEnter(ctx, clk, es)
	}
	return ""
}

func submitSeedInput(ctx context.Context, inj pasteInjecter, bufName string, payload []byte) (handler.Ack, error) {
	if ip, ok := inj.(handler.InputPort); ok {
		return ip.SubmitInput(ctx, handler.InputRequest{Payload: payload})
	}
	return handler.Ack{}, inj.WriteLastPane(ctx, bufName, payload)
}

func injectAndVerifySeed(ctx context.Context, clk substrate.ClockPort, inj pasteInjecter, bufName string, payload []byte, marker, phase string) string {
	pc, canCapture := inj.(paneCapturer)
	var lastErr error
	captureEverSucceeded := false
	for attempt := 1; attempt <= pasteVerifyAttempts; attempt++ {
		if ack, err := submitSeedInput(ctx, inj, bufName, payload); err != nil {
			reason := fmt.Sprintf("%s SubmitInput: %v", phase, err)
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
			return reason
		} else if attempt == 1 {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s delivered via SubmitInput (ack=%s)\n", phase, ack.Outcome)
		}
		if !canCapture {
			return ""
		}
		pane, capErr := pc.CaptureLastPane(ctx, pasteVerifyScrollback)
		if errors.Is(capErr, errPaneCaptureUnsupported) {
			return ""
		}
		switch {
		case capErr != nil:
			lastErr = capErr
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s CaptureLastPane attempt %d/%d: %v\n", phase, attempt, pasteVerifyAttempts, capErr)
		case strings.Contains(pane, marker):
			if attempt > 1 {
				fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s seed landed on attempt %d/%d\n", phase, attempt, pasteVerifyAttempts)
			}
			return ""
		default:
			captureEverSucceeded = true
			lastErr = fmt.Errorf("seed marker %q absent from pane", marker)
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s seed marker %q not yet in pane (attempt %d/%d)\n", phase, marker, attempt, pasteVerifyAttempts)
		}
		if attempt < pasteVerifyAttempts {
			select {
			case <-ctx.Done():
				return fmt.Sprintf("%s: ctx cancelled during paste-verify: %v", phase, ctx.Err())
			case <-substrate.After(clk, pasteVerifyBackoffDur()): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
			}
		}
	}
	if canCapture && !captureEverSucceeded {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s pane capture failed on all %d attempts but every paste write succeeded; trusting the write: %v\n", phase, pasteVerifyAttempts, lastErr)
		return ""
	}
	return fmt.Sprintf("%s: seed paste unverified after %d attempts: %v", phase, pasteVerifyAttempts, lastErr)
}

func sendSubmitEnterWithRetry(ctx context.Context, clk substrate.ClockPort, es enterSender, phase string) {
	if err := es.SendEnterToLastPane(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s post-paste SendEnterToLastPane: %v\n", phase, err)
	}
	for i := 0; i < resumeSubmitRetries; i++ {
		select {
		case <-ctx.Done():
			return
		case <-substrate.After(clk, resumeSubmitRetryDelayDur()): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
		}
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s submit-retry %d SendEnterToLastPane: %v\n", phase, i+1, err)
		}
	}
}

func sendResumeSubmitEnter(ctx context.Context, clk substrate.ClockPort, es enterSender) {
	sendSubmitEnterWithRetry(ctx, clk, es, "implementer-resume")
}

const reviewerSeedMaxLen = 300

const reviewerKickoffSeed = "Read .harmonik/review-target.md in this worktree" +
	" and produce your verdict exactly as instructed there.\n"

func pasteInjectReviewer(ctx context.Context, clk substrate.ClockPort, inj pasteInjecter, claudeSessID, wtPath string, runner tmux.CommandRunner) string {
	reviewFile := filepath.Join(wtPath, ".harmonik", "review-target.md")
	if err := statTaskFileVia(ctx, runner, reviewFile); err != nil {
		reason := fmt.Sprintf("reviewer: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx, clk)
	}

	bufName := bufferName(claudeSessID, "review")
	msg := reviewerKickoffSeed
	if reason := injectAndVerifySeed(ctx, clk, inj, bufName, []byte(msg), "review-target.md", "reviewer"); reason != "" {
		return reason
	}
	if es, ok := inj.(enterSender); ok {
		sendSubmitEnterWithRetry(ctx, clk, es, "reviewer")
	}
	return ""
}

var reviewFileTimeout = 10 * time.Minute

var reviewFileHardCeiling = 60 * time.Minute

var reviewerHeartbeatActiveGrace = 10 * time.Minute

var reviewFilePerKLineBudget = 10 * time.Minute

var reviewFilePollInterval = 2 * time.Second

var reviewerReseedGrace = 75 * time.Second

func reviewBudgetForDiff(changedLines int, base, perKLine, ceiling time.Duration) time.Duration {
	if changedLines <= 0 {
		if base > ceiling {
			return ceiling
		}
		return base
	}
	extra := time.Duration(int64(perKLine) * int64(changedLines) / 1000)
	budget := base + extra
	if budget > ceiling {
		return ceiling
	}
	if budget < base {
		return base
	}
	return budget
}

func worktreeDiffLineCount(ctx context.Context, wtPath string) int {
	for _, ref := range []string{"origin/HEAD", "origin/main"} {
		cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", ref+"...HEAD")
		cmd.Dir = wtPath
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		total, ok := sumNumstatLines(string(out))
		if ok {
			return total
		}
	}
	return -1
}

func sumNumstatLines(numstat string) (int, bool) {
	total := 0
	sawRow := false
	for _, line := range strings.Split(numstat, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		added, aErr := strconv.Atoi(fields[0])
		deleted, dErr := strconv.Atoi(fields[1])
		if aErr != nil || dErr != nil {
			sawRow = true
			continue
		}
		total += added + deleted
		sawRow = true
	}
	if !sawRow {
		return 0, true
	}
	return total, true
}

func worktreeActivityFingerprint(ctx context.Context, wtPath string) (string, bool) {
	head, err := gitprobe.ResolveWorktreeHEAD(ctx, wtPath)
	if err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteByte(0)
	sb.Write(out)
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if strings.Contains(path, " -> ") || strings.HasPrefix(path, "\"") {
			continue
		}
		if fi, statErr := os.Stat(filepath.Join(wtPath, path)); statErr == nil {
			fmt.Fprintf(&sb, "\x00%s:%d:%d", path, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return sb.String(), true
}

const reviewerBudgetSentinelName = "reviewer-budget-exceeded.json"

type reviewerBudgetSentinel struct {
	BudgetMS     int64  `json:"budget_ms"`
	ChangedLines int    `json:"changed_lines"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	Reason       string `json:"reason"`
}

func reviewerBudgetSentinelPath(wtPath string) string {
	return filepath.Join(wtPath, ".harmonik", reviewerBudgetSentinelName)
}

func writeReviewerBudgetSentinel(wtPath string, budget time.Duration, changedLines int, elapsed time.Duration, reason string) {
	pl := reviewerBudgetSentinel{
		BudgetMS:     budget.Milliseconds(),
		ChangedLines: changedLines,
		ElapsedMS:    elapsed.Milliseconds(),
		Reason:       reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: writeReviewerBudgetSentinel: marshal: %v\n", err)
		return
	}
	path := reviewerBudgetSentinelPath(wtPath)
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: writeReviewerBudgetSentinel: mkdir: %v\n", mkErr)
		return
	}
	if wErr := os.WriteFile(path, b, 0o644); wErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: writeReviewerBudgetSentinel: write %s: %v\n", path, wErr)
	}
}

// ReadReviewerBudgetSentinel reads the budget-kill marker (if present) for the
// worktree at wtPath.  Returns (nil, nil) when the marker is absent — the normal
// case for a successful or true-no-verdict review.  Callers use a non-nil result
// to emit a distinct "reviewer budget exceeded" diagnostic in place of the
// generic "verdict absent at iteration N".
//
// Exported (capitalized) so the
// DOT reviewer-node path (dot_cascade.go) can consult it without changing the
// pasteInjectQuitOnReviewFile signature.
//
// Bead: hk-sah87.
func ReadReviewerBudgetSentinel(wtPath string) (*reviewerBudgetSentinel, error) {
	path := reviewerBudgetSentinelPath(wtPath)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil //nolint:nilnil // absent marker = normal case (successful/true-no-verdict review)
		}
		return nil, fmt.Errorf("daemon: ReadReviewerBudgetSentinel: read %s: %w", path, err)
	}
	var pl reviewerBudgetSentinel
	if uErr := json.Unmarshal(b, &pl); uErr != nil {
		return nil, fmt.Errorf("daemon: ReadReviewerBudgetSentinel: unmarshal %s: %w", path, uErr)
	}
	return &pl, nil
}

// ReadReviewerBudgetSentinelVia is like ReadReviewerBudgetSentinel but routes the
// marker-file read through runner (e.g. an SSHRunner for a remote-substrate worker
// whose worktree lives on a separate filesystem). For a remote run the marker is
// written into the reviewer's worktree ON THE WORKER, so a box-A os.ReadFile never
// finds it → the daemon can't distinguish a budget-kill from a true no-verdict.
// Routing through the runner (cat the file over the transport) reads the
// worker-side marker and applies the identical unmarshal.
//
// Callers pass nil to use ReadReviewerBudgetSentinel's byte-identical bare-local
// path (NFR7); a local-FS runner (tmux.LocalRunner) is also treated as local.
//
// Bead: hk-f3u6o.
func ReadReviewerBudgetSentinelVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*reviewerBudgetSentinel, error) {
	if runner == nil || gitprobe.RunnerIsLocalFS(runner) {
		return ReadReviewerBudgetSentinel(wtPath)
	}
	path := reviewerBudgetSentinelPath(wtPath)
	out, err := runner.Command(ctx, "cat", path).Output()
	if err != nil {
		if tmux.IsSSHConnectionFailure(err) {
			return nil, fmt.Errorf("%w: cat %s: %w", workspace.ErrRemoteTransport, path, err)
		}
		// A non-transport cat failure means the marker is absent, mirroring
		// ReadReviewerBudgetSentinel's os.ErrNotExist branch (nil,nil).
		//nolint:nilnil // absent marker is the normal no-budget-kill case
		return nil, nil
	}
	var pl reviewerBudgetSentinel
	if uErr := json.Unmarshal(out, &pl); uErr != nil {
		return nil, fmt.Errorf("daemon: ReadReviewerBudgetSentinelVia: unmarshal %s: %w", path, uErr)
	}
	return &pl, nil
}

func pasteInjectQuitOnReviewFile(
	ctx context.Context,
	clk substrate.ClockPort,
	qs quitSender,
	killer sessionKiller,
	inj pasteInjecter,
	claudeSessID string,
	wtPath string,
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope, // hk-60t8: heartbeat tracking; nil = disabled
	overrideCeiling time.Duration, // hk-60t8: 0 = use reviewFileHardCeiling
) {
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	if briefDelivered != nil {
		bdTimeout := briefDeliveredTimeout
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
		case <-substrate.After(clk, bdTimeout): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-review-file: brief_delivered timeout after %v for %s; proceeding\n",
				bdTimeout, wtPath)
		}
	}

	verdictPath := filepath.Join(wtPath, ".harmonik", "review.json")
	pollInterval := reviewFilePollInterval
	killDelay := noChangeKillDelay

	effectiveCeiling := reviewFileHardCeiling
	if overrideCeiling > 0 {
		effectiveCeiling = overrideCeiling
	}

	changedLines := worktreeDiffLineCount(ctx, wtPath)
	budget := reviewBudgetForDiff(changedLines, reviewFileTimeout, reviewFilePerKLineBudget, effectiveCeiling)
	fmt.Fprintf(os.Stderr,
		"daemon: pasteinject: quit-on-review-file: verdict budget %v for %s (changed_lines=%d, ceiling=%v)\n",
		budget, wtPath, changedLines, effectiveCeiling)

	loopStart := clk.Now()
	deadline := loopStart.Add(budget)
	var livenessChecker paneLivenessChecker
	if lc, ok := qs.(paneLivenessChecker); ok {
		livenessChecker = lc
	}
	hardDeadline := loopStart.Add(effectiveCeiling)

	heartbeatExtensionCeiling := loopStart.Add(2 * budget)
	if heartbeatExtensionCeiling.After(hardDeadline) {
		heartbeatExtensionCeiling = hardDeadline
	}

	var verdictRunner tmux.CommandRunner
	if crp, ok := qs.(commandRunnerProvider); ok {
		if r := crp.commandRunner(); !gitprobe.RunnerIsLocalFS(r) {
			verdictRunner = r
		}
	}

	var lastHeartbeatAt time.Time
	heartbeatActiveGrace := reviewerHeartbeatActiveGrace

	reseedDeadline := loopStart.Add(reviewerReseedGrace)
	reseeded := inj == nil

	ticker := clk.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case env, ok := <-eventCh:
			if !ok {
				eventCh = nil
				continue
			}
			if env.Type == core.EventTypeAgentHeartbeat {
				lastHeartbeatAt = clk.Now()
			}

		case <-ticker.C():
			now := clk.Now()

			if eventCh != nil {
			drainReviewerHB:
				for {
					select {
					case env, ok := <-eventCh:
						if !ok {
							eventCh = nil
							break drainReviewerHB
						}
						if env.Type == core.EventTypeAgentHeartbeat {
							lastHeartbeatAt = now
						}
					default:
						break drainReviewerHB
					}
				}
			}

			if !reseeded && now.After(reseedDeadline) {
				if statTaskFileVia(ctx, verdictRunner, verdictPath) == nil {
					reseeded = true
				} else if livenessChecker == nil || livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: no verdict after %v re-seed grace and pane active in %s; re-seeding reviewer brief once (hk-7rgqs)\n",
						reviewerReseedGrace, wtPath)
					var reseedRunner tmux.CommandRunner
					if crp, ok2 := inj.(commandRunnerProvider); ok2 {
						if r := crp.commandRunner(); !gitprobe.RunnerIsLocalFS(r) {
							reseedRunner = r
						}
					}
					if reason := pasteInjectReviewer(ctx, clk, inj, claudeSessID, wtPath, reseedRunner); reason != "" {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-review-file: re-seed failed for %s: %s\n",
							wtPath, reason)
					}
					reseeded = true
				}
			}

			if now.After(deadline) {
				recentHB := !lastHeartbeatAt.IsZero() && now.Sub(lastHeartbeatAt) < heartbeatActiveGrace
				paneActive := livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx)
				extCeiling := hardDeadline
				if !paneActive {
					extCeiling = heartbeatExtensionCeiling
				}
				if (paneActive || recentHB) && now.Before(extCeiling) {
					signal := "pane-active"
					if recentHB && !paneActive {
						signal = "heartbeat"
					} else if recentHB {
						signal = "pane-active+heartbeat"
					}
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: budget %v elapsed but reviewer active (%s) in %s; extending (ceiling %v, hard ceiling %v) [hk-60t8/hk-4u1mb]\n",
						budget, signal, wtPath, extCeiling.Sub(loopStart), effectiveCeiling)
					deadline = now.Add(reviewFileTimeout)
					if deadline.After(extCeiling) {
						deadline = extCeiling
					}
					continue
				}
				reason := "budget-exceeded"
				if !now.Before(hardDeadline) {
					reason = "hard-ceiling"
				} else if !now.Before(heartbeatExtensionCeiling) {
					reason = "heartbeat-ceiling"
				}
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-review-file: %s after %v waiting for %s (budget=%v, changed_lines=%d); sending /quit\n",
					reason, clk.Since(loopStart), verdictPath, budget, changedLines)
				writeReviewerBudgetSentinel(wtPath, budget, changedLines, clk.Since(loopStart), reason)
				if quitErr := qs.SendQuitToLastPane(ctx); quitErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: send /quit failed: %v\n", quitErr)
				}
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, killDelay): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					if killErr := killer.Kill(ctx); killErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-review-file: kill session failed: %v (the pane may still be alive)\n", killErr)
					}
				}
				return
			}

			if v, verr := workspace.ReadReviewVerdictVia(ctx, verdictRunner, wtPath); verr == nil && v != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-review-file: valid verdict detected at %s; sending /quit\n",
					verdictPath)
				if quitErr := qs.SendQuitToLastPane(ctx); quitErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: send /quit failed: %v\n", quitErr)
				}
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, postQuitKillGrace): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					if killErr := killer.Kill(ctx); killErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-review-file: kill session failed: %v (the pane may still be alive)\n", killErr)
					}
				}
				return
			}
		}
	}
}

func bufferName(sessionID, purpose string) string {
	return tmux.BufferName(sessionID, purpose)
}

func statTaskFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: task file absent: %s", tmux.ErrStructural, path)
		}
		return fmt.Errorf("daemon: pasteinject: stat %s: %w", path, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%w: task file empty: %s", tmux.ErrStructural, path)
	}
	return nil
}

func statTaskFileVia(ctx context.Context, runner tmux.CommandRunner, path string) error {
	if runner == nil {
		return statTaskFile(path)
	}
	out, err := runner.Command(ctx, "stat", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: task file absent on worker %s: %v\n%s",
			tmux.ErrStructural, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
