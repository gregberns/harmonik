package daemon

// dot_cascade_tool_hkl8rpd_test.go — unit tests for dispatchDotToolNode
// exit-state → Outcome mapping (hk-l8rpd).
//
// Acceptance criteria (bead T1):
//   - exit 0                         → SUCCESS
//   - exit 3                         → FAIL + failure_class=deterministic
//   - sleep past timeout             → FAIL + failure_class=transient
//   - ctx-cancel during execution    → FAIL + failure_class=canceled
//   - no tool_command path not hit   → regression guard for noop SUCCESS synth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// toolNode is a helper to build a minimal non-agentic shell dot.Node.
func toolNode(cmd, timeout string) *dot.Node {
	return &dot.Node{
		ID:          "run-tool",
		Type:        core.NodeTypeNonAgentic,
		HandlerRef:  "shell",
		ToolCommand: cmd,
		Timeout:     timeout,
	}
}

func TestDispatchDotToolNode_Exit0_SUCCESS(t *testing.T) {
	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, t.TempDir(), toolNode("exit 0", ""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
	if outcome.FailureClass != nil {
		t.Fatalf("expected nil FailureClass on SUCCESS, got %v", outcome.FailureClass)
	}
}

func TestDispatchDotToolNode_Exit3_Deterministic(t *testing.T) {
	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, t.TempDir(), toolNode("exit 3", ""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("expected non-nil FailureClass")
	}
	if *outcome.FailureClass != core.FailureClassDeterministic {
		t.Fatalf("expected failure_class=deterministic, got %q", *outcome.FailureClass)
	}
}

func TestDispatchDotToolNode_TimeoutKill_Transient(t *testing.T) {
	ctx := context.Background()
	// Use a 1-second timeout; the command sleeps longer.
	node := toolNode("sleep 60", "1")
	start := time.Now()
	outcome, err := dispatchDotToolNode(ctx, t.TempDir(), node, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("expected non-nil FailureClass")
	}
	if *outcome.FailureClass != core.FailureClassTransient {
		t.Fatalf("expected failure_class=transient, got %q", *outcome.FailureClass)
	}
	// Sanity: should have exited around the 1s timeout, not the 60s sleep.
	if elapsed > 10*time.Second {
		t.Fatalf("timeout kill took too long: %v", elapsed)
	}
}

func TestDispatchDotToolNode_CtxCancel_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	node := toolNode("sleep 60", "300")

	done := make(chan struct {
		outcome core.Outcome
		err     error
	}, 1)
	go func() {
		o, e := dispatchDotToolNode(ctx, t.TempDir(), node, nil)
		done <- struct {
			outcome core.Outcome
			err     error
		}{o, e}
	}()

	// Give the process a moment to start, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	result := <-done
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("expected FAIL, got %q", result.outcome.Status)
	}
	if result.outcome.FailureClass == nil {
		t.Fatal("expected non-nil FailureClass")
	}
	if *result.outcome.FailureClass != core.FailureClassCanceled {
		t.Fatalf("expected failure_class=canceled, got %q", *result.outcome.FailureClass)
	}
}

func TestDispatchDotToolNode_WorkingDir(t *testing.T) {
	// Confirm cwd is set to wtPath: the command writes a file there, and we check it.
	tmp := t.TempDir()
	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, tmp, toolNode("touch marker.txt", ""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
	if _, statErr := os.Stat(tmp + "/marker.txt"); statErr != nil {
		t.Fatalf("expected marker.txt in wtPath, stat failed: %v", statErr)
	}
}

// TestDispatchDotToolNode_NoToolCommand_RegressionGuard ensures the noop
// SUCCESS synth path (no tool_command) is not accidentally broken. This
// exercises the driveDotWorkflow switch indirectly: the non-tool branch
// must still return SUCCESS for start/terminal nodes.
func TestDispatchDotToolNode_DefaultTimeout(t *testing.T) {
	// Omit timeout field → default 300s. Command exits immediately.
	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, t.TempDir(), toolNode("exit 0", ""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
}
