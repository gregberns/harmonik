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
	"os/exec"
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

// TestConformanceGate_BlocksOnRedCell exercises the conformance-floor execution gate
// (scripts/conformance-gate.sh, WS-E/4 hk-g6plo.4) against a verdict file that contains
// a red cell.  The gate MUST exit non-zero (BLOCK). Acceptance criteria for hk-pnjgh.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceGate_BlocksOnRedCell(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	gateScript := filepath.Join(root, "scripts", "conformance-gate.sh")
	if _, err := os.Stat(gateScript); err != nil {
		t.Fatalf("conformance-gate.sh not found: %v", err)
	}

	// The golden fixture has summary.red=1 (pi:local cell is red).
	verdictFile := filepath.Join(root, "scenarios", "core-loop-proof", "testdata", "matrix-verdict-golden.json")
	cmd := exec.Command("bash", gateScript,
		"--verdict", verdictFile,
		"--skip-block-query",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("conformance-gate.sh exited 0 (PASS) on a red-cell verdict; want non-zero (BLOCK)\noutput:\n%s", out)
	}
	// Verify the gate emitted a BLOCK verdict line.
	outStr := string(out)
	if !containsLine(outStr, "GATE_VERDICT=BLOCK") {
		t.Errorf("expected GATE_VERDICT=BLOCK in output; got:\n%s", outStr)
	}
	t.Logf("OK: gate blocked as expected\n%s", outStr)
}

// TestConformanceGate_BlocksOnPendingCell exercises the gate against a verdict file that
// contains a pending cell.  Per the T9 full-green requirement, residual PENDING must BLOCK.
// Acceptance criteria for hk-pnjgh.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceGate_BlocksOnPendingCell(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	gateScript := filepath.Join(root, "scripts", "conformance-gate.sh")
	if _, err := os.Stat(gateScript); err != nil {
		t.Fatalf("conformance-gate.sh not found: %v", err)
	}

	verdictFile := filepath.Join(root, "scenarios", "core-loop-proof", "testdata", "matrix-verdict-pending.json")
	cmd := exec.Command("bash", gateScript,
		"--verdict", verdictFile,
		"--skip-block-query",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("conformance-gate.sh exited 0 (PASS) on a pending-cell verdict; want non-zero (BLOCK)\noutput:\n%s", out)
	}
	outStr := string(out)
	if !containsLine(outStr, "GATE_VERDICT=BLOCK") {
		t.Errorf("expected GATE_VERDICT=BLOCK in output; got:\n%s", outStr)
	}
	t.Logf("OK: gate blocked on pending cell as expected\n%s", outStr)
}

// TestConformanceGate_PassesOnFullGreen exercises the gate against a full-green verdict
// file (all fixtured cells green; skip cells do not count against the gate).  With the
// block query skipped the gate MUST exit zero (PASS). Acceptance criteria for hk-pnjgh.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceGate_PassesOnFullGreen(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	gateScript := filepath.Join(root, "scripts", "conformance-gate.sh")
	if _, err := os.Stat(gateScript); err != nil {
		t.Fatalf("conformance-gate.sh not found: %v", err)
	}

	verdictFile := filepath.Join(root, "scenarios", "core-loop-proof", "testdata", "matrix-verdict-allgreen.json")
	cmd := exec.Command("bash", gateScript,
		"--verdict", verdictFile,
		"--skip-block-query",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("conformance-gate.sh exited non-zero on a full-green verdict; want 0 (PASS)\noutput:\n%s", out)
	}
	outStr := string(out)
	if !containsLine(outStr, "GATE_VERDICT=PASS") {
		t.Errorf("expected GATE_VERDICT=PASS in output; got:\n%s", outStr)
	}
	t.Logf("OK: gate passed as expected\n%s", outStr)
}

// TestConformanceGate_BlocksOnMissingVerdictFile proves fail-closed behaviour: the gate
// must exit non-zero when the verdict file is absent (never false-green).
// Acceptance criteria for hk-pnjgh.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceGate_BlocksOnMissingVerdictFile(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	gateScript := filepath.Join(root, "scripts", "conformance-gate.sh")
	if _, err := os.Stat(gateScript); err != nil {
		t.Fatalf("conformance-gate.sh not found: %v", err)
	}

	cmd := exec.Command("bash", gateScript,
		"--verdict", filepath.Join(t.TempDir(), "nonexistent.json"),
		"--skip-block-query",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("conformance-gate.sh exited 0 (PASS) on a missing verdict file; want non-zero (BLOCK)\noutput:\n%s", out)
	}
	outStr := string(out)
	if !containsLine(outStr, "GATE_VERDICT=BLOCK") {
		t.Errorf("expected GATE_VERDICT=BLOCK in output; got:\n%s", outStr)
	}
	t.Logf("OK: gate blocked on missing file as expected\n%s", outStr)
}

