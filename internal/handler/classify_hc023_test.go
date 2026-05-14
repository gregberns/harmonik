package handler_test

// classify_hc023_test.go — tests for ClassifyExitState (HC-023).
//
// HC-023: Mapping a subprocess exit state or adapter-detected condition to a
// sentinel class MUST be deterministic from structured fields (exit code,
// payload flags, typed adapter return). No cognition in classification.
//
// Spec: specs/handler-contract.md §4.5.HC-023, §8.
// Bead: hk-8i31.27.
//
// Helper prefix: hc023Fixture (per implementer-protocol.md §Helper-prefix discipline).

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// HC-023: Priority 1 — CtxCanceled supersedes all other fields.
// ─────────────────────────────────────────────────────────────────────────────

// TestClassifyExitState_CtxCanceled_SupersedesAll verifies that CtxCanceled==true
// always produces ErrCanceled regardless of exit code or adapter condition (§8.4).
func TestClassifyExitState_CtxCanceled_SupersedesAll(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		state   handler.ExitState
		wantErr error
	}{
		{
			name: "canceled_zero_exit",
			state: handler.ExitState{
				CtxCanceled: true,
				ExitCode:    0,
			},
			wantErr: handler.ErrCanceled,
		},
		{
			name: "canceled_nonzero_exit",
			state: handler.ExitState{
				CtxCanceled: true,
				ExitCode:    1,
			},
			wantErr: handler.ErrCanceled,
		},
		{
			name: "canceled_adapter_transient",
			state: handler.ExitState{
				CtxCanceled:   true,
				AdapterResult: handler.AdapterConditionTransient,
			},
			wantErr: handler.ErrCanceled,
		},
		{
			name: "canceled_adapter_deterministic",
			state: handler.ExitState{
				CtxCanceled:   true,
				AdapterResult: handler.AdapterConditionDeterministic,
			},
			wantErr: handler.ErrCanceled,
		},
		{
			name: "canceled_adapter_structural",
			state: handler.ExitState{
				CtxCanceled:   true,
				AdapterResult: handler.AdapterConditionStructural,
			},
			wantErr: handler.ErrCanceled,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := handler.ClassifyExitState(tc.state)
			if !errors.Is(got, tc.wantErr) {
				t.Errorf(
					"ClassifyExitState(%+v) does not satisfy errors.Is(_, %v); got %v",
					tc.state, tc.wantErr, got,
				)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-023: Priority 2–4 — adapter-returned typed condition.
// ─────────────────────────────────────────────────────────────────────────────

// TestClassifyExitState_AdapterCondition verifies that each AdapterCondition
// maps to the correct primary sentinel when CtxCanceled==false (§8.1–§8.3).
func TestClassifyExitState_AdapterCondition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		condition handler.AdapterCondition
		wantErr   error
	}{
		{
			name:      "adapter_transient",
			condition: handler.AdapterConditionTransient,
			wantErr:   handler.ErrTransient,
		},
		{
			name:      "adapter_structural",
			condition: handler.AdapterConditionStructural,
			wantErr:   handler.ErrStructural,
		},
		{
			name:      "adapter_deterministic",
			condition: handler.AdapterConditionDeterministic,
			wantErr:   handler.ErrDeterministic,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := handler.ExitState{
				ExitCode:      1,
				CtxCanceled:   false,
				AdapterResult: tc.condition,
			}
			got := handler.ClassifyExitState(state)
			if !errors.Is(got, tc.wantErr) {
				t.Errorf(
					"ClassifyExitState(%+v): errors.Is(_, %v) = false; got %v",
					state, tc.wantErr, got,
				)
			}
		})
	}
}

