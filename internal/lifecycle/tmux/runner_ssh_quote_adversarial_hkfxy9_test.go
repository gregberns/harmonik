package tmux

// runner_ssh_quote_adversarial_hkfxy9_test.go — ADVERSARIAL hardening for the
// SSHRunner remote-quoting fix (hk-fxy9/hk-538l). The original bug shipped the
// tmux argv UNQUOTED, so the worker's login shell re-parsed
// `tmux new-window -F #{pane_id} …` and read `#{pane_id}` as a `#` comment,
// truncating the command → the agent claude window/process was never created →
// agent_ready_timeout. The fix single-quotes every token (shellQuoteArg).
//
// The existing runner_ssh_quote_hkfxy9_test.go proves a happy-path round-trip and
// the basic single-quoted-operand structure. This file attacks the quoting from
// the angles that test misses:
//   1. A wide table of shell-metachar / injection / edge-shape tokens, each
//      round-tripped through a REAL /bin/sh login-shell simulation, with EXACTLY
//      ONE post-`--` operand asserted (the invariant that makes ssh's space-join
//      a no-op).
//   2. A NEGATIVE CONTROL proving the harness would CATCH the original bug.
//   3. Opts-before-host structure with a space-containing remote token.
//   4. Stdin left nil for the caller's load-buffer path.
//
// RECOVERY STRATEGY: we do NOT use `printf '%s\n'` to recover argv, because a
// token that itself contains a newline is then indistinguishable from a token
// boundary. Instead we run a tiny remote script that NUL-delimits each argv
// element (`for a in "$@"; do printf '%s\0' "$a"; done`), then split the captured
// bytes on NUL. NUL cannot appear inside a shell word, so this faithfully
// distinguishes every token shape — including empty strings, embedded newlines,
// and embedded tabs.

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

// recoverArgvViaLoginShell simulates exactly what OpenSSH does to the post-host
// operand: it space-joins the operands (here there is exactly one) and runs the
// result via a POSIX login shell. We prepend a NUL-delimiting wrapper so we can
// recover the argv the remote shell actually reconstructed.
//
// The `remoteOperand` is the single shell-quoted string SSHRunner places after
// `--`. We feed it as the argument list of an inline script `for a in "$@" …`.
// Because the operand is itself a sequence of quoted words, `"$@"` after word
// splitting yields one shell arg per intended token.
func recoverArgvViaLoginShell(ctx context.Context, t *testing.T, remoteOperand string) []string {
	t.Helper()
	// `set -- <remoteOperand>` makes the remote shell re-parse the quoted words
	// into positional params, then we NUL-delimit each. This is precisely the
	// remote-login-shell re-parse path, with a faithful (NUL) recovery channel.
	script := "set -- " + remoteOperand + `; for a in "$@"; do printf '%s\0' "$a"; done`
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("login-shell simulation failed for operand %q: %v", remoteOperand, err)
	}
	return splitNUL(out)
}

// splitNUL splits NUL-delimited bytes into strings. Because each token is emitted
// as `<token>\0`, a trailing NUL produces a trailing empty element we drop; an
// EMPTY token emits just `\0` and is preserved as "" in its slot. With zero
// tokens the output is empty and we return an empty slice.
func splitNUL(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	parts := bytes.Split(b, []byte{0})
	// Each token contributes "<token>\0", so the final split element is the empty
	// tail after the last NUL — drop exactly that one.
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(p)
	}
	return out
}

// postDashDashOperands returns the operands AFTER the `--` separator in an ssh
// argv. The invariant under test: there is EXACTLY ONE, regardless of how many
// tokens / spaces the remote command has.
func postDashDashOperands(t *testing.T, sshArgs []string) []string {
	t.Helper()
	for i, a := range sshArgs {
		if a == "--" {
			return sshArgs[i+1:]
		}
	}
	t.Fatalf("no `--` separator in ssh argv: %q", sshArgs)
	return nil
}

