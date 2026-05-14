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

// ErrTaskFileCollision is returned by WriteAgentTask when a pre-existing
// agent-task.md is found at materialization time and the caller has NOT
// indicated a re-attach path (AgentTaskPayload.ReAttach = false).
//
// Per claude-hook-bridge.md §4.11 CHB-028: a pre-existing agent-task.md at
// materialization time on a fresh launch is a fatal structural error; the
// caller MUST NOT exec Claude and MUST emit agent_failed.
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
//   - ReAttach: when true, WriteAgentTask skips the collision check and returns
//     nil if agent-task.md already exists (idempotent re-attach path per
//     CHB-028 re-launch semantics).
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
	// When false (default), a pre-existing file is ErrTaskFileCollision.
	ReAttach bool
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
// # Collision semantics
//
// A pre-existing agent-task.md on a fresh launch (payload.ReAttach = false)
// is a fatal structural error: returns ErrTaskFileCollision (wrapped). The
// caller MUST NOT exec Claude.
//
// On re-attach (payload.ReAttach = true), a pre-existing file is a no-op —
// the existing file is idempotent for a given (run_id, phase, iteration) tuple.
//
// # Validation
//
// payload.Body MUST be non-empty; an empty Body returns ErrTaskFileEmpty.
//
// # Gitignore hygiene
//
// Appends .harmonik/agent-task* to the worktree .gitignore if absent, in the
// same function call (though not in the same atomic operation as the file write;
// the gitignore append is idempotent and best-effort tolerant per WM-013e
// worktree scope).
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

	// Collision check (skip on re-attach path).
	if !payload.ReAttach {
		if _, err := os.Stat(target); err == nil {
			// File exists on a fresh launch — fatal collision.
			return fmt.Errorf("%w: agent-task.md already exists at %q (not on re-attach path)",
				ErrTaskFileCollision, target)
		}
	} else {
		// Re-attach: if file already exists, treat as idempotent no-op.
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

	// Gitignore hygiene: ensure .harmonik/agent-task* is excluded from commits.
	if err := ensureWorktreeGitignore(workspacePath, AgentTaskGitignoreLine); err != nil {
		return fmt.Errorf("workspace: WriteAgentTask: gitignore hygiene: %w", err)
	}

	return nil
}

// buildAgentTaskContent constructs the UTF-8 Markdown content for agent-task.md
// per the CHB-028 content shape.
func buildAgentTaskContent(p AgentTaskPayload) string {
	var sb strings.Builder

	sb.WriteString("# Harmonik Task\n\n")
	sb.WriteString(fmt.Sprintf("bead_id: %s\n", p.BeadID))
	sb.WriteString(fmt.Sprintf("title: %s\n", p.Title))
	sb.WriteString(fmt.Sprintf("phase: %s\n", p.Phase))
	sb.WriteString(fmt.Sprintf("iteration: %d\n", p.Iteration))
	sb.WriteString(fmt.Sprintf("run_id: %s\n", p.RunID))
	sb.WriteString(fmt.Sprintf("workspace_path: %s\n", p.WorkspacePath))
	sb.WriteString("\n## Task Description\n\n")
	sb.WriteString(p.Body)
	if !strings.HasSuffix(p.Body, "\n") {
		sb.WriteString("\n")
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
// Appends .harmonik/reviewer-feedback* to the worktree .gitignore if absent.
func WriteReviewerFeedback(payload ReviewerFeedbackPayload) error {
	target := ReviewerFeedbackPath(payload.WorkspacePath, payload.PriorIteration)
	content := buildReviewerFeedbackContent(payload)

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewerFeedback: atomic write %q: %w", target, err)
	}

	// Gitignore hygiene.
	if err := ensureWorktreeGitignore(payload.WorkspacePath, ReviewerFeedbackGitignoreLine); err != nil {
		return fmt.Errorf("workspace: WriteReviewerFeedback: gitignore hygiene: %w", err)
	}

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
// Appends .harmonik/review-target.md to the worktree .gitignore if absent.
func WriteReviewTarget(payload ReviewTargetPayload) error {
	target := ReviewTargetPath(payload.WorkspacePath)
	content := buildReviewTargetContent(payload)

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: atomic write %q: %w", target, err)
	}

	// Gitignore hygiene.
	if err := ensureWorktreeGitignore(payload.WorkspacePath, ReviewTargetGitignoreLine); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: gitignore hygiene: %w", err)
	}

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
