package daemon

// dot_cascade_gatekilled_test.go — a commit gate that was KILLED said nothing
// about the code, so it must not be reported as a test failure
// (hk-killed-gate-read-as-red-0hi5z).
//
// Measured live 2026-08-11 by lane bravo: `make full` ran about 19 minutes of a
// 3600s node budget, passed build, lint and the subprocess tier, reached the
// scenario tier and was SIGTERM'd mid-flight:
//
//	make[1]: *** [test-scenario] Terminated: 15
//	make:     *** [full] Terminated: 15
//
// Nothing failed. The gate never reached a verdict. dispatchDotToolNode had no
// case for a signal kill, so it fell to the deterministic default, drove the
// commit_gate→implement back-edge, and resumed the implementer with "the
// build/test gate did not pass. Fix the failure and re-commit". There was no
// failure to fix.
//
// A killed gate is canceled: handler-contract.md HC-063 already names that class
// for a gate a signal stopped, and standard-bead.dot conditions its commit_gate
// out-edges on SUCCESS, deterministic and transient only. canceled matches none
// of them, so the unconditional fallback carries the run to
// close-needs-attention. The run stops and says why, rather than retrying into a
// kill nobody has identified.
//
// Two detection paths, because neither alone covers both cases:
//   - process state: the LOCAL gate is a direct child, so a signal that reaches
//     it shows up in syscall.WaitStatus. This is exact.
//   - output signature: a signal that reaches only a DESCENDANT leaves the top
//     process exiting normally (make reports its recipe's death and exits 2),
//     and the REMOTE path sees ssh's exit status, not the gate's. The make
//     "Terminated: N" line is the only evidence in both of those cases.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// TestGateKilledBySignal_ProcessState is the direct-child case: the signal
// reaches the gate shell itself, so the exit state carries it.
func TestGateKilledBySignal_ProcessState(t *testing.T) {
	node := &dot.Node{
		ID:         "commit_gate",
		Type:       core.NodeTypeNonAgentic,
		HandlerRef: "shell",
		// The gate shell SIGTERMs itself. Stands in for an outside SIGTERM to
		// the gate's process group without needing a second process to send it.
		ToolCommand: "kill -TERM $$",
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
	if *outcome.FailureClass != core.FailureClassCanceled {
		t.Fatalf("a gate killed by a signal is classified %q; canceled is what stops the run for triage instead of telling the implementer its tests failed", *outcome.FailureClass)
	}
}

// TestGateKilledBySignal_MakeOutput is the descendant case, and the exact shape
// the live run produced: the top-level make survives long enough to report its
// recipe's death and exits 2, so the exit STATE says "exit 2" and only the
// OUTPUT says the run was killed.
func TestGateKilledBySignal_MakeOutput(t *testing.T) {
	node := &dot.Node{
		ID:         "commit_gate",
		Type:       core.NodeTypeNonAgentic,
		HandlerRef: "shell",
		ToolCommand: "echo 'ok  	github.com/gregberns/harmonik/internal/daemon	25.5s'; " +
			"echo 'make[1]: *** [test-scenario] Terminated: 15'; " +
			"echo 'make: *** [full] Terminated: 15'; exit 2",
		Timeout: "30",
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, gateLogNewRunID(t), nil, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.FailureClass == nil {
		t.Fatal("a gate FAIL carries no failure class")
	}
	if *outcome.FailureClass != core.FailureClassCanceled {
		t.Fatalf("a gate whose output shows it was terminated by a signal is classified %q; canceled is what keeps it off the implement back-edge", *outcome.FailureClass)
	}
}

// TestGateSignalKillOutputLine pins the output detector in both directions. The
// anchor is make's `*** [` recipe-failure prefix on the SAME line as the signal
// word, so ordinary test output that mentions a signal does not trip it.
func TestGateSignalKillOutputLine(t *testing.T) {
	t.Parallel()

	killed := []string{
		// The exact shape the live run produced.
		"ok  	internal/daemon	25.5s\nmake[1]: *** [test-scenario] Terminated: 15\nmake: *** [full] Terminated: 15\n",
		"make: *** [full] Killed: 9\n",
		"make: *** [Makefile:41: test-scenario] Terminated\n",
	}
	for _, out := range killed {
		if _, ok := gateSignalKillOutputLine([]byte(out)); !ok {
			t.Errorf("a gate that was killed reads as a clean exit:\n%s", out)
		}
	}

	ranAndFailed := []string{
		// A genuine test failure. MUST stay off this detector.
		"--- FAIL: TestThing (0.02s)\n    thing_test.go:41: want 3, got 4\nFAIL\nmake[1]: *** [test] Error 1\n",
		"internal/daemon/x.go:12:2: undefined: Foo\nmake: *** [gate-static] Error 2\n",
		// A test whose own output talks about signals, with no make recipe line.
		"--- FAIL: TestReaper\n    reaper_test.go:9: expected the child Terminated, got Killed\nFAIL\n",
		// A missing tool — structural, and a different branch owns it.
		"make[2]: *** [fmt-check] Error 127\n",
		"",
	}
	for _, out := range ranAndFailed {
		if line, ok := gateSignalKillOutputLine([]byte(out)); ok {
			// gateEvidenceQuote, not a raw %s: `out` here carries "] Error 127",
			// and a FAILING run of this test puts it in the log the NEXT gate's
			// classifier reads.
			t.Error(gateEvidenceQuote(fmt.Sprintf("a gate that RAN and found a fault reads as killed (matched %q):\n%s", line, out)))
		}
	}
}

// TestGateBackEdgeMessage_NeverAssertsAnUnobservedFailure is the part that holds
// even if the reclassification is argued: whatever class reaches the implement
// back-edge, the message must describe what the gate actually did.
func TestGateBackEdgeMessage_NeverAssertsAnUnobservedFailure(t *testing.T) {
	t.Parallel()

	const notes = "make: *** [full] Terminated: 15"

	det := gateBackEdgeMessage(core.FailureClassDeterministic, notes)
	if !strings.Contains(det, "build/test gate did not pass") {
		t.Errorf("a gate that RAN and found a fault must still tell the implementer to fix it; got:\n%s", det)
	}

	// Every class other than deterministic means the gate reached no verdict.
	for _, class := range []core.FailureClass{
		core.FailureClassTransient,
		core.FailureClassCanceled,
		core.FailureClassStructural,
		core.FailureClass(""), // no class recorded — still not evidence of a failure
	} {
		msg := gateBackEdgeMessage(class, notes)
		if strings.Contains(msg, "did not pass") || strings.Contains(msg, "Fix the failure") {
			t.Errorf("a %q gate reached no verdict, but the implementer is told its tests failed:\n%s", class, msg)
		}
		if !strings.Contains(msg, "did not finish") {
			t.Errorf("a %q gate message does not say the gate stopped early:\n%s", class, msg)
		}
	}
}

// TestGateRanAndFailed_StillDeterministic_Killed guards the other direction. A
// gate that RAN and found a fault is the case the fix-loop exists for, and this
// change must not take it away.
func TestGateRanAndFailed_StillDeterministic_Killed(t *testing.T) {
	for _, cmd := range []string{
		"echo '--- FAIL: TestThing (0.02s)'; echo 'FAIL'; echo 'make[1]: *** [test] Error 1'; exit 2",
		"echo 'internal/daemon/x.go:12:2: undefined: Foo'; echo 'make: *** [gate-static] Error 2'; exit 2",
	} {
		node := &dot.Node{
			ID:          "commit_gate",
			Type:        core.NodeTypeNonAgentic,
			HandlerRef:  "shell",
			ToolCommand: cmd,
			Timeout:     "30",
		}
		outcome, err := dispatchDotToolNode(context.Background(), nil, gateLogNewRunID(t), nil, t.TempDir(), t.TempDir(), node, nil)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if outcome.FailureClass == nil {
			t.Fatalf("a real gate failure carries no failure class at all; %q must stay deterministic so the implementer gets its fix-loop", cmd)
		}
		if *outcome.FailureClass != core.FailureClassDeterministic {
			t.Fatalf("a real gate failure must stay deterministic so the implementer gets its fix-loop; %q was classified %q", cmd, *outcome.FailureClass)
		}
	}
}

// TestStandardBeadRoutesCanceledGateFailToNeedsAttention is the other half of
// the claim: classifying the failure differently only helps if the graph routes
// it somewhere different. standard-bead.dot conditions its commit_gate
// back-edges on deterministic (→ implement) and transient (→ self-loop) ONLY, so
// its unconditional fallback catches canceled and carries the run to
// close-needs-attention. No graph edit is needed, and this test says so out loud
// in case someone later adds a canceled edge and re-opens the loop.
func TestStandardBeadRoutesCanceledGateFailToNeedsAttention(t *testing.T) {
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
	canceled := core.FailureClassCanceled
	outcome := core.Outcome{
		Status:       core.OutcomeStatusFail,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &canceled,
	}

	dec := workflow.DecideNextNode(g, "commit_gate", outcome, run, core.NewCycleCounter())
	if !dec.Advance {
		t.Fatalf("a canceled gate FAIL did not advance: failed=%v class=%q reason=%q", dec.Failed, dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID == "implement" {
		t.Fatal("a gate that was killed routed BACK TO THE IMPLEMENTER; that is the defect — the implementer is sent to fix a fault the gate never observed")
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("a canceled gate FAIL routed to %q; a gate that reached no verdict must stop at close-needs-attention for triage", dec.NextNodeID)
	}
}

// TestStandardBeadRoutesTransientGateFailToSelfLoop keeps the transient
// self-loop covered. The killed gate no longer takes that edge, but the edge is
// still live for the causes that ARE worth an immediate retry on the same tree:
// a node-timeout kill, and the go build-cache TOCTOU signature. This asserts the
// self-loop stays reachable for them.
func TestStandardBeadRoutesTransientGateFailToSelfLoop(t *testing.T) {
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
	transient := core.FailureClassTransient
	outcome := core.Outcome{
		Status:       core.OutcomeStatusFail,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &transient,
	}

	dec := workflow.DecideNextNode(g, "commit_gate", outcome, run, core.NewCycleCounter())
	if !dec.Advance {
		t.Fatalf("a transient gate FAIL did not advance: failed=%v class=%q reason=%q", dec.Failed, dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID != "commit_gate" {
		t.Fatalf("a transient gate FAIL routed to %q; the self-loop is what retries a gate that hit an infra glitch", dec.NextNodeID)
	}
}

// TestGateTimedOut_ClassifiesTransientNotCanceled pins the check ORDER inside
// classifyDotToolNodeFailure. The node's own timeout is a kill the daemon
// issues, and it must be read BEFORE the outside-signal branch.
//
// exec.CommandContext kills on the deadline with SIGKILL, so a gate that ran out
// of time is Signaled() too. Move the signal branch up and it swallows every
// timeout and calls it canceled, which stops the run for triage instead of
// retrying the gate on the transient self-loop. The whole package stayed green
// when a reviewer made exactly that swap, so nothing else guards the order.
func TestGateTimedOut_ClassifiesTransientNotCanceled(t *testing.T) {
	node := &dot.Node{
		ID:         "commit_gate",
		Type:       core.NodeTypeNonAgentic,
		HandlerRef: "shell",
		// Runs much longer than the budget below, so the node deadline is what
		// stops it.
		ToolCommand: "sleep 5",
		Timeout:     "1",
	}

	outcome, err := dispatchDotToolNode(context.Background(), nil, gateLogNewRunID(t), nil, t.TempDir(), t.TempDir(), node, nil)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q; the gate outlived its 1s budget, so the timeout must stop it", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("a gate FAIL carries no failure class")
	}
	if *outcome.FailureClass != core.FailureClassTransient {
		t.Fatalf("a gate that the node timeout killed is classified %q; transient is what retries it on the self-loop, and canceled would stop the run for a kill the daemon itself issued", *outcome.FailureClass)
	}
}