// TestConformanceCorpus_PiTier3ModelLeakGate is the §10.1 conformance-corpus
// enforcement gate for the pi tier-3 model-leak bug (hk-pkugu, hk-vovyi).
//
// The FIX (workloop resolving the harness agent-type up front via
// resolveHarnessAgentTypeQuiet before calling ResolveModelPreference, instead of
// hardcoding agentType=claude-code) is unit-tested in isolation by
// internal/daemon/hk_pkugu_pi_model_leak_test.go and
// internal/daemon/hk_pkugu_pi_launch_e2e_test.go. What was missing is a standing
// entry in the §10.1 conformance corpus — the cells.json/ndjson matrix gated by
// scripts/conformance-gate.sh — that drives the same class of leak assertion
// through the production core-loop-assert.jq contract. This mirrors the
// hk-d170r-over-hk-heh3t pattern: a routing/scenario gate registered ABOVE a
// fix's unit tests, so a regression re-breaks the standing corpus GATE, not
// production.
//
// Runs the real assertion library (scripts/core-loop-assert.jq) — the same
// contract the live core-loop-proof matrix runner uses — against captured
// event-stream fixtures for the pi:local cell, extended (hk-vovyi) to forbid
// BOTH known pi model-leak values: the hk-lfrub node-model-pin leak
// (claude-opus-4-8) and the hk-pkugu tier-3-default leak
// (claude-sonnet-4-6 / sonnet). Zero Claude tokens: pure ndjson replay through
// jq, no live daemon or provider.
//
// Spec ref: specs/scenario-harness.md §10.1 (conformance scenario set).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
func TestConformanceCorpus_PiTier3ModelLeakGate(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}

	root := conformanceCorpusFixtureRepoRoot(t)
	lib := filepath.Join(root, "scripts", "core-loop-assert.jq")
	td := filepath.Join(root, "scenarios", "core-loop-proof", "testdata")

	// pi:local cell spec (cells.json), no_leak_models extended per hk-vovyi to
	// cover the hk-pkugu tier-3-default leak alongside the pre-existing
	// hk-lfrub node-model-pin leak.
	const spec = `{"schema_version":1,"cell":"pi:local","seed_bead":"hk-clp-pi","expect":{"harness_selected":{"agent_type":"pi","tier":1},"model_selected":{"harness":"pi","model":"deepseek-reasoner","no_leak_models":["claude-opus-4-8","claude-sonnet-4-6","sonnet"]}},"gaps":["gap1"]}`

	cases := []struct {
		name   string
		stream string
		want   string
	}{
		{"hk-lfrub node-pin leak still caught", "pi-local-modelleak.ndjson", "fail"},
		{"hk-pkugu tier-3-default leak caught", "pi-local-tier3leak.ndjson", "fail"},
		{"clean pi run passes (no false positive)", "pi-local-pass.ndjson", "pass"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := conformanceCorpusFixtureGap1Verdict(t, lib, filepath.Join(td, tc.stream), spec)
			if got != tc.want {
				t.Errorf("gap1 verdict for %s = %q; want %q", tc.stream, got, tc.want)
			}
		})
	}
}

// conformanceCorpusFixtureGap1Verdict runs streamPath through the core-loop-assert.jq
// library against spec and returns the gap1 verdict ("pass"|"fail"|"pending").
func conformanceCorpusFixtureGap1Verdict(t *testing.T, lib, streamPath, spec string) string {
	t.Helper()

	cmd := exec.Command("jq", "-n",
		"--slurpfile", "events", streamPath,
		"--argjson", "spec", spec,
		"--argjson", "ref_events", "null",
		"-f", lib,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("jq assert-library run failed: %v\noutput: %s", err, out)
	}

	var results []struct {
		Gap     string `json:"gap"`
		Verdict string `json:"verdict"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("unmarshal jq output: %v\noutput: %s", err, out)
	}
	for _, r := range results {
		if r.Gap == "gap1" {
			return r.Verdict
		}
	}
	t.Fatalf("no gap1 result in jq output: %s", out)
	return ""
}

// conformanceGateFixtureContainsLine returns true when s contains a line equal to want
// (ignoring leading/trailing whitespace on each line).
func containsLine(s, want string) bool {
	for _, line := range splitLines(s) {
		if trimSpace(line) == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
