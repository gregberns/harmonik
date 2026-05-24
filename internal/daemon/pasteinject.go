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
	"path/filepath"
	"time"

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

// commitPollTimeout is the maximum time pasteInjectQuitOnCommit will wait
// for a new commit before giving up.  Chosen to exceed typical bead execution
// times while avoiding infinite hangs.
//
// Declared as var (not const) so tests can override it without waiting real
// wall time.
var commitPollTimeout = 10 * time.Minute

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
//   - commitPollTimeout elapses without a new commit: /quit is sent
//     unconditionally, noChangeKillDelay is waited, then killer.Kill is called,
//     and noChangeTimeoutCh is closed to signal the workloop (hk-trjef).
//   - ctx is cancelled (daemon shutting down).
//
// killer and noChangeTimeoutCh may be nil (the kill and signal steps are
// skipped when either is absent).
//
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Beads: hk-cmybm, hk-trjef, hk-5s7tg, hk-930o3.
func pasteInjectQuitOnCommit(
	ctx context.Context,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
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
	deadline := time.Now().Add(pollTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: timeout waiting for new commit in %s (initial=%s); sending /quit unconditionally\n",
					wtPath, initialSHA)
				// Step 1: send /quit unconditionally — Claude may have self-quit
				// without committing (e.g. detected nothing-to-do).
				if qErr := qs.SendQuitToLastPane(ctx); qErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: timeout SendQuitToLastPane: %v\n", qErr)
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
							"daemon: pasteinject: quit-on-commit: timeout Kill: %v\n", kErr)
					}
				}
				// Step 3: signal the workloop that we killed due to noChange-timeout.
				if noChangeTimeoutCh != nil {
					close(noChangeTimeoutCh)
				}
				return
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
