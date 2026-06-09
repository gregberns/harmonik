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
// Bead: hk-rf4ux.
const splashDismissDelay = 750 * time.Millisecond

// resumeSubmitRetries and resumeSubmitRetryDelay govern the bounded submit-retry
// on the implementer-resume (iteration ≥ 2) paste path (hk-ip33d).
//
// Root cause: on a `claude --resume <session-id>` reattach the REPL's input
// handler is intermittently not yet ready to accept the Enter keypress at the
// instant the post-paste SendEnterToLastPane fires — the freshly-resumed TUI is
// still settling after the welcome splash.  The single Enter is dropped, the
// combined task+feedback prompt sits in the input bar unsubmitted, claude stays
// idle, and the run goes run_stale with no iteration-2 progress.  Confirmed in
// production: a manual `tmux send-keys -t <pane> Enter` submitted the prompt and
// iteration 2 began immediately.  This is a residual timing race left over from
// the hk-poy7k combined-paste fix.
//
// There is no pane-capture primitive on tmux.Adapter to detect "input cleared",
// so we cannot positively confirm submission.  Instead we send the submit Enter,
// wait a short settle, and re-send it up to resumeSubmitRetries additional times.
// A redundant Enter at a REPL that has ALREADY submitted is a harmless no-op
// (an empty line at the now-clear prompt), so the retries only ever help: at
// least one of them lands after the input handler is ready.  This reuses the
// same send-keys-Enter key-event idiom as the splash-dismiss path (hk-rf4ux) and
// the time-grace patterns already in this file.
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

	// hk-fbydv: optional pane liveness checker — probed once; nil when qs does
	// not implement paneLivenessChecker (e.g. test stubs, nil substrate path).
	livenessChecker, _ := qs.(paneLivenessChecker)

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
			// elapsed AND the pane is not making progress.  If the pane still has
			// an active child process, treat it as progress-without-heartbeat
			// (e.g. a long go-test loop that emits no agent_heartbeat): extend the
			// budget rather than guillotine a working session.  A pane that has
			// genuinely gone dark falls through to the kill.
			if now.After(totalDeadline) {
				if livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: commit-budget %v elapsed but pane has active child process in %s; extending budget (hard ceiling %v)\n",
						pollTimeout, wtPath, hardCeiling)
					lastProgress = now
					totalDeadline = now.Add(pollTimeout)
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
				// hk-jgxqc: the active-pane suppression is bounded by an absolute
				// ceiling (launchSuppressDeadline, NEVER extended).  Past that
				// ceiling the suppression is no longer permitted: a pane that has
				// reported "active child process" for the entire launch-suppression
				// window without ever emitting a heartbeat OR committing is wedged
				// (e.g. an idle claude whose heartbeats were consumed by a competing
				// tapCh reader under concurrency), so we MUST fire the kill so
				// sess.Wait unblocks and the workflow advances.  Within the ceiling
				// the legitimate launch-phase suppression is preserved unchanged.
				if now.Before(launchSuppressDeadline) &&
					livenessChecker != nil && livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: launch-heartbeat-timeout suppressed: pane has active child process in %s; resetting staleness clock\n",
						wtPath)
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
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: heartbeat-staleness suppressed: pane has active child process in %s; resetting staleness clock\n",
						wtPath)
					lastHeartbeat = now
				} else {
					fireNoChangePath(fmt.Sprintf(
						"heartbeat stale for >%v (no agent_heartbeat received)",
						stalenessThreshold,
					), "heartbeat-stale", true)
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

		var failReason string
		switch phase {
		case handlercontract.ReviewLoopPhaseReviewer:
			failReason = pasteInjectReviewer(ctx, inj, claudeSessID, wtPath)

		case handlercontract.ReviewLoopPhaseImplementerResume:
			failReason = pasteInjectImplementerResume(ctx, inj, claudeSessID, iterCount, wtPath)

		default:
			// Single-mode or implementer-initial: deliver agent-task.md kick-off.
			failReason = pasteInjectImplementerInitial(ctx, inj, claudeSessID, wtPath)
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
func pasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
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
	// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial post-paste SendEnterToLastPane: %v\n", err)
		}
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
func pasteInjectImplementerResume(ctx context.Context, inj pasteInjecter, claudeSessID string, iterCount int, wtPath string) string {
	// Dismiss the welcome splash first (hk-rf4ux) — same as implementer-initial.
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx)
	}

	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		reason := fmt.Sprintf("implementer-resume task: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
		return reason
	}

	// Build the combined message. Append the feedback section when the prior
	// iteration's feedback file exists. Both sections are delivered in a single
	// WriteLastPane call (one paste, one Enter) to eliminate the race (hk-poy7k).
	priorIter := iterCount - 1
	feedbackFile := filepath.Join(wtPath, ".harmonik", fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIter))
	feedbackExists := statTaskFile(feedbackFile) == nil

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

