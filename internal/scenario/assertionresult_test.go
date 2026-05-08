package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

// assertionResultFixtureEventPresent returns a valid AssertionResult with
// AssertionKind=event_present.
func assertionResultFixtureEventPresent(t *testing.T) AssertionResult {
	t.Helper()
	return AssertionResult{
		AssertionKind: AssertionResultKindEventPresent,
		Description:   "task_started event must be emitted",
		ActualValue:   map[string]any{"type": "task_started"},
		ExpectedValue: map[string]any{"type": "task_started"},
		Passed:        true,
	}
}

// assertionResultFixtureEventAbsent returns a valid AssertionResult with
// AssertionKind=event_absent.
func assertionResultFixtureEventAbsent(t *testing.T) AssertionResult {
	t.Helper()
	return AssertionResult{
		AssertionKind: AssertionResultKindEventAbsent,
		Description:   "error event must NOT be emitted",
		ActualValue:   nil,
		ExpectedValue: nil,
		Passed:        true,
	}
}

// assertionResultFixtureWorkspaceState returns a valid AssertionResult with
// AssertionKind=workspace_state.
func assertionResultFixtureWorkspaceState(t *testing.T) AssertionResult {
	t.Helper()
	return AssertionResult{
		AssertionKind: AssertionResultKindWorkspaceState,
		Description:   "output file must contain expected content",
		ActualValue:   "hello world\n",
		ExpectedValue: "hello world\n",
		Passed:        true,
	}
}

// assertionResultFixtureExitCode returns a valid AssertionResult with
// AssertionKind=exit_code.
func assertionResultFixtureExitCode(t *testing.T) AssertionResult {
	t.Helper()
	return AssertionResult{
		AssertionKind: AssertionResultKindExitCode,
		Description:   "run must exit with SUCCESS",
		ActualValue:   "SUCCESS",
		ExpectedValue: "SUCCESS",
		Passed:        true,
	}
}

func TestAssertionResultKindValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AssertionResultKind
		want  bool
	}{
		{"event_present", AssertionResultKindEventPresent, true},
		{"event_absent", AssertionResultKindEventAbsent, true},
		{"workspace_state", AssertionResultKindWorkspaceState, true},
		{"exit_code", AssertionResultKindExitCode, true},
		{"empty string", AssertionResultKind(""), false},
		{"unknown value", AssertionResultKind("unknown"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("AssertionResultKind(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestAssertionResultKindMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   AssertionResultKind
		want    string
		wantErr bool
	}{
		{"event_present", AssertionResultKindEventPresent, "event_present", false},
		{"event_absent", AssertionResultKindEventAbsent, "event_absent", false},
		{"workspace_state", AssertionResultKindWorkspaceState, "workspace_state", false},
		{"exit_code", AssertionResultKindExitCode, "exit_code", false},
		{"invalid", AssertionResultKind("bad"), "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := tc.input.MarshalText()
			if tc.wantErr {
				if err == nil {
					t.Errorf("MarshalText(%q): expected error, got nil", string(tc.input))
				}
				return
			}
			if err != nil {
				t.Fatalf("MarshalText(%q) unexpected error: %v", string(tc.input), err)
			}
			if string(b) != tc.want {
				t.Errorf("MarshalText(%q) = %q, want %q", string(tc.input), string(b), tc.want)
			}
		})
	}
}

func TestAssertionResultValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AssertionResult
		want  bool
	}{
		{
			name:  "valid: event_present kind",
			input: assertionResultFixtureEventPresent(t),
			want:  true,
		},
		{
			name:  "valid: event_absent kind",
			input: assertionResultFixtureEventAbsent(t),
			want:  true,
		},
		{
			name:  "valid: workspace_state kind",
			input: assertionResultFixtureWorkspaceState(t),
			want:  true,
		},
		{
			name:  "valid: exit_code kind",
			input: assertionResultFixtureExitCode(t),
			want:  true,
		},
		{
			name: "valid: passed=false is still structurally valid",
			input: AssertionResult{
				AssertionKind: AssertionResultKindExitCode,
				Description:   "should have been SUCCESS",
				ActualValue:   "FAIL",
				ExpectedValue: "SUCCESS",
				Passed:        false,
			},
			want: true,
		},
		{
			name: "invalid: empty description",
			input: AssertionResult{
				AssertionKind: AssertionResultKindEventPresent,
				Description:   "",
				ActualValue:   nil,
				ExpectedValue: nil,
				Passed:        false,
			},
			want: false,
		},
		{
			name: "invalid: unknown kind",
			input: AssertionResult{
				AssertionKind: AssertionResultKind("not_a_kind"),
				Description:   "some description",
				ActualValue:   nil,
				ExpectedValue: nil,
				Passed:        false,
			},
			want: false,
		},
		{
			name:  "invalid: zero value",
			input: AssertionResult{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("AssertionResult.Valid() = %v, want %v (input: %+v)", got, tc.want, tc.input)
			}
		})
	}
}

func TestAssertionResultJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AssertionResult
	}{
		{
			name:  "null values",
			input: assertionResultFixtureEventAbsent(t),
		},
		{
			name: "bool values",
			input: AssertionResult{
				AssertionKind: AssertionResultKindWorkspaceState,
				Description:   "file exists check",
				ActualValue:   true,
				ExpectedValue: true,
				Passed:        true,
			},
		},
		{
			name: "string values",
			input: assertionResultFixtureWorkspaceState(t),
		},
		{
			name: "number values",
			input: AssertionResult{
				AssertionKind: AssertionResultKindExitCode,
				Description:   "exit code check",
				ActualValue:   float64(0),
				ExpectedValue: float64(0),
				Passed:        true,
			},
		},
		{
			name: "array values",
			input: AssertionResult{
				AssertionKind: AssertionResultKindEventPresent,
				Description:   "events list check",
				ActualValue:   []any{"a", "b", float64(3)},
				ExpectedValue: []any{"a", "b", float64(3)},
				Passed:        true,
			},
		},
		{
			name: "nested object values",
			input: AssertionResult{
				AssertionKind: AssertionResultKindEventPresent,
				Description:   "nested payload check",
				ActualValue: map[string]any{
					"type":    "task_started",
					"payload": map[string]any{"task_id": "abc-123", "count": float64(5)},
				},
				ExpectedValue: map[string]any{
					"type": "task_started",
				},
				Passed: false,
			},
		},
		{
			name:  "passed=false preserved",
			input: assertionResultFixtureExitCode(t),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got AssertionResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestAssertionResultHeterogeneousValues(t *testing.T) {
	t.Parallel()

	// ActualValue and ExpectedValue can be different types and both round-trip correctly.
	tests := []struct {
		name          string
		actualValue   any
		expectedValue any
	}{
		{
			name:          "actual=null expected=bool",
			actualValue:   nil,
			expectedValue: true,
		},
		{
			name:          "actual=string expected=number",
			actualValue:   "FAIL",
			expectedValue: float64(0),
		},
		{
			name:          "actual=array expected=object",
			actualValue:   []any{"x", "y"},
			expectedValue: map[string]any{"key": "val"},
		},
		{
			name:          "actual=bool expected=null",
			actualValue:   false,
			expectedValue: nil,
		},
		{
			name:          "actual=nested object expected=string",
			actualValue:   map[string]any{"nested": map[string]any{"deep": float64(1)}},
			expectedValue: "flat",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := AssertionResult{
				AssertionKind: AssertionResultKindWorkspaceState,
				Description:   "heterogeneous values test",
				ActualValue:   tc.actualValue,
				ExpectedValue: tc.expectedValue,
				Passed:        false,
			}

			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got AssertionResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(input, got) {
				t.Errorf("heterogeneous round-trip mismatch:\n  in:  %+v\n  out: %+v", input, got)
			}
		})
	}
}
