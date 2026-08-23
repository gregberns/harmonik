package daemon

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/runlease"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
)

type portleaseAlloc struct {
	mu              sync.Mutex
	ports           []int
	reservedAtAlloc []bool
}

func (a *portleaseAlloc) took() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ports)
}

func (a *portleaseAlloc) wasReservedAtAlloc(i int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reservedAtAlloc[i]
}

func portleaseWatchAlloc(t *testing.T) *portleaseAlloc {
	t.Helper()
	rec := &portleaseAlloc{}
	orig := tunnelpkg.AllocatePort
	t.Cleanup(func() {
		tunnelpkg.AllocatePort = orig
		for _, p := range rec.took() {
			tunnelpkg.ReleasePort(p)
		}
	})
	tunnelpkg.AllocatePort = func() (int, error) {
		port, err := orig()
		if err != nil {
			return port, err
		}
		rec.mu.Lock()
		rec.ports = append(rec.ports, port)
		rec.reservedAtAlloc = append(rec.reservedAtAlloc, tunnelpkg.PortReserved(port))
		rec.mu.Unlock()
		return port, nil
	}
	return rec
}

func portleaseTheOnePortTaken(t *testing.T, alloc *portleaseAlloc) int {
	t.Helper()
	ports := alloc.took()
	if len(ports) != 1 {
		t.Fatalf("the run allocated %d tunnel port(s), want exactly 1 (ports=%v).\n"+
			"Nothing below means anything without one: a run that never took a port has "+
			"nothing to give back, and every free-port claim here would pass for free.",
			len(ports), ports)
	}
	if !alloc.wasReservedAtAlloc(0) {
		t.Fatalf("port %d was not in the reservation set at the instant the allocator returned it.\n"+
			"Either the allocator stopped reserving or the reader is looking at the wrong set. "+
			"Both make the give-back claim below unfalsifiable.", ports[0])
	}
	return ports[0]
}

func portleaseWantFreed(t *testing.T, port int, ending string) {
	t.Helper()
	if tunnelpkg.PortReserved(port) {
		t.Errorf("port %d is still reserved after %s.\n"+
			"The reservation set is process-global and nothing in production ever clears it, so this "+
			"port is now unusable for the life of the daemon and nothing anywhere reports that.",
			port, ending)
	}
}

type portleaseCountingTunnel struct {
	mu    sync.Mutex
	built int
}

func (p *portleaseCountingTunnel) build(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	p.mu.Lock()
	p.built++
	p.mu.Unlock()
	return exec.CommandContext(ctx, "sh", "-c", "sleep 300")
}

func (p *portleaseCountingTunnel) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.built
}

func portleaseWantTunnelRefusal(t *testing.T, ledger *runplanacqLedger, bus *runplanBus, bead core.BeadID) {
	t.Helper()
	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d, want exactly 1.\n"+
			"Without the refusal this run did something else entirely, and the port claim below "+
			"is about a path the test did not drive.\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != bead {
		t.Errorf("ReopenBead beadID = %q, want %q", calls[0].beadID, bead)
	}
	if !strings.HasPrefix(calls[0].reason, "reverse-tunnel not ready: ") {
		t.Errorf("ReopenBead reason = %q, want a reverse-tunnel refusal", calls[0].reason)
	}
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})
}

// TestTunnelPort_ARemoteRunGivesItsPortReservationBackWhenItEnds drives the
// plainest remote run there is — the tunnel comes up, the readiness probe passes
// over the ssh shim, the stub agent runs and exits — and asserts the port
// reservation is gone once beadRunOne has returned.
//
// Nothing about the ending is special, which is the point. A reservation that
// survives the most ordinary run there is survives every run.
//
// The test proves three things happened before it claims the port is free: the
// run allocated exactly one port, the set really held it, and the run carried
// that port number all the way to the worker — the readiness probe in the ssh
// log names it. The last one is what rules out a run that allocated a port and
// then refused quietly above the launch.
func TestTunnelPort_ARemoteRunGivesItsPortReservationBackWhenItEnds(t *testing.T) {
	projectDir := remotefixRepo(t)
	sshLog := remotefixSSHShim(t, 0)
	tunnels := &portleaseCountingTunnel{}
	remotefixTunnelSeam(t, tunnels.build)
	alloc := portleaseWatchAlloc(t)

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

	env := remotefixRunEnv(deps, remotefixBead("hk-portlease-ordinary", "tunnel port give-back probe"))

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	runBeadOneTest(ctx, deps, env, "", preSelected, false)

	port := portleaseTheOnePortTaken(t, alloc)
	if got := tunnels.count(); got != 1 {
		t.Fatalf("the run built %d reverse tunnel(s), want 1.\n"+
			"A run that took a port and then refused above the tunnel is a refusal test, not this "+
			"one, and it would prove nothing about the ordinary ending.", got)
	}
	calls := remotefixSSHCalls(t, sshLog)
	portDigits := strconv.Itoa(port)
	if !slices.ContainsFunc(calls, func(line string) bool {
		return strings.Contains(line, "nc") && strings.Contains(line, portDigits)
	}) {
		t.Fatalf("no ssh call carried the readiness probe for port %d.\n"+
			"The run must reach the readiness gate with the port it reserved. Without that, the "+
			"port was allocated and abandoned rather than used, and this is not the ordinary "+
			"ending it claims to be.\ncalls:\n%s",
			port, strings.Join(calls, "\n"))
	}

	portleaseWantFreed(t, port, "an ordinary remote run ended")
}