// TestSSHRunner_AdversarialArgvRoundTrip drives a table of nasty single-token
// argvs through SSHRunner.Command + a real login-shell re-parse, asserting each
// recovers EXACTLY. Each case asserts the single-operand invariant too.
func TestSSHRunner_AdversarialArgvRoundTrip(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		argv []string // full argv: [name, args...]
	}{
		{"embedded_single_quote_apostrophe", []string{"echo", "it's"}},
		{"embedded_single_quotes_multiple", []string{"echo", "a'b'c"}},
		{"command_substitution_dollar_paren", []string{"echo", "$(touch /tmp/pwned)"}},
		{"command_substitution_backtick", []string{"echo", "`id`"}},
		{"semicolon", []string{"echo", ";"}},
		{"logical_and", []string{"echo", "&&"}},
		{"logical_or", []string{"echo", "||"}},
		{"pipe", []string{"echo", "|"}},
		{"redirect_out", []string{"echo", ">redirect"}},
		{"redirect_in", []string{"echo", "<in"}},
		{"glob_star", []string{"echo", "*"}},
		{"glob_question", []string{"echo", "?"}},
		{"tilde", []string{"echo", "~"}},
		{"param_expansion", []string{"echo", "${HOME}"}},
		{"token_starting_with_hash", []string{"echo", "#comment-bait"}},
		{"token_is_pane_id_format", []string{"tmux", "new-window", "-F", "#{pane_id}"}},
		{"leading_trailing_spaces", []string{"echo", "  pad  "}},
		{"embedded_newline", []string{"echo", "line1\nline2"}},
		{"embedded_tab", []string{"echo", "col1\tcol2"}},
		{"empty_string", []string{"echo", ""}},
		{"leading_dash", []string{"echo", "-leadingdash"}},
		{"env_assignment_with_spaces", []string{"env", "K=value with spaces"}},
		{"unicode", []string{"echo", "café—naïve—日本語—🚀"}},
		// A combined torture argv exercising several shapes at once.
		{"combined_torture", []string{
			"tmux", "new-window", "-P", "-F", "#{pane_id}", "-d",
			"-n", "win/name", "-c", "/path/with space/and'quote",
			"-e", "K=v $(id) `id` ; && | > <", "",
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			name := tc.argv[0]
			args := tc.argv[1:]

			cmd := SSHRunner{Host: "h"}.Command(ctx, name, args...)

			// Single-operand invariant: exactly one operand after `--`. This is
			// what makes OpenSSH's space-join of post-host operands a no-op, so
			// the remote shell sees precisely the string we built.
			operands := postDashDashOperands(t, cmd.Args)
			if len(operands) != 1 {
				t.Fatalf("hk-fxy9: expected exactly ONE operand after `--`, got %d: %q",
					len(operands), operands)
			}

			got := recoverArgvViaLoginShell(ctx, t, operands[0])

			if len(got) != len(tc.argv) {
				t.Fatalf("hk-fxy9: recovered %d tokens, want %d (quoting dropped/added tokens)\n got=%q\nwant=%q",
					len(got), len(tc.argv), got, tc.argv)
			}
			for i := range tc.argv {
				if got[i] != tc.argv[i] {
					t.Errorf("hk-fxy9: token %d round-trip mismatch\n got=%q\nwant=%q\n(full got=%q want=%q)",
						i, got[i], tc.argv[i], got, tc.argv)
				}
			}
		})
	}
}

