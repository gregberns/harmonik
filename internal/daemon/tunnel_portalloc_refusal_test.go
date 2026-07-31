package daemon

// tunnel_portalloc_refusal_test.go — a remote run whose tunnel port
// allocation fails refuses at the point of failure, and takes nothing.
//
// The port is the FIRST thing the remote tunnel block acquires. When the
// allocation failed, the block used to log and carry on: it spent an ssh round
// trip to make a directory on the worker, then started a real
// `ssh -N -R 127.0.0.1:0:…` that could never carry traffic, and only the
// readiness gate ten seconds later failed the run. Every one of those was
// spent on a run that was already lost.
//
// This test is the sibling of the hook-socket refusal test in
// runplan_hooksocket_test.go and of the refusal invariant in
// runplan_before_acquisition_test.go, and it asserts the same shape they do —
// one reopen, no worktree, the worker slot back — plus the two costs that are
// unique to this refusal: no ssh round trip, and no tunnel process.
//
// Those two are "X did not happen" claims, which any dead harness satisfies
// for free. They are worth something here only because both recorders fire
// when the refusal is removed: with the refusal reverted, the ssh shim's log
// carries the mkdir round trip and the tunnel seam records an argv.
//
// Helper prefix: tunport (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
)

// tunportWorker is the worker the outer dispatch loop pre-selects. The host is
// deliberately unroutable: nothing in this test may reach a network, and a run
// that somehow got past the refusal should fail loudly rather than dial a real
// machine.
var tunportWorker = workers.Worker{
	Name:     "tunport-worker",
	Host:     "tunport.invalid",
	Enabled:  true,
	MaxSlots: 1,
}

// tunportRepo returns a git repository whose <dir>/.harmonik/daemon.sock path
// fits the platform's socket-path limit.
//
// It does NOT use t.TempDir: that path carries the test's own name, which on a
// machine with a long temp prefix pushes the socket past the limit and makes
// the run plan refuse for the hook-socket reason instead — a green test that
// never reaches the port at all.
func tunportRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hktun")
	if err != nil {
		t.Fatalf("tunportRepo: MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("tunportRepo: cleanup %s: %v", dir, rmErr)
		}
	})
	if lenErr := lifecycle.ValidateSocketPathLength(hooksockPath(dir)); lenErr != nil {
		t.Fatalf("tunportRepo: fixture socket path is already too long, so this test would "+
			"measure the hook-socket refusal instead of the port refusal: %v", lenErr)
	}
	// The same one-commit repository the hook-socket fixture builds; the run
	// plan's branching decision needs a real branch tip either way.
	hooksockGitInit(t, dir)
	return dir
}

// tunportSSHLog puts a recording `ssh` first on PATH and returns the path of
// the file it appends one line to per invocation. Every ssh the run would make
// is a subprocess resolved through PATH (tmux.SSHRunner.Command and the tunnel
// alike), so the shim is the one place that sees all of them — and it keeps a
// test that regressed from touching a network.
func tunportSSHLog(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "ssh-calls.log")
	shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\nexit 1\n"
	// 0o700 rather than 0o600: the shim is put on PATH and must be executable.
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(shim), 0o700); err != nil { //nolint:gosec // G306: an exec shim in a per-test temp dir must carry the execute bit
		t.Fatalf("tunportSSHLog: write shim: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// tunportSSHCalls returns the lines the shim recorded, or nothing when no ssh
// ran at all.
func tunportSSHCalls(t *testing.T, logPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(logPath) //nolint:gosec // a path this test just created under its own temp dir
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("tunportSSHCalls: read %s: %v", logPath, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestTunnelSetup_FailedPortAllocRefusesBeforeSpendingAnything drives a remote
// run whose port allocation fails through beadRunOne and asserts the refusal is
// complete and free: the bead is reopened once with the reverse-tunnel reason,
// the one worker_tunnel_failed event is emitted, the pre-reserved worker slot
// comes back, no worktree is created, no ssh round trip is made, and no tunnel
// process is constructed.
func TestTunnelSetup_FailedPortAllocRefusesBeforeSpendingAnything(t *testing.T) {
	// Not parallel: swaps two package-level seams in internal/transport/tunnel
	// and the process PATH.
	projectDir := tunportRepo(t)
	sshLog := tunportSSHLog(t)

	// The failure this test exists for. It is unreachable without the seam:
	// the real allocator fails only when box A can hand out no loopback port.
	allocErr := errors.New("tunport: no free port")
	origAlloc := tunnelpkg.AllocatePort
	t.Cleanup(func() { tunnelpkg.AllocatePort = origAlloc })
	tunnelpkg.AllocatePort = func() (int, error) { return 0, allocErr }

	// The tunnel seam records rather than spawns, so a tunnel started for this
	// run is a test failure and not a stray ssh process.
	var tunnelBuilds atomic.Int32
	origRunner := tunnelpkg.ReverseTunnelRunner
	t.Cleanup(func() { tunnelpkg.ReverseTunnelRunner = origRunner })
	tunnelpkg.ReverseTunnelRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		tunnelBuilds.Add(1)
		t.Errorf("a reverse tunnel was constructed for a run whose port allocation failed: %s %v", name, args)
		return exec.CommandContext(ctx, "true")
	}

	// One worker with one slot, reserved before the call — exactly what the
	// outer dispatch loop hands beadRunOne.
	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{tunportWorker}})
	preSelected := reg.SelectWorker()
	if preSelected == nil {
		t.Fatal("setup: SelectWorker returned nil; expected a reserved slot")
	}

	var worktreeCreated bool
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		worktreeCreated = true
		t.Error("worktree factory ran on a refused bead")
		return "", func() {}, nil
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: runplanacqSealedRegistry(t),
		WorkerRegistry:   reg,
		WorktreeFactory:  worktreeFactory,
		TargetBranch:     "main",
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runID := core.RunID(uuid.New())
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hk-tunport-probe"),
		Title:    "port-alloc refuse probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}

	env := deps.runEnv(runID, bead, "", nil, nil, 0, "", "", nil, false, "", core.AgentType(""))
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

	// The refusal fired once, and named the reverse tunnel and the cause.
	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if calls[0].beadID != bead.BeadID {
		t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, bead.BeadID)
	}
	wantReason := "reverse-tunnel not ready: " + allocErr.Error()
	if calls[0].reason != wantReason {
		t.Errorf("ReopenBead reason = %q; want %q", calls[0].reason, wantReason)
	}

	// The one event this refusal owes, and no other.
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	// Nothing was taken.
	if worktreeCreated {
		t.Error("a worktree was created for a refused bead")
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot after the refusal = false; the slot was not returned")
	}

	// Nothing was spent on the worker either: no ssh round trip, and no
	// `ssh -N -R` process for a tunnel that could never carry traffic.
	if sshCalls := tunportSSHCalls(t, sshLog); len(sshCalls) != 0 {
		t.Errorf("the run made %d ssh call(s) after a failed port allocation; want 0\ncalls:\n%s",
			len(sshCalls), strings.Join(sshCalls, "\n"))
	}
	if got := tunnelBuilds.Load(); got != 0 {
		t.Errorf("reverse tunnels constructed = %d; want 0", got)
	}
}
