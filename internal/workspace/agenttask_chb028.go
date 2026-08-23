package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ErrTaskFileCollision is retained for backwards compatibility of the public
// surface; it is no longer returned by WriteAgentTask. Each launch overwrites
// the prior file (see WriteAgentTask docs and CHB-028 amendment 2026-05-13).
var ErrTaskFileCollision = errors.New("workspace: TaskFileCollision")

// ErrTaskFileEmpty is returned by WriteAgentTask when the constructed file
// content is empty, or when the post-write assertion finds the file absent or
// empty.
//
// Per CHB-028 Invariant: an empty or absent agent-task.md after the write step
// is a fatal structural error.
var ErrTaskFileEmpty = errors.New("workspace: TaskFileEmpty")

// AgentTaskPayload carries all fields required by the CHB-028 content shape
// for the per-launch task-delivery file (agent-task.md).
//
// Common fields (all phases):
//   - BeadID, Title, Phase, Iteration, RunID, WorkspacePath, Body.
//
// Phase-specific fields:
//   - PriorVerdictFile: set for phase = implementer-resume; path to
//     .harmonik/review.iter-<N-1>.json.
//   - PriorVerdictSummary: human-readable one-liner derived from the prior
//     verdict; MAY be empty (Claude will read the file).
//   - ReviewBaseSHA, ReviewHeadSHA: set for phase = reviewer; base and head
//     commit SHAs for the diff under review.
//
// Re-attach flag:
//   - ReAttach: when true, WriteAgentTask returns nil immediately if
//     agent-task.md already exists (idempotent re-attach path per CHB-028
//     re-launch semantics — daemon restart finding its own prior write).
type AgentTaskPayload struct {
	// BeadID is the opaque bead correlation identifier (HARMONIK_BEAD_ID).
	// Use "none" when not bead-tied.
	BeadID string

	// Title is the bead title, or run_id when not bead-tied.
	Title string

	// Phase is the review-loop phase string:
	// "implementer-initial" | "implementer-resume" | "reviewer".
	// Empty string is treated as a single-mode dispatch; spec requires one of the
	// three values for review-loop, but single-mode is valid.
	Phase string

	// Iteration is the 1-based iteration index (LaunchSpec.iteration_count).
	Iteration int

	// RunID is the UUIDv7 run identifier (HARMONIK_RUN_ID).
	RunID string

	// WorkspacePath is the absolute path to the workspace root.
	WorkspacePath string

	// Body is the bead body verbatim (or operator-provided task string).
	// MUST NOT be empty.
	Body string

	// PriorVerdictFile is the absolute path to the archived reviewer verdict
	// for the previous iteration (.harmonik/review.iter-<N-1>.json).
	// Required when Phase = "implementer-resume". Ignored otherwise.
	PriorVerdictFile string

	// PriorVerdictSummary is a human-readable one-line summary of the prior
	// verdict (e.g. "REQUEST_CHANGES — address flagged issues before proceeding").
	// Optional; used in the Prior-Iteration Context section when Phase =
	// "implementer-resume". MAY be empty.
	PriorVerdictSummary string

	// ReviewBaseSHA is the base commit SHA for the diff under review.
	// Required when Phase = "reviewer". Ignored otherwise.
	ReviewBaseSHA string

	// ReviewHeadSHA is the head commit SHA for the diff under review.
	// Required when Phase = "reviewer". Ignored otherwise.
	ReviewHeadSHA string

	// ReAttach signals that the daemon is re-attaching to an existing session.
	// When true, WriteAgentTask returns nil immediately if agent-task.md is
	// already present (idempotent re-attach semantics per CHB-028).
	// When false (default), the existing file is overwritten with the new
	// content; review-loop phase transitions are normal overwrite, not error.
	ReAttach bool

	// ExtraContext is an optional operator-supplied free-form string that is
	// appended as an "## Extra Context" section after the Task Description
	// (hk-boiwe). When empty, the section is omitted entirely. Intended to
	// carry predecessor-commit SHAs, dependency-landing notes, or orchestrator
	// briefs that are not part of the bead body itself.
	ExtraContext string

	// BaseBranch is the resolved lands_on branch for this run per WM-005b
	// (hk-mtm0w). Rendered as base_branch in the agent-task header so the
	// implementer can rebase against origin/$BaseBranch before exiting.
	// When empty, the header line is omitted.
	BaseBranch string

	// Completion is the launching harness's own declared completion mode —
	// pass h.Completion() verbatim. It selects the wording of the Session
	// Completion section, because how an agent is told to finish is a
	// property of the harness it runs on and of nothing else (hk-quit-
	// instruction-not-portable-ms55w).
	//
	// The section used to be hard-coded to the claude REPL form: "you MUST
	// run /quit". Pi and codex have no REPL and no slash commands, and a pi
	// agent that had already committed its work obeyed that instruction with
	// the only tool it has — `echo "/quit" | pbcopy`. The session stayed
	// alive, the daemon killed it on the budget, and a correct run was
	// scored as a structural crash with its commit stranded on the run
	// branch.
	//
	// The zero value is CompletionEventStreamThenQuit, the claude form, so an
	// unset field renders what every caller rendered before this field
	// existed.
	Completion handlercontract.CompletionMode
}

