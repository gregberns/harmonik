package scenario

// sh034_result_emit_test.go — contract tests for SH-034 result emission.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
//
// SH-034 mandates:
//   - Every scenario's ScenarioResult MUST be written to
//     <fixture-root>/<scenario-name>/result.json immediately after the scenario
//     completes (before the next scenario begins).
//   - The aggregate SuiteResult MUST be written to
//     <fixture-root>/suite-result.json at suite completion.
//   - SuiteResult MUST be emitted to stdout in the chosen format (human|json).
//
// Helper prefix: sh034 (per implementer-protocol.md §Helper-prefix discipline).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func sh034TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-sh034-")
	if err != nil {
		t.Fatalf("sh034TempDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func sh034MinimalScenarioResult(name string) ScenarioResult {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return ScenarioResult{
		ScenarioName:          name,
		SourcePath:            "scenarios/smoke/" + name + ".yaml",
		StartedAt:             now,
		CompletedAt:           now.Add(time.Second),
		Verdict:               ScenarioVerdictPass,
		AssertionResults:      []AssertionResult{},
		EventLogPath:          name + "/.harmonik/events/events.jsonl",
		WorkspaceSnapshotPath: name + "/workspace",
	}
}

func sh034MinimalSuiteResult(t *testing.T, fixtureRoot string, results []ScenarioResult) SuiteResult {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	verdict := SuiteVerdictPass
	for _, r := range results {
		if r.Verdict != ScenarioVerdictPass {
			verdict = SuiteVerdictFail
			break
		}
	}
	return SuiteResult{
		SuiteID:       mustParseSuiteID(t, "018f5e1a-0000-7034-8000-000000000001"),
		StartedAt:     now,
		CompletedAt:   now.Add(2 * time.Second),
		FixtureRoot:   fixtureRoot,
		CadenceFilter: CadenceFilterSmoke,
		Results:       results,
		SuiteVerdict:  verdict,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Path helpers
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_ScenarioResultPath verifies the path formula
// <fixture-root>/<scenario-name>/result.json.
func TestSH034_ScenarioResultPath(t *testing.T) {
	t.Parallel()

	got := ScenarioResultPath("/tmp/fixture-root", "my-scenario")
	want := filepath.Join("/tmp/fixture-root", "my-scenario", "result.json")
	if got != want {
		t.Errorf("ScenarioResultPath = %q; want %q", got, want)
	}
}

// TestSH034_SuiteResultPath verifies the path formula
// <fixture-root>/suite-result.json.
func TestSH034_SuiteResultPath(t *testing.T) {
	t.Parallel()

	got := SuiteResultPath("/tmp/fixture-root")
	want := filepath.Join("/tmp/fixture-root", "suite-result.json")
	if got != want {
		t.Errorf("SuiteResultPath = %q; want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WriteScenarioResult
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_WriteScenarioResult_WritesValidJSON verifies that WriteScenarioResult
// writes valid JSON containing the scenario name to the expected path.
func TestSH034_WriteScenarioResult_WritesValidJSON(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	r := sh034MinimalScenarioResult("twin-launch-and-ready")

	if err := WriteScenarioResult(fixtureRoot, r); err != nil {
		t.Fatalf("WriteScenarioResult: %v", err)
	}

	p := ScenarioResultPath(fixtureRoot, r.ScenarioName)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", p, err)
	}

	var decoded ScenarioResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.ScenarioName != r.ScenarioName {
		t.Errorf("decoded.ScenarioName = %q; want %q", decoded.ScenarioName, r.ScenarioName)
	}
	if decoded.Verdict != r.Verdict {
		t.Errorf("decoded.Verdict = %q; want %q", decoded.Verdict, r.Verdict)
	}
}

// TestSH034_WriteScenarioResult_PathFormula verifies the file is written at
// exactly <fixture-root>/<scenario-name>/result.json per SH-034.
func TestSH034_WriteScenarioResult_PathFormula(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	const name = "checkpoint-and-merge"
	r := sh034MinimalScenarioResult(name)

	if err := WriteScenarioResult(fixtureRoot, r); err != nil {
		t.Fatalf("WriteScenarioResult: %v", err)
	}

	wantPath := filepath.Join(fixtureRoot, name, "result.json")
	if _, err := os.Stat(wantPath); os.IsNotExist(err) {
		t.Errorf("SH-034: result.json not found at %q", wantPath)
	}
}

// TestSH034_WriteScenarioResult_CreatesParentDir verifies that
// WriteScenarioResult creates the <fixture-root>/<scenario-name>/ directory if
// it does not already exist (e.g. scenario failed before fixture-setup created it).
func TestSH034_WriteScenarioResult_CreatesParentDir(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	r := sh034MinimalScenarioResult("new-scenario")

	// Parent dir does not exist; WriteScenarioResult must create it.
	if err := WriteScenarioResult(fixtureRoot, r); err != nil {
		t.Fatalf("WriteScenarioResult: %v", err)
	}

	p := ScenarioResultPath(fixtureRoot, r.ScenarioName)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Errorf("SH-034: result.json not found at %q after directory creation", p)
	}
}

// TestSH034_WriteScenarioResult_MultipleScenarios verifies that
// WriteScenarioResult writes independent files for each scenario name, satisfying
// SH-034's "one result.json per executed scenario" contract.
func TestSH034_WriteScenarioResult_MultipleScenarios(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	names := []string{"scenario-a", "scenario-b", "scenario-c"}

	for _, name := range names {
		r := sh034MinimalScenarioResult(name)
		if err := WriteScenarioResult(fixtureRoot, r); err != nil {
			t.Fatalf("WriteScenarioResult(%q): %v", name, err)
		}
	}

	for _, name := range names {
		p := ScenarioResultPath(fixtureRoot, name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("ReadFile %q: %v", p, err)
		}
		var decoded ScenarioResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal %q: %v", name, err)
		}
		if decoded.ScenarioName != name {
			t.Errorf("result for %q has ScenarioName=%q; want %q",
				name, decoded.ScenarioName, name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WriteSuiteResult
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_WriteSuiteResult_WritesValidJSON verifies that WriteSuiteResult
// writes valid JSON containing the suite verdict to the expected path.
func TestSH034_WriteSuiteResult_WritesValidJSON(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, []ScenarioResult{
		sh034MinimalScenarioResult("s1"),
	})

	if err := WriteSuiteResult(fixtureRoot, sr); err != nil {
		t.Fatalf("WriteSuiteResult: %v", err)
	}

	p := SuiteResultPath(fixtureRoot)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", p, err)
	}

	var decoded SuiteResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.SuiteVerdict != sr.SuiteVerdict {
		t.Errorf("decoded.SuiteVerdict = %q; want %q", decoded.SuiteVerdict, sr.SuiteVerdict)
	}
	if len(decoded.Results) != 1 {
		t.Errorf("decoded.Results len = %d; want 1", len(decoded.Results))
	}
}

// TestSH034_WriteSuiteResult_PathFormula verifies the file is written at
// exactly <fixture-root>/suite-result.json per SH-034.
func TestSH034_WriteSuiteResult_PathFormula(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, nil)

	if err := WriteSuiteResult(fixtureRoot, sr); err != nil {
		t.Fatalf("WriteSuiteResult: %v", err)
	}

	wantPath := filepath.Join(fixtureRoot, "suite-result.json")
	if _, err := os.Stat(wantPath); os.IsNotExist(err) {
		t.Errorf("SH-034: suite-result.json not found at %q", wantPath)
	}
}

// TestSH034_WriteSuiteResult_VacuousPassOnEmptyResults verifies that
// WriteSuiteResult correctly writes suite_verdict=pass when Results is empty
// (vacuous truth per SH-029 / SuiteResult.Valid()).
func TestSH034_WriteSuiteResult_VacuousPassOnEmptyResults(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, nil)

	if err := WriteSuiteResult(fixtureRoot, sr); err != nil {
		t.Fatalf("WriteSuiteResult: %v", err)
	}

	p := SuiteResultPath(fixtureRoot)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", p, err)
	}
	var decoded SuiteResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.SuiteVerdict != SuiteVerdictPass {
		t.Errorf("SuiteVerdict = %q; want pass (vacuous truth)", decoded.SuiteVerdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EmitSuiteResult — JSON format
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_EmitSuiteResult_JSONFormat_ValidJSON verifies that EmitSuiteResult
// with format=json writes parseable JSON to the writer.
func TestSH034_EmitSuiteResult_JSONFormat_ValidJSON(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, []ScenarioResult{
		sh034MinimalScenarioResult("smoke-1"),
	})

	var buf bytes.Buffer
	if err := EmitSuiteResult(&buf, SuiteResultOutputFormatJSON, sr); err != nil {
		t.Fatalf("EmitSuiteResult(json): %v", err)
	}

	var decoded SuiteResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal of EmitSuiteResult output: %v", err)
	}
	if decoded.SuiteVerdict != sr.SuiteVerdict {
		t.Errorf("decoded.SuiteVerdict = %q; want %q", decoded.SuiteVerdict, sr.SuiteVerdict)
	}
}

// TestSH034_EmitSuiteResult_JSONFormat_ContainsSuiteID verifies that the JSON
// output contains the suite_id field so downstream tools can correlate logs.
func TestSH034_EmitSuiteResult_JSONFormat_ContainsSuiteID(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, nil)

	var buf bytes.Buffer
	if err := EmitSuiteResult(&buf, SuiteResultOutputFormatJSON, sr); err != nil {
		t.Fatalf("EmitSuiteResult(json): %v", err)
	}

	if !strings.Contains(buf.String(), "suite_id") {
		t.Error("EmitSuiteResult(json): output does not contain suite_id field")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EmitSuiteResult — human format
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_EmitSuiteResult_HumanFormat_ContainsVerdict verifies that the
// human-format output contains the suite verdict for operator diagnosis.
func TestSH034_EmitSuiteResult_HumanFormat_ContainsVerdict(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, nil)

	var buf bytes.Buffer
	if err := EmitSuiteResult(&buf, SuiteResultOutputFormatHuman, sr); err != nil {
		t.Fatalf("EmitSuiteResult(human): %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, string(sr.SuiteVerdict)) {
		t.Errorf("EmitSuiteResult(human): output does not contain verdict %q\noutput: %s",
			sr.SuiteVerdict, out)
	}
}

// TestSH034_EmitSuiteResult_HumanFormat_ContainsScenarioNames verifies that
// per-scenario outcome lines appear in human-format output.
func TestSH034_EmitSuiteResult_HumanFormat_ContainsScenarioNames(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, []ScenarioResult{
		sh034MinimalScenarioResult("twin-launch-and-ready"),
		sh034MinimalScenarioResult("checkpoint-and-merge"),
	})

	var buf bytes.Buffer
	if err := EmitSuiteResult(&buf, SuiteResultOutputFormatHuman, sr); err != nil {
		t.Fatalf("EmitSuiteResult(human): %v", err)
	}

	out := buf.String()
	for _, name := range []string{"twin-launch-and-ready", "checkpoint-and-merge"} {
		if !strings.Contains(out, name) {
			t.Errorf("EmitSuiteResult(human): output does not contain scenario name %q\noutput: %s",
				name, out)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EmitSuiteResult — unrecognised format
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_EmitSuiteResult_UnrecognisedFormat_ReturnsError verifies that
// EmitSuiteResult returns an error for an unrecognised format string without
// writing anything to the writer.
func TestSH034_EmitSuiteResult_UnrecognisedFormat_ReturnsError(t *testing.T) {
	t.Parallel()

	fixtureRoot := sh034TempDir(t)
	sr := sh034MinimalSuiteResult(t, fixtureRoot, nil)

	var buf bytes.Buffer
	err := EmitSuiteResult(&buf, SuiteResultOutputFormat("xml"), sr)
	if err == nil {
		t.Error("EmitSuiteResult with unrecognised format: want error, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("EmitSuiteResult with unrecognised format: wrote %d bytes; want 0", buf.Len())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SuiteResultOutputFormat
// ─────────────────────────────────────────────────────────────────────────────

// TestSH034_SuiteResultOutputFormat_Valid verifies the two declared constants
// are valid and an arbitrary string is not.
func TestSH034_SuiteResultOutputFormat_Valid(t *testing.T) {
	t.Parallel()

	for _, f := range []SuiteResultOutputFormat{
		SuiteResultOutputFormatJSON,
		SuiteResultOutputFormatHuman,
	} {
		if !f.Valid() {
			t.Errorf("SuiteResultOutputFormat(%q).Valid() = false; want true", f)
		}
	}
	if SuiteResultOutputFormat("xml").Valid() {
		t.Error("SuiteResultOutputFormat(\"xml\").Valid() = true; want false")
	}
}
