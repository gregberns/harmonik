package daemon

// launchspecbuild.go — review-loop LaunchSpec constructors (T-WM-019).
//
// Provides three builder functions that assemble a handlercontract.LaunchSpec
// for each call site in the review-loop dispatch:
//
//   - buildLaunchSpecImplementerInitial: first implementer launch in a cycle
//   - buildLaunchSpecImplementerResume:  subsequent implementer launch resuming a session
//   - buildLaunchSpecReviewer:           reviewer launch (fresh Claude session)
//
// Each builder takes a "base" LaunchSpec (with all required fields already
// populated by the caller) and layers the review-loop-specific optional fields
// on top per specs/handler-contract.md §6.1 HC-006.
//
// Field rules (HC-006):
//
//   - workflow_mode:      always set to "review-loop" for all three phases
//   - phase:             set to the caller's phase constant
//   - iteration_count:   set to the caller-supplied iteration (1-based, 1..3)
//   - claude_session_id: set only for implementer-resume; omitted for initial and reviewer
//
// Spec refs:
//   - specs/handler-contract.md §6.1 HC-006 (LaunchSpec field rules)
//   - specs/execution-model.md §4.3 EM-015d (review-loop lifecycle)
//
// Bead: hk-7om2q.19

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// reviewLoopMode is the workflow_mode string for review-loop dispatches.
// Used as the WorkflowMode value in all three LaunchSpec builders.
// Matches core.WorkflowModeReviewLoop per execution-model.md §4.3.EM-012a.
const reviewLoopMode = string(core.WorkflowModeReviewLoop)

// buildLaunchSpecImplementerInitial returns a LaunchSpec for the first
// implementer launch of a review-loop cycle (phase = implementer-initial,
// iteration = 1).
//
// HC-006 field shape:
//   - WorkflowMode:   &"review-loop"
//   - Phase:          &ReviewLoopPhaseImplementerInitial
//   - IterationCount: &iterationCount
//   - ClaudeSessionID: nil (no prior session on initial launch)
//
// The caller must supply a valid base spec with all required fields set.
// iterationCount must be positive; the spec enforces 1..3 for review-loop per
// operator-nfr.md §4.1 ON-004, but the builder does not hard-cap it here —
// the review-loop driver enforces the cap before calling the builder.
func buildLaunchSpecImplementerInitial(base handlercontract.LaunchSpec, iterationCount int) (handlercontract.LaunchSpec, error) {
	if iterationCount <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"daemon: buildLaunchSpecImplementerInitial: iterationCount must be positive, got %d",
			iterationCount,
		)
	}
	mode := reviewLoopMode
	phase := handlercontract.ReviewLoopPhaseImplementerInitial
	spec := base
	spec.WorkflowMode = &mode
	spec.Phase = &phase
	spec.IterationCount = &iterationCount
	spec.ClaudeSessionID = nil // no prior session on first launch
	return spec, nil
}

// buildLaunchSpecImplementerResume returns a LaunchSpec for a subsequent
// implementer launch resuming a prior Claude Code session
// (phase = implementer-resume).
//
// HC-006 field shape:
//   - WorkflowMode:    &"review-loop"
//   - Phase:           &ReviewLoopPhaseImplementerResume
//   - IterationCount:  &iterationCount
//   - ClaudeSessionID: &claudeSessionID (required for resume per HC-006)
//
// The caller must supply a valid base spec, a positive iterationCount, and a
// non-empty claudeSessionID captured from the initial implementer launch per
// execution-model.md §4.3 EM-015d (RunContextKeyClaudeSessionID).
func buildLaunchSpecImplementerResume(base handlercontract.LaunchSpec, iterationCount int, claudeSessionID string) (handlercontract.LaunchSpec, error) {
	if iterationCount <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"daemon: buildLaunchSpecImplementerResume: iterationCount must be positive, got %d",
			iterationCount,
		)
	}
	if claudeSessionID == "" {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"daemon: buildLaunchSpecImplementerResume: claudeSessionID must be non-empty (required for phase=implementer-resume per HC-006)",
		)
	}
	mode := reviewLoopMode
	phase := handlercontract.ReviewLoopPhaseImplementerResume
	spec := base
	spec.WorkflowMode = &mode
	spec.Phase = &phase
	spec.IterationCount = &iterationCount
	spec.ClaudeSessionID = &claudeSessionID
	return spec, nil
}

// buildLaunchSpecReviewer returns a LaunchSpec for the reviewer launch in a
// review-loop cycle (phase = reviewer).
//
// HC-006 field shape:
//   - WorkflowMode:    &"review-loop"
//   - Phase:           &ReviewLoopPhaseReviewer
//   - IterationCount:  &iterationCount
//   - ClaudeSessionID: nil (each reviewer launch is a fresh Claude session)
//
// The caller must supply a valid base spec and a positive iterationCount.
// ClaudeSessionID is explicitly omitted: each reviewer is a fresh Claude
// session per specs/handler-contract.md §6.1 HC-006.
func buildLaunchSpecReviewer(base handlercontract.LaunchSpec, iterationCount int) (handlercontract.LaunchSpec, error) {
	if iterationCount <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"daemon: buildLaunchSpecReviewer: iterationCount must be positive, got %d",
			iterationCount,
		)
	}
	mode := reviewLoopMode
	phase := handlercontract.ReviewLoopPhaseReviewer
	spec := base
	spec.WorkflowMode = &mode
	spec.Phase = &phase
	spec.IterationCount = &iterationCount
	spec.ClaudeSessionID = nil // reviewer launches are always fresh sessions
	return spec, nil
}
