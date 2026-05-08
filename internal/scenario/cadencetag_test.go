package scenario

import (
	"encoding/json"
	"testing"
)

// cadenceTagFixtureSmoke returns the smoke CadenceTag constant for use in tests.
func cadenceTagFixtureSmoke(t *testing.T) CadenceTag {
	t.Helper()
	return CadenceTagSmoke
}

// cadenceTagFixtureRegression returns the regression CadenceTag constant for
// use in tests.
func cadenceTagFixtureRegression(t *testing.T) CadenceTag {
	t.Helper()
	return CadenceTagRegression
}

// cadenceTagFixtureNightly returns the nightly CadenceTag constant for use in
// tests.
func cadenceTagFixtureNightly(t *testing.T) CadenceTag {
	t.Helper()
	return CadenceTagNightly
}

func TestCadenceTagValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CadenceTag
		want  bool
	}{
		{"smoke", CadenceTagSmoke, true},
		{"regression", CadenceTagRegression, true},
		{"nightly", CadenceTagNightly, true},
		{"empty string", CadenceTag(""), false},
		{"unknown value", CadenceTag("unknown"), false},
		{"mixed case smoke", CadenceTag("Smoke"), false},
		{"mixed case regression", CadenceTag("Regression"), false},
		{"mixed case nightly", CadenceTag("Nightly"), false},
		{"all is not a valid tag", CadenceTag("all"), false},
		{"partial smoke", CadenceTag("smok"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("CadenceTag(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestCadenceTagMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   CadenceTag
		want    string
		wantErr bool
	}{
		{"smoke", CadenceTagSmoke, "smoke", false},
		{"regression", CadenceTagRegression, "regression", false},
		{"nightly", CadenceTagNightly, "nightly", false},
		{"empty string is invalid", CadenceTag(""), "", true},
		{"unknown value", CadenceTag("weirdtag"), "", true},
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

func TestCadenceTagUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    CadenceTag
		wantErr bool
	}{
		{"smoke", "smoke", CadenceTagSmoke, false},
		{"regression", "regression", CadenceTagRegression, false},
		{"nightly", "nightly", CadenceTagNightly, false},
		{"empty string rejected", "", CadenceTag(""), true},
		{"unknown rejected", "unknown", CadenceTag(""), true},
		{"mixed-case smoke rejected", "Smoke", CadenceTag(""), true},
		{"all is not a valid tag", "all", CadenceTag(""), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var v CadenceTag
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

func TestCadenceTagJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tags := []CadenceTag{
		CadenceTagSmoke,
		CadenceTagRegression,
		CadenceTagNightly,
	}

	for _, tag := range tags {
		t.Run(string(tag), func(t *testing.T) {
			t.Parallel()

			b, err := tag.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(tag), err)
			}

			var got CadenceTag
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(b), err)
			}

			if got != tag {
				t.Errorf("cadence tag round-trip: in=%q out=%q", string(tag), string(got))
			}
		})
	}
}

func TestCadenceTagJSONFieldRoundTrip(t *testing.T) {
	t.Parallel()

	// Verify CadenceTag serialises correctly when embedded in a JSON struct.
	type wrapper struct {
		Tag CadenceTag `json:"cadence_tag"`
	}

	for _, tag := range []CadenceTag{CadenceTagSmoke, CadenceTagRegression, CadenceTagNightly} {
		t.Run(string(tag), func(t *testing.T) {
			t.Parallel()

			in := wrapper{Tag: tag}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if out.Tag != tag {
				t.Errorf("JSON field round-trip: in=%q out=%q (encoded: %s)", string(tag), string(out.Tag), data)
			}
		})
	}
}

func TestCadenceFilterValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CadenceFilter
		want  bool
	}{
		{"smoke", CadenceFilterSmoke, true},
		{"regression", CadenceFilterRegression, true},
		{"nightly", CadenceFilterNightly, true},
		{"all", CadenceFilterAll, true},
		{"empty string", CadenceFilter(""), false},
		{"unknown value", CadenceFilter("unknown"), false},
		{"mixed case smoke", CadenceFilter("Smoke"), false},
		{"mixed case all", CadenceFilter("All"), false},
		{"partial", CadenceFilter("smok"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("CadenceFilter(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestCadenceFilterMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   CadenceFilter
		want    string
		wantErr bool
	}{
		{"smoke", CadenceFilterSmoke, "smoke", false},
		{"regression", CadenceFilterRegression, "regression", false},
		{"nightly", CadenceFilterNightly, "nightly", false},
		{"all", CadenceFilterAll, "all", false},
		{"empty string is invalid", CadenceFilter(""), "", true},
		{"unknown value", CadenceFilter("weirdfilter"), "", true},
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

func TestCadenceFilterUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    CadenceFilter
		wantErr bool
	}{
		{"smoke", "smoke", CadenceFilterSmoke, false},
		{"regression", "regression", CadenceFilterRegression, false},
		{"nightly", "nightly", CadenceFilterNightly, false},
		{"all", "all", CadenceFilterAll, false},
		{"empty string rejected", "", CadenceFilter(""), true},
		{"unknown rejected", "unknown", CadenceFilter(""), true},
		{"mixed-case rejected", "Smoke", CadenceFilter(""), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var f CadenceFilter
			err := f.UnmarshalText([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Errorf("UnmarshalText(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalText(%q) unexpected error: %v", tc.input, err)
			}
			if f != tc.want {
				t.Errorf("UnmarshalText(%q) = %q, want %q", tc.input, string(f), string(tc.want))
			}
		})
	}
}

func TestCadenceFilterJSONRoundTrip(t *testing.T) {
	t.Parallel()

	filters := []CadenceFilter{
		CadenceFilterSmoke,
		CadenceFilterRegression,
		CadenceFilterNightly,
		CadenceFilterAll,
	}

	for _, f := range filters {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			b, err := f.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(f), err)
			}

			var got CadenceFilter
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(b), err)
			}

			if got != f {
				t.Errorf("cadence filter round-trip: in=%q out=%q", string(f), string(got))
			}
		})
	}
}

// TestCadenceFilterIncludes verifies the SH-029 superset relation for every
// (filter, tag) combination.
//
// SH-029 table:
//
//	smoke      → smoke
//	regression → smoke, regression
//	nightly    → smoke, regression, nightly
//	all        → smoke, regression, nightly
func TestCadenceFilterIncludes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filter CadenceFilter
		tag    CadenceTag
		want   bool
	}{
		// smoke filter: only smoke-tagged scenarios
		{CadenceFilterSmoke, CadenceTagSmoke, true},
		{CadenceFilterSmoke, CadenceTagRegression, false},
		{CadenceFilterSmoke, CadenceTagNightly, false},

		// regression filter: smoke and regression
		{CadenceFilterRegression, CadenceTagSmoke, true},
		{CadenceFilterRegression, CadenceTagRegression, true},
		{CadenceFilterRegression, CadenceTagNightly, false},

		// nightly filter: all three
		{CadenceFilterNightly, CadenceTagSmoke, true},
		{CadenceFilterNightly, CadenceTagRegression, true},
		{CadenceFilterNightly, CadenceTagNightly, true},

		// all filter: all three (same as nightly)
		{CadenceFilterAll, CadenceTagSmoke, true},
		{CadenceFilterAll, CadenceTagRegression, true},
		{CadenceFilterAll, CadenceTagNightly, true},
	}

	for _, tc := range tests {
		tc := tc
		name := string(tc.filter) + "/" + string(tc.tag)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := tc.filter.Includes(tc.tag)
			if got != tc.want {
				t.Errorf("CadenceFilter(%q).Includes(%q) = %v, want %v",
					string(tc.filter), string(tc.tag), got, tc.want)
			}
		})
	}
}

