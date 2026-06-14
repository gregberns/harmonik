package tmux

import (
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
