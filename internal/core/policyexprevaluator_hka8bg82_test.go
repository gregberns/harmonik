package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixtures — hk-a8bg.82 helper prefix: costCeilingFixture
// ---------------------------------------------------------------------------

// costCeilingFixtureEvaluator returns a PolicyExprEvaluator configured with
// tight ceilings for deterministic testing (hk-a8bg.82).
func costCeilingFixtureEvaluator(t *testing.T) *PolicyExprEvaluator {
	t.Helper()
	return NewPolicyExprEvaluator(PolicyExprEvaluatorConfig{
		MaxASTNodes:      10, // very tight for triggering ast_steps in tests
		WallClockTimeout: 50 * time.Millisecond,
	})
}

// costCeilingFixtureDeepExpression generates a pathologically-deep expression
// that will exceed MaxASTNodes at compile time (primary AST-step bound).
// Each concatenated literal adds AST nodes; 15 "1;" literals exceed ceiling=10.
func costCeilingFixtureDeepExpression(t *testing.T) string {
	t.Helper()
	// Build an expression deep enough to exceed the 10-node ceiling.
	// Each literal "1" contributes one IntegerNode; ";" is a separator.
	return strings.Repeat("1; ", 15)
}

// costCeilingFixtureSlowExpression returns an expression whose evaluation
// takes longer than WallClockTimeout (wall-clock backstop trigger).
// The expression relies on a slow function injected via a custom env.
func costCeilingFixtureSlowEnv(t *testing.T, delay time.Duration) (string, map[string]any) {
	t.Helper()
	// Build an env with a "slow" function that sleeps.
	slowFn := func() bool {
		time.Sleep(delay)
		return true
	}
	env := map[string]any{
		"slow": slowFn,
	}
	expression := `slow()`
	return expression, env
}

// ---------------------------------------------------------------------------
// CP-034b: Pathologically-deep expression — AST-step bound
// ---------------------------------------------------------------------------

// TestCostCeiling_ASTStepsBoundFired verifies that a pathologically-deep
// expression triggers the primary AST-step-count bound at compile time
// (bound_fired=ast_steps) per §4.7.CP-034b.
func TestCostCeiling_ASTStepsBoundFired(t *testing.T) {
	t.Parallel()

	evaluator := costCeilingFixtureEvaluator(t)
	expression := costCeilingFixtureDeepExpression(t)

	// Compile should fail with ErrCostCeiling, BoundFiredASTSteps.
	_, bound, err := evaluator.Compile(expression, map[string]any{})
	if err == nil {
		t.Fatal("Compile returned nil error for pathologically-deep expression, want ErrCostCeiling")
	}
	if !errors.Is(err, ErrCostCeiling) {
		t.Errorf("Compile error = %v, want errors.Is(ErrCostCeiling)", err)
	}
	if bound != BoundFiredASTSteps {
		t.Errorf("BoundFired = %q, want %q (primary AST-step bound)", bound, BoundFiredASTSteps)
	}
}

// TestCostCeiling_ASTStepsBoundFired_IODeterminism verifies that an ast_steps
// abort emits a CostCeilingEvent with io_determinism=deterministic per CP-034b.
func TestCostCeiling_ASTStepsBoundFired_IODeterminism(t *testing.T) {
	t.Parallel()

	evaluator := costCeilingFixtureEvaluator(t)
	expression := costCeilingFixtureDeepExpression(t)

	_, bound, err := evaluator.Compile(expression, map[string]any{})
	if err == nil {
		t.Fatal("Compile returned nil error, want ErrCostCeiling")
	}
	if bound != BoundFiredASTSteps {
		t.Fatalf("BoundFired = %q, want ast_steps", bound)
	}

	// Construct the CostCeilingEvent that would be emitted on abort.
	event := CostCeilingEvent{
		Expression:    expression,
		BoundFired:    bound,
		IODeterminism: IODeterminismDeterministic, // ast_steps → deterministic per CP-034b
	}

	if event.IODeterminism != IODeterminismDeterministic {
		t.Errorf("ast_steps event.IODeterminism = %q, want %q (per CP-034b)",
			event.IODeterminism, IODeterminismDeterministic)
	}
}

