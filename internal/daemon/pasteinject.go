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
//   - implementer-resume                 → "-task" (task instruction) then "-feedback" (reviewer notes)
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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// Bead: hk-rf4ux.
const splashDismissDelay = 750 * time.Millisecond

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
//   1. Empty pane — paste delivered but claude never started → kill fast (~60s).
//   2. Active thinking — claude is running and downloading tokens but has not
//      yet made a tool call → do NOT kill.
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
var livePaneCommandSubstrings = []string{"claude", "node"}

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
	return commandMatchesLiveAgent(pid)
}

// hasAnyDirectChild reports whether pid has at least one direct child process,
// via `pgrep -P <pid>` (exit-0 ⇒ at least one match).
func hasAnyDirectChild(pid int) bool {
	return exec.Command("pgrep", "-P", fmt.Sprintf("%d", pid)).Run() == nil
}

// commandMatchesLiveAgent reports whether the command name of pid contains one
// of livePaneCommandSubstrings (e.g. "claude" or its "node" runtime).  Uses
// `ps -o comm= -p <pid>`, which is available on macOS and mainstream Linux.
// Returns false on any error or empty output (conservative).
func commandMatchesLiveAgent(pid int) bool {
	out, err := exec.Command("ps", "-o", "comm=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false
	}
	comm := strings.ToLower(strings.TrimSpace(string(out)))
	if comm == "" {
		return false
	}
	for _, frag := range livePaneCommandSubstrings {
		if strings.Contains(comm, frag) {
			return true
		}
	}
	return false
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

// commitPollTimeout is the maximum total wall-clock time pasteInjectQuitOnCommit
// will wait for a new commit before giving up.  This is a safety backstop only;
// the primary kill trigger is heartbeat staleness (heartbeatStalenessThreshold).
// Raised from 10 min to 30 min to give productive long-running beads more room.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
var commitPollTimeout = 30 * time.Minute

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
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Beads: hk-cmybm, hk-trjef, hk-5s7tg, hk-930o3, hk-7srrd.
func pasteInjectQuitOnCommit(
	ctx context.Context,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
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

	totalDeadline := time.Now().Add(pollTimeout)
	lastHeartbeat := time.Now() // initialised to now; first real beat resets it
	heartbeatProvided := eventCh != nil
	// hk-3gq0b: launch-verification window — starts after brief delivery.
	// When heartbeatProvided, the first heartbeat must arrive within launchWindow
	// or the session is killed (paste likely landed in an empty pane).
	launchDeadline := time.Now().Add(launchWindow)
	firstHeartbeatSeen := false

	// hk-fbydv: optional pane liveness checker — probed once; nil when qs does
	// not implement paneLivenessChecker (e.g. test stubs, nil substrate path).
	livenessChecker, _ := qs.(paneLivenessChecker)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// fireNoChangePath sends /quit, waits killDelay, kills, and closes
	// noChangeTimeoutCh.  Extracted to avoid duplication between the heartbeat-
	// stale and total-deadline paths.
	fireNoChangePath := func(reason string) {
		fmt.Fprintf(os.Stderr,
			"daemon: pasteinject: quit-on-commit: %s in %s (initial=%s); sending /quit unconditionally\n",
			reason, wtPath, initialSHA)
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
				lastHeartbeat = time.Now()
				firstHeartbeatSeen = true
			}

		case <-ticker.C:
			now := time.Now()

			// Check total wall-clock backstop first.
			if now.After(totalDeadline) {
				fireNoChangePath("total-timeout waiting for new commit")
				return
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
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: launch-heartbeat-timeout suppressed: pane has active child process in %s; resetting staleness clock\n",
						wtPath)
					lastHeartbeat = now
					launchDeadline = now.Add(launchWindow)
				} else {
					fireNoChangePath("launch-heartbeat-timeout: no heartbeat within launch window after brief delivery")
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
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: heartbeat-staleness suppressed: pane has active child process in %s; resetting staleness clock\n",
						wtPath)
					lastHeartbeat = now
				} else {
					fireNoChangePath(fmt.Sprintf(
						"heartbeat stale for >%v (no agent_heartbeat received)",
						stalenessThreshold,
					))
					return
				}
			}

			headSHA, err := resolveWorktreeHEAD(ctx, wtPath)
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
//
// Returns a channel that is closed once the kick-off paste has been written
// (or immediately when no paste is performed because the substrate does not
// implement pasteInjecter).  The caller passes this channel to
// pasteInjectQuitOnCommit as the briefDelivered gate (hk-930o3).
//
// Errors are non-fatal to the caller: a failed paste-inject is logged to
// stderr but does not reopen the bead.  The operator may manually trigger a
// paste using tmux.
func pasteInjectOnLaunch(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
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

		switch phase {
		case handlercontract.ReviewLoopPhaseReviewer:
			pasteInjectReviewer(ctx, inj, claudeSessID, wtPath)

		case handlercontract.ReviewLoopPhaseImplementerResume:
			pasteInjectImplementerResume(ctx, inj, claudeSessID, iterCount, wtPath)

		default:
			// Single-mode or implementer-initial: deliver agent-task.md kick-off.
			pasteInjectImplementerInitial(ctx, inj, claudeSessID, wtPath)
		}
	}()
	return ch
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
func pasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial: %v (skipping inject)\n", err)
		return
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
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial WriteLastPane: %v\n", err)
	}
	// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial post-paste SendEnterToLastPane: %v\n", err)
		}
	}
}

