package tmux

// runner_real_argv_hkfxy9_test.go — the companion to runner_ssh_quote_hkfxy9_test.go.
//
// That test proves the quoting idiom round-trips a HAND-WRITTEN token list. This
// one proves the ACTUAL tmux argv shapes the daemon's remote-spawn path issues
// survive the ssh → remote-login-shell round-trip — the shapes built by
// buildNewWindowArgs / buildNewSessionArgs (osadapter.go), the display-message /
// list-* / kill-* / send-keys / load-buffer / paste-buffer commands
// (osadapter.go + tmuxsubstrate.go), with their REAL `#{...}` format strings and
// the control-byte payloads (bracketed-paste ESC sequences, `#`, `$`, backtick,
// literal newline) that are the precise bytes the hk-fxy9/hk-538l bug truncated.
//
// The bug (hk-fxy9/hk-538l): SSHRunner shipped the tmux argv UNQUOTED, OpenSSH
// space-joined the operands, and the worker's LOGIN SHELL re-parsed them — so a
// token like `-F #{pane_id}` made the remote shell treat `#{pane_id}` as a `#`
// comment and truncate `tmux new-window`, so claude never launched on the worker
// → agent_ready_timeout. The fix single-quotes every token.
//
// Source map for the argv shapes asserted here (line numbers are at authorship time):
//   - new-window   -P -F #{pane_id} -d -t <sess>: -n <win> -c <cwd> -e K=V -- <cmd>
//       internal/lifecycle/tmux/osadapter.go buildNewWindowArgs (~L566-584)
//   - new-session  -P -F #{pane_id} -d -s <sess> -n <win> -c <cwd> -e K=V -- <cmd>
//       internal/lifecycle/tmux/osadapter.go buildNewSessionArgs (~L348-366)
//   - list-sessions  -F #{session_name}                osadapter.go ListSessions (~L90)
//   - list-windows   -t <sess> -F #{window_name}       osadapter.go ListWindows (~L106)
//   - display-message -p -t <handle> #{pane_pid}       osadapter.go WindowPanePID (~L211)
//   - display-message -p -t <handle> #{pane_id}        osadapter.go WindowPaneID (~L242)
//   - display-message -t <target> -p "#{history_size} #{cursor_y}"
//       internal/daemon/tmuxsubstrate.go PaneOutputDims probe (~L1787-1788)
//   - kill-window   -t <target>                        osadapter.go KillWindow (~L181)
//   - kill-session  -t <sess>                          osadapter.go KillSession (~L261)
//   - load-buffer   -b <buf> -                         osadapter.go LoadBuffer (~L385)
//   - paste-buffer  -b <buf> -t <pane> -d              osadapter.go PasteBuffer (~L411)
//   - send-keys -l  -t <pane> <literal>                osadapter.go SendKeysLiteral (~L443)
//   - send-keys     -t <pane> Enter                    osadapter.go SendKeysEnter (~L466)
//   - send-keys     -t <pane> /quit Enter              osadapter.go SendKeysQuit (~L488)

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// recoverRemoteArgv runs the single post-`--` operand SSHRunner built through a
// real /bin/sh exactly as the worker's login shell would, recovering the argv it
// reconstructs. The shell runs `printf '%s\0' <operand>`: printf emits each argv
// word it receives NUL-terminated, so spaces, `#`, ESC bytes, and newlines inside
// a word are unambiguous (NUL is the one byte a shell word cannot contain).
//
// It returns the recovered argv (the tmux subcommand vector, including "tmux").
func recoverRemoteArgv(t *testing.T, remoteOperand string) []string {
	t.Helper()
	script := "printf '%s\\0' " + remoteOperand
	out, err := exec.CommandContext(context.Background(), "/bin/sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("/bin/sh -c failed for operand %q: %v", remoteOperand, err)
	}
	// printf wrote one trailing NUL after the last word; split on NUL and drop the
	// final empty element.
	parts := strings.Split(string(out), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// remoteOperand returns the single shell operand SSHRunner places after `--`.
func remoteOperand(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := SSHRunner{Host: "h"}.Command(context.Background(), name, args...)
	a := cmd.Args
	if len(a) < 4 || a[0] != "ssh" {
		t.Fatalf("expected `ssh ... -- <operand>`, got %q", a)
	}
	if a[len(a)-2] != "--" {
		t.Fatalf("expected `--` immediately before the single remote operand, got %q", a)
	}
	return a[len(a)-1]
}

// assertArgvRoundTrip builds argv via SSHRunner, recovers it through /bin/sh, and
// asserts the recovered vector EXACTLY equals ["tmux", args...].
func assertArgvRoundTrip(t *testing.T, label string, args []string) {
	t.Helper()
	want := append([]string{"tmux"}, args...)
	operand := remoteOperand(t, "tmux", args...)
	got := recoverRemoteArgv(t, operand)
	if len(got) != len(want) {
		t.Fatalf("%s: recovered %d tokens, want %d (quoting truncated the command?)\n operand=%q\n got=%q\nwant=%q",
			label, len(got), len(want), operand, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: token %d: got %q want %q\n operand=%q", label, i, got[i], want[i], operand)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A. REAL-SHAPES ROUND-TRIP
// ─────────────────────────────────────────────────────────────────────────────

// TestRealArgv_RoundTripsThroughRemoteShell drives every concrete tmux argv shape
// the daemon's remote-spawn path constructs through SSHRunner → real /bin/sh and
// asserts byte-exact recovery. Each shape is built from the SAME helpers the
// production OSAdapter / tmuxSubstrate call (buildNewWindowArgs etc.), so a drift
// in the production format strings would surface here.
func TestRealArgv_RoundTripsThroughRemoteShell(t *testing.T) {
	t.Parallel()

	const (
		sess    = "harmonik-3f9a1c-default"
		runID   = "019ee7d6-4b22-7a10-9c3e-aa11bb22cc33"
		workDir = "/Users/gb/harmonik-worker/repo/.harmonik/worktrees/" + runID
		// A window name with a slash — the hk-aievp filesystem-path-as-window-name case.
		winSlash = "hk-3f9a1c-bead/i1"
		paneID   = "%1964"
		handle   = sess + ":" + winSlash
	)

	// ── new-window: the exact spawn shape, via the production builder. Carries
	// -F #{pane_id} (the truncation trigger), a -c worktree path with the run-id,
	// a slash window name, and an -e OAUTH-token env operand. This is THE bug shape.
	nwParams := NewWindowIn{
		Session:    sess,
		WindowName: winSlash,
		WorkDir:    workDir,
		Env: []string{
			"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-AbC_def-123.456",
			"HARMONIK_PHASE=implementer-initial",
			"HARMONIK_RUN_ID=" + runID,
		},
		Command: "claude --dangerously-skip-permissions --session-id " + runID,
	}
	assertArgvRoundTrip(t, "new-window", buildNewWindowArgs(nwParams))

	// ── new-session: crew/worker session-create shape (same #{pane_id} + -e + -c).
	nsParams := NewWindowIn{
		Session:    "harmonik-worker-7c1d",
		WindowName: winSlash,
		WorkDir:    workDir,
		Env:        []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-ZZ"},
		Command:    "claude --dangerously-skip-permissions",
	}
	assertArgvRoundTrip(t, "new-session", buildNewSessionArgs(nsParams))

	// ── list-sessions -F #{session_name}  (osadapter.go ListSessions)
	assertArgvRoundTrip(t, "list-sessions", []string{"list-sessions", "-F", "#{session_name}"})

	// ── list-windows -t <sess> -F #{window_name}  (osadapter.go ListWindows)
	assertArgvRoundTrip(t, "list-windows", []string{"list-windows", "-t", sess, "-F", "#{window_name}"})

	// ── display-message -p -t <handle> #{pane_pid}  (osadapter.go WindowPanePID)
	assertArgvRoundTrip(t, "display-message pane_pid",
		[]string{"display-message", "-p", "-t", handle, "#{pane_pid}"})

	// ── display-message -p -t <handle> #{pane_id}  (osadapter.go WindowPaneID)
	assertArgvRoundTrip(t, "display-message pane_id",
		[]string{"display-message", "-p", "-t", handle, "#{pane_id}"})

	// ── display-message -t <target> -p "#{history_size} #{cursor_y}"
	//    (tmuxsubstrate.go PaneOutputDims probe). The format string is ONE arg
	//    with an embedded space AND two #{...} expansions — exactly the kind of
	//    operand an unquoted ssh round-trip would both re-split and #-truncate.
	assertArgvRoundTrip(t, "display-message dims",
		[]string{"display-message", "-t", paneID, "-p", "#{history_size} #{cursor_y}"})

	// ── kill-window -t <target>  (osadapter.go KillWindow)
	assertArgvRoundTrip(t, "kill-window", []string{"kill-window", "-t", handle})

	// ── kill-session -t <sess>  (osadapter.go KillSession)
	assertArgvRoundTrip(t, "kill-session", []string{"kill-session", "-t", sess})

	// ── load-buffer -b <buf> -  (osadapter.go LoadBuffer)
	assertArgvRoundTrip(t, "load-buffer",
		[]string{"load-buffer", "-b", "harmonik-3f9a1c-kickoff", "-"})

	// ── paste-buffer -b <buf> -t <pane> -d  (osadapter.go PasteBuffer)
	assertArgvRoundTrip(t, "paste-buffer",
		[]string{"paste-buffer", "-b", "harmonik-3f9a1c-kickoff", "-t", paneID, "-d"})

	// ── send-keys -t <pane> Enter  (osadapter.go SendKeysEnter)
	assertArgvRoundTrip(t, "send-keys Enter",
		[]string{"send-keys", "-t", paneID, "Enter"})

	// ── send-keys -t <pane> /quit Enter  (osadapter.go SendKeysQuit)
	assertArgvRoundTrip(t, "send-keys quit",
		[]string{"send-keys", "-t", paneID, "/quit", "Enter"})

	// ── send-keys -l -t <pane> <literal-with-control-bytes>  (osadapter.go SendKeysLiteral)
	//    THE POINT: a literal payload carrying bracketed-paste ESC sequences
	//    (\x1b[200~ … \x1b[201~), a `#` (comment-trigger), `$`, backtick, and a
	//    literal newline. Every one of these is a byte the unquoted bug mangled.
	literalPayload := "\x1b[200~" +
		"line one #notacomment $HOME `whoami`\n" +
		"line two with 'single' and \"double\" quotes" +
		"\x1b[201~"
	assertArgvRoundTrip(t, "send-keys literal ESC+control",
		[]string{"send-keys", "-l", "-t", paneID, literalPayload})

	// ── Standalone hostile-payload word: the #/$/backtick/newline quartet as ONE
	//    argv word, to isolate that each survives without help from neighbours.
	hostile := "#hash $dollar `backtick` \n newline-in-word"
	assertArgvRoundTrip(t, "hostile single word",
		[]string{"send-keys", "-l", "-t", paneID, hostile})
}

// ─────────────────────────────────────────────────────────────────────────────
// B. LOAD-BUFFER STDIN PATH
// ─────────────────────────────────────────────────────────────────────────────

// TestLoadBuffer_StdinPathOrthogonalToArgvQuoting proves the daemon's payload
// path — `tmux load-buffer -` with bytes on STDIN — composes with SSH quoting:
//   - SSHRunner leaves cmd.Stdin nil so the caller can attach a reader (the daemon
//     attaches bytes.NewReader(payload) in OSAdapter.LoadBuffer).
//   - the remote operand is exactly `'tmux' 'load-buffer' '-'`.
//   - attaching a reader with shell-nasty bytes does NOT change the argv operand:
//     stdin is a separate channel from the quoted command string.
func TestLoadBuffer_StdinPathOrthogonalToArgvQuoting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cmd := SSHRunner{Host: "h"}.Command(ctx, "tmux", "load-buffer", "-")

	// (1) Stdin is nil right after construction — caller owns it.
	if cmd.Stdin != nil {
		t.Fatalf("SSHRunner.Command left cmd.Stdin non-nil (%T); caller must be free to attach the payload reader", cmd.Stdin)
	}

	// (2) The remote operand is the fully-quoted load-buffer shape, and it
	//     round-trips to exactly ["tmux", "load-buffer", "-"].
	operand := cmd.Args[len(cmd.Args)-1]
	const wantOperand = `'tmux' 'load-buffer' '-'`
	if operand != wantOperand {
		t.Fatalf("load-buffer remote operand:\n got=%q\nwant=%q", operand, wantOperand)
	}
	if got := recoverRemoteArgv(t, operand); len(got) != 3 ||
		got[0] != "tmux" || got[1] != "load-buffer" || got[2] != "-" {
		t.Fatalf("load-buffer operand did not round-trip: got %q", got)
	}

	// (3) Attach a payload reader containing shell-nasty bytes (the bracketed-paste
	//     wrapper + #/$/backtick/newline) and confirm the ssh argv is UNCHANGED.
	//     The payload travels on stdin; it is orthogonal to argv quoting. We do not
	//     run ssh — only assert the command shape.
	var payload io.Reader = bytes.NewReader([]byte(
		"\x1b[200~kick-off: fix #hk-fxy9; run `go test`; cost $0\n\x1b[201~"))
	cmd.Stdin = payload

	if cmd.Stdin == nil {
		t.Fatal("after attaching, cmd.Stdin is nil")
	}
	// argv unchanged: still `ssh h -- 'tmux' 'load-buffer' '-'`.
	a := cmd.Args
	if a[0] != "ssh" || a[len(a)-2] != "--" || a[len(a)-1] != wantOperand {
		t.Fatalf("attaching a stdin reader changed the ssh argv: %q", a)
	}
}
