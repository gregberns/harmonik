package scenario

import "fmt"

// CadenceTag is the per-scenario cadence membership tag declared in every
// ScenarioFile. It is one of the three values smoke, regression, or nightly.
//
// Spec ref: specs/scenario-harness.md §4.9 SH-029, §6.1 RECORD ScenarioFile
// field cadence_tag.
type CadenceTag string

// Declared CadenceTag constants per specs/scenario-harness.md §4.9 SH-029.
const (
	// CadenceTagSmoke identifies scenarios included in every CI lane.
	// Smoke scenarios MUST be fast and cover the most critical paths.
	CadenceTagSmoke CadenceTag = "smoke"

	// CadenceTagRegression identifies scenarios run in smoke and regression lanes
	// (superset of smoke). Regression scenarios cover broader correctness checks.
	CadenceTagRegression CadenceTag = "regression"

	// CadenceTagNightly identifies scenarios run in all lanes (smoke, regression,
	// and nightly). Nightly scenarios MAY be slow or exercise wall-clock-mode
	// behaviour per SH-027's carve-out.
	CadenceTagNightly CadenceTag = "nightly"
)

// Valid reports whether t is one of the three declared CadenceTag constants.
// An unknown or empty value is invalid and MUST cause a scenario-load-failure
// per SH-029 and §4.2 SH-004.
func (t CadenceTag) Valid() bool {
	switch t {
	case CadenceTagSmoke, CadenceTagRegression, CadenceTagNightly:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CadenceTag serialises
// correctly in JSON and YAML output.
func (t CadenceTag) MarshalText() ([]byte, error) {
	if !t.Valid() {
		return nil, fmt.Errorf("cadencetag: unknown value %q", string(t))
	}
	return []byte(t), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value not
// in the declared set; such a value MUST be surfaced as scenario-load-failure
// by the harness loader.
func (t *CadenceTag) UnmarshalText(text []byte) error {
	candidate := CadenceTag(text)
	if !candidate.Valid() {
		return fmt.Errorf("cadencetag: unknown value %q; must be one of smoke, regression, nightly", string(text))
	}
	*t = candidate
	return nil
}

// CadenceFilter is the value of the harness --cadence CLI flag. It controls
// which scenarios are included in a suite run via the superset relation defined
// in SH-029. The sentinel value "all" (or a missing flag) runs every scenario
// regardless of cadence tag.
//
// Spec ref: specs/scenario-harness.md §4.9 SH-029, §6.1 RECORD SuiteResult
// field cadence_filter.
type CadenceFilter string

// Declared CadenceFilter constants per specs/scenario-harness.md §4.9 SH-029.
const (
	// CadenceFilterSmoke selects only smoke-tagged scenarios.
	CadenceFilterSmoke CadenceFilter = "smoke"

	// CadenceFilterRegression selects smoke and regression-tagged scenarios.
	CadenceFilterRegression CadenceFilter = "regression"

	// CadenceFilterNightly selects smoke, regression, and nightly-tagged scenarios.
	CadenceFilterNightly CadenceFilter = "nightly"

	// CadenceFilterAll selects every scenario regardless of cadence tag.
	// This is the default when --cadence is omitted.
	CadenceFilterAll CadenceFilter = "all"
)

// Valid reports whether f is one of the four declared CadenceFilter constants.
func (f CadenceFilter) Valid() bool {
	switch f {
	case CadenceFilterSmoke, CadenceFilterRegression, CadenceFilterNightly, CadenceFilterAll:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so CadenceFilter serialises
// correctly in JSON and YAML output.
func (f CadenceFilter) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("cadencefilter: unknown value %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value not
// in the declared set.
func (f *CadenceFilter) UnmarshalText(text []byte) error {
	candidate := CadenceFilter(text)
	if !candidate.Valid() {
		return fmt.Errorf("cadencefilter: unknown value %q; must be one of smoke, regression, nightly, all", string(text))
	}
	*f = candidate
	return nil
}

// Includes reports whether a scenario carrying tag t should be included in a
// suite run governed by filter f. The superset relation is defined in
// specs/scenario-harness.md §4.9 SH-029:
//
//   - smoke    → includes scenarios tagged smoke
//   - regression → includes scenarios tagged smoke or regression
//   - nightly  → includes scenarios tagged smoke, regression, or nightly
//   - all      → includes all three tags (equivalent to nightly)
//
// Includes returns false if f is not a valid CadenceFilter; callers SHOULD
// validate f via Valid() before calling Includes. Returning false on an
// unknown filter rather than panicking keeps Includes safe for use in
// production paths (no panic outside testhelpers per project lint policy).
func (f CadenceFilter) Includes(t CadenceTag) bool {
	switch f {
	case CadenceFilterSmoke:
		return t == CadenceTagSmoke
	case CadenceFilterRegression:
		return t == CadenceTagSmoke || t == CadenceTagRegression
	case CadenceFilterNightly, CadenceFilterAll:
		return t == CadenceTagSmoke || t == CadenceTagRegression || t == CadenceTagNightly
	default:
		return false
	}
}
