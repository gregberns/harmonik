package daemon

// pasteinject.go — post-spawn paste-inject step (hk-zrj83).
//
// After the daemon spawns a Claude pane via the tmux substrate, it must
// deliver a kick-off instruction to the pane so Claude knows which task file
// to read and begin work.  This is the B8 mechanism described in
// docs/claude-session-comms-audit-2026-05-13.md §6.
//
// Ordering invariant per specs/process-lifecycle.md §4.7 PL-021d and
// specs/claude-hook-bridge.md §4.11 CHB-028:
//
//  1. agent-task.md (or phase-variant) written to disk (owned by hk-9ow36).
//  2. Pane is live (SpawnWindow returned non-error).
//  3. pasteInjectOnLaunch fires — delivers the kick-off message via
//     WriteLastPane (tmux load-buffer + paste-buffer per PL-021d).
//
// Phase mapping:
//
//   - implementer-initial (single-mode)  → "-task"    → "Please read .harmonik/agent-task.md and begin."
//   - implementer-resume                 → "-task" (combined task + feedback in a single paste, hk-poy7k)
//   - reviewer                           → "-review"  → "Please read .harmonik/review-target.md ..."
//
// Spec refs:
//   - specs/process-lifecycle.md §4.7 PL-021d (paste mechanism, buffer-name discipline)
//   - specs/claude-hook-bridge.md §4.11 CHB-028 (agent-task.md contract)
//   - specs/execution-model.md §4.3 EM-015d-RFD (reviewer-feedback delivery)
//   - specs/execution-model.md §4.3 EM-015d-RIA (reviewer input artifact)
//
// Bead: hk-zrj83.

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
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// splashDismissDelay is the grace period between the Enter keypress (splash
// dismiss) and the paste-buffer write (kick-off message delivery).  The
// Claude Code welcome splash needs ~400–600ms to animate away and transition
// the terminal to the REPL input state; 750ms provides a conservative margin.
//
// NOTE (hk-7rgqs): this single fixed wait is NOT sufficient on its own under
// concurrent cold-boots, where the splash can take >750ms to clear and the
// post-paste submit Enter then lands on the still-up splash and is swallowed.
// The robust submit is the bounded-retry Enter (sendSubmitEnterWithRetry); this
// delay just keeps the FIRST submit attempt from arriving absurdly early.
//
// Declared as a var (not const) so tests can override it without waiting real
// wall time, matching every other timing knob in this file.
//
// Bead: hk-rf4ux, hk-7rgqs.
var splashDismissDelay = 750 * time.Millisecond

// resumeSubmitRetries and resumeSubmitRetryDelay govern the bounded submit-retry
// on EVERY post-paste submit Enter (implementer-initial, reviewer, and the
// implementer-resume iteration ≥ 2 path).
//
// Root cause (hk-ip33d, generalised by hk-7rgqs): the post-paste Enter that
// SUBMITS the kick-off prompt is intermittently dropped because the Claude Code
// REPL's input handler is not yet ready to accept the keypress at the instant
// SendEnterToLastPane fires.  Two arrival paths exhibit this:
//
//   - implementer-resume (hk-ip33d): a freshly `claude --resume <id>` TUI is
//     still settling after the welcome splash; the single Enter is dropped, the
//     combined task+feedback prompt sits unsubmitted, claude stays idle, and the
//     run goes run_stale with no iteration-2 progress.
//   - reviewer / implementer-initial under load (hk-7rgqs): under concurrent
//     cold-boots the splash takes >750ms to clear, so the FIXED splashDismissDelay
//     elapses while the splash is still up; the post-paste submit Enter lands on
//     the splash and is SWALLOWED, leaving the brief typed-but-UNSUBMITTED.  The
//     reviewer then idles, never reads review-target.md, never writes review.json,
//     and the run stalls until the verdict budget elapses.
//
// There is no pane-capture primitive on the enterSender interface to detect
// "input cleared", so we cannot positively confirm submission.  Instead we send
// the submit Enter, wait a short settle, and re-send it up to resumeSubmitRetries
// additional times.  A redundant Enter at a REPL that has ALREADY submitted is a
// harmless no-op (an empty line at the now-clear prompt), so the retries only
// ever help: at least one of them lands after the input handler is ready (and
// after a still-animating splash has cleared).  This reuses the same
// send-keys-Enter key-event idiom as the splash-dismiss path (hk-rf4ux) and the
// time-grace patterns already in this file.
//
// Declared as vars (not consts) so tests can override them without waiting real
// wall time.
//
// Bead: hk-ip33d.
var (
	resumeSubmitRetries    = 2
	resumeSubmitRetryDelay = 400 * time.Millisecond
)

// enterSender is an optional interface for tmux-backed Substrates that can
// send a bare Enter keypress to the last spawned pane via
// `tmux send-keys -t <pane> Enter` (NOT the -l literal form).
//
// This is the mechanism used to dismiss the Claude Code welcome splash before
// paste-inject, per the hk-rf4ux fix.  The splash is a React/ink TUI that
// processes key events; paste-buffer operates in bracketed-paste mode on
// modern terminals, which means literal bytes in the paste payload (including
// '\n') are not dispatched as key events.  Only send-keys without -l can
// generate a true Enter keypress that the TUI key-event handler sees.
//
// Bead: hk-rf4ux.
type enterSender interface {
	// SendEnterToLastPane sends a bare "Enter" key to the most recently
	// spawned window's first pane.  Returns a non-nil error if no window
	// has been spawned yet or if the underlying send-keys call fails.
	SendEnterToLastPane(ctx context.Context) error
}

// quitSender is an optional interface implemented by tmux-backed Substrates
// that can send `/quit Enter` to the most recently spawned pane as real
// key events (not bracketed paste).
//
// This is the mechanism for the daemon-side session-exit injection:  after
// the task commit lands in the worktree, the daemon calls SendQuitToLastPane
// to cause Claude Code's REPL to execute /quit, which fires the Stop hook
// and delivers outcome_emitted to the daemon socket.
//
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Bead: hk-cmybm.
type quitSender interface {
	// SendQuitToLastPane sends `/quit` followed by Enter to the most recently
	// spawned window's first pane.  Returns a non-nil error if no window has
	// been spawned yet or if the underlying send-keys call fails.
	SendQuitToLastPane(ctx context.Context) error
}

// paneOutputSizer is an optional interface that quitSender implementations
// may also satisfy to report the current pane output fingerprint (scrollback
// history size + cursor position).  Used by the activity-aware launch
// suppression (hk-az4fd, hk-ue0u2) to detect read-heavy implementers that
// are actively reading files and planning without yet editing the worktree.
// Such beads produce visible pane output (streaming LLM responses, tool
// results) even though git status is clean, so the worktree-activity
// fingerprint alone (hk-az4fd) cannot distinguish them from a genuinely-
// wedged pane.
//
// Bead: hk-ue0u2.
type paneOutputSizer interface {
	// PaneOutputFingerprint returns a string that changes as the pane
	// produces visible output (history size grows, cursor advances).
	// Returns ("", false) on any error (conservative: treat unknown as
	// no growth — the ceiling kill is allowed to proceed).
	PaneOutputFingerprint(ctx context.Context) (string, bool)
}

// paneLivenessChecker is an optional interface that quitSender implementations
// may also satisfy to report whether the tmux pane has an active child process
// (i.e. claude is running under the shell).
//
// pasteInjectQuitOnCommit probes the qs parameter for this interface at
// construction time.  When present, the liveness check is consulted before
// firing the noChange kill path on a launch-heartbeat-timeout or heartbeat-
// staleness event: if the pane has an active child process, the session is
// still alive (thinking phase, not yet emitting heartbeats) and the kill is
// suppressed; lastHeartbeat is reset so the staleness clock restarts.
//
// This distinguishes two cases the heartbeat watchdog cannot separate on its
// own:
//  1. Empty pane — paste delivered but claude never started → kill fast (~60s).
//  2. Active thinking — claude is running and downloading tokens but has not
//     yet made a tool call → do NOT kill.
//
// Bead: hk-fbydv.
type paneLivenessChecker interface {
	// PaneHasActiveProcess returns true when the tmux pane shell has at least
	// one child process (i.e. the hosted claude process is still running).
	// Returns false on any error (conservative: treat unknown as dead).
	PaneHasActiveProcess(ctx context.Context) bool
}

// livePaneCommandSubstrings are command-name fragments that identify a hosted
// agent process running directly as the tmux pane's foreground process.
//
// hk-tgqy5: tmux often runs a pane command via `sh -c "<command>"`, and when
// the command is a single program the shell may exec into it, so the pane PID
// becomes the agent process itself (no child shell, no descendants while it is
// in a thinking phase with no tool subprocess spawned).  In that arrangement a
// children-only probe (`pgrep -P <panePID>`) returns nothing for a perfectly
// healthy agent.  We therefore also accept the pane PID *itself* as evidence of
// liveness when its command matches one of these fragments.
//
// This is the fallback used by hasChildProcess and by perRunSubstrate instances
// whose HandlerBinary resolves to "claude". Derived-binary overrides are set per
// run via agentCommandFragmentsFor.
var livePaneCommandSubstrings = []string{"claude", "node"}

// agentCommandFragmentsFor returns command-name substrings to match against the
// tmux pane's foreground process command for liveness detection.
//
// When binary is empty or its basename is "claude", the function returns
// livePaneCommandSubstrings (preserving the existing "claude"/"node" behaviour,
// since the claude CLI is a Node.js application that may exec as "node").  For
// any other binary the basename alone is returned so that custom handler binaries
// (non-claude agents) are matched correctly.
//
// Bead: hk-vhped.
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

// hasChildProcess reports whether the process identified by pid represents a
// live hosted agent — either because pid has at least one descendant process,
// or because pid itself is a recognised agent command.  NOT a direct-children-
// only check.
//
// hk-tgqy5 root cause: the watchdog drives the pane-liveness probe through this
// function with the tmux pane PID.  A "true" result suppresses the no-commit
// kill.  The original implementation only checked direct children
// (`pgrep -P <pid>` exit-0).  Two failure modes produced false negatives for a
// healthy claude implementer mid-work, causing the daemon to falsely declare
// `no_commit_during_implementer` while the commit was still minutes away:
//
//  1. Pane runs `sh -c "claude …"` and the shell exec'd into claude → pane PID
//     IS claude, with no children during a thinking phase → direct-children
//     check returns false.
//  2. Pane runs a wrapper shell that hosts claude as a descendant; during a
//     thinking phase claude has spawned no tool subprocess, so the only live
//     descendant is claude itself at some depth.
//
// The fix: (a) walk the full descendant subtree (any live descendant → active),
// and (b) recognise the pane PID itself as a live agent when its command name
// matches livePaneCommandSubstrings.  A pane where claude has genuinely exited
// has no agent descendant and the residual shell command does not match, so it
// still returns false — preserving legitimate dead-pane detection.
//
// A non-positive PID returns false.  Any probe error is treated conservatively
// as "not alive at this level".
//
// Bead: hk-fbydv (original), hk-tgqy5 (descendant-tree + self-command fix).
func hasChildProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	// (a) Any descendant process at all → the pane hosts a live process tree.
	//     If pid has even a single direct child, a descendant exists; a deeper
	//     descendant cannot exist without its ancestor chain, so a single
	//     direct-child probe is sufficient for existence.
	if hasAnyDirectChild(pid) {
		return true
	}
	// (b) No children — but the pane PID may itself be the agent (exec'd shell).
	//     Treat a recognised agent command as live.
	return commandMatchesLiveAgent(pid, livePaneCommandSubstrings)
}