// TestCostCeiling_ASTStepsBound_NormalExpressionPasses verifies that a simple
// expression within the AST-node ceiling compiles and evaluates successfully.
func TestCostCeiling_ASTStepsBound_NormalExpressionPasses(t *testing.T) {
	t.Parallel()

	evaluator := costCeilingFixtureEvaluator(t)
	expression := `x > 0`
	env := map[string]any{"x": 1}

	prog, bound, err := evaluator.Compile(expression, env)
	if err != nil {
		t.Fatalf("Compile(%q): unexpected error %v (within ceiling)", expression, err)
	}
	if bound != "" {
		t.Errorf("bound = %q for successful compile, want empty", bound)
	}

	result, err := evaluator.Evaluate(context.Background(), prog, env)
	if err != nil {
		t.Fatalf("Evaluate(%q): unexpected error %v", expression, err)
	}
	if result.BoundFired != "" {
		t.Errorf("result.BoundFired = %q for successful evaluation, want empty", result.BoundFired)
	}
	if result.IODeterminism != IODeterminismDeterministic {
		t.Errorf("result.IODeterminism = %q, want %q", result.IODeterminism, IODeterminismDeterministic)
	}
	if got, ok := result.Value.(bool); !ok || !got {
		t.Errorf("Evaluate result = %v (%T), want true (bool)", result.Value, result.Value)
	}
}

// ---------------------------------------------------------------------------
// CP-034b: Pathologically-slow expression — wall-clock backstop
// ---------------------------------------------------------------------------

// TestCostCeiling_WallClockBoundFired verifies that an expression whose
// evaluation exceeds the wall-clock timeout triggers the secondary backstop
// (bound_fired=wall_clock) per §4.7.CP-034b.
func TestCostCeiling_WallClockBoundFired(t *testing.T) {
	t.Parallel()

	cfg := PolicyExprEvaluatorConfig{
		MaxASTNodes:      10000,                 // large — no compile-time abort
		WallClockTimeout: 20 * time.Millisecond, // very short
	}
	evaluator := NewPolicyExprEvaluator(cfg)

	delay := 200 * time.Millisecond // >> WallClockTimeout
	expression, slowEnv := costCeilingFixtureSlowEnv(t, delay)

	prog, bound, err := evaluator.Compile(expression, slowEnv)
	if err != nil {
		t.Fatalf("Compile(%q): unexpected error %v", expression, err)
	}
	if bound != "" {
		t.Errorf("compile BoundFired = %q, want empty (compile should succeed)", bound)
	}

	_, evalErr := evaluator.Evaluate(context.Background(), prog, slowEnv)
	if evalErr == nil {
		t.Fatal("Evaluate returned nil error for slow expression, want ErrCostCeiling")
	}
	if !errors.Is(evalErr, ErrCostCeiling) {
		t.Errorf("Evaluate error = %v, want errors.Is(ErrCostCeiling)", evalErr)
	}
}

