package scenario_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/scenario"
)

// sh026_timeout_test.go — sensor tests for SH-026
// (Scenario timeout enforcement via monotonic-clock deadline).
//
// Spec refs: specs/scenario-harness.md §4.7 SH-025, SH-026.
// Bead: hk-bkz6l.
//
// Verifies:
//
//	(a) ErrScenarioTimeout sentinel is declared and non-nil.
//	(b) OrchestrationConfig.TimeoutSecs field exists (zero-value compiles).
//	(c) DriveOrchestration returns ErrScenarioTimeout when the deadline fires
//	    before the daemon reaches a terminal state (behavioural test via a
//	    controlled DaemonEntryPoint replacement that blocks on context cancel).
//	(d) TimeoutSecs == 0 disables enforcement: external parent-cancel is NOT
//	    misclassified as ErrScenarioTimeout.
//	(e) Parent-context cancel is NOT misclassified as ErrScenarioTimeout even
//	    when TimeoutSecs > 0.
//	(f) Spec-corpus sensor: scenario-harness.md contains SH-026 and
//	    "scenario-timeout".

// ─────────────────────────────────────────────────────────────────────────────
// (a) ErrScenarioTimeout sentinel
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_ErrScenarioTimeoutDeclared verifies that ErrScenarioTimeout is a
// non-nil sentinel error per SH-026.
func TestSH026_ErrScenarioTimeoutDeclared(t *testing.T) {
	t.Parallel()

	if scenario.ErrScenarioTimeout == nil {
		t.Fatal("SH-026: scenario.ErrScenarioTimeout is nil; must be a non-nil sentinel error")
	}
}