// AgentTaskPath returns the canonical path for the per-launch task-delivery
// file per claude-hook-bridge.md §4.11 CHB-028:
//
//	${workspace_path}/.harmonik/agent-task.md
//
// The caller MUST pass the absolute worktree path.
func AgentTaskPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "agent-task.md")
}

// ReviewerFeedbackPath returns the canonical path for the reviewer-feedback
// delivery file for the given prior iteration per execution-model.md EM-015d-RFD:
//
//	${workspace_path}/.harmonik/reviewer-feedback.iter-<priorIteration>.md
func ReviewerFeedbackPath(workspacePath string, priorIteration int) string {
	return filepath.Join(workspacePath, ".harmonik",
		fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIteration))
}

// ReviewTargetPath returns the canonical path for the reviewer input artifact
// per execution-model.md EM-015d-RIA and workspace-model.md §6.2 WM-RIA-001:
//
//	${workspace_path}/.harmonik/review-target.md
func ReviewTargetPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "review-target.md")
}

// AgentTaskGitignoreLine is the gitignore pattern that MUST be present in the
// worktree's .gitignore to exclude the agent-task.md file from checkpoint
// commits per CHB-028 gitignore hygiene.
const AgentTaskGitignoreLine = ".harmonik/agent-task*"

// ReviewerFeedbackGitignoreLine is the gitignore pattern that MUST be present
// in the worktree's .gitignore to exclude reviewer-feedback files from
// checkpoint commits per EM-015d-RFD gitignore hygiene.
const ReviewerFeedbackGitignoreLine = ".harmonik/reviewer-feedback*"

// ReviewTargetGitignoreLine is the gitignore pattern that MUST be present in
// the worktree's .gitignore to exclude review-target.md from checkpoint commits
// per EM-015d-RIA gitignore hygiene.
const ReviewTargetGitignoreLine = ".harmonik/review-target.md"

