// Package shared holds the pieces that more than one harness implementation
// needs and none of them owns.
//
// It exists because P2 unit E1a lifts the codex harness out of internal/daemon:
// the seed-prompt builder (seedprompt.go), the Refs:<bead> commit-trailer
// machinery (refstrailer.go) and the launch-spec env-key split (env.go) are all
// used by both codex and pi, so leaving them in the daemon would have forced the
// extracted harness to import the monolith it was just separated from.
//
// The package is a leaf WITH RESPECT TO THE DAEMON AND TO EVERY CONCRETE
// HARNESS: it may reach the same seam packages the harness implementations reach
// (internal/core, internal/handler, internal/handlercontract, internal/gitprobe,
// internal/lifecycle/tmux) and nothing else. It must NEVER import
// internal/daemon, internal/harness/codex, internal/harness/claude or
// internal/harness/pi. A harness edge here would turn the leaf into a hub and
// re-couple pi to codex through the back door — which is the exact coupling the
// split exists to remove (picommit.go's piRefsOutcome was literally a type alias
// of the codex enum). The rule is enforced in CI by the `harness-shared`
// depguard block in .golangci.yml.
//
// Origin: internal/daemon/agentseedprompt.go, internal/daemon/codexcommit.go,
// internal/daemon/codexlaunchspec.go.
// Plan: plans/2026-07-21-p2-extraction/E1a-codex-harness.md.
package shared

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

// ImplementerResumeSeedPrompt builds the resume-turn seed prompt for a
// non-claude harness.
//
// Both pi and codex deliver the task as a positional seed-prompt argv (they have
// no TUI/paste path). On a DOT review-loop back-edge re-entry the daemon has
// already written the prior reviewer's verdict to
// .harmonik/reviewer-feedback.iter-<N-1>.md, but the INITIAL seed prompt never
// references it: a resumed implementer handed the identical prompt it already
// satisfied has nothing new to do, produces no commit, and the run no-progress-
// fails and loops. Both harnesses route their resume turn through here so that
// feedback delivery cannot drift between them.
//
// priorIteration is the iteration whose reviewer feedback to deliver
// (iterationCount-1); it is clamped to a minimum of 1 so the referenced path
// always matches a real reviewer-feedback.iter-N.md (mirrors buildAgentTaskContent's
// priorN clamp in internal/workspace/agenttask_chb028.go).
//
// Ref: specs/execution-model.md §4.3 EM-015d-RFD (reviewer-feedback delivery);
// c073 (WS4-4) product-defect finding — the pi/codex DOT back-edge never
// received reviewer feedback.
func ImplementerResumeSeedPrompt(beadID string, priorIteration int) string {
	if priorIteration < 1 {
		priorIteration = 1
	}
	return fmt.Sprintf(implementerResumeSeedTemplate, priorIteration, beadID)
}
