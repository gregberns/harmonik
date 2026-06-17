package daemon

// dot_cascade_tool_remote_env_hk230h_test.go — unit tests for remote-env-inlining
// in dispatchDotToolNode (hk-230h).
//
// Gap: the REMOTE branch (runner != nil) used a login shell for PATH but did
// NOT propagate the handler-supplied env vars (the `env` parameter, e.g.
// HARMONIK_PROJECT_HASH) to the remote shell. Any tool_command that referenced
// those vars received an empty/unset value, causing env-dependent shell nodes
// to fail remotely.
//
// Fix: the REMOTE branch now prepends `export KEY='VALUE'; …` for every entry
// in `env` before the `cd && tool_command` so the worker shell sees them.
//
// Acceptance criteria (T1 / T2):
//   T1: env var from handler env is accessible inside tool_command when runner
//       is non-nil (RecordingRunner executing locally simulates the worker).
//   T2: the generated argv carries the export statements verbatim, proving the
//       inlining happens at the correct layer (argv, not cmd.Env).
//   T3: env values containing single-quote characters are shell-quoted safely
//       (no injection / syntax error on the worker shell).
//   T4: zero env entries → no export statements → command unchanged (NFR7 parity).

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestDispatchDotToolNode_Remote_EnvVarAccessible (T1): verifies that a
// handler-supplied env var is accessible inside tool_command when dispatched
// through a non-nil runner (simulating a remote worker via RecordingRunner).
//
// The RecordingRunner with nil CmdFunc executes commands directly via
// exec.CommandContext, which lets us run the inlined shell script locally and
// observe its exit code — a real exit 0 proves the env var was present and
// matched the expected value.
func TestDispatchDotToolNode_Remote_EnvVarAccessible(t *testing.T) {
	ctx := context.Background()
	rr := &tmux.RecordingRunner{} // nil CmdFunc → exec.CommandContext directly
	handlerEnv := []string{"HARMONIK_PROJECT_HASH=remote-env-test-hash-hk230h"}
	node := toolNode(`[ "$HARMONIK_PROJECT_HASH" = "remote-env-test-hash-hk230h" ]`, "")
	outcome, err := dispatchDotToolNode(ctx, rr, t.TempDir(), node, handlerEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("T1: expected SUCCESS — env var HARMONIK_PROJECT_HASH not visible to "+
			"remote tool_command; got %q (failure_class=%v, notes=%q) [hk-230h]",
			outcome.Status, outcome.FailureClass, outcome.Notes)
	}
}

// TestDispatchDotToolNode_Remote_MultipleEnvVarsAccessible extends T1 to two
// env vars to confirm all entries in the `env` slice are exported, not just the
// first one.
func TestDispatchDotToolNode_Remote_MultipleEnvVarsAccessible(t *testing.T) {
	ctx := context.Background()
	rr := &tmux.RecordingRunner{}
	handlerEnv := []string{
		"HARMONIK_PROJECT_HASH=hash-abc",
		"EXTRA_VAR=extra-value-xyz",
	}
	node := toolNode(
		`[ "$HARMONIK_PROJECT_HASH" = "hash-abc" ] && [ "$EXTRA_VAR" = "extra-value-xyz" ]`,
		"",
	)
	outcome, err := dispatchDotToolNode(ctx, rr, t.TempDir(), node, handlerEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("T1-multi: expected SUCCESS — one or more env vars missing in remote shell; "+
			"got %q (failure_class=%v, notes=%q) [hk-230h]",
			outcome.Status, outcome.FailureClass, outcome.Notes)
	}
}

// TestDispatchDotToolNode_Remote_EnvInlinedInArgv (T2): verifies that the
// generated argv contains `export KEY='VALUE'; ` for every env entry, proving
// inlining happens at the argv layer (not cmd.Env, which SSHRunner ignores).
func TestDispatchDotToolNode_Remote_EnvInlinedInArgv(t *testing.T) {
	ctx := context.Background()
	rr := &tmux.RecordingRunner{}
	handlerEnv := []string{"FOO=bar", "BAZ=qux"}
	node := toolNode("exit 0", "")
	_, _ = dispatchDotToolNode(ctx, rr, t.TempDir(), node, handlerEnv)
	if len(rr.Calls) == 0 {
		t.Fatal("T2: no Command calls recorded by RecordingRunner")
	}
	// The last positional arg to /bin/sh -lc is the script string.
	args := rr.Calls[0].Args
	script := args[len(args)-1]
	for _, want := range []string{"export FOO='bar';", "export BAZ='qux';"} {
		if !strings.Contains(script, want) {
			t.Errorf("T2: script arg does not contain %q; script=%q [hk-230h]", want, script)
		}
	}
}

// TestDispatchDotToolNode_Remote_EnvValueWithSingleQuote (T3): verifies that a
// value containing a single-quote character is inlined without causing a shell
// syntax error (shellQuote uses the '\” escape convention).
func TestDispatchDotToolNode_Remote_EnvValueWithSingleQuote(t *testing.T) {
	ctx := context.Background()
	rr := &tmux.RecordingRunner{}
	// Value with an embedded single-quote: it's
	handlerEnv := []string{`MSG=it's a test`}
	// The shell test: $MSG must equal exactly "it's a test"
	node := toolNode(`[ "$MSG" = "it's a test" ]`, "")
	outcome, err := dispatchDotToolNode(ctx, rr, t.TempDir(), node, handlerEnv)
	if err != nil {
		t.Fatalf("T3: unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("T3: expected SUCCESS — single-quote in env value caused a shell error "+
			"or wrong expansion; got %q (notes=%q) [hk-230h]",
			outcome.Status, outcome.Notes)
	}
}

// TestDispatchDotToolNode_Remote_ZeroEnvEntries (T4): verifies that passing an
// empty env slice produces a script with no export statements — i.e. the cd &&
// tool_command form is unchanged (NFR7 parity: no regression for callers
// supplying no env overrides).
func TestDispatchDotToolNode_Remote_ZeroEnvEntries(t *testing.T) {
	ctx := context.Background()
	rr := &tmux.RecordingRunner{}
	node := toolNode("exit 0", "")
	tmp := t.TempDir()
	_, _ = dispatchDotToolNode(ctx, rr, tmp, node, nil)
	if len(rr.Calls) == 0 {
		t.Fatal("T4: no Command calls recorded")
	}
	script := rr.Calls[0].Args[len(rr.Calls[0].Args)-1]
	if strings.Contains(script, "export ") {
		t.Errorf("T4: script contains 'export' despite nil env; script=%q [hk-230h]", script)
	}
}
