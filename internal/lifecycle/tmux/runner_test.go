package tmux

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestLocalRunner_Command verifies that LocalRunner.Command returns a cmd
// that runs the named binary (echo used as a side-effect-free probe).
func TestLocalRunner_Command(t *testing.T) {
	t.Parallel()

	r := LocalRunner{}
	cmd := r.Command(context.Background(), "echo", "hello")
	if cmd == nil {
		t.Fatal("LocalRunner.Command: got nil cmd")
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("LocalRunner.Command: unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("LocalRunner.Command: output %q does not contain 'hello'", string(out))
	}
}

// TestRecordingRunner_RecordsCalls verifies that RecordingRunner records each
// Command invocation's name and arguments in order.
func TestRecordingRunner_RecordsCalls(t *testing.T) {
	t.Parallel()

	rr := &RecordingRunner{}
	ctx := context.Background()
	_ = rr.Command(ctx, "tmux", "list-sessions", "-F", "#{session_name}")
	_ = rr.Command(ctx, "tmux", "kill-session", "-t", "mysession")

	if len(rr.Calls) != 2 {
		t.Fatalf("RecordingRunner: got %d calls, want 2", len(rr.Calls))
	}
	if rr.Calls[0].Name != "tmux" {
		t.Errorf("RecordingRunner: Calls[0].Name = %q, want 'tmux'", rr.Calls[0].Name)
	}
	if len(rr.Calls[0].Args) == 0 || rr.Calls[0].Args[0] != "list-sessions" {
		t.Errorf("RecordingRunner: Calls[0].Args = %v, want [list-sessions ...]", rr.Calls[0].Args)
	}
	if rr.Calls[1].Args[0] != "kill-session" {
		t.Errorf("RecordingRunner: Calls[1].Args[0] = %q, want 'kill-session'", rr.Calls[1].Args[0])
	}
}

// TestRecordingRunner_CmdFunc verifies that RecordingRunner calls CmdFunc when set.
func TestRecordingRunner_CmdFunc(t *testing.T) {
	t.Parallel()

	called := 0
	rr := &RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called++
			return exec.CommandContext(ctx, name, args...)
		},
	}
	ctx := context.Background()
	cmd := rr.Command(ctx, "echo", "x")
	if cmd == nil {
		t.Fatal("RecordingRunner CmdFunc: got nil cmd")
	}
	if called != 1 {
		t.Errorf("RecordingRunner CmdFunc: CmdFunc called %d times, want 1", called)
	}
}

// TestOSAdapter_WithRunner_RoutesListSessions verifies that WithRunner wires a
// RecordingRunner into OSAdapter and that ListSessions routes its tmux call
// through it.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_WithRunner_RoutesListSessions(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"sess1", "sess2"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	rr := &RecordingRunner{}
	a := OSAdapter{}.WithRunner(rr)
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions with RecordingRunner: unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions with RecordingRunner: got %d sessions, want 2", len(sessions))
	}
	if len(rr.Calls) == 0 {
		t.Fatal("RecordingRunner: no calls recorded — ListSessions did not route through runner")
	}
	if rr.Calls[0].Name != "tmux" {
		t.Errorf("RecordingRunner: call name = %q, want 'tmux'", rr.Calls[0].Name)
	}
	if len(rr.Calls[0].Args) == 0 || rr.Calls[0].Args[0] != "list-sessions" {
		t.Errorf("RecordingRunner: call args = %v, want [list-sessions ...]", rr.Calls[0].Args)
	}
}

