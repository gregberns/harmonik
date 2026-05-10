package scenario

// conformancecorpus_test.go — corpus tests for the §10.1 conformance scenario set.
//
// Per specs/scenario-harness.md §10.2 SH-001–SH-007 test obligation:
// "corpus tests on the conformance scenario set verifying every file parses."
//
// This file asserts that every scenario file declared in the §10.1 conformance
// floor (a) passes ParseScenarioFile, (b) has the expected cadence tag, and
// (c) has at least one expected_event or expected_outcome assertion.
// It is intentionally a structural corpus check, NOT an execution check; the
// acceptance criterion "scenario runs against the built twin and produces
// verdict=pass" is an integration-test obligation that requires a built harness.
//
// Helper prefix: conformanceCorpusFixture (per implementer-protocol.md
// §Helper-prefix discipline; hk-ahvq.48.6, hk-ahvq.48.7, hk-ahvq.48.8).
//
// Spec ref: specs/scenario-harness.md §10.1, §10.2 (SH-001–SH-007 obligation).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"path/filepath"
	"runtime"
	"testing"
)

// conformanceCorpusFixtureRepoRoot returns the absolute path to the repo root
// by walking up two directories from this file's location.
func conformanceCorpusFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	// __file__ is in internal/scenario/ — repo root is two levels up.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("conformanceCorpusFixtureRepoRoot: runtime.Caller(0) failed")
	}
	// file is the absolute path to this source file.
	// Walk: internal/scenario/conformancecorpus_test.go → internal/scenario → internal → <root>
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	return root
}

// TestConformanceCorpus_SH101ScenariosParse verifies that every §10.1 conformance
// scenario file passes ParseScenarioFile and is structurally valid.
//
// Spec ref: specs/scenario-harness.md §10.1 (conformance scenario set),
// §10.2 SH-001–SH-007 corpus obligation.
func TestConformanceCorpus_SH101ScenariosParse(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)

	cases := []struct {
		// path is the repo-relative path to the scenario file under scenarios/.
		path string
		// wantCadence is the expected cadence_tag value.
		wantCadence CadenceTag
		// wantMinAssertions is the minimum number of expected_events +
		// expected_outcome assertions declared (as a sanity guard).
		wantMinAssertions int
	}{
		{
			// hk-ahvq.48.6: first conformance scenario.
			path:              filepath.Join("scenarios", "smoke", "twin-launch-and-ready.yaml"),
			wantCadence:       CadenceTagSmoke,
			wantMinAssertions: 2, // event_present(agent_ready) + event_present(agent_completed) + outcome
		},
		{
			// hk-ahvq.48.7: second conformance scenario.
			path:              filepath.Join("scenarios", "smoke", "checkpoint-and-merge.yaml"),
			wantCadence:       CadenceTagSmoke,
			wantMinAssertions: 3, // checkpoint_written + 2x workspace_merge_status + outcome + workspace_state
		},
		{
			// hk-ahvq.48.8: third conformance scenario.
			path:              filepath.Join("scenarios", "regression", "twin-failure-classification.yaml"),
			wantCadence:       CadenceTagRegression,
			wantMinAssertions: 2, // agent_failed + event_absent(outcome_emitted) + outcome
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			absPath := filepath.Join(root, tc.path)
			sf, err := ParseScenarioFile(absPath)
			if err != nil {
				t.Fatalf("ParseScenarioFile(%q): %v", tc.path, err)
			}

			// Cadence tag must match the expected value.
			if sf.CadenceTag != tc.wantCadence {
				t.Errorf("CadenceTag = %q, want %q", sf.CadenceTag, tc.wantCadence)
			}

			// At least wantMinAssertions assertions must be declared.
			totalAssertions := len(sf.ExpectedEvents)
			if sf.ExpectedOutcome != nil {
				totalAssertions++
			}
			totalAssertions += len(sf.ExpectedWorkspace)
			if totalAssertions < tc.wantMinAssertions {
				t.Errorf("total assertions = %d, want >= %d", totalAssertions, tc.wantMinAssertions)
			}

			// Every declared AgentOverride must be structurally valid.
			for role, ao := range sf.AgentOverrides {
				if !ao.Valid() {
					t.Errorf("AgentOverride[%q].Valid() = false", role)
				}
			}

			// FixtureSetup must be valid.
			if !sf.FixtureSetup.Valid() {
				t.Error("FixtureSetup.Valid() = false")
			}

			// TimeoutSecs must be positive and within the SH-025 range.
			if sf.TimeoutSecs < 1 || sf.TimeoutSecs > 7200 {
				t.Errorf("TimeoutSecs = %d, want [1, 7200]", sf.TimeoutSecs)
			}

			t.Logf("OK: name=%q cadence=%q timeout=%ds agents=%d events=%d workspace=%d outcome=%v",
				sf.Name, sf.CadenceTag, sf.TimeoutSecs,
				len(sf.AgentOverrides), len(sf.ExpectedEvents),
				len(sf.ExpectedWorkspace), sf.ExpectedOutcome != nil)
		})
	}
}