// hasAnyDirectChild reports whether pid has at least one direct child process,
// via `pgrep -P <pid>` (exit-0 ⇒ at least one match).
func hasAnyDirectChild(pid int) bool {
	return exec.Command("pgrep", "-P", fmt.Sprintf("%d", pid)).Run() == nil
}

// hasAnyDirectChildVia is like hasAnyDirectChild but routes the pgrep probe
// through runner instead of bare exec.Command.  Used by perRunSubstrate so
// that remote-substrate workers can probe processes on the remote host via
// SSHRunner.
//
// Bead: hk-rs-b9-liveness-1m9n.
func hasAnyDirectChildVia(ctx context.Context, runner tmux.CommandRunner, pid int) bool {
	return runner.Command(ctx, "pgrep", "-P", fmt.Sprintf("%d", pid)).Run() == nil
}

// commandMatchesLiveAgent reports whether the command name of pid contains one
// of the provided fragments (e.g. "claude" or its "node" runtime).  Uses
// `ps -o comm= -p <pid>`, which is available on macOS and mainstream Linux.
// Returns false on any error or empty output (conservative).
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

// commandMatchesLiveAgentVia is like commandMatchesLiveAgent but routes the
// ps probe through runner instead of bare exec.Command.  Used by
// perRunSubstrate for remote-substrate liveness detection.
//
// Bead: hk-rs-b9-liveness-1m9n.
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

// hasAnyDirectChildOrSSHFail is like hasAnyDirectChildVia but also returns
// whether the failure was an SSH connection error (ssh exit-255). Callers use
// the connFailed flag to emit worker_offline and disable the worker in-memory.
//
// Returns: (alive bool, connFailed bool).
//
// Bead: hk-rs-b11-offline-dh57.
func hasAnyDirectChildOrSSHFail(ctx context.Context, runner tmux.CommandRunner, pid int) (alive bool, connFailed bool) {
	err := runner.Command(ctx, "pgrep", "-P", fmt.Sprintf("%d", pid)).Run()
	if err == nil {
		return true, false
	}
	return false, tmux.IsSSHConnectionFailure(err)
}

// commandMatchesLiveAgentOrSSHFail is like commandMatchesLiveAgentVia but also
// returns whether the failure was an SSH connection error (ssh exit-255).
//
// Returns: (alive bool, connFailed bool).
//
// Bead: hk-rs-b11-offline-dh57.
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

// probeLivenessOrSSHFail runs both liveness probes (pgrep -P then ps comm=)
// via runner and returns (alive, connFailed). connFailed is true when an SSH
// connection failure (exit-255) is detected on any probe; in that case alive
// is always false. The function returns immediately (no wedge) even when SSH
// is unreachable because the underlying exec returns promptly on failure.
//
// This is the testable core used by perRunSubstrate.PaneHasActiveProcess.
//
// Bead: hk-rs-b11-offline-dh57.
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

// commandRunnerProvider is an optional interface that a quitSender may
// implement to expose its CommandRunner.  pasteInjectQuitOnCommit probes qs
// for this interface so that resolveWorktreeHEAD and worktreeActivityFingerprint
// are routed through the run's CommandRunner (e.g. SSHRunner for remote
// substrates) instead of bare exec.Command.
//
// Bead: hk-rs-b9-liveness-1m9n.
type commandRunnerProvider interface {
	commandRunner() tmux.CommandRunner
}

// resolveWorktreeHEADVia is like resolveWorktreeHEAD but routes the git probe
// through runner instead of bare exec.CommandContext.  Uses `git -C <wtPath>
// rev-parse HEAD` (the -C form works for both local and SSH runners).
//
// When runner is nil the call delegates to the bare-local resolveWorktreeHEAD,
// so callers can pass the per-run runner unconditionally and get byte-identical
// local behaviour for LOCAL runs (nil runner) — NFR7.
//
// Bead: hk-rs-b9-liveness-1m9n.
func resolveWorktreeHEADVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	if runner == nil {
		return resolveWorktreeHEAD(ctx, wtPath)
	}
	out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("daemon: resolveWorktreeHEADVia: git -C %q rev-parse HEAD: %w", wtPath, err)
	}
	sha := string(out)
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: resolveWorktreeHEADVia: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return sha, nil
}

// runnerIsLocalFS reports whether r operates on box A's local filesystem — i.e.
// the worktree paths it is given are directly stat-able with os.Stat. A nil
// runner (defensive) and tmux.LocalRunner both qualify; an SSHRunner (or any
// other transport) does NOT, because its worktree lives on a remote worker.
func runnerIsLocalFS(r tmux.CommandRunner) bool {
	switch r.(type) {
	case nil, tmux.LocalRunner:
		return true
	default:
		return false
	}
}

