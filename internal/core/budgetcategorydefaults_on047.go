package core

// ON-047 category default constants.
//
// Each constant corresponds to one row in the ON-047 five-row defaults table.
// They are exported so callers can reference the authoritative default without
// embedding magic numbers.
const (
	// DefaultTokenBudgetPerRunTokens is the per-run token budget (200,000 tokens).
	// Per ON-047 row 1: applies to any agentic node; override at node/role policy.
	DefaultTokenBudgetPerRunTokens int64 = 200_000

	// DefaultWallClockBudgetPerRunSeconds is the per-run wall-clock budget
	// (30 minutes expressed as seconds = 1800 s).
	// Per ON-047 row 2: applies to any agentic node; override at node/role policy.
	DefaultWallClockBudgetPerRunSeconds int64 = 1800

	// DefaultIterationsBudgetPerRunIterations is the per-run iterations budget
	// (50 tool-use cycles).
	// Per ON-047 row 3: applies to any agentic node; override at node/role policy.
	DefaultIterationsBudgetPerRunIterations int64 = 50

	// DefaultWallClockBudgetPerReconciliationSeconds is the per-reconciliation-workflow
	// wall-clock budget (10 minutes expressed as seconds = 600 s).
	// Per ON-047 row 4: applies to reconciliation-dispatched investigator runs;
	// override at reconciliation/spec.md §4.4 policy.
	DefaultWallClockBudgetPerReconciliationSeconds int64 = 600

	// DefaultBudgetWarningThreshold is the warning threshold fraction (0.8 = 80%).
	// Per ON-047 row 5 and CP-025: applies to all budget categories; override at
	// control-points.md §4.5 CP-025 policy.
	DefaultBudgetWarningThreshold float64 = 0.8
)

// DefaultCategoryBudgets returns the four concrete default PolicyBudget values
// that implement the ON-047 category defaults table.
//
// These form the lowest-precedence layer in the §4.7 config-precedence stack.
// Higher-precedence layers (operator policy, workflow definition, runtime
// override) may override any field.
//
// The warning threshold (ON-047 row 5) is not a separate PolicyBudget; it is
// the DefaultBudgetWarningThreshold constant (0.8) applied as WarningThreshold
// on all four returned budgets.
//
// Every returned budget satisfies:
//   - CP-022: resource, scope, limit, and warning_threshold all set
//   - ON-047: values match the five-row defaults table
//
// Spec: specs/operator-nfr.md §4.11.ON-047.
func DefaultCategoryBudgets() []PolicyBudget {
	return []PolicyBudget{
		defaultTokenBudgetPerRun(),
		defaultWallClockBudgetPerRun(),
		defaultIterationsBudgetPerRun(),
		defaultWallClockBudgetPerReconciliation(),
	}
}

func defaultTokenBudgetPerRun() PolicyBudget {
	return PolicyBudget{
		Name:             "default-token-per-run",
		Resource:         string(BudgetResourceTokens),
		Scope:            string(BudgetScopePerRun),
		Limit:            DefaultTokenBudgetPerRunTokens,
		WarningThreshold: DefaultBudgetWarningThreshold,
	}
}

func defaultWallClockBudgetPerRun() PolicyBudget {
	return PolicyBudget{
		Name:             "default-wall-clock-per-run",
		Resource:         string(BudgetResourceWallClockSeconds),
		Scope:            string(BudgetScopePerRun),
		Limit:            DefaultWallClockBudgetPerRunSeconds,
		WarningThreshold: DefaultBudgetWarningThreshold,
	}
}

func defaultIterationsBudgetPerRun() PolicyBudget {
	return PolicyBudget{
		Name:             "default-iterations-per-run",
		Resource:         string(BudgetResourceIterations),
		Scope:            string(BudgetScopePerRun),
		Limit:            DefaultIterationsBudgetPerRunIterations,
		WarningThreshold: DefaultBudgetWarningThreshold,
	}
}

func defaultWallClockBudgetPerReconciliation() PolicyBudget {
	return PolicyBudget{
		Name:             "default-wall-clock-per-reconciliation",
		Resource:         string(BudgetResourceWallClockSeconds),
		Scope:            string(BudgetScopePerRun),
		Limit:            DefaultWallClockBudgetPerReconciliationSeconds,
		WarningThreshold: DefaultBudgetWarningThreshold,
	}
}
