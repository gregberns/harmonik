package core

import (
	"pgregory.net/rapid"
)

func drawNonEmptyString(rt *rapid.T, label string) string {
	return rapid.StringN(1, 64, -1).Draw(rt, label)
}

var allReconciliationTriggers = []ReconciliationTrigger{
	ReconciliationTriggerStartup,
	ReconciliationTriggerOnDemand,
	ReconciliationTriggerScheduled,
	ReconciliationTriggerDivergenceDetected,
}

var allDivergenceKinds = []DivergenceKind{
	DivergenceKindCheckpointMissing,
	DivergenceKindBeadsClosedNoCommit,
	DivergenceKindJSONLReferencesMissingCommit,
	DivergenceKindParseFailure,
	DivergenceKindSchemaMismatch,
	DivergenceKindLogMissing,
}

var allOperatorEscalationReasons = []OperatorEscalationReason{
	OperatorEscalationReasonCat6aInvestigatorEscalated,
	OperatorEscalationReasonCat6bAutoEscalated,
	OperatorEscalationReasonCat3StaleWrite,
	OperatorEscalationReasonBudgetExhausted,
	OperatorEscalationReasonMergeConflict,
	OperatorEscalationReasonGateEscalated,
	OperatorEscalationReasonOtherVerdictDriven,
}

var allDivergenceInconclusiveReasons = []DivergenceInconclusiveReason{
	DivergenceInconclusiveReasonNoAuthorityReference,
	DivergenceInconclusiveReasonAuthorityUnavailable,
}

var allBeadTerminalTransitionOps = []BeadTerminalTransitionOp{
	BeadTerminalTransitionOpClaim,
	BeadTerminalTransitionOpClose,
	BeadTerminalTransitionOpReopen,
}

var allPolicyCostBounds = []PolicyCostBound{
	PolicyCostBoundASTSteps,
	PolicyCostBoundWallClock,
}

var allPolicyEvalIODeterminisms = []PolicyEvalIODeterminism{
	PolicyEvalIODeterminismDeterministic,
	PolicyEvalIODeterminismBestEffort,
}

var allWorkspaceMergeStatuses = []WorkspaceMergeStatus{
	WorkspaceMergeStatusPending,
	WorkspaceMergeStatusMerged,
}

var allAgentRateLimitStatuses = []AgentRateLimitStatus{
	AgentRateLimitStatusActive,
	AgentRateLimitStatusCleared,
}

var allShedPolicies = []ShedPolicy{
	ShedPolicyFsyncSpilled,
	ShedPolicyOrdinaryDropped,
	ShedPolicyLossyDropped,
}

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

var allVerdicts = []Verdict{
	VerdictResumeHere,
	VerdictResumeWithContext,
	VerdictResetToCheckpoint,
	VerdictReopenBead,
	VerdictAcceptCloseWithNote,
	VerdictNoOpAccept,
	VerdictEscalateToHuman,
}

var allDivergenceCorroborations = []DivergenceCorroboration{
	DivergenceCorroborationGitCorroborated,
	DivergenceCorroborationBeadsCorroborated,
}
