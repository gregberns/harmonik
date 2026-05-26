package workspace

// agenttask_chb028.go — WriteAgentTask, WriteReviewerFeedback, WriteReviewTarget
// (claude-hook-bridge.md §4.11 CHB-028; execution-model.md EM-015d-RFD, EM-015d-RIA).
//
// Atomic task-artifact writes that the daemon MUST perform AFTER
// MaterializeClaudeSettings + EnsureWorktreeTrust and BEFORE SubstrateSpawn
// (CHB-028 + CHB-029 ordering window).
//
// Bead: hk-9ow36

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// three values for review-loop, but single-mode is valid at MVH.
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

	// Each launch overwrites the file with content for the current
	// (run_id, phase, iteration) tuple. Review-loop phase transitions
	// (impl → reviewer → impl-resume) reuse the same worktree; each is
	// its own logical launch with its own task content, so overwrite is
	// expected, not a collision. The ReAttach field is retained for
	// callers who want to short-circuit on a same-tuple existing file
	// (daemon restart finding its own prior write); when true and the
	// file is present, return early as a no-op.
	if payload.ReAttach {
		if _, err := os.Stat(target); err == nil {
			return nil
		}
	}

	// Build content.
	content := buildAgentTaskContent(payload)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: constructed content is empty for bead %q run %q",
			ErrTaskFileEmpty, payload.BeadID, payload.RunID)
	}

	// Ensure .harmonik/ directory exists in the worktree before writing.
	// git worktree add creates the worktree root but not its .harmonik/ subdirectory;
	// this MkdirAll is idempotent and safe to call on every launch.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("workspace: WriteAgentTask: MkdirAll %q: %w", filepath.Dir(target), err)
	}

	// Atomic write per WM-026.
	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteAgentTask: atomic write %q: %w", target, err)
	}

	// Post-write assertion: file MUST exist and be non-empty.
	fi, err := os.Stat(target) //nolint:gosec // G304: path constructed from workspacePath + known suffix
	if err != nil {
		return fmt.Errorf("%w: stat after write failed for %q: %v", ErrTaskFileEmpty, target, err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("%w: file is zero bytes after write at %q", ErrTaskFileEmpty, target)
	}

	// Gitignore hygiene is an operator-setup obligation (hk-jvzc2): the parent
	// repo's root .gitignore MUST cover .harmonik/* before the daemon runs. The
	// daemon no longer mutates the worktree .gitignore per-launch — silent edits
	// leaked into the parent repo's working tree across dogfood runs.
	return nil
}

