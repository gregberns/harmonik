package handler_test

// launch_runner_m4c4_test.go — remote-substrate M4-C4 (T6): handler.Launch's
// direct exec path (Substrate == nil) must build the subprocess *exec.Cmd through
// LaunchSpec.Runner when one is set, so a worker-selected argv-driven harness
// (pi/codex) spawns its process ON THE WORKER via the SSHRunner. A nil Runner
// keeps the byte-identical local exec.CommandContext path (NFR7).

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// recordingRunner is a handler.CommandRunner that records the (name, args) of each
// Command call and produces a benign local process that emits agent_ready so the
// watcher exits cleanly.
type recordingRunner struct {
	mu       sync.Mutex
	called   bool
	lastName string
	lastArgs []string
}

func (r *recordingRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	r.called = true
	r.lastName = name
	r.lastArgs = append([]string(nil), args...)
	r.mu.Unlock()
	// Stand in for the remote spawn: emit a terminal event so the watcher closes.
	return exec.CommandContext(ctx, "sh", "-c", `printf '{"type":"agent_ready"}\n'`)
}

// TestHandler_Launch_ExecPath_RoutesThroughRunner verifies the exec path calls
// LaunchSpec.Runner.Command with the launch binary + args instead of spawning
// locally via exec.CommandContext.
func TestHandler_Launch_ExecPath_RoutesThroughRunner(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	rr := &recordingRunner{}
	spec := handler.LaunchSpec{
		Binary:  "pi",
		Args:    []string{"--mode", "json", "task text"},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "implementer",
		Runner:  rr, // worker-selected run: spawn via the runner (SSHRunner in prod)
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close before test context cancelled")
	}
	if err := sess.Wait(t.Context()); err != nil {
		t.Logf("sess.Wait returned (non-fatal for this test): %v", err)
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()
	if !rr.called {
		t.Fatal("LaunchSpec.Runner.Command was NOT called — the exec path spawned " +
			"locally instead of through the runner; a worker-selected pi/codex run " +
			"would run on box A instead of the worker (M4-C4)")
	}
	if rr.lastName != "pi" {
		t.Errorf("Runner.Command name = %q; want the launch binary %q", rr.lastName, "pi")
	}
	if got := strings.Join(rr.lastArgs, " "); got != "--mode json task text" {
		t.Errorf("Runner.Command args = %q; want the launch argv %q", got, "--mode json task text")
	}
}

// TestHandler_Launch_ExecPath_NilRunnerSpawnsLocally verifies NFR7: a nil Runner
// leaves the exec.CommandContext local path intact (the subprocess still runs and
// the watcher completes).
func TestHandler_Launch_ExecPath_NilRunnerSpawnsLocally(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	spec := handler.LaunchSpec{
		Binary:  "sh",
		Args:    []string{"-c", `printf '{"type":"agent_ready"}\n'`},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "implementer",
		// Runner intentionally nil — LOCAL byte-identical path.
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch (nil runner): %v", err)
	}
	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close (nil-runner local path broken)")
	}
	if err := sess.Wait(t.Context()); err != nil {
		t.Logf("sess.Wait returned (non-fatal for this test): %v", err)
	}
}