// WriteAgentTask materializes the per-launch task-delivery file at
// ${workspace_path}/.harmonik/agent-task.md per CHB-028.
//
// # Ordering obligation
//
// MUST be called AFTER MaterializeClaudeSettings (WM-040a) and
// EnsureWorktreeTrust (WM-040b) and BEFORE SubstrateSpawn. See CHB-028
// materialization timing and CHB-029 ordering.
//
// # Atomic-write discipline
//
// Follows the WM-026 atomic temp-write + rename + fsync(parent_dir) pattern:
// writes to agent-task.tmp-<pid>, fsyncs, renames to agent-task.md, fsyncs
// the parent directory.
//
// # Overwrite / re-attach semantics
//
// Each call writes the file with content for the caller's
// (run_id, phase, iteration) tuple. Review-loop phase transitions
// (impl → reviewer → impl-resume) reuse the same worktree and each is its
// own logical launch with its own content, so a pre-existing file is
// overwritten — not an error.
//
// When payload.ReAttach = true (daemon restart finding its own prior write
// for the same launch), WriteAgentTask returns nil immediately if the file
// is already present, avoiding a redundant write.
//
// # Validation
//
// payload.Body MUST be non-empty; an empty Body returns ErrTaskFileEmpty.
//
// # Gitignore hygiene
//
// WriteAgentTask does NOT mutate any .gitignore (hk-jvzc2). Excluding
// .harmonik/agent-task* from commits is an operator setup obligation: the
// parent repo's root .gitignore MUST already cover .harmonik/* (the worktree
// inherits this on `git worktree add`). Earlier revisions of this function
// appended the entry to the worktree .gitignore per launch; that silent edit
// surfaced as uncommitted churn in the parent repo's working tree across
// dogfood runs (hk-cd92e, hk-jvzc2).
//
// # Post-write assertion
//
// After the atomic write, the file is stat'd. An absent or zero-size file is
// ErrTaskFileEmpty (fatal structural error per CHB-028 Invariant).
func WriteAgentTask(workspacePath string, payload AgentTaskPayload) error {
	if strings.TrimSpace(payload.Body) == "" {
		return fmt.Errorf("%w: payload.Body is empty for bead %q run %q",
			ErrTaskFileEmpty, payload.BeadID, payload.RunID)
	}

	target := AgentTaskPath(workspacePath)

	if payload.ReAttach {
		if _, err := os.Stat(target); err == nil {
			return nil
		}
	}

	content := buildAgentTaskContent(payload)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: constructed content is empty for bead %q run %q",
			ErrTaskFileEmpty, payload.BeadID, payload.RunID)
	}

	if err := os.MkdirAll(filepath.Dir(target), core.HarmonikDirMode); err != nil {
		return fmt.Errorf("workspace: WriteAgentTask: MkdirAll %q: %w", filepath.Dir(target), err)
	}

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteAgentTask: atomic write %q: %w", target, err)
	}

	fi, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("%w: stat after write failed for %q: %w", ErrTaskFileEmpty, target, err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("%w: file is zero bytes after write at %q", ErrTaskFileEmpty, target)
	}

	return nil
}