// worktreeActivityFingerprintVia is like worktreeActivityFingerprint but routes
// the git probes through runner.
//
// The per-file os.Stat(size, mtime) precision component is retained ONLY when
// runner is local-filesystem (nil / tmux.LocalRunner) — for a LOCAL run wtPath
// is on box A and the stat is meaningful, keeping the fingerprint byte-identical
// to worktreeActivityFingerprint (NFR7). For a REMOTE run wtPath lives on the
// worker, so a box-A os.Stat would either error (file absent) or read an
// unrelated box-A file; we DROP that component and rely on the routed
// HEAD + `git status --porcelain` (which run on the worker via the SSHRunner)
// to detect implementer activity. This loses sub-status-change precision but
// preserves correctness for remote runs.
//
// Bead: hk-rs-b9-liveness-1m9n.
func worktreeActivityFingerprintVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, bool) {
	head, err := resolveWorktreeHEADVia(ctx, runner, wtPath)
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
	if runnerIsLocalFS(runner) {
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

// briefDeliveredTimeout is the maximum time pasteInjectQuitOnCommit will wait
// for the briefDelivered channel to be closed (i.e. for pasteInjectOnLaunch to
// confirm the kick-off paste landed in the pane) before proceeding with the
// commit poll loop.
//
// 2 minutes is generous: paste delivery normally completes within ~1 second
// (splashDismissDelay + WriteLastPane).  If briefDelivered is not signalled
// within this window, the session is likely broken and the commit poll loop is
// started anyway — the subsequent commitPollTimeout will clean it up.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-930o3.
var briefDeliveredTimeout = 2 * time.Minute

// commitPollInterval is the interval between git HEAD checks in
// pasteInjectQuitOnCommit.  500ms balances responsiveness with avoiding
// excessive git subprocess overhead.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
var commitPollInterval = 500 * time.Millisecond

// commitPollTimeout is the per-progress commit-budget window: the maximum time
// pasteInjectQuitOnCommit will wait for a new commit WITHOUT a fresh progress
// signal before giving up.  It is NOT a flat wall-clock deadline — every genuine
// progress signal (an agent_heartbeat event) extends the budget by another
// commitPollTimeout window (see commitHardCeiling for the absolute backstop).
// This is a safety backstop only; the primary kill trigger is heartbeat
// staleness (heartbeatStalenessThreshold).
//
// hk-9vp51: previously this was a FLAT 30-min wall clock that guillotined any
// implementer that was genuinely working but slow to commit (e.g. a deep
// go-test loop): the pane stayed "active" forever, slipping the 180s/8m
// heartbeat checks, and was killed only by this flat deadline — silently, as
// no_commit.  Making the budget progress-extended (with a hard ceiling) lets a
// progressing session run as long as it keeps making progress, while a
// stalled-but-active session is still killed once progress goes stale.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
var commitPollTimeout = 30 * time.Minute

// commitHardCeiling is the absolute wall-clock backstop for the commit-poll
// loop.  Unlike commitPollTimeout it is NEVER extended by progress signals: once
// the loop has run this long, the session is force-killed regardless of pane
// activity or heartbeats.  This bounds a truly-hung-but-pane-active implementer
// (one that emits heartbeats forever but never commits) so it cannot run
// indefinitely.
//
// hk-9vp51: set to 90 min — generous enough that a legitimate long task (deep
// go-test loops, multi-file refactors that run the full suite, ~30–45 min) that
// keeps making progress survives well past the old flat 30-min guillotine, while
// still bounding a genuinely-stuck session.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
var commitHardCeiling = 90 * time.Minute

// heartbeatStalenessThreshold is the maximum time pasteInjectQuitOnCommit will
// tolerate without receiving an agent_heartbeat event before it fires the kill
// path (sends /quit, waits noChangeKillDelay, calls killer.Kill).
//
// The daemon emits agent_heartbeat every ~5 minutes (handler.HeartbeatInterval =
// 300 s).  8 minutes of staleness means we allow ~1.6 missed heartbeats before
// concluding the session is stuck — long enough to survive a single missed beat
// while still killing sessions that have gone dark.
//
// The threshold only applies when an event channel is provided (eventCh != nil).
// When eventCh is nil the check is skipped and the function falls back to the
// wall-clock commitPollTimeout as the sole guard.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-7srrd.
var heartbeatStalenessThreshold = 8 * time.Minute

// launchHeartbeatTimeout is the maximum time pasteInjectQuitOnCommit will wait
// for the first agent_heartbeat event after brief delivery before concluding
// the paste landed in an empty pane and killing the session.
//
// The "launch verification" window begins when briefDelivered closes (or when
// the function enters the poll loop, if briefDelivered is nil) and ends when
// either the first heartbeat arrives or launchHeartbeatTimeout elapses.  If it
// elapses without a heartbeat (and without a commit landing), the session is
// killed via the noChange path so the workloop reopens the bead for retry.
//
// 180s gives Claude Code time to start, read the brief, load context, and
// emit its first activity.  The original 60s was too tight for complex beads
// that require multi-file reads before the first tool call (which emits a
// heartbeat).  Without this guard, an empty-pane stall (paste delivered to a
// dead tmux pane) would not be detected until the 8-minute heartbeat-staleness
// threshold fires, wasting a full slot.  The paneLivenessChecker provides a
// secondary defense: when the pane has an active child process, the timeout
// is suppressed and the deadline extended.
//
// Only active when eventCh is non-nil (heartbeatProvided = true).
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-3gq0b.
var launchHeartbeatTimeout = 180 * time.Second

// launchSuppressionCeiling is the ABSOLUTE bound on how long the launch-
// verification window (hk-3gq0b) may be suppressed by a still-active pane
// (hk-fbydv) before the kill fires unconditionally.
//
// hk-jgxqc root cause: the launch-verification branch resets launchDeadline on
// every tick where the first agent_heartbeat has NOT yet arrived but the pane
// reports an active child process.  Under concurrency the per-run heartbeat tap
// (tapCh) is drained by a competing consumer (chanAgentEventSource feeding
// waitAgentReady), so pasteInjectQuitOnCommit NEVER observes a heartbeat,
// firstHeartbeatSeen stays false forever, and the suppression resets the launch
// deadline UNBOUNDEDLY — the goroutine spins in a sleep/reset loop emitting
// "launch-heartbeat-timeout suppressed" every launchWindow and never proceeds to
// /quit → grace → force-kill, so sess.Wait never unblocks and the workflow never
// advances from implement → merge.  The commit-budget path has commitHardCeiling
// as its absolute backstop; the launch-verification path had NO such ceiling.
//
// This ceiling caps the TOTAL launch-suppression window measured from loopStart.
// Once it elapses the launch-verification branch stops suppressing and fires the
// noChange kill even when the pane still reports an active child process —
// guaranteeing the post-spawn watchdog always terminates.  It does NOT regress
// the legitimate launch-phase suppression: a genuinely-booting Claude emits its
// first heartbeat (or commits) well within this window, which clears
// firstHeartbeatSeen / triggers commit detection and exits the branch normally.
//
// 12 min is a generous multiple of the 180s launchWindow (≈4 windows): long
// enough that no legitimate launch is guillotined, short enough that a wedged
// run is freed in minutes rather than at the 90-min commitHardCeiling.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-jgxqc.
var launchSuppressionCeiling = 12 * time.Minute

// noChangeKillDelay is the grace period between the unconditional /quit send
// (on commitPollTimeout) and the forced sess.Kill call.  30 s gives Claude Code
// time to respond to /quit and exit cleanly; if the pane is still alive after
// this window the session is killed unconditionally.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-trjef.
var noChangeKillDelay = 30 * time.Second

// implementerReseedGrace is the window after brief delivery within which
// pasteInjectQuitOnCommit expects a commit to land.  When this window elapses
// without a new commit AND qs also implements enterSender, a one-shot
// "reseed Enter" is sent to submit any pending unsubmitted input in the pane.
//
// The targeted failure mode (hk-76n5g): the brief was pasted into the input
// bar and all retry Enters (hk-ip33d: 3 total, over ~800 ms) were swallowed
// because the TUI was still absorbing the paste when they fired.  The seed
// then sits typed-but-unsubmitted; the implementer never reads agent-task.md;
// the run hangs until the 30-minute commitPollTimeout fires.  Sending one
// additional Enter ~75 s later submits the pending input and restores normal
// flow — long before the 30-minute kill that was the only prior recovery.
//
// 75 s is short enough to recover a wedged-at-unsubmitted seed quickly, yet
// long enough that a fast implementer (committed in < 75 s) never sees a
// spurious empty-line Enter.  A redundant Enter at an already-clear prompt is
// a harmless no-op.
//
// Declared as var so tests can override it without waiting real wall time.
//
// Bead: hk-76n5g.
var implementerReseedGrace = 75 * time.Second

// postQuitKillGrace is the grace period between the post-commit /quit send and
// the forced sess.Kill call on the substrate-path session.  Without this kill,
// sess.Wait (which polls the tmux pane PID for liveness) can hang indefinitely
// when claude has exited but the surrounding shell pid is still alive, or when
// /quit landed in the wrong pane (stale handle from a prior daemon's killed
// run).  60 s gives Claude Code plenty of time to respond to /quit and exit
// cleanly under normal conditions; if the pane is still alive after that the
// session is force-killed so the workloop's sess.Wait unblocks and the review
// loop proceeds to the reviewer phase.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
//
// Bead: hk-5s7tg.
var postQuitKillGrace = 60 * time.Second

// sessionKiller is a subset of handler.Session used by pasteInjectQuitOnCommit
// to force-kill the hosted session when commitPollTimeout fires without a commit.
//
// Bead: hk-trjef.
type sessionKiller interface {
	Kill(ctx context.Context) error
}

// pasteInjectQuitOnCommit watches the worktree at wtPath for a new commit
// (HEAD changing from initialSHA).  When detected, it sends `/quit Enter`
// to the pane via qs to cause Claude Code to exit and fire the Stop hook.
//
// This is the daemon-side complement to the agent-task.md session-completion
// instruction (CHB-028 / hk-cmybm).  Claude Code agents cannot execute slash
// commands from their tool API; the daemon detects the commit landing and
// injects /quit programmatically.
//
// briefDelivered is a channel that pasteInjectOnLaunch closes after the
// kick-off message has been written to the pane via WriteLastPane.  The
// function blocks on briefDelivered (up to briefDeliveredTimeout) before
// entering the commit poll loop.  This prevents a stale-pane /exit race:
// without the gate, if a stale tmux handle from a prior run receives the
// /quit before the newly-launched claude sees the brief, the implementer
// session is torn down with zero assistant turns (hk-930o3).  briefDelivered
// may be nil — when nil the gate is skipped (backward-compat for callers
// that do not have paste-inject capability).
//
// The function runs in a goroutine and returns when:
//   - A new commit is detected and /quit is sent (success).  A post-quit
//     watchdog goroutine is also launched (hk-5s7tg): it waits
//     postQuitKillGrace then calls killer.Kill so sess.Wait unblocks even
//     when the pane is stuck (stale tmux handle, surviving shell pid).
//   - Heartbeat staleness exceeds heartbeatStalenessThreshold (when eventCh
//     is non-nil): the session has gone dark without committing; /quit is sent
//     unconditionally, noChangeKillDelay is waited, then killer.Kill is called,
//     and noChangeTimeoutCh is closed (hk-7srrd).
//   - commitPollTimeout (total wall-clock backstop) elapses without a new
//     commit: same kill sequence as heartbeat-stale path (hk-trjef).
//   - ctx is cancelled (daemon shutting down).
//
// eventCh receives core.EventEnvelope values from the per-run event tap (the
// same channel used by waitAgentReady).  When an agent_heartbeat event arrives,
// the last-heartbeat timestamp is refreshed and the staleness clock resets.
// When eventCh is nil the heartbeat-staleness check is skipped; only the
// wall-clock commitPollTimeout acts as the kill trigger.
//
// killer and noChangeTimeoutCh may be nil (the kill and signal steps are
// skipped when either is absent).
//
// bus and runID drive the hk-9vp51 implementer_budget_exceeded diagnostic: when
// a kill fires because the commit budget (hard ceiling or stale progress) was
// exhausted, an implementer_budget_exceeded event is emitted carrying the
// elapsed time and time-since-last-progress, so a previously-silent no_commit
// becomes self-explaining.  Both may be nil (event emission is skipped); the
// kill still fires.
//
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Beads: hk-cmybm, hk-trjef, hk-5s7tg, hk-930o3, hk-7srrd, hk-9vp51.
func pasteInjectQuitOnCommit(
	ctx context.Context,
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
	// hk-930o3: wait for brief delivery confirmation before entering the commit
	// poll loop.  This prevents a /quit racing the brief when a stale tmux pane
	// handle from a prior run is reused: without this gate the commit watcher
	// may fire /quit before the newly-launched claude has read agent-task.md,
	// tearing down the session with zero assistant turns.
	if briefDelivered != nil {
		bdTimeout := briefDeliveredTimeout // snapshot before blocking
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
			// Brief delivered — proceed to commit polling.
		case <-time.After(bdTimeout):
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-commit: brief_delivered timeout after %v for %s; proceeding with commit poll (session may be broken)\n",
				bdTimeout, wtPath)
		}
	}

	// Snapshot the tunable durations into locals so tests that restore
	// package vars after the surrounding run returns do not race with our
	// reads inside the for loop.  Only the values captured here matter.
	pollTimeout := commitPollTimeout
	pollInterval := commitPollInterval
	killDelay := noChangeKillDelay
	stalenessThreshold := heartbeatStalenessThreshold
	launchWindow := launchHeartbeatTimeout
	hardCeiling := commitHardCeiling
	// hk-jgxqc: snapshot the absolute launch-suppression ceiling. After this
	// window (measured from loopStart) the launch-verification branch may no
	// longer suppress on an active pane — it fires the kill so the watchdog
	// always terminates.
	launchSuppressCeil := launchSuppressionCeiling
	// hk-76n5g: snapshot the reseed-Enter grace so a test that restores the
	// package var after the run returns does not race with the in-flight read.
	reseedGrace := implementerReseedGrace

	loopStart := time.Now()
	// hk-9vp51: totalDeadline is the per-PROGRESS commit budget, extended on every
	// genuine progress signal (agent_heartbeat) rather than a flat wall clock.
	totalDeadline := loopStart.Add(pollTimeout)
	// hk-9vp51: hardDeadline is the absolute backstop — never extended; bounds a
	// truly-hung-but-pane-active session.
	hardDeadline := loopStart.Add(hardCeiling)
	lastHeartbeat := time.Now() // initialised to now; first real beat resets it
	// hk-9vp51: lastProgress tracks the last genuine progress signal for the
	// implementer_budget_exceeded diagnostic (since_last_progress_ms).
	lastProgress := loopStart
	heartbeatProvided := eventCh != nil
	// hk-3gq0b: launch-verification window — starts after brief delivery.
	// When heartbeatProvided, the first heartbeat must arrive within launchWindow
	// or the session is killed (paste likely landed in an empty pane).
	launchDeadline := time.Now().Add(launchWindow)
	// hk-jgxqc: absolute backstop for the launch-verification window. Unlike
	// launchDeadline (which the suppress branch resets on every active-pane
	// tick), this is NEVER extended — once it passes the suppression is no
	// longer permitted and the kill fires even on an active pane.
	launchSuppressDeadline := loopStart.Add(launchSuppressCeil)
	firstHeartbeatSeen := false
	// lastLaunchSuppressLog / lastStalenessLog were throttle variables for
	// "suppressed" diagnostic lines (F21). Log lines removed — the suppression
	// behavior (clock reset) is the observable effect; no log needed.
	var lastLaunchSuppressLog time.Time
	var lastStalenessLog time.Time

	// hk-rs-b9: resolve the CommandRunner from qs when it implements
	// commandRunnerProvider (production: perRunSubstrate with an SSHRunner for
	// remote workers).  Falls back to tmux.LocalRunner{} so local behaviour is
	// unchanged when qs does not carry a runner.
	probeRunner := tmux.CommandRunner(tmux.LocalRunner{})
	if crp, ok := qs.(commandRunnerProvider); ok {
		probeRunner = crp.commandRunner()
	}

	// hk-az4fd: worktree-activity fingerprint for activity-aware launch
	// suppression.  An implementer that is ACTIVELY editing files (but has not
	// yet committed or emitted a heartbeat the tap can observe) advances this
	// fingerprint every tick; a truly-wedged pane (active child but doing
	// nothing) leaves it stable.  When the launch-suppression ceiling elapses we
	// consult this to distinguish "working past the ceiling" (defer to the 90-min
	// hard budget) from "wedged at the ceiling" (kill — preserves hk-jgxqc).
	lastActivityFingerprint, _ := worktreeActivityFingerprintVia(ctx, probeRunner, wtPath)

	// hk-ue0u2: pane-output fingerprint baseline — initialised before the
	// loop so the first tick has a reference to diff against.  Nil when qs
	// does not implement paneOutputSizer (e.g. test stubs, nil substrate).
	outputSizer, _ := qs.(paneOutputSizer)
	lastPaneOutputFP := ""
	if outputSizer != nil {
		lastPaneOutputFP, _ = outputSizer.PaneOutputFingerprint(ctx)
	}

	// hk-fbydv: optional pane liveness checker — probed once; nil when qs does
	// not implement paneLivenessChecker (e.g. test stubs, nil substrate path).
	livenessChecker, _ := qs.(paneLivenessChecker)

	// hk-76n5g: one-shot reseed-Enter setup.  When qs also implements
	// enterSender (production: perRunSubstrate; test stubs that combine both),
	// fire one Enter after reseedGrace if no commit has appeared — submits any
	// pending unsubmitted input from a dropped paste-inject seed Enter.  A
	// redundant Enter at an already-submitted REPL is a harmless empty line.
	// Disable when qs has no enterSender capability (reseedEnterFired=true
	// short-circuits the check on every tick).
	reseedES, _ := qs.(enterSender)
	reseedEnterDeadline := loopStart.Add(reseedGrace)
	reseedEnterFired := reseedES == nil

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// fireNoChangePath sends /quit, waits killDelay, kills, and closes
	// noChangeTimeoutCh.  Extracted to avoid duplication between the heartbeat-
	// stale and total-deadline paths.
	//
	// hk-9vp51: budgetExceeded marks a kill caused by exhausting the commit
	// budget (hard ceiling reached, or progress went stale).  When set, an
	// implementer_budget_exceeded diagnostic is emitted (when bus != nil) carrying
	// elapsed and since-last-progress so a previously-silent no_commit explains
	// itself.  reasonTag is the short machine-readable reason for that payload.
	fireNoChangePath := func(reason, reasonTag string, budgetExceeded bool) {
		fmt.Fprintf(os.Stderr,
			"daemon: pasteinject: quit-on-commit: %s in %s (initial=%s); sending /quit unconditionally\n",
			reason, wtPath, initialSHA)
		// hk-9vp51: emit the budget-exceeded diagnostic before tearing down so
		// the event is durable even if the kill steps below block on ctx.Done().
		if budgetExceeded {
			now := time.Now()
			emitImplementerBudgetExceeded(ctx, bus, runID,
				now.Sub(loopStart), now.Sub(lastProgress), reasonTag)
		}
		// Step 1: send /quit unconditionally — Claude may have self-quit
		// without committing (e.g. detected nothing-to-do).
		if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-commit: noChange SendQuitToLastPane: %v\n", qErr)
		}
		// Step 2: wait noChangeKillDelay for Claude to exit cleanly, then
		// force-kill so sess.Wait unblocks in the workloop.
		select {
		case <-ctx.Done():
			return
		case <-time.After(killDelay):
		}
		if killer != nil {
			if kErr := killer.Kill(ctx); kErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: noChange Kill: %v\n", kErr)
			}
		}
		// Step 3: signal the workloop that we killed due to noChange-timeout.
		if noChangeTimeoutCh != nil {
			close(noChangeTimeoutCh)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return

		case env, ok := <-eventCh:
			// eventCh is nil-safe: a nil channel blocks forever, so this case
			// is never selected when eventCh is nil.
			if !ok {
				// Channel closed — treat as lost heartbeat source; fall through
				// to poll-tick logic by nulling the channel so future selects
				// don't re-enter this branch.
				eventCh = nil
				continue
			}
			if core.EventType(env.Type) == core.EventTypeAgentHeartbeat {
				now := time.Now()
				lastHeartbeat = now
				firstHeartbeatSeen = true
				// hk-9vp51: a genuine progress signal extends the per-progress
				// commit budget so a session that keeps making progress is not
				// guillotined by the flat budget window.  The absolute
				// hardDeadline is NOT extended.
				lastProgress = now
				totalDeadline = now.Add(pollTimeout)
			}

		case <-ticker.C:
			now := time.Now()

			// hk-ukx: drain any heartbeats that arrived in eventCh between the
			// last iteration and this tick.  Without this drain, the ticker case
			// can fire before a buffered heartbeat is processed by the eventCh
			// case, causing totalDeadline to look expired even though progress
			// was imminent.  The drain is non-blocking (default: exit) and runs
			// only for the heartbeat event type so other event types are not
			// silently consumed.
			if eventCh != nil {
			drainHeartbeats:
				for {
					select {
					case env, ok := <-eventCh:
						if !ok {
							eventCh = nil
							break drainHeartbeats
						}
						if core.EventType(env.Type) == core.EventTypeAgentHeartbeat {
							drainNow := time.Now()
							lastHeartbeat = drainNow
							firstHeartbeatSeen = true
							lastProgress = drainNow
							totalDeadline = drainNow.Add(pollTimeout)
						}
					default:
						break drainHeartbeats
					}
				}
			}

			// hk-9vp51: absolute hard-ceiling backstop — never extended by
			// progress.  Bounds a truly-hung-but-pane-active implementer (one
			// that emits heartbeats forever, or keeps the pane active, but never
			// commits) so it cannot run indefinitely.  Checked FIRST so it always
			// wins over the progress-aware budget below.
			if now.After(hardDeadline) {
				fireNoChangePath(
					fmt.Sprintf("hard-ceiling %v reached without a new commit", hardCeiling),
					"hard-ceiling", true)
				return
			}

			// hk-9vp51: per-progress commit-budget backstop.  Unlike the old flat
			// wall-clock deadline, this fires only when the budget window has
			// elapsed AND the pane is not making observable progress.
			//
			// hk-ukx: ACTIVITY-AWARE budget extension.  Previously an active child
			// process alone was enough to extend the budget ("a long go-test loop
			// that emits no agent_heartbeat").  But an idle Claude pane — one
			// waiting for input with no pending tool calls — also has an active
			// process, causing wedged runs to hang until the 90-min hardDeadline
			// instead of the 30-min commitPollTimeout.  The fix mirrors the launch-
			// verification block: require demonstrable progress (worktree change OR
			// pane output growth) in addition to an active process before extending.
			// A go-test loop produces pane output (test results streaming to the
			// pane), so it still gets the extension.  An idle Claude waiting for
			// input has a stable fingerprint and is killed at the 30-min boundary.
			if now.After(totalDeadline) {
				// Any buffered heartbeats were drained above; if totalDeadline
				// is still expired, no heartbeat arrived in this budget window.
				// Require observable progress before extending (hk-ukx).
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					wtFP, wtOK := worktreeActivityFingerprintVia(ctx, probeRunner, wtPath)
					budgetWorktreeProgressed := wtOK && wtFP != lastActivityFingerprint

					budgetPaneOutputProgressed := false
					if outputSizer != nil {
						if fp, ok := outputSizer.PaneOutputFingerprint(ctx); ok && fp != lastPaneOutputFP {
							budgetPaneOutputProgressed = true
							lastPaneOutputFP = fp
						}
					}

					if budgetWorktreeProgressed || budgetPaneOutputProgressed {
						signal := "changed working tree"
						if budgetPaneOutputProgressed && !budgetWorktreeProgressed {
							signal = "pane output growth"
						} else if budgetPaneOutputProgressed {
							signal = "changed working tree + pane output growth"
						}
						if budgetWorktreeProgressed {
							lastActivityFingerprint = wtFP
						}
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-commit: commit-budget %v elapsed but pane is making progress (%s) in %s; extending budget (hard ceiling %v)\n",
							pollTimeout, signal, wtPath, hardCeiling)
						lastProgress = now
						totalDeadline = now.Add(pollTimeout)
					} else {
						// Pane has active process but no observable progress —
						// idle Claude or wedged session.  Fire the ceiling so the
						// slot is freed within the 30-min window (hk-ukx).
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

			// hk-3gq0b: launch-verification check — if the first heartbeat has
			// not arrived within launchWindow after brief delivery, the paste
			// likely landed in an empty/dead pane.  Kill so the workloop reopens
			// the bead for retry.  Skipped once firstHeartbeatSeen is true.
			//
			// hk-fbydv: before killing, consult the pane liveness checker.  If
			// the pane shell has an active child process, Claude is in its initial
			// thinking phase (context loading, planning) and has not yet emitted
			// a heartbeat.  Reset lastHeartbeat and extend the launch deadline so
			// the kill does not fire until the session actually goes dark.
			if heartbeatProvided && !firstHeartbeatSeen && now.After(launchDeadline) {
				// hk-az4fd / hk-ue0u2: ACTIVITY-AWARE launch suppression.  Before
				// applying the hk-jgxqc ceiling, check for demonstrable progress
				// using two complementary signals:
				//
				//  1. Worktree activity (hk-az4fd): HEAD advanced, or working-tree
				//     changes churning.  Covers implementers that are EDITING files.
				//
				//  2. Pane output growth (hk-ue0u2): tmux scrollback history size or
				//     cursor position advanced.  Covers READ-HEAVY implementers that
				//     are reading/planning (streaming LLM responses, tool results)
				//     without yet editing the worktree — the T12 codex-registration
				//     false-kill scenario.
				//
				// Either signal alone is sufficient to treat the tick as a heartbeat:
				// clear firstHeartbeatSeen so the launch branch is permanently bypassed
				// and the run defers to the per-progress commit budget bounded by the
				// 90-minute hard ceiling (hk-9vp51) — NOT infinite.
				//
				// hk-jgxqc intent preserved: a pane that reports an active child but
				// produces NO progress on EITHER signal (stable worktree + stable pane
				// output) does NOT clear firstHeartbeatSeen, so the launch-suppression
				// ceiling below still fires for the genuinely-wedged case.
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

				// hk-jgxqc: the active-pane suppression is bounded by an absolute
				// ceiling (launchSuppressDeadline, NEVER extended).  Past that
				// ceiling the suppression is no longer permitted: a pane that has
				// reported "active child process" for the entire launch-suppression
				// window WITHOUT ever emitting a heartbeat, committing, OR making
				// observable worktree progress is wedged (e.g. an idle claude whose
				// heartbeats were consumed by a competing tapCh reader under
				// concurrency), so we MUST fire the kill so sess.Wait unblocks and
				// the workflow advances.  Within the ceiling the legitimate launch-
				// phase suppression is preserved unchanged.
				if now.Before(launchSuppressDeadline) &&
					livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					// F21: suppression log removed — fires per-run from a zero-time
					// baseline; the clock reset is the behavior, no log needed.
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

			// Check heartbeat staleness (only when eventCh was provided at init).
			//
			// hk-fbydv: before killing, consult the pane liveness checker.  If
			// the pane shell still has an active child process, the session is
			// alive but not emitting heartbeats (still in thinking phase).  Reset
			// lastHeartbeat so the staleness clock restarts from now.
			if heartbeatProvided && now.Sub(lastHeartbeat) > stalenessThreshold {
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					// F21: suppression log removed — same as launch-heartbeat suppression;
					// fires per-run from a zero baseline; clock reset is the behavior.
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

			// hk-76n5g: one-shot reseed-Enter. After reseedGrace with no new
			// commit, send one Enter to submit any pending unsubmitted input in
			// the pane (brief typed but Enter dropped on all paste-inject retry
			// attempts). Fires at most once per run. A redundant Enter at an
			// already-submitted REPL is a harmless empty line; it will not
			// re-submit a previously-processed prompt because the REPL is clear
			// after a submission is handled.
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

			headSHA, err := resolveWorktreeHEADVia(ctx, probeRunner, wtPath)
			if err != nil {
				// Worktree may not be ready yet; keep polling.
				continue
			}
			if headSHA != initialSHA {
				// New commit detected — send /quit to trigger Stop hook.
				if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: SendQuitToLastPane: %v\n", qErr)
				}
				// hk-5s7tg: schedule a post-quit watchdog that force-kills the
				// session if it has not exited after postQuitKillGrace.  Without
				// this, sess.Wait (substrate path) can hang indefinitely when
				// /quit landed in the wrong pane (stale handle from a prior
				// daemon's killed run) or when the surrounding shell pid stays
				// alive after claude exits.  The 1.75-hour daemon hang on the
				// hk-g0ckv dispatch (2026-05-21) had exactly this shape: the
				// implementer committed cleanly but the daemon's sess.Wait never
				// unblocked, so reviewer_launched was never emitted.
				//
				// killer may be nil (some callers pass nil); in that case we
				// skip the kill step but still return — the workloop will then
				// fall back to its own (much longer) ctx-cancel timeout.
				if killer != nil {
					// Snapshot the grace duration here so the goroutine does
					// not race with test code that restores the package var
					// after the run returns.
					grace := postQuitKillGrace
					go func() {
						select {
						case <-ctx.Done():
							return
						case <-time.After(grace):
						}
						if kErr := killer.Kill(ctx); kErr != nil {
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

// pasteInjectMsgs holds the phase-specific kick-off messages delivered to the
// pane after spawn.  Newline-terminated so the pane receives a complete line
// ready for Claude to act on.

// pasteInjectOnLaunch fires the post-spawn paste-inject step for a single
// agent launch.  It is a no-op when the substrate does not implement
// pasteInjecter (e.g. in exec.CommandContext tests).
//
// Parameters:
//   - ctx          — caller context; cancellation propagates into WriteLastPane.
//   - substrate    — the handler.Substrate used for this launch; may be nil.
//   - claudeSessID — the Claude session ID minted for this launch (used in the
//     buffer name per PL-021d: "harmonik-<session-id>-<purpose>").
//   - phase        — the review-loop phase (empty string = single-mode / implementer-initial).
//   - iterCount    — the 1-based iteration count (used in the feedback file name).
//   - wtPath       — absolute worktree path; used to stat the task/review files.
//   - bus          — event emitter used to emit pasteinject_failed on failure;
//     may be nil (event emission is skipped when nil).
//   - runID        — run identifier stamped on pasteinject_failed events; ignored
//     when bus is nil.
//
// Returns a channel that is closed once the kick-off paste has been written
// (or immediately when no paste is performed because the substrate does not
// implement pasteInjecter).  The caller passes this channel to
// pasteInjectQuitOnCommit as the briefDelivered gate (hk-930o3).
//
// Errors are non-fatal to the caller: a failed paste-inject is logged to
// stderr but does not reopen the bead.  The operator may manually trigger a
// paste using tmux.
//
// Bead: hk-fra5l (bus/runID parameters for pasteinject_failed emission).
func pasteInjectOnLaunch(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		if substrate == nil {
			return
		}
		inj, ok := substrate.(pasteInjecter)
		if !ok {
			return
		}

		// Extract the per-run runner for remote-aware file-stat probes (hk-hh5e).
		// For local runs commandRunner() returns LocalRunner{} (runnerIsLocalFS=true)
		// so runner stays nil and statTaskFileVia falls back to os.Stat — unchanged
		// local behaviour (NFR7).  For remote runs the SSHRunner is non-local, so
		// runner is set and statTaskFileVia checks file existence on the worker.
		var runner tmux.CommandRunner
		if crp, ok2 := substrate.(commandRunnerProvider); ok2 {
			if r := crp.commandRunner(); !runnerIsLocalFS(r) {
				runner = r
			}
		}

		var failReason string
		switch phase {
		case handlercontract.ReviewLoopPhaseReviewer:
			failReason = pasteInjectReviewer(ctx, inj, claudeSessID, wtPath, runner)

		case handlercontract.ReviewLoopPhaseImplementerResume:
			failReason = pasteInjectImplementerResume(ctx, inj, claudeSessID, iterCount, wtPath, runner)

		default:
			// Single-mode or implementer-initial: deliver agent-task.md kick-off.
			failReason = pasteInjectImplementerInitial(ctx, inj, claudeSessID, wtPath, runner)
		}

		// hk-fra5l: emit pasteinject_failed when the delivery failed.
		if failReason != "" && bus != nil {
			emitPasteInjectFailed(ctx, bus, runID, string(phase), failReason)
		}
	}()
	return ch
}

// emitPasteInjectFailed emits a pasteinject_failed event to bus.
// Non-fatal: errors are silently discarded (the paste failure is already logged
// to stderr by the calling helper).
//
// Bead: hk-fra5l.
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
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypePasteInjectFailed, b)
}

// emitImplementerBudgetExceeded emits an implementer_budget_exceeded event
// (hk-9vp51) when pasteInjectQuitOnCommit force-kills a hosted implementer
// session for exhausting its commit budget.  It makes a previously-silent
// no_commit self-explaining: operators see how long the session ran (elapsed)
// and when it last made progress (sinceProgress).
//
// Non-fatal: a nil bus or a marshal error is silently discarded; the kill itself
// is already surfaced via the reopen/done path.  elapsed/sinceProgress are
// clamped so the payload always validates (Valid requires ElapsedMS > 0,
// SinceLastProgressMS >= 0).
//
// Mirrors emitSpawnCapBlocked (hk-4l7zs).
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
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerBudgetExceeded, b)
}

// splashDismissWait sleeps for splashDismissDelay or until ctx is cancelled.
// Used after SendEnterToLastPane to give the Claude Code welcome splash time
// to animate away before the paste-buffer write arrives (hk-rf4ux).
func splashDismissWait(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(splashDismissDelay):
	}
}

// pasteInjectImplementerInitial delivers the task kick-off message for the
// implementer-initial (and single-mode) phase.
//
// Buffer purpose slug: "task" → buffer name "harmonik-<session-id>-task".
// Kick-off message: directs Claude to read .harmonik/agent-task.md.
//
// Splash-dismiss (hk-rf4ux): before writing the kick-off payload, an Enter
// keypress is sent via SendEnterToLastPane (tmux send-keys Enter, NOT -l
// literal) to dismiss the Claude Code welcome splash.  The splash is a
// React/ink TUI that processes key events; paste-buffer operates in
// bracketed-paste mode, meaning '\n' in the payload is NOT dispatched as an
// Enter key event.  The send-keys form bypasses bracketed-paste mode.
// A 750ms delay between Enter and paste allows the splash animation to
// complete and the REPL input state to activate before the message arrives.
//
// Returns a non-empty failure reason string when the paste-inject step could
// not complete (e.g. task file absent, WriteLastPane error).  The caller
// (pasteInjectOnLaunch) emits pasteinject_failed when the reason is non-empty.
// Returns "" on success.
func pasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string, runner tmux.CommandRunner) string {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFileVia(ctx, runner, taskFile); err != nil {
		reason := fmt.Sprintf("implementer-initial: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	// Dismiss the welcome splash with an Enter keypress before the paste (hk-rf4ux).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			// Non-fatal: log and proceed; the paste may still succeed if the
			// splash has already auto-dismissed.
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial SendEnterToLastPane: %v\n", err)
		}
		// Wait for splash to dismiss before delivering the paste.
		splashDismissWait(ctx)
	}

	bufName := bufferName(claudeSessID, "task")
	msg := "Please read .harmonik/agent-task.md and begin.\n"
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		reason := fmt.Sprintf("implementer-initial WriteLastPane: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
		return reason
	}
	// Settle after the paste before submitting (hk-76n5g, mirrors hk-jzpqo).
	//
	// The bracketed paste is still being absorbed by the TUI when the submit
	// Enter fires; all retry Enters (hk-ip33d: up to 3, over ~800 ms) can land
	// inside the absorption window and be swallowed, leaving the brief typed-
	// but-unsubmitted.  Waiting splashDismissDelay gives the REPL time to finish
	// absorbing the paste and return to an input-ready state before the first
	// retry Enter arrives.
	splashDismissWait(ctx)
	// Send Enter after paste to submit the message regardless of terminal
	// bracketed-paste mode (hk-8cq23).  Under a concurrent cold-boot the splash
	// can outlast the fixed splashDismissDelay, so a single submit Enter lands on
	// the still-up splash and is swallowed, leaving the brief unsubmitted
	// (hk-7rgqs).  Send the submit Enter with the same bounded retry the resume
	// path uses so at least one keypress lands after the splash clears.
	if es, ok := inj.(enterSender); ok {
		sendSubmitEnterWithRetry(ctx, es, "implementer-initial")
	}
	return ""
}

