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

// ErrTaskFileEmpty is returned by WriteAgentTask when the constructed file
// content is empty, or when the post-write assertion finds the file absent or
// empty.
//
// Per CHB-028 Invariant: an empty or absent agent-task.md after the write step
// is a fatal structural error.
var ErrTaskFileEmpty = errors.New("workspace: TaskFileEmpty")

// AgentTaskPayload carries the fields the CHB-028 content shape needs for the
// per-launch task-delivery file (agent-task.md).
type AgentTaskPayload struct {
	// HARMONIK_BEAD_ID. Use "none" when not bead-tied.
	BeadID string

	// The bead title, or run_id when not bead-tied.
	Title string

	// The review-loop phase: "implementer-initial" | "implementer-resume" |
	// "reviewer". Empty is a single-mode dispatch, which is also valid.
	Phase string

	// 1-based iteration index (LaunchSpec.iteration_count).
	Iteration int

	// HARMONIK_RUN_ID (UUIDv7).
	RunID string

	// Absolute path to the workspace root.
	WorkspacePath string

	// The bead body verbatim, or the operator-provided task string.
	// MUST NOT be empty.
	Body string

	// Absolute path to the prior iteration's feedback file, named verbatim in
	// the Prior-Iteration Context section when Phase = "implementer-resume".
	//
	// NOTHING SETS THIS TODAY. Every production caller leaves it empty, so the
	// brief falls back to the path resumeFeedbackPath derives —
	// .harmonik/reviewer-feedback.iter-<N-1>.md, the file WriteReviewerFeedback
	// actually writes. It used to derive .harmonik/review.iter-<N-1>.json, which
	// only the (now deleted) ArchiveVerdict would have produced.
	PriorVerdictFile string

	// One-line summary of the prior verdict, e.g. "REQUEST_CHANGES — address
	// flagged issues before proceeding". MAY be empty.
	PriorVerdictSummary string

	// Base commit SHA for the diff under review.
	// Required when Phase = "reviewer". Ignored otherwise.
	ReviewBaseSHA string

	// Head commit SHA for the diff under review.
	// Required when Phase = "reviewer". Ignored otherwise.
	ReviewHeadSHA string

	// The daemon is re-attaching to an existing session. WriteAgentTask then
	// returns nil immediately if agent-task.md is already present. When false,
	// the existing file is overwritten — a review-loop phase transition is a
	// normal overwrite, not an error.
	ReAttach bool

	// Optional operator-supplied text rendered as an "## Extra Context" section
	// after the Task Description (hk-boiwe). Empty omits the section. Carries
	// predecessor-commit SHAs, dependency-landing notes, or orchestrator briefs
	// that are not part of the bead body.
	ExtraContext string

	// The resolved lands_on branch for this run per WM-005b (hk-mtm0w),
	// rendered as base_branch so the implementer can rebase against
	// origin/$BaseBranch before exiting. Empty omits the header line.
	BaseBranch string

	// Completion is the launching harness's own declared completion mode —
	// pass h.Completion() verbatim. It selects the wording of the Session
	// Completion section, because how an agent is told to finish is a
	// property of the harness it runs on and of nothing else (hk-quit-
	// instruction-not-portable-ms55w).
	//
	// The zero value is CompletionEventStreamThenQuit, the claude form, so an
	// unset field renders what every caller rendered before this field
	// existed.
	Completion handlercontract.CompletionMode
}

// AgentTaskPath returns ${workspace_path}/.harmonik/agent-task.md, the
// per-launch task-delivery file per claude-hook-bridge.md §4.11 CHB-028.
func AgentTaskPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "agent-task.md")
}

// ReviewerFeedbackPath returns .harmonik/reviewer-feedback.iter-<priorIteration>.md
// under workspacePath, per execution-model.md EM-015d-RFD.
func ReviewerFeedbackPath(workspacePath string, priorIteration int) string {
	return filepath.Join(workspacePath, ".harmonik",
		fmt.Sprintf("reviewer-feedback.iter-%d.md", priorIteration))
}

