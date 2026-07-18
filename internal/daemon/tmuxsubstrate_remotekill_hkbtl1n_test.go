package daemon

// tmuxsubstrate_remotekill_hkbtl1n_test.go — regression for the remote-Kill
// forceful-termination asymmetry (hk-btl1n).
//
// Bug: the local Kill path SIGTERM/SIGKILLs the pane PID (killProcessWithGrace)
// because tmux kill-window signal propagation is unreliable. The remote Kill
// branch did only SendKeysQuit + KillWindow over SSH — no equivalent forceful
// kill of the WORKER pane PID. A remote agent surviving the pane SIGHUP from
// KillWindow could leak on the worker.
//
// Fix (hk-btl1n): the remote Kill branch now runs a TERM→grace→KILL sequence
// against the worker pane PID over the run's SSH runner (killRemoteProcessWithGrace),
// the remote analog of the local path. s.pid is NEVER local-signalled (it names
// the worker's process table, not the daemon host's — hk-r1zq/H8).

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// recordingKillRunner records every Command invocation and returns a harmless
// no-op ("true") so cmd.Run() succeeds without touching any real process.
type recordingKillRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingKillRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mu.Unlock()
	return exec.CommandContext(ctx, "true")
}

func (r *recordingKillRunner) callsCopy() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// recordingKillAdapter records SendKeysQuit + KillWindow; all other methods are
// the noop stub (defined in export_test.go, same package).
type recordingKillAdapter struct {
	noopTmuxAdapter
	mu           sync.Mutex
	sendKeysQuit int
	killWindow   int
}

func (a *recordingKillAdapter) SendKeysQuit(_ context.Context, _ string) error {
	a.mu.Lock()
	a.sendKeysQuit++
	a.mu.Unlock()
	return nil
}

func (a *recordingKillAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	a.mu.Lock()
	a.killWindow++
	a.mu.Unlock()
	return nil
}

// TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N asserts that a remote session's
// Kill forcefully terminates the worker pane PID over the SSH runner (TERM then
// KILL), in addition to the graceful SendKeysQuit and the authoritative
// KillWindow. Pre-fix, no command runs over the runner and the worker PID leaks.
func TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N(t *testing.T) {
	runner := &recordingKillRunner{}
	adapter := &recordingKillAdapter{}
	const workerPID = 424242

	sess := &tmuxSubstrateSession{
		adapter:  adapter,
		handle:   "worker-session:hk-btl1n/i1",
		paneID:   "%77",
		pid:      workerPID,
		remote:   true,
		runner:   runner,
		waitDone: make(chan struct{}),
	}

	if err := sess.Kill(context.Background()); err != nil {
		t.Fatalf("Kill(remote): %v", err)
	}

	// The graceful stop and the authoritative window cleanup still fire.
	if adapter.sendKeysQuit == 0 {
		t.Error("SendKeysQuit not called on remote Kill (graceful stop missing)")
	}
	if adapter.killWindow == 0 {
		t.Error("KillWindow not called on remote Kill (authoritative cleanup missing)")
	}

	// The worker pane PID was forcefully terminated over the SSH runner.
	calls := runner.callsCopy()
	if len(calls) == 0 {
		t.Fatalf("hk-btl1n regression: remote Kill ran no command over the SSH runner — the worker "+
			"pane PID %d is never forcefully terminated (SendKeysQuit + KillWindow only), so an agent "+
			"surviving the pane SIGHUP leaks on the worker.", workerPID)
	}
	var joined []string
	for _, c := range calls {
		joined = append(joined, strings.Join(c, " "))
	}
	all := strings.Join(joined, " | ")
	wantTerm := fmt.Sprintf("kill -TERM %d", workerPID)
	wantKill := fmt.Sprintf("kill -KILL %d", workerPID)
	if !strings.Contains(all, wantTerm) {
		t.Errorf("remote Kill SSH command does not SIGTERM the worker PID: want substring %q, got calls: %s", wantTerm, all)
	}
	if !strings.Contains(all, wantKill) {
		t.Errorf("remote Kill SSH command does not escalate to SIGKILL of the worker PID: want substring %q, got calls: %s", wantKill, all)
	}
}

// TestRemoteKill_NilRunner_NoPanic_HKBTL1N is the defensive counter-case: a
// remote session with no runner (nil) must still complete Kill (SendKeysQuit +
// KillWindow) without panicking — the forceful-kill step is skipped, not required.
func TestRemoteKill_NilRunner_NoPanic_HKBTL1N(t *testing.T) {
	adapter := &recordingKillAdapter{}
	sess := &tmuxSubstrateSession{
		adapter:  adapter,
		handle:   "worker-session:hk-btl1n/i1",
		paneID:   "%78",
		pid:      424243,
		remote:   true,
		runner:   nil,
		waitDone: make(chan struct{}),
	}

	if err := sess.Kill(context.Background()); err != nil {
		t.Fatalf("Kill(remote, nil runner): %v", err)
	}
	if adapter.killWindow == 0 {
		t.Error("KillWindow not called on remote Kill with nil runner")
	}
}