// pasteInjectImplementerResume delivers a single combined paste-inject message
// for the implementer-resume phase containing both the task instruction and the
// reviewer feedback for the prior iteration.
//
// Root cause of hk-poy7k: the previous two-message approach sent task+feedback
// back-to-back with no synchronization between the first Enter (task submit) and
// the second WriteLastPane (feedback). Claude was still processing the first
// message when the second Enter fired, so the feedback message was dropped and
// the resumed implementer reproduced the identical diff → no-progress failure.
//
// Fix (option b): combine task+feedback into a SINGLE paste buffer separated by
// a blank line, submitted with one Enter. One paste → zero inter-message race.
//
// If the feedback file is absent (first iteration or write failure), the message
// degrades gracefully to the task-only form used by implementer-initial.
//
// Returns a non-empty failure reason string when the paste-inject step could not
// complete.  Returns "" on success.
func pasteInjectImplementerResume(ctx context.Context, inj pasteInjecter, claudeSessID string, iterCount int, wtPath string, runner tmux.CommandRunner) string {
	// Dismiss the welcome splash first (hk-rf4ux) — same as implementer-initial.
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx)
	}

	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFileVia(ctx, runner, taskFile); err != nil {
		reason := fmt.Sprintf("implementer-resume task: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	// Build the combined message. Append the feedback section when the prior
	// iteration's feedback file exists. Both sections are delivered in a single
	// WriteLastPane call (one paste, one Enter) to eliminate the race (hk-poy7k).
	priorIter := iterCount - 1
	feedbackFile := filepath.Join(wtPath, ".harmonik", fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIter))
	feedbackExists := statTaskFileVia(ctx, runner, feedbackFile) == nil

	var msg string
	if feedbackExists {
		msg = fmt.Sprintf(
			"Please read .harmonik/agent-task.md and begin.\n\n"+
				"Before continuing, also read .harmonik/reviewer-feedback.iter-%d.md in your worktree."+
				" It contains the prior reviewer's verdict, flags, and notes for iteration %d."+
				" Address every flag marked REQUEST_CHANGES before proceeding.\n",
			priorIter, priorIter,
		)
	} else {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume feedback iter %d: not found (delivering task-only message)\n", priorIter)
		msg = "Please read .harmonik/agent-task.md and begin.\n"
	}

	bufName := bufferName(claudeSessID, "task")
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		reason := fmt.Sprintf("implementer-resume WriteLastPane: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
		return reason
	}
	// Settle after the paste before submitting (hk-76n5g).
	//
	// Root cause of the iter-2 not-submitted seed (observed in production on
	// 2026-06-10, even with the hk-ip33d retry fix in place): the TUI is still
	// absorbing the bracketed-paste content when the first retry Enter fires,
	// and ALL retry Enters (3 total, over ~800 ms) land within the absorption
	// window and are swallowed.  The brief then sits typed-but-unsubmitted;
	// the resumed implementer stays idle and the bead wedges until the 30-min
	// commitPollTimeout fires.  Waiting splashDismissDelay gives the REPL time
	// to finish absorbing the paste and return to an input-ready state, shifting
	// the first retry Enter to ~750 ms post-paste where the input handler is
	// reliably accepting keystrokes.  Mirrors the hk-jzpqo crew path fix.
	splashDismissWait(ctx)
	// Send Enter after paste to submit the message regardless of terminal
	// bracketed-paste mode (hk-8cq23).  On the resume path the freshly-resumed
	// REPL is intermittently not yet input-ready when this fires, so the single
	// Enter is dropped and the prompt sits unsubmitted → run_stale (hk-ip33d).
	// Send the submit Enter with a bounded retry so at least one keypress lands
	// after the input handler is ready; a redundant Enter at an already-submitted
	// REPL is a harmless no-op.
	if es, ok := inj.(enterSender); ok {
		sendResumeSubmitEnter(ctx, es)
	}
	return ""
}

