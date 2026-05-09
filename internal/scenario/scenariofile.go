package scenario

import (
	"regexp"

	"github.com/gregberns/harmonik/internal/core"
)

// scenarioNameRe is the regular expression every scenario name MUST match per
// SH-005: ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$
// Names that do not match MUST fail with scenario-load-failure at suite-load.
var scenarioNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// scenarioMatrixMaxCells is the maximum number of cartesian-product cells
// allowed in a scenario's matrix field per SH-030. A matrix that would expand
// beyond this count MUST fail at scenario-load time as scenario-load-failure.
const scenarioMatrixMaxCells = 1024

// ScenarioFile is the top-level record parsed from a scenario YAML file. It
// contains all 12 fields declared in the §6.1 RECORD ScenarioFile. Every
// field is normatively governed by specs/scenario-harness.md §6.1; field
// comments cite the controlling requirement.
//
// Exactly one of WorkflowPath / WorkflowID MUST be set (the other MUST be nil
// or the zero value). Declaring both or neither is a scenario-load-failure
// per SH-004 (schema check).
//
// WorkflowID is typed as core.WorkflowID per [execution-model.md §4.1]; it
// carries the stable UUID-backed workflow identifier. WorkflowPath is a
// repo-relative DOT file path within the synthetic project root; resolution
// order is declared in §6.1 (synthetic root first, then
// <repo-root>/scenarios/_workflows/).
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD ScenarioFile.
// Resolution rules + load-failure modes: SH-002, SH-003, SH-004, SH-005.
type ScenarioFile struct {
	// Name is the repo-wide unique scenario identifier per SH-005.
	// MUST match ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ and be unique across the
	// entire scenarios/ tree; names that do not match or collide MUST fail with
	// scenario-load-failure. Byte-lexicographic order of the name field drives
	// execution order per SH-007.
	Name string `json:"name" yaml:"name"`

	// Description is a one-line operator-facing description of the scenario.
	Description string `json:"description" yaml:"description"`

	// WorkflowPath is the DOT workflow file path (repo-relative within the
	// synthetic project root); MUTUALLY EXCLUSIVE with WorkflowID — exactly
	// one MUST be set. nil means this field is absent (None in the spec).
	// Resolution: harness attempts <synthetic-project-root>/<WorkflowPath>
	// first, then <repo-root>/scenarios/_workflows/<WorkflowPath>; resolution
	// failure is scenario-load-failure per §6.1.
	WorkflowPath *string `json:"workflow_path,omitempty" yaml:"workflow_path,omitempty"`

	// WorkflowID is the stable workflow identifier per [execution-model.md §4.1].
	// MUTUALLY EXCLUSIVE with WorkflowPath — exactly one MUST be set.
	// nil means this field is absent (None in the spec). Typed as *core.WorkflowID
	// so the pointer cleanly represents String|None without the zero-UUID ambiguity.
	WorkflowID *core.WorkflowID `json:"workflow_id,omitempty" yaml:"workflow_id,omitempty"`

	// AgentOverrides maps agent role names (as declared by the resolved workflow)
	// to twin-binary selectors. A key that references an undeclared role is a
	// scenario-load-failure per §6.1. nil or empty means no overrides.
	AgentOverrides map[string]AgentOverride `json:"agent_overrides,omitempty" yaml:"agent_overrides,omitempty"`

	// FixtureSetup holds workspace seed instructions applied before orchestration.
	// A zero-value FixtureSetup (all nil fields) is valid and means "no seeding."
	FixtureSetup FixtureSetup `json:"fixture_setup" yaml:"fixture_setup"`

	// ExpectedEvents is the list of event_present / event_absent assertions
	// evaluated against the captured JSONL event log per SH-020 and SH-021.
	// nil or empty means no event assertions are declared.
	ExpectedEvents []EventExpectation `json:"expected_events,omitempty" yaml:"expected_events,omitempty"`

	// ExpectedWorkspace is the list of file/git-ref workspace predicates
	// evaluated against the captured per-scenario worktree per SH-022.
	// nil or empty means no workspace assertions are declared.
	ExpectedWorkspace []WorkspacePredicate `json:"expected_workspace,omitempty" yaml:"expected_workspace,omitempty"`

	// ExpectedOutcome is the optional run-level terminal-outcome assertion
	// (outcome_status from the run_completed / run_failed event payload per
	// event-model.md §8.1.8). nil means no outcome assertion is declared
	// (None in the spec).
	ExpectedOutcome *OutcomeExpectation `json:"expected_outcome,omitempty" yaml:"expected_outcome,omitempty"`

	// TimeoutSecs is the per-scenario wall-clock budget in seconds per SH-025.
	// MUST be in the range [1, 7200]; values outside this range MUST fail at
	// scenario-load time as scenario-load-failure per SH-004. There is no
	// harness-default budget; every scenario MUST declare this field explicitly.
	TimeoutSecs int `json:"timeout_secs" yaml:"timeout_secs"`

	// CadenceTag is the cadence-filter membership tag per SH-029.
	// MUST be one of smoke, regression, or nightly. An unknown or empty value
	// MUST fail at scenario-load time per SH-004.
	CadenceTag CadenceTag `json:"cadence_tag" yaml:"cadence_tag"`

	// Matrix is the optional parameter expansion map per SH-030.
	// Keys are parameter names; values are the list of candidate values for
	// that parameter. The harness expands the scenario into one execution per
	// cell of the cartesian product, capped at 1024 cells (scenarioMatrixMaxCells).
	// A matrix that exceeds the cell cap MUST fail at scenario-load time as
	// scenario-load-failure. nil means no matrix expansion is declared (None in
	// the spec).
	Matrix map[string][]string `json:"matrix,omitempty" yaml:"matrix,omitempty"`
}

