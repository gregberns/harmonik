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

// paneSession is the per-session pane-write interface satisfied by
// tmuxSubstrateSession (hk-wx8z8).  Each method targets the session's own
// pane — captured atomically at SpawnWindow time — and does NOT consult any
// daemon-shared "last pane" state.  This is the parallel-safe alternative to
// the substrate-level pasteInjecter/enterSender/quitSender interfaces, which
// shared lastPaneID across concurrent SpawnWindow calls and caused pane
// collisions in --max-concurrent > 1 dispatch.
//
// Callers obtain a paneSession by type-asserting handler.Session →
// handler.SubstrateSessionAccessor → SubstrateSession → paneSession.
// Non-tmux paths (exec.CommandContext, test fixtures) do not satisfy the
// chain; pasteInjectOnLaunchSession is a no-op in that case.
//
// Bead: hk-wx8z8.
type paneSession interface {
	WritePane(ctx context.Context, bufferName string, payload []byte) error
	SendEnter(ctx context.Context) error
	SendQuit(ctx context.Context) error
}

// extractPaneSession reaches through handler.Session → SubstrateSessionAccessor
// → SubstrateSession → paneSession and returns the per-session pane writer.
// Returns (nil, false) when the chain cannot be satisfied — e.g. legacy
// exec.CommandContext sessions or test fixtures that don't expose paneSession.
//
// Bead: hk-wx8z8.
func extractPaneSession(sess handler.Session) (paneSession, bool) {
	if sess == nil {
		return nil, false
	}
	accessor, ok := sess.(handler.SubstrateSessionAccessor)
	if !ok {
		return nil, false
	}
	inner := accessor.Inner()
	if inner == nil {
		return nil, false
	}
	ps, ok := inner.(paneSession)
	return ps, ok
}

// pasteInjectQuitOnCommitSession is the per-session variant of
// pasteInjectQuitOnCommit.  It uses the session-scoped SendQuit (which targets
// the pane captured atomically at SpawnWindow) instead of the substrate-shared
// SendQuitToLastPane.  Required for --max-concurrent > 1 correctness (hk-wx8z8).
func pasteInjectQuitOnCommitSession(
	ctx context.Context,
	ps paneSession,
	wtPath string,
	initialSHA string,
) {
	deadline := time.Now().Add(commitPollTimeout)
	ticker := time.NewTicker(commitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: timeout waiting for new commit in %s (initial=%s)\n",
					wtPath, initialSHA)
				return
			}

			headSHA, err := resolveWorktreeHEAD(ctx, wtPath)
			if err != nil {
				continue
			}
			if headSHA != initialSHA {
				if qErr := ps.SendQuit(ctx); qErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-commit: SendQuit: %v\n", qErr)
				}
				return
			}
		}
	}
}

// pasteInjectOnLaunchSession is the per-session variant of pasteInjectOnLaunch.
// All paste/enter operations target the session's own pane (captured atomically
// at SpawnWindow), so concurrent SpawnWindow calls cannot misdirect one
// session's kick-off to another session's pane (hk-wx8z8).
//
// When ps is nil (non-substrate sessions / test fixtures), this is a no-op.
func pasteInjectOnLaunchSession(
	ctx context.Context,
	ps paneSession,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
) {
	if ps == nil {
		return
	}
	switch phase {
	case handlercontract.ReviewLoopPhaseReviewer:
		pasteInjectReviewerSession(ctx, ps, claudeSessID, wtPath)
	case handlercontract.ReviewLoopPhaseImplementerResume:
		pasteInjectImplementerResumeSession(ctx, ps, claudeSessID, iterCount, wtPath)
	default:
		pasteInjectImplementerInitialSession(ctx, ps, claudeSessID, wtPath)
	}
}

func pasteInjectImplementerInitialSession(ctx context.Context, ps paneSession, claudeSessID, wtPath string) {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial: %v (skipping inject)\n", err)
		return
	}
	if err := ps.SendEnter(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial SendEnter: %v\n", err)
	}
	splashDismissWait(ctx)
	bufName := bufferName(claudeSessID, "task")
	msg := "Please read .harmonik/agent-task.md and begin.\n"
	if err := ps.WritePane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial WritePane: %v\n", err)
	}
}

func pasteInjectImplementerResumeSession(ctx context.Context, ps paneSession, claudeSessID string, iterCount int, wtPath string) {
	if err := ps.SendEnter(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume SendEnter: %v\n", err)
	}
	splashDismissWait(ctx)

	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume task: %v (skipping task inject)\n", err)
	} else {
		bufName := bufferName(claudeSessID, "task")
		msg := "Please read .harmonik/agent-task.md and begin.\n"
		if err := ps.WritePane(ctx, bufName, []byte(msg)); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume WritePane(task): %v\n", err)
		}
	}

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
	if err := ps.WritePane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-resume WritePane(feedback): %v\n", err)
	}
}

func pasteInjectReviewerSession(ctx context.Context, ps paneSession, claudeSessID, wtPath string) {
	reviewFile := filepath.Join(wtPath, ".harmonik", "review-target.md")
	if err := statTaskFile(reviewFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer: %v (skipping inject)\n", err)
		return
	}
	if err := ps.SendEnter(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer SendEnter: %v\n", err)
	}
	splashDismissWait(ctx)
	bufName := bufferName(claudeSessID, "review")
	msg := "Read .harmonik/review-target.md in this worktree." +
		" It contains the bead context, the diff range to review, and any prior-iteration verdicts." +
		" Produce your verdict by writing .harmonik/review.json conforming to the agent-reviewer schema v1.\n"
	if err := ps.WritePane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: reviewer WritePane: %v\n", err)
	}
}

// commitPollInterval is the interval between git HEAD checks in
// pasteInjectQuitOnCommit.  500ms balances responsiveness with avoiding
// excessive git subprocess overhead.
const commitPollInterval = 500 * time.Millisecond

// commitPollTimeout is the maximum time pasteInjectQuitOnCommit will wait
// for a new commit before giving up.  Chosen to exceed typical bead execution
// times while avoiding infinite hangs.
const commitPollTimeout = 10 * time.Minute

// pasteInjectQuitOnCommit watches the worktree at wtPath for a new commit
// (HEAD changing from initialSHA).  When detected, it sends `/quit Enter`
// to the pane via qs to cause Claude Code to exit and fire the Stop hook.
//
// This is the daemon-side complement to the agent-task.md session-completion
// instruction (CHB-028 / hk-cmybm).  Claude Code agents cannot execute slash
// commands from their tool API; the daemon detects the commit landing and
// injects /quit programmatically.
//
// The function runs in a goroutine and returns when:
//   - A new commit is detected and /quit is sent (success).
//   - commitPollTimeout elapses without a new commit (non-fatal: log only).
//   - ctx is cancelled (daemon shutting down).
//
// Non-fatal: errors are logged to stderr; the caller's waitWithSocketGrace
// will eventually time out via stopHookGrace (3s) if /quit is never sent.
//
// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
// Bead: hk-cmybm.
func pasteInjectQuitOnCommit(
	ctx context.Context,
	qs quitSender,
	wtPath string,
	initialSHA string,
) {
	deadline := time.Now().Add(commitPollTimeout)
	ticker := time.NewTicker(commitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-commit: timeout waiting for new commit in %s (initial=%s)\n",
					wtPath, initialSHA)
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
) {
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