// sendSubmitEnterWithRetry delivers the post-paste submit Enter with a bounded
// retry (hk-ip33d, generalised by hk-7rgqs).
//
// It sends Enter once, then re-sends it up to resumeSubmitRetries additional
// times with resumeSubmitRetryDelay between attempts.  The retries defend against
// the post-paste lost-Enter timing race where the REPL input handler is not yet
// ready to accept the first keypress — either because a fresh `--resume` TUI is
// still settling (hk-ip33d) or because the welcome splash is still up under a
// concurrent cold-boot whose animation overran the fixed splashDismissDelay
// (hk-7rgqs).  A dropped first Enter leaves the brief unsubmitted (claude idles →
// run_stale); a redundant Enter at an already-submitted REPL is a harmless empty
// line.  The loop returns early if ctx is cancelled.  phase is a short label used
// only in the diagnostic log line.
//
// Bead: hk-ip33d, hk-7rgqs.
func sendSubmitEnterWithRetry(ctx context.Context, es enterSender, phase string) {
	if err := es.SendEnterToLastPane(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s post-paste SendEnterToLastPane: %v\n", phase, err)
	}
	for i := 0; i < resumeSubmitRetries; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(resumeSubmitRetryDelay):
		}
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s submit-retry %d SendEnterToLastPane: %v\n", phase, i+1, err)
		}
	}
}

