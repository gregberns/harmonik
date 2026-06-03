package core

// eventpayloads_hqwn59_prop_helpers_test.go — shared test generators used by
// the property-test files for the hqwn59 event-payload Valid() methods.
//
// Naming: drawXxx helpers follow the rapid draw convention; validXxx vars are
// slices of declared constants used with rapid.SampledFrom.
//
// Refs: hk-qgzso (property-test coverage uplift).

import (
	"pgregory.net/rapid"
)

// drawNonEmptyString returns a non-empty string drawn by rapid.
func drawNonEmptyString(rt *rapid.T, label string) string {
	return rapid.StringN(1, 64, -1).Draw(rt, label)
}

// allReconciliationTriggers lists all declared ReconciliationTrigger constants.
var allReconciliationTriggers = []ReconciliationTrigger{
	ReconciliationTriggerStartup,
	ReconciliationTriggerOnDemand,
	ReconciliationTriggerScheduled,
	ReconciliationTriggerDivergenceDetected,
}

// allDivergenceKinds lists all declared DivergenceKind constants.
var allDivergenceKinds = []DivergenceKind{
	DivergenceKindCheckpointMissing,
	DivergenceKindBeadsClosedNoCommit,
	DivergenceKindJSONLReferencesMissingCommit,
	DivergenceKindParseFailure,
	DivergenceKindSchemaMismatch,
	DivergenceKindLogMissing,
}

// allOperatorEscalationReasons lists all declared OperatorEscalationReason constants.
var allOperatorEscalationReasons = []OperatorEscalationReason{
	OperatorEscalationReasonCat6aInvestigatorEscalated,
	OperatorEscalationReasonCat6bAutoEscalated,
	OperatorEscalationReasonCat3StaleWrite,
	OperatorEscalationReasonBudgetExhausted,
	OperatorEscalationReasonMergeConflict,
	OperatorEscalationReasonGateEscalated,
	OperatorEscalationReasonOtherVerdictDriven,
}

// allDivergenceInconclusiveReasons lists all declared DivergenceInconclusiveReason constants.
var allDivergenceInconclusiveReasons = []DivergenceInconclusiveReason{
	DivergenceInconclusiveReasonNoAuthorityReference,
	DivergenceInconclusiveReasonAuthorityUnavailable,
}

// allBeadTerminalTransitionOps lists all declared BeadTerminalTransitionOp constants.
var allBeadTerminalTransitionOps = []BeadTerminalTransitionOp{
	BeadTerminalTransitionOpClaim,
	BeadTerminalTransitionOpClose,
	BeadTerminalTransitionOpReopen,
}

// allPolicyCostBounds lists all declared PolicyCostBound constants.
var allPolicyCostBounds = []PolicyCostBound{
	PolicyCostBoundASTSteps,
	PolicyCostBoundWallClock,
}

// allPolicyEvalIODeterminisms lists all declared PolicyEvalIODeterminism constants.
var allPolicyEvalIODeterminisms = []PolicyEvalIODeterminism{
	PolicyEvalIODeterminismDeterministic,
	PolicyEvalIODeterminismBestEffort,
}

// allWorkspaceMergeStatuses lists all declared WorkspaceMergeStatus constants.
var allWorkspaceMergeStatuses = []WorkspaceMergeStatus{
	WorkspaceMergeStatusPending,
	WorkspaceMergeStatusMerged,
}

// allAgentRateLimitStatuses lists all declared AgentRateLimitStatus constants.
var allAgentRateLimitStatuses = []AgentRateLimitStatus{
	AgentRateLimitStatusActive,
	AgentRateLimitStatusCleared,
}

// allShedPolicies lists all declared ShedPolicy / BusOverflowShedPolicy constants.
var allShedPolicies = []ShedPolicy{
	ShedPolicyFsyncSpilled,
	ShedPolicyOrdinaryDropped,
	ShedPolicyLossyDropped,
}

// allErrorCategories lists all declared ErrorCategory constants.
var allErrorCategories = []ErrorCategory{
	ErrorCategoryTransient,
	ErrorCategoryStructural,
	ErrorCategoryDeterministic,
	ErrorCategoryCanceled,
	ErrorCategoryBudget,
	ErrorCategorySkillProvisioningFailed,
	ErrorCategoryProtocolMismatch,
	ErrorCategoryOverflow,
	ErrorCategoryPanic,
}

// allReconciliationCategories lists all declared ReconciliationCategory constants.
var allReconciliationCategories = []ReconciliationCategory{
	ReconciliationCategoryCat0,
	ReconciliationCategoryCat1,
	ReconciliationCategoryCat2,
	ReconciliationCategoryCat3,
	ReconciliationCategoryCat3a,
	ReconciliationCategoryCat3b,
	ReconciliationCategoryCat3c,
	ReconciliationCategoryCat4,
	ReconciliationCategoryCat5,
	ReconciliationCategoryCat6a,
	ReconciliationCategoryCat6b,
}

// allVerdicts lists all declared Verdict constants.
var allVerdicts = []Verdict{
	VerdictResumeHere,
	VerdictResumeWithContext,
	VerdictResetToCheckpoint,
	VerdictReopenBead,
	VerdictAcceptCloseWithNote,
	VerdictNoOpAccept,
	VerdictEscalateToHuman,
}

// allDivergenceCorroborations lists all declared DivergenceCorroboration constants.
var allDivergenceCorroborations = []DivergenceCorroboration{
	DivergenceCorroborationGitCorroborated,
	DivergenceCorroborationBeadsCorroborated,
}
