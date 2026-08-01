package daemon

// tunnel_teardown_test.go — a remote run kills the reverse tunnel it started.
//
// # What was here before
//
// Nothing. The reverse-tunnel process had no teardown test. The tunnel PORT has
// tests, but they live in internal/transport/tunnel and cover the reservation
// map on its own; nothing drove a run and asked what it left behind. A leaked
// `ssh -N -R` holds a worker-side listener on the run's port for the life of the
// daemon, and the next run that reserves that port number gets a forward it does
// not own.
//
// # What makes this test able to fail
//
// The run's context is still live when the assertion runs — the test cancels it
// on the way out, after it has read the process state. So nothing but an
// explicit kill could have ended the tunnel by then, and "the process is dead"
// is evidence about the teardown rather than about the context.
//
// The fixture's tunnel is also an exec.Command rather than an
// exec.CommandContext, which the production builder uses. That is belt to the
// brace above: it keeps the claim true even if somebody later restructures the
// test to cancel earlier. It was NOT needed to make the test fail — checked, and
// the mutation below is red with a context-bound tunnel too. It does mean a
// failure leaves a sleeping process behind, so the fixture reaps its own
// children.
//
// # Mutation record
//
// Remove the tunnel process from the run's scope — hold it on a bare lease the
// scope does not close — and this test goes red with the process still running.
// Run twice, once with the detached tunnel above and once with a context-bound
// one, and red both times.
//
// The remote setting is in remoterunfixture_test.go and is shared with the
// cold-start and port-refusal tests. Helper prefix: tunteardown.

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/harness/claude"
)

// tunteardownTunnels records every reverse tunnel a run started, so a test can
// ask what happened to each one after the run returned.
type tunteardownTunnels struct {
	mu   sync.Mutex
	cmds []*exec.Cmd
}

// build is the seam the daemon calls to construct the tunnel process. It ignores
// the context on purpose — see the file header.
func (t *tunteardownTunnels) build(_ context.Context, _ string, _ ...string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", "sleep 300") //nolint:noctx // deliberately detached from the run context, so only an explicit kill can end it however the test is later restructured
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cmds = append(t.cmds, cmd)
	return cmd
}

// started returns the tunnels that reached a real process.
func (t *tunteardownTunnels) started() []*exec.Cmd {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*exec.Cmd, 0, len(t.cmds))
	for _, c := range t.cmds {
		if c.Process != nil {
			out = append(out, c)
		}
	}
	return out
}

// reapOnFailure kills anything still alive when the test ends, so a red test
// does not leave a sleeping process behind.
func (t *tunteardownTunnels) reapOnFailure(tb testing.TB) {
	tb.Helper()
	tb.Cleanup(func() {
		for _, c := range t.started() {
			if c.ProcessState == nil {
				_ = c.Process.Kill() //nolint:errcheck // fixture reap of a process the test already reported as leaked
				_ = c.Wait()         //nolint:errcheck // reaps the kill above
			}
		}
	})
}

// TestTunnelTeardown_ARemoteRunKillsItsReverseTunnelWhenItEnds drives a real
// remote run and asserts the `ssh -N -R` process it started is dead once
// beadRunOne has returned.
//
// The run is the ordinary happy-ish one: the tunnel comes up, the readiness
// probe passes over the ssh shim, the stub agent runs and exits. Nothing about
// the ending is special, which is the point — the tunnel must not outlive the
// most ordinary run there is.
func TestTunnelTeardown_ARemoteRunKillsItsReverseTunnelWhenItEnds(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	tunnels := &tunteardownTunnels{}
	tunnels.reapOnFailure(t)

	projectDir := remotefixRepo(t)
	// Exit 0: the tunnel readiness probe runs over this shim, and a non-zero exit
	// refuses the run before it ever starts a tunnel.
	remotefixSSHShim(t, 0)
	remotefixTunnelSeam(t, tunnels.build)

	workerReg, preSelected := remotefixReserveWorker(t)

	worktreeDir := t.TempDir()
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		return worktreeDir, func() {}, nil
	}

	params := remotefixParams(t, projectDir)
	params.BrAdapter = &runplanacqLedger{}
	params.Bus = &runplanBus{}
	params.HandlerArgs = []string{"-c", "exit 0"}
	params.AdapterRegistry2 = coldstartRegistryFor(t, coldstartReadyAtOnce)
	params.WorkerRegistry = workerReg
	params.WorktreeFactory = worktreeFactory
	params.LaunchSpecBuilder = claude.BuildLaunchSpec
	deps := ExportedWorkLoopDeps(params)

	env := remotefixRunEnv(deps, remotefixBead("hk-tunnel-teardown-probe", "reverse tunnel teardown probe"))

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	runBeadOneTest(ctx, deps, env, "", preSelected, false)

	live := tunnels.started()
	if len(live) == 0 {
		t.Fatal("the run started no reverse tunnel, so it left none behind and this test measured " +
			"nothing. A remote run must build one before it launches its agent.")
	}
	for i, cmd := range live {
		if cmd.ProcessState == nil {
			t.Errorf("reverse tunnel %d (pid %d) is still running after the run returned.\n"+
				"An `ssh -N -R` that outlives its run holds the worker-side listener on that run's "+
				"port for the life of the daemon, and the next run handed the same port number gets "+
				"a forward it does not own.", i, cmd.Process.Pid)
		}
	}
}