// TestCadenceFilterIncludes_NightlyEquivalentToAll verifies that the nightly
// and all filters produce identical Includes results for every tag per SH-029.
func TestCadenceFilterIncludes_NightlyEquivalentToAll(t *testing.T) {
	t.Parallel()

	allTags := []CadenceTag{CadenceTagSmoke, CadenceTagRegression, CadenceTagNightly}
	for _, tag := range allTags {
		t.Run(string(tag), func(t *testing.T) {
			t.Parallel()
			nightlyResult := CadenceFilterNightly.Includes(tag)
			allResult := CadenceFilterAll.Includes(tag)
			if nightlyResult != allResult {
				t.Errorf("CadenceFilterNightly.Includes(%q)=%v != CadenceFilterAll.Includes(%q)=%v; "+
					"nightly and all must be equivalent per SH-029",
					string(tag), nightlyResult, string(tag), allResult)
			}
		})
	}
}

// TestCadenceFilterIncludes_SmokeIsSubsetOfRegression verifies that any tag
// included by smoke is also included by regression per SH-029 superset relation.
func TestCadenceFilterIncludes_SmokeIsSubsetOfRegression(t *testing.T) {
	t.Parallel()

	allTags := []CadenceTag{CadenceTagSmoke, CadenceTagRegression, CadenceTagNightly}
	for _, tag := range allTags {
		t.Run(string(tag), func(t *testing.T) {
			t.Parallel()
			if CadenceFilterSmoke.Includes(tag) && !CadenceFilterRegression.Includes(tag) {
				t.Errorf("smoke includes %q but regression does not; regression MUST be a superset of smoke per SH-029",
					string(tag))
			}
		})
	}
}

// TestCadenceFilterIncludes_RegressionIsSubsetOfNightly verifies that any tag
// included by regression is also included by nightly per SH-029 superset relation.
func TestCadenceFilterIncludes_RegressionIsSubsetOfNightly(t *testing.T) {
	t.Parallel()

	allTags := []CadenceTag{CadenceTagSmoke, CadenceTagRegression, CadenceTagNightly}
	for _, tag := range allTags {
		t.Run(string(tag), func(t *testing.T) {
			t.Parallel()
			if CadenceFilterRegression.Includes(tag) && !CadenceFilterNightly.Includes(tag) {
				t.Errorf("regression includes %q but nightly does not; nightly MUST be a superset of regression per SH-029",
					string(tag))
			}
		})
	}
}

// TestCadenceFilterIncludes_EmptyResultVacuouslyPass documents the SH-029
// contract that a filter resolving to zero scenarios MUST emit suite_verdict=pass.
// This test verifies that the Includes logic can produce false for all tags
// under the smoke filter when the scenario corpus contains only nightly-tagged
// items — i.e., that the empty-set condition is reachable from the filter
// superset table, confirming the harness caller MUST handle it.
func TestCadenceFilterIncludes_EmptyResultVacuouslyPass(t *testing.T) {
	t.Parallel()

	// A smoke filter against a nightly-only corpus yields zero matching scenarios.
	nightlyOnlyCorpus := []CadenceTag{CadenceTagNightly, CadenceTagNightly}
	matchCount := 0
	for _, tag := range nightlyOnlyCorpus {
		if CadenceFilterSmoke.Includes(tag) {
			matchCount++
		}
	}
	if matchCount != 0 {
		t.Errorf("smoke filter against nightly-only corpus: expected 0 matches (vacuous pass), got %d", matchCount)
	}

	// Document: when matchCount == 0 the harness MUST emit suite_verdict=pass
	// with an empty results list per SH-029. The enforcement of that rule is at
	// the harness runner layer, not in the Includes predicate itself.
}

// TestCadenceTagFixtures verifies that the fixture helpers return the expected
// constant values; these helpers are shared across sibling test files.
func TestCadenceTagFixtures(t *testing.T) {
	t.Parallel()

	if cadenceTagFixtureSmoke(t) != CadenceTagSmoke {
		t.Errorf("cadenceTagFixtureSmoke: got %q, want %q", cadenceTagFixtureSmoke(t), CadenceTagSmoke)
	}
	if cadenceTagFixtureRegression(t) != CadenceTagRegression {
		t.Errorf("cadenceTagFixtureRegression: got %q, want %q", cadenceTagFixtureRegression(t), CadenceTagRegression)
	}
	if cadenceTagFixtureNightly(t) != CadenceTagNightly {
		t.Errorf("cadenceTagFixtureNightly: got %q, want %q", cadenceTagFixtureNightly(t), CadenceTagNightly)
	}
}
