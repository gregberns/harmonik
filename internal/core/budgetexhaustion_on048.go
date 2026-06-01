package core

// budgetexhaustion_on048.go — ON-048: Exhaustion protocol (4-step sequence).
//
// ON-048 requires that on budget exhaustion (any category reaches 100%) the
// enforcing subsystem (agent runner for per-run budgets per CP §4.5) MUST
// execute a deterministic 4-step sequence:
//
//  1. Emit budget_exhausted (event-model.md §8.4.3); tag category and scope
//     via EV's structured-fields mechanism. Payload shape is EV-owned.
//  2. Terminate the in-flight LLM call or tool invocation at the next safe
//     boundary: post-chunk for token budgets; post-iteration for iterations
//     budgets; post-step for wall-clock budgets.
//  3. Route the run through the exhaustion-routing policy: default is
//     pause-and-escalate — the run transitions to a failed state with a fallback
//     verdict per RC-018, and the daemon MAY enter the paused state if the
//     policy declares pause-on-exhaustion=true (default: false).
//  4. Emit dispatch_deferred per §8 code 18 if exhaustion cascades to a
//     multi-run ceiling breach.
//
// The protocol is deterministic (mechanism-tagged). The pause-vs-escalate
// decision in step (3) is an operator-policy decision, not a spec decision.
//
// This file declares the pure, I/O-free layer:
//
//   - ExhaustionProtocolStep        — typed label for each of the four steps.
//   - ExhaustionSafeBoundary        — typed enum for the three safe-boundary
//                                     points keyed to BudgetResource.
//   - ExhaustionRoutingPolicy       — per-policy struct governing step (3).
//   - ExhaustionProtocolHandlerStep — struct pairing a step label with its
//                                     canonical description.
//   - ExhaustionProtocolSequence    — returns the canonical ordered 4-step slice.
//   - SafeBoundaryForResource       — maps a BudgetResource to the correct
//                                     ExhaustionSafeBoundary per ON-048 step (2).
//   - DefaultExhaustionRoutingPolicy — returns the spec-mandated default policy.
//
// The new DispatchDeferredReasonBudgetExhaustedCascade constant is declared in
// dispatchdeferredreason.go alongside the existing machine-ceiling reason.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048.
// Refs: hk-sx9r.67

// ExhaustionProtocolStep is the typed label for one step of the ON-048
// exhaustion protocol sequence.
type ExhaustionProtocolStep string

const (
	// ExhaustionProtocolStepEmitBudgetExhausted is step (1): emit budget_exhausted
	// per [event-model.md §8.4.3] with category and scope tagged via EV's
	// structured-fields mechanism. This is the first fsync-boundary write;
	// payload shape is EV-owned.
	ExhaustionProtocolStepEmitBudgetExhausted ExhaustionProtocolStep = "1-emit-budget-exhausted"

	// ExhaustionProtocolStepTerminateAtSafeBoundary is step (2): terminate the
	// in-flight LLM call or tool invocation at the next safe boundary determined
	// by the budget resource: post-chunk for token budgets; post-iteration for
	// iterations budgets; post-step for wall-clock budgets.
	ExhaustionProtocolStepTerminateAtSafeBoundary ExhaustionProtocolStep = "2-terminate-at-safe-boundary"

	// ExhaustionProtocolStepRouteExhaustionPolicy is step (3): route the run
	// through the exhaustion-routing policy. Default is pause-and-escalate —
	// the run transitions to failed state with fallback verdict per RC-018.
	// The daemon MAY additionally enter the paused state if the policy declares
	// pause-on-exhaustion=true (default: false).
	ExhaustionProtocolStepRouteExhaustionPolicy ExhaustionProtocolStep = "3-route-exhaustion-policy"

	// ExhaustionProtocolStepEmitDispatchDeferredIfCascade is step (4): emit
	// dispatch_deferred per §8 code 18 ONLY IF the exhaustion cascades to a
	// multi-run ceiling breach. This step is a conditional no-op when no
	// ceiling breach occurs.
	ExhaustionProtocolStepEmitDispatchDeferredIfCascade ExhaustionProtocolStep = "4-emit-dispatch-deferred-if-cascade"
)

// ExhaustionSafeBoundary identifies the point at which an in-flight LLM call
// or tool invocation MUST be terminated per ON-048 step (2).
//
// The correct boundary is keyed to the exhausted BudgetResource:
//   - Tokens budget → PostChunk (terminate after the current output chunk)
//   - Iterations budget → PostIteration (terminate after the current tool-use cycle)
//   - WallClock budget → PostStep (terminate after the current tool invocation step)
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048 step (2).
type ExhaustionSafeBoundary string

const (
	// ExhaustionSafeBoundaryPostChunk is the safe boundary for token budgets.
	// The enforcing subsystem waits for the current LLM output chunk to complete
	// before terminating the in-flight call; no partial chunk is discarded.
	ExhaustionSafeBoundaryPostChunk ExhaustionSafeBoundary = "post-chunk"

	// ExhaustionSafeBoundaryPostIteration is the safe boundary for iterations
	// budgets. The enforcing subsystem waits for the current agent tool-use cycle
	// (one iteration) to complete before terminating.
	ExhaustionSafeBoundaryPostIteration ExhaustionSafeBoundary = "post-iteration"

	// ExhaustionSafeBoundaryPostStep is the safe boundary for wall-clock budgets.
	// The enforcing subsystem waits for the current tool invocation step to
	// complete before terminating.
	ExhaustionSafeBoundaryPostStep ExhaustionSafeBoundary = "post-step"
)

