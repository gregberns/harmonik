package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// mustParseSuiteID constructs a core.SuiteID from a UUID string, failing the test on error.
func mustParseSuiteID(t *testing.T, s string) core.SuiteID {
	t.Helper()

	var id core.SuiteID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("SuiteID UnmarshalText(%q): %v", s, err)
	}

	return id
}

// suiteResultFixtureStartedAt is a stable non-zero timestamp used by suite fixtures.
var suiteResultFixtureStartedAt = time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)

// suiteResultFixtureCompletedAt is a stable non-zero timestamp after suiteResultFixtureStartedAt.
var suiteResultFixtureCompletedAt = time.Date(2026, 5, 7, 9, 1, 0, 0, time.UTC)

// suiteResultFixtureValid returns a canonical valid SuiteResult with
// SuiteVerdict=pass and all required fields populated.
func suiteResultFixtureValid(t *testing.T) SuiteResult {
	t.Helper()
	return SuiteResult{
		SuiteID:       mustParseSuiteID(t, "018f5e1a-0000-7000-8000-000000000001"),
		StartedAt:     suiteResultFixtureStartedAt,
		CompletedAt:   suiteResultFixtureCompletedAt,
		FixtureRoot:   "/tmp/harmonik-harness-abc123",
		CadenceFilter: CadenceFilterSmoke,
		Results:       []ScenarioResult{scenarioResultFixtureValid(t)},
		SuiteVerdict:  SuiteVerdictPass,
	}
}

// suiteResultFixtureEmpty returns a valid SuiteResult with an empty Results
// list (cadence filter matched zero scenarios — suite_verdict=pass, vacuously).
func suiteResultFixtureEmpty(t *testing.T) SuiteResult {
	t.Helper()
	return SuiteResult{
		SuiteID:       mustParseSuiteID(t, "018f5e1a-0000-7000-8000-000000000002"),
		StartedAt:     suiteResultFixtureStartedAt,
		CompletedAt:   suiteResultFixtureCompletedAt,
		FixtureRoot:   "/tmp/harmonik-harness-empty",
		CadenceFilter: CadenceFilterAll,
		Results:       []ScenarioResult{},
		SuiteVerdict:  SuiteVerdictPass,
	}
}

func TestSuiteVerdictValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input SuiteVerdict
		want  bool
	}{
		{"pass", SuiteVerdictPass, true},
		{"fail", SuiteVerdictFail, true},
		{"empty string", SuiteVerdict(""), false},
		{"unknown value", SuiteVerdict("error"), false},
		{"mixed case", SuiteVerdict("Pass"), false},
		{"partial", SuiteVerdict("fai"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("SuiteVerdict(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestSuiteVerdictMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   SuiteVerdict
		want    string
		wantErr bool
	}{
		{"pass", SuiteVerdictPass, "pass", false},
		{"fail", SuiteVerdictFail, "fail", false},
		{"empty string is invalid", SuiteVerdict(""), "", true},
		{"unknown value", SuiteVerdict("timeout"), "", true},
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

func TestSuiteVerdictUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    SuiteVerdict
		wantErr bool
	}{
		{"pass", "pass", SuiteVerdictPass, false},
		{"fail", "fail", SuiteVerdictFail, false},
		{"empty string rejected", "", SuiteVerdict(""), true},
		{"unknown rejected", "timeout", SuiteVerdict(""), true},
		{"mixed-case rejected", "Pass", SuiteVerdict(""), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var v SuiteVerdict
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

func TestSuiteResultValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(t *testing.T) SuiteResult
		want  bool
	}{
		{
			name:  "canonical pass: all required fields, one passing scenario",
			build: suiteResultFixtureValid,
			want:  true,
		},
		{
			name:  "valid: empty results list with pass verdict (vacuous)",
			build: suiteResultFixtureEmpty,
			want:  true,
		},
		{
			name: "valid: fail verdict with one failing scenario",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.Results = []ScenarioResult{
					scenarioResultFixtureNonPass(t, ScenarioVerdictFail, "assertion-failed"),
				}
				r.SuiteVerdict = SuiteVerdictFail
				return r
			},
			want: true,
		},
		{
			name: "valid: fail verdict with timeout scenario",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.Results = []ScenarioResult{
					scenarioResultFixtureNonPass(t, ScenarioVerdictTimeout, "scenario-timeout"),
				}
				r.SuiteVerdict = SuiteVerdictFail
				return r
			},
			want: true,
		},
		{
			name: "valid: fail verdict with error scenario",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.Results = []ScenarioResult{
					scenarioResultFixtureNonPass(t, ScenarioVerdictError, "harness-internal-error"),
				}
				r.SuiteVerdict = SuiteVerdictFail
				return r
			},
			want: true,
		},
		{
			name: "invalid: zero SuiteID",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.SuiteID = core.SuiteID{}
				return r
			},
			want: false,
		},
		{
			name: "invalid: zero StartedAt",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.StartedAt = time.Time{}
				return r
			},
			want: false,
		},
		{
			name: "invalid: zero CompletedAt",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.CompletedAt = time.Time{}
				return r
			},
			want: false,
		},
		{
			name: "invalid: CompletedAt before StartedAt",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.CompletedAt = r.StartedAt.Add(-time.Second)
				return r
			},
			want: false,
		},
		{
			name: "invalid: empty FixtureRoot",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.FixtureRoot = ""
				return r
			},
			want: false,
		},
		{
			name: "invalid: unknown CadenceFilter",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.CadenceFilter = CadenceFilter("weekly")
				return r
			},
			want: false,
		},
		{
			name: "invalid: unknown SuiteVerdict",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.SuiteVerdict = SuiteVerdict("unknown")
				return r
			},
			want: false,
		},
		{
			name: "invalid: suite-verdict invariant — pass verdict with failing result",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				r.Results = []ScenarioResult{
					scenarioResultFixtureNonPass(t, ScenarioVerdictFail, "assertion-failed"),
				}
				r.SuiteVerdict = SuiteVerdictPass // wrong: should be fail
				return r
			},
			want: false,
		},
		{
			name: "invalid: suite-verdict invariant — fail verdict with all passing results",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				// Results contains one passing scenario (from fixture), verdict is wrong.
				r.SuiteVerdict = SuiteVerdictFail
				return r
			},
			want: false,
		},
		{
			name: "invalid: suite-verdict invariant — fail verdict with empty results",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureEmpty(t)
				r.SuiteVerdict = SuiteVerdictFail // wrong: empty list is vacuously pass
				return r
			},
			want: false,
		},
		{
			name: "invalid: Results element with structurally invalid ScenarioResult",
			build: func(t *testing.T) SuiteResult {
				t.Helper()
				r := suiteResultFixtureValid(t)
				bad := scenarioResultFixtureValid(t)
				bad.ScenarioName = "" // invalid
				r.Results = []ScenarioResult{bad}
				return r
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := tc.build(t)
			if got := input.Valid(); got != tc.want {
				t.Errorf("SuiteResult.Valid() = %v, want %v (input: %+v)", got, tc.want, input)
			}
		})
	}
}

func TestSuiteResultJSONRoundTrip(t *testing.T) {
	t.Parallel()

	// Use timestamps that round-trip cleanly through RFC 3339.
	started := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 5, 7, 8, 5, 0, 0, time.UTC)

	input := SuiteResult{
		SuiteID:       mustParseSuiteID(t, "018f5e1a-0000-7000-8000-000000000003"),
		StartedAt:     started,
		CompletedAt:   completed,
		FixtureRoot:   "/tmp/harmonik-harness-roundtrip",
		CadenceFilter: CadenceFilterRegression,
		Results:       []ScenarioResult{},
		SuiteVerdict:  SuiteVerdictPass,
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got SuiteResult
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

	// Zero out time fields before DeepEqual.
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

func TestSuiteVerdictJSONRoundTrip(t *testing.T) {
	t.Parallel()

	verdicts := []SuiteVerdict{SuiteVerdictPass, SuiteVerdictFail}

	for _, v := range verdicts {
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			b, err := v.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(v), err)
			}

			var got SuiteVerdict
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(b), err)
			}

			if got != v {
				t.Errorf("suite verdict round-trip: in=%q out=%q", string(v), string(got))
			}
		})
	}
}
