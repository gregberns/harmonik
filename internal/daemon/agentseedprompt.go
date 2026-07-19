package daemon

// agentseedprompt.go — shared implementer seed-prompt construction for the
// non-claude harnesses (pi, codex).
//
// Both pi and codex deliver the task to the implementer as a positional
// seed-prompt argv (they have no TUI/paste path). On a DOT review-loop
// back-edge re-entry (priorSessionID/priorThreadID != nil) the daemon has
// already written the prior reviewer's verdict to
// .harmonik/reviewer-feedback.iter-<N-1>.md (dot_cascade.go WriteReviewerFeedback),
// but the INITIAL seed prompt never references it. A resumed implementer that
// receives the identical initial prompt it already satisfied has nothing new to
// do → produces no commit → the run no-progress-fails and loops.
//
// This is the generic peer of pasteInjectImplementerResume (pasteinject.go),
// which delivers the same feedback pointer to the claude/tmux harness. Both pi
// and codex route their resume turn through implementerResumeSeedPrompt so the
// DOT review-loop feedback delivery cannot drift between harnesses.
//
// Ref: specs/execution-model.md §4.3 EM-015d-RFD (reviewer-feedback delivery);
// c073 (WS4-4) product-defect finding — the pi/codex DOT back-edge never
// received reviewer feedback.

import "fmt"

// implementerResumeSeedTemplate is the seed prompt for a DOT back-edge resume
// turn. It points the resumed implementer at the prior iteration's
// reviewer-feedback file, instructs it to address every point, and REQUIRES a
// new commit carrying the Refs: trailer (the no-commit-on-resume failure this
// fixes). It degrades gracefully when the feedback file is absent.
//
// The first %d is the prior iteration number; the second %s is the bead ID.
const implementerResumeSeedTemplate = `You are resuming a task you already worked on in a prior turn. A reviewer examined your prior work and requested changes, so you have been re-dispatched to address them.

FIRST read .harmonik/reviewer-feedback.iter-%d.md in your worktree — it contains the prior reviewer's verdict, flags, and notes. Address EVERY point it raises. (If that file is not present, re-read .harmonik/agent-task.md and make sure your prior changes were actually committed.)

Then commit ALL your changes in a single NEW git commit. The commit message MUST include the line "Refs: %s" on its own line in the commit body — this trailer is required; without it the system cannot detect that your work is complete. You MUST produce a new commit: if HEAD does not advance, the workflow will loop back to you again.`

// implementerResumeSeedPrompt builds the resume-turn seed prompt for a
// non-claude harness. priorIteration is the iteration whose reviewer feedback
// to deliver (iterationCount-1); it is clamped to a minimum of 1 so the
// referenced path always matches a real reviewer-feedback.iter-N.md (mirrors
// buildAgentTaskContent's priorN clamp in internal/workspace/agenttask_chb028.go).
func implementerResumeSeedPrompt(beadID string, priorIteration int) string {
	if priorIteration < 1 {
		priorIteration = 1
	}
	return fmt.Sprintf(implementerResumeSeedTemplate, priorIteration, beadID)
}