func buildAgentTaskContent(p AgentTaskPayload) string {
	var sb strings.Builder

	sb.WriteString("# Harmonik Task\n\n")
	sb.WriteString(fmt.Sprintf("bead_id: %s\n", p.BeadID))
	sb.WriteString(fmt.Sprintf("title: %s\n", p.Title))
	sb.WriteString(fmt.Sprintf("phase: %s\n", p.Phase))
	sb.WriteString(fmt.Sprintf("iteration: %d\n", p.Iteration))
	sb.WriteString(fmt.Sprintf("run_id: %s\n", p.RunID))
	sb.WriteString(fmt.Sprintf("workspace_path: %s\n", p.WorkspacePath))
	if p.BaseBranch != "" {
		sb.WriteString(fmt.Sprintf("base_branch: %s\n", p.BaseBranch))
	}
	if p.WorkspacePath != "" {
		sb.WriteString("\n## Worktree Discipline (CRITICAL — read first)\n\n")
		sb.WriteString(fmt.Sprintf("Your working directory is `%s`.\n", p.WorkspacePath))
		sb.WriteString("ALL file paths you read, write, or edit MUST be inside this worktree.\n")
		sb.WriteString("NEVER use absolute paths that begin with the MAIN repo root outside your worktree — writing there silently loses your work because the daemon merges from THIS worktree, not main.\n\n")
		sb.WriteString("When running discovery commands (find, grep, ls, rg), use relative paths anchored to your worktree, NOT the main repo:\n\n")
		sb.WriteString("  CORRECT:   find . -name '*.go'\n")
		sb.WriteString(fmt.Sprintf("  CORRECT:   find %s/internal -name '*.go'\n", p.WorkspacePath))
		sb.WriteString("  WRONG:     any absolute path outside the worktree above   (your edits will be lost)\n\n")
		sb.WriteString("If a discovery command returns paths under the main repo root, translate them into your worktree before reading or editing.\n")
		sb.WriteString(fmt.Sprintf("Note: your worktree has its OWN `.harmonik/` directory at `%s/.harmonik/` (agent-task.md, reviewer-feedback files). The MAIN repo's `.harmonik/` (queue.json, events.jsonl, daemon.sock) is a DIFFERENT tree — do not read or write there.\n", p.WorkspacePath))
	}

	sb.WriteString("\n## Bead Lifecycle (CRITICAL — read before acting)\n\n")
	sb.WriteString("DO NOT run `br close`, `br update --status closed`, or any terminal bead transition from inside this worktree.\n")
	sb.WriteString("The daemon owns all bead lifecycle transitions (open → in_progress → closed/failed).\n")
	sb.WriteString("Running `br close` from the worktree causes premature closure that leaks to the parent repo even when no implementation has landed.\n")
	sb.WriteString(fmt.Sprintf("Your job is to %s. The daemon will close the bead on your behalf after verifying the commit.\n",
		jobSummary(p.Completion)))

	sb.WriteString("\n## Task Description\n\n")
	sb.WriteString(p.Body)
	if !strings.HasSuffix(p.Body, "\n") {
		sb.WriteString("\n")
	}

	if strings.TrimSpace(p.ExtraContext) != "" {
		sb.WriteString("\n## Extra Context\n\n")
		sb.WriteString(p.ExtraContext)
		if !strings.HasSuffix(p.ExtraContext, "\n") {
			sb.WriteString("\n")
		}
	}

	if p.Phase != "reviewer" {
		sb.WriteString("\n## Tests\n\n")
		sb.WriteString("A test earns its place by executing product code and failing when the behavior breaks. Asserting that a file exists, grepping prose, or pinning an internal signature is not a test.\n")
		sb.WriteString("Reach the change through the entry point a user or the daemon actually calls; prefer the existing `_test.go` for the code you changed, and name any new file after the behavior it protects — never after a bead or ticket ID.\n")
		sb.WriteString("Do not write a tier you will not run. If the natural gate is a suite you cannot run here, that is a signal the change is too big for one dispatch — say so rather than committing an unrun test.\n")
		sb.WriteString("A bad test is worse than bad production code: it makes the production code harder to fix while claiming it is protected. When a test already sitting in your path does not clearly earn its keep by the standard above — it never reaches product code, it pins an internal signature, or it is large and intricate out of proportion to what it protects — the default is to delete it, as part of this change rather than as a follow-up.\n")
		sb.WriteString("Deleting such a test is ordinary work, not a permission you need. Say what you deleted and why in the commit message.\n")

		sb.WriteString("\n## Structure\n\n")
		sb.WriteString("If the shape of the file or function you have to touch is what makes this change hard, say so in the bead rather than working around it. Make the smallest change that does not deepen the problem.\n")

		sb.WriteString("\n## Commit Message (a gate refuses any other shape)\n\n")
		sb.WriteString("Write the commit message into a file under `${TMPDIR:-/tmp}` — a scratch directory git ignores — so the file itself never becomes part of your change. Commit with `git commit -F \"${TMPDIR:-/tmp}/commit-msg.txt\"`. Do NOT use `git commit -m`.\n\n")
		sb.WriteString("`${TMPDIR:-/tmp}` and this worktree are the only places you can count on being able to write. A sandboxed run refuses every other path, and `sudo` does not lift that refusal.\n\n")
		sb.WriteString("The subject is the first line. It MUST have this shape:\n\n")
		sb.WriteString("```\n")
		sb.WriteString("<type>(<scope>): <description>\n")
		sb.WriteString("```\n\n")
		sb.WriteString("The `<type>` MUST be one of these nine words: `feat` `fix` `refactor` `test` `docs` `chore` `spec` `build` `perf` — no other word is accepted.\n")
		sb.WriteString("The `(<scope>)` part is optional. A scope holds only lower-case letters, digits, commas, hyphens and colons.\n")
		sb.WriteString("The subject MUST be 72 characters or fewer. A subject of 73 characters is refused.\n")
		sb.WriteString("The subject MUST NOT end with a period.\n")
		sb.WriteString("The body MUST hold a `Reviewed-By:` line and a `Review-Verdict:` line. Each one starts at the beginning of its own line, with no spaces before it.\n")
		sb.WriteString("The `Review-Verdict:` JSON MUST be on ONE line.\n")
		sb.WriteString("The body MUST also hold a `Refs:` line that names the bead. The commit-message gate never reads that line — the daemon does.\n")
		sb.WriteString("Your work counts as done only when BOTH of these are true: this worktree's HEAD has advanced past where it started, and the new commit carries that `Refs:` line. If HEAD does not advance, the workflow sends this task back to you.\n")
		sb.WriteString("That line is also what ties the commit to the bead, so a later reconciliation can match the two.\n")
		sb.WriteString("NEVER write a verdict of APPROVE. You did not review your own work. Record the honest form below.\n\n")
		sb.WriteString("Copy the lines between the two fence lines below. Do NOT copy the fence lines. Write your own subject line and your own sentence, and keep the `Refs:` line, the `Reviewed-By:` line and the `Review-Verdict:` line exactly as they are:\n\n")
		sb.WriteString("```\n")
		sb.WriteString("docs(workspace): remove the stale mode note from the Terminal comment\n")
		sb.WriteString("\n")
		sb.WriteString("The comment named a mode this type no longer has. Remove that sentence.\n")
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("Refs: %s\n", p.BeadID))
		sb.WriteString("Reviewed-By: none\n")
		sb.WriteString("Review-Verdict: {\"schema_version\": 1, \"verdict\": \"NOT_REVIEWED\", \"flags\": [\"no-reviewer-reached\"], \"notes\": \"No reviewer was reached for this commit.\"}\n")
		sb.WriteString("```\n\n")
		sb.WriteString("If your change is only a typo or a whitespace fix, you MAY write the single line `Trivial: true` in place of the `Reviewed-By:` and `Review-Verdict:` lines. Start it at the beginning of its own line, with no spaces before it.\n")
	}

	switch p.Phase {
	case "implementer-resume":
		sb.WriteString("\n## Prior-Iteration Context\n\n")
		priorN := p.Iteration - 1
		if priorN < 1 {
			priorN = 1
		}
		if p.PriorVerdictFile != "" {
			sb.WriteString(fmt.Sprintf("reviewer-feedback: %s\n", p.PriorVerdictFile))
		} else {
			derivedPath := filepath.Join(p.WorkspacePath, ".harmonik",
				fmt.Sprintf("review.iter-%d.json", priorN))
			sb.WriteString(fmt.Sprintf("reviewer-feedback: %s\n", derivedPath))
		}
		if p.PriorVerdictSummary != "" {
			sb.WriteString(fmt.Sprintf("prior-verdict-summary: %s\n", p.PriorVerdictSummary))
		}

	case "reviewer":
		sb.WriteString("\n")
		renderReviewerConstraint(&sb)

		sb.WriteString("\n## Prior-Iteration Context\n\n")
		sb.WriteString(fmt.Sprintf("review_base_sha: %s\n", p.ReviewBaseSHA))
		sb.WriteString(fmt.Sprintf("review_head_sha: %s\n", p.ReviewHeadSHA))
	}

	renderSessionCompletion(&sb, p.Completion)

	return sb.String()
}

