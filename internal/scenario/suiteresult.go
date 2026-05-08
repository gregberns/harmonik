package scenario

import (
	"fmt"
	"time"
)

// CadenceFilter is the per-suite cadence filter enum.
//
// Spec ref: specs/scenario-harness.md §6.1 RECORD SuiteResult — field cadence_filter.
type CadenceFilter string

// Declared CadenceFilter constants per specs/scenario-harness.md §6.1.
const (
	// CadenceFilterSmoke selects only smoke-tagged scenarios.
	CadenceFilterSmoke CadenceFilter = "smoke"

	// CadenceFilterRegression selects smoke and regression scenarios
	// (superset per SH-029).
	CadenceFilterRegression CadenceFilter = "regression"

	// CadenceFilterNightly selects smoke, regression, and nightly scenarios
	// (superset per SH-029).
	CadenceFilterNightly CadenceFilter = "nightly"

	// CadenceFilterAll selects every scenario regardless of cadence tag
	// (superset per SH-029).
	CadenceFilterAll CadenceFilter = "all"
)

// Valid reports whether f is one of the four declared CadenceFilter constants.
func (f CadenceFilter) Valid() bool {
	switch f {
	case CadenceFilterSmoke,
		CadenceFilterRegression,
		CadenceFilterNightly,
		CadenceFilterAll:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CadenceFilter serialises
// correctly in JSON and YAML.
func (f CadenceFilter) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("cadencefilter: unknown value %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not in the declared set.
func (f *CadenceFilter) UnmarshalText(text []byte) error {
	candidate := CadenceFilter(text)
	if !candidate.Valid() {
		return fmt.Errorf("cadencefilter: unknown value %q; must be one of smoke, regression, nightly, all", string(text))
	}
	*f = candidate
	return nil
}

// SuiteVerdict is the top-level outcome of a suite execution.
//
// Spec ref: specs/scenario-harness.md §6.1 RECORD SuiteResult — field suite_verdict.
type SuiteVerdict string

// Declared SuiteVerdict constants per specs/scenario-harness.md §6.1.
const (
	// SuiteVerdictPass indicates that every ScenarioResult in the suite had
	// verdict=pass (including the vacuous case of an empty results list).
	SuiteVerdictPass SuiteVerdict = "pass"

	// SuiteVerdictFail indicates that at least one ScenarioResult had a
	// non-pass verdict (fail, timeout, or error).
	SuiteVerdictFail SuiteVerdict = "fail"
)

// Valid reports whether v is one of the two declared SuiteVerdict constants.
func (v SuiteVerdict) Valid() bool {
	switch v {
	case SuiteVerdictPass, SuiteVerdictFail:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so SuiteVerdict serialises
// correctly in JSON and YAML.
func (v SuiteVerdict) MarshalText() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("suiteverdict: unknown value %q", string(v))
	}
	return []byte(v), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not in the declared set.
func (v *SuiteVerdict) UnmarshalText(text []byte) error {
	candidate := SuiteVerdict(text)
	if !candidate.Valid() {
		return fmt.Errorf("suiteverdict: unknown value %q; must be one of pass, fail", string(text))
	}
	*v = candidate
	return nil
}

// SuiteResult is the aggregate record produced at suite completion, containing
// the per-scenario results and the overall suite verdict.
//
// SuiteVerdict is pass iff every ScenarioResult.Verdict is pass; any non-pass
// result (fail, timeout, or error) implies SuiteVerdict=fail. The vacuous case
// (empty Results list, e.g. cadence filter matched zero scenarios) is
// SuiteVerdict=pass per SH-029.
//
// Spec ref: specs/scenario-harness.md §6.1 RECORD SuiteResult.
type SuiteResult struct {
	// SuiteID is the UUIDv7 identifier generated at suite invocation.
	//
	// TODO(hk-i0tw.54): this field uses a string placeholder pending the typed
	// core.SuiteID alias (specs/scenario-harness.md §6.1 field suite_id UUID).
	// When hk-i0tw.54 lands, change this field to core.SuiteID (non-breaking:
	// string-constant assignment is assignable to the typed alias).
	SuiteID string `json:"suite_id" yaml:"suite_id"`

	// StartedAt is the RFC 3339 UTC wall-clock timestamp at suite invocation
	// (millisecond precision).
	StartedAt time.Time `json:"started_at" yaml:"started_at"`

	// CompletedAt is the RFC 3339 UTC wall-clock timestamp at suite completion
	// (millisecond precision).
	CompletedAt time.Time `json:"completed_at" yaml:"completed_at"`

	// FixtureRoot is the absolute path to the per-suite ephemeral fixture root
	// per SH-016. Operator-facing: reported in suite-level output and used for
	// debugging. The harness MUST NOT delete this directory automatically; the
	// operator removes it manually or via `harmonik harness clean`.
	FixtureRoot string `json:"fixture_root" yaml:"fixture_root"`

	// CadenceFilter is the cadence filter applied to select scenarios for this
	// suite run. One of smoke, regression, nightly, all.
	CadenceFilter CadenceFilter `json:"cadence_filter" yaml:"cadence_filter"`

	// Results holds one ScenarioResult per executed scenario (or per expanded
	// matrix row per SH-030). An empty slice is valid when the cadence filter
	// matched zero scenarios (suite_verdict=pass, vacuously).
	Results []ScenarioResult `json:"results" yaml:"results"`

	// SuiteVerdict is the aggregate verdict: pass iff every element of Results
	// has Verdict=pass (including the empty-Results case); fail otherwise.
	SuiteVerdict SuiteVerdict `json:"suite_verdict" yaml:"suite_verdict"`
}

// Valid reports whether the SuiteResult is structurally well-formed per
// specs/scenario-harness.md §6.1 RECORD SuiteResult:
//
//   - SuiteID is non-empty.
//   - StartedAt is non-zero.
//   - CompletedAt is non-zero and not before StartedAt.
//   - FixtureRoot is non-empty.
//   - CadenceFilter is one of the four declared constants.
//   - Every element of Results satisfies ScenarioResult.Valid().
//   - SuiteVerdict is one of the two declared constants.
//   - Suite-verdict invariant (§6.1 / SH-034): SuiteVerdict MUST be pass iff
//     every Results element has Verdict=pass (including the empty-list case).
func (s SuiteResult) Valid() bool {
	if s.SuiteID == "" {
		return false
	}
	if s.StartedAt.IsZero() {
		return false
	}
	if s.CompletedAt.IsZero() {
		return false
	}
	if s.CompletedAt.Before(s.StartedAt) {
		return false
	}
	if s.FixtureRoot == "" {
		return false
	}
	if !s.CadenceFilter.Valid() {
		return false
	}
	for _, r := range s.Results {
		if !r.Valid() {
			return false
		}
	}
	if !s.SuiteVerdict.Valid() {
		return false
	}
	// Suite-verdict invariant: pass iff every result has Verdict=pass.
	allPass := true
	for _, r := range s.Results {
		if r.Verdict != ScenarioVerdictPass {
			allPass = false
			break
		}
	}
	if allPass && s.SuiteVerdict != SuiteVerdictPass {
		return false
	}
	if !allPass && s.SuiteVerdict != SuiteVerdictFail {
		return false
	}
	return true
}
