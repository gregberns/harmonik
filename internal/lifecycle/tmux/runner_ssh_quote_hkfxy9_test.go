package tmux

// runner_ssh_quote_hkfxy9_test.go — regression for the remote-launch bug where
// SSHRunner shipped the tmux argv UNQUOTED, so the worker's login shell re-parsed
// `tmux new-window -F #{pane_id} …` and treated `#{pane_id}` as a `#` comment —
// truncating the command, so the agent claude window (and process) was never
// created on the worker → agent_ready_timeout. The fix shell-quotes every token.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestShellQuoteArg_RoundTripsThroughPOSIXShell proves that a real /bin/sh,
// parsing the quoted tokens, recovers each one EXACTLY — including the tmux
// format string `#{pane_id}` (which unquoted would start a comment), spaces, and
// embedded single quotes.
func TestShellQuoteArg_RoundTripsThroughPOSIXShell(t *testing.T) {
	tokens := []string{
		"tmux", "new-window", "-P", "-F", "#{pane_id}", "-d",
		"-t", "harmonik-sess:", "-n", "win/name",
		"-c", "/Users/gb/harmonik-worker/repo/.harmonik/worktrees/019ee7d6",
		"-e", "K=value with spaces",
		"--", "claude --dangerously-skip-permissions",
		"it's-got-a-quote",
	}
	quoted := make([]string, len(tokens))
	for i, tk := range tokens {
		quoted[i] = shellQuoteArg(tk)
	}
	// `printf '%s\n' <quoted tokens>` — the shell parses the quoted tokens as
	// argv to printf, which prints each on its own line. If quoting were wrong
	// (e.g. `#{pane_id}` truncating as a comment) the count/values would differ.
	script := "printf '%s\\n' " + strings.Join(quoted, " ")
	out, err := exec.CommandContext(context.Background(), "/bin/sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("sh -c failed: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(tokens) {
		t.Fatalf("recovered %d tokens, want %d (quoting truncated the command?)\n got=%q\nwant=%q", len(got), len(tokens), got, tokens)
	}
	for i := range tokens {
		if got[i] != tokens[i] {
			t.Errorf("token %d: got %q want %q", i, got[i], tokens[i])
		}
	}
}

// TestSSHRunner_Command_QuotesRemoteArgv asserts SSHRunner builds the remote
// command as a SINGLE shell-quoted operand after the host, not a raw argv vector
// (which OpenSSH would space-join and the remote shell would re-parse).
func TestSSHRunner_Command_QuotesRemoteArgv(t *testing.T) {
	s := SSHRunner{Host: "100.87.151.114", Opts: []string{"-o", "ConnectTimeout=8"}}
	cmd := s.Command(context.Background(), "tmux", "new-window", "-F", "#{pane_id}")
	// cmd.Args = ["ssh", "-o", "ConnectTimeout=8", "<host>", "--", "<remote>"]
	args := cmd.Args
	if len(args) < 2 || args[0] != "ssh" {
		t.Fatalf("expected ssh invocation, got %q", args)
	}
	remote := args[len(args)-1]
	want := `'tmux' 'new-window' '-F' '#{pane_id}'`
	if remote != want {
		t.Fatalf("remote command not single-quoted:\n got=%q\nwant=%q", remote, want)
	}
	// The whole tmux argv must be the LAST operand (one string), so the host is
	// directly followed by `--` then exactly one remaining element.
	if args[len(args)-2] != "--" {
		t.Fatalf("expected `--` immediately before the single remote operand, got %q", args)
	}
}