func jobSummary(mode handlercontract.CompletionMode) string {
	if mode == handlercontract.CompletionProcessExit {
		return "implement and commit"
	}
	return "implement, commit, and `/quit`"
}

func renderSessionCompletion(sb *strings.Builder, mode handlercontract.CompletionMode) {
	sb.WriteString("\n## Session Completion\n\n")

	if mode == handlercontract.CompletionProcessExit {
		sb.WriteString("Committing your work is what completes the task. Your session ends by itself when your turn ends — there is nothing else to run.\n")
		sb.WriteString("The commit message MUST carry the `Refs: <bead-id>` line on its own line in the body. That trailer is how the daemon detects that your work is done.\n")
		sb.WriteString("Do NOT try to quit, exit, or end the session yourself, and do not run any slash command. This harness has none; an attempt to end the session keeps it alive past its budget and the daemon then scores your correct work as a crash.\n")
		return
	}

	sb.WriteString("IMPORTANT: You MUST run `/quit` as your final action after committing all work.\n")
	sb.WriteString("Do not ask the user to run it — you must type `/quit` yourself and submit it.\n")
	sb.WriteString("The daemon cannot detect that your task is complete until you exit this session.\n")
	sb.WriteString("Failure to run `/quit` will leave the workflow permanently stalled.\n")
}

