package main

import "testing"

// keeper_await_ack_hkuldg_test.go — CLI exit-code mapping for
// `harmonik keeper await-ack` (hk-uldg). These cases short-circuit BEFORE any
// real tmux poll (flag misuse / missing required flags), or — for the no-pane
// case — resolve no tmux target and hit the fast no_tmux_target timeout path
// with a tiny timeout so the test is fast and touches no live tmux.

func TestKeeperAwaitAck_ExitCodes(t *testing.T) {
	projectDir := t.TempDir()

	cases := []struct {
		name string
		args []string
		want int
	}{
		// Flag-only contract: a positional argument is rejected with exit 2.
		{"positional-rejected", []string{"--project", projectDir, "someagent"}, 2},
		// Unrecognized flag → exit 2.
		{"bogus-flag", []string{"--project", projectDir, "--bogus"}, 2},
		// Missing --agent → exit 1.
		{"missing-agent", []string{"--project", projectDir, "--nonce", "n1"}, 1},
		// Missing --nonce → exit 1.
		{"missing-nonce", []string{"--project", projectDir, "--agent", "a1"}, 1},
		// No resolvable pane + a tiny timeout → timeout path → exit 3. The agent
		// name is deliberately one that has no live tmux session, so
		// ResolveTmuxTarget returns "" and AwaitAck takes the fast
		// no_tmux_target → ErrAckTimeout branch (exit 3) without polling tmux.
		{"no-pane-timeout", []string{"--project", projectDir, "--agent", "await-ack-no-pane-xyz", "--nonce", "n1", "--timeout", "50ms", "--poll", "10ms"}, 3},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if code := runKeeperAwaitAck(tc.args); code != tc.want {
				t.Fatalf("args %v: want exit %d, got %d", tc.args, tc.want, code)
			}
		})
	}
}