// TestTunnelPort_ARunRefusedAtTheReadinessGateStillGivesItsPortBack is the case
// the run scope exists for.
//
// The readiness gate is the last of the refusals that sit BELOW the port
// allocation, and it returns straight out of beadRunOne. Before the give-back
// moved onto the run's scope it was a defer registered further down, so this
// early return took a port and kept it. That is the leak, and it fired on every
// worker that was reachable by ssh but whose reverse forward never came up —
// the common remote failure, not a corner.
//
// The gate is given ten seconds by internal/transport/tunnel and there is no
// knob, so this test spends them. Shortening it by cancelling the run context
// from a seam would make the run end for a different reason and would test the
// cancellation path instead of the gate.
func TestTunnelPort_ARunRefusedAtTheReadinessGateStillGivesItsPortBack(t *testing.T) {
	projectDir := remotefixRepo(t)
	remotefixSSHShim(t, 1)
	tunnels := &portleaseCountingTunnel{}
	remotefixTunnelSeam(t, tunnels.build)
	alloc := portleaseWatchAlloc(t)

	workerReg, preSelected := remotefixReserveWorker(t)

	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		t.Error("a worktree was created for a run refused at the readiness gate")
		return t.TempDir(), func() {}, nil
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}
	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = bus
	params.AdapterRegistry2 = runplanacqSealedRegistry(t)
	params.WorkerRegistry = workerReg
	params.WorktreeFactory = worktreeFactory
	deps := ExportedTestRuntime(params)

	bead := remotefixBead("hk-portlease-readiness", "readiness-gate refusal port probe")
	env := remotefixRunEnv(deps, bead)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a run refused at the readiness gate")
	}

	port := portleaseTheOnePortTaken(t, alloc)
	if got := tunnels.count(); got != 1 {
		t.Fatalf("the run built %d reverse tunnel(s), want 1.\n"+
			"The readiness gate runs below the tunnel, so a run that built none refused somewhere "+
			"else and this test is measuring a different path.", got)
	}
	portleaseWantTunnelRefusal(t, ledger, bus, bead.BeadID)
	if got := workerReg.InFlight(); got != 0 {
		t.Errorf("InFlight after the refusal = %d, want 0", got)
	}

	portleaseWantFreed(t, port, "a run was refused at the tunnel readiness gate")
}

// TestTunnelPort_ARunRefusedForItsSocketPathStillGivesItsPortBack is the other
// refusal below the allocation, and it returns from a different line.
//
// The two refusals are worth separate tests because they are separate returns.
// A give-back written as a defer at one of them leaves the other leaking, and
// the whole reason the release moved onto the run's scope is that a scope covers
// a return the author did not think of.
//
// Reaching this gate needs a run whose worker was NOT pre-selected. The run plan
// runs the same check and refuses above the allocation, but only for a run
// already known to be remote; a run that becomes remote through the fallback
// worker selection reaches the copy inside the tunnel block instead.
func TestTunnelPort_ARunRefusedForItsSocketPathStillGivesItsPortBack(t *testing.T) {
	projectDir := hooksockDeepRepo(t)
	remotefixSSHShim(t, 0)
	tunnels := &portleaseCountingTunnel{}
	remotefixTunnelSeam(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		t.Errorf("a reverse tunnel was built for a run whose hook socket path is too long: %s %v", name, args)
		return tunnels.build(ctx, name, args...)
	})
	alloc := portleaseWatchAlloc(t)

	workerReg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{remotefixWorker}})

	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		t.Error("a worktree was created for a run refused on its socket path")
		return t.TempDir(), func() {}, nil
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}
	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = bus
	params.AdapterRegistry2 = runplanacqSealedRegistry(t)
	params.WorkerRegistry = workerReg
	params.WorktreeFactory = worktreeFactory
	deps := ExportedTestRuntime(params)

	bead := remotefixBead("hk-portlease-sockpath", "socket-path refusal port probe")
	env := remotefixRunEnv(deps, bead)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if succeeded := runBeadOneTest(ctx, deps, env, "", nil, false); succeeded {
		t.Error("beadRunOne reported success for a run refused on its socket path")
	}

	if len(alloc.took()) == 0 {
		t.Fatal("the run allocated no tunnel port.\n" +
			"Either the fallback worker selection did not make this run remote, or the socket-path " +
			"check now refuses ABOVE the allocation. The second would be an improvement and would " +
			"make this test obsolete rather than wrong — delete it and say so.")
	}
	port := portleaseTheOnePortTaken(t, alloc)
	portleaseWantTunnelRefusal(t, ledger, bus, bead.BeadID)
	if got := workerReg.InFlight(); got != 0 {
		t.Errorf("InFlight after the refusal = %d, want 0", got)
	}

	portleaseWantFreed(t, port, "a run was refused for its hook-socket path")
}