// TestClassifyExitState_AdapterCondition_MutualExclusion verifies that each
// adapter-condition result satisfies errors.Is for exactly one primary sentinel
// (HC-020: boundary-crossing error wraps exactly one primary class).
func TestClassifyExitState_AdapterCondition_MutualExclusion(t *testing.T) {
	t.Parallel()

	primaries := []struct {
		name string
		err  error
	}{
		{"ErrTransient", handler.ErrTransient},
		{"ErrStructural", handler.ErrStructural},
		{"ErrDeterministic", handler.ErrDeterministic},
		{"ErrCanceled", handler.ErrCanceled},
		{"ErrBudget", handler.ErrBudget},
	}

	cases := []struct {
		adapterCond handler.AdapterCondition
		wantPrimary error
	}{
		{handler.AdapterConditionTransient, handler.ErrTransient},
		{handler.AdapterConditionStructural, handler.ErrStructural},
		{handler.AdapterConditionDeterministic, handler.ErrDeterministic},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.wantPrimary.Error(), func(t *testing.T) {
			t.Parallel()
			state := handler.ExitState{
				AdapterResult: tc.adapterCond,
			}
			got := handler.ClassifyExitState(state)
			for _, p := range primaries {
				want := errors.Is(got, p.err)
				should := p.err == tc.wantPrimary
				if want != should {
					t.Errorf(
						"ClassifyExitState adapter=%v: errors.Is(got, %v) = %v, want %v — "+
							"must wrap exactly one primary sentinel (HC-020 / HC-023)",
						tc.adapterCond, p.name, want, should,
					)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-023: Priority 5–6 — exit-code fallback (no adapter, no cancel).
// ─────────────────────────────────────────────────────────────────────────────

// TestClassifyExitState_ExitCodeFallback verifies that when CtxCanceled==false
// and AdapterResult==None, both zero-exit-without-outcome and non-zero exit
// produce ErrStructural (§8.2: crash or exit without outcome is structural).
func TestClassifyExitState_ExitCodeFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state handler.ExitState
	}{
		{
			name: "zero_exit_no_outcome",
			state: handler.ExitState{
				ExitCode:       0,
				OutcomeEmitted: false,
				CtxCanceled:    false,
				AdapterResult:  handler.AdapterConditionNone,
			},
		},
		{
			name: "nonzero_exit_no_outcome",
			state: handler.ExitState{
				ExitCode:       1,
				OutcomeEmitted: false,
				CtxCanceled:    false,
				AdapterResult:  handler.AdapterConditionNone,
			},
		},
		{
			name: "exit_code_2",
			state: handler.ExitState{
				ExitCode:      2,
				AdapterResult: handler.AdapterConditionNone,
			},
		},
		{
			name: "exit_code_minus1_abnormal",
			state: handler.ExitState{
				ExitCode:      -1,
				AdapterResult: handler.AdapterConditionNone,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := handler.ClassifyExitState(tc.state)
			if !errors.Is(got, handler.ErrStructural) {
				t.Errorf(
					"ClassifyExitState(%+v): want ErrStructural, got %v (§8.2 / HC-023)",
					tc.state, got,
				)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-023: Determinism — identical inputs always produce equivalent results.
// ─────────────────────────────────────────────────────────────────────────────

// TestClassifyExitState_Determinism verifies that calling ClassifyExitState
// twice with identical ExitState values yields errors that satisfy errors.Is
// for the same primary sentinel — confirming the function is deterministic and
// side-effect-free (core HC-023 property: "no cognition in classification").
func TestClassifyExitState_Determinism(t *testing.T) {
	t.Parallel()

	states := []handler.ExitState{
		{ExitCode: 0, CtxCanceled: false, AdapterResult: handler.AdapterConditionNone},
		{ExitCode: 1, CtxCanceled: false, AdapterResult: handler.AdapterConditionNone},
		{CtxCanceled: true},
		{AdapterResult: handler.AdapterConditionTransient},
		{AdapterResult: handler.AdapterConditionStructural},
		{AdapterResult: handler.AdapterConditionDeterministic},
	}

	primaries := []error{
		handler.ErrTransient,
		handler.ErrStructural,
		handler.ErrDeterministic,
		handler.ErrCanceled,
		handler.ErrBudget,
	}

	for _, s := range states {
		s := s
		r1 := handler.ClassifyExitState(s)
		r2 := handler.ClassifyExitState(s)
		for _, p := range primaries {
			if errors.Is(r1, p) != errors.Is(r2, p) {
				t.Errorf(
					"ClassifyExitState(%+v) non-deterministic for primary %v: "+
						"call1=%v call2=%v",
					s, p, r1, r2,
				)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-023: Every result wraps exactly one primary sentinel (HC-020 compliance).
// ─────────────────────────────────────────────────────────────────────────────

// TestClassifyExitState_ExactlyOnePrimary verifies that every ClassifyExitState
// result satisfies errors.Is for exactly one of the five primary sentinels.
// This is the boundary-crossing invariant of HC-020 applied to the classifier.
func TestClassifyExitState_ExactlyOnePrimary(t *testing.T) {
	t.Parallel()

	// Enumerate representative ExitState inputs covering all code paths.
	inputs := []struct {
		name  string
		state handler.ExitState
	}{
		{"cancel_wins", handler.ExitState{CtxCanceled: true, ExitCode: 42}},
		{"adapter_transient", handler.ExitState{AdapterResult: handler.AdapterConditionTransient}},
		{"adapter_structural", handler.ExitState{AdapterResult: handler.AdapterConditionStructural}},
		{"adapter_deterministic", handler.ExitState{AdapterResult: handler.AdapterConditionDeterministic}},
		{"exit_zero_no_outcome", handler.ExitState{ExitCode: 0}},
		{"exit_nonzero", handler.ExitState{ExitCode: 1}},
		{"exit_negative", handler.ExitState{ExitCode: -1}},
		{"zero_value", handler.ExitState{}},
	}

	primaries := []struct {
		name string
		err  error
	}{
		{"ErrTransient", handler.ErrTransient},
		{"ErrStructural", handler.ErrStructural},
		{"ErrDeterministic", handler.ErrDeterministic},
		{"ErrCanceled", handler.ErrCanceled},
		{"ErrBudget", handler.ErrBudget},
	}

	for _, inp := range inputs {
		inp := inp
		t.Run(inp.name, func(t *testing.T) {
			t.Parallel()
			got := handler.ClassifyExitState(inp.state)
			count := 0
			for _, p := range primaries {
				if errors.Is(got, p.err) {
					count++
				}
			}
			if count != 1 {
				t.Errorf(
					"ClassifyExitState(%+v) satisfies errors.Is for %d primaries, want exactly 1 "+
						"(HC-020 / HC-023: every boundary-crossing error wraps exactly one primary); "+
						"got error: %v",
					inp.state, count, got,
				)
			}
		})
	}
}