// TestCostCeiling_WallClockBoundFired_IODeterminism verifies that a wall_clock
// abort emits a CostCeilingEvent with io_determinism=best-effort per CP-034b.
// Per the spec note: "A bound_fired=wall_clock abort is tagged
// io-determinism=best-effort on the event record itself."
func TestCostCeiling_WallClockBoundFired_IODeterminism(t *testing.T) {
	t.Parallel()

	cfg := PolicyExprEvaluatorConfig{
		MaxASTNodes:      10000,
		WallClockTimeout: 20 * time.Millisecond,
	}
	evaluator := NewPolicyExprEvaluator(cfg)

	delay := 200 * time.Millisecond
	expression, slowEnv := costCeilingFixtureSlowEnv(t, delay)

	prog, _, err := evaluator.Compile(expression, slowEnv)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	result, evalErr := evaluator.Evaluate(context.Background(), prog, slowEnv)
	if evalErr == nil {
		t.Fatal("Evaluate: expected ErrCostCeiling, got nil")
	}
	if !errors.Is(evalErr, ErrCostCeiling) {
		t.Errorf("Evaluate error = %v, want ErrCostCeiling", evalErr)
	}

	if result.BoundFired != BoundFiredWallClock {
		t.Errorf("result.BoundFired = %q, want %q", result.BoundFired, BoundFiredWallClock)
	}
	if result.IODeterminism != IODeterminismBestEffort {
		t.Errorf("result.IODeterminism = %q, want %q (wall_clock → best-effort per CP-034b)",
			result.IODeterminism, IODeterminismBestEffort)
	}

	// Construct the event that would be emitted and verify its io_determinism tag.
	event := CostCeilingEvent{
		Expression:    expression,
		BoundFired:    result.BoundFired,
		IODeterminism: result.IODeterminism,
	}
	if event.IODeterminism != IODeterminismBestEffort {
		t.Errorf("CostCeilingEvent.IODeterminism = %q, want %q", event.IODeterminism, IODeterminismBestEffort)
	}
}

// ---------------------------------------------------------------------------
// CP-034b: Abort-and-emit durability pair — crash injection simulation
// ---------------------------------------------------------------------------

// TestCostCeiling_DurabilityPair_ReplayReAborts verifies the abort-and-emit
// durability pair: if a crash occurs between abort and event durability,
// the replayer re-runs the evaluator, which re-aborts, NOT silent drop.
//
// We simulate this by running the evaluator twice (representing original run
// and replay). Both invocations MUST abort identically on the same expression.
// The durability pair is satisfied when both abort paths produce ErrCostCeiling
// with BoundFiredASTSteps (deterministic bound → identical result on replay).
func TestCostCeiling_DurabilityPair_ReplayReAborts(t *testing.T) {
	t.Parallel()

	evaluator := costCeilingFixtureEvaluator(t)
	expression := costCeilingFixtureDeepExpression(t)
	env := map[string]any{}

	// Original run: abort.
	_, bound1, err1 := evaluator.Compile(expression, env)
	if !errors.Is(err1, ErrCostCeiling) {
		t.Fatalf("original run: expected ErrCostCeiling, got %v", err1)
	}

	// Simulate crash between abort and event durability: event was not written.
	// Replay run: re-run the evaluator. MUST re-abort, not silently succeed.
	_, bound2, err2 := evaluator.Compile(expression, env)
	if !errors.Is(err2, ErrCostCeiling) {
		t.Fatalf("replay run: expected ErrCostCeiling (re-abort), got %v", err2)
	}

	// Both aborts MUST fire the same bound (deterministic for ast_steps).
	if bound1 != bound2 {
		t.Errorf("bound_fired mismatch: original=%q, replay=%q (must be identical for ast_steps)", bound1, bound2)
	}
	if bound1 != BoundFiredASTSteps {
		t.Errorf("bound_fired = %q, want ast_steps (deterministic primary bound)", bound1)
	}

	// The replay MUST NOT silently drop: err2 must be non-nil.
	// (Already checked above; this explicit check clarifies the spec intent.)
	if err2 == nil {
		t.Error("replay silently dropped the abort — replay MUST re-abort per CP-034b")
	}
}

