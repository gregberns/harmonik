package scenario

import "fmt"

// FailureClass is the scenario-harness failure-class taxonomy defined in
// specs/scenario-harness.md §8.  The eight classes cover every failure
// transition in the scenario lifecycle (§7.1 lifecycle pseudocode).
//
// This type is DISTINCT from internal/core/FailureClass (the execution-model
// §8 enum with six values).  The two enums share a name but are scoped to
// different specs and packages; they MUST NOT be conflated.
//
// The enum is closed: unknown values are never tolerated.
type FailureClass string

// Failure-class constants per specs/scenario-harness.md §8.
const (
	// FailureClassScenarioLoadFailure is emitted when the scenario file fails
	// YAML parse, §6.1 schema validation, workflow-ref lookup, cadence-tag
	// lookup, parameter expansion, or name uniqueness check (§8.1).
	FailureClassScenarioLoadFailure FailureClass = "scenario-load-failure"

	// FailureClassTwinBinaryNotFound is emitted when agent_overrides references
	// a twin binary that does not exist on disk or cannot be resolved against
	// the configured twin-binary search-path prefix (§8.2).
	FailureClassTwinBinaryNotFound FailureClass = "twin-binary-not-found"

	// FailureClassFixtureSetupFailed is emitted when the fixture-setup phase
	// (SH-012) returns an error: workspace creation, git seed ops, file writes,
	// or fixture-root unreachable (§8.3).
	FailureClassFixtureSetupFailed FailureClass = "fixture-setup-failed"

	// FailureClassOrchestrationInternalError is emitted when the orchestration
	// drive returns an error from daemon startup, dispatch, or shutdown that is
	// not classifiable as a scenario-attributable failure (§8.4).
	FailureClassOrchestrationInternalError FailureClass = "orchestration-internal-error"

	// FailureClassHarnessInternalError is emitted for the closed list of
	// harness-local defects: event-log unreadable, real-model handler attempted
	// launch, non-loopback outbound call, harness panic, operator interrupt, or
	// failure to start the per-scenario daemon (§8.7).
	FailureClassHarnessInternalError FailureClass = "harness-internal-error"

	// FailureClassAssertionFailed is emitted when one or more declared
	// assertions evaluate to passed=false per SH-023 (§8.5).
	FailureClassAssertionFailed FailureClass = "assertion-failed"

	// FailureClassScenarioTimeout is emitted when the orchestration drive does
	// not reach a terminal state within timeout_secs per SH-026 (§8.6).
	FailureClassScenarioTimeout FailureClass = "scenario-timeout"

	// FailureClassCleanupFailed is emitted when fixture teardown (SH-015)
	// fails.  Per §8.0 precedence it never overwrites a prior verdict; it is
	// appended to error_detail (§8.8).
	FailureClassCleanupFailed FailureClass = "cleanup-failed"
)

// Valid reports whether f is one of the eight declared FailureClass constants.
// The failure-class taxonomy is closed; unknown values are never valid.
func (f FailureClass) Valid() bool {
	switch f {
	case FailureClassScenarioLoadFailure,
		FailureClassTwinBinaryNotFound,
		FailureClassFixtureSetupFailed,
		FailureClassOrchestrationInternalError,
		FailureClassHarnessInternalError,
		FailureClassAssertionFailed,
		FailureClassScenarioTimeout,
		FailureClassCleanupFailed:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so FailureClass serialises
// correctly in JSON and YAML.
func (f FailureClass) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("failureclass: unknown value %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the eight declared constants.
func (f *FailureClass) UnmarshalText(text []byte) error {
	v := FailureClass(text)
	if !v.Valid() {
		return fmt.Errorf(
			"failureclass: unknown value %q; must be one of scenario-load-failure, twin-binary-not-found, fixture-setup-failed, orchestration-internal-error, harness-internal-error, assertion-failed, scenario-timeout, cleanup-failed",
			string(text),
		)
	}
	*f = v
	return nil
}

// Precedence returns the ordinal precedence rank of f per the §8.0 table
// (specs/scenario-harness.md §8.0).  Lower numeric values indicate HIGHER
// precedence (rank 1 wins over rank 8).  The zero value (empty string) is not
// a valid FailureClass; callers SHOULD call Valid() before Precedence.
//
// Precedence table (highest first):
//
//	1 — harness-internal-error
//	2 — orchestration-internal-error
//	3 — scenario-load-failure
//	4 — twin-binary-not-found
//	5 — fixture-setup-failed
//	6 — scenario-timeout
//	7 — assertion-failed
//	8 — cleanup-failed
//
// Returns 0 for an unknown (invalid) FailureClass.
func (f FailureClass) Precedence() int {
	switch f {
	case FailureClassHarnessInternalError:
		return 1
	case FailureClassOrchestrationInternalError:
		return 2
	case FailureClassScenarioLoadFailure:
		return 3
	case FailureClassTwinBinaryNotFound:
		return 4
	case FailureClassFixtureSetupFailed:
		return 5
	case FailureClassScenarioTimeout:
		return 6
	case FailureClassAssertionFailed:
		return 7
	case FailureClassCleanupFailed:
		return 8
	default:
		return 0
	}
}

// HigherPrecedenceThan reports whether f has strictly higher precedence than
// other per the §8.0 table.  When two failure classes co-occur on a single
// scenario, the higher-precedence class determines the recorded failure_class;
// the lower-precedence class is appended to error_detail only.
//
// Returns false if either f or other is invalid (zero-value or unknown), and
// returns false when f == other (equal precedence, not strictly higher).
func (f FailureClass) HigherPrecedenceThan(other FailureClass) bool {
	fp := f.Precedence()
	op := other.Precedence()
	if fp == 0 || op == 0 {
		return false
	}
	// Lower numeric rank = higher precedence.
	return fp < op
}
