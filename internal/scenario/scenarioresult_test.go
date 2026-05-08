package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// scenarioResultFixtureStartedAt is a stable non-zero timestamp used by fixtures.
var scenarioResultFixtureStartedAt = time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)

// scenarioResultFixtureCompletedAt is a stable non-zero timestamp after scenarioResultFixtureStartedAt.
var scenarioResultFixtureCompletedAt = time.Date(2026, 5, 7, 10, 0, 5, 0, time.UTC)

// scenarioResultFixtureValid returns a canonical valid ScenarioResult with
// Verdict=pass and all required fields populated.
func scenarioResultFixtureValid(t *testing.T) ScenarioResult {
	t.Helper()
	return ScenarioResult{
		ScenarioName:          "basic-task-run",
		SourcePath:            "scenarios/basic_task_run.yaml",
		StartedAt:             scenarioResultFixtureStartedAt,
		CompletedAt:           scenarioResultFixtureCompletedAt,
		Verdict:               ScenarioVerdictPass,
		FailureClass:          "",
		AssertionResults:      []AssertionResult{assertionResultFixtureEventPresent(t)},
		EventLogPath:          "fixture/events.jsonl",
		WorkspaceSnapshotPath: "fixture/workspace",
		StdoutLogPaths:        map[string]string{"orchestrator": "fixture/orchestrator.stdout"},
		StderrLogPaths:        map[string]string{"orchestrator": "fixture/orchestrator.stderr"},
		ErrorDetail:           "",
	}
}

// scenarioResultFixtureNonPass returns a valid ScenarioResult with the given
// non-pass verdict and a non-empty FailureClass.
func scenarioResultFixtureNonPass(t *testing.T, verdict ScenarioVerdict, failureClass string) ScenarioResult {
	t.Helper()
	r := scenarioResultFixtureValid(t)
	r.Verdict = verdict
	r.FailureClass = failureClass
	return r
}

func TestScenarioVerdictValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input ScenarioVerdict
		want  bool
	}{
		{"pass", ScenarioVerdictPass, true},
		{"fail", ScenarioVerdictFail, true},
		{"timeout", ScenarioVerdictTimeout, true},
		{"error", ScenarioVerdictError, true},
		{"empty string", ScenarioVerdict(""), false},
		{"unknown value", ScenarioVerdict("unknown"), false},
		{"mixed case", ScenarioVerdict("Pass"), false},
		{"partial", ScenarioVerdict("fai"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ScenarioVerdict(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestScenarioVerdictMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   ScenarioVerdict
		want    string
		wantErr bool
	}{
		{"pass", ScenarioVerdictPass, "pass", false},
		{"fail", ScenarioVerdictFail, "fail", false},
		{"timeout", ScenarioVerdictTimeout, "timeout", false},
		{"error", ScenarioVerdictError, "error", false},
		{"empty string is invalid", ScenarioVerdict(""), "", true},
		{"unknown value", ScenarioVerdict("weirdverdict"), "", true},
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

func TestScenarioVerdictUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    ScenarioVerdict
		wantErr bool
	}{
		{"pass", "pass", ScenarioVerdictPass, false},
		{"fail", "fail", ScenarioVerdictFail, false},
		{"timeout", "timeout", ScenarioVerdictTimeout, false},
		{"error", "error", ScenarioVerdictError, false},
		{"empty string rejected", "", ScenarioVerdict(""), true},
		{"unknown rejected", "unknown", ScenarioVerdict(""), true},
		{"mixed-case rejected", "Pass", ScenarioVerdict(""), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var v ScenarioVerdict
			err := v.UnmarshalText([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Errorf("UnmarshalText(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalText(%q) unexpected error: %v", tc.input, err)
			}
			if v != tc.want {
				t.Errorf("UnmarshalText(%q) = %q, want %q", tc.input, string(v), string(tc.want))
			}
		})
	}
}

func TestScenarioResultValid(t *testing.T) {
	t.Parallel()

	invalidKindResult := assertionResultFixtureEventPresent(t)
	invalidKindResult.AssertionKind = AssertionResultKind("not_a_kind")

	tests := []struct {
		name  string
		build func(t *testing.T) ScenarioResult
		want  bool
	}{
		{
			name:  "canonical pass: all required fields, no failure class",
			build: scenarioResultFixtureValid,
			want:  true,
		},
		{
			name: "pass with non-empty FailureClass violates invariant",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.FailureClass = "assertion-failed"
				return r
			},
			want: false,
		},
		{
			name: "fail with empty FailureClass is invalid",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.Verdict = ScenarioVerdictFail
				r.FailureClass = ""
				return r
			},
			want: false,
		},
		{
			name: "fail with sample FailureClass is valid",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				return scenarioResultFixtureNonPass(t, ScenarioVerdictFail, "assertion-failed")
			},
			want: true,
		},
		{
			name: "timeout with sample FailureClass is valid",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				return scenarioResultFixtureNonPass(t, ScenarioVerdictTimeout, "scenario-timeout")
			},
			want: true,
		},
		{
			name: "error with sample FailureClass is valid",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				return scenarioResultFixtureNonPass(t, ScenarioVerdictError, "harness-internal-error")
			},
			want: true,
		},
		{
			name: "empty ScenarioName",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.ScenarioName = ""
				return r
			},
			want: false,
		},
		{
			name: "empty SourcePath",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.SourcePath = ""
				return r
			},
			want: false,
		},
		{
			name: "zero StartedAt",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.StartedAt = time.Time{}
				return r
			},
			want: false,
		},
		{
			name: "zero CompletedAt",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.CompletedAt = time.Time{}
				return r
			},
			want: false,
		},
		{
			name: "CompletedAt before StartedAt",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.CompletedAt = r.StartedAt.Add(-time.Second)
				return r
			},
			want: false,
		},
		{
			name: "invalid Verdict",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.Verdict = ScenarioVerdict("weirdverdict")
				return r
			},
			want: false,
		},
		{
			name: "empty EventLogPath",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.EventLogPath = ""
				return r
			},
			want: false,
		},
		{
			name: "empty WorkspaceSnapshotPath",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.WorkspaceSnapshotPath = ""
				return r
			},
			want: false,
		},
		{
			name: "AssertionResults element with invalid kind",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.AssertionResults = []AssertionResult{invalidKindResult}
				return r
			},
			want: false,
		},
		{
			name: "nil StdoutLogPaths with otherwise valid record",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.StdoutLogPaths = nil
				return r
			},
			want: true,
		},
		{
			name: "nil StderrLogPaths with otherwise valid record",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.StderrLogPaths = nil
				return r
			},
			want: true,
		},
		{
			name: "both log path maps nil",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureValid(t)
				r.StdoutLogPaths = nil
				r.StderrLogPaths = nil
				return r
			},
			want: true,
		},
		{
			name: "empty AssertionResults is valid for early-termination scenario",
			build: func(t *testing.T) ScenarioResult {
				t.Helper()
				r := scenarioResultFixtureNonPass(t, ScenarioVerdictError, "fixture-setup-failed")
				r.AssertionResults = []AssertionResult{}
				return r
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := tc.build(t)
			if got := input.Valid(); got != tc.want {
				t.Errorf("ScenarioResult.Valid() = %v, want %v (input: %+v)", got, tc.want, input)
			}
		})
	}
}