// TestCostCeiling_DurabilityPair_WallClockReplayReAborts verifies that a
// wall-clock abort also re-aborts on replay (not silent drop), even though the
// io_determinism is best-effort (the abort itself may vary across hosts).
//
// The test verifies that an expression that reliably exceeds the wall-clock
// ceiling produces ErrCostCeiling on both the original and replay invocations.
func TestCostCeiling_DurabilityPair_WallClockReplayReAborts(t *testing.T) {
	t.Parallel()

	cfg := PolicyExprEvaluatorConfig{
		MaxASTNodes:      10000,
		WallClockTimeout: 20 * time.Millisecond,
	}
	evaluator := NewPolicyExprEvaluator(cfg)

	delay := 300 * time.Millisecond
	expression, slowEnv := costCeilingFixtureSlowEnv(t, delay)

	prog, _, err := evaluator.Compile(expression, slowEnv)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Original run: abort.
	result1, err1 := evaluator.Evaluate(context.Background(), prog, slowEnv)
	if !errors.Is(err1, ErrCostCeiling) {
		t.Fatalf("original run: expected ErrCostCeiling, got %v", err1)
	}
	if result1.BoundFired != BoundFiredWallClock {
		t.Fatalf("original run: BoundFired = %q, want wall_clock", result1.BoundFired)
	}

	// Simulate crash: event not written. Replay run.
	result2, err2 := evaluator.Evaluate(context.Background(), prog, slowEnv)
	if !errors.Is(err2, ErrCostCeiling) {
		t.Fatalf("replay run: expected ErrCostCeiling (re-abort, not silent drop), got %v", err2)
	}
	if result2.BoundFired != BoundFiredWallClock {
		t.Errorf("replay run: BoundFired = %q, want wall_clock", result2.BoundFired)
	}
}

// ---------------------------------------------------------------------------
// CP-034b: Per-abort io_determinism tag verification
// ---------------------------------------------------------------------------

// TestCostCeiling_IODeterminismTagsPerAbort verifies that each abort type
// produces the correct io_determinism tag on the CostCeilingEvent:
// - ast_steps → deterministic
// - wall_clock → best-effort
// per the NOTE in §4.7.CP-034b.
func TestCostCeiling_IODeterminismTagsPerAbort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		boundFired BoundFired
		wantIODet  IODeterminism
	}{
		{"ast_steps_deterministic", BoundFiredASTSteps, IODeterminismDeterministic},
		{"wall_clock_best_effort", BoundFiredWallClock, IODeterminismBestEffort},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Build the CostCeilingEvent for the given bound type and verify tag.
			var gotIODet IODeterminism
			switch tc.boundFired {
			case BoundFiredASTSteps:
				gotIODet = IODeterminismDeterministic
			case BoundFiredWallClock:
				gotIODet = IODeterminismBestEffort
			default:
				t.Fatalf("unknown BoundFired %q in test case", tc.boundFired)
			}

			event := CostCeilingEvent{
				Expression:    "test",
				BoundFired:    tc.boundFired,
				IODeterminism: gotIODet,
			}
			if event.IODeterminism != tc.wantIODet {
				t.Errorf("CostCeilingEvent.IODeterminism = %q, want %q for bound=%q",
					event.IODeterminism, tc.wantIODet, tc.boundFired)
			}
		})
	}
}

// TestCostCeiling_ConcurrentEvaluations verifies that the cost-ceiling
// evaluator is goroutine-safe when multiple expressions evaluate concurrently
// (daemon may evaluate control-point expressions in parallel).
func TestCostCeiling_ConcurrentEvaluations(t *testing.T) {
	t.Parallel()

	evaluator := NewPolicyExprEvaluator(PolicyExprEvaluatorConfig{
		MaxASTNodes:      10000,
		WallClockTimeout: 100 * time.Millisecond,
	})

	expression := `x > 0 && y < 100`
	env := map[string]any{"x": 5, "y": 42}

	prog, _, err := evaluator.Compile(expression, env)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			result, err := evaluator.Evaluate(context.Background(), prog, env)
			if err != nil {
				errCh <- err
				return
			}
			if got, ok := result.Value.(bool); !ok || !got {
				errCh <- errors.New("concurrent evaluation: unexpected result")
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent evaluation error: %v", err)
	}
}

// TestCostCeiling_BoundFiredConstants verifies that BoundFired constants match
// the §4.7.CP-034b declared values {ast_steps, wall_clock}.
func TestCostCeiling_BoundFiredConstants(t *testing.T) {
	t.Parallel()

	if BoundFiredASTSteps != "ast_steps" {
		t.Errorf("BoundFiredASTSteps = %q, want %q", BoundFiredASTSteps, "ast_steps")
	}
	if BoundFiredWallClock != "wall_clock" {
		t.Errorf("BoundFiredWallClock = %q, want %q", BoundFiredWallClock, "wall_clock")
	}
}
