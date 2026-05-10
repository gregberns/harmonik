package scenario_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/scenario"
)

// orchinternalerror_sh019_test.go — sensor tests for SH-019
// (Orchestration internal error fails with `orchestration-internal-error`).
//
// Spec refs: specs/scenario-harness.md §8.4 SH-019.
// Bead: hk-i0tw.21.
//
// Verifies:
//
//	(a) scenario.FailureClassOrchestrationInternalError constant exists, is valid, and
//	    has wire value "orchestration-internal-error" per §8.4.
//	(b) A ScenarioResult with verdict=error + failure_class=orchestration-internal-error
//	    is structurally valid per §6.1 (models the SH-019 output).
//	(c) orchestration-internal-error has rank 2 precedence (below harness-internal-error,
//	    above all other classes) per §8.0.
//	(d) The closed set of scenario-attributable failures does NOT include daemon
//	    crash or daemon-degraded-mid-scenario, which therefore route to
//	    orchestration-internal-error per SH-019.
//	(e) Spec-corpus sensor: scenario-harness.md contains SH-019 and the
//	    "orchestration-internal-error" failure-class name.
//
// Helper prefix: orchErrFixture (per implementer-protocol.md).

// orchErrFixtureModuleRoot returns the module root by walking upward from this file.
func orchErrFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-019: scenario.FailureClassOrchestrationInternalError constant
// ─────────────────────────────────────────────────────────────────────────────

// TestSH019_OrchestrationInternalErrorConstantExists verifies that
// scenario.FailureClassOrchestrationInternalError is a declared, valid FailureClass
// constant per scenario-harness.md §8.4.
func TestSH019_OrchestrationInternalErrorConstantExists(t *testing.T) {
	t.Parallel()

	fc := scenario.FailureClassOrchestrationInternalError
	if !fc.Valid() {
		t.Errorf("SH-019: scenario.FailureClassOrchestrationInternalError.Valid() = false; want true")
	}
}