// sendResumeSubmitEnter is the implementer-resume call site of
// sendSubmitEnterWithRetry (hk-ip33d).  Retained as a named wrapper so the
// hk-ip33d resume path reads clearly and existing references stay stable.
func sendResumeSubmitEnter(ctx context.Context, es enterSender) {
	sendSubmitEnterWithRetry(ctx, es, "implementer-resume")
}

// pasteInjectReviewer delivers the reviewer kick-off message.
//
// Buffer purpose slug: "review" → buffer name "harmonik-<session-id>-review".
// Kick-off message: directs Claude to read .harmonik/review-target.md and
// produce the verdict file.
//
// Returns a non-empty failure reason string when the paste-inject step could not
// complete.  Returns "" on success.
func pasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string, runner tmux.CommandRunner) string {
	reviewFile := filepath.Join(wtPath, ".harmonik", "review-target.md")
	if err := statTaskFileVia(ctx, runner, reviewFile); err != nil {
		reason := fmt.Sprintf("reviewer: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	// Dismiss the welcome splash first (hk-rf4ux) — same as implementer-initial.
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx)
	}

	bufName := bufferName(claudeSessID, "review")
	msg := "Read .harmonik/review-target.md in this worktree." +
		" It contains the bead context, the diff range to review, and any prior-iteration verdicts." +
		" Produce your verdict by writing .harmonik/review.json conforming to the agent-reviewer schema v1." +
		" CRITICAL: when the bead body's '## Implementation Notes' section names exact field/struct names" +
		" (e.g. 'MUST be SessionID string — NOT SessID'), grep the diff for every named identifier and" +
		" verify the exact name appears. When a prior verdict has flag 'spec-field-name' or notes naming" +
		" a field-name violation, re-check that EXACT field name in the new diff before approving." +
		// hk-hay: all-X coverage check — reviewer must not approve a partial 'all-sites'/'all-X' change.
		" COVERAGE CHECK: if the bead title or body uses all-inclusive language ('all X', 'all sites'," +
		" 'every X', 'all callers', 'all handlers', 'all usages', etc.), grep the worktree for every" +
		" occurrence of the targeted pattern and verify each one appears in the diff. If any occurrence" +
		" is absent from the diff, emit flags: [\"incomplete-coverage\"] and REQUEST_CHANGES naming the" +
		" missed file paths and line numbers in notes — do NOT approve a partial 'all-X' change." +
		// hk-805f7: explicit read-only constraint — reviewer MUST NOT run git state-changing commands.
		" READ-ONLY CONSTRAINT: you MUST NOT run git reset, git checkout, git cherry-pick, git merge," +
		" git push, git rebase, or any other state-mutating git command. You are on a detached-HEAD" +
		" reviewer worktree; mutating git state can corrupt the implementer's task branch.\n"
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		reason := fmt.Sprintf("reviewer WriteLastPane: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
		return reason
	}
	// Send Enter after paste to submit the message regardless of terminal
	// bracketed-paste mode (hk-8cq23).  hk-7rgqs (the reviewer SEED-SUBMIT RACE):
	// under concurrent claude cold-boots the splash takes >750ms to clear, so the
	// fixed splashDismissDelay elapses while the splash is still up; a single
	// submit Enter lands on the splash and is SWALLOWED, leaving the review brief
	// typed-but-UNSUBMITTED — the reviewer idles, never reads review-target.md,
	// never writes review.json, and the run stalls until the verdict budget.
	// Send the submit Enter with the same bounded retry the resume path uses so at
	// least one keypress lands after the splash clears.  The safety net in
	// pasteInjectQuitOnReviewFile re-seeds once if even the retries lose the race.
	if es, ok := inj.(enterSender); ok {
		sendSubmitEnterWithRetry(ctx, es, "reviewer")
	}
	return ""
}

// reviewFileTimeout is the BASE (floor) window to wait for .harmonik/review.json
// to appear after the reviewer brief is delivered.  It is no longer the flat
// deadline it once was: the effective deadline is reviewFileTimeout plus a
// diff-size-scaled extension (see reviewBudgetForDiff), capped at
// reviewFileHardCeiling.  After the effective deadline elapses /quit is sent
// unconditionally and the session is force-killed.
//
// hk-sah87: the old behaviour was a FLAT 10-minute deadline.  On a heavy /
// large-diff bead the implementer commits fine (~10–13 min) but the reviewer
// claude — which must read the whole diff before it can write a verdict — was
// /quit+killed at the flat 10 min BEFORE it wrote .harmonik/review.json, so
// ReadReviewVerdict returned nil and the run false-failed as "verdict absent".
// The implementer phase, by contrast, has a 90-minute progress-aware ceiling
// (commitHardCeiling).  Scaling the reviewer budget by diff size — with a hard
// ceiling well below the implementer's — bounds heavy reviews without letting a
// hung reviewer (hk-m5axg: alive 31 min emitting zero events) run forever.
//
// Declared as var so tests can override.
//
// Bead: hk-zimkh, hk-sah87.
var reviewFileTimeout = 10 * time.Minute

// reviewFileHardCeiling is the absolute upper bound on the reviewer-verdict
// wait, regardless of diff size.  A hung-at-empty-prompt reviewer keeps its
// claude pane alive (so pane-liveness alone cannot distinguish it from a working
// reviewer); this ceiling is the firm backstop that bounds it.  It is set well
// below the implementer's commitHardCeiling (90 min) because a review is
// read-only — it has no test loops or multi-file edits to run.
//
// hk-60t8: raised from 30 to 40 minutes — opus/high reviewers on non-trivial
// DOT-mode cascade runs were killed at exactly the budget deadline (20 min for
// a ~2000-line diff) because the hard ceiling left no headroom for the
// heartbeat-based extension added in the same bead.  40 min gives those
// reviewers sufficient runway while still bounding a genuinely hung session.
//
// hk-4p2h: raised from 40 to 60 minutes — coupled with the reviewFilePerKLineBudget
// increase (5→10 min/kline), a 2000-line diff now gets a 30-min base budget with
// room for up to three 10-min heartbeat-based extensions before hitting the ceiling.
// The 60-min ceiling remains well below the 90-min implementer hard ceiling.
//
// Declared as var so tests can override.
//
// Bead: hk-sah87, hk-60t8, hk-4p2h.
var reviewFileHardCeiling = 60 * time.Minute

// reviewerHeartbeatActiveGrace is the window after the most-recent
// agent_heartbeat within which the reviewer is considered "actively reasoning"
// for the purposes of the budget-extension check.  Set to twice the heartbeat
// interval (2×5 min = 10 min) so a single missed heartbeat tick does not
// trigger a premature kill.
//
// hk-60t8: the paul canary (run 019ed1ad-77a3) was killed at EXACTLY 20 min
// while the opus/high reviewer was still heartbeating (4×5-min beats observed).
// PaneHasActiveProcess correctly returned true, but the extension only fires
// when livenessChecker is non-nil.  Adding a heartbeat-based extension
// (liveness OR recent heartbeat) ensures an actively-reasoning reviewer is
// never killed while it is still emitting progress signals.
//
// Declared as var so tests can override.
//
// Bead: hk-60t8.
var reviewerHeartbeatActiveGrace = 10 * time.Minute

