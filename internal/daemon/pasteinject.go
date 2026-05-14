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

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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

// pasteInjectImplementerInitial delivers the task kick-off message for the
// implementer-initial (and single-mode) phase.
//
// Buffer purpose slug: "task" → buffer name "harmonik-<session-id>-task".
// Kick-off message: directs Claude to read .harmonik/agent-task.md.
func pasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) {
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	if err := statTaskFile(taskFile); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: pasteinject: implementer-initial: %v (skipping inject)\n", err)
		return
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
