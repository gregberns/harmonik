package daemon

// runplan_hooksocket_test.go — the hook-socket length refusal, and the fact
// that it now fires before anything is taken.
//
// The check is a length comparison against a platform constant and reads
// nothing but the project dir. It used to run in the tunnel setup, AFTER the
// run had reserved a worker slot, allocated a tunnel port, and made an ssh
// round trip to create a directory on the worker. All three were spent on a run
// that could never carry a hook.
//
// These tests pin the move: the refusal reports exactly what it reported
// before, it stays remote-only, and it stays last, so a bead that would also
// fail an earlier decision still reports the earlier reason.
//
// Helper prefix: hooksock (implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-ta6dg.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workers"
)

// hooksockDeepRepo returns a git repository whose <dir>/.harmonik/daemon.sock
// path is at or beyond the platform's socket-path limit.
//
// The depth is derived from the validator rather than from a hard-coded number,
// because the limit differs by platform (104 bytes on Darwin, 108 elsewhere)
// and the temp-dir prefix differs by machine.
func hooksockDeepRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		if lifecycle.ValidateSocketPathLength(hooksockPath(dir)) != nil {
			break
		}
		dir = filepath.Join(dir, "deeeeeeeeep")
	}
	if lifecycle.ValidateSocketPathLength(hooksockPath(dir)) == nil {
		t.Fatalf("hooksockDeepRepo: could not build a path past the socket limit under %q", dir)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("hooksockDeepRepo: MkdirAll: %v", err)
	}
	hooksockGitInit(t, dir)
	return dir
}

// hooksockPath is the daemon hook socket the reverse tunnel forwards to.
func hooksockPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

// hooksockGitInit makes dir a git repository with one commit on main, so the
// branching decision — which runs BEFORE the socket check — resolves.
func hooksockGitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hooksockGitInit: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hook-socket fixture\n"), 0o600); err != nil {
		t.Fatalf("hooksockGitInit: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// hooksockWorker is the pre-selected worker the outer dispatch loop hands in.
var hooksockWorker = workers.Worker{
	Name:     "hooksock-worker",
	Host:     "hooksock.example.com",
	Enabled:  true,
	MaxSlots: 1,
}

// hooksockResolve resolves a plan with a pre-selected worker.
func hooksockResolve(t *testing.T, env runloop.RunEnv, worker *workers.Worker) (runPlan, *runplanBus) {
	t.Helper()
	bus := &runplanBus{}
	plan := resolveRunPlan(t.Context(), runPlanRequest{
		Env:               env,
		Emit:              bus,
		Handles:           runloop.SharedHandles{RunRegistry: runplanRegistry{h: &runplanHandle{}}},
		PreSelectedWorker: worker,
	})
	return plan, bus
}

// TestRunPlan_HookSocketTooLongRefusesARemoteRun pins the refusal's report: the
// exact stderr line, the exact reopen reason, and the worker_tunnel_failed
// payload it owes an operator.
func TestRunPlan_HookSocketTooLongRefusesARemoteRun(t *testing.T) {
	t.Parallel()
	projectDir := hooksockDeepRepo(t)
	env := runplanEnv(projectDir, runplanBead(nil, ""))

	worker := hooksockWorker
	plan, bus := hooksockResolve(t, env, &worker)

	if plan.Verdict != runPlanRefusedSocketPath {
		t.Fatalf("Verdict = %q; want %q (refusal %+v)", plan.Verdict, runPlanRefusedSocketPath, plan.Refusal)
	}

	sockPath := hooksockPath(projectDir)
	lenErr := lifecycle.ValidateSocketPathLength(sockPath)
	if lenErr == nil {
		t.Fatal("fixture: the socket path fits after all")
	}

	wantLog := "daemon: workloop: reverse-tunnel socket-path bead hk-runplan-probe run " +
		env.RunID.String() + ": " + lenErr.Error() + " (reopening, not launching)\n"
	if plan.Refusal.LogLine != wantLog {
		t.Errorf("LogLine  = %q\nwant       %q", plan.Refusal.LogLine, wantLog)
	}
	wantReason := "reverse-tunnel not ready: " + lenErr.Error()
	if plan.Refusal.ReopenReason != wantReason {
		t.Errorf("ReopenReason = %q\nwant           %q", plan.Refusal.ReopenReason, wantReason)
	}

	tf := plan.Refusal.TunnelFailure
	if tf == nil {
		t.Fatal("TunnelFailure = nil; the refusal must carry the worker_tunnel_failed report")
	}
	if tf.RunID != env.RunID.String() || tf.BeadID != "hk-runplan-probe" {
		t.Errorf("TunnelFailure run/bead = %q/%q; want %q/hk-runplan-probe", tf.RunID, tf.BeadID, env.RunID.String())
	}
	if tf.WorkerName != worker.Name || tf.WorkerHost != worker.Host {
		t.Errorf("TunnelFailure worker = %q/%q; want %q/%q", tf.WorkerName, tf.WorkerHost, worker.Name, worker.Host)
	}
	if tf.SocketPath != sockPath {
		t.Errorf("TunnelFailure SocketPath = %q; want %q", tf.SocketPath, sockPath)
	}
	if tf.Detail != lenErr.Error() {
		t.Errorf("TunnelFailure Detail = %q; want %q", tf.Detail, lenErr.Error())
	}

	// The plan itself emits nothing for this refusal. The event is the
	// reporter's job, so that it lands between the stderr line and the reopen.
	runplanWantEvents(t, bus.seen(), nil)
}

// TestRunPlan_HookSocketCheckIsRemoteOnly pins that a run with no pre-selected
// worker is not refused, however long the path is. A local run never opens a
// reverse tunnel, so the socket length cannot hurt it.
func TestRunPlan_HookSocketCheckIsRemoteOnly(t *testing.T) {
	t.Parallel()
	projectDir := hooksockDeepRepo(t)
	env := runplanEnv(projectDir, runplanBead(nil, ""))

	plan, _ := hooksockResolve(t, env, nil)

	if plan.Verdict != runPlanReady {
		t.Fatalf("Verdict = %q; want ready — the check is remote-only (%+v)", plan.Verdict, plan.Refusal)
	}
	if plan.Refusal.TunnelFailure != nil {
		t.Error("TunnelFailure set on a run that was not refused")
	}
}

// TestRunPlan_HookSocketDecision drives the decision on its own. It reads only
// the project dir and the pre-selected worker, so it needs no repository, and a
// synthetic short path makes the fits-case deterministic on any machine — a
// temp dir is not, because some machines' temp prefix is long enough to refuse
// on its own.
func TestRunPlan_HookSocketDecision(t *testing.T) {
	t.Parallel()

	longDir := "/" + strings.Repeat("d", 200)

	for _, tc := range []struct {
		name        string
		projectDir  string
		worker      *workers.Worker
		wantVerdict runPlanVerdict
	}{
		{name: "short path, remote run", projectDir: "/p", worker: &hooksockWorker, wantVerdict: runPlanReady},
		{name: "short path, local run", projectDir: "/p", wantVerdict: runPlanReady},
		{name: "long path, local run", projectDir: longDir, wantVerdict: runPlanReady},
		{name: "long path, remote run", projectDir: longDir, worker: &hooksockWorker, wantVerdict: runPlanRefusedSocketPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := runPlan{Verdict: runPlanReady}
			resolveRunPlanHookSocket(runPlanRequest{
				Env: runloop.RunEnv{
					ProjectDir: tc.projectDir,
					RunID:      core.RunID(uuid.New()),
					BeadRecord: runplanBead(nil, ""),
				},
				PreSelectedWorker: tc.worker,
			}, &plan)

			if plan.Verdict != tc.wantVerdict {
				t.Fatalf("Verdict = %q; want %q (refusal %+v)", plan.Verdict, tc.wantVerdict, plan.Refusal)
			}
			if tc.wantVerdict == runPlanReady && plan.Refusal.TunnelFailure != nil {
				t.Error("TunnelFailure set on a run that was not refused")
			}
		})
	}
}

