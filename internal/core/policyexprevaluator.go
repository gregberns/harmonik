package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// BoundFired identifies which cost-ceiling bound triggered an evaluation abort
// (specs/control-points.md §4.7.CP-034b).
//
// The payload of the policy_expression_exceeded_cost event MUST carry a
// bound_fired discriminator valued in {ast_steps, wall_clock} per CP-034b.
type BoundFired string

const (
	// BoundFiredASTSteps means the primary AST-step-count bound triggered the
	// abort. This bound is deterministic (CP-034b); the event record carries
	// io_determinism=deterministic.
	BoundFiredASTSteps BoundFired = "ast_steps"

	// BoundFiredWallClock means the secondary wall-clock soft-cap triggered the
	// abort. This bound is non-deterministic across runtime versions and host-
	// clock speed (CP-034b); the event record carries io_determinism=best-effort.
	BoundFiredWallClock BoundFired = "wall_clock"
)

// ErrCostCeiling is the sentinel error returned by PolicyExprEvaluator.Evaluate
// when a policy expression exceeds the harmonik-level cost ceiling per
// §4.7.CP-034b. Callers MUST treat this as a typed ErrDeterministic per
// [handler-contract.md §4.5].
//
// Use errors.Is to check for this sentinel.
var ErrCostCeiling = errors.New("policy expression exceeded cost ceiling")

// PolicyExprEvalResult carries the result of a successful expression evaluation.
// On cost-ceiling abort, PolicyExprEvaluator.Evaluate returns an error wrapping
// ErrCostCeiling instead.
type PolicyExprEvalResult struct {
	// Value is the expression output. Type depends on the ControlPoint Kind
	// per specs/control-points.md §6.4.2.
	Value any

	// BoundFired identifies which bound triggered an abort. Zero value (empty
	// string) when evaluation completed normally.
	BoundFired BoundFired

	// IODeterminism is the per-evaluation io_determinism axis tag per CP-034b.
	// Set to IODeterminismDeterministic on ast_steps abort; IODeterminismBestEffort
	// on wall_clock abort; IODeterminismDeterministic on successful evaluation.
	IODeterminism IODeterminism
}

// PolicyExprEvaluatorConfig is the harmonik-level cost-ceiling configuration
// for PolicyExprEvaluator.
//
// These values are NOT policy-level knobs; they are operator-fixed at daemon
// init per §4.7.CP-034b.
type PolicyExprEvaluatorConfig struct {
	// MaxASTNodes is the compile-time AST-node ceiling (primary static bound).
	// Expressions whose AST exceeds this size fail at compile time.
	// Default: 1000 (harmonik MVH ceiling; operator-fixed, not policy-tunable).
	MaxASTNodes uint

	// WallClockTimeout is the per-evaluation wall-clock soft-cap (secondary
	// backstop per CP-034b). Wall-clock is non-deterministic; it is a backstop.
	// Default: 100ms (harmonik MVH ceiling; operator-fixed, not policy-tunable).
	WallClockTimeout time.Duration
}

// DefaultPolicyExprEvaluatorConfig returns the MVH harmonik-level cost-ceiling
// configuration. Values are operator-fixed and NOT policy-tunable per CP-034b.
func DefaultPolicyExprEvaluatorConfig() PolicyExprEvaluatorConfig {
	return PolicyExprEvaluatorConfig{
		MaxASTNodes:      1000,
		WallClockTimeout: 100 * time.Millisecond,
	}
}

// PolicyExprEvaluator evaluates a policy expression with a harmonik-level cost
// ceiling (specs/control-points.md §4.7.CP-034b).
//
// Two bounds are enforced:
//
//  1. Primary bound — AST node count (deterministic). Expressions whose AST
//     exceeds Config.MaxASTNodes fail at compile time with BoundFiredASTSteps.
//
//  2. Secondary bound — wall-clock soft-cap (best-effort). Evaluations
//     exceeding Config.WallClockTimeout are interrupted via context cancellation
//     with BoundFiredWallClock. Wall-clock aborts are tagged
//     io_determinism=best-effort on the CostCeilingEvent.
//
// On any abort, the caller MUST emit a policy_expression_exceeded_cost event
// with the BoundFired discriminator and per-abort io_determinism tag before
// returning to its caller, per CP-034b. See [CostCeilingEvent].
type PolicyExprEvaluator struct {
	Config PolicyExprEvaluatorConfig
}