// Valid reports whether the ScenarioFile is structurally well-formed per §6.1:
//
//   - Name is non-empty and matches ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ (SH-005).
//   - Exactly one of WorkflowPath / WorkflowID is set (mutual exclusivity per §6.1).
//   - Every AgentOverride value satisfies AgentOverride.Valid().
//   - FixtureSetup satisfies FixtureSetup.Valid().
//   - Every EventExpectation satisfies EventExpectation.Valid().
//   - Every WorkspacePredicate satisfies WorkspacePredicate.Valid().
//   - ExpectedOutcome, if non-nil, satisfies OutcomeExpectation.Valid().
//   - TimeoutSecs is in [1, 7200] (SH-025).
//   - CadenceTag is a declared constant (SH-029).
//   - Matrix, if non-nil, expands to at most 1024 cells (SH-030).
//
// Repository-wide name uniqueness (SH-005), workflow resolution (SH-002,
// SH-003), and agent-role verification against the resolved workflow (§6.1)
// are caller responsibilities at suite-load time and are NOT checked here.
func (s ScenarioFile) Valid() bool {
	// Name must be non-empty and match the SH-005 regex.
	if !scenarioNameRe.MatchString(s.Name) {
		return false
	}

	// Exactly one of WorkflowPath / WorkflowID must be set.
	workflowPathSet := s.WorkflowPath != nil && *s.WorkflowPath != ""
	workflowIDSet := s.WorkflowID != nil
	if workflowPathSet == workflowIDSet {
		// Both set or neither set — mutual exclusivity violated.
		return false
	}

	// Each AgentOverride value must be valid.
	for _, ao := range s.AgentOverrides {
		if !ao.Valid() {
			return false
		}
	}

	// FixtureSetup must be valid.
	if !s.FixtureSetup.Valid() {
		return false
	}

	// Each EventExpectation must be valid.
	for _, ee := range s.ExpectedEvents {
		if !ee.Valid() {
			return false
		}
	}

	// Each WorkspacePredicate must be valid.
	for _, wp := range s.ExpectedWorkspace {
		if !wp.Valid() {
			return false
		}
	}

	// ExpectedOutcome, if present, must be valid.
	if s.ExpectedOutcome != nil && !s.ExpectedOutcome.Valid() {
		return false
	}

	// TimeoutSecs must be in [1, 7200] per SH-025.
	if s.TimeoutSecs < 1 || s.TimeoutSecs > 7200 {
		return false
	}

	// CadenceTag must be a declared constant per SH-029.
	if !s.CadenceTag.Valid() {
		return false
	}

	// Matrix cell count must not exceed 1024 per SH-030.
	if s.Matrix != nil {
		cells := matrixCellCount(s.Matrix)
		if cells > scenarioMatrixMaxCells {
			return false
		}
	}

	return true
}

// matrixCellCount returns the number of cartesian-product cells for the given
// matrix map. An empty map or a map with any zero-length value list has 0 cells
// (no expansion is possible). Otherwise the cell count is the product of the
// lengths of all value lists.
func matrixCellCount(matrix map[string][]string) int {
	if len(matrix) == 0 {
		return 0
	}
	cells := 1
	for _, vals := range matrix {
		if len(vals) == 0 {
			return 0
		}
		cells *= len(vals)
	}
	return cells
}
