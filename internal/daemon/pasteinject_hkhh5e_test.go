package daemon_test

// pasteinject_hkhh5e_test.go — unit tests for the remote-substrate paste-inject
// fix (hk-hh5e).
//
// Root cause: statTaskFile used os.Stat (box-A-local), so for remote runs where
// the worktree lives on an SSH worker the stat always failed, causing the
// paste-inject to skip before any bytes were sent.  statTaskFileVia routes the
// stat through the per-run CommandRunner when the runner is non-nil (remote),
// and falls through to os.Stat when the runner is nil (local, NFR7).
//
// External tests (this file) cover statTaskFileVia in isolation via the
// exported wrapper ExportedStatTaskFileVia.  The end-to-end pasteInjectOnLaunch
// test is in pasteinject_hkhh5e_internal_test.go (package daemon) because
// commandRunnerProvider has unexported methods — external types cannot satisfy it.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// hkhh5eRunner — recording CommandRunner for these tests
// ─────────────────────────────────────────────────────────────────────────────

// hkhh5eRunner is a tmux.CommandRunner that records calls and returns a
// controlled *exec.Cmd without needing a real SSH connection.
type hkhh5eRunner struct {
	mu  sync.Mutex
	got []tmux.RecordingCall

	// cmdFn, when non-nil, produces the *exec.Cmd.  When nil, defaults to `true`
	// (always exits 0).
	cmdFn func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (r *hkhh5eRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cp := make([]string, len(args))
	copy(cp, args)
	r.mu.Lock()
	r.got = append(r.got, tmux.RecordingCall{Name: name, Args: cp})
	r.mu.Unlock()
	if r.cmdFn != nil {
		return r.cmdFn(ctx, name, args...)
	}
	return exec.CommandContext(ctx, "true")
}

func (r *hkhh5eRunner) calls() []tmux.RecordingCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]tmux.RecordingCall, len(r.got))
	copy(out, r.got)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — ExportedStatTaskFileVia
// ─────────────────────────────────────────────────────────────────────────────

// TestStatTaskFileVia_RemoteRunner_RecordsStatCall verifies that a non-nil
// runner receives a "stat <path>" call and nil is returned when it exits 0.
func TestStatTaskFileVia_RemoteRunner_RecordsStatCall(t *testing.T) {
	runner := &hkhh5eRunner{} // default cmdFn → `true`, exits 0
	const fakePath = "/remote/worker/.harmonik/agent-task.md"

	if err := daemon.ExportedStatTaskFileVia(context.Background(), runner, fakePath); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	calls := runner.calls()
	if len(calls) == 0 {
		t.Fatal("runner received no Command() calls; stat was not routed via runner")
	}
	c := calls[0]
	if c.Name != "stat" {
		t.Errorf("first runner call: name = %q, want %q", c.Name, "stat")
	}
	if len(c.Args) < 1 || c.Args[0] != fakePath {
		t.Errorf("first runner call: args = %v, want [%q]", c.Args, fakePath)
	}
}

// TestStatTaskFileVia_RemoteRunner_PropagatesFailure verifies that a non-nil
// runner that exits non-zero causes statTaskFileVia to return a non-nil error
// mentioning the path.
func TestStatTaskFileVia_RemoteRunner_PropagatesFailure(t *testing.T) {
	runner := &hkhh5eRunner{
		cmdFn: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false") // always exits 1
		},
	}
	const fakePath = "/remote/worker/.harmonik/agent-task.md"

	err := daemon.ExportedStatTaskFileVia(context.Background(), runner, fakePath)
	if err == nil {
		t.Fatal("expected non-nil error when runner exits non-zero")
	}
	if !strings.Contains(err.Error(), fakePath) {
		t.Errorf("error %q does not mention path %q", err.Error(), fakePath)
	}
}

// TestStatTaskFileVia_NilRunner_LocalStat verifies NFR7: a nil runner falls
// back to os.Stat and returns nil when the file exists locally.
func TestStatTaskFileVia_NilRunner_LocalStat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".harmonik", "agent-task.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("# Task\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := daemon.ExportedStatTaskFileVia(context.Background(), nil, p); err != nil {
		t.Errorf("nil runner + existing local file: expected nil error, got: %v", err)
	}
}

// TestStatTaskFileVia_NilRunner_LocalMissing verifies that a nil runner falls
// back to os.Stat and returns an error wrapping tmux.ErrStructural when the
// file is absent.
func TestStatTaskFileVia_NilRunner_LocalMissing(t *testing.T) {
	const absent = "/tmp/hkhh5e-definitely-does-not-exist/agent-task.md"
	err := daemon.ExportedStatTaskFileVia(context.Background(), nil, absent)
	if err == nil {
		t.Fatal("nil runner + absent local file: expected error, got nil")
	}
	if !errors.Is(err, tmux.ErrStructural) {
		t.Errorf("expected errors.Is(err, tmux.ErrStructural); err = %v", err)
	}
}
