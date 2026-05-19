package core

// HandlerFatalClass is the closed taxonomy of handler-fatal failure classes
// per specs/handler-contract.md §4.5a HC-020a.  A failure is handler-fatal
// iff, with high confidence, every subsequent invocation of the same
// agent_type will fail until external resolution (HP-010).
//
// Only two FailureClass values carry handler-fatal sub-cases at MVH; the
// remaining four (structural, deterministic, canceled, compilation_loop) are
// per-bead and never trigger a handler-wide pause.  This type names those
// sub-cases, not the parent FailureClass values.
type HandlerFatalClass string

const (
	// HandlerFatalClassRateLimit identifies the transient/rate_limit
	// sub-case: the handler-pause controller has observed agent_rate_limited
	// two consecutive times without an intervening agent_rate_limit_cleared,
	// indicating a handler-wide rate-limit condition per HC-020a §(i) and
	// HP-011.  Trip condition: two consecutive agent_rate_limited events per
	// agent_type without an intervening agent_rate_limit_cleared.
	HandlerFatalClassRateLimit HandlerFatalClass = "transient/rate_limit"

	// HandlerFatalClassBudgetAccount identifies the
	// budget_exhausted/handler-account sub-case: the budget point that
	// triggered the budget_exhausted event declares budget_scope =
	// handler-account (session-token cap, daily quota), meaning the
	// exhaustion applies to the entire handler type until reset, per
	// HC-020a §(ii) and HP-012.  Trip condition: immediate on first
	// budget_exhausted event with budget_scope = handler-account.
	HandlerFatalClassBudgetAccount HandlerFatalClass = "budget_exhausted/handler-account"
)

// HandlerFatalSubReason is the sub-reason string carried on bus events
// (agent_rate_limited, budget_exhausted) that are used as classifier inputs.
// Values are declared in handler-contract.md §4.6.HC-025 (rate_limit) and
// execution-model.md §8.5 (budget_exhausted).
type HandlerFatalSubReason string

const (
	// HandlerFatalSubReasonRateLimit is the sub-reason value on the
	// agent_rate_limited event (handler-contract.md §4.6.HC-025).
	HandlerFatalSubReasonRateLimit HandlerFatalSubReason = "rate_limit"

	// HandlerFatalSubReasonHandlerAccount is the budget_scope discriminator
	// on budget_exhausted events (execution-model.md §8.5 / control-points.md
	// §4.5).  Presence of this value means the budget is per-handler-account.
	HandlerFatalSubReasonHandlerAccount HandlerFatalSubReason = "handler-account"
)

// handlerFatalEntry records a row in the closed taxonomy table of
// handler-fatal classes (HC-020a).
type handlerFatalEntry struct {
	class     FailureClass
	subReason HandlerFatalSubReason
	fatal     HandlerFatalClass
}

// handlerFatalTaxonomy is the closed table of handler-fatal class × sub-reason
// combinations at MVH per handler-contract.md §4.5a HC-020a.
//
// Classification MUST be deterministic from structured fields; no cognition
// participates (HC-023).
var handlerFatalTaxonomy = []handlerFatalEntry{
	{
		class:     FailureClassTransient,
		subReason: HandlerFatalSubReasonRateLimit,
		fatal:     HandlerFatalClassRateLimit,
	},
	{
		class:     FailureClassBudgetExhausted,
		subReason: HandlerFatalSubReasonHandlerAccount,
		fatal:     HandlerFatalClassBudgetAccount,
	},
}

// ClassifyHandlerFatal maps a (FailureClass, HandlerFatalSubReason) pair to
// the corresponding HandlerFatalClass using the closed MVH taxonomy per
// handler-contract.md §4.5a HC-020a.
//
// Returns (class, true) when the pair is in the handler-fatal set; returns
// ("", false) for every other combination (per-bead failures that do not
// trigger a handler-wide pause).
//
// Classification is mechanism-tagged: the result is deterministic from
// structured fields alone (HC-023).
func ClassifyHandlerFatal(fc FailureClass, sub HandlerFatalSubReason) (HandlerFatalClass, bool) {
	for _, e := range handlerFatalTaxonomy {
		if e.class == fc && e.subReason == sub {
			return e.fatal, true
		}
	}
	return "", false
}
