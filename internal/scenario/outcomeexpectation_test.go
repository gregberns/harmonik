package scenario

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// outcomeExpectationFixtureSuccess returns a minimally valid OutcomeExpectation
// with outcome_status=SUCCESS and a non-empty description.
func outcomeExpectationFixtureSuccess(t *testing.T) OutcomeExpectation {
	t.Helper()
	return OutcomeExpectation{
		OutcomeStatus: core.OutcomeStatusSuccess,
		Description:   "run must complete with SUCCESS",
	}
}

func TestOutcomeExpectationValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input OutcomeExpectation
		want  bool
	}{
		{
			name:  "valid: SUCCESS status with description",
			input: outcomeExpectationFixtureSuccess(t),
			want:  true,
		},
		{
			name: "valid: FAIL status with description",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusFail,
				Description:   "run must fail",
			},
			want: true,
		},
		{
			name: "valid: RETRY status with description",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusRetry,
				Description:   "run must be retried",
			},
			want: true,
		},
		{
			name: "valid: PARTIAL_SUCCESS status with description",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusPartialSuccess,
				Description:   "run must partially succeed",
			},
			want: true,
		},
		{
			name: "invalid: unknown outcome_status",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatus("UNKNOWN"),
				Description:   "bad status",
			},
			want: false,
		},
		{
			name: "invalid: empty outcome_status",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatus(""),
				Description:   "empty status",
			},
			want: false,
		},
		{
			name: "invalid: empty description",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusSuccess,
				Description:   "",
			},
			want: false,
		},
		{
			name:  "invalid: zero value",
			input: OutcomeExpectation{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("OutcomeExpectation.Valid() = %v, want %v (input: %+v)", got, tc.want, tc.input)
			}
		})
	}
}

func TestOutcomeExpectationJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input OutcomeExpectation
	}{
		{
			name:  "SUCCESS",
			input: outcomeExpectationFixtureSuccess(t),
		},
		{
			name: "FAIL",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusFail,
				Description:   "run must fail",
			},
		},
		{
			name: "PARTIAL_SUCCESS",
			input: OutcomeExpectation{
				OutcomeStatus: core.OutcomeStatusPartialSuccess,
				Description:   "partial success expected",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got OutcomeExpectation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if got != tc.input {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestOutcomeExpectationJSONKeys(t *testing.T) {
	t.Parallel()

	// Verify the JSON wire shape: outcome_status and description keys present.
	e := outcomeExpectationFixtureSuccess(t)
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	if _, ok := raw["outcome_status"]; !ok {
		t.Errorf("marshaled JSON missing 'outcome_status' key; got %s", data)
	}
	if _, ok := raw["description"]; !ok {
		t.Errorf("marshaled JSON missing 'description' key; got %s", data)
	}
}
