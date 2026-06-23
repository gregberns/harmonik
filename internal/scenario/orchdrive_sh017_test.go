package scenario_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/scenario"
)

// orchdrive_sh017_test.go — sensor tests for SH-017
// (Harness uses the production orchestrator entry-point).
//
// Spec refs: specs/scenario-harness.md §4.5 SH-017, §10.2 (test obligation).
// Bead: hk-jjn9y.
//
// Verifies:
//
//	(a) scenario.DaemonEntryPoint is function-identity-equal to daemon.Start
//	    per the §10.2 cross-package composition-root check.
//	(b) The function name (via runtime.FuncForPC) has suffix "daemon.Start".
//	(c) DriveOrchestration returns a non-nil error when the daemon cannot start
//	    (invalid ProjectDir), modelling the SH-019 orchestration-internal-error
//	    classification path.
//	(d) OrchestrationConfig zero-value WorkflowMode defaults to review-loop
//	    without a PL-004a fail-closed error.
//	(e) Spec-corpus sensor: scenario-harness.md contains SH-017 and the phrase
//	    "production orchestrator entry-point".

// ─────────────────────────────────────────────────────────────────────────────
// (a)+(b) Function identity: DaemonEntryPoint == daemon.Start
// ─────────────────────────────────────────────────────────────────────────────

// TestSH017_DaemonEntryPointIdentity verifies that scenario.DaemonEntryPoint
// is the SAME function as daemon.Start per specs/scenario-harness.md §10.2.
//
// reflect.ValueOf.Pointer() returns the PC of the first instruction of the
// function; two variables pointing to different functions always differ.
func TestSH017_DaemonEntryPointIdentity(t *testing.T) {
	t.Parallel()

	got := reflect.ValueOf(scenario.DaemonEntryPoint).Pointer()
	want := reflect.ValueOf(daemon.Start).Pointer()

	if got != want {
		gotName := runtime.FuncForPC(got).Name()
		wantName := runtime.FuncForPC(want).Name()
		t.Errorf("SH-017: scenario.DaemonEntryPoint = %q; want %q\n"+
			"  The harness MUST use the same composition-root function as production daemon mode (SH-017).",
			gotName, wantName)
	}
}

// TestSH017_DaemonEntryPointName verifies that the resolved function name ends
// with "daemon.Start", catching any shim that passes the pointer check via
// indirect assignment but carries a different qualified name.
func TestSH017_DaemonEntryPointName(t *testing.T) {
	t.Parallel()

	ptr := reflect.ValueOf(scenario.DaemonEntryPoint).Pointer()
	name := runtime.FuncForPC(ptr).Name()

	const wantSuffix = "daemon.Start"
	if !strings.HasSuffix(name, wantSuffix) {
		t.Errorf("SH-017: scenario.DaemonEntryPoint name = %q; want suffix %q\n"+
			"  DaemonEntryPoint must resolve to daemon.Start, not a wrapper (SH-017).",
			name, wantSuffix)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) DriveOrchestration error path → caller maps to orchestration-internal-error
// ─────────────────────────────────────────────────────────────────────────────

// TestSH017_DriveOrchestrationFailsOnInvalidProjectDir verifies that
// DriveOrchestration returns a non-nil error when the daemon cannot start due
// to an invalid (non-existent) ProjectDir. This models the daemon-startup-failure
// path that callers MUST classify as orchestration-internal-error per SH-019.
func TestSH017_DriveOrchestrationFailsOnInvalidProjectDir(t *testing.T) {
	t.Parallel()

	cfg := scenario.OrchestrationConfig{
		ProjectDir:    "/nonexistent-sh017-test/project",
		JSONLLogPath:  "/nonexistent-sh017-test/project/.harmonik/events/events.jsonl",
		HandlerBinary: "/nonexistent-sh017-test/twin",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := scenario.DriveOrchestration(ctx, cfg)
	if err == nil {
		t.Error("SH-017: DriveOrchestration with invalid ProjectDir returned nil; want non-nil error (daemon startup must fail)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) WorkflowMode zero-value default guard
// ─────────────────────────────────────────────────────────────────────────────

// TestSH017_WorkflowModeDefaultsToReviewLoop verifies that a zero WorkflowMode
// in OrchestrationConfig is silently replaced with core.WorkflowModeReviewLoop
// before the call reaches daemon.Start.
//
// Evidence: if the default substitution is absent, daemon.Start returns the
// PL-004a fail-closed error "WorkflowModeDefault must be set" before any
// ProjectDir I/O. With the substitution in place, the error comes from the
// missing ProjectDir (OS-level), not from a missing mode.
func TestSH017_WorkflowModeDefaultsToReviewLoop(t *testing.T) {
	t.Parallel()

	cfg := scenario.OrchestrationConfig{
		ProjectDir:    "/nonexistent-sh017-wfmode/project",
		JSONLLogPath:  "/nonexistent-sh017-wfmode/project/.harmonik/events/events.jsonl",
		HandlerBinary: "/nonexistent-sh017-wfmode/twin",
		// WorkflowMode intentionally zero.
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := scenario.DriveOrchestration(ctx, cfg)
	if err == nil {
		t.Fatal("SH-017: expected non-nil error from DriveOrchestration; got nil")
	}
	if strings.Contains(err.Error(), "WorkflowModeDefault must be set") ||
		strings.Contains(err.Error(), "invalid workflow_mode_default") {
		t.Errorf("SH-017: DriveOrchestration surfaced a missing-mode error: %v\n"+
			"  DriveOrchestration must substitute WorkflowModeReviewLoop when WorkflowMode is empty (PL-004a guard).",
			err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e) Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestSH017_SpecCorpusClause verifies scenario-harness.md contains SH-017 and
// the phrase "production orchestrator entry-point".
func TestSH017_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	root := sh017ModuleRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("SH-017: reading scenario-harness.md: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "SH-017") {
		t.Error("SH-017: scenario-harness.md missing SH-017 clause")
	}
	if !strings.Contains(text, "production orchestrator entry-point") {
		t.Error("SH-017: scenario-harness.md missing 'production orchestrator entry-point'; spec may have drifted")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func sh017ModuleRoot(t *testing.T) string {
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