// TestTunnelPort_ASurvivingRunKeepsItsPortReservationAndAReclaimedOneDoesNot
// pins the tunnel port against the disposition, over a REAL reservation.
//
// The port is in the survive set: a run whose agent has a tmux session of its
// own and whose daemon is stopping leaves the agent working, and the agent's
// hooks still come home down that tunnel. Freeing the number would let the next
// run be handed a port a live forward is already using.
//
// # What this test does NOT do, and why
//
// It does not drive beadRunOne. No run can reach that function holding both a
// tunnel port and the survive disposition, and three independent things each
// make that true — the two arms of the ConfigurePerRunSubstrate closure, the
// workflow-mode branch, and the tunnel's own context lifetime. They are written
// out in survive_shutdown_run_resources_test.go. So there is no end-to-end
// ordering to drive, and a fixture that appeared to drive one would be lying
// about which branch it took.
//
// What is left is the composition, and it is worth pinning because it is the
// only place the real reservation set meets the disposition: the give-back the
// run registers, closed under Survive, must leave the reservation standing.
//
// # How it avoids the trap
//
// "The port is still reserved" is true of a scope that never ran. So the test
// reads the close's own report: the scope must say it KEPT the tunnel port,
// which is a statement that the machinery ran and chose not to act, not an
// absence. The release closure counts its own calls as well, and the Reclaim
// half runs the identical scope to the opposite answer — so a disposition wired
// to ignore the resource fails one half or the other whichever way it is wrong.
//
// Mutating survivesWithTheRun to drop TunnelPort turns the survive half red on
// both the report and the reservation.
func TestTunnelPort_ASurvivingRunKeepsItsPortReservationAndAReclaimedOneDoesNot(t *testing.T) {
	cases := []struct {
		name string
		exit runlease.Exit
		// keeps says whether this ending leaves the reservation standing.
		keeps bool
		why   string
	}{
		{
			name:  "a stopping daemon leaves an own-session run its port",
			exit:  runlease.Exit{SessionRunsIndependently: true, DaemonStopping: true},
			keeps: true,
			why: "the agent is still working and still reporting through that forward; " +
				"handing the number to the next run points a second forward at a live one",
		},
		{
			name:  "an ordinary ending gives the port back",
			exit:  runlease.Exit{},
			keeps: false,
			why:   "nothing is using the forward, and a number nobody frees is a number nobody reuses",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, err := tunnelpkg.AllocatePort()
			if err != nil {
				t.Fatalf("AllocatePort: %v", err)
			}
			t.Cleanup(func() { tunnelpkg.ReleasePort(port) })
			if !tunnelpkg.PortReserved(port) {
				t.Fatalf("port %d was not reserved after the allocation, so this test has nothing "+
					"to keep or give back", port)
			}

			var releases int
			scope := &runlease.Scope{}
			scope.Hold(runlease.TunnelPort, func() error {
				releases++
				tunnelpkg.ReleasePort(port)
				return nil
			})

			report := scope.Close(runlease.Decide(tc.exit))

			gotKept := slices.Contains(report.Kept, runlease.TunnelPort)
			gotReleased := slices.Contains(report.Released, runlease.TunnelPort)
			if gotKept == gotReleased {
				t.Fatalf("the close reported the tunnel port as kept=%v released=%v.\n"+
					"Exactly one must be true, or the scope did not reach the resource at all and "+
					"nothing below is evidence.\nreport=%+v", gotKept, gotReleased, report)
			}
			if gotKept != tc.keeps {
				t.Errorf("the close reported the tunnel port as kept=%v, want %v — %s", gotKept, tc.keeps, tc.why)
			}
			if wantReleases := map[bool]int{true: 0, false: 1}[tc.keeps]; releases != wantReleases {
				t.Errorf("the give-back ran %d time(s), want %d — %s", releases, wantReleases, tc.why)
			}
			if got := tunnelpkg.PortReserved(port); got != tc.keeps {
				t.Errorf("PortReserved(%d) = %v after the close, want %v — %s", port, got, tc.keeps, tc.why)
			}
		})
	}
}
