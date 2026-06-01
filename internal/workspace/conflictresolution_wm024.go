package workspace

// conflictresolution_wm024.go — conflict-resolver dispatch mechanism (WM-024).
//
// Implements the deterministic (mechanism-tagged) aspects of the conflict-resolution
// re-dispatch harness per workspace-model.md §4.6 WM-024:
//
//   - Attempt-cap constants (default 3, operator-configurable [1, 10]).
//   - ValidateConflictResolutionAttemptCap — daemon-startup guard (WM-024 terminal clause).
//   - EffectiveConflictResolutionAttemptCap — resolves operator override or default.
//   - ShouldDispatchConflictResolver — dispatch-or-escalate decision per WM-022a, WM-023, WM-024.
//   - BuildConflictResolverLaunchSpec — fresh LaunchSpec construction for the re-dispatch.
//
// Cognition (handler reasoning during resolution) is delegated to the implementer's
// handler per handler-contract.md §4.1 and is NOT owned here.
//
// Spec refs:
//   - specs/workspace-model.md §4.6 WM-022, WM-022a, WM-023, WM-024.
//   - specs/handler-contract.md §6.1 HC-006 (LaunchSpec field rules).
//   - specs/operator-nfr.md §4.3 (operator-configurable attempt cap).
//
// Bead ref: hk-8mwo.36.