// TestRunPlan_HookSocketRefusesLast pins the ordering. The socket check moved
// UP past three acquisitions, but it must not move up past the other refusals:
// a bead that would also fail an earlier decision has to keep reporting that
// earlier reason, or an operator reading the reopen reason is sent to the wrong
// repair.
func TestRunPlan_HookSocketRefusesLast(t *testing.T) {
	t.Parallel()
	projectDir := hooksockDeepRepo(t)
	env := runplanEnv(projectDir, runplanBead(nil, runplanBeadBody("start_from: no-such-ref")))

	worker := hooksockWorker
	plan, _ := hooksockResolve(t, env, &worker)

	if plan.Verdict != runPlanRefusedStartFrom {
		t.Fatalf("Verdict = %q; want %q — an earlier decision refused first",
			plan.Verdict, runPlanRefusedStartFrom)
	}
	if plan.Refusal.TunnelFailure != nil {
		t.Error("TunnelFailure set on a refusal that came from another decision")
	}
}

// TestRunPlan_HookSocketRefusalTakesNothing drives the whole path through
// beadRunOne: the refusal reports the same three things in the same order, the
// pre-reserved slot comes back, and no worktree is created.
func TestRunPlan_HookSocketRefusalTakesNothing(t *testing.T) {
	projectDir := hooksockDeepRepo(t)

	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{hooksockWorker}})
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
		BeadID:   core.BeadID("hk-hooksock-probe"),
		Title:    "hook-socket refuse probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}

	env := deps.runEnv(runID, bead, "", nil, nil, 0, "", "", nil, false, "", core.AgentType(""))
	if succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false); succeeded {
		t.Error("beadRunOne reported success for a refused bead")
	}

	calls := ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d; want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if !strings.Contains(calls[0].reason, "reverse-tunnel not ready") {
		t.Errorf("ReopenBead reason = %q; want it to name the reverse tunnel", calls[0].reason)
	}

	// The one event this refusal owes, and no other.
	runplanWantEvents(t, bus.seen(), []core.EventType{core.EventTypeWorkerTunnelFailed})

	if worktreeCreated {
		t.Error("a worktree was created for a refused bead")
	}
	if got := reg.InFlight(); got != 0 {
		t.Fatalf("InFlight after the refusal = %d; want 0", got)
	}
	if !reg.HasFreeSlot() {
		t.Fatal("HasFreeSlot after the refusal = false; the slot was not returned")
	}
}