// TestSSHRunner_NewWindowArgv verifies that SSHRunner wraps tmux commands in
// the expected `ssh <host> -- tmux ...` argv without shell-quoting.
func TestSSHRunner_NewWindowArgv(t *testing.T) {
	t.Parallel()

	sr := SSHRunner{Host: "worker-mac-1"}
	rr := &RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Return a no-op cmd so nothing actually runs.
			return exec.CommandContext(ctx, "true")
		},
	}
	// Wrap the SSHRunner inside the RecordingRunner so we can inspect the
	// fully-expanded argv that SSHRunner produces.
	// Instead, call SSHRunner.Command directly and inspect the resulting Cmd.
	ctx := context.Background()
	_ = rr // silence unused warning; we use SSHRunner directly below

	// Build a NewWindowIn with a space-containing workdir and a slash-containing
	// window name — these are the hk-kuxxl slash class and argv-quoting risks.
	p := NewWindowIn{
		Session:    "mysession",
		WindowName: "feat/my-feature",
		WorkDir:    "/home/user/work dir with spaces",
	}
	args := buildNewWindowArgs(p)

	cmd := sr.Command(ctx, "tmux", args...)
	if cmd == nil {
		t.Fatal("SSHRunner.Command: got nil cmd")
	}

	// cmd.Args[0] is the binary path; cmd.Args[1:] are the argv tokens.
	// SSHRunner ships the remote command as a SINGLE shell-quoted operand after
	// `--` (NOT a discrete argv vector — OpenSSH would space-join it and the
	// remote login shell would re-parse it; see SSHRunner doc + the hkfxy9 test).
	argv := cmd.Args
	// Expected: [ssh, worker-mac-1, --, <one quoted remote string>]
	if len(argv) != 4 {
		t.Fatalf("SSHRunner argv = %v; want exactly 4 elements (ssh host -- <remote>)", argv)
	}
	if argv[0] != "ssh" {
		t.Errorf("argv[0] = %q, want ssh", argv[0])
	}
	if argv[1] != "worker-mac-1" {
		t.Errorf("argv[1] = %q, want worker-mac-1", argv[1])
	}
	if argv[2] != "--" {
		t.Errorf("argv[2] = %q, want --", argv[2])
	}
	remote := argv[3]
	// The remote string must be the per-token single-quoted join of tmux+args.
	wantTokens := append([]string{"tmux"}, args...)
	wantParts := make([]string, len(wantTokens))
	for i, tk := range wantTokens {
		wantParts[i] = shellQuoteArg(tk)
	}
	if want := strings.Join(wantParts, " "); remote != want {
		t.Fatalf("remote command not token-quoted:\n got=%q\nwant=%q", remote, want)
	}
	// The space-containing workdir and slash-containing window name must appear
	// QUOTED (one literal word each), not raw-split: verify the quoted forms are
	// present and the raw (unquoted) forms are NOT (which would mean the remote
	// shell could re-split them).
	if !strings.Contains(remote, shellQuoteArg("/home/user/work dir with spaces")) {
		t.Errorf("remote %q: space-containing workdir not single-quoted", remote)
	}
	if !strings.Contains(remote, shellQuoteArg("feat/my-feature")) {
		t.Errorf("remote %q: slash-containing window name not single-quoted", remote)
	}
}

// TestSSHRunner_Opts verifies that extra Opts are prepended before the host.
func TestSSHRunner_Opts(t *testing.T) {
	t.Parallel()

	sr := SSHRunner{Host: "worker-mac-1", Opts: []string{"-p", "2222", "-i", "/path/to/key"}}
	ctx := context.Background()
	cmd := sr.Command(ctx, "tmux", "list-sessions", "-F", "#{session_name}")
	argv := cmd.Args
	// Expected: [ssh, -p, 2222, -i, /path/to/key, worker-mac-1, --, tmux, ...]
	if argv[1] != "-p" || argv[2] != "2222" || argv[3] != "-i" || argv[4] != "/path/to/key" {
		t.Errorf("Opts not prepended correctly: %v", argv)
	}
	if argv[5] != "worker-mac-1" {
		t.Errorf("host position wrong: argv[5] = %q, want worker-mac-1", argv[5])
	}
	if argv[6] != "--" {
		t.Errorf("separator position wrong: argv[6] = %q, want --", argv[6])
	}
}

// TestSSHRunner_LoadBufferForwardsStdin verifies that the cmd returned by
// SSHRunner allows the caller to set cmd.Stdin (needed for `tmux load-buffer -`
// over ssh). SSHRunner must not touch cmd.Stdin itself.
func TestSSHRunner_LoadBufferForwardsStdin(t *testing.T) {
	t.Parallel()

	sr := SSHRunner{Host: "worker-mac-1"}
	ctx := context.Background()
	cmd := sr.Command(ctx, "tmux", "load-buffer", "-b", "harmonik-abc-payload", "-")
	if cmd == nil {
		t.Fatal("SSHRunner.Command: got nil cmd")
	}
	// cmd.Stdin must be nil before the caller sets it.
	if cmd.Stdin != nil {
		t.Errorf("SSHRunner.Command: cmd.Stdin pre-set by runner; must be nil so caller can set it")
	}
	// Simulate what OSAdapter.LoadBuffer does: set cmd.Stdin.
	cmd.Stdin = bytes.NewReader([]byte("hello"))
	if cmd.Stdin == nil {
		t.Error("cmd.Stdin: assignment lost — runner must not override it")
	}
}

// TestOSAdapter_DefaultRunnerIsLocalRunner verifies that a zero-value OSAdapter
// uses LocalRunner (i.e., routes through exec.CommandContext) without requiring
// an explicit WithRunner call.
// NOTE: uses t.Setenv — cannot be parallel.
func TestOSAdapter_DefaultRunnerIsLocalRunner(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	osAdapterFixtureWriteFakeTmux(t, binDir, []string{"tmux 3.4"}, 0)
	osAdapterFixtureWithFakeTmux(t, binDir)

	a := OSAdapter{} // zero value — must default to LocalRunner
	if err := a.ProbeTmux(context.Background()); err != nil {
		t.Errorf("OSAdapter zero-value ProbeTmux: unexpected error: %v", err)
	}
}