// buildAgentTaskContent constructs the UTF-8 Markdown content for agent-task.md
// per the CHB-028 content shape.
//
// Every task file ends with a ## Session Completion section that instructs
// Claude to run `/quit` after committing the work.  This is the mechanism that
// causes Claude's Stop hook to fire, which triggers the `outcome_emitted`
// envelope via harmonik hook-relay, which unblocks the daemon's workloop
// (CHB-028 §session-completion-instruction, hk-cmybm).
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
	// Worktree-discipline guidance (hk-6zylj): inject explicit instructions
	// telling the implementer to keep ALL file paths inside its worktree.
	// Root cause of cross-contamination: implementers running discovery
	// commands like `find /Users/gb/github/harmonik/internal -name '*.go'`
	// receive MAIN-repo absolute paths back, then Write/Edit to those
	// paths. The work lands in main; the worktree branch tip never moves;
	// daemon never sees a commit on the run branch. This block is rendered
	// IMMEDIATELY before the Task Description so it precedes any reading
	// the implementer does of its brief.
	if p.WorkspacePath != "" {
		sb.WriteString("\n## Worktree Discipline (CRITICAL — read first)\n\n")
		sb.WriteString(fmt.Sprintf("Your working directory is `%s`.\n", p.WorkspacePath))
		sb.WriteString("ALL file paths you read, write, or edit MUST be inside this worktree.\n")
		sb.WriteString("NEVER use absolute paths that begin with the MAIN repo root outside your worktree — writing there silently loses your work because the daemon merges from THIS worktree, not main.\n\n")
		sb.WriteString("When running discovery commands (find, grep, ls, rg), use relative paths anchored to your worktree, NOT the main repo:\n\n")
		sb.WriteString("  CORRECT:   find . -name '*.go'\n")
		sb.WriteString(fmt.Sprintf("  CORRECT:   find %s/internal -name '*.go'\n", p.WorkspacePath))
		sb.WriteString("  WRONG:     find /Users/gb/github/harmonik/internal -name '*.go'   (main repo — your edits will be lost)\n\n")
		sb.WriteString("If a discovery command returns paths under the main repo root, translate them into your worktree before reading or editing.\n")
	}

	// Bead Lifecycle prohibition (hk-4jipv, hk-2hb2y): rendered BEFORE the Task
	// Description so implementers see it before reading their brief.  Prior
	// placement at end-of-file meant agents stopped reading after the task body
	// and never reached this guard.  Consistent with the Session Completion
	// comment block below (which documents the same guard from the daemon side).
	sb.WriteString("\n## Bead Lifecycle (CRITICAL — read before acting)\n\n")
	sb.WriteString("DO NOT run `br close`, `br update --status closed`, or any terminal bead transition from inside this worktree.\n")
	sb.WriteString("The daemon owns all bead lifecycle transitions (open → in_progress → closed/failed).\n")
	sb.WriteString("Running `br close` from the worktree causes premature closure that leaks to the parent repo even when no implementation has landed.\n")
	sb.WriteString("Your job is to implement, commit, and `/quit`. The daemon will close the bead on your behalf after verifying the commit.\n")

	sb.WriteString("\n## Task Description\n\n")
	sb.WriteString(p.Body)
	if !strings.HasSuffix(p.Body, "\n") {
		sb.WriteString("\n")
	}

	// Extra Context section (hk-boiwe): operator-supplied briefing notes.
	// Rendered immediately after Task Description, before phase-specific sections.
	// Omitted entirely when ExtraContext is empty.
	if strings.TrimSpace(p.ExtraContext) != "" {
		sb.WriteString("\n## Extra Context\n\n")
		sb.WriteString(p.ExtraContext)
		if !strings.HasSuffix(p.ExtraContext, "\n") {
			sb.WriteString("\n")
		}
	}

	// Prior-Iteration Context section: present only for implementer-resume and reviewer.
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
			// Derive canonical path from workspacePath if not provided.
			derivedPath := filepath.Join(p.WorkspacePath, ".harmonik",
				fmt.Sprintf("review.iter-%d.json", priorN))
			sb.WriteString(fmt.Sprintf("reviewer-feedback: %s\n", derivedPath))
		}
		if p.PriorVerdictSummary != "" {
			sb.WriteString(fmt.Sprintf("prior-verdict-summary: %s\n", p.PriorVerdictSummary))
		}

	case "reviewer":
		sb.WriteString("\n## Prior-Iteration Context\n\n")
		sb.WriteString(fmt.Sprintf("review_base_sha: %s\n", p.ReviewBaseSHA))
		sb.WriteString(fmt.Sprintf("review_head_sha: %s\n", p.ReviewHeadSHA))
	}

	// Session Completion section (hk-cmybm): every task file instructs Claude
	// to run /quit after completing and committing the work.  In interactive TUI
	// mode, the Stop hook fires on session exit (/quit or Ctrl-C) — NOT after
	// each assistant response.  Without /quit, the daemon's workloop sits at
	// sess.Wait() forever because the claude process remains alive at the REPL.
	//
	// Commit-before-close guard (hk-2hb2y): agents MUST NOT run `br close` from
	// inside the worktree — bead lifecycle transitions are owned by the daemon.
	// Running `br close` without a commit causes the closure to leak into the
	// parent repo's .beads/issues.jsonl even though no implementation landed.
	//
	// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
	// Bead ref: hk-2hb2y (commit-before-close guard).
	sb.WriteString("\n## Session Completion\n\n")
	sb.WriteString("IMPORTANT: You MUST run `/quit` as your final action after committing all work.\n")
	sb.WriteString("Do not ask the user to run it — you must type `/quit` yourself and submit it.\n")
	sb.WriteString("The daemon cannot detect that your task is complete until you exit this session.\n")
	sb.WriteString("Failure to run `/quit` will leave the workflow permanently stalled.\n")

	return sb.String()
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

	// Gitignore hygiene is an operator-setup obligation (hk-jvzc2); no per-run
	// worktree-.gitignore edit happens here.
	return nil
}

// buildReviewerFeedbackContent constructs the Markdown content for the
// reviewer-feedback file per EM-015d-RFD.
func buildReviewerFeedbackContent(p ReviewerFeedbackPayload) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Reviewer feedback — iteration %d\n\n", p.PriorIteration))
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

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: atomic write %q: %w", target, err)
	}

	// Gitignore hygiene is an operator-setup obligation (hk-jvzc2); no per-run
	// worktree-.gitignore edit happens here.
	return nil
}

// buildReviewTargetContent constructs the Markdown content for review-target.md
// per EM-015d-RIA.
func buildReviewTargetContent(p ReviewTargetPayload) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Review target — bead %s, iteration %d\n\n", p.BeadID, p.Iteration))

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

	// Prior verdicts section: omit entirely when iteration = 1 (no prior verdicts).
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

	// Hints section: omit when empty.
	if strings.TrimSpace(p.ReviewerHints) != "" {
		sb.WriteString("\n## Hints\n\n")
		sb.WriteString(p.ReviewerHints)
		if !strings.HasSuffix(p.ReviewerHints, "\n") {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
