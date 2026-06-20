package main

// keeper_positional_reject_hknbft_test.go — hk-nbft regression guard.
//
// EVERY `harmonik keeper` subcommand (and the bare watcher) is FLAG-ONLY: the
// agent is named via --agent ONLY. A positional argument is the recurring keeper
// footgun (a bare token silently took the place of --agent and routed to the
// wrong project / a literal "--agent" agent name). This table-driven test asserts
// that EACH subcommand rejects a positional argument with a NON-ZERO exit (2, the
// shared restart-now/await-ack contract) AND the shared rejection message
// substrings ("flag-only" + "--agent").
//
// All subcommands reject the positional at the argument-PARSE stage, BEFORE any
// tmux pane resolution or lockfile acquisition, so this test performs no live
// tmux/process side effects (no fork-bomb risk per the keeper-smoke note).

import (
	"os"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and returns whatever
// fn wrote to stderr. Several keeper run-entry functions write directly to
// os.Stderr (not an injectable writer), so the test captures it at the FD level.
func captureStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
		done <- sb.String()
	}()

	code := fn()

	_ = w.Close()
	os.Stderr = orig
	out := <-done
	_ = r.Close()
	return code, out
}

// TestKeeperSubcommands_RejectPositional_HkNbft is the hk-nbft regression guard:
// every keeper subcommand must reject a positional agent with a non-zero exit and
// the shared flag-only message.
func TestKeeperSubcommands_RejectPositional_HkNbft(t *testing.T) {
	// NOT t.Parallel(): captureStderr swaps the process-global os.Stderr.

	// The positional token a confused operator would type instead of --agent.
	const positional = "captain"

	cases := []struct {
		name string
		run  func(args []string) int
	}{
		{"keeper (bare watcher)", runKeeperSubcommand},
		{"set-dispatching", runKeeperSetDispatching},
		{"clear-dispatching", runKeeperClearDispatching},
		{"restart-now", runKeeperRestartNow},
		{"ping", runKeeperPing},
		{"await-ack", runKeeperAwaitAck},
		{"enable", runKeeperEnableSubcommand},
		{"doctor", runKeeperDoctorSubcommand},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := captureStderr(t, func() int {
				return tc.run([]string{positional})
			})
			if code == 0 {
				t.Fatalf("%s: positional %q accepted (exit 0) — must be flag-only with a non-zero exit\nstderr: %s",
					tc.name, positional, stderr)
			}
			// restart-now / await-ack established the contract at exit 2; every
			// keeper subcommand must match it for a positional reject.
			if code != 2 {
				t.Errorf("%s: positional reject exit = %d; want 2 (the restart-now/await-ack contract)\nstderr: %s",
					tc.name, code, stderr)
			}
			if !strings.Contains(stderr, "flag-only") {
				t.Errorf("%s: stderr missing %q; got: %s", tc.name, "flag-only", stderr)
			}
			if !strings.Contains(stderr, "--agent") {
				t.Errorf("%s: stderr missing %q hint; got: %s", tc.name, "--agent", stderr)
			}
		})
	}
}

// TestKeeperSubcommands_AcceptAgentFlag_HkNbft is the positive companion: with
// --agent supplied and NO positional, the parse stage must NOT reject on the
// flag-only path (i.e. it must not return exit 2 with the flag-only message).
// We assert by the ABSENCE of the flag-only reject — the subcommands then proceed
// into their real bodies (which may exit non-zero for unrelated reasons such as a
// missing pane / stale handoff), so we do not assert exit 0 here.
func TestKeeperSubcommands_AcceptAgentFlag_HkNbft(t *testing.T) {
	cases := []struct {
		name string
		run  func(args []string) int
		args []string
	}{
		{"set-dispatching", runKeeperSetDispatching, []string{"--agent", "kpr-test-agent", "--project", t.TempDir()}},
		{"clear-dispatching", runKeeperClearDispatching, []string{"--agent", "kpr-test-agent", "--project", t.TempDir()}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := captureStderr(t, func() int {
				return tc.run(tc.args)
			})
			if strings.Contains(stderr, "flag-only") {
				t.Errorf("%s: --agent form wrongly hit the flag-only reject (code=%d)\nstderr: %s",
					tc.name, code, stderr)
			}
		})
	}
}