func renderReviewerConstraint(sb *strings.Builder) {
	sb.WriteString("## Reviewer Constraint (CRITICAL — read before acting)\n\n")
	sb.WriteString("You are a READ-ONLY reviewer. You MUST NOT run any git command that changes repository state.\n")
	sb.WriteString("Forbidden commands: `git reset`, `git checkout`, `git cherry-pick`, `git merge`, `git branch -d`, `git push`, `git rebase`, or any other state-mutating git operation.\n")
	sb.WriteString("You operate on a detached-HEAD reviewer worktree. Produce your verdict by running `harmonik write-review-verdict --verdict=<APPROVE|REQUEST_CHANGES|BLOCK> --notes=\"<your rationale>\" --flags=<comma,separated,tags>`.\n")
	sb.WriteString("DO NOT hand-write `.harmonik/review.json` directly with the Write tool — always use the `harmonik write-review-verdict` command above, even when notes quotes code containing backticks. This command writes the file atomically (temp file + rename), so no separate atomic-write step is needed.\n")
	sb.WriteString("Violating this constraint can corrupt the implementer's task branch and break the merge pipeline.\n")
}

// ReviewerFeedbackPayload carries the inputs for WriteReviewerFeedback.
type ReviewerFeedbackPayload struct {
	// WorkspacePath is the absolute path to the workspace root.
	WorkspacePath string

	// PriorIteration is the just-completed iteration ordinal (1-indexed).
	// The file is written to .harmonik/reviewer-feedback.iter-<PriorIteration>.md.
	PriorIteration int

	// Verdict is the prior reviewer's verdict string (APPROVE, REQUEST_CHANGES, BLOCK).
	Verdict string

	// Flags is the prior reviewer's flags array. MAY be nil or empty.
	Flags []string

	// Notes is the full notes text from the prior reviewer verdict.
	Notes string

	// DiffHash is the SHA-256 hex of the diff at the time the hash was computed.
	// MAY be empty if unavailable.
	DiffHash string

	// DiffLines is the line count of the diff, for the diff_summary section.
	// Zero means unavailable.
	DiffLines int
}

// WriteReviewerFeedback materializes the reviewer-feedback delivery file at
// ${workspace_path}/.harmonik/reviewer-feedback.iter-<N-1>.md per
// execution-model.md EM-015d-RFD.
//
// The daemon MUST call this BEFORE launching the implementer-resume pane;
// only after this file exists on disk may the paste-inject occur.
//
// Uses the WM-026 atomic temp-write + rename + fsync(parent_dir) discipline.
// Does NOT mutate the worktree .gitignore (hk-jvzc2); operator setup MUST
// cover .harmonik/* in the parent repo's root .gitignore.
func WriteReviewerFeedback(payload ReviewerFeedbackPayload) error {
	target := ReviewerFeedbackPath(payload.WorkspacePath, payload.PriorIteration)
	content := buildReviewerFeedbackContent(payload)

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewerFeedback: atomic write %q: %w", target, err)
	}

	return nil
}

