package daemon

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/harness/claude"
)

type tunteardownTunnels struct {
	mu   sync.Mutex
	cmds []*exec.Cmd
}

func (t *tunteardownTunnels) build(_ context.Context, _ string, _ ...string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", "sleep 300") //nolint:noctx // deliberately detached from the run context, so only an explicit kill can end it however the test is later restructured
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cmds = append(t.cmds, cmd)
	return cmd
}

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
	tunnels := &tunteardownTunnels{}
	tunnels.reapOnFailure(t)

	projectDir := remotefixRepo(t)
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
	deps := ExportedTestRuntime(params)

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
