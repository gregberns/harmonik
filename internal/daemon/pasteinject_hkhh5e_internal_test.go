package daemon

// pasteinject_hkhh5e_internal_test.go — end-to-end test for the remote paste-inject
// fix (hk-hh5e), placed in package daemon so it can satisfy commandRunnerProvider
// (unexported interface) via a local stub.
//
// Test: pasteInjectOnLaunch with a substrate that reports a remote runner routes
// the statTaskFile probe through the runner (not os.Stat) and still delivers
// the paste payload via WriteLastPane.

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkhh5eInternalRunner records Command() calls; by default returns `true` (exit 0).
type hkhh5eInternalRunner struct {
	mu    sync.Mutex
	got   []tmux.RecordingCall
	cmdFn func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (r *hkhh5eInternalRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
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

func (r *hkhh5eInternalRunner) calls() []tmux.RecordingCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]tmux.RecordingCall, len(r.got))
	copy(out, r.got)
	return out
}

// hkhh5eInternalSubstrate implements handler.Substrate + pasteInjecter +
// enterSender + commandRunnerProvider.  All unexported interface methods are
// accessible here because this file is in package daemon.
type hkhh5eInternalSubstrate struct {
	runner tmux.CommandRunner

	mu         sync.Mutex
	writeCalls []struct{ buf, payload string }
	enterCount int
}

func (s *hkhh5eInternalSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return &hkhh5eInternalSession{}, nil
}

// WriteLastPane implements pasteInjecter.
func (s *hkhh5eInternalSubstrate) WriteLastPane(_ context.Context, buf string, payload []byte) error {
	s.mu.Lock()
	s.writeCalls = append(s.writeCalls, struct{ buf, payload string }{buf, string(payload)})
	s.mu.Unlock()
	return nil
}

// SendEnterToLastPane implements enterSender.
func (s *hkhh5eInternalSubstrate) SendEnterToLastPane(_ context.Context) error {
	s.mu.Lock()
	s.enterCount++
	s.mu.Unlock()
	return nil
}

// commandRunner implements commandRunnerProvider.
func (s *hkhh5eInternalSubstrate) commandRunner() tmux.CommandRunner { return s.runner }

func (s *hkhh5eInternalSubstrate) writes() []struct{ buf, payload string } {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]struct{ buf, payload string }, len(s.writeCalls))
	copy(out, s.writeCalls)
	return out
}

// hkhh5eInternalSession is a minimal handler.SubstrateSession stub.
type hkhh5eInternalSession struct{}

func (*hkhh5eInternalSession) Kill(_ context.Context) error { return nil }
func (*hkhh5eInternalSession) Wait(_ context.Context) error { return nil }
func (*hkhh5eInternalSession) Outcome() handler.Outcome     { return handler.Outcome{} }
func (*hkhh5eInternalSession) PID() int                     { return 0 }
func (*hkhh5eInternalSession) Stdout() io.Reader            { return nil }

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectOnLaunch_RemoteRunner_RoutesStatViaRunner verifies that when
// a substrate reports a non-local CommandRunner:
//
//  1. The stat probe is issued to the runner (not os.Stat).
//  2. WriteLastPane is called with a payload that mentions "agent-task.md".
//
// The agent-task.md file does NOT exist on box A; only the mocked runner
// returns success for stat, confirming stat was routed remotely.
func TestPasteInjectOnLaunch_RemoteRunner_RoutesStatViaRunner(t *testing.T) {
	// Shrink splash dismiss delay to keep the test fast.
	old := splashDismissDelayNs.Load()
	splashDismissDelayNs.Store(1)
	t.Cleanup(func() { splashDismissDelayNs.Store(old) })

	runner := &hkhh5eInternalRunner{} // exits 0 for any command
	wtPath := t.TempDir()             // agent-task.md intentionally absent
	sub := &hkhh5eInternalSubstrate{runner: runner}

	const sessionID = "hkhh5e-internal-sess"
	briefDelivered := pasteInjectOnLaunch(
		context.Background(), sub, sessionID,
		handlercontract.ReviewLoopPhase(""), // empty = implementer-initial
		1, wtPath, nil, core.RunID{},
	)
	<-briefDelivered

	// Assert: runner received a stat call for agent-task.md.
	var foundStat bool
	for _, c := range runner.calls() {
		if c.Name == "stat" && len(c.Args) > 0 && strings.HasSuffix(c.Args[0], "agent-task.md") {
			foundStat = true
			break
		}
	}
	if !foundStat {
		t.Errorf("runner calls = %v; expected a stat call for agent-task.md", runner.calls())
	}

	// Assert: WriteLastPane was called with a task payload.
	writes := sub.writes()
	if len(writes) == 0 {
		t.Fatal("WriteLastPane was never called; paste was not sent")
	}
	if !strings.Contains(writes[0].payload, "agent-task.md") {
		t.Errorf("WriteLastPane payload = %q; want mention of agent-task.md", writes[0].payload)
	}
}