// ReviewTargetPath returns ${workspace_path}/.harmonik/review-target.md, the
// reviewer input artifact per EM-015d-RIA and workspace-model.md §6.2 WM-RIA-001.
func ReviewTargetPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "review-target.md")
}

// WriteAgentTask materializes the per-launch task-delivery file at
// ${workspace_path}/.harmonik/agent-task.md per CHB-028.
//
// # Ordering obligation
//
// MUST be called AFTER MaterializeClaudeSettings (WM-040a) and
// EnsureWorktreeTrust (WM-040b) and BEFORE SubstrateSpawn. See CHB-028
// materialization timing and CHB-029 ordering.
//
// Each call renders the caller's (run_id, phase, iteration) tuple and
// overwrites any prior file; see the ReAttach field for the one exception.
//
// Does not touch .gitignore (hk-jvzc2).
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
	return renderProse("agent-task.md.tmpl", p)
}

// ReviewerFeedbackPayload carries the inputs for WriteReviewerFeedback.
type ReviewerFeedbackPayload struct {
	// Absolute path to the workspace root.
	WorkspacePath string

	// The just-completed iteration ordinal (1-indexed); names the file.
	PriorIteration int

	// The prior reviewer's verdict string.
	Verdict string

	// The prior reviewer's flags. MAY be nil or empty.
	Flags []string

	// The full notes text from the prior reviewer verdict.
	Notes string
}

// WriteReviewerFeedback materializes the reviewer-feedback delivery file at
// ${workspace_path}/.harmonik/reviewer-feedback.iter-<N-1>.md per
// execution-model.md EM-015d-RFD.
//
// The daemon MUST call this BEFORE launching the implementer-resume pane;
// only after this file exists on disk may the paste-inject occur.
func WriteReviewerFeedback(payload ReviewerFeedbackPayload) error {
	target := ReviewerFeedbackPath(payload.WorkspacePath, payload.PriorIteration)
	content := buildReviewerFeedbackContent(payload)
	if content == "" {
		return fmt.Errorf("%w: rendered reviewer feedback is empty for %q", ErrTaskFileEmpty, target)
	}

	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewerFeedback: atomic write %q: %w", target, err)
	}

	return nil
}

func buildReviewerFeedbackContent(p ReviewerFeedbackPayload) string {
	return renderProse("reviewer-feedback.md.tmpl", p)
}

// ReviewTargetPayload carries the inputs for WriteReviewTarget.
type ReviewTargetPayload struct {
	// Absolute path to the workspace root.
	WorkspacePath string

	BeadID string

	// The current iteration ordinal (1-indexed).
	Iteration int

	BeadTitle string

	// The bead body verbatim.
	BeadBody string

	// Task-branch fork point commit SHA (parent_commit from the Workspace
	// record per WM-026 / Run.context.parent_commit).
	BaseSHA string

	// HEAD of the task branch at reviewer-launch time.
	HeadSHA string
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
// The file is overwritten on each reviewer launch, not archived.
func WriteReviewTarget(payload ReviewTargetPayload) error {
	target := ReviewTargetPath(payload.WorkspacePath)
	content := buildReviewTargetContent(payload)
	if content == "" {
		return fmt.Errorf("%w: rendered review target is empty for %q", ErrTaskFileEmpty, target)
	}

	if err := os.MkdirAll(filepath.Dir(target), core.HarmonikDirMode); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: MkdirAll %q: %w", filepath.Dir(target), err)
	}
	if err := atomicWriteWithParentFsync(target, []byte(content)); err != nil {
		return fmt.Errorf("workspace: WriteReviewTarget: atomic write %q: %w", target, err)
	}

	return nil
}

func buildReviewTargetContent(p ReviewTargetPayload) string {
	return renderProse("review-target.md.tmpl", p)
}
