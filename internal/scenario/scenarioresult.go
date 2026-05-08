package scenario

import (
	"fmt"
	"time"
)

// ScenarioVerdict is the top-level outcome of a single scenario execution.
// Spec ref: specs/scenario-harness.md §6.1 RECORD ScenarioResult — field verdict.
type ScenarioVerdict string

// Declared ScenarioVerdict constants per specs/scenario-harness.md §6.1.
const (
	// ScenarioVerdictPass indicates the scenario completed and all assertions passed.
	ScenarioVerdictPass ScenarioVerdict = "pass"

	// ScenarioVerdictFail indicates the scenario completed but one or more assertions failed.
	ScenarioVerdictFail ScenarioVerdict = "fail"

	// ScenarioVerdictTimeout indicates the scenario was cancelled because it exceeded
	// the declared timeout window before reaching teardown.
	ScenarioVerdictTimeout ScenarioVerdict = "timeout"

	// ScenarioVerdictError indicates an internal harness error prevented normal execution
	// or evaluation. ErrorDetail carries the operator-readable explanation.
	ScenarioVerdictError ScenarioVerdict = "error"
)

// Valid reports whether v is one of the four declared ScenarioVerdict constants.
func (v ScenarioVerdict) Valid() bool {
	switch v {
	case ScenarioVerdictPass,
		ScenarioVerdictFail,
		ScenarioVerdictTimeout,
		ScenarioVerdictError:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ScenarioVerdict serialises
// correctly in JSON and YAML.
func (v ScenarioVerdict) MarshalText() ([]byte, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("scenarioverdict: unknown value %q", string(v))
	}
	return []byte(v), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not in the declared set.
func (v *ScenarioVerdict) UnmarshalText(text []byte) error {
	candidate := ScenarioVerdict(text)
	if !candidate.Valid() {
		return fmt.Errorf("scenarioverdict: unknown value %q; must be one of pass, fail, timeout, error", string(text))
	}
	*v = candidate
	return nil
}

// ScenarioResult is the complete record of a single scenario execution as
// produced by the harness after teardown. One ScenarioResult is written per
// scenario run (or per expanded matrix row per SH-030).
//
// Spec ref: specs/scenario-harness.md §6.1 RECORD ScenarioResult.
type ScenarioResult struct {
	// ScenarioName is the scenario identifier: the name field from the originating
	// ScenarioFile, or the expanded matrix row name per SH-030.
	ScenarioName string `json:"scenario_name" yaml:"scenario_name"`

	// SourcePath is the repo-relative path to the originating scenario file.
	SourcePath string `json:"source_path" yaml:"source_path"`

	// StartedAt is the RFC 3339 UTC wall-clock timestamp at fixture-setup start
	// (millisecond precision).
	StartedAt time.Time `json:"started_at" yaml:"started_at"`

	// CompletedAt is the RFC 3339 UTC wall-clock timestamp at teardown completion
	// (millisecond precision).
	CompletedAt time.Time `json:"completed_at" yaml:"completed_at"`

	// Verdict is the top-level outcome of the scenario execution.
	Verdict ScenarioVerdict `json:"verdict" yaml:"verdict"`

	// FailureClass is the harness-level failure classification per §8 ENUM
	// FailureClass: one of {scenario-load-failure, twin-binary-not-found,
	// fixture-setup-failed, orchestration-internal-error, harness-internal-error,
	// assertion-failed, scenario-timeout, cleanup-failed}. This field is
	// currently `string` pending the typed `FailureClass` enum (TODO: hk-ido0
	// — see follow-up bead). When that lands, this field should hoist to the
	// typed enum (non-breaking: string-constant assignment is assignable to
	// typed-string). NOTE: this is the SCENARIO-HARNESS FailureClass enum, NOT
	// internal/core/FailureClass (the execution-model §8 enum) — the two enums
	// share a name but are scoped to different specs. Empty string represents
	// `None` (absent — required iff verdict=pass).
	FailureClass string `json:"failure_class,omitempty" yaml:"failure_class,omitempty"`

	// AssertionResults holds one entry per declared assertion. It is empty only
	// if the scenario terminated before §7.1 step 4 entered (i.e., no assertions
	// were evaluated at all).
	AssertionResults []AssertionResult `json:"assertion_results" yaml:"assertion_results"`

	// EventLogPath is the fixture-root-relative path to the captured JSONL event
	// log for this scenario run.
	EventLogPath string `json:"event_log_path" yaml:"event_log_path"`

	// WorkspaceSnapshotPath is the fixture-root-relative path to the
	// post-teardown worktree snapshot per SH-015a.
	WorkspaceSnapshotPath string `json:"workspace_snapshot_path" yaml:"workspace_snapshot_path"`

	// StdoutLogPaths maps agent role name to the fixture-root-relative path of
	// the captured stdout for that role per SH-020. nil and non-nil are both valid.
	StdoutLogPaths map[string]string `json:"stdout_log_paths" yaml:"stdout_log_paths"`

	// StderrLogPaths maps agent role name to the fixture-root-relative path of
	// the captured stderr for that role per SH-020. nil and non-nil are both valid.
	StderrLogPaths map[string]string `json:"stderr_log_paths" yaml:"stderr_log_paths"`

	// ErrorDetail is an operator-readable string present when Verdict=error or
	// when a cleanup-failed note was appended. Empty string represents None (absent).
	// Producers MUST set this to a non-empty string when Verdict=error; enforcement
	// is a producer obligation, not a structural validation rule at this layer.
	ErrorDetail string `json:"error_detail,omitempty" yaml:"error_detail,omitempty"`
}

// Valid reports whether the ScenarioResult is structurally well-formed per
// specs/scenario-harness.md §6.1 RECORD ScenarioResult:
//
//   - ScenarioName is non-empty.
//   - SourcePath is non-empty.
//   - StartedAt is non-zero.
//   - CompletedAt is non-zero and not before StartedAt.
//   - Verdict is one of the four declared ScenarioVerdict constants.
//   - Pass-iff-no-failure-class invariant (§6.1): FailureClass MUST be empty
//     iff Verdict=pass; FailureClass MUST be non-empty for any non-pass verdict.
//   - EventLogPath is non-empty.
//   - WorkspaceSnapshotPath is non-empty.
//   - Every element of AssertionResults satisfies AssertionResult.Valid().
//   - StdoutLogPaths and StderrLogPaths may be nil or non-nil; both are valid.
//   - ErrorDetail is unconstrained at the structural layer (producer obligation).
func (r ScenarioResult) Valid() bool {
	if r.ScenarioName == "" {
		return false
	}
	if r.SourcePath == "" {
		return false
	}
	if r.StartedAt.IsZero() {
		return false
	}
	if r.CompletedAt.IsZero() {
		return false
	}
	if r.CompletedAt.Before(r.StartedAt) {
		return false
	}
	if !r.Verdict.Valid() {
		return false
	}
	// Pass-iff-no-failure-class invariant per §6.1 "absent iff verdict=pass".
	if r.Verdict == ScenarioVerdictPass {
		if r.FailureClass != "" {
			return false
		}
	} else {
		if r.FailureClass == "" {
			return false
		}
	}
	if r.EventLogPath == "" {
		return false
	}
	if r.WorkspaceSnapshotPath == "" {
		return false
	}
	for _, ar := range r.AssertionResults {
		if !ar.Valid() {
			return false
		}
	}
	return true
}