import (
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// DefaultConflictResolutionAttemptCap is the default conflict-resolution
// re-dispatch attempt cap per workspace-model.md §4.6 WM-024.
//
// "The workspace manager MUST cap conflict-resolution re-dispatch attempts at
// a DEFAULT of THREE (3) attempts per merge-pending cycle."
const DefaultConflictResolutionAttemptCap = 3

// ConflictResolutionAttemptCapMin is the minimum operator-configurable cap per
// WM-024: "lower bound of 1 (no re-dispatch; escalate on first conflict)."
const ConflictResolutionAttemptCapMin = 1

// ConflictResolutionAttemptCapMax is the maximum operator-configurable cap per
// WM-024: "upper bound of 10 (beyond which escalation is heuristically better)."
const ConflictResolutionAttemptCapMax = 10

// ConflictResolutionSkillName is the required_skills entry injected into the
// conflict-resolver LaunchSpec per WM-024 "input shape".
//
// NOTE (OQ-WM-007): the authoritative skill name and provisioning path are
// tracked in OQ-WM-007 pending a handler-contract skill registration. This
// placeholder is used until that registration lands. Per the OQ-WM-007
// default-if-unresolved: the handler relies on the LaunchSpec workspace_path
// and the staged conflict markers; this skill entry is a provisioning hint.
const ConflictResolutionSkillName = "conflict-resolution"

// ErrConflictResolutionCapOutOfRange is returned by ValidateConflictResolutionAttemptCap
// when the operator-configured cap falls outside [ConflictResolutionAttemptCapMin,
// ConflictResolutionAttemptCapMax] per WM-024.
//
// Callers SHOULD use errors.Is to test for this sentinel.
var ErrConflictResolutionCapOutOfRange = errors.New(
	"workspace: conflict-resolution attempt cap must be in [1, 10]",
)

// ValidateConflictResolutionAttemptCap checks that cap is within the
// operator-configurable bound [1, 10] per workspace-model.md §4.6 WM-024.
//
// Operator overrides outside [1, 10] MUST be rejected at daemon startup.
// Returns an error wrapping ErrConflictResolutionCapOutOfRange when cap is
// outside the valid range; returns nil when cap is within [1, 10].
func ValidateConflictResolutionAttemptCap(cap int) error {
	if cap < ConflictResolutionAttemptCapMin || cap > ConflictResolutionAttemptCapMax {
		return fmt.Errorf(
			"workspace: conflict-resolution attempt cap %d is outside [%d, %d]: %w",
			cap, ConflictResolutionAttemptCapMin, ConflictResolutionAttemptCapMax,
			ErrConflictResolutionCapOutOfRange,
		)
	}
	return nil
}

// EffectiveConflictResolutionAttemptCap resolves the effective attempt cap.
//
// When operatorCap is zero (the field was not set by the operator), the
// DefaultConflictResolutionAttemptCap (3) is returned. When operatorCap is
// non-zero, it is returned as-is; the caller MUST have already validated it
// via ValidateConflictResolutionAttemptCap at daemon startup.
//
// The zero-value rule preserves backward-compatible behaviour: existing daemon
// configurations that do not set the cap field automatically use the default.
func EffectiveConflictResolutionAttemptCap(operatorCap int) int {
	if operatorCap == 0 {
		return DefaultConflictResolutionAttemptCap
	}
	return operatorCap
}

// ConflictResolveDecision is the typed outcome of ShouldDispatchConflictResolver.
// It drives the workspace manager's routing decision after conflict detection.
type ConflictResolveDecision string

const (
	// ConflictResolveDispatch indicates the workspace manager MUST dispatch a
	// fresh conflict-resolver LaunchSpec to the implementer handler. The
	// workspace transitions to conflict-resolving per §7.1.
	ConflictResolveDispatch ConflictResolveDecision = "dispatch"

	// ConflictResolveEscalateNullRef indicates implementer_handler_ref is null
	// (all-mechanical task branch per WM-022a). The workspace manager MUST
	// skip re-dispatch and emit merge_conflict_escalation per WM-023 directly.
	ConflictResolveEscalateNullRef ConflictResolveDecision = "escalate-null-ref"

	// ConflictResolveEscalateRetiredHandler indicates the recorded handler class
	// has been retired between initial commit time and merge-time re-dispatch
	// (WM-024 terminal clause). The workspace manager MUST route to WM-023
	// escalation without a silent handler-class remap.
	ConflictResolveEscalateRetiredHandler ConflictResolveDecision = "escalate-retired-handler"

	// ConflictResolveEscalateCapExhausted indicates the attempt cap has been
	// reached (attemptCount >= cap). The workspace manager MUST route to
	// merge_conflict_escalation per WM-022a / WM-023.
	ConflictResolveEscalateCapExhausted ConflictResolveDecision = "escalate-cap-exhausted"
)

// ShouldDispatchConflictResolver returns the routing decision for a conflict
// detected during the merge-pending → conflict-resolving transition per
// workspace-model.md §4.6 WM-022, WM-022a, WM-023, WM-024.
//
// Decision priority (highest to lowest):
//  1. If ref is nil (implementer_handler_ref = null): EscalateNullRef — all-mechanical
//     task branch; the workspace manager MUST skip re-dispatch per WM-022a.
//  2. If isRetired(ref) is true: EscalateRetiredHandler — the recorded handler class
//     has been retired; route to WM-023 escalation per WM-024 terminal clause.
//  3. If attemptCount >= cap: EscalateCapExhausted — attempt cap reached; route to
//     merge_conflict_escalation per WM-022a / WM-023.
//  4. Otherwise: Dispatch — the workspace manager MUST build a fresh LaunchSpec and
//     dispatch to the implementer handler.
//
// Parameters:
//   - ref: the workspace's ImplementerHandlerRef (nil = null / all-mechanical).
//   - attemptCount: number of conflict-resolution re-dispatch attempts already
//     recorded for this merge-pending cycle (0 on first conflict detection).
//   - cap: the effective attempt cap (use EffectiveConflictResolutionAttemptCap).
//   - isRetired: a function that reports whether the given handler class has been
//     retired in the handler registry per WM-024 terminal clause. Callers pass
//     the registry lookup; test callers pass a stub.
func ShouldDispatchConflictResolver(
	ref *core.HandlerRef,
	attemptCount int,
	cap int,
	isRetired func(core.HandlerRef) bool,
) ConflictResolveDecision {
	// Priority 1: null ref — all-mechanical task branch (WM-022a).
	if ref == nil {
		return ConflictResolveEscalateNullRef
	}

	// Priority 2: retired handler class (WM-024 terminal clause).
	if isRetired != nil && isRetired(*ref) {
		return ConflictResolveEscalateRetiredHandler
	}

	// Priority 3: attempt cap exhausted (WM-024 / WM-023).
	if attemptCount >= cap {
		return ConflictResolveEscalateCapExhausted
	}

	// Priority 4: dispatch.
	return ConflictResolveDispatch
}

// ConflictResolverLaunchSpecParams holds the inputs required to build a fresh
// handlercontract.LaunchSpec for a conflict-resolution re-dispatch per WM-024.
//
// Fields mirror the required LaunchSpec fields (handler-contract.md §6.1 HC-006)
// plus WM-024-specific inputs. The caller must populate all required fields;
// optional fields (BeadID) may be nil.
type ConflictResolverLaunchSpecParams struct {
	// RunID is the stable run identifier for this dispatch. Required.
	RunID core.RunID

	// WorkflowID is the workflow containing the run. Required.
	WorkflowID core.WorkflowID

	// NodeID is the conflict-resolution node identifier in the workflow graph.
	// Required per HC-006.
	NodeID core.NodeID

	// AgentType is the agent-type identifier resolved from
	// Workspace.ImplementerHandlerRef (e.g. "agentic-claude"). Required.
	AgentType core.AgentType

	// WorkspacePath is the absolute filesystem path to the EXISTING worktree
	// where conflicts are staged. Required; must point at the workspace whose
	// merge produced the conflict (same worktree, not a new one).
	WorkspacePath string

	// AdditionalSkills is an ordered list of skill names to include AFTER
	// ConflictResolutionSkillName in RequiredSkills. May be nil.
	AdditionalSkills []string

	// SkillSearchPaths is the ordered list of absolute directories searched
	// for skill packages. Required (may be empty slice).
	SkillSearchPaths []string

	// Timeout is the wall-clock budget in seconds for the conflict-resolution
	// session. Required; must be positive.
	Timeout int

	// ProvisioningTimeout is the skill-provisioning deadline in seconds.
	// Required; must be positive. Defaults to the HC-048a value (60) when the
	// caller follows the handler-contract default.
	ProvisioningTimeout int

	// Budget names the Budget in the policy-layer registry for this session.
	// Per WM-024: "Budget: fresh per handler-contract LaunchSpec default; prior
	// session's unused budget NOT inherited." Required.
	Budget core.BudgetRef

	// FreedomProfileRef names the freedom profile governing this session.
	// Required; non-empty.
	FreedomProfileRef string

	// BeadID is the opaque bead correlation identifier. Optional; nil when the
	// run is not bead-tied.
	BeadID *string

	// AttemptNumber is the 1-based conflict-resolution attempt index for this
	// dispatch (1 on first re-dispatch, 2 on second, etc.). Required; must be
	// positive. Recorded in workspace-local events JSONL per §4.3.WM-013b; NOT
	// threaded into the LaunchSpec (Phase + IterationCount co-presence rule in
	// HC-006 requires both or neither; conflict-resolution is not a review-loop
	// phase, so both are omitted).
	AttemptNumber int
}

// BuildConflictResolverLaunchSpec constructs a fresh handlercontract.LaunchSpec
// for a conflict-resolution re-dispatch per workspace-model.md §4.6 WM-024.
//
// Input shape per WM-024:
//   - workspace_path: params.WorkspacePath (existing worktree, NOT a new one).
//   - required_skills: [ConflictResolutionSkillName] + params.AdditionalSkills.
//   - budget: fresh default (params.Budget); prior session budget NOT inherited.
//   - No WorkflowMode or Phase: conflict-resolution is not a review-loop phase.
//   - IterationCount: params.AttemptNumber (1-based attempt index).
//
// Returns a non-nil error when any required field is missing or invalid.
//
// The returned LaunchSpec passes handlercontract.LaunchSpec.Valid() when the
// input params are well-formed. Callers SHOULD call Valid() to confirm before
// passing the spec to handler.Launch.
func BuildConflictResolverLaunchSpec(params ConflictResolverLaunchSpecParams) (handlercontract.LaunchSpec, error) {
	if params.RunID == (core.RunID{}) {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: RunID must be non-zero",
		)
	}
	if params.WorkflowID == (core.WorkflowID{}) {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: WorkflowID must be non-zero",
		)
	}
	if params.NodeID == "" {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: NodeID must be non-empty",
		)
	}
	if params.AgentType == "" {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: AgentType must be non-empty",
		)
	}
	if params.WorkspacePath == "" {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: WorkspacePath must be non-empty",
		)
	}
	if params.Timeout <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: Timeout must be positive, got %d",
			params.Timeout,
		)
	}
	if params.ProvisioningTimeout <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: ProvisioningTimeout must be positive, got %d",
			params.ProvisioningTimeout,
		)
	}
	if !params.Budget.Valid() {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: Budget must be non-empty",
		)
	}
	if params.FreedomProfileRef == "" {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: FreedomProfileRef must be non-empty",
		)
	}
	if params.AttemptNumber <= 0 {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: AttemptNumber must be positive, got %d",
			params.AttemptNumber,
		)
	}

	// Build RequiredSkills: conflict-resolution skill first, then caller-supplied skills.
	// Per OQ-WM-007: if the skill name is unresolved downstream, the handler falls back
	// to the workspace_path's staged conflict markers + transition history.
	skills := make([]string, 0, 1+len(params.AdditionalSkills))
	skills = append(skills, ConflictResolutionSkillName)
	skills = append(skills, params.AdditionalSkills...)

	skillPaths := params.SkillSearchPaths
	if skillPaths == nil {
		skillPaths = []string{}
	}

	spec := handlercontract.LaunchSpec{
		RunID:               params.RunID,
		WorkflowID:          params.WorkflowID,
		NodeID:              params.NodeID,
		AgentType:           params.AgentType,
		WorkspacePath:       params.WorkspacePath,
		RequiredSkills:      skills,
		SkillSearchPaths:    skillPaths,
		Timeout:             params.Timeout,
		ProvisioningTimeout: params.ProvisioningTimeout,
		Budget:              params.Budget,
		FreedomProfileRef:   params.FreedomProfileRef,
		BeadID:              params.BeadID,
		// WorkflowMode, Phase, and IterationCount are all nil: conflict-resolution
		// is NOT a review-loop phase. HC-006 co-presence rule requires Phase and
		// IterationCount to both be present or both absent; omitting both satisfies
		// the rule. Attempt tracking for WM-024 is recorded in the workspace-local
		// events JSONL per §4.3.WM-013b, NOT in the LaunchSpec fields.
		//
		// ClaudeSessionID is nil: each conflict-resolver dispatch is a fresh session
		// per WM-024 ("budget: fresh per handler-contract LaunchSpec default; prior
		// session's unused budget NOT inherited").
		SchemaVersion: handlercontract.LaunchSpecSchemaVersion,
	}

	// Validate before returning so callers get early feedback.
	if err := spec.Valid(); err != nil {
		return handlercontract.LaunchSpec{}, fmt.Errorf(
			"workspace: BuildConflictResolverLaunchSpec: produced invalid LaunchSpec: %w", err,
		)
	}

	return spec, nil
}