// TestSSHRunner_NegativeControl_UnquotedBugIsCaught proves the harness has teeth:
// the OLD-style unquoted remote string (raw space-join of tokens) does NOT
// round-trip — the `#{pane_id}` token starts a `#` comment and truncates the
// command, so fewer tokens are recovered. If this ever round-tripped, the
// adversarial test above would be vacuous.
func TestSSHRunner_NegativeControl_UnquotedBugIsCaught(t *testing.T) {
	ctx := context.Background()

	argv := []string{"tmux", "new-window", "-P", "-F", "#{pane_id}", "-d", "-n", "agent"}

	// OLD (buggy) behavior: ship the raw tokens space-joined, UNQUOTED.
	oldOperand := strings.Join(argv, " ")

	got := recoverArgvViaLoginShell(ctx, t, oldOperand)

	if len(got) == len(argv) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the unquoted operand %q round-tripped to %q "+
			"(want truncation). The harness has no teeth — the adversarial test would be vacuous.",
			oldOperand, got)
	}
	// Concretely: the `#{pane_id}` and everything after it should be swallowed by
	// the `#` comment, so we recover only the tokens up to (not including)
	// `#{pane_id}`.
	wantTruncated := []string{"tmux", "new-window", "-P", "-F"}
	if len(got) != len(wantTruncated) {
		t.Logf("note: unquoted operand truncated to %q (expected ~%q); any truncation proves the point",
			got, wantTruncated)
	}
	for i := range got {
		if i < len(wantTruncated) && got[i] != wantTruncated[i] {
			t.Errorf("unquoted truncation token %d: got %q want %q", i, got[i], wantTruncated[i])
		}
	}
}

// TestSSHRunner_OptsStructure asserts the exact ssh argv shape: Opts appear
// BEFORE the host, UNQUOTED; the host is present; then `--`; then EXACTLY ONE
// trailing operand even when a remote token contains a space.
func TestSSHRunner_OptsStructure(t *testing.T) {
	ctx := context.Background()
	s := SSHRunner{
		Host: "100.87.151.114",
		Opts: []string{"-o", "ConnectTimeout=8", "-p", "2222"},
	}
	// A remote token with an interior space must NOT increase the operand count.
	cmd := s.Command(ctx, "tmux", "new-window", "-e", "K=value with spaces")

	want := []string{
		"ssh", "-o", "ConnectTimeout=8", "-p", "2222",
		"100.87.151.114", "--",
		`'tmux' 'new-window' '-e' 'K=value with spaces'`,
	}
	if len(cmd.Args) != len(want) {
		t.Fatalf("ssh argv length mismatch\n got=%q\nwant=%q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("ssh argv[%d] mismatch\n got=%q\nwant=%q\n(full got=%q)", i, cmd.Args[i], want[i], cmd.Args)
		}
	}

	// Independent of the literal-match above, re-assert the structural invariants
	// so a future Opts/host reordering fails loudly with a clear message.
	dashIdx := -1
	for i, a := range cmd.Args {
		if a == "--" {
			dashIdx = i
			break
		}
	}
	if dashIdx < 0 {
		t.Fatalf("no `--` in ssh argv: %q", cmd.Args)
	}
	// Opts unquoted and before host.
	if cmd.Args[1] != "-o" || cmd.Args[2] != "ConnectTimeout=8" || cmd.Args[3] != "-p" || cmd.Args[4] != "2222" {
		t.Errorf("Opts not verbatim-before-host (unquoted): %q", cmd.Args[1:5])
	}
	// Host immediately before `--`.
	if cmd.Args[dashIdx-1] != "100.87.151.114" {
		t.Errorf("host not immediately before `--`: %q", cmd.Args)
	}
	// Exactly one operand after `--` despite the space-containing token.
	if got := len(cmd.Args) - (dashIdx + 1); got != 1 {
		t.Errorf("hk-fxy9: expected exactly ONE operand after `--`, got %d: %q", got, cmd.Args[dashIdx+1:])
	}
}

// TestSSHRunner_StdinUntouched asserts SSHRunner.Command leaves Stdin nil so the
// caller can attach it afterward (the daemon's `tmux load-buffer -` payload path
// relies on this).
func TestSSHRunner_StdinUntouched(t *testing.T) {
	cmd := SSHRunner{Host: "h"}.Command(context.Background(), "tmux", "load-buffer", "-")
	if cmd.Stdin != nil {
		t.Fatalf("expected Stdin nil so caller can attach the load-buffer payload, got %v", cmd.Stdin)
	}
}
