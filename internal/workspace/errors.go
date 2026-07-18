package workspace

import "errors"

// Error taxonomy for the workspace subsystem — workspace-model.md §8.
//
// Each sentinel names one of the twelve error classes that the workspace manager
// (S06) MUST classify and route downstream. Three dimensions are fixed for every
// class:
//
//   - Triggered-when — the detection condition the daemon observes.
//   - Workspace transition — the lifecycle-state consequence (or absence thereof).
//   - Downstream routing — where the error propagates after detection.
//
// Callers that need to inspect the class string (e.g. for event-bus payloads)
// use [Class]. errors.Is walks the Unwrap chain, so any error that wraps one
// of these sentinels at any depth is correctly identified.

// ErrWorkspaceAlreadyExists is returned when create_workspace observes an
// existing directory at the canonical worktree path.
//
// Triggered when: create_workspace observes an existing directory at the
// canonical path (workspace-model.md §8 / WM-002).
//
// Workspace transition: none — the workspace record is not created.
//
// Downstream routing: orchestrator reports run-create failure; reconciliation
// Cat 3c may fire on reboot if a prior run's terminal transition did not
// complete.
var ErrWorkspaceAlreadyExists = errors.New("workspace: WorkspaceAlreadyExists")

// ErrRunIDReuseForbidden is returned when create_workspace observes that the
// run_id has been dispatched before, violating WM-034.
//
// Triggered when: create_workspace observes that run_id has been dispatched
// before (workspace-model.md §8 / WM-034).
//
// Workspace transition: none.
//
// Downstream routing: orchestrator reports run-create failure; this is a
// caller-error class.
var ErrRunIDReuseForbidden = errors.New("workspace: RunIdReuseForbidden")

// ErrWorktreeCreationFailed is returned when git worktree add returns non-zero
// (bad parent_commit, concurrent git lock, disk full).
//
// Triggered when: git worktree add returns non-zero
// (workspace-model.md §8 / WM-001).
//
// Workspace transition: workspace remains in initial — the created state is
// never reached.
//
// Downstream routing: orchestrator routes to run-create failure;
// operator-observability per [operator-nfr.md §4.1 ON-001].
var ErrWorktreeCreationFailed = errors.New("workspace: WorktreeCreationFailed")

// ErrLeaseLockHeldByOrphan is returned when create_workspace or launch_session
// observes a live lease-lock file belonging to a prior daemon generation.
//
// Triggered when: create_workspace / launch_session observes a live lease-lock
// file belonging to a prior daemon generation per WM-013c / HC-044a
// (workspace-model.md §8).
//
// Workspace transition: workspace stays out of leased state.
//
// Downstream routing: launch fail-fast per [handler-contract.md §4.10
// HC-044a]; reconciliation Cat 6a investigator if correlation reveals a lost
// lease.
var ErrLeaseLockHeldByOrphan = errors.New("workspace: LeaseLockHeldByOrphan")

// ErrLeaseAlreadyHeld is returned when WriteLeaseLockAtomic finds an existing
// lease-lock file at the target path — a second claimant on the same workspace
// path must fail rather than silently overwrite the holder's lease (test-and-set
// acquisition per WM-013a; single-holder invariant).
var ErrLeaseAlreadyHeld = errors.New("workspace: LeaseAlreadyHeld")

// ErrSidecarWriteFailed is returned when the metadata sidecar write fails on
// I/O (disk full, permissions, concurrent file conflict).
//
// Triggered when: metadata sidecar write fails on I/O
// (workspace-model.md §8 / WM-026).
//
// Workspace transition: workspace stays out of leased state.
//
// Downstream routing: run reported failed; operator-observability.
var ErrSidecarWriteFailed = errors.New("workspace: SidecarWriteFailed")

// ErrMergeConflictUnresolvable is returned when implementer re-dispatch or an
// all-mechanical branch per WM-022a produces no successful merge.
//
// Triggered when: implementer re-dispatch or all-mechanical-branch per WM-022a
// produces no successful merge (workspace-model.md §8 / WM-022).
//
// Workspace transition: conflict-resolving → discarded.
//
// Downstream routing: merge_conflict_escalation emission per WM-023.
var ErrMergeConflictUnresolvable = errors.New("workspace: MergeConflictUnresolvable")

// ErrInterruptOnTerminalWorkspace is returned when an operator-control or
// reconciliation signal targets a merged or discarded workspace.
//
// Triggered when: operator-control or reconciliation signal targets a merged /
// discarded workspace (workspace-model.md §8 / WM-037a).
//
// Workspace transition: none — terminal states are absorbing per WM-037a.
//
// Downstream routing: silent reject; operator-observability log entry only.
var ErrInterruptOnTerminalWorkspace = errors.New("workspace: InterruptOnTerminalWorkspace")

// ErrRefNameInvalid is returned when integration-branch template substitution
// (WM-006a) produces a name that fails git check-ref-format after the canonical
// fallback, or the transformed name is empty or collides case-insensitively.
//
// Triggered when: integration-branch template substitution (WM-006a) produces a
// name that fails git check-ref-format after canonical fallback, or the
// transformed name is empty / collides case-insensitively
// (workspace-model.md §8 / WM-006a).
//
// Workspace transition: workspace remains in initial state.
//
// Downstream routing: orchestrator reports run-create failure;
// operator-observability.
var ErrRefNameInvalid = errors.New("workspace: RefNameInvalid")