// NewPolicyExprEvaluator constructs a PolicyExprEvaluator with the given config.
// Use DefaultPolicyExprEvaluatorConfig() for the MVH harmonik-level ceilings.
func NewPolicyExprEvaluator(cfg PolicyExprEvaluatorConfig) *PolicyExprEvaluator {
	return &PolicyExprEvaluator{Config: cfg}
}

// Compile compiles expression against the given environment, enforcing the
// MaxASTNodes ceiling at compile time (primary static bound per CP-034b).
//
// Returns an error wrapping ErrCostCeiling when the expression exceeds
// MaxASTNodes. Other compilation errors (syntax, type-check) are returned as-is.
func (e *PolicyExprEvaluator) Compile(expression string, env any) (*vm.Program, BoundFired, error) {
	prog, err := expr.Compile(expression, expr.Env(env), expr.MaxNodes(e.Config.MaxASTNodes))
	if err != nil {
		// Check if the error is from the MaxNodes ceiling.
		if isMaxNodesError(err) {
			return nil, BoundFiredASTSteps, fmt.Errorf("%w: %v", ErrCostCeiling, err)
		}
		return nil, "", err
	}
	return prog, "", nil
}

// Evaluate runs a pre-compiled program against env with the wall-clock timeout
// enforced (secondary backstop per CP-034b).
//
// Returns EvalResult with Value on success. Returns an error wrapping
// ErrCostCeiling with BoundFiredWallClock when the evaluation exceeds the
// wall-clock timeout.
//
// The caller MUST emit policy_expression_exceeded_cost before returning to its
// caller on any ErrCostCeiling return per CP-034b.
func (e *PolicyExprEvaluator) Evaluate(ctx context.Context, prog *vm.Program, env any) (PolicyExprEvalResult, error) {
	type runResult struct {
		val any
		err error
	}

	ch := make(chan runResult, 1)

	evalCtx, cancel := context.WithTimeout(ctx, e.Config.WallClockTimeout)
	defer cancel()

	go func() {
		val, err := expr.Run(prog, env)
		ch <- runResult{val: val, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return PolicyExprEvalResult{}, res.err
		}
		return PolicyExprEvalResult{
			Value:         res.val,
			IODeterminism: IODeterminismDeterministic,
		}, nil
	case <-evalCtx.Done():
		return PolicyExprEvalResult{
			BoundFired:    BoundFiredWallClock,
			IODeterminism: IODeterminismBestEffort,
		}, fmt.Errorf("%w: wall-clock timeout %s exceeded", ErrCostCeiling, e.Config.WallClockTimeout)
	}
}

// CostCeilingEvent is the event payload for the policy_expression_exceeded_cost
// event emitted when a policy expression evaluation exceeds the harmonik-level
// cost ceiling (specs/control-points.md §4.7.CP-034b, §6.5).
//
// The abort and event emission form a durability pair: the event MUST reach
// JSONL durability BEFORE the evaluator wrapper returns to its caller per
// CP-034b. On crash between abort and durability, replay MUST re-run the
// evaluator, rely on the ceiling to re-abort, and emit the event on the replay.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism per BoundFired (deterministic for
// ast_steps, best-effort for wall_clock); replay-safety=safe; idempotency=non-idempotent
type CostCeilingEvent struct {
	// Expression is the policy expression that exceeded the ceiling.
	Expression string `json:"expression"`

	// BoundFired identifies which bound triggered the abort.
	// Values: ast_steps (deterministic primary bound) or wall_clock (best-effort backstop).
	BoundFired BoundFired `json:"bound_fired"`

	// IODeterminism is the per-abort io-determinism tag per CP-034b.
	// ast_steps → IODeterminismDeterministic; wall_clock → IODeterminismBestEffort.
	IODeterminism IODeterminism `json:"io_determinism"`
}

// isMaxNodesError heuristically detects whether err is an expr-lang/expr
// MaxNodes compile-time ceiling error. The expr library does not expose a typed
// error for this; we inspect the error string.
func isMaxNodesError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "exceeds maximum allowed nodes")
}

// contains reports whether s contains substr (avoids strings import).
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
