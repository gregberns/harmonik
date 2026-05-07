package handler

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelIdentity(t *testing.T) {
	t.Parallel()

	// Each primary sentinel must equal itself.
	tests := []struct {
		name string
		err  error
		want error
		is   bool
	}{
		{name: "ErrTransient is ErrTransient", err: ErrTransient, want: ErrTransient, is: true},
		{name: "ErrStructural is ErrStructural", err: ErrStructural, want: ErrStructural, is: true},
		{name: "ErrDeterministic is ErrDeterministic", err: ErrDeterministic, want: ErrDeterministic, is: true},
		{name: "ErrCanceled is ErrCanceled", err: ErrCanceled, want: ErrCanceled, is: true},
		{name: "ErrBudget is ErrBudget", err: ErrBudget, want: ErrBudget, is: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := errors.Is(tc.err, tc.want); got != tc.is {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tc.err, tc.want, got, tc.is)
			}
		})
	}
}

func TestPrimaryMutualExclusion(t *testing.T) {
	t.Parallel()

	// No primary sentinel should match any other primary sentinel.
	primaries := []struct {
		name string
		err  error
	}{
		{"ErrTransient", ErrTransient},
		{"ErrStructural", ErrStructural},
		{"ErrDeterministic", ErrDeterministic},
		{"ErrCanceled", ErrCanceled},
		{"ErrBudget", ErrBudget},
	}

	for _, a := range primaries {
		for _, b := range primaries {
			if a.name == b.name {
				continue
			}
			t.Run(a.name+"_not_"+b.name, func(t *testing.T) {
				t.Parallel()
				if errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%v, %v) = true, want false (primaries must be mutually exclusive)", a.err, b.err)
				}
			})
		}
	}
}

func TestSubSentinelErrProtocolMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target error
		want   bool
	}{
		// Sub-sentinel matches itself.
		{"is ErrProtocolMismatch", ErrProtocolMismatch, true},
		// Sub-sentinel matches its structural parent (HC-021).
		{"is ErrStructural", ErrStructural, true},
		// Sub-sentinel does NOT match unrelated primaries.
		{"not ErrTransient", ErrTransient, false},
		{"not ErrDeterministic", ErrDeterministic, false},
		{"not ErrCanceled", ErrCanceled, false},
		{"not ErrBudget", ErrBudget, false},
		// Sub-sentinel does NOT match its sibling.
		{"not ErrSkillProvisioningFailed", ErrSkillProvisioningFailed, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := errors.Is(ErrProtocolMismatch, tc.target); got != tc.want {
				t.Errorf("errors.Is(ErrProtocolMismatch, %v) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

func TestSubSentinelErrSkillProvisioningFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target error
		want   bool
	}{
		// Sub-sentinel matches itself.
		{"is ErrSkillProvisioningFailed", ErrSkillProvisioningFailed, true},
		// Sub-sentinel matches its structural parent (HC-022).
		{"is ErrStructural", ErrStructural, true},
		// Sub-sentinel does NOT match unrelated primaries.
		{"not ErrTransient", ErrTransient, false},
		{"not ErrDeterministic", ErrDeterministic, false},
		{"not ErrCanceled", ErrCanceled, false},
		{"not ErrBudget", ErrBudget, false},
		// Sub-sentinel does NOT match its sibling.
		{"not ErrProtocolMismatch", ErrProtocolMismatch, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := errors.Is(ErrSkillProvisioningFailed, tc.target); got != tc.want {
				t.Errorf("errors.Is(ErrSkillProvisioningFailed, %v) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

func TestBoundaryCrossingWrap(t *testing.T) {
	t.Parallel()

	// Simulate a boundary-crossing wrap as required by HC-020:
	// every error returned across a subsystem boundary wraps exactly one primary class.
	wrapped := fmt.Errorf("subsystem boundary: %w", ErrCanceled)

	if !errors.Is(wrapped, ErrCanceled) {
		t.Errorf("errors.Is(wrapped, ErrCanceled) = false, want true")
	}

	// Mutual exclusion preserved through wrapping.
	for _, other := range []error{ErrTransient, ErrStructural, ErrDeterministic, ErrBudget} {
		if errors.Is(wrapped, other) {
			t.Errorf("errors.Is(wrapped, %v) = true, want false (must wrap exactly one primary)", other)
		}
	}
}

func TestNarrowestFirstDispatch(t *testing.T) {
	t.Parallel()

	// Simulate a call site receiving an error that wraps ErrProtocolMismatch.
	// The call site MUST check the sub-sentinel before the structural parent.
	incoming := fmt.Errorf("watcher: version negotiation: %w", ErrProtocolMismatch)

	// Step 1: narrowest check catches the specific sub-sentinel.
	if !errors.Is(incoming, ErrProtocolMismatch) {
		t.Fatal("narrowest-first: errors.Is(incoming, ErrProtocolMismatch) = false, want true")
	}

	// Step 2: structural fallback also matches (the wrapping chain is intact).
	if !errors.Is(incoming, ErrStructural) {
		t.Fatal("structural fallback: errors.Is(incoming, ErrStructural) = false, want true")
	}

	// Step 3: unrelated primaries do NOT match.
	for _, other := range []error{ErrTransient, ErrDeterministic, ErrCanceled, ErrBudget} {
		if errors.Is(incoming, other) {
			t.Errorf("narrowest-first: errors.Is(incoming, %v) = true, want false", other)
		}
	}
}

func TestNarrowestFirstDispatchSkillProvisioning(t *testing.T) {
	t.Parallel()

	// Same narrowest-first test for ErrSkillProvisioningFailed.
	incoming := fmt.Errorf("launcher: skill inject: %w", ErrSkillProvisioningFailed)

	if !errors.Is(incoming, ErrSkillProvisioningFailed) {
		t.Fatal("narrowest-first: errors.Is(incoming, ErrSkillProvisioningFailed) = false, want true")
	}

	if !errors.Is(incoming, ErrStructural) {
		t.Fatal("structural fallback: errors.Is(incoming, ErrStructural) = false, want true")
	}

	for _, other := range []error{ErrTransient, ErrDeterministic, ErrCanceled, ErrBudget} {
		if errors.Is(incoming, other) {
			t.Errorf("narrowest-first: errors.Is(incoming, %v) = true, want false", other)
		}
	}
}
