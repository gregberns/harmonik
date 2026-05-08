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