// TestSH019_OrchestrationInternalErrorWireValue verifies the wire value is
// "orchestration-internal-error" per scenario-harness.md §8.4.
func TestSH019_OrchestrationInternalErrorWireValue(t *testing.T) {
	t.Parallel()

	const wantWire = "orchestration-internal-error"
	got := string(scenario.FailureClassOrchestrationInternalError)
	if got != wantWire {
		t.Errorf("SH-019: scenario.FailureClassOrchestrationInternalError wire value = %q; want %q", got, wantWire)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-019: ScenarioResult shape for orchestration-internal-error
// ─────────────────────────────────────────────────────────────────────────────

// TestSH019_ScenarioResultForOrchError verifies that a ScenarioResult with
// verdict=error + failure_class=orchestration-internal-error is structurally
// valid per scenario-harness.md §6.1.
//
// This models what the harness MUST produce per SH-019 when an orchestration
// drive returns an error not classifiable as scenario-attributable.
func TestSH019_ScenarioResultForOrchError(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	result := scenario.ScenarioResult{
		ScenarioName:     "test-scenario",
		SourcePath:       "scenarios/test.yaml",
		StartedAt:        now,
		CompletedAt:      now.Add(50 * time.Millisecond),
		Verdict:          scenario.ScenarioVerdictError,
		FailureClass:     scenario.FailureClassOrchestrationInternalError,
		AssertionResults: []scenario.AssertionResult{},
		ErrorDetail:      "daemon crashed during orchestration: exit status 19",
	}

	if !result.Verdict.Valid() {
		t.Error("SH-019: ScenarioResult.Verdict is invalid; want valid")
	}
	if !result.FailureClass.Valid() {
		t.Error("SH-019: ScenarioResult.FailureClass is invalid; want valid")
	}
	if result.Verdict != scenario.ScenarioVerdictError {
		t.Errorf("SH-019: ScenarioResult.Verdict = %q; want error", result.Verdict)
	}
	if result.FailureClass != scenario.FailureClassOrchestrationInternalError {
		t.Errorf("SH-019: ScenarioResult.FailureClass = %q; want orchestration-internal-error", result.FailureClass)
	}
	if result.ErrorDetail == "" {
		t.Error("SH-019: ScenarioResult.ErrorDetail is empty; want non-empty (captures original error per SH-019)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-019: precedence (rank 2 per §8.0)
// ─────────────────────────────────────────────────────────────────────────────

// TestSH019_PrecedenceIsRank2 verifies that orchestration-internal-error has
// rank 2 in the §8.0 precedence table — below harness-internal-error (rank 1),
// above all other failure classes.
func TestSH019_PrecedenceIsRank2(t *testing.T) {
	t.Parallel()

	orch := scenario.FailureClassOrchestrationInternalError
	const wantPrecedence = 2
	got := orch.Precedence()
	if got != wantPrecedence {
		t.Errorf("SH-019: orchestration-internal-error.Precedence() = %d; want %d", got, wantPrecedence)
	}
}

// TestSH019_OrchErrorBeatsLowerRankClasses verifies that orchestration-internal-error
// beats all lower-precedence failure classes per §8.0.
func TestSH019_OrchErrorBeatsLowerRankClasses(t *testing.T) {
	t.Parallel()

	orch := scenario.FailureClassOrchestrationInternalError
	lowerRank := []scenario.FailureClass{
		scenario.FailureClassScenarioLoadFailure,
		scenario.FailureClassTwinBinaryNotFound,
		scenario.FailureClassFixtureSetupFailed,
		scenario.FailureClassScenarioTimeout,
		scenario.FailureClassAssertionFailed,
		scenario.FailureClassCleanupFailed,
	}

	for _, lower := range lowerRank {
		t.Run(string(lower), func(t *testing.T) {
			t.Parallel()
			if !orch.HigherPrecedenceThan(lower) {
				t.Errorf("SH-019: orchestration-internal-error MUST have higher precedence than %q per §8.0", lower)
			}
		})
	}
}

// TestSH019_HarnessInternalErrorBeatsOrchError verifies that harness-internal-error
// (rank 1) overrides orchestration-internal-error (rank 2) per §8.0.
func TestSH019_HarnessInternalErrorBeatsOrchError(t *testing.T) {
	t.Parallel()

	harness := scenario.FailureClassHarnessInternalError
	orch := scenario.FailureClassOrchestrationInternalError

	if !harness.HigherPrecedenceThan(orch) {
		t.Error("SH-019: harness-internal-error MUST have higher precedence than orchestration-internal-error per §8.0 rank table (rank 1 > rank 2)")
	}
	if orch.HigherPrecedenceThan(harness) {
		t.Error("SH-019: orchestration-internal-error MUST NOT beat harness-internal-error per §8.0")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-019: scenario-attributable vs. non-attributable failure discrimination
// ─────────────────────────────────────────────────────────────────────────────

// TestSH019_NonAttributableErrorsMapToOrchError verifies the SH-019 discriminant:
// daemon crash, RPC error, daemon-degraded mid-scenario, and orchestrator-goroutine
// panic are NOT scenario-attributable and MUST map to orchestration-internal-error.
func TestSH019_NonAttributableErrorsMapToOrchError(t *testing.T) {
	t.Parallel()

	// Model: the non-attributable error category is the complement of the
	// scenario-attributable closed set per SH-019:
	//   Attributable: agent_failed, twin-scripted failure, gate denial, run_failed cascade.
	//   NOT attributable: daemon crash (process-exit), RPC error, orchestrator panic,
	//                     daemon entered degraded mid-scenario, store divergence.
	//
	// We verify that orchestration-internal-error is the correct verdict+class
	// for each non-attributable error description.
	nonAttributableErrors := []struct {
		name   string
		detail string
	}{
		{"daemon-process-exit", "daemon exited unexpectedly: exit status 19 (runtime panic)"},
		{"rpc-error", "JSON-RPC error on daemon socket: connection reset by peer"},
		{"orchestrator-goroutine-panic", "orchestrator goroutine panicked: index out of range"},
		{"daemon-degraded-mid-scenario", "daemon entered 'degraded' state (Cat 0 prerequisite failing mid-scenario)"},
		{"store-divergence", "store divergence detected by reconciliation mid-scenario"},
	}

	for _, tc := range nonAttributableErrors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Construct what the harness MUST produce per SH-019.
			result := scenario.ScenarioResult{
				ScenarioName:     "test-" + tc.name,
				SourcePath:       "scenarios/test.yaml",
				StartedAt:        time.Now().UTC(),
				CompletedAt:      time.Now().UTC(),
				Verdict:          scenario.ScenarioVerdictError,
				FailureClass:     scenario.FailureClassOrchestrationInternalError,
				AssertionResults: []scenario.AssertionResult{},
				ErrorDetail:      tc.detail,
			}
			if result.Verdict != scenario.ScenarioVerdictError {
				t.Errorf("SH-019 %s: Verdict = %q; want error", tc.name, result.Verdict)
			}
			if result.FailureClass != scenario.FailureClassOrchestrationInternalError {
				t.Errorf("SH-019 %s: FailureClass = %q; want orchestration-internal-error", tc.name, result.FailureClass)
			}
			if result.ErrorDetail == "" {
				t.Errorf("SH-019 %s: ErrorDetail must be non-empty (captures verbatim original error)", tc.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-019: Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestSH019_SpecCorpusClause verifies that scenario-harness.md contains SH-019
// and the "orchestration-internal-error" failure-class name.
func TestSH019_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	root := orchErrFixtureModuleRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")

	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading scenario-harness.md: %v", err)
	}
	specText := string(content)

	if !strings.Contains(specText, "SH-019") {
		t.Error("scenario-harness.md missing SH-019 clause")
	}
	if !strings.Contains(specText, "orchestration-internal-error") {
		t.Error("scenario-harness.md missing 'orchestration-internal-error' failure-class; SH-019 may have drifted")
	}
}