// pasteInjectImplementerResume delivers two paste-inject messages for the
// implementer-resume phase:
//  1. Task instruction (agent-task.md) — same as initial.
//  2. Reviewer-feedback instruction (reviewer-feedback.iter-<N-1>.md).
//
// Both files must exist; if either is missing the inject for that file is
// skipped with a stderr log (non-fatal).
func pasteInjectImplementerResume(ctx context.Context, inj pasteInjecter, claudeSessID string, iterCount int, wtPath string) {
	// Inject 1: task file.
	// Dismiss the welcome splash first (hk-rf4ux) — same as implementer-initial.
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx)
	}

	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume task: %v (skipping task inject)\n", err)
	} else {
		bufName := bufferName(claudeSessID, "task")
		msg := "Please read .harmonik/agent-task.md and begin.\n"
		if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume WriteLastPane(task): %v\n", err)
		}
		// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
		if es, ok := inj.(enterSender); ok {
			if err := es.SendEnterToLastPane(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume post-paste SendEnterToLastPane(task): %v\n", err)
			}
		}
	}

	// Inject 2: reviewer feedback for prior iteration (N-1).
	priorIter := iterCount - 1
	feedbackFile := filepath.Join(wtPath, ".harmonik", fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIter))
	if err := statTaskFile(feedbackFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume feedback iter %d: %v (skipping feedback inject)\n", priorIter, err)
		return
	}
	bufName := bufferName(claudeSessID, "feedback")
	msg := fmt.Sprintf(
		"Before continuing, read .harmonik/reviewer-feedback.iter-%d.md in your worktree."+
			" It contains the prior reviewer's verdict, flags, and notes for iteration %d."+
			" Address every flag marked REQUEST_CHANGES before proceeding.\n",
		priorIter, priorIter,
	)
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume WriteLastPane(feedback): %v\n", err)
	}
	// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume post-paste SendEnterToLastPane(feedback): %v\n", err)
		}
	}
}

// pasteInjectReviewer delivers the reviewer kick-off message.
//
// Buffer purpose slug: "review" → buffer name "harmonik-<session-id>-review".
// Kick-off message: directs Claude to read .harmonik/review-target.md and
// produce the verdict file.
func pasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) {
	reviewFile := filepath.Join(wtPath, ".harmonik", "review-target.md")
	if err := statTaskFile(reviewFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer: %v (skipping inject)\n", err)
		return
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
		" Produce your verdict by writing .harmonik/review.json conforming to the agent-reviewer schema v1.\n"
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer WriteLastPane: %v\n", err)
	}
	// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer post-paste SendEnterToLastPane: %v\n", err)
		}
	}
}

// reviewFileTimeout is the maximum time to wait for .harmonik/review.json to
// appear after the reviewer brief is delivered. After this, /quit is sent
// unconditionally.
//
// Declared as var so tests can override.
//
// Bead: hk-zimkh.
var reviewFileTimeout = 10 * time.Minute

// reviewFilePollInterval is how often to check for the review verdict file.
var reviewFilePollInterval = 2 * time.Second

// pasteInjectQuitOnReviewFile watches for <wtPath>/.harmonik/review.json to
// appear (indicating the reviewer has written its verdict), then sends /quit
// to terminate the reviewer session. Without this, the reviewer claude sits
// idle at a prompt after writing the verdict, blocking the daemon indefinitely.
//
// Mirrors pasteInjectQuitOnCommit for the implementer phase, but watches for
// a file instead of a git commit.
//
// Bead: hk-zimkh.
func pasteInjectQuitOnReviewFile(
	ctx context.Context,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	briefDelivered <-chan struct{},
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
	timeout := reviewFileTimeout
	pollInterval := reviewFilePollInterval
	killDelay := noChangeKillDelay

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-review-file: timeout waiting for %s; sending /quit\n",
					verdictPath)
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
