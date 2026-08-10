package daemon

// dot_cascade_gatecannotrun_hk2f3v4_test.go — a gate that could not RUN must not
// be routed back to the implementer (hk-2f3v4).
//
// Measured live on the codex:local cell, 2026-08-10: the scratch clone had no
// pinned gofumpt, so `make full` died at fmt-check on every attempt. The gate
// FAIL was classified deterministic, which is the class standard-bead.dot routes
// back to implement, and the run spent four implement passes and about an hour of
// real agent time on a fault no implementation could reach. It reported only
// "incomplete".
//
// The daemon never sees the 127 itself — make reports its own exit 2 — so the
// signature has to be read out of the gate output.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func TestIsGateCannotRunError(t *testing.T) {
	t.Parallel()

	cannotRun := []string{
		// The exact shape observed on the cell.
		"scripts/go-format.sh: line 49: /private/tmp/h/core-loop-lt/.tools/gofumpt: No such file or directory\nmake[2]: *** [fmt-check] Error 127\nmake[1]: *** [gate-static] Error 2\n",
		"make: *** [full] Error 127\n",
		"/bin/sh: gofumpt: command not found\n",
	}
	for _, out := range cannotRun {
		if !isGateCannotRunError([]byte(out)) {
			t.Errorf("a gate that could not run reads as runnable:\n%s", out)
		}
	}

	ranAndFailed := []string{
		// A genuine test failure — the gate ran and found a fault. This MUST stay
		// deterministic so the implementer gets its fix-loop.
		"--- FAIL: TestThing (0.02s)\n    thing_test.go:41: want 3, got 4\nFAIL\nmake[1]: *** [test] Error 1\n",
		// A build error.
		"internal/daemon/x.go:12:2: undefined: Foo\nmake: *** [gate-static] Error 2\n",
		// A bare 127 with no make-recipe or shell shape around it.
		"exit status 127 was expected by this test and printed here\n",
		"",
	}
	for _, out := range ranAndFailed {
		if isGateCannotRunError([]byte(out)) {
			t.Errorf("a gate that RAN and found a fault reads as un-runnable:\n%s", out)
		}
	}
}

// TestGateCannotRun_IsStructuralNotDeterministic is the routing consequence:
// deterministic drives the commit_gate→implement back-edge, structural does not.
func TestGateCannotRun_IsStructuralNotDeterministic(t *testing.T) {
	node := &dot.Node{
		ID:         "commit_gate",
		Type:       core.NodeTypeNonAgentic,
		HandlerRef: "shell",
		// Reproduces make's recipe-failure line and exits non-zero, without
		// needing a real make or a real missing tool.
		ToolCommand: "echo 'make[2]: *** [fmt-check] Error 127'; exit 2",
		Timeout:     "30",
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, gateLogNewRunID(t), nil, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("a gate FAIL carries no failure class")
	}
	if *outcome.FailureClass != core.FailureClassStructural {
		t.Fatalf("a gate that could not run is classified %q; structural is what keeps it off the implement back-edge", *outcome.FailureClass)
	}
}

// TestGateRanAndFailed_StaysDeterministic guards the other direction: the fix
// loop is the right answer for a fault the change actually caused, and this
// change must not take it away.
func TestGateRanAndFailed_StaysDeterministic(t *testing.T) {
	node := &dot.Node{
		ID:          "commit_gate",
		Type:        core.NodeTypeNonAgentic,
		HandlerRef:  "shell",
		ToolCommand: "echo '--- FAIL: TestThing'; exit 1",
		Timeout:     "30",
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, gateLogNewRunID(t), nil, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.FailureClass == nil || *outcome.FailureClass != core.FailureClassDeterministic {
		t.Fatalf("a real test failure must stay deterministic so the implementer gets its fix-loop; got %v", outcome.FailureClass)
	}
}

// TestStandardBeadRoutesStructuralGateFailToNeedsAttention is the other half of
// the claim: classifying the failure differently only helps if the graph routes
// it somewhere different. standard-bead.dot conditions its commit_gate back-edges
// on deterministic (→ implement) and transient (→ self-loop) ONLY, so its
// unconditional fallback catches structural and carries the run to
// close-needs-attention. No graph edit is needed, and this test says so out loud
// in case someone later adds a structural edge and reopens the loop.
func TestStandardBeadRoutesStructuralGateFailToNeedsAttention(t *testing.T) {
	g, err := loadStandardGraph(nil)
	if err != nil {
		t.Fatalf("load embedded standard-bead.dot: %v", err)
	}

	run := &core.Run{
		RunID:        gateLogNewRunID(t),
		WorkflowMode: core.WorkflowModeDot,
		Context:      make(map[string]any),
		StartTime:    time.Now(),
	}
	structural := core.FailureClassStructural
	outcome := core.Outcome{
		Status:       core.OutcomeStatusFail,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &structural,
	}

	dec := workflow.DecideNextNode(g, "commit_gate", outcome, run, core.NewCycleCounter())
	if !dec.Advance {
		t.Fatalf("a structural gate FAIL did not advance: failed=%v class=%q reason=%q", dec.Failed, dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("a structural gate FAIL routed to %q; it must reach close-needs-attention, not the implement back-edge", dec.NextNodeID)
	}
}