func TestScenarioResultJSONRoundTrip(t *testing.T) {
	t.Parallel()

	// Use timestamps that round-trip cleanly through RFC 3339 (no sub-second
	// precision so encoding/json time.Time marshalling is lossless).
	started := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 5, 7, 12, 0, 10, 0, time.UTC)

	input := ScenarioResult{
		ScenarioName:          "round-trip-scenario",
		SourcePath:            "scenarios/round_trip.yaml",
		StartedAt:             started,
		CompletedAt:           completed,
		Verdict:               ScenarioVerdictPass,
		FailureClass:          "",
		AssertionResults:      []AssertionResult{assertionResultFixtureExitCode(t)},
		EventLogPath:          "fixture/events.jsonl",
		WorkspaceSnapshotPath: "fixture/workspace",
		StdoutLogPaths:        map[string]string{"worker": "fixture/worker.stdout"},
		StderrLogPaths:        map[string]string{"worker": "fixture/worker.stderr"},
		ErrorDetail:           "",
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got ScenarioResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// time.Time comparison: use Equal to handle monotonic clock differences.
	if !input.StartedAt.Equal(got.StartedAt) {
		t.Errorf("StartedAt round-trip mismatch: in=%v out=%v", input.StartedAt, got.StartedAt)
	}
	if !input.CompletedAt.Equal(got.CompletedAt) {
		t.Errorf("CompletedAt round-trip mismatch: in=%v out=%v", input.CompletedAt, got.CompletedAt)
	}

	// Zero out time fields before DeepEqual so we can use reflect (time.Time
	// has unexported monotonic state that reflect.DeepEqual sees).
	inputCopy := input
	gotCopy := got
	inputCopy.StartedAt = time.Time{}
	inputCopy.CompletedAt = time.Time{}
	gotCopy.StartedAt = time.Time{}
	gotCopy.CompletedAt = time.Time{}

	if !reflect.DeepEqual(inputCopy, gotCopy) {
		t.Errorf("JSON round-trip mismatch (excluding time fields):\n  in:  %+v\n  out: %+v", inputCopy, gotCopy)
	}
}

func TestScenarioVerdictJSONRoundTrip(t *testing.T) {
	t.Parallel()

	verdicts := []ScenarioVerdict{
		ScenarioVerdictPass,
		ScenarioVerdictFail,
		ScenarioVerdictTimeout,
		ScenarioVerdictError,
	}

	for _, v := range verdicts {
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			b, err := v.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(v), err)
			}

			var got ScenarioVerdict
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(b), err)
			}

			if got != v {
				t.Errorf("verdict round-trip: in=%q out=%q", string(v), string(got))
			}
		})
	}
}