// ErrBareWorktreeNoLease is returned when discovery (WM-003a) finds a
// registered worktree directory with no lease-lock and no sessions.
//
// Triggered when: discovery (WM-003a) finds a registered worktree directory
// with no lease-lock and no sessions; evidence type "bare-worktree-no-lease"
// (workspace-model.md §8 / WM-003a).
//
// Workspace transition: none — directory is an orphan.
//
// Downstream routing: reconciliation Cat 3 via evidence type
// "bare-worktree-no-lease".
var ErrBareWorktreeNoLease = errors.New("workspace: BareWorktreeNoLease")

// ErrSidecarWithoutLease is returned when discovery (WM-003a) finds a
// registered worktree with sidecars but no lease-lock.
//
// Triggered when: discovery (WM-003a) finds a registered worktree with sidecars
// but no lease-lock; evidence type "sidecar-without-lease"
// (workspace-model.md §8 / WM-003a).
//
// Workspace transition: none — directory is an orphan.
//
// Downstream routing: reconciliation Cat 3 via evidence type
// "sidecar-without-lease".
var ErrSidecarWithoutLease = errors.New("workspace: SidecarWithoutLease")

// ErrGitignoreWriteForbidden is returned when WM-013e detects that .gitignore
// is missing required entries AND the daemon lacks write permission.
//
// Triggered when: WM-013e detects .gitignore is missing required entries AND
// the daemon lacks write permission (workspace-model.md §8 / WM-013e).
//
// Workspace transition: startup-fail — the daemon refuses to start.
//
// Downstream routing: daemon refuses to start; operator must fix .gitignore
// permissions.
var ErrGitignoreWriteForbidden = errors.New("workspace: GitignoreWriteForbidden")

// ErrGitVersionTooOld is returned when WM-ENV-002 detects installed git below
// version 2.34 at daemon startup.
//
// Triggered when: WM-ENV-002 detects installed git below 2.34 at daemon startup
// (workspace-model.md §8 / WM-ENV-002).
//
// Workspace transition: startup-fail — the daemon refuses to start.
//
// Downstream routing: daemon refuses to start; operator must upgrade git.
var ErrGitVersionTooOld = errors.New("workspace: GitVersionTooOld")

// ErrNotFound is returned by ArchiveVerdict when the source verdict file
// ${workspace_path}/.harmonik/review.json does not exist at the time of the
// archive call.
//
// Triggered when: ArchiveVerdict is called but .harmonik/review.json is absent
// (workspace-model.md §4.7.WM-027a / T-WM-016).
//
// Workspace transition: none — the caller decides how to handle a missing
// verdict source; likely a malformed-reviewer-outcome per WM-027a §(e).
//
// Downstream routing: caller routes per handler-contract.md §4.6 failure rules.
var ErrNotFound = errors.New("workspace: NotFound")

// Class returns the canonical error-class string for err as defined in §8 of
// specs/workspace-model.md.
//
// The return values are:
//   - ""                             if err is nil or does not match any known class.
//   - "WorkspaceAlreadyExists"       if errors.Is(err, ErrWorkspaceAlreadyExists).
//   - "RunIdReuseForbidden"          if errors.Is(err, ErrRunIDReuseForbidden).
//   - "WorktreeCreationFailed"       if errors.Is(err, ErrWorktreeCreationFailed).
//   - "LeaseLockHeldByOrphan"        if errors.Is(err, ErrLeaseLockHeldByOrphan).
//   - "SidecarWriteFailed"           if errors.Is(err, ErrSidecarWriteFailed).
//   - "MergeConflictUnresolvable"    if errors.Is(err, ErrMergeConflictUnresolvable).
//   - "InterruptOnTerminalWorkspace" if errors.Is(err, ErrInterruptOnTerminalWorkspace).
//   - "RefNameInvalid"               if errors.Is(err, ErrRefNameInvalid).
//   - "BareWorktreeNoLease"          if errors.Is(err, ErrBareWorktreeNoLease).
//   - "SidecarWithoutLease"          if errors.Is(err, ErrSidecarWithoutLease).
//   - "GitignoreWriteForbidden"      if errors.Is(err, ErrGitignoreWriteForbidden).
//   - "GitVersionTooOld"             if errors.Is(err, ErrGitVersionTooOld).
//   - "NotFound"                     if errors.Is(err, ErrNotFound).
//
// errors.Is walks the full Unwrap chain, so any error that wraps a class
// sentinel at any depth is correctly classified.
func Class(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrWorkspaceAlreadyExists):
		return "WorkspaceAlreadyExists"
	case errors.Is(err, ErrRunIDReuseForbidden):
		return "RunIdReuseForbidden"
	case errors.Is(err, ErrWorktreeCreationFailed):
		return "WorktreeCreationFailed"
	case errors.Is(err, ErrLeaseLockHeldByOrphan):
		return "LeaseLockHeldByOrphan"
	case errors.Is(err, ErrSidecarWriteFailed):
		return "SidecarWriteFailed"
	case errors.Is(err, ErrMergeConflictUnresolvable):
		return "MergeConflictUnresolvable"
	case errors.Is(err, ErrInterruptOnTerminalWorkspace):
		return "InterruptOnTerminalWorkspace"
	case errors.Is(err, ErrRefNameInvalid):
		return "RefNameInvalid"
	case errors.Is(err, ErrBareWorktreeNoLease):
		return "BareWorktreeNoLease"
	case errors.Is(err, ErrSidecarWithoutLease):
		return "SidecarWithoutLease"
	case errors.Is(err, ErrGitignoreWriteForbidden):
		return "GitignoreWriteForbidden"
	case errors.Is(err, ErrGitVersionTooOld):
		return "GitVersionTooOld"
	case errors.Is(err, ErrNotFound):
		return "NotFound"
	default:
		return ""
	}
}