// sendResumeSubmitEnter delivers the submit Enter for the implementer-resume
// paste with a bounded retry (hk-ip33d).
//
// It sends Enter once, then re-sends it up to resumeSubmitRetries additional
// times with resumeSubmitRetryDelay between attempts.  The retries defend
// against the fresh-`--resume` timing race where the REPL input handler is not
// yet ready to accept the first keypress: a dropped first Enter leaves the
// prompt unsubmitted (the hk-ip33d run_stale), while a redundant Enter at an
// already-submitted REPL is a harmless empty line.  The loop returns early if
// ctx is cancelled.
//
// Bead: hk-ip33d.
func sendResumeSubmitEnter(ctx context.Context, es enterSender) {
	if err := es.SendEnterToLastPane(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume post-paste SendEnterToLastPane: %v\n", err)
	}
	for i := 0; i < resumeSubmitRetries; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(resumeSubmitRetryDelay):
		}
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume submit-retry %d SendEnterToLastPane: %v\n", i+1, err)
		}
	}
}

// pasteInjectReviewer delivers the reviewer kick-off message.
//
// Buffer purpose slug: "review" → buffer name "harmonik-<session-id>-review".
// Kick-off message: directs Claude to read .harmonik/review-target.md and
// produce the verdict file.
//
// Returns a non-empty failure reason string when the paste-inject step could not
// complete.  Returns "" on success.
func pasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	reviewFile := filepath.Join(wtPath, ".harmonik", "review-target.md")
	if err := statTaskFile(reviewFile); err != nil {
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
		" a field-name violation, re-check that EXACT field name in the new diff before approving.\n"
	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		reason := fmt.Sprintf("reviewer WriteLastPane: %v", err)
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
		return reason
	}
	// Send Enter after paste to submit the message regardless of terminal bracketed-paste mode (hk-8cq23).
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer post-paste SendEnterToLastPane: %v\n", err)
		}
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
// read-only — it has no test loops or multi-file edits to run, so 30 minutes is
// generous even for a very large diff.
//
// Declared as var so tests can override.
//
// Bead: hk-sah87.
var reviewFileHardCeiling = 30 * time.Minute

// reviewFilePerKLineBudget is the extra wait granted per 1000 changed lines in
// the diff under review, added on top of reviewFileTimeout.  5 minutes/1000
// lines means a 2000-line diff gets 10 (base) + 10 = 20 min, a 4000-line diff
// gets 10 + 20 = 30 (capped by reviewFileHardCeiling).  A small diff stays at
// the 10-minute base.
//
// Declared as var so tests can override.
//
// Bead: hk-sah87.
var reviewFilePerKLineBudget = 5 * time.Minute

// reviewFilePollInterval is how often to check for the review verdict file.
var reviewFilePollInterval = 2 * time.Second

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
// Bead: hk-zimkh, hk-sah87.
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
	pollInterval := reviewFilePollInterval
	killDelay := noChangeKillDelay

	// hk-sah87: size the verdict budget by the diff the reviewer must read.
	// worktreeDiffLineCount returns -1 on any measurement failure → base budget.
	changedLines := worktreeDiffLineCount(ctx, wtPath)
	budget := reviewBudgetForDiff(changedLines, reviewFileTimeout, reviewFilePerKLineBudget, reviewFileHardCeiling)
	fmt.Fprintf(os.Stderr,
		"daemon: pasteinject: quit-on-review-file: verdict budget %v for %s (changed_lines=%d)\n",
		budget, wtPath, changedLines)

	loopStart := time.Now()
	deadline := loopStart.Add(budget)
	// hk-sah87: optional pane-liveness checker (same interface the implementer
	// path uses).  When present, a deadline that lands while the reviewer pane
	// still has an active child process is extended by one base window rather
	// than killing a reviewer that is genuinely still reading the diff — but the
	// extension is itself bounded by the absolute hard ceiling below.
	livenessChecker, _ := qs.(paneLivenessChecker)
	hardDeadline := loopStart.Add(reviewFileHardCeiling)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			if now.After(deadline) {
				// hk-sah87: before killing, consult pane liveness.  If the
				// reviewer pane still hosts an active claude process AND we are
				// not yet past the absolute hard ceiling, extend the budget by
				// one base window — the reviewer is still working, not hung.
				if livenessChecker != nil && now.Before(hardDeadline) && livenessChecker.PaneHasActiveProcess(ctx) {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-review-file: budget %v elapsed but reviewer pane active in %s; extending (hard ceiling %v)\n",
						budget, wtPath, reviewFileHardCeiling)
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
