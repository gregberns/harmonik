package scenario

// resultemit.go — SH-034 ScenarioResult/SuiteResult durability and emission.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SuiteResultOutputFormat controls the stdout format for SuiteResult emission
// per specs/scenario-harness.md §4.12 SH-032 (--output flag).
type SuiteResultOutputFormat string

// Declared SuiteResultOutputFormat constants per SH-032.
const (
	// SuiteResultOutputFormatJSON emits SuiteResult as indented JSON.
	SuiteResultOutputFormatJSON SuiteResultOutputFormat = "json"
	// SuiteResultOutputFormatHuman emits a human-readable summary.
	SuiteResultOutputFormatHuman SuiteResultOutputFormat = "human"
)

// Valid reports whether f is one of the two declared constants.
func (f SuiteResultOutputFormat) Valid() bool {
	switch f {
	case SuiteResultOutputFormatJSON, SuiteResultOutputFormatHuman:
		return true
	default:
		return false
	}
}

// ScenarioResultPath returns the absolute path at which WriteScenarioResult
// writes a scenario's result record per SH-034:
//
//	<fixture-root>/<scenario-name>/result.json
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
func ScenarioResultPath(fixtureRoot, scenarioName string) string {
	return filepath.Join(fixtureRoot, scenarioName, "result.json")
}

// SuiteResultPath returns the absolute path at which WriteSuiteResult writes
// the aggregate suite result record per SH-034:
//
//	<fixture-root>/suite-result.json
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
func SuiteResultPath(fixtureRoot string) string {
	return filepath.Join(fixtureRoot, "suite-result.json")
}

// WriteScenarioResult writes r to <fixture-root>/<scenario-name>/result.json
// per SH-034. The parent directory is created if absent (the fixture setup
// phase SH-012 typically creates it, but WriteScenarioResult is idempotent
// with respect to directory creation so callers need not check).
//
// The file is written as indented JSON (UTF-8). On error the file may be
// partially written; the caller MUST NOT treat a write error as a no-op.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
func WriteScenarioResult(fixtureRoot string, r ScenarioResult) error {
	p := ScenarioResultPath(fixtureRoot, r.ScenarioName)
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("write scenario result: mkdir %q: %w", filepath.Dir(p), err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("write scenario result: marshal %q: %w", r.ScenarioName, err)
	}
	data = append(data, '\n')
	//nolint:gosec // G306: 0644 is correct for result files
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write scenario result: write %q: %w", p, err)
	}
	return nil
}

// WriteSuiteResult writes sr to <fixture-root>/suite-result.json per SH-034.
// The fixture root directory must already exist (created by NewFixtureRoot at
// suite start).
//
// The file is written as indented JSON (UTF-8). On error the file may be
// partially written; the caller MUST NOT treat a write error as a no-op.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-034.
func WriteSuiteResult(fixtureRoot string, sr SuiteResult) error {
	p := SuiteResultPath(fixtureRoot)
	data, err := json.MarshalIndent(sr, "", "  ")
	if err != nil {
		return fmt.Errorf("write suite result: marshal: %w", err)
	}
	data = append(data, '\n')
	//nolint:gosec // G306: 0644 is correct for result files
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write suite result: write %q: %w", p, err)
	}
	return nil
}

// EmitSuiteResult writes sr to w in the chosen format per SH-032/SH-034.
// Harness-internal log messages are written to stderr by the caller; this
// function writes ONLY the SuiteResult payload to w.
//
// format must be SuiteResultOutputFormatJSON or SuiteResultOutputFormatHuman.
// An unrecognised format returns an error without writing to w.
//
// Spec ref: specs/scenario-harness.md §4.12 SH-032, §4.13 SH-034.
func EmitSuiteResult(w io.Writer, format SuiteResultOutputFormat, sr SuiteResult) error {
	switch format {
	case SuiteResultOutputFormatJSON:
		data, err := json.MarshalIndent(sr, "", "  ")
		if err != nil {
			return fmt.Errorf("emit suite result (json): marshal: %w", err)
		}
		data = append(data, '\n')
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("emit suite result (json): write: %w", err)
		}
		return nil

	case SuiteResultOutputFormatHuman:
		return emitSuiteResultHuman(w, sr)

	default:
		return fmt.Errorf("emit suite result: unrecognised format %q; must be json or human", string(format))
	}
}

// emitSuiteResultHuman writes a human-readable summary of sr to w.
func emitSuiteResultHuman(w io.Writer, sr SuiteResult) error {
	passed, failed := 0, 0
	for _, r := range sr.Results {
		if r.Verdict == ScenarioVerdictPass {
			passed++
		} else {
			failed++
		}
	}

	lines := []string{
		fmt.Sprintf("suite_id:      %s", sr.SuiteID),
		fmt.Sprintf("suite_verdict: %s", sr.SuiteVerdict),
		fmt.Sprintf("fixture_root:  %s", sr.FixtureRoot),
		fmt.Sprintf("cadence:       %s", sr.CadenceFilter),
		fmt.Sprintf("scenarios:     %d total, %d passed, %d failed",
			len(sr.Results), passed, failed),
		fmt.Sprintf("started_at:    %s", sr.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z")),
		fmt.Sprintf("completed_at:  %s", sr.CompletedAt.UTC().Format("2006-01-02T15:04:05.000Z")),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("emit suite result (human): %w", err)
		}
	}
	if len(sr.Results) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return fmt.Errorf("emit suite result (human): %w", err)
		}
		for _, r := range sr.Results {
			indicator := "PASS"
			if r.Verdict != ScenarioVerdictPass {
				indicator = "FAIL"
			}
			line := fmt.Sprintf("  [%s] %s", indicator, r.ScenarioName)
			if r.FailureClass != "" {
				line += fmt.Sprintf(" (%s)", r.FailureClass)
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return fmt.Errorf("emit suite result (human): %w", err)
			}
		}
	}
	return nil
}
