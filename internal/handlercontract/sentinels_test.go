package handlercontract_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

func TestSentinelsDistinct(t *testing.T) {
	t.Parallel()

	primaries := []struct {
		name string
		err  error
	}{
		{"ErrTransient", handlercontract.ErrTransient},
		{"ErrStructural", handlercontract.ErrStructural},
		{"ErrDeterministic", handlercontract.ErrDeterministic},
		{"ErrCanceled", handlercontract.ErrCanceled},
		{"ErrBudget", handlercontract.ErrBudget},
	}

	// Every primary sentinel must NOT match any other primary sentinel.
	for i, a := range primaries {
		for j, b := range primaries {
			if i == j {
				continue
			}
			a, b := a, b // capture
			t.Run(fmt.Sprintf("%s_not_is_%s", a.name, b.name), func(t *testing.T) {
				t.Parallel()
				if errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%s, %s) = true, want false — sentinels must be mutually distinct", a.name, b.name)
				}
			})
		}
	}
}

func TestSubSentinelsWrapStructural(t *testing.T) {
	t.Parallel()

	t.Run("ErrSkillProvisioningFailed_is_ErrStructural", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(handlercontract.ErrSkillProvisioningFailed, handlercontract.ErrStructural) {
			t.Error("errors.Is(ErrSkillProvisioningFailed, ErrStructural) = false, want true")
		}
	})

	t.Run("ErrProtocolMismatch_is_ErrStructural", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(handlercontract.ErrProtocolMismatch, handlercontract.ErrStructural) {
			t.Error("errors.Is(ErrProtocolMismatch, ErrStructural) = false, want true")
		}
	})

	t.Run("ErrSkillProvisioningFailed_identity", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(handlercontract.ErrSkillProvisioningFailed, handlercontract.ErrSkillProvisioningFailed) {
			t.Error("errors.Is(ErrSkillProvisioningFailed, ErrSkillProvisioningFailed) = false, want true")
		}
	})

	t.Run("ErrSkillProvisioningFailed_not_ErrProtocolMismatch", func(t *testing.T) {
		t.Parallel()
		if errors.Is(handlercontract.ErrSkillProvisioningFailed, handlercontract.ErrProtocolMismatch) {
			t.Error("errors.Is(ErrSkillProvisioningFailed, ErrProtocolMismatch) = true, want false — sub-sentinels must be mutually distinct")
		}
	})

	t.Run("ErrProtocolMismatch_not_ErrTransient", func(t *testing.T) {
		t.Parallel()
		if errors.Is(handlercontract.ErrProtocolMismatch, handlercontract.ErrTransient) {
			t.Error("errors.Is(ErrProtocolMismatch, ErrTransient) = true, want false")
		}
	})
}

func TestClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input error
		want  string
	}{
		{
			name:  "nil returns empty string",
			input: nil,
			want:  "",
		},
		{
			name:  "ErrTransient returns transient",
			input: handlercontract.ErrTransient,
			want:  "transient",
		},
		{
			name:  "ErrStructural returns structural",
			input: handlercontract.ErrStructural,
			want:  "structural",
		},
		{
			name:  "ErrDeterministic returns deterministic",
			input: handlercontract.ErrDeterministic,
			want:  "deterministic",
		},
		{
			name:  "ErrCanceled returns canceled",
			input: handlercontract.ErrCanceled,
			want:  "canceled",
		},
		{
			name:  "ErrBudget returns budget",
			input: handlercontract.ErrBudget,
			want:  "budget",
		},
		{
			name:  "ErrSkillProvisioningFailed returns structural",
			input: handlercontract.ErrSkillProvisioningFailed,
			want:  "structural",
		},
		{
			name:  "ErrProtocolMismatch returns structural",
			input: handlercontract.ErrProtocolMismatch,
			want:  "structural",
		},
		{
			name:  "wrapped ErrTransient returns transient",
			input: fmt.Errorf("wrapped: %w", handlercontract.ErrTransient),
			want:  "transient",
		},
		{
			name:  "doubly wrapped ErrCanceled returns canceled",
			input: fmt.Errorf("doubly: %w", fmt.Errorf("wrapped: %w", handlercontract.ErrCanceled)),
			want:  "canceled",
		},
		{
			name:  "unknown error returns empty string",
			input: errors.New("unknown"),
			want:  "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := handlercontract.Class(tc.input)
			if got != tc.want {
				t.Errorf("Class(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSentinelMessages(t *testing.T) {
	t.Parallel()

	const prefix = "handlercontract: "

	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrTransient", handlercontract.ErrTransient},
		{"ErrStructural", handlercontract.ErrStructural},
		{"ErrDeterministic", handlercontract.ErrDeterministic},
		{"ErrCanceled", handlercontract.ErrCanceled},
		{"ErrBudget", handlercontract.ErrBudget},
		{"ErrSkillProvisioningFailed", handlercontract.ErrSkillProvisioningFailed},
		{"ErrProtocolMismatch", handlercontract.ErrProtocolMismatch},
	}

	for _, s := range sentinels {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			msg := s.err.Error()
			if msg == "" {
				t.Errorf("%s.Error() returned empty string", s.name)
			}
			if !strings.HasPrefix(msg, prefix) {
				t.Errorf("%s.Error() = %q; want prefix %q", s.name, msg, prefix)
			}
		})
	}
}