// reviewFilePerKLineBudget is the extra wait granted per 1000 changed lines in
// the diff under review, added on top of reviewFileTimeout.  10 minutes/1000
// lines means a 2000-line diff gets 10 (base) + 20 = 30 min, a 4000-line diff
// gets 10 + 40 = 50 min (capped by reviewFileHardCeiling).  A small diff stays
// at the 10-minute base.
//
// hk-4p2h: raised from 5 to 10 min/kline — the paul canary (run 019ed1ad-77a3,
// hk-t1wd) had its opus/high reviewer killed at EXACTLY 20:00 (= 10+10 min for
// a ~2000-line diff) because the 5 min/kline budget was too tight for an opus
// reviewer to finish reading and reasoning over a large diff.  10 min/kline
// gives a 2000-line diff a 30-min base budget, preventing the premature kill
// without the reviewer needing the heartbeat-based extension as a crutch.
//
// Declared as var so tests can override.
//
// Bead: hk-sah87, hk-4p2h.
var reviewFilePerKLineBudget = 10 * time.Minute

// reviewFilePollInterval is how often to check for the review verdict file.
var reviewFilePollInterval = 2 * time.Second

// reviewerReseedGrace is the short window pasteInjectQuitOnReviewFile waits for
// .harmonik/review.json to appear before it RE-SEEDS the reviewer brief once
// (hk-7rgqs safety net).  If no verdict has appeared within this grace AND the
// pane still hosts an active claude process, the brief was almost certainly typed
// but never submitted (the splash swallowed the submit Enter under a concurrent
// cold-boot — see pasteInjectReviewer), so we re-run the splash-dismiss +
// paste-brief + submit-Enter sequence once.  This mirrors the implementer path's
// activity-aware re-detection: a reviewer that already submitted writes review.json
// well within the budget and never reaches the re-seed; a wedged-at-unsubmitted
// reviewer is recovered without waiting out the full diff-scaled verdict budget.
//
// 75s is long enough that a reviewer which DID submit has begun reading the diff
// (no spurious re-seed) yet short enough to recover a stalled reviewer minutes
// before the 10-minute base budget would otherwise fire.
//
// Declared as var (not const) so tests can override it without waiting real wall
// time.
//
// Bead: hk-7rgqs.
var reviewerReseedGrace = 75 * time.Second

// reviewBudgetForDiff computes the effective reviewer-verdict wait for a diff of
// changedLines: the base timeout plus reviewFilePerKLineBudget per 1000 changed
// lines, clamped to [base, reviewFileHardCeiling].  A negative changedLines
// (diff size unknown / measurement failed) yields the base timeout — the
// conservative pre-hk-sah87 behaviour.
//
// Snapshots of the package vars are passed in so the caller can read them once
// (avoiding a race with tests that restore the vars after the run returns).
//
// Bead: hk-sah87.
func reviewBudgetForDiff(changedLines int, base, perKLine, ceiling time.Duration) time.Duration {
	if changedLines <= 0 {
		// Unknown or empty diff → base budget.
		if base > ceiling {
			return ceiling
		}
		return base
	}
	// Scale linearly: perKLine for every 1000 changed lines (integer-safe via
	// nanosecond arithmetic so a sub-1000-line diff still earns a proportional
	// slice).
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

// worktreeDiffLineCount returns the number of changed lines (added + deleted)
// across the commits unique to the worktree branch at wtPath, measured against
// the repository's default branch (origin/HEAD, falling back to origin/main).
// It is the diff the reviewer must read, so it drives the reviewer's verdict
// budget (reviewBudgetForDiff).
//
// Returns -1 when the count cannot be determined (any git error, no remote-
// tracking ref, detached state) — callers treat -1 as "use the base budget".
// Best-effort and side-effect-free: it only runs read-only git plumbing.
//
// Bead: hk-sah87.
func worktreeDiffLineCount(ctx context.Context, wtPath string) int {
	// Try refs in order; the three-dot form diffs against the merge-base so a
	// stale default-branch tip does not inflate the count with unrelated work.
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

// sumNumstatLines parses `git diff --numstat` output and returns the sum of
// added + deleted line counts.  Binary files (numstat emits "-\t-\t<path>") are
// skipped.  The ok result is false only when no parseable data row was seen
// AND the output was non-empty in a way that suggests a parse problem; an empty
// diff (no rows) returns (0, true) — a genuinely empty diff is a valid result.
//
// Bead: hk-sah87.
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
			// Binary file ("-\t-") or unparseable row — skip but count as data.
			sawRow = true
			continue
		}
		total += added + deleted
		sawRow = true
	}
	// Empty diff (no rows at all) is a valid zero result.
	if !sawRow {
		return 0, true
	}
	return total, true
}

