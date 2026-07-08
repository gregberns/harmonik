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
	"encoding/json"
	"os"
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

// matrixVerdictFile is the schema of the JSON verdict artifact written by
// scripts/core-loop-matrix.sh (schema_version=1, WS-E/3 hk-g6plo.3).
// WS-E/4 reads this to determine per-cell PASS/FAIL without parsing human text.
type matrixVerdictFile struct {
	SchemaVersion int                  `json:"schema_version"`
	RunAt         string               `json:"run_at"`
	Summary       matrixVerdictSummary `json:"summary"`
	Cells         []matrixVerdictCell  `json:"cells"`
}

type matrixVerdictSummary struct {
	Green   int `json:"green"`
	Red     int `json:"red"`
	Pending int `json:"pending"`
	Skip    int `json:"skip"`
	Total   int `json:"total"`
}

type matrixVerdictCell struct {
	Cell      string             `json:"cell"`
	Harness   string             `json:"harness"`
	Substrate string             `json:"substrate"`
	Verdict   string             `json:"verdict"`
	Detail    string             `json:"detail"`
	Gaps      []matrixVerdictGap `json:"gaps"`
}

type matrixVerdictGap struct {
	Gap     string `json:"gap"`
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
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
		{
			// hk-ifu19 (ST4): merge-race same-file regression scenario.
			path:              filepath.Join("scenarios", "regression", "merge-race-samefile.yaml"),
			wantCadence:       CadenceTagRegression,
			wantMinAssertions: 6, // 4 expected_events + 1 expected_workspace + 1 expected_outcome
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

// TestConformanceCorpus_MatrixVerdictSchema validates the JSON verdict file schema
// written by scripts/core-loop-matrix.sh (schema_version=1, WS-E/3 hk-g6plo.3).
// This is the §10.1 conformance-floor registry entry for the core-loop-proof matrix:
// it anchors the per-cell PASS/FAIL schema contract so WS-E/4 and the assessor can
// read it without parsing human text.
//
// Uses the golden fixture at scenarios/core-loop-proof/testdata/matrix-verdict-golden.json;
// the real artifact is written by the matrix runner on every live run to
// $SCRATCH/.harmonik/matrix-verdict.json.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceCorpus_MatrixVerdictSchema(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	goldenPath := filepath.Join(root, "scenarios", "core-loop-proof", "testdata", "matrix-verdict-golden.json")

	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", goldenPath, err)
	}

	var v matrixVerdictFile
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// schema_version must be 1.
	if v.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", v.SchemaVersion)
	}

	// run_at must be non-empty.
	if v.RunAt == "" {
		t.Error("run_at is empty")
	}

	// summary totals must be self-consistent.
	wantTotal := v.Summary.Green + v.Summary.Red + v.Summary.Pending + v.Summary.Skip
	if v.Summary.Total != wantTotal {
		t.Errorf("summary.total = %d, want %d (green=%d + red=%d + pending=%d + skip=%d)",
			v.Summary.Total, wantTotal,
			v.Summary.Green, v.Summary.Red, v.Summary.Pending, v.Summary.Skip)
	}

	// every cell must have valid enum values and a unique cell name.
	validCellVerdicts := map[string]bool{"green": true, "red": true, "pending": true, "skip": true}
	validGapVerdicts := map[string]bool{"pass": true, "fail": true, "pending": true}
	seenCells := map[string]bool{}
	for i, c := range v.Cells {
		if c.Cell == "" {
			t.Errorf("cells[%d].cell is empty", i)
		}
		if seenCells[c.Cell] {
			t.Errorf("cells[%d].cell %q is a duplicate", i, c.Cell)
		}
		seenCells[c.Cell] = true
		if !validCellVerdicts[c.Verdict] {
			t.Errorf("cells[%d] (%s) verdict = %q, want one of green|red|pending|skip", i, c.Cell, c.Verdict)
		}
		if c.Harness == "" {
			t.Errorf("cells[%d] (%s) harness is empty", i, c.Cell)
		}
		if c.Substrate == "" {
			t.Errorf("cells[%d] (%s) substrate is empty", i, c.Cell)
		}
		for j, g := range c.Gaps {
			if g.Gap == "" {
				t.Errorf("cells[%d] (%s) gaps[%d].gap is empty", i, c.Cell, j)
			}
			if !validGapVerdicts[g.Verdict] {
				t.Errorf("cells[%d] (%s) gaps[%d] (%s) verdict = %q, want one of pass|fail|pending",
					i, c.Cell, j, g.Gap, g.Verdict)
			}
		}
	}

	// cell count must match summary total.
	if len(v.Cells) != v.Summary.Total {
		t.Errorf("len(cells) = %d, want summary.total = %d", len(v.Cells), v.Summary.Total)
	}

	// can query a cell by name (the Go equivalent of jq '.cells[] | select(.cell=="codex:local")')
	cellByName := func(name string) *matrixVerdictCell {
		for i := range v.Cells {
			if v.Cells[i].Cell == name {
				return &v.Cells[i]
			}
		}
		return nil
	}

	// golden has codex:local=green with gap results; verify the jq-style lookup works.
	c := cellByName("codex:local")
	if c == nil {
		t.Error("could not query cell codex:local by name")
	} else {
		if c.Verdict != "green" {
			t.Errorf("codex:local verdict = %q, want green", c.Verdict)
		}
		if len(c.Gaps) == 0 {
			t.Error("codex:local has no gap results in golden fixture")
		}
	}

	t.Logf("OK: schema_version=%d cells=%d summary=green:%d red:%d pending:%d skip:%d",
		v.SchemaVersion, len(v.Cells),
		v.Summary.Green, v.Summary.Red, v.Summary.Pending, v.Summary.Skip)
}