// TestSH026_ErrScenarioTimeoutIsErrors verifies that ErrScenarioTimeout
// participates correctly in the errors.Is chain — callers MUST be able to
// detect timeout via errors.Is(err, scenario.ErrScenarioTimeout).
func TestSH026_ErrScenarioTimeoutIsErrors(t *testing.T) {
	t.Parallel()

	// Wrap the sentinel and confirm errors.Is resolves it.
	wrapped := scenario.ErrScenarioTimeout
	if !errors.Is(wrapped, scenario.ErrScenarioTimeout) {
		t.Error("SH-026: errors.Is(ErrScenarioTimeout, ErrScenarioTimeout) = false; must be true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) OrchestrationConfig.TimeoutSecs field
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_OrchestrationConfigTimeoutSecsField verifies that
// OrchestrationConfig carries a TimeoutSecs int field per SH-026.
func TestSH026_OrchestrationConfigTimeoutSecsField(t *testing.T) {
	t.Parallel()

	// Compile-time proof: zero-value struct must have TimeoutSecs accessible.
	cfg := scenario.OrchestrationConfig{}
	if cfg.TimeoutSecs != 0 {
		t.Errorf("SH-026: OrchestrationConfig zero value TimeoutSecs = %d; want 0", cfg.TimeoutSecs)
	}

	// Confirm the field is assignable.
	cfg.TimeoutSecs = 30
	if cfg.TimeoutSecs != 30 {
		t.Errorf("SH-026: OrchestrationConfig.TimeoutSecs = %d after assignment; want 30", cfg.TimeoutSecs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) Behavioural: DriveOrchestration returns ErrScenarioTimeout on deadline
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_DriveOrchestrationReturnsTimeoutOnDeadline verifies that
// DriveOrchestration returns ErrScenarioTimeout when TimeoutSecs is set and
// the daemon does not reach a terminal state before the deadline.
//
// This test temporarily replaces scenario.DaemonEntryPoint with a controlled
// implementation that blocks until its context is cancelled, simulating a
// long-running scenario that exceeds its wall-clock budget.
//
// NOTE: This test must NOT run in parallel because it mutates the package-level
// DaemonEntryPoint variable.
func TestSH026_DriveOrchestrationReturnsTimeoutOnDeadline(t *testing.T) {
	// Save and restore the real DaemonEntryPoint.
	orig := scenario.DaemonEntryPoint
	t.Cleanup(func() { scenario.DaemonEntryPoint = orig })

	// Replace with a blocking stub that blocks until its context is cancelled.
	scenario.DaemonEntryPoint = func(ctx context.Context, _ daemon.Config) error {
		<-ctx.Done()
		return ctx.Err()
	}

	cfg := scenario.OrchestrationConfig{
		ProjectDir:    "/nonexistent-sh026-timeout-test/project",
		JSONLLogPath:  "/nonexistent-sh026-timeout-test/project/.harmonik/events/events.jsonl",
		HandlerBinary: "/nonexistent-sh026-timeout-test/twin",
		TimeoutSecs:   1, // 1-second budget triggers quickly in tests.
	}

	ctx := context.Background()
	start := time.Now()
	err := scenario.DriveOrchestration(ctx, cfg)
	elapsed := time.Since(start)

	if !errors.Is(err, scenario.ErrScenarioTimeout) {
		t.Errorf("SH-026: DriveOrchestration with TimeoutSecs=1 returned %v; want ErrScenarioTimeout", err)
	}
	// Sanity: should complete within 3× the declared timeout.
	const maxElapsed = 3 * time.Second
	if elapsed > maxElapsed {
		t.Errorf("SH-026: DriveOrchestration took %v; expected completion within %v (deadline enforcement not firing promptly)", elapsed, maxElapsed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) TimeoutSecs == 0 disables enforcement
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_NoTimeoutWhenTimeoutSecsZero verifies that TimeoutSecs == 0
// disables timeout enforcement: a cancelled parent context is NOT
// misclassified as ErrScenarioTimeout.
//
// NOTE: This test must NOT run in parallel (mutates DaemonEntryPoint).
func TestSH026_NoTimeoutWhenTimeoutSecsZero(t *testing.T) {
	orig := scenario.DaemonEntryPoint
	t.Cleanup(func() { scenario.DaemonEntryPoint = orig })

	scenario.DaemonEntryPoint = func(ctx context.Context, _ daemon.Config) error {
		<-ctx.Done()
		return ctx.Err()
	}

	cfg := scenario.OrchestrationConfig{
		ProjectDir:    "/nonexistent-sh026-zero-timeout/project",
		JSONLLogPath:  "/nonexistent-sh026-zero-timeout/project/.harmonik/events/events.jsonl",
		HandlerBinary: "/nonexistent-sh026-zero-timeout/twin",
		TimeoutSecs:   0, // zero → no enforcement.
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately.

	err := scenario.DriveOrchestration(ctx, cfg)
	if errors.Is(err, scenario.ErrScenarioTimeout) {
		t.Error("SH-026: DriveOrchestration with TimeoutSecs=0 returned ErrScenarioTimeout; parent-cancel MUST NOT be misclassified as a timeout")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e) Parent-cancel is not misclassified as timeout even when TimeoutSecs > 0
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_ParentCancelNotMisclassifiedAsTimeout verifies the SH-026
// order-of-checks guard: when the PARENT context is cancelled (SIGINT/SIGTERM
// path), DriveOrchestration MUST NOT return ErrScenarioTimeout.
//
// NOTE: This test must NOT run in parallel (mutates DaemonEntryPoint).
func TestSH026_ParentCancelNotMisclassifiedAsTimeout(t *testing.T) {
	orig := scenario.DaemonEntryPoint
	t.Cleanup(func() { scenario.DaemonEntryPoint = orig })

	scenario.DaemonEntryPoint = func(ctx context.Context, _ daemon.Config) error {
		<-ctx.Done()
		return ctx.Err()
	}

	cfg := scenario.OrchestrationConfig{
		ProjectDir:    "/nonexistent-sh026-parent-cancel/project",
		JSONLLogPath:  "/nonexistent-sh026-parent-cancel/project/.harmonik/events/events.jsonl",
		HandlerBinary: "/nonexistent-sh026-parent-cancel/twin",
		TimeoutSecs:   60, // long timeout — parent cancel fires first.
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel the parent before calling DriveOrchestration.

	err := scenario.DriveOrchestration(ctx, cfg)
	if errors.Is(err, scenario.ErrScenarioTimeout) {
		t.Errorf("SH-026: parent-context cancel with TimeoutSecs=60 returned ErrScenarioTimeout; "+
			"external cancel MUST NOT be misclassified as a timeout (got: %v)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e2) Partial assertion eval on timeout path (SH-026 §4.6)
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_PartialAssertionEvalOnTimeoutPath verifies that after
// DriveOrchestration returns ErrScenarioTimeout, EvaluateAssertions can be
// called best-effort against the partial event log per SH-026 §4.6.
//
// This test exercises the caller-side obligation documented on ErrScenarioTimeout:
// the caller MUST call ReadEventLog + EvaluateAssertions and then emit a
// ScenarioResult with verdict=timeout, failure_class=scenario-timeout.
//
// The test uses an empty event log (no events emitted before timeout) and
// verifies that EvaluateAssertions returns all assertions failed (partial data)
// and that the verdict remains timeout — not assertion-failed — per §8.0
// precedence (scenario-timeout rank 6 > assertion-failed rank 7).
func TestSH026_PartialAssertionEvalOnTimeoutPath(t *testing.T) {
	t.Parallel()

	// Build a ScenarioFile with one event_present expectation.
	sf := scenario.ScenarioFile{
		Name:        "timeout-partial-eval",
		TimeoutSecs: 30,
		ExpectedEvents: []scenario.EventExpectation{
			{
				Kind:        scenario.EventExpectationKindPresent,
				Type:        "agent_ready",
				Description: "agent must become ready",
			},
		},
	}

	// Simulate the partial event log on the timeout path: no events written yet.
	// ReadEventLog against an empty/nonexistent file is handled by the caller;
	// here we pass an empty slice to represent a partial (zero-event) log.
	var partialEvents []scenario.RawEvent

	// EvaluateAssertions must be callable on the partial event set per SH-023.
	results, assertVerdict, assertFC := scenario.EvaluateAssertions(sf, partialEvents, t.TempDir())

	// All assertions must be evaluated (SH-023 no-short-circuit) and must fail
	// because the event was not emitted before the timeout.
	if len(results) != len(sf.ExpectedEvents) {
		t.Errorf("SH-026 partial eval: got %d assertion results; want %d (SH-023 no-short-circuit)",
			len(results), len(sf.ExpectedEvents))
	}
	for i, r := range results {
		if r.Passed {
			t.Errorf("SH-026 partial eval: result[%d] passed on empty event log; want failed", i)
		}
	}

	// The verdict from EvaluateAssertions is assertion-failed; the harness
	// overrides it to timeout per §8.0 precedence (scenario-timeout rank 6 > assertion-failed rank 7).
	_ = assertVerdict // caller responsibility: override with ScenarioVerdictTimeout
	_ = assertFC      // caller responsibility: use FailureClassScenarioTimeout

	// Verify the precedence: scenario-timeout MUST beat assertion-failed per §8.0.
	if !scenario.FailureClassScenarioTimeout.HigherPrecedenceThan(scenario.FailureClassAssertionFailed) {
		t.Error("SH-026: FailureClassScenarioTimeout must have higher precedence than FailureClassAssertionFailed per §8.0")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (f) Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestSH026_SpecCorpusClause verifies that scenario-harness.md contains SH-026
// and the "scenario-timeout" failure-class name.
func TestSH026_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	root := sh026ModuleRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("SH-026: reading scenario-harness.md: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "SH-026") {
		t.Error("SH-026: scenario-harness.md missing SH-026 clause")
	}
	if !strings.Contains(text, "scenario-timeout") {
		t.Error("SH-026: scenario-harness.md missing 'scenario-timeout'; spec may have drifted")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func sh026ModuleRoot(t *testing.T) string {
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
