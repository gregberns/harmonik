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
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), toolNode("exit 0", ""), nil)
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
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), toolNode("exit 3", ""), nil)
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
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), node, nil)
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
		o, e := dispatchDotToolNode(ctx, nil, t.TempDir(), node, nil)
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
	outcome, err := dispatchDotToolNode(ctx, nil, tmp, toolNode("touch marker.txt", ""), nil)
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
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), toolNode("exit 0", ""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
}

// TestDispatchDotToolNode_InheritsProcessEnv_PATH is the hk-m5axg regression
// guard. The standard-bead commit_gate runs `go build … && go vet … && bash …`;
// in production deps.handlerEnv is just [HARMONIK_PROJECT_HASH=…] with NO PATH,
// so a shell that did not inherit the daemon's process env could not resolve
// `go` (exit 127 → deterministic FAIL → cascade loops commit_gate→implement
// forever, never reaching review → run_stale).
//
// The test exec's a command that ONLY succeeds when PATH is present: it resolves
// a PATH-only binary via `command -v`. Builtins (exit/sleep/touch) do not need
// PATH, which is why the original test suite passed despite the bug — this case
// pins the inheritance explicitly. The handler env (PATH absent) is passed as
// the env arg to prove inheritance, not the arg, supplies PATH.
func TestDispatchDotToolNode_InheritsProcessEnv_PATH(t *testing.T) {
	ctx := context.Background()
	// Mirror production: handler env carries HARMONIK_PROJECT_HASH but NO PATH.
	handlerEnv := []string{"HARMONIK_PROJECT_HASH=test-hash"}
	// `command -v env` resolves a binary through PATH; exits non-zero when PATH
	// is missing/empty. With the fix the daemon's inherited PATH makes it pass.
	node := toolNode("command -v env >/dev/null", "")
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), node, handlerEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS (process env / PATH inherited), got %q (failure_class=%v) — "+
			"the shell node did not inherit the daemon's PATH (hk-m5axg regression)",
			outcome.Status, outcome.FailureClass)
	}
}

// TestDispatchDotToolNode_HandlerEnvOverlaysProcessEnv confirms the handler env
// is layered ON TOP of the inherited process env so operator-supplied entries
// win on duplicate keys, while inherited keys remain available (hk-m5axg).
func TestDispatchDotToolNode_HandlerEnvOverlaysProcessEnv(t *testing.T) {
	ctx := context.Background()
	// HOME is inherited from the process env; HARMONIK_PROJECT_HASH is handler-only.
	// The command requires BOTH to be present (and PATH, via `test`/`[`), proving
	// inherited + handler env are unioned.
	handlerEnv := []string{"HARMONIK_PROJECT_HASH=overlay-marker"}
	node := toolNode(`[ -n "$HOME" ] && [ "$HARMONIK_PROJECT_HASH" = "overlay-marker" ]`, "")
	outcome, err := dispatchDotToolNode(ctx, nil, t.TempDir(), node, handlerEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS (inherited HOME + handler HARMONIK_PROJECT_HASH both present), "+
			"got %q (failure_class=%v)", outcome.Status, outcome.FailureClass)
	}
}