// worktreeActivityFingerprint returns a string that changes whenever the
// implementer makes demonstrable progress in its worktree WITHOUT yet having
// committed.  It combines three signals so that BOTH tracked-file edits and
// brand-new untracked files (including content churn of an untracked file, which
// `git status` alone cannot see) move the fingerprint:
//
//   - HEAD — so a commit (the success case) always changes the fingerprint;
//   - `git status --porcelain=v1` — the SET of working-tree changes (added,
//     modified, deleted, untracked PATHS); and
//   - a size+mtime signature over every changed/untracked file's bytes, so that
//     ongoing edits to a file already present in the porcelain set (e.g. an
//     untracked draft claude keeps growing) still advance the fingerprint.
//
// An implementer that is actively editing files (the false-kill scenario at the
// 12-minute launch-suppression ceiling) produces a fingerprint that advances
// from one check to the next; a genuinely-wedged pane (active child but doing
// nothing, e.g. an idle claude whose heartbeats were drained by a competing
// tapCh reader under concurrency — the hk-jgxqc wedge) produces a STABLE
// fingerprint, so the launch-suppression ceiling still fires for it.
//
// Returns ("", false) on any git error so callers treat unknown as "no
// observable progress" (conservative — the ceiling kill is allowed to proceed).
//
// Bead: hk-az4fd.
func worktreeActivityFingerprint(ctx context.Context, wtPath string) (string, bool) {
	head, err := resolveWorktreeHEAD(ctx, wtPath)
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
	// Stat each changed/untracked path so content churn of files already in the
	// porcelain set (notably untracked files, whose bytes git does not track)
	// advances the fingerprint.  porcelain v1 rows are "XY <path>"; the path
	// begins at byte offset 3.  Renames ("R  old -> new") and quoted paths are
	// not stat-able as-is — they are skipped (the path-set change in `out`
	// already captures them).
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

// reviewerBudgetSentinelName is the basename of the marker file
// pasteInjectQuitOnReviewFile writes into <wtPath>/.harmonik/ when it force-kills
// a reviewer for exceeding its (diff-scaled) verdict budget.  Its presence lets
// the caller distinguish a BUDGET kill (the reviewer was working but ran out of
// time on a heavy diff) from a true no-verdict (reviewer produced nothing),
// turning the previously-generic "verdict absent at iteration N" into a
// self-explaining "reviewer budget exceeded" diagnostic — see the reviewloop
// verdict-absent branch (reviewloop.go) and dot_cascade's reviewer-node path.
//
// Bead: hk-sah87.
const reviewerBudgetSentinelName = "reviewer-budget-exceeded.json"

// reviewerBudgetSentinel is the JSON shape written to the budget-kill marker
// file.  It mirrors the fields of core.ImplementerBudgetExceededPayload so an
// operator (or a future bead that registers a reviewer_budget_exceeded event
// type and emits it from the caller) has the same diagnostic surface.
//
// Bead: hk-sah87.
type reviewerBudgetSentinel struct {
	BudgetMS     int64  `json:"budget_ms"`
	ChangedLines int    `json:"changed_lines"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	Reason       string `json:"reason"`
}

// reviewerBudgetSentinelPath returns the absolute path of the budget-kill marker
// for the worktree at wtPath.
func reviewerBudgetSentinelPath(wtPath string) string {
	return filepath.Join(wtPath, ".harmonik", reviewerBudgetSentinelName)
}

// writeReviewerBudgetSentinel best-effort writes the budget-kill marker file so
// the caller can emit a distinct "reviewer budget exceeded" diagnostic instead
// of the generic "verdict absent".  Errors are logged and swallowed: the marker
// is observability, not correctness — the kill still fires regardless.
//
// Bead: hk-sah87.
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
// Exported (capitalized) so both the builtin review-loop (reviewloop.go) and the
// DOT reviewer-node path (dot_cascade.go) can consult it without changing the
// pasteInjectQuitOnReviewFile signature.
//
// Bead: hk-sah87.
func ReadReviewerBudgetSentinel(wtPath string) (*reviewerBudgetSentinel, error) {
	path := reviewerBudgetSentinelPath(wtPath)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("daemon: ReadReviewerBudgetSentinel: read %s: %w", path, err)
	}
	var pl reviewerBudgetSentinel
	if uErr := json.Unmarshal(b, &pl); uErr != nil {
		return nil, fmt.Errorf("daemon: ReadReviewerBudgetSentinel: unmarshal %s: %w", path, uErr)
	}
	return &pl, nil
}

// pasteInjectQuitOnReviewFile watches for <wtPath>/.harmonik/review.json to
// appear (indicating the reviewer has written its verdict), then sends /quit
// to terminate the reviewer session. Without this, the reviewer claude sits
// idle at a prompt after writing the verdict, blocking the daemon indefinitely.
//
// Mirrors pasteInjectQuitOnCommit for the implementer phase, but watches for
// a file instead of a git commit.
//
// hk-sah87: the verdict-wait deadline is no longer a FLAT reviewFileTimeout.
// It is now diff-size-scaled — reviewBudgetForDiff(reviewFileTimeout +
// reviewFilePerKLineBudget × changed-kLOC, capped at reviewFileHardCeiling) —
// because a heavy/large-diff bead's reviewer needs longer than 10 min just to
// read the diff before it can write a verdict.  The flat 10-min deadline was
// /quit+killing such reviewers BEFORE they wrote review.json, false-failing the
// run as "verdict absent".  A pane-liveness check (mirroring the implementer
// path) suppresses a kill that would land while the reviewer is still actively
// working, but the hard ceiling (reviewFileHardCeiling, well below the
// implementer's 90-min commitHardCeiling) is the firm backstop so a hung
// reviewer (hk-m5axg: alive 31 min emitting zero events) cannot run forever.
// On a budget kill a marker file is written (writeReviewerBudgetSentinel) so the
// caller emits a distinct "reviewer budget exceeded" diagnostic.
//
// hk-7rgqs (reviewer SEED-SUBMIT RACE safety net): before the budget logic can
// help, a one-shot re-seed recovers the common stall where the reviewer brief was
// typed but never SUBMITTED (the splash swallowed the submit Enter under a
// concurrent cold-boot).  If no review.json has appeared within reviewerReseedGrace
// AND the pane still hosts an active claude process (so we are not re-seeding a
// dead pane), the reviewer kick-off (splash-dismiss + paste-brief + bounded submit
// Enter) is re-run ONCE via pasteInjectReviewer, then the loop continues.  inj and
// claudeSessID are the pasteInjecter and claude session id for this reviewer's
// pane (the same pair pasteInjectOnLaunch used to deliver the brief); when inj is
// nil (the deterministic test path / a non-pasteInjecter substrate) the re-seed is
// skipped and only the budget logic applies.
//
// Bead: hk-zimkh, hk-sah87, hk-7rgqs.
// pasteInjectQuitOnReviewFile watches for <wtPath>/.harmonik/review.json to
// appear (indicating the reviewer has written its verdict), then sends /quit
// to terminate the reviewer session. Without this, the reviewer claude sits
// idle at a prompt after writing the verdict, blocking the daemon indefinitely.
//
// hk-60t8: two new parameters extend the reviewer liveness logic:
//   - eventCh: an independent per-run event tap subscriber (nil = no heartbeat
//     tracking). When a recent agent_heartbeat is observed, the reviewer is
//     considered "actively reasoning" and the budget is extended (same semantics
//     as the pane-liveness extension, but keyed on heartbeat rather than OS
//     process state — more reliable under concurrent dispatch).
//   - overrideCeiling: when > 0 overrides reviewFileHardCeiling for this node
//     only (matches the DOT timeout= attribute from the node graph).  This lets
//     DOT authors author per-node reviewer timeouts for opus/high nodes that
//     legitimately need more time than the default ceiling.
func pasteInjectQuitOnReviewFile(
	ctx context.Context,
	qs quitSender,
	killer sessionKiller,
	inj pasteInjecter,
	claudeSessID string,
	wtPath string,
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope, // hk-60t8: heartbeat tracking; nil = disabled
	overrideCeiling time.Duration, // hk-60t8: 0 = use reviewFileHardCeiling
) {
	if briefDelivered != nil {
		bdTimeout := briefDeliveredTimeout
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
		case <-time.After(bdTimeout):
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-review-file: brief_delivered timeout after %v for %s; proceeding\n",
				bdTimeout, wtPath)
		}
	}

	verdictPath := filepath.Join(wtPath, ".harmonik", "review.json")
	pollInterval := reviewFilePollInterval
	killDelay := noChangeKillDelay

	// hk-60t8: resolve the effective hard ceiling.  A per-node overrideCeiling
	// (from the DOT timeout= attribute) takes precedence over the package default.
	effectiveCeiling := reviewFileHardCeiling
	if overrideCeiling > 0 {
		effectiveCeiling = overrideCeiling
	}

	// hk-sah87: size the verdict budget by the diff the reviewer must read.
	// worktreeDiffLineCount returns -1 on any measurement failure → base budget.
	changedLines := worktreeDiffLineCount(ctx, wtPath)
	budget := reviewBudgetForDiff(changedLines, reviewFileTimeout, reviewFilePerKLineBudget, effectiveCeiling)
	fmt.Fprintf(os.Stderr,
		"daemon: pasteinject: quit-on-review-file: verdict budget %v for %s (changed_lines=%d, ceiling=%v)\n",
		budget, wtPath, changedLines, effectiveCeiling)

	loopStart := time.Now()
	deadline := loopStart.Add(budget)
	// hk-sah87: optional pane-liveness checker (same interface the implementer
	// path uses).  When present, a deadline that lands while the reviewer pane
	// still has an active child process is extended by one base window rather
	// than killing a reviewer that is genuinely still reading the diff — but the
	// extension is itself bounded by the absolute hard ceiling below.
	livenessChecker, _ := qs.(paneLivenessChecker)
	hardDeadline := loopStart.Add(effectiveCeiling)

	// hk-60t8: track the most-recent agent_heartbeat time.  When a heartbeat
	// arrived within reviewerHeartbeatActiveGrace the reviewer is considered
	// actively reasoning; the budget is extended even when pane-liveness is
	// unavailable (livenessChecker == nil) or the OS-level probe misses the
	// process.  Initialized to zero (never seen) so the first check on an
	// unlaunched reviewer never spuriously extends.
	var lastHeartbeatAt time.Time
	heartbeatActiveGrace := reviewerHeartbeatActiveGrace

	// hk-7rgqs (reviewer SEED-SUBMIT RACE safety net): fire a one-shot re-seed of
	// the reviewer brief if no verdict appears within reviewerReseedGrace and the
	// pane is still active (brief typed but submit Enter swallowed by the splash).
	// reseedDeadline is the absolute instant the grace elapses; reseeded guards the
	// once-only semantics.  Disabled when inj is nil (no pasteInjecter to re-seed
	// through, e.g. the deterministic test path).
	reseedDeadline := loopStart.Add(reviewerReseedGrace)
	reseeded := inj == nil

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case env, ok := <-eventCh:
			// eventCh is nil-safe: a nil channel blocks forever, so this case is
			// never selected when eventCh is nil.  Drain agent_heartbeat events
			// promptly so lastHeartbeatAt stays current between ticker ticks.
			if !ok {
				eventCh = nil
				continue
			}
			if core.EventType(env.Type) == core.EventTypeAgentHeartbeat {
				lastHeartbeatAt = time.Now()
			}

		case <-ticker.C:
			now := time.Now()

			// hk-60t8: drain any heartbeats buffered in eventCh since the last
			// tick so that lastHeartbeatAt reflects all progress that has arrived
			// between ticks (mirrors the hk-ukx drain in pasteInjectQuitOnCommit).
			if eventCh != nil {
			drainReviewerHB:
				for {
					select {
					case env, ok := <-eventCh:
						if !ok {
							eventCh = nil
							break drainReviewerHB
						}
						if core.EventType(env.Type) == core.EventTypeAgentHeartbeat {
							lastHeartbeatAt = now
						}
					default:
						break drainReviewerHB
					}
				}
			}

			// hk-7rgqs: one-shot re-seed BEFORE the budget/verdict checks.  When
			// the grace has elapsed, no verdict file exists yet, and the pane still
			// hosts an active claude process, re-run the reviewer kick-off once: the
			// brief was almost certainly typed but never submitted (splash swallowed
			// the submit Enter).  A pane with NO active process is left to the budget
			// path (re-seeding a dead pane cannot help).  When liveness is
			// unobservable (livenessChecker == nil) we still re-seed once on the
			// grace, since the original submit may simply have been dropped — a
			// redundant brief at an already-working reviewer is a harmless extra
			// prompt it ignores.
			if !reseeded && now.After(reseedDeadline) {
				if _, err := os.Stat(verdictPath); err == nil {
					// Verdict already present — no re-seed needed.
					reseeded = true
				} else if livenessChecker == nil || livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: no verdict after %v re-seed grace and pane active in %s; re-seeding reviewer brief once (hk-7rgqs)\n",
						reviewerReseedGrace, wtPath)
					// Extract runner for remote-aware stat probe (hk-hh5e): inj is a
					// perRunSubstrate which also implements commandRunnerProvider.
					var reseedRunner tmux.CommandRunner
					if crp, ok2 := inj.(commandRunnerProvider); ok2 {
						if r := crp.commandRunner(); !runnerIsLocalFS(r) {
							reseedRunner = r
						}
					}
					if reason := pasteInjectReviewer(ctx, inj, claudeSessID, wtPath, reseedRunner); reason != "" {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-review-file: re-seed failed for %s: %s\n",
							wtPath, reason)
					}
					reseeded = true
				}
			}

			if now.After(deadline) {
				// hk-sah87 / hk-60t8: before killing, check whether the reviewer
				// is still actively working.  Two independent signals qualify:
				//   1. Pane liveness: the OS-level probe detects an active child
				//      process in the tmux pane (hk-sah87).
				//   2. Recent heartbeat: an agent_heartbeat arrived within
				//      reviewerHeartbeatActiveGrace, indicating active reasoning
				//      at the LLM level — more reliable than pane-liveness under
				//      concurrent dispatch (hk-60t8).
				// When either signal fires AND we have not yet reached the absolute
				// hard ceiling, extend the budget by one base window.
				recentHB := !lastHeartbeatAt.IsZero() && now.Sub(lastHeartbeatAt) < heartbeatActiveGrace
				paneActive := livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx)
				if (paneActive || recentHB) && now.Before(hardDeadline) {
					signal := "pane-active"
					if recentHB && !paneActive {
						signal = "heartbeat"
					} else if recentHB {
						signal = "pane-active+heartbeat"
					}
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: budget %v elapsed but reviewer active (%s) in %s; extending (hard ceiling %v) [hk-60t8]\n",
						budget, signal, wtPath, effectiveCeiling)
					deadline = now.Add(reviewFileTimeout)
					if deadline.After(hardDeadline) {
						deadline = hardDeadline
					}
					continue
				}
				reason := "budget-exceeded"
				if !now.Before(hardDeadline) {
					reason = "hard-ceiling"
				}
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-review-file: %s after %v waiting for %s (budget=%v, changed_lines=%d); sending /quit\n",
					reason, time.Since(loopStart), verdictPath, budget, changedLines)
				// hk-sah87: write the budget-kill marker so the caller can emit a
				// distinct "reviewer budget exceeded" diagnostic instead of the
				// generic "verdict absent".
				writeReviewerBudgetSentinel(wtPath, budget, changedLines, time.Since(loopStart), reason)
				_ = qs.SendQuitToLastPane(ctx)
				select {
				case <-ctx.Done():
				case <-time.After(killDelay):
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}

			if info, err := os.Stat(verdictPath); err == nil && info.Size() > 0 {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-review-file: verdict detected at %s; sending /quit\n",
					verdictPath)
				_ = qs.SendQuitToLastPane(ctx)
				// Grace period for claude to process /quit before force-kill.
				select {
				case <-ctx.Done():
				case <-time.After(postQuitKillGrace):
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}
		}
	}
}

// bufferName constructs the PL-021d buffer name "harmonik-<sessionID>-<purpose>".
//
// The sessionID component is the claudeSessionID for the current launch;
// purpose is a short lowercase slug ("task", "feedback", "review").
func bufferName(sessionID, purpose string) string {
	return fmt.Sprintf("harmonik-%s-%s", sessionID, purpose)
}

// statTaskFile checks that path exists and is a non-empty regular file.
// Returns [tmux.ErrStructural] when the file is absent or empty.
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

// statTaskFileVia is like statTaskFile but routes the existence check through
// runner for remote runs (hk-hh5e).  When runner is nil, delegates to
// statTaskFile (local os.Stat).  When runner is non-nil (e.g. an SSHRunner
// targeting a remote worker), stat is executed on the worker via
// runner.Command(ctx, "stat", path).
//
// File-content emptiness is NOT re-checked for the remote path: WriteAgentTaskVia
// validates non-empty content at write time, so existence implies non-empty for
// runner-managed runs.
//
// Bead: hk-hh5e.
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