func buildReviewerFeedbackContent(p ReviewerFeedbackPayload) string {
	var sb strings.Builder

	if VerdictCameFromAReviewer(p.Verdict) {
		sb.WriteString(fmt.Sprintf("# Reviewer feedback — iteration %d\n\n", p.PriorIteration))
	} else {
		sb.WriteString(fmt.Sprintf("# Workflow feedback — iteration %d\n\n", p.PriorIteration))
		sb.WriteString("NO REVIEWER READ YOUR CHANGE. The harmonik daemon wrote this file because the\n")
		sb.WriteString("workflow routed the work back to you. It uses the reviewer-feedback name only\n")
		sb.WriteString("because that is the name your resume instruction reads. Nobody has judged the\n")
		sb.WriteString("change itself yet.\n\n")
	}
	sb.WriteString(fmt.Sprintf("verdict: %s\n\n", p.Verdict))

	sb.WriteString("flags:\n\n")
	if len(p.Flags) == 0 {
		sb.WriteString("(none)\n")
	} else {
		for _, f := range p.Flags {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	sb.WriteString("\n## Notes\n\n")
	sb.WriteString(p.Notes)
	if !strings.HasSuffix(p.Notes, "\n") {
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Diff summary\n\n")
	if p.DiffHash != "" {
		sb.WriteString(fmt.Sprintf("diff_hash: %s\n", p.DiffHash))
	}
	if p.DiffLines > 0 {
		sb.WriteString(fmt.Sprintf("diff_lines: %d\n", p.DiffLines))
	}

	return sb.String()
}

// ReviewTargetPayload carries the inputs for WriteReviewTarget.
type ReviewTargetPayload struct {
	// WorkspacePath is the absolute path to the workspace root.
	WorkspacePath string

	// BeadID is the opaque bead correlation identifier.
	BeadID string

	// Iteration is the current iteration ordinal (1-indexed).
	Iteration int

	// BeadTitle is the bead title.
	BeadTitle string

	// BeadBody is the bead body verbatim.
	BeadBody string

	// BaseSHA is the task-branch fork point commit SHA (parent_commit from
	// Workspace record per WM-026 / Run.context.parent_commit).
	BaseSHA string

	// HeadSHA is the current HEAD of the task branch at reviewer-launch time.
	HeadSHA string

	// PriorVerdicts is the ordered list of prior-iteration verdicts (1..N-1).
	// Each entry carries the iteration number, verdict, flags, and first 200 chars
	// of notes, per EM-015d-RIA §Prior verdicts section.
	// MAY be nil for iteration 1 (section is omitted entirely).
	PriorVerdicts []ReviewTargetPriorVerdict

	// ReviewerHints is the operator-configured reviewer-tier hints string.
	// Reproduced verbatim in the ## Hints section when non-empty.
	ReviewerHints string
}

// ReviewTargetPriorVerdict carries the per-iteration summary used in the
// Prior verdicts section of review-target.md.
type ReviewTargetPriorVerdict struct {
	// Iteration is the 1-indexed ordinal of this prior review.
	Iteration int

	// Verdict is the verdict string (APPROVE, REQUEST_CHANGES, BLOCK).
	Verdict string

	// Flags is the flags list from the verdict. MAY be nil or empty.
	Flags []string

	// NotesSummary is the first 200 chars of the notes field, truncated with "…"
	// per EM-015d-RIA. The caller is responsible for truncation; this field is
	// reproduced verbatim.
	NotesSummary string
}

// WriteReviewTarget materializes the reviewer input artifact at
// ${workspace_path}/.harmonik/review-target.md per execution-model.md
// EM-015d-RIA and workspace-model.md §6.2 WM-RIA-001.
//
// The daemon MUST call this BEFORE spawning the reviewer pane via
// tmux new-window; only after this file exists on disk may the pane be started
// and the paste-inject occur (EM-015d-RIA ordering: file exists → pane live →
// paste-inject fires).
//
// The file is overwritten on each reviewer launch (not appended); the prior
// iteration's review-target.md is not archived.
//
// Uses the WM-026 atomic temp-write + rename + fsync(parent_dir) discipline.
// Does NOT mutate the worktree .gitignore (hk-jvzc2); operator setup MUST
// cover .harmonik/* in the parent repo's root .gitignore.
func WriteReviewTarget(payload ReviewTargetPayload) error {
	target := ReviewTargetPath(payload.WorkspacePath)
	content := buildReviewTargetContent(payload)

	if err := os.MkdirAll(filepath.Dir(target), core.HarmonikDirMode); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: MkdirAll %q: %w", filepath.Dir(target), err)
	}
	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: atomic write %q: %w", target, err)
	}

	return nil
}

func buildReviewTargetContent(p ReviewTargetPayload) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Review target — bead %s, iteration %d\n\n", p.BeadID, p.Iteration))

	renderReviewerConstraint(&sb)
	sb.WriteString("\n")

	sb.WriteString("## Coverage Check (CRITICAL — all-X completeness)\n\n")
	sb.WriteString("If the bead title or body uses all-inclusive language — 'all X', 'all sites', 'every X',\n")
	sb.WriteString("'all callers', 'all handlers', 'all usages', 'all implementations', or any similar claim\n")
	sb.WriteString("of complete coverage — you MUST perform the following check before approving:\n\n")
	sb.WriteString("1. Identify the specific identifier, pattern, or construct the bead claims to update everywhere.\n")
	sb.WriteString("2. Grep the worktree for every occurrence: e.g. `grep -rn <pattern> . --include='*.go'` (exclude `.harmonik/`).\n")
	sb.WriteString("3. Obtain the diff: `git diff <base>..<head>` (base and head SHAs are in the Diff range section below).\n")
	sb.WriteString("4. For each occurrence found in step 2, verify it appears in the diff from step 3.\n")
	sb.WriteString("5. If ANY occurrence is absent from the diff, the change is INCOMPLETE:\n")
	sb.WriteString("   - Set `flags: [\"incomplete-coverage\"]` in `.harmonik/review.json`.\n")
	sb.WriteString("   - Use `REQUEST_CHANGES` verdict (or `BLOCK` if the missed site is load-bearing).\n")
	sb.WriteString("   - Name every missed file path and line number in your `notes`.\n\n")
	sb.WriteString("Approving a partial 'all-X' change without running this grep check is a reviewer failure.\n\n")

	sb.WriteString("## Spec Field-Name Check (CRITICAL)\n\n")
	sb.WriteString("When the bead body's '## Implementation Notes' section names exact field/struct names\n")
	sb.WriteString("(e.g. 'MUST be SessionID string — NOT SessID'), grep the diff for every named identifier\n")
	sb.WriteString("and verify the exact name appears. When a prior verdict has flag 'spec-field-name' or notes\n")
	sb.WriteString("naming a field-name violation, re-check that EXACT field name in the new diff before approving.\n\n")

	sb.WriteString("## Bead\n\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", p.BeadID))
	sb.WriteString(fmt.Sprintf("title: %s\n\n", p.BeadTitle))
	sb.WriteString(p.BeadBody)
	if !strings.HasSuffix(p.BeadBody, "\n") {
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Diff range\n\n")
	sb.WriteString(fmt.Sprintf("base: %s\n", p.BaseSHA))
	sb.WriteString(fmt.Sprintf("head: %s\n", p.HeadSHA))

	if len(p.PriorVerdicts) > 0 {
		sb.WriteString("\n## Prior verdicts\n")
		for _, pv := range p.PriorVerdicts {
			sb.WriteString(fmt.Sprintf("\n### Iteration %d\n\n", pv.Iteration))
			verdictFilePath := filepath.Join(p.WorkspacePath, ".harmonik",
				fmt.Sprintf("review.iter-%d.json", pv.Iteration))
			sb.WriteString(fmt.Sprintf("Verdict file: %s\n\n", verdictFilePath))
			flagsStr := "(none)"
			if len(pv.Flags) > 0 {
				flagsStr = strings.Join(pv.Flags, ", ")
			}
			sb.WriteString(fmt.Sprintf("verdict: %s  flags: %s  notes: %s\n",
				pv.Verdict, flagsStr, pv.NotesSummary))
		}
	}

	if strings.TrimSpace(p.ReviewerHints) != "" {
		sb.WriteString("\n## Hints\n\n")
		sb.WriteString(p.ReviewerHints)
		if !strings.HasSuffix(p.ReviewerHints, "\n") {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
