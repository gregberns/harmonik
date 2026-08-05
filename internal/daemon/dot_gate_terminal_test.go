package daemon_test

// dot_gate_terminal_test.go — a cognition gate must be judged on what its
// evaluator REPORTED, not only on the verdict file it left behind.
//
// executeCognitionGate switched on launch.Fail alone. It never read the
// evaluator's socket outcome, its exit code, or its progress-stream watcher, so
// an evaluator that wrote an `allow` verdict and then declared failure or
// crashed was still believed and the workflow proceeded past the gate.
//
// A gate that cannot be trusted fails CLOSED: the error fails the run. That is
// the safe direction for a gate in a way it is not for an implementer, so there
// is no budget-kill exemption here — pasteInjectQuitOnGateFile writes no budget
// marker, and its only deadline kill fires when no verdict exists at all.
//
// Bead: hk-sb8jy.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// cognitionGateAllowJSON is a well-formed allow verdict. policy.ParseGateVerdict
// reads it, so it must carry schema_version 1 and a declared decision value.
const cognitionGateAllowJSON = `{"schema_version":1,"decision":"allow",` +
	`"reason":"fixture gate evaluator allows the change."}`

// cognitionGateControlPoint builds the minimal cognition-tagged Gate the
// evaluator path needs: a DelegationPath (which is what makes it cognition) and
// Kind=Gate.
func cognitionGateControlPoint() core.ControlPoint {
	return core.ControlPoint{
		Name:    "fixture-cognition-gate",
		Kind:    core.KindGate,
		ModeTag: core.ModeTagCognition,
		Evaluator: core.Evaluator{
			Mode: core.ModeTagCognition,
			DelegationPath: &core.DelegationPath{
				Role:              "gate-evaluator",
				ModelClass:        "reviewer-tier-1",
				InputSchemaRef:    "gate-input-v1",
				ResponseSchemaRef: "gate-verdict-v1",
				PromptTemplateRef: "gate-eval-v1",
			},
		},
		SchemaVersion: 1,
	}
}

// cognitionGateHandler writes the /bin/sh evaluator: it lands an allow verdict
// and then exits with the supplied code.
//
// Writing the verdict is load-bearing. Without it the verdict read fails on its
// own and the assertion below passes for free, proving nothing about the exit.
func cognitionGateHandler(t *testing.T, exitCode int) string {
	t.Helper()
	body := "mkdir -p .harmonik\n" +
		"printf '%s' '" + cognitionGateAllowJSON + "' > .harmonik/gate-verdict.json\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	return dotFixtureHandlerScript(t, "cognition-gate-fixture.sh", body)
}

// cognitionGateMissingReviewFile is what the hook bridge really relays when a
// cognition gate's Stop hook fires.
//
// hookrelay.go branches on phase == "reviewer" and reads .harmonik/review.json.
// The gate launches with ReviewLoopPhaseReviewer (dot_gate.go) but writes
// gate-verdict.json, so the bridge never finds review.json and emits this: an
// outcome_emitted carrying an error and NO kind. Every real cognition gate
// produces it, including one that wrote a perfectly good verdict.
const cognitionGateMissingReviewFile = `{"error":"missing_review_file"}`

// runCognitionGate drives the production executeCognitionGate against a /bin/sh
// evaluator in a scratch worktree and returns its error.
func runCognitionGate(t *testing.T, exitCode int) error {
	t.Helper()
	return runCognitionGateWithOutcome(t, exitCode, "")
}

// runCognitionGateWithOutcome is runCognitionGate with an explicit
// outcome_emitted payload standing in for what the hook bridge relayed.
func runCognitionGateWithOutcome(t *testing.T, exitCode int, hookOutcome string) error {
	t.Helper()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	wtPath := filepath.Join(projectDir, "gate-worktree")
	//nolint:gosec // G301: test-only scratch dir.
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("runCognitionGate: mkdir worktree: %v", err)
	}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{cognitionGateHandler(t, exitCode)},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		HookStore:        dotFixtureHookStore{Outcome: json.RawMessage(hookOutcome)},
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	node := &dot.Node{
		ID:      "gate",
		Type:    "gate",
		GateRef: "fixture-cognition-gate",
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	return daemon.ExportedExecuteCognitionGate(ctx, deps, core.RunID(uuid.New()),
		cognitionGateControlPoint(), wtPath, node,
		core.BeadID("hk-sb8jy-cognition-gate"), core.BeadRecord{BeadID: "hk-sb8jy-cognition-gate"}, "")
}

// TestCognitionGate_AllowThenCleanExitIsAccepted is the control. Same evaluator,
// same allow verdict, exit 0. Without it the failure test below is satisfied by
// any fixture in which the gate can never succeed.
func TestCognitionGate_AllowThenCleanExitIsAccepted(t *testing.T) {
	t.Parallel()

	if err := runCognitionGate(t, 0); err != nil {
		t.Fatalf("cognition gate returned an error for an evaluator that wrote allow and exited 0: %v\n"+
			"The happy path must be accepted, or the failure test below proves nothing.", err)
	}
}

// TestCognitionGate_AllowThenNonZeroExitIsRefused is the claim: an evaluator
// that writes `allow` and then dies has not produced a decision the workflow may
// act on. The verdict file is well-formed and the read succeeds, so the exit is
// the only thing standing between this run and a gate that lets the work past.
func TestCognitionGate_AllowThenNonZeroExitIsRefused(t *testing.T) {
	t.Parallel()

	if err := runCognitionGate(t, 4); err == nil {
		t.Error("cognition gate ACCEPTED an evaluator that exited non-zero after writing an allow verdict.\n" +
			"executeCognitionGate switched on launch.Fail alone and never read the exit, so a crashed evaluator's allow was believed and the workflow proceeded past the gate.")
	}
}

// TestCognitionGate_MissingReviewFileRelayIsNotAFailure is the realistic control,
// and it is the one the first version of this fix got wrong.
//
// EVERY real cognition gate relays outcome_emitted{"error":"missing_review_file"}
// on its Stop hook, because dot_gate.go launches the evaluator with
// ReviewLoopPhaseReviewer while hookrelay.go's reviewer branch reads review.json
// and the gate writes gate-verdict.json instead. That payload is non-nil and has
// no kind, so a classifier that keyed its clean-exit escape on
// `socketOutcome == nil` refused every gate — a good verdict included.
//
// The bridge saying it could not find a file is not the agent reporting a
// failure. A clean exit with a good verdict is accepted.
func TestCognitionGate_MissingReviewFileRelayIsNotAFailure(t *testing.T) {
	t.Parallel()

	err := runCognitionGateWithOutcome(t, 0, cognitionGateMissingReviewFile)
	if err != nil {
		t.Errorf("cognition gate REFUSED an evaluator that wrote a good allow verdict and exited 0: %v\n"+
			"The only thing against it was the bridge's own missing_review_file relay, which every cognition gate produces. This refuses all of them.", err)
	}
}

// TestCognitionGate_FailureSignalIsStillAFailure keeps the fix above from
// over-reaching. Treating a KINDLESS relay payload as "nothing reported" must not
// also excuse a payload that reports a real FAILURE_SIGNAL.
func TestCognitionGate_FailureSignalIsStillAFailure(t *testing.T) {
	t.Parallel()

	if err := runCognitionGateWithOutcome(t, 0, dotFixtureFailureSignal); err == nil {
		t.Error("cognition gate ACCEPTED an evaluator that signalled FAILURE_SIGNAL after writing an allow verdict")
	}
}