// SafeBoundaryForResource returns the ExhaustionSafeBoundary that MUST be used
// when a budget of the given BudgetResource is exhausted, per ON-048 step (2):
//
//   - BudgetResourceTokens          → ExhaustionSafeBoundaryPostChunk
//   - BudgetResourceIterations      → ExhaustionSafeBoundaryPostIteration
//   - BudgetResourceWallClockSeconds → ExhaustionSafeBoundaryPostStep
//
// Returns the zero string and false for any unrecognised BudgetResource.
// The ok=false sentinel signals a caller error (unknown resource) rather than
// a spec-level gap; callers MUST check ok before using the returned boundary.
func SafeBoundaryForResource(r BudgetResource) (ExhaustionSafeBoundary, bool) {
	switch r {
	case BudgetResourceTokens:
		return ExhaustionSafeBoundaryPostChunk, true
	case BudgetResourceIterations:
		return ExhaustionSafeBoundaryPostIteration, true
	case BudgetResourceWallClockSeconds:
		return ExhaustionSafeBoundaryPostStep, true
	default:
		return "", false
	}
}

// ExhaustionRoutingPolicy is the per-policy configuration that governs step (3)
// of the ON-048 exhaustion protocol.
//
// The spec mandates a default of pause-and-escalate with pause-on-exhaustion=false.
// Use DefaultExhaustionRoutingPolicy to obtain the spec default. Operator policy
// files MAY declare pause-on-exhaustion=true to additionally enter the paused
// daemon state on exhaustion.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048 step (3).
type ExhaustionRoutingPolicy struct {
	// PauseOnExhaustion controls whether the daemon enters the paused state after
	// routing the exhausted run to the fallback verdict (RC-018). Default: false.
	// When true, the daemon additionally transitions to paused after completing the
	// fallback-verdict route.
	PauseOnExhaustion bool `json:"pause_on_exhaustion"`
}

// DefaultExhaustionRoutingPolicy returns the spec-mandated default exhaustion
// routing policy: pause-and-escalate with PauseOnExhaustion=false.
//
// This is the lowest-precedence layer; operator policy may override
// PauseOnExhaustion=true at any higher-precedence config layer.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048 step (3).
func DefaultExhaustionRoutingPolicy() ExhaustionRoutingPolicy {
	return ExhaustionRoutingPolicy{
		PauseOnExhaustion: false,
	}
}

// ExhaustionProtocolHandlerStep pairs an ExhaustionProtocolStep label with its
// canonical human-readable description and metadata for documentation and
// invariant-checking.
type ExhaustionProtocolHandlerStep struct {
	// Label is the typed ExhaustionProtocolStep constant for this step.
	// Required (must be a valid constant).
	Label ExhaustionProtocolStep

	// Description is the canonical prose description of the step and its
	// spec obligations. Required (non-empty).
	Description string

	// IsConditional is true when this step is a conditional no-op — i.e., the
	// action is only taken when a specific condition holds. Step (4) is
	// conditional on a multi-run ceiling breach occurring.
	IsConditional bool
}

// ExhaustionProtocolSequence returns the canonical ordered slice of all four
// ON-048 exhaustion protocol steps for the agent runner enforcing path.
//
// The returned slice is the normative documentation of the step ordering per
// ON-048. Any reordering requires a spec-level amendment. Tests verify the
// count and ordering.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-048.
func ExhaustionProtocolSequence() []ExhaustionProtocolHandlerStep {
	return []ExhaustionProtocolHandlerStep{
		{
			Label: ExhaustionProtocolStepEmitBudgetExhausted,
			Description: "emit budget_exhausted (event-model.md §8.4.3) with category and scope " +
				"tagged via EV's structured-fields mechanism; payload shape is EV-owned; " +
				"emitter MUST supply run_id, budget_ref, attempted_dispatch_cost, category, scope",
			IsConditional: false,
		},
		{
			Label: ExhaustionProtocolStepTerminateAtSafeBoundary,
			Description: "terminate in-flight LLM call or tool invocation at the next safe " +
				"boundary: post-chunk for token budgets (BudgetResourceTokens); " +
				"post-iteration for iterations budgets (BudgetResourceIterations); " +
				"post-step for wall-clock budgets (BudgetResourceWallClockSeconds); " +
				"safe boundary determined by SafeBoundaryForResource(exhaustedResource)",
			IsConditional: false,
		},
		{
			Label: ExhaustionProtocolStepRouteExhaustionPolicy,
			Description: "route the run through ExhaustionRoutingPolicy: default pause-and-escalate " +
				"— run transitions to failed state with fallback verdict per RC-018; " +
				"daemon additionally enters paused state only when PauseOnExhaustion=true (default: false)",
			IsConditional: false,
		},
		{
			Label: ExhaustionProtocolStepEmitDispatchDeferredIfCascade,
			Description: "emit dispatch_deferred{reason=budget_exhausted_cascade} per §8 code 18 " +
				"ONLY IF the exhaustion cascades to a multi-run ceiling breach; " +
				"this step is a no-op when no ceiling breach occurs (IsConditional=true)",
			IsConditional: true,
		},
	}
}
