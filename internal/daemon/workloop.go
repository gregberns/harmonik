package daemon

// workloop.go — main work loop for the harmonik daemon.
//
// RunWorkLoop polls the bead ledger for ready work, claims beads up to
// MaxConcurrent at a time, materialises git worktrees, spawns handler
// subprocesses, and closes (or reopens) beads based on outcome.
//
// # Concurrency model (hk-e61c3.2, POST_OPERATIONAL_PARALLELISM_ROADMAP row 5)
//
// Goroutine-per-active-bead: the outer poll loop spawns one goroutine per
// claimed bead. The in-flight count is gated by MaxConcurrent via RunRegistry's
// claim semaphore (hk-e61c3.3). Parallelism roadmap rows 1–6 are shipped.
// At MaxConcurrent=1 (the default), the loop is semantically equivalent to the
// prior serial implementation: only one goroutine is ever in-flight, so
// behaviour is byte-identical to the pre-parallelism code.
//
// Anti-pattern (roadmap §6): do NOT use a worker-pool-fed-by-queue. One
// goroutine per active bead — in-flight count MUST equal runRegistry.Len().
//
// Spec refs: specs/execution-model.md §4.11 EM-049 (in-flight-run capacity gate:
// daemon MUST cap concurrent runs at max_concurrent); §4.11 EM-050 (claim-write
// serialization: token-pool of size max_concurrent before ClaimBead);
// §4.11 EM-051 (max_concurrent configuration: ≥ 1, default 1, sealed at startup).
//
// # Configurable binary
//
// HandlerBinary on daemon.Config controls which binary is spawned. The
// exploratory testing wave injects a twin binary rather than "claude" so that
// no API credits are consumed during wave runs. If HandlerBinary is empty the
// loop defaults to "claude".
//
// Spec ref: EARLY_ROADMAP.md row #10; specs/execution-model.md §4.3 EM-013 (run_id
// as join key); specs/event-model.md §8.1 (run_started / run_completed events).
// Beads: hk-ecrxy (work loop), hk-e61c3.2 (parallelism).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/sessiondata"
	codesyncpkg "github.com/gregberns/harmonik/internal/transport/codesync"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

// dashboardGateEvalInterval is the minimum interval between successive
// dashboard forcing-gate evaluations (hk-xg6rw). The gate reads two small
// JSON files (dashboard.json, lanes.json) plus config.yaml; rate-limiting
// avoids doing that disk I/O on every 2s poll tick while still reacting
// promptly to a captain's refresh.
const dashboardGateEvalInterval = 30 * time.Second

// diskLowWatermarkDefault is the default free-disk threshold below which the
// daemon pauses new bead dispatch and attempts a go-cache reap (hk-sxlb).
// 10 GiB chosen because --max-concurrent 4 with go build can consume ~3–5 GiB
// of build intermediates and worktree content per concurrent run; 10 GiB
// provides enough headroom to finish in-flight runs while rejecting new ones.
const diskLowWatermarkDefault uint64 = 10 * 1024 * 1024 * 1024 // 10 GiB

// diskCheckInterval is the default minimum interval between successive disk
// free-space probes in the work loop (hk-sxlb). 10 minutes is frequent enough
// to catch rapid accumulation (go build cache) without adding syscall overhead
// on every 2-second poll tick.
const diskCheckInterval = 10 * time.Minute

// // beadLedger is the work loop's Beads-ledger interface (the subset of
// brcli.Adapter it uses, extracted so tests can substitute a stub). The
// interface itself moved to internal/runloop (LIFT L0) because SharedHandles —
// which carries it — now lives there; this alias keeps the daemon's uses (the
// legacy aggregate.brAdapter field, resolveOwningEpicFromRecord, ~25 test stubs)
// spelled with the local name. The architectural note (bead-body access, the
// pre-claim ShowBead guard hk-p4xbw / hk-33tcf) lives with the definition in
// internal/runloop/ports.go.
type beadLedger = runloop.BeadLedger

// strandedInProgressResetter is the subset of brcli.Adapter used to auto-reset
// an in_progress bead that has no active run (hk-l2xd1). Separated from
// beadLedger so existing test stubs do not need to implement ResetBead.
type strandedInProgressResetter interface {
	ResetBead(
		ctx context.Context,
		intentLogDir string,
		cfg brcli.TimeoutConfig,
		beadID core.BeadID,
		projectHash core.ProjectHash,
		daemonStartNS int64,
	) error
}

// newLocalRunRegistry creates the registry owned by one dispatch loop.
func newLocalRunRegistry() *RunRegistry {
	return NewRunRegistry()
}

// beadRunOne executes a single claimed bead end-to-end: worktree creation,
// mode dispatch, close/reopen, worktree removal. It is called from within a
// goroutine spawned by the outer poll loop of runWorkLoop.
//
// The function never returns an error; all per-bead failures result in
// ReopenBead so the bead re-enters the ready queue for retry. Fatal conditions
// (UUID generation, worktree setup) are surfaced to stderr and cause the bead
// to be reopened rather than aborting the daemon.
//
// env carries the immutable per-run values (RSM-010): the daemon-level config
// plus the dispatched item's identity and per-item overrides. env.QueueID and
// env.QueueGroupIndex are optional: when non-nil they are stamped into
// run_started / run_completed / run_failed payloads per EM-015a/EM-015b and
// QM-011/QM-012. They are nil for non-queue-dispatched runs.
//
// The returned success flag is the Run machine's terminal state (RSM-022): true
// only when the run reached Done{closed, success}. The goroutine wrapper in
// runWorkLoop reads it to drive the EM-015f group-advance evaluation and the
// hk-f722 staged-generator eval. Early guard returns (before the Run bridge
// exists) report false.
//
// Bead ref: hk-e61c3.2, hk-45ude.
//
//nolint:funlen,gocognit,cyclop // pre-existing: beadRunOne is the run-path giant the RT ports stream (RT15-RT20) exists to decompose; the signature change re-anchors the grandfathered findings and splitting the body here would defeat the behaviour-preserving property of the slice
func beadRunOne(ctx context.Context, env runloop.RunEnv, rp runloop.RunPorts, handles runloop.SharedHandles, extraContext string, preSelectedWorker *workers.Worker, localSlotHeld bool) (succeeded bool) {
	// RSM-010: alias the per-run values off env under the names the body already
	// uses. Aliasing rather than rewriting ~140 reads is what keeps the
	// signature change behaviour-obvious.
	//
	// The four per-item override fields are NOT aliased here. Every one of them
	// is a tier-0 INPUT to the run plan below, and the body must read the plan's
	// RESOLVED answer instead. The workflow ref is the reason this matters: an
	// alias of the raw field would shadow the resolved one, and a reader that
	// kept the alias would silently take the unresolved value. Leaving the name
	// undefined makes such a reader a build failure rather than a bug.
	runID, beadRecord := env.RunID, env.BeadRecord
	queueName, queueID := env.QueueName, env.QueueID
	queueGroupIndex, queueItemIndex := env.QueueGroupIndex, env.QueueItemIndex
	// mport.Submit() is the merge exclusion-domain submit surface (RSM-015).
	mport := rp.Merge
	// RSM-010: the run's EmitterPort, off the bundle rp already holds.
	// EmitterPort is a type ALIAS for handlercontract.EventEmitter
	// (runports.go), so this is the same value and the same static type the
	// 24 emissions below already used — only the spelling changes.
	emit := rp.Emitter
	beadID := beadRecord.BeadID

	// ── The run's resources (RSM-036 … RSM-038) ─────────────────────────────
	//
	// runScope holds every resource this run takes, and the deferred close gives
	// them back in the reverse of the order they were taken, under ONE
	// disposition read once for the whole run rather than a predicate per
	// release site.
	//
	// The close is registered HERE, above every acquisition, for two reasons.
	// It must run after every other give-back, because the outermost resources —
	// the worker slot and the local count — are given back last. And a resource
	// taken on a path that has already passed this line would be held by a scope
	// nobody will close again.
	//
	// runExit is a function because two of the three facts the disposition
	// depends on do not exist yet: whether the agent got a tmux session of its
	// own, and whether a Pi run failed with output worth reading, are both known
	// only after the launch. It is replaced below, once they exist. Until then
	// the run holds only resources that every disposition gives back, so the
	// partial answer here cannot keep anything standing.
	runScope := &runlease.Scope{}
	runExit := func() runlease.Exit { return runlease.Exit{DaemonStopping: ctx.Err() != nil} }
	defer func() {
		if relErr := runScope.Close(runlease.Decide(runExit())).Err(); relErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s: giving resources back: %v\n",
				beadID, runID.String(), relErr)
		}
	}()

	// hk-hs7ex: the outer loop increments localInFlight before it starts this run,
	// and this run gives it back. The lease replaces a mutable flag: the fallback
	// worker selection below turns a "local" dispatch into a remote one, and it
	// gives the count back THERE, through this lease, rather than switching off a
	// deferred cleanup. A run that never held the count gets a lease born spent,
	// so the give-back site needs no test for whether there is anything to give.
	localSlot := runlease.Hold(runlease.LocalSlot, nil)
	if localSlotHeld && handles.LocalInFlight != nil {
		localSlot = runScope.Hold(runlease.LocalSlot, func() error {
			handles.LocalInFlight.Add(-1)
			return nil
		})
	}

	// hk-3hozm: give the pre-reserved REMOTE worker slot back on ANY exit path,
	// including the four refuse-before-launch early returns below (bad pi profile,
	// CrossRepoUnsafeError, unresolvable start_from/parent commit, LandsOnProtected).
	// The outer dispatch loop pre-reserved this slot via SelectWorker and the
	// caller MUST balance it with ReleaseSlot. That release was once registered
	// only at the remote-runner setup far below, AFTER those early returns — so a
	// refused remote bead reopened and returned without releasing, permanently
	// over-counting the registry until HasFreeSlot() was false for ever and the
	// remote path wedged. Holding it here, above every early return, is what fixed
	// that, and the scope is what makes it fire exactly once.
	//
	// Keyed on preSelectedWorker so it is inert for the fallback path, which is
	// mutually exclusive — it runs only when rbc == nil, so preSelectedWorker is
	// nil — and takes a slot of its own after these early returns.
	if preSelectedWorker != nil && handles.Workers != nil {
		runScope.Hold(runlease.WorkerSlot, func() error {
			handles.Workers.ReleaseSlot()
			return nil
		})
	}

	// runTipSHA is set (in the DOT failure path) to the worktree HEAD SHA when
	// HEAD has advanced past the parent commit — meaning the implementer produced
	// a commit that the gate later bounced. Included in run_failed so operators
	// can salvage the stranded run-branch commit (hk-8b35c orphan-salvage).
	var runTipSHA *string

	// Resolve owning-epic attribution (hk-7evda, logmine F13): find the parent
	// epic from the bead's edges and look up its assignee (the crew name) so
	// terminal events carry it directly, eliminating captain br round-trips.
	// Best-effort: errors leave the fields empty (non-fatal).
	owningEpicID, owningEpicAssignee := resolveOwningEpicFromRecord(ctx, handles.BrAdapter, beadRecord)
	// runHandle is looked up ONCE and held, rather than fetched again at each use.
	// The exit disposition below reads it from a deferred close, and the force-reap
	// watchdog (StaleWatcher.forceReap) can Unregister a wedged run while its
	// goroutine is still unwinding — so a second lookup at close time can miss a
	// handle the run still owns, and would then throw away the evidence of the very
	// failure that wedged it. Nil only when the caller registered no handle.
	runHandle, _ := handles.RunRegistry.Get(runID)
	// Propagate to RunHandle so StaleWatcher can read the attribution without
	// its own br calls.
	if runHandle != nil {
		runHandle.SetOwningEpic(owningEpicID, owningEpicAssignee)
	}

	// sdStartedAt, sdModel, sdHarness are captured by the run-terminal effector
	// for the sessiondata.Collect goroutine. They are assigned after their
	// respective resolutions below (ResolveModelPreference, implHarnessWL).
	sdStartedAt := rp.Clock.Now()
	var sdModel, sdHarness string

	// emitRunTerminalEff is the ActEmitRunTerminal effector binding (RT9,
	// RSM-020/022): it stamps queue_id + queue_group_index onto every
	// run_completed / run_failed event emitted from this run and fires the
	// sessiondata collection. The hk-e3fy background-context swap (a cancelled
	// per-run ctx must not drop the terminal — the daemon is still running) and
	// the RSM-021 drain policy (background batch, NO sessiondata — the pre-RT9
	// drain block never collected) live HERE, as effector policy, instead of
	// being open-coded at each terminal block.
	//
	// Spec ref: specs/execution-model.md §4.3.EM-015b; QM-011/QM-012; RSM-021.
	// Bead ref: hk-45ude.
	emitRunTerminalEff := func(emitCtx context.Context, success bool, summary string, draining bool) { //nolint:contextcheck // hk-e3fy: a cancelled per-run ctx must not drop the terminal; Background swap by design
		if emitCtx.Err() != nil {
			emitCtx = context.Background()
		}
		emitRunCompleted(emitCtx, emit, runID, string(beadID), owningEpicID, owningEpicAssignee, success, summary, queueID, queueGroupIndex, runTipSHA)
		if draining {
			return // RSM-021: the drain batch collects no sessiondata.
		}
		// Fire sessiondata.Collect off the hot path (hk-eval-prog-sessiondata-hook-vmxrk).
		// Best-effort: errors are silently discarded — a missed record is preferable
		// to a panicking goroutine that could affect the daemon.
		sdEndedAt := rp.Clock.Now()
		sdQID := ""
		if queueID != nil {
			sdQID = *queueID
		}
		sdCommitSHA := ""
		if runTipSHA != nil {
			sdCommitSHA = *runTipSHA
		}
		go func() {
			_ = sessiondata.Collect(sessiondata.CollectParams{
				RunID:             runID.String(),
				BeadID:            string(beadID),
				QueueID:           sdQID,
				Harness:           sdHarness,
				Model:             sdModel,
				Success:           success,
				CommitSHA:         sdCommitSHA,
				StartedAt:         sdStartedAt,
				EndedAt:           sdEndedAt,
				ProjectDir:        env.ProjectDir,
				ClaudeProjectsDir: filepath.Join(os.Getenv("HOME"), ".claude", "projects"),
			})
		}()
	}

	// ── The run plan: every decision that precedes an acquisition ────────────
	//
	// Ten decisions resolve here, in workloop_runplan.go: workflow mode and
	// ref, harness, model and effort, Pi provider profile, active repo,
	// protected branches, parent commit, lands_on, and merge target. All ten
	// sit above every acquisition in this function — the worktree, the tunnel
	// port, the agent process, the ssh session on a worker. Five of the
	// decisions can refuse the bead, and a refusal reopens it and returns having
	// taken nothing. Keep that order: nothing between here and the
	// worker-selection block below may acquire a resource.
	//
	// The plan carries the RUN-level harness tuple. A DOT run corrects the
	// harness and the model per node further down — see the note on runPlan.
	plan := resolveRunPlan(ctx, runPlanRequest{
		Env:               env,
		Emit:              emit,
		Handles:           handles,
		PreSelectedWorker: preSelectedWorker,
	})
	if plan.Verdict != runPlanReady {
		refuseRunPlan(ctx, env, handles, emit, plan.Refusal)
		return false
	}

	// Alias the plan's answers under the names the body below already uses.
	workflowMode := plan.Workflow.Mode
	resolvedModel, resolvedEffort := plan.Model, plan.Effort
	resolvedProfile := plan.PiProfile
	activeRepo := plan.ActiveRepo
	effectiveMergeProtectBranches := plan.MergeProtectBranches
	headSHA := plan.ParentSHA
	baseBranch := plan.BaseBranch
	mergeTarget := plan.MergeTarget
	sdModel = resolvedModel

	// ── RT7/RT9: the per-run Run reactor bridge (RSM-007, RSM-020..022) ──────
	//
	// The pure Run machine (internal/runexec, composed in runbridge.go) owns the
	// terminal spine for ALL FOUR terminal paths (review-loop, DOT,
	// agent-completed, exit-0): every reopen + run-terminal / close-ladder
	// pairing below rides its actions instead of open-coded blocks. Constructed
	// here, before the worktree critical section, so provisioning-phase failures
	// ride the reopen spine via EvProvisionFailed (RSM-032). The run's success
	// is the machine's terminal state (bridge.Success(), RSM-022).
	// Guard paths that return before (or without) feeding the machine yield the
	// zero value (false); every terminal-spine path returns bridge.Success().
	bridge := runloop.NewRunBridge(env, rp, handles, runID, beadID, workflowMode, emitRunTerminalEff)
	failRun := func(reason, summary string) { bridge.Fail(ctx, reason, summary) }

	// ── DD1 code-sync: select remote worker (remote-substrate B8) ───────────
	//
	// When a worker is available, three new git steps wrap the existing
	// worktree-add and merge operations:
	//   (a) fetch-base on worker (before worktree-add)
	//   (b) push run-branch from worker to origin (before merge)
	//   (c) fetch run-branch on box A (before merge)
	// Local runs (no registry or no available slot) skip all three steps.
	//
	// remoteBeadCtx is nil for local runs; non-nil for remote runs.
	//
	// rs-tunnel-spawn: the long-lived `ssh -N -R` reverse-tunnel process for this
	// remote run is held on the run's scope, which kills it. It is a local at the
	// spawn site rather than a field here, because nothing outside that site ever
	// read it. workerHookSock is the per-run worker-side reverse-
	// tunnel TCP endpoint the tunnel binds (tcp://127.0.0.1:<port>); the
	// env-override bead (2) injects it as HARMONIK_DAEMON_SOCKET so the
	// worker-side agent's hook relay dials the tunnel rather than box A's
	// unreachable local socket, and the readiness-gate bead (3) references it.
	//
	// hk-ege6: the worker-side bind is a TCP loopback listener, NOT a unix socket.
	// On macOS sshd is root, so a `-R` StreamLocal unix bind is root-owned 0600 and
	// the unprivileged hook user gets connect: permission denied → agent_ready_timeout.
	// A TCP loopback listener has no filesystem permission bits.
	type remoteBeadCtx struct {
		worker         workers.Worker
		sshRunner      tmuxpkg.CommandRunner
		workerHookSock string
	}
	var rbc *remoteBeadCtx
	// hk-hs7ex: use the worker pre-selected at dispatch time when provided. This
	// avoids a double SelectWorker call and keeps slot accounting consistent with
	// the split gate. The pre-selection was performed by the outer dispatch loop
	// after ClaimBead and before runRegistry.Register.
	if preSelectedWorker != nil {
		rbc = &remoteBeadCtx{
			worker: *preSelectedWorker,
			// hk-zexsj: pin the tmux SSHRunner off the shared SSH ControlMaster.
			sshRunner: tmuxpkg.SSHRunner{Host: preSelectedWorker.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
		}
		// hk-3hozm: this pre-reserved slot is held on the run's scope at the top of
		// beadRunOne, so the give-back also covers the refuse-before-launch early
		// returns above. Nothing to do here — a second hold would give the slot
		// back twice.
	}
	// hk-f10xl [L5 Move 2]: per-queue routing gate fallback. Applies when
	// preSelectedWorker is nil (e.g. br-ready path with no available worker at
	// dispatch time, or a race where a worker slot freed up after the outer loop's
	// HasFreeSlot peek). This path is rare after the hk-hs7ex hoist but kept for
	// correctness.
	if rbc == nil && !plan.LocalOnly && handles.Workers != nil {
		var w *workers.Worker
		if plan.WorkerTarget != "" {
			w = handles.Workers.SelectWorkerByName(plan.WorkerTarget)
		} else {
			w = handles.Workers.SelectWorker()
		}
		if w != nil {
			rbc = &remoteBeadCtx{
				worker: *w,
				// hk-zexsj: pin the tmux SSHRunner off the shared SSH ControlMaster
				// (mirroring internal/transport/tunnel's tunnel opts). A churning multiplexed
				// master can silently drop a multiplexed load-buffer / paste-buffer
				// mid-write (the hk-cnp17 truncation family), discarding the seed
				// paste → agent never starts → 30-min timeout. A dedicated,
				// non-multiplexed connection per tmux command removes that failure
				// mode. ssh uses the first value per option, so these precede any
				// worker opts.
				sshRunner: tmuxpkg.SSHRunner{Host: w.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
			}
			runScope.Hold(runlease.WorkerSlot, func() error {
				handles.Workers.ReleaseSlot()
				return nil
			})
			// hk-hs7ex: the outer loop incremented localInFlight thinking this was
			// a local run. A worker slot became available between the gate and here,
			// so this run is actually remote and never needed the local count.
			// Giving it back NOW rather than at the end is the point: the increment
			// was made on a guess that is now known to be wrong, and the lease makes
			// the end-of-run give-back a no-op rather than a double decrement.
			//nolint:errcheck // the give-back is an atomic decrement; it cannot fail
			_ = localSlot.Release()
			// hk-4tjt6: mirror the Remote flag update so LenForQueueLocal
			// stops counting this run against the per-queue local cap.
			if h, ok := handles.RunRegistry.Get(runID); ok {
				h.SetRemote(true)
			}
		}
	}
	// gap #7 Option A: ensure the worker's .harmonik/ dir exists, then
	// start the per-run SSH reverse tunnel — BOTH before any agent Launch.
	//
	//  1. workerHookSock is the per-run worker-side TCP endpoint the tunnel
	//     binds (tcp://127.0.0.1:<port>), shared by beads 1, 2, and 3. The
	//     port is allocated from box A's free ephemeral space as a HINT for
	//     sshd's worker-side bind (collision-safe: see
	//     tunnel.AllocatePort + ExitOnForwardFailure=yes).
	//  2. tunnel.EnsureWorkerHarmonikDir (bead 2) mkdir-p's the worker's .harmonik/
	//     dir for other per-run artifacts; non-fatal — the readiness gate
	//     (bead 3) is the authority.
	//  3. The tunnel (bead 1) is a SEPARATE long-lived `ssh -N -R`
	//     process: the implementer agent is spawned via a DETACHED ssh
	//     (tmux new-window -d) that returns immediately, so a -R flag on
	//     THAT ssh would tear the tunnel down before the agent's first
	//     hook. The tunnel is keyed to this run and held open for its
	//     lifetime, forwarding the worker-side per-run socket back to box
	//     A's daemon hook socket. Start is non-fatal; teardown defers a
	//     Kill+Wait.

	// hk-hs7ex: this block is now outside both the pre-selected and fallback
	// worker selection blocks, so it runs for ALL remote runs (rbc != nil)
	// regardless of which selection path set rbc. NFR7: local runs (rbc == nil)
	// skip this block entirely — byte-identical to prior behavior.
	if rbc != nil {
		daemonHookSock := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")

		// The three gates below each refuse the run the same way, and they report
		// it through the same call the run plan's refusals use: tunnelRefusal
		// builds the value, refuseRunPlan makes the stderr line, the
		// worker_tunnel_failed event and the best-effort reopen. This block used
		// to hold a second copy of that report, and the two copies drifted in the
		// stage wording. Each caller returns immediately after refusing, so no
		// resource acquired past this point is left held.

		// Allocate a free TCP port (hint for sshd's worker-side loopback bind)
		// and form the per-run worker TCP endpoint the hook relay will dial.
		// A failed alloc is fatal HERE rather than at the readiness gate below:
		// falling through spent an ssh round trip and started a real
		// `ssh -N -R 127.0.0.1:0:...` that cannot carry traffic, and the gate
		// then failed the run anyway. Refusing at the point of failure takes
		// nothing on a run that is already lost.
		tunnelPort, portErr := tunnelpkg.AllocatePort()
		if portErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "port alloc", daemonHookSock, portErr))
			// succeeded is never assigned before this point, so the explicit
			// false is byte-equivalent to a naked return (nakedret).
			return false
		}
		rbc.workerHookSock = tunnelpkg.WorkerTCPEndpoint(tunnelPort)
		// hk-cnp17: free the reserved port when this run ends, so a later
		// run may reuse it (the reservation prevents two concurrent runs
		// from being handed the same worker-side hint port).
		runScope.Hold(runlease.TunnelPort, func() error {
			tunnelpkg.ReleasePort(tunnelPort)
			return nil
		})

		if mkErr := tunnelpkg.EnsureWorkerHarmonikDir(ctx, rbc.sshRunner, rbc.worker.RepoPath); mkErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: tunnel.EnsureWorkerHarmonikDir bead %s run %s: %v (non-fatal; readiness gate is authority)\n",
				beadID, runID.String(), mkErr)
		}

		// This check also runs in the run plan, which refuses BEFORE the port and
		// the ssh round trip above. The plan can only cover a run whose worker was
		// pre-selected. A run that got its worker from the fallback selection was
		// not yet known to be remote when the plan ran, so this copy is that
		// path's guard. The two never both refuse: a pre-selected run that failed
		// the plan returned before this block.
		//
		// hk-ta6dg: `ssh -N -R <port>:<daemonHookSock>` never validates this local
		// forward destination at tunnel start — only when a connection actually
		// needs forwarding — so a too-long daemonHookSock would let the tunnel
		// come up and the readiness gate below (a TCP probe against the WORKER's
		// listener only) pass, then silently swallow every hook-relay connection
		// once traffic actually tries to flow. Fail loud here, before spawning the
		// doomed tunnel, exactly like a readiness-gate failure: no Launch, reopen
		// the bead.
		if lenErr := lifecycle.ValidateSocketPathLength(daemonHookSock); lenErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "socket-path", daemonHookSock, lenErr))
			return false
		}
		// Mirror the SSHRunner host/opts argv pattern (runner.go SSHRunner.Command):
		// extra opts BEFORE the host. Fall back to the worker record's Host when
		// the runner is not an SSHRunner (e.g. a test double).
		tunnelHost, tunnelOpts, hostOK := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if !hostOK {
			tunnelHost = rbc.worker.Host
		}
		tunnelArgs := tunnelpkg.BuildArgs(tunnelPort, daemonHookSock, tunnelHost, tunnelOpts)
		tunnelCmd := tunnelpkg.ReverseTunnelRunner(ctx, "ssh", tunnelArgs...)
		if startErr := tunnelCmd.Start(); startErr != nil {
			// Non-fatal: a failed tunnel start means the worker-side agent's hooks
			// will not reach box A, but the readiness gate (bead 3) is the
			// authority that fails the run. Nothing is held: a start that failed
			// left no process to kill.
			fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel start bead %s run %s: %v\n",
				beadID, runID.String(), startErr)
		} else {
			// A successful Start leaves a live process, so the lease needs no test
			// for whether there is one. That is the whole reason the hold is in this
			// arm rather than below the branch.
			runScope.Hold(runlease.TunnelProcess, func() error {
				// Neither call's error is actionable and both are expected: Wait
				// reports the signal the Kill just sent. The lease is what makes the
				// pair run once, which the bare defer here relied on having a single
				// caller for.
				_ = tunnelCmd.Process.Kill() //nolint:errcheck // best-effort kill of a tunnel that is ending either way (pre-RT8 idiom)
				_ = tunnelCmd.Wait()         //nolint:errcheck // reaps the killed process; the error is the signal we sent
				return nil
			})
		}

		// gap #7 bead 3: tunnel readiness gate. The worker-side implementer
		// agent can fire its first agent_ready hook BEFORE the `ssh -N -R`
		// forward above is actually live. The hook relay does retry a refused
		// dial on a TCP endpoint, on a backoff inside a bounded window, so a
		// forward that comes up late is survivable — but a forward that never
		// comes up burns that whole window and gives up, which reads as a
		// silent bridge_daemon_startup_window_exceeded → agent_ready_timeout.
		// This gate is the authority on that case. Block until the
		// worker-side per-run TCP listener is confirmed CONNECTABLE (nc -z over
		// the SSHRunner, as the worker user) before any Launch — an
		// existence-only check would false-green a non-connectable endpoint
		// (hk-ege6). On timeout or failure, do NOT launch: refuse and return —
		// the run's scope closes on the way out and kills the tunnel it holds, so
		// the `ssh -N` process does not leak. The gate runs ONLY here,
		// inside the remote branch (NFR7: local runs never construct a tunnel
		// and never reach it).
		if waitErr := tunnelpkg.WaitWorkerSocketLive(ctx, rbc.sshRunner, rbc.workerHookSock, tunnelpkg.WorkerSocketReadyTimeout); waitErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "readiness gate", rbc.workerHookSock, waitErr))
			return false
		}
	}

	// notifyWorkerOffline emits a worker_offline event and disables the worker
	// in-memory. Active only for remote runs (rbc != nil). Phase is "spawn" for
	// code-sync failures and "liveness" for mid-run probe failures (B11).
	notifyWorkerOffline := func(phase, detail string) {
		if rbc == nil {
			return
		}
		workers.EmitWorkerOfflineEvent(ctx, rbc.worker.Name, rbc.worker.Host, phase, detail, emit.Emit)
		if handles.Workers != nil {
			handles.Workers.SetEnabled(false)
		}
	}

	// preMergeSync brings the run branch onto box A before the merge. For remote
	// runs it fetches the branch DIRECTLY from the worker repo over SSH
	// (ssh://<host><repoPath>) — hk-7bwx — rather than the old worker→GitHub→box-A
	// round-trip, which failed when the worker had no valid GitHub push credential.
	// Returns an error string on failure (empty string = success). No-op for local
	// runs (rbc == nil). The final mergeRunBranchToMain pushes box A's MAIN to
	// GitHub with box A's own (valid) credentials — unaffected by this change.
	preMergeSync := func() string {
		if rbc == nil {
			return ""
		}
		// host/opts come from the worker SSHRunner so git's ssh:// fetch dials the
		// worker exactly like the rest of the remote path.
		workerHost, sshOpts, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if err := codesyncpkg.FetchRunBranchBoxA(ctx, nil, env.ProjectDir, runID.String(), workerHost, rbc.worker.RepoPath, sshOpts); err != nil {
			// B11: SSH connection failure → emit worker_offline + disable worker.
			if tmuxpkg.IsSSHConnectionFailure(err) {
				notifyWorkerOffline("spawn", fmt.Sprintf("codesync.FetchRunBranchBoxA: %v", err))
			}
			return fmt.Sprintf("fetch run branch from worker on box A: %v", err)
		}
		return ""
	}
	// ── end DD1 code-sync setup ──────────────────────────────────────────────

	wtFactory := handles.WorktreeFactory
	if wtFactory == nil {
		if rbc != nil {
			// Remote run: create the worktree on the worker via SSHRunner (B7+B8).
			sshRunner := rbc.sshRunner
			workerRepoPath := rbc.worker.RepoPath
			wtFactory = func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
				// hk-5qp7z: thread worktreeCreateMu into the config so CreateWorktree
				// serialises the git-worktree-add + HEAD-resolve loop across all
				// concurrent remote dispatch goroutines (prevents empty-HEAD race).
				cfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner).WithCreateMutex(handles.WorktreeCreateMu)
				if err := workspace.CreateWorktree(ctx, workerRepoPath, runID, headSHA, cfg); err != nil {
					return "", nil, err
				}
				wtPath := workspace.WorktreePath(workerRepoPath, runID, workspace.NoWorktreeRootOverride())
				// B11: return a cleanup func that removes the remote worktree on
				// run completion (GC orphaned remote worktrees via the SSHRunner).
				cleanup := func() {
					cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					rmCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "remove", "--force", "--force", wtPath)
					_ = rmCmd.Run()
					pruneCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "prune")
					_ = pruneCmd.Run()
				}
				return wtPath, cleanup, nil
			}
		} else {
			wtFactory = productionWorktreeFactory
		}
	}
	// RSM-010 (RT7): thread the assembled factory onto RunPorts as WorktreePort.
	// The create call site below reaches it via rp.Worktree — byte-identical to
	// calling wtFactory directly (ports-design §6).
	rp.Worktree = worktreePort(wtFactory)
	// Serialize codesync.EnsureBaseOnWorker (step a, DD1 code-sync) + 'git worktree add'
	// inside the merge exclusion domain (mergeq, RSM-018) so concurrent
	// beadRunOne goroutines do not run concurrent git operations on the same
	// remote worker (hk-lt091) or race on projectDir/.git/index.lock (hk-h8u7p),
	// and so this box-A .git + worker git work excludes concurrent merge commits.
	//
	// hk-lt091: before hk-zexsj added -o ControlMaster=no, all SSH commands to a
	// worker shared one TCP connection, so the remote OS serialised them naturally.
	// With ControlMaster=no each SSH command is an independent TCP connection; a
	// sibling bead's codesync fetch-base (git-fetch) can therefore race git-worktree-add
	// at the remote-OS level, leaving the worktree dir created but HEAD uninitialised
	// — the empty-HEAD race that hk-iaj1w retries cannot fix because the race persists
	// across all retry attempts. Running codesync fetch-base + worktree-add as ONE
	// critical section in the domain eliminates the race at its source.
	var baseSyncErr error
	var wtPath string
	var wtCleanup func()
	var wtErr error
	if subErr := mport.Submit()(ctx, "base-sync-create", func(qctx context.Context) error {
		// Step (a): for remote runs, ensure baseSHA is on the worker before the
		// worktree is created there (DD1 code-sync, remote-substrate B8).
		if rbc != nil {
			// hk-2hfyt: use codesync.EnsureBaseOnWorker (not a bare fetch-base) so
			// an unpushed base commit triggers a direct push from box A to the worker
			// rather than leaving an empty-HEAD worktree.
			workerHostEBOW, sshOptsEBOW, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
			baseSyncErr = codesyncpkg.EnsureBaseOnWorker(qctx, rbc.sshRunner, rbc.worker.RepoPath, headSHA,
				nil, env.ProjectDir, workerHostEBOW, sshOptsEBOW)
		}
		// baseSyncErr (a business outcome, handled after the critical section) does
		// not fail the critical section itself; only skip the worktree-add on it.
		if baseSyncErr == nil {
			wtPath, wtCleanup, wtErr = rp.Worktree.Create(qctx, activeRepo, runID.String(), headSHA)
		}
		return nil
	}); subErr != nil {
		// The critical section never entered the domain (ctx cancelled before
		// execution, or the queue owner stopped) — surface as a worktree-create
		// failure so the run reopens rather than proceeding with an empty wtPath.
		wtErr = subErr
	}
	if baseSyncErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: codesync.EnsureBaseOnWorker bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), baseSyncErr)
		// B11: SSH connection failure → emit worker_offline + disable worker.
		if tmuxpkg.IsSSHConnectionFailure(baseSyncErr) {
			notifyWorkerOffline("spawn", fmt.Sprintf("codesync.EnsureBaseOnWorker: %v", baseSyncErr))
		}
		reopenTID, tidErr := handles.TIDGen.Next()
		if tidErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: tidGen.Next (codesync.EnsureBaseOnWorker reopen) bead %s: %v\n", beadID, tidErr)
		}
		if reopenErr := handles.BrAdapter.ReopenBead(ctx, env.IntentLogDir, env.BrTimeoutCfg, runID, reopenTID, beadID,
			fmt.Sprintf("ensure base on worker failed: %v", baseSyncErr)); reopenErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead (codesync.EnsureBaseOnWorker) bead %s run %s: %v\n",
				beadID, runID.String(), reopenErr)
		}
		return succeeded
	}
	if wtErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: CreateWorktree for bead %s run %s: %v (reopening)\n", beadID, runID.String(), wtErr)
		// RT7 / RSM-032: the worktree-create failure rides the machine's reopen
		// spine (EvProvisionFailed) carrying BOTH strings — the distinct terminal
		// summary keeps the failure visible in events.jsonl (hk-3vbc).
		failRun(fmt.Sprintf("create worktree failed: %v", wtErr),
			fmt.Sprintf("worktree_create_failed: %v", wtErr))
		return
	}
	// useIndepSession is set true when this run is launched in an independent tmux
	// session (runSessionSpawner path, hk-o85ye). Deferred cleanup (wtCleanup,
	// runlaunch.ForceTeardownSession) is skipped on daemon shutdown so the session and its
	// worktree survive SIGKILL; on normal exit cleanup runs as usual.
	useIndepSession := false

	// hk-j6wm7: runIsPi says this run's harness resolved to Pi. It gates ONE thing
	// now — the single-mode post-mortem stderr write far below, which is a
	// single-mode step and has no graph-mode equivalent. It no longer decides
	// whether the worktree is kept. That fact is recorded at the LAUNCH, which is
	// the only place both workflow modes pass through. Setting it here left a
	// graph-mode Pi run writing its capture and having it deleted, because this
	// line sits below the graph branch's return.
	runIsPi := false

	// Both post-launch facts now exist, so the scope's close can read the whole
	// exit rather than the shutdown fact alone. Decide (RSM-037) is where the
	// polarity lives: survival needs an independent session AND a stopping
	// daemon, and it wins over retained evidence when both apply.
	//
	// The evidence fact comes off the run's handle, so it is true for a graph run
	// and a single-mode run alike. The launch marks it, and both modes launch.
	// A run with no handle reports no evidence — it also captured none, because
	// the mark and the capture are made by the same call.
	runExit = func() runlease.Exit {
		return runlease.Exit{
			SessionRunsIndependently: useIndepSession,
			DaemonStopping:           ctx.Err() != nil,
			EvidenceWorthKeeping:     runHandle != nil && runHandle.CapturedAgentOutput() && !bridge.Success(),
		}
	}

	if wtCleanup != nil {
		// The worktree is the resource two of the three dispositions keep, for two
		// different reasons: a surviving run still has an agent working inside it,
		// and a run whose captured output is the only record of why it failed
		// (hk-j6wm7) has nothing else to show an operator. The run no longer asks
		// which reason applies. It asks once, at the close.
		runScope.Hold(runlease.Worktree, func() error {
			wtCleanup()
			return nil
		})
		// The give-back is the scope's. This notice is not, because this is the
		// only place that knows WHERE the worktree is, and an operator who has to
		// read a failed run's captured output needs the path. It reads the same
		// one answer the close reads.
		defer func() {
			d := runlease.Decide(runExit())
			if d.Releases(runlease.Worktree) {
				return
			}
			// Only a run kept FOR its evidence has captured output to point at. A
			// surviving run is kept because an agent is still working in there.
			capture := ""
			if d == runlease.RetainEvidence {
				capture = fmt.Sprintf(" — the captured output is under %s/.harmonik/pi-agent/", wtPath)
			}
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: run %s (bead %s) ends %s — its worktree is kept at %s%s\n",
				runID.String(), beadID, d, wtPath, capture)
		}()
	}

	// hk-ooexj: snapshot the active repo's pre-existing untracked files at run-start
	// (before the implementer launches) so the post-run escape check can exclude
	// files that already existed and which the implementer never touched. A failed
	// snapshot leaves preRunUntracked nil — the escape check then degrades to its
	// prior, baseline-free behaviour rather than silently suppressing escapes.
	// Use activeRepo: cross-repo runs create the worktree in the target repo.
	preRunUntracked, snapErr := runmerge.SnapshotUntrackedFiles(ctx, activeRepo)
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: runmerge.SnapshotUntrackedFiles for bead %s run %s: %v (escape check will run without baseline)\n", beadID, runID.String(), snapErr)
	}

	// Emit run_started with the graph decision that this run will execute.
	// Worker fields are explicit null for local runs and set for remote runs.
	var runStartedWorkerName, runStartedWorkerOS *string
	if rbc != nil {
		runStartedWorkerName = &rbc.worker.Name
		runStartedWorkerOS = &rbc.worker.OS
	}
	emitRunStarted(ctx, emit, runID, beadID, wtPath, queueID, queueGroupIndex,
		plan.Workflow.Descriptor, workflowMode, plan.Workflow.ReviewPolicy, plan.Workflow.SelectionSource,
		runStartedWorkerName, runStartedWorkerOS)

	// Do NOT add a pre-dispatch "already landed on main?" check here. One used to
	// sit at this point and closed the bead when shared.MainHistoryHasRefsTrailer
	// — a bare "Refs: <id>" git-log grep — matched. On a bead worked in several
	// parts an older partial commit carries the same ID, so the grep matched and
	// the daemon closed a bead whose remaining work had not run. The crash-restart
	// case the check was meant to cover is handled at runtime instead, by the
	// noChange timeout and by noCommitGuardShouldReopen, and both of those check
	// whether the work is present before closing. Full record: hk-f38n, and the
	// informative note under BI-022 in specs/beads-integration.md §4.7.

	// The routed launch builder (T12 hk-xhawy) is resolved ONCE by the caller in
	// buildRunBundles and threaded here on rp.Launch / rp.LaunchBuilder, so ALL
	// workflow modes (review-loop, DOT cascade, single) share the same
	// harness-resolved builder. RT18.11 relocated that resolution to the caller —
	// where deps is live — which is what let this function drop the deps param; the
	// old by-value deps.launchSpecBuilder smuggle is gone.

	// Mode-dispatch: route to the mode-specific driver.
	//
	// dot mode: DOT-defined workflow graph; loader validates the artifact,
	// then drives the cascade (driveDotWorkflow). Default uses the embedded
	// standard-bead.dot (pre-loaded above into preloadedDotGraph).
	//
	// single mode: one-shot implementer dispatch. Reachable ONLY via an explicit
	// per-bead workflow:single label, audited via review_bypassed (EM-012a).
	//
	// review-loop mode was RETIRED (EM-015d): it was a hand-written particular of
	// the graph the dot walker executes generally, and the dot path is the
	// production default.
	switch workflowMode {
	case core.WorkflowModeDot:
		// The resolver has already loaded, substituted, and validated this graph.
		// The descriptor below and the start event name the same selected artifact.
		graph := plan.Workflow.Graph

		// WG-044: thread the (substituted) graph-level goal into every agentic node's
		// brief via the ExtraContext channel.  Prepend so it appears before any
		// operator-supplied --context text.
		dotExtraContext := extraContext
		if graph.Goal != "" {
			goalLine := "Workflow goal: " + graph.Goal
			if dotExtraContext != "" {
				dotExtraContext = goalLine + "\n\n" + dotExtraContext
			} else {
				dotExtraContext = goalLine
			}
		}

		// remote-substrate: for a remote run the cascade's worktree git probes
		// (resolve HEAD, diff-hash) and the agentic-node spawn must target the
		// WORKER via its SSHRunner; box A cannot chdir into the worker's worktree.
		// nil for local runs keeps the cascade byte-identical (NFR7).
		var dotRunner tmuxpkg.CommandRunner
		// hk-538l: dotWorkerBinary resolves each node's SessionStart hook command to the
		// WORKER's harmonik path; dotWorkerHookSock is the worker-side reverse-tunnel TCP
		// endpoint each node's claude dials for the hook relay; dotWorkerSession/Cwd tell
		// the per-run substrate which tmux session to ensure+spawn into ON THE WORKER.
		// All empty for a LOCAL run (rbc == nil) ⇒ byte-identical box-A path (NFR7).
		var dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd string
		if rbc != nil {
			dotRunner = rbc.sshRunner
			dotWorkerBinary = tunnelpkg.WorkerHarmonikPath(rbc.worker)
			dotWorkerHookSock = rbc.workerHookSock
			dotWorkerCwd = rbc.worker.RepoPath
			if ts, ok := handles.Substrate.(*tmuxSubstrate); ok {
				dotWorkerSession = ts.workerSpawnSessionName(rbc.worker.Name)
			}
		} else if handles.Runner != nil {
			dotRunner = handles.Runner // hk-hd2w6: Config.Runner injection (test seam)
		}

		// Drive the cascade: walk start → … → terminal, dispatching each node by
		// type (non-agentic synthesize-success, agentic substrate-dispatch,
		// gate/sub-workflow out-of-scope error).
		dotResult := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, beadRecord, beadRecord.Title, beadRecord.Description,
			activeRepo, wtPath, headSHA, graph, plan.Workflow.Descriptor, resolvedModel, resolvedEffort, resolvedProfile, dotExtraContext, baseBranch, dotRunner,
			dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd)

		// ── RT9: the DOT terminal rides the Run tail (RSM-020) ────────────────
		//
		// No post-mode scenario gate on this path: the DOT cascade engine
		// (dispatchDotToolNode) runs its gate inside the graph (standard-bead.dot
		// commit_gate tool node) — skipGate records the pass. The hk-whru3 /
		// hk-vbv3b already-approved-on-main carve-out rides as the carveOut
		// classifier onto the machine's AlreadyApprovedOnMain row; the hk-tnui
		// trailer stamp is the amendTrailers policy (single attempt — DOT has no
		// merge-retry loop).
		transitionTID, _ := handles.TIDGen.Next()
		bridge.Start(ctx, workflowMode)
		bridge.WireSpine(runloop.SpineArgs{
			RunRunner:       dotRunner,
			WTPath:          wtPath,
			HeadSHA:         headSHA,
			PreMergeSync:    preMergeSync,
			MPort:           mport,
			ActiveRepo:      activeRepo,
			ProtectBranches: effectiveMergeProtectBranches,
			TransitionTID:   transitionTID,
			EmitBeadClosed: func(c context.Context) {
				emitBeadClosedAndMaybeEpic(c, rp, handles, runID, beadID)
			},
			MergeTarget: mergeTarget, // hk-lgykq: per-bead integration-branch landing target (resolved baseBranch w/ fallback)
			SkipGate:    true,
			// hk-f9xzs: classify transient merge failures (rebase_conflict,
			// non_ff_merge, merge_fmt_failed) as retryable so the spine spends the
			// 3-attempt budget runBridgeConfig now grants DOT. Inherited from the
			// retired review-loop path, which was the only mode that carried it;
			// the rationale is on runBridgeConfig.
			Retryable: runmerge.IsRetryableReason,
			// hk-tnui: stamp Reviewed-By / Review-Verdict trailers on the HEAD
			// commit before the FF merge. LOCAL runs only (rbc == nil): remote runs
			// keep the trailer injection deferred (FLAGGED).
			//
			// Re-amends before EACH retry rather than only the first (RF :3899):
			// now that merges retry, the prior inner rebase may have rewritten HEAD,
			// so a once-only amend would leave the retried merge carrying no
			// trailers. The amend is idempotent.
			AmendTrailers: func(c context.Context, retry int) {
				if dotResult.approveVerdict == nil || rbc != nil {
					return
				}
				if amendErr := runmerge.AppendReviewTrailersToHEAD(c, wtPath, dotResult.approveVerdict); amendErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: runmerge.AppendReviewTrailersToHEAD (dot, merge retry %d) bead %s: %v (non-fatal)\n",
						retry, beadID, amendErr)
				}
			},
			// hk-whru3: advisory-RC + rebase_dropped_commits → work already on
			// main; a prior run merged the same patch and the rebase dropped the
			// commit. hk-vbv3b: extended to the genuine APPROVE terminal path
			// (terminalNodeID == "close") and the hk-8ps7q approved-and-done path
			// (approveVerdict != nil). Falls through to CloseBead so the infinite
			// re-dispatch loop terminates instead of re-queuing.
			CarveOut: func(reason string) bool {
				alreadyApprovedOnMain := dotResult.advisoryRC ||
					dotResult.terminalNodeID == "close" ||
					dotResult.approveVerdict != nil
				return alreadyApprovedOnMain && strings.Contains(reason, "rebase_dropped_commits")
			},
		})
		switch {
		case dotResult.success:
			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSuccess,
				PathLabel: "dot", Detail: dotResult.summary,
			})
		case dotResult.subsumed:
			// noChange-subsumed: implementer exited without advancing HEAD because
			// the work already landed in main via a prior run. Approved close, no
			// merge — no new commits (hk-9v5yo); RSM-035 event-carried strings.
			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
				EmitOutcome: true, PathLabel: "dot noChange-subsumed",
				Detail: "noChange-subsumed: bead found in main",
			})
		default:
			// Non-success terminal (BLOCK / cap-hit / no-progress / structural
			// failure / gate-out-of-scope / context_cancelled) → the reopen spine.
			//
			// Orphan-salvage (hk-8b35c): if the implementer advanced HEAD past the
			// parent (a commit landed on the run branch before the gate bounced
			// it), record the tip SHA in the run_failed payload so the operator
			// can find and manually cherry-pick / merge the stranded work.
			// REMOTE: resolve via dotRunner (nil ⇒ box-A-local, NFR7).
			//
			// hk-e3fy: use context.Background() for the HEAD resolve so a
			// context_cancelled DOT failure (daemon shutdown or per-run abort) can
			// still capture the tip SHA — ctx is already cancelled on that class.
			// The reopen itself rides the machine's spine; the reopen hook applies
			// the same cancellation-free fallback (RSM-022).
			tipResolveCtx := ctx
			if ctx.Err() != nil {
				tipResolveCtx = context.Background()
			}
			if tipSHA, tipErr := gitprobe.ResolveWorktreeHEADVia(tipResolveCtx, dotRunner, wtPath); tipErr == nil && tipSHA != "" && tipSHA != headSHA {
				runTipSHA = &tipSHA
			}

			// Retry-spend budget (hk-c1ah6). Inherited from the retired review-loop
			// path, which was its only caller: without it a bead that cannot pass
			// review is reopened and re-dispatched forever, paying for a fresh set
			// of sessions each time. The ladder charges the per-item failure counter
			// and, once the budget is spent, closes the bead flagged
			// needs-attention instead of reopening it — so a human sees it and the
			// spend stops.
			//
			// Charged only when the cascade asked for attention: a transient or
			// structural failure that is not the bead's fault should not consume a
			// budget meant for "this work keeps failing review".
			budgetExhausted := false
			if dotResult.needsAttention {
				budgetExhausted = handles.Budget.ChargeReviewLoopFailure(
					ctx, queueName, queueID, queueGroupIndex, queueItemIndex, beadID)
			}
			if budgetExhausted {
				exhaustedSummary := fmt.Sprintf("run_budget_exhausted (max=%d failures): %s",
					queue.MaxReviewLoopFailures, dotResult.summary)
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s retry budget exhausted — closing with needs-attention (hk-c1ah6)\n",
					beadID, runID.String())
				bridge.SetRejectReason(exhaustedSummary)
				bridge.Feed(ctx, runexec.Event{
					Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeBudget,
					NeedsAttention: true, Detail: exhaustedSummary,
				})
				break
			}

			bridge.Feed(ctx, runexec.Event{
				Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeFailure,
				Reason: dotResult.summary, Detail: dotResult.summary,
			})
		}
		return bridge.Success()

	default:
		// WorkflowModeSingle or any normalised-to-single value: fall through
		// to the single-mode dispatch path below.
	}

	// ─── Single-mode dispatch (production path) ───────────────────────────────

	// Step 1: build the Claude launch spec via buildClaudeLaunchSpec.
	daemonSock := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")
	// gap #7 bead 2: a REMOTE worker cannot reach box A's local daemon.sock. For
	// remote runs, the implementer agent must dial the worker-side reverse-tunnel
	// TCP endpoint (rbc.workerHookSock, tcp://127.0.0.1:<port>) instead, which the
	// `ssh -N -R` tunnel launched above forwards back to box A's daemon.sock (it is
	// a TCP loopback listener, not a unix socket, so the unprivileged hook user can
	// connect — hk-ege6). tunnel.ResolveAgentDaemonSocket returns
	// rbc.workerHookSock for a remote run and the unchanged box-A daemonSock for a
	// local run (rbc == nil), so local runs remain byte-identical (NFR7). The
	// resolved path flows into rc.daemonSocket → ClaudeEnvVars(HARMONIK_DAEMON_SOCKET).
	var rbcHookSock string
	if rbc != nil {
		rbcHookSock = rbc.workerHookSock
	}
	agentDaemonSock := tunnelpkg.ResolveAgentDaemonSocket(rbcHookSock, daemonSock)
	rc := shared.LaunchCtx{
		RunID:             runID,
		BeadID:            string(beadID),
		WorkspacePath:     wtPath,
		DaemonSocket:      agentDaemonSock,
		WorkflowMode:      workflowMode,
		Phase:             "", // empty = single-mode
		IterationCount:    1,
		PriorClaudeSessID: nil,
		HandlerBinary:     env.HandlerBinary,
		DaemonBinaryPath:  env.DaemonBinaryPath,
		BaseEnv:           env.HandlerEnv,
		BeadTitle:         beadRecord.Title,
		BeadDescription:   beadRecord.Description,
		Model:             resolvedModel,
		Effort:            resolvedEffort,
		Provider:          resolvedProfile.Provider,
		APIKeyEnv:         resolvedProfile.APIKeyEnv,
		APIKeyFile:        resolvedProfile.APIKeyFile,
		BaseURL:           resolvedProfile.BaseURL,
		API:               resolvedProfile.API,
		// worktreeRootPath is used by buildClaudeLaunchSpec to check whether the
		// workspace is a harmonik-managed worktree for --dangerously-skip-permissions
		// per HC-055b. Derived from activeRepo (= target repo for cross-repo runs).
		WorktreeRootPath: workspace.WorktreeRootPath(activeRepo, workspace.NoWorktreeRootOverride()),
		ExtraContext:     extraContext, // hk-boiwe: per-item context from queue.Item.Context
		BaseBranch:       baseBranch,   // hk-mtm0w: pre-exit rebase target
	}
	// hk-z8ek: for a REMOTE run, thread the worker's SSHRunner into the launch
	// spec so the three materialization writes (.claude/settings.json,
	// .harmonik/agent-task.md, ~/.claude.json trust) land on the WORKER's
	// filesystem — where the worktree actually lives — instead of box A's mirror
	// path. Resolve the hook "command" to the worker's harmonik path too (the
	// hook subprocess runs ON THE WORKER). Nil runner + empty workerBinaryPath
	// for a LOCAL run keeps the materialization byte-identical (NFR7).
	if rbc != nil {
		rc.Runner = rbc.sshRunner
		rc.WorkerBinaryPath = tunnelpkg.WorkerHarmonikPath(rbc.worker)
	}
	// RSM-010 (RT7): build the launch spec through LaunchPort (assembled above,
	// before the mode switch, over the pre-built routed builder). Byte-identical to
	// the pre-port `deps.launchSpecBuilder(ctx, rc)` (T12, hk-xhawy).
	spec, artifacts, specErr := rp.Launch.BuildSpec(ctx, rc)
	if specErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: buildClaudeLaunchSpec bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), specErr)
		reason := fmt.Sprintf("build launch spec error: %v", specErr)
		failRun(reason, reason)
		return
	}
	// PI-073: record the resolved agent type on the RunHandle so that
	// bandwidthTunerBackstop can filter Pi rate-limit events from the global
	// tuner. The type is only known after specBuilder resolves the harness.
	if rh, ok := handles.RunRegistry.Get(runID); ok && rh != nil {
		rh.SetAgentType(shared.ArtifactAgentType(artifacts))
	}
	// hk-j6wm7: record whether this run is a Pi run, for the single-mode
	// post-mortem stderr write below. The worktree retention no longer reads this
	// — the launch marks the run's handle instead, so a graph run reports it too.
	if shared.ArtifactAgentType(artifacts) == core.AgentTypePi {
		runIsPi = true
	}

	// In production HandlerArgs is always nil and spec.Args already contains the
	// bridge flags (--session-id or --resume) from buildClaudeLaunchSpec.
	// For test fixtures that supply HandlerArgs (e.g. ["-c", "exit 0"]), prepend
	// them so that the bridge flags become extra positional args the fixture can
	// safely ignore (e.g. /bin/sh -c "exit 0" sh --session-id <uuid>).
	if len(env.HandlerArgs) > 0 {
		spec.Args = append(env.HandlerArgs, spec.Args...)
	}

	// B10: for remote runs the SSHRunner tunnels liveness probes (pgrep, ps) and
	// commit-detection to the worker host instead of executing them locally.
	var runRunner tmuxpkg.CommandRunner
	if rbc != nil {
		runRunner = rbc.sshRunner
	}

	// noChangeTimeoutCh is declared here so the default switch branch at the
	// post-wait select can read it (nil = no watchdog, treated as an open
	// channel). The deliver hook below is what assigns it (hk-trjef).
	var noChangeTimeoutCh chan struct{}

	// hk-5z1f0: cold-start spawn semaphore, ONE for the whole daemon. Acquire immediately before
	// the remote agent Launch so no more than cap (3) claude cold-starts run
	// concurrently on a single worker — the 2nd (reviewer) cold-start over the
	// reverse tunnel otherwise trips agent_ready_timeout under 6-concurrent remote
	// load. Remote-only (rbc != nil): local runs never construct a tunnel and are
	// never gated. Given back once agent_ready resolves (success/failure/timeout),
	// through the lease below.
	//
	// A local run gets a lease that is born spent: it names the resource, it can
	// never fire, and the give-back site needs no test for whether there is
	// anything to give back (RSM-036).
	spawnSlot := runlease.Hold(runlease.ColdStartToken, nil)
	if rbc != nil && handles.AgentSpawnSem != nil {
		select {
		case handles.AgentSpawnSem <- struct{}{}:
		case <-ctx.Done():
			// ctx cancelled while waiting for a slot — reopen and bail before Launch.
			reason := fmt.Sprintf("cancelled awaiting cold-start spawn slot: %v", ctx.Err())
			failRun(reason, reason)
			return
		}
		// The scope is the backstop the deferred give-back used to be: the token
		// comes back on the readiness edge below, and on a path that never reaches
		// that edge the close returns it. The lease runs the give-back at most
		// once across both, which is what the sync.Once here used to do.
		spawnSlot = runScope.Hold(runlease.ColdStartToken, func() error {
			<-handles.AgentSpawnSem
			return nil
		})
	}

	// RT7: provisioning is complete — start the Run machine so every
	// dispatch-phase failure below rides EvModeOutcome{failure} (RSM-031) and
	// the terminal spine rides the machine.
	bridge.Start(ctx, workflowMode)

	// The launch itself is the ONE path in agentlaunch.go. This site keeps only
	// what to launch (above) and what the exit means (below).
	logPrefix := fmt.Sprintf("daemon: workloop: bead %s run %s", beadID, runID.String())
	launch := runAgentLaunch(ctx, agentLaunchInput{
		Env:           env,
		Ports:         rp,
		Handles:       handles,
		RunID:         runID,
		LogPrefix:     logPrefix,
		Spec:          spec,
		Artifacts:     artifacts,
		WorktreePath:  wtPath,
		DaemonSocket:  agentDaemonSock,
		Runner:        runRunner,
		Remote:        rbc != nil,
		BaseSubstrate: handles.Substrate,
		ConfigurePerRunSubstrate: func(prs *perRunSubstrate) {
			if rbc != nil {
				// B11: wire the offline callback so a mid-run SSH failure emits
				// worker_offline and disables the worker.
				prs.onConnectionFailure = func(c context.Context, detail string) {
					notifyWorkerOffline("liveness", detail)
				}
				// remote-substrate worker-spawn gap: name the tmux session to ENSURE
				// + spawn into ON THE WORKER and the cwd to create it with (the
				// worker's repo_path). Without this the spawn targets box A's local
				// "-default" session — which does not exist on the worker — and the
				// launch wedges at launch_initiated.
				prs.workerSessionName = prs.inner.workerSpawnSessionName(rbc.worker.Name)
				prs.workerSessionCwd = rbc.worker.RepoPath
				return
			}
			// hk-o85ye (Move 3): route a LOCAL run through an independent tmux
			// session so it survives a daemon SIGKILL. Only where the substrate
			// implements runSessionSpawner AND the underlying tmux adapter supports
			// independent session creation (sessionCreator). Adapters that lack it
			// (test stubs, the $TMUX-reuse mode) fall through to the shared-session
			// path — no behavior change for them.
			if env.ProjectDir == "" {
				return
			}
			canIndepSession := false
			if ts, tsOK := handles.Substrate.(*tmuxSubstrate); tsOK {
				_, canIndepSession = ts.adapter.(sessionCreator)
			}
			if _, ok := handles.Substrate.(runSessionSpawner); !ok || !canIndepSession {
				return
			}
			prs.runSessionID = runID.String()
			useIndepSession = true
			// Pre-compute the session name for the registry (best-effort; empty is fine).
			sessName := ""
			if ts, tsOK := handles.Substrate.(*tmuxSubstrate); tsOK {
				if sn, snErr := ts.runSessionName(runID.String()); snErr == nil {
					sessName = sn
				}
			}
			queueIDStr := ""
			if queueID != nil {
				queueIDStr = *queueID
			}
			queueGroupIdx := -1
			if queueGroupIndex != nil {
				queueGroupIdx = *queueGroupIndex
			}
			if writeErr := runpkg.Write(env.ProjectDir, runpkg.Record{
				SchemaVersion: 1,
				RunID:         runID.String(),
				BeadID:        string(beadID),
				QueueName:     queueName,
				QueueID:       queueIDStr,
				GroupIndex:    queueGroupIdx,
				ItemIndex:     queueItemIndex,
				SessionName:   sessName,
				StartedAt:     rp.Clock.Now(),
			}); writeErr != nil {
				// Registry write failed: fall back to the shared-session path (no
				// survive-restart).
				fmt.Fprintf(os.Stderr, "daemon: workloop: run registry write failed for %s: %v (using shared session)\n", runID.String(), writeErr)
				prs.runSessionID = ""
				useIndepSession = false
				return
			}
			// The record exists, so the run holds it. A surviving run leaves it
			// standing — it is how the next boot finds this agent's session by
			// name — and every other ending gives it back. The hold is HERE rather
			// than at the top of the run because a record that was never written
			// is not a resource, and the write is the only place that knows.
			runScope.Hold(runlease.RunRecord, func() error {
				// An absent record is the already-cleaned case, not a failure. Any
				// other failure is retried by the next boot's adoption sweep, so it
				// is reported and not acted on — the same handling
				// adoptDeadRunSessions gives this call.
				if remErr := runpkg.Remove(env.ProjectDir, runID.String()); remErr != nil &&
					!errors.Is(remErr, runpkg.ErrNotFound) {
					return remErr
				}
				return nil
			})
		},
		// hk-wnqos: the single-mode implementer is the terminal/merge spawn — it
		// draws from the reserved +1 slot in spawnSem so a saturated non-terminal
		// pool cannot starve it at launch.
		Terminal: true,
		// Single-mode beadRunOne is always a fresh launch: it has no iteration
		// counter and never issues `claude --resume`.
		IsResume:        false,
		ProbeResume:     false,
		HeartbeatViaTap: true,
		Deliver: func(dctx context.Context, dc agentDeliverCtx) {
			// hk-zlo8: a ProcessExit harness (codex) has no tmux pane and receives
			// its task via argv; pasteInjectOnLaunch would fail with "WriteLastPane:
			// cant find pane" → no_commit in ~4s.
			if dc.ProcessExit {
				return
			}
			// pasteInjectOnLaunch delivers "Please read .harmonik/agent-task.md and
			// begin." to the pane. It MUST run on the post-ready deliver edge (smoke
			// v9 RED, hk-zchbu): pasted before agent_ready, the trailing \n is
			// consumed by Claude Code's welcome-splash render, the text sits in the
			// input bar unsubmitted, and the run hangs. Errors are non-fatal (PL-021d).
			briefDelivered := pasteInjectOnLaunch(dctx, rp.Clock, dc.PasteTarget, artifacts.ClaudeSessionID,
				rc.Phase, rc.IterationCount, wtPath, emit, runID)

			// pasteInjectQuitOnCommit: in interactive TUI mode the Stop hook fires on
			// session exit, not after each response, and a Claude Code agent cannot
			// run a slash command from its tool API — so once the task commit lands
			// the daemon injects `/quit` via tmux send-keys to trigger the hook and
			// unblock the workloop (CHB-028, hk-cmybm). briefDelivered gates the
			// commit poll so a stale pane cannot be /exit-raced (hk-930o3).
			//
			// noChangeTimeoutCh is closed by the watchdog when it kills the session
			// after commitPollTimeout without a new commit; the post-wait switch reads
			// it non-blockingly to tell a forced kill from a genuine agent failure
			// (hk-trjef).
			//
			// hk-37giq: the watchdog MUST take its own tap subscription. Sharing the
			// ready pump's channel let the ready-side drain goroutine steal every
			// heartbeat under concurrent dispatch, wedging the watchdog in its
			// launch-suppression branch forever.
			qs, ok := dc.PasteTarget.(quitSender)
			if !ok {
				return
			}
			noChangeTimeoutCh = make(chan struct{})
			watchdogCh := dc.Tap.Subscribe()
			go pasteInjectQuitOnCommit(ctx, rp.Clock, qs, dc.Session, wtPath, headSHA, noChangeTimeoutCh, briefDelivered, watchdogCh, emit, runID)
		},
		OnLaunchedExtra: func(lctx context.Context, sess handler.Session) {
			// Store the session's lifecycle Machine in the RunHandle so the stale
			// watcher can read the current state and drive Ready→Failed(silent_hang)
			// before emitting run_stale (hk-xrygh iter-2).
			if handle, ok := handles.RunRegistry.Get(runID); ok {
				handle.SetMachine(sess.Machine())
			}
			// hk-xnnd: register the implementer identity on the comms bus so peers
			// can attribute escalation messages sent under "<beadID>-impl". Retired
			// by the defer registered below, which fires on every exit path.
			emitImplPresence(lctx, emit, beadID, core.AgentPresenceStatusOnline, core.AgentPresenceReasonJoin)
		},
		// The launch's own resources — its hook session and its agent session —
		// hang in a scope nested inside this run's, because a graph run takes one
		// of each per node while it holds one worktree for the whole run
		// (RSM-038). Nesting is also what makes the session die before the
		// worktree is removed, without either site knowing about the other.
		RunScope: runScope,
		// The launch reads the run's exit facts through this, rather than through
		// the two skip predicates that used to sit here. Those were the last
		// per-site predicates: the abort kill and the teardown each carried their
		// own copy of `useIndepSession && ctx.Err() != nil`, so a wrong spelling
		// at one was invisible to the other. Both now read the one answer
		// runlease.Decide gives (RSM-037).
		//
		// It is wrapped rather than passed directly because runExit is a VARIABLE
		// this run re-points once the post-launch facts exist. Passing its current
		// value would freeze the launch on whatever the run knew at this line.
		RunExit: func() runlease.Exit { return runExit() },
		// hk-5z1f0: agent_ready has resolved (or was skipped) — the cold-start
		// window is over, so give the token back rather than holding it for the
		// whole run body. The scope's close still covers the paths that never
		// reach this edge.
		AfterReadyResolved: func() {
			//nolint:errcheck // the give-back is a receive on a channel this run filled; it cannot fail
			_ = spawnSlot.Release()
		},
	})

	// The resolved harness identity stamps the run-terminal diagnostic. It must
	// be set before any failRun below, which reads sdHarness.
	if launch.Harness != nil {
		sdHarness = string(launch.Harness.AgentType())
	}

	sess := launch.Session
	watcher := launch.Watcher

	switch launch.Fail {
	case agentLaunchPrelaunchFailed:
		// A pre-launch guard refused: srt engagement verification, the srt argv
		// wrap, or the D2 remote-credential check. No session was created.
		reason := launch.FailErr.Error()
		failRun(reason, reason)
		return false
	case agentLaunchErrored:
		reason := fmt.Sprintf("launch error: %v", launch.FailErr)
		failRun(reason, reason)
		// succeeded is never assigned before this point, so the explicit false is
		// byte-equivalent to the pre-RT14 naked return (nakedret).
		return false
	case agentLaunchReadyTimeout, agentLaunchOK:
		// A session exists; register its cleanup defers before deciding.
	}

	// hk-j6wm7: on a Pi FAILURE, persist the session's stderr tail alongside the
	// captured stdout so the fast-fail error output survives with the retained
	// worktree. Registered here so it runs BEFORE the deferred wtCleanup (LIFO:
	// wtCleanup was registered earlier at the worktree-factory step). On success
	// this is a no-op — the worktree is cleaned up so no capture is needed.
	// sess.Outcome() blocks until the run's Wait completes, which has already
	// happened by the time this defer fires.
	if runIsPi && launch.PiCaptureDir != "" {
		capturedSess := sess
		capturedCaptureDir := launch.PiCaptureDir
		defer func() {
			if bridge.Success() {
				return
			}
			if capturedSess == nil {
				return
			}
			tail := capturedSess.Outcome().StderrTail
			if len(tail) == 0 {
				return
			}
			stderrPath := filepath.Join(capturedCaptureDir, "pi-stderr.log")
			if wErr := os.WriteFile(stderrPath, tail, 0o644); wErr != nil { //nolint:gosec // G306: post-mortem log, not a secret
				fmt.Fprintf(os.Stderr, "daemon: workloop: hk-j6wm7: write pi-stderr.log: %v\n", wErr)
			}
		}()
	}

	// Stop the heartbeat and tear the session down, in that order. Deferred
	// rather than run inside the launch so the CHB-019 heartbeat keeps beating
	// through the whole post-run spine below — it is what holds the stale
	// watcher's dead-process reap off a long merge or no-commit inspection.
	// Registered after the Pi capture defer so, under LIFO, teardown still runs
	// BEFORE that defer reads sess.Outcome().
	defer launch.Cleanup()

	// hk-xnnd: retire the implementer identity on the comms bus. The join is
	// emitted by the launch's onLaunched hook; this defer fires the leave on
	// every exit path (normal, abort, error).
	defer func() {
		emitImplPresence(context.Background(), emit, beadID, core.AgentPresenceStatusOffline, core.AgentPresenceReasonLeave)
	}()

	if launch.Fail == agentLaunchReadyTimeout {
		// RT7 / RSM-031 row 1: the ready-timeout Dispatch terminal maps onto the
		// Run reopen spine (reopen "agent_ready_timeout" + run_failed).
		failRun("agent_ready_timeout", "agent_ready_timeout")
		// succeeded is never assigned before this point, so the explicit false is
		// byte-equivalent to the pre-RT14 naked return (nakedret).
		return false
	}

	socketOutcome, ei := launch.SocketOutcome, launch.Exit

	// The post-exit interpretation's shared opening is the ONE path in
	// agentlaunch.go, the way the launch above is: the HC-065 terminal
	// transition, the implementer_phase_complete emit, the abort check and the
	// process-exit commit fallback. Everything below the call is single mode's
	// own — the terminal classification, the spine, the two guards and the
	// shutdown drain — and none of it has a graph counterpart.
	postExit := runAgentPostExit(ctx, agentPostExitInput{
		Env:          env,
		Ports:        rp,
		RunID:        runID,
		BeadID:       beadID,
		LogPrefix:    logPrefix,
		Launch:       launch,
		Runner:       runRunner,
		WorktreePath: wtPath,
		// Single mode measures HEAD advance against the repo HEAD its worktree
		// was cut from. That value cannot be empty, so this side needs no probe
		// and carries no probe error.
		BaselineSHA: headSHA,
		AgentType:   shared.ArtifactAgentType(artifacts),
		Implementer: true,
		// hk-0z5x: the per-run abort. The never-spawned reaper in StaleWatcher
		// cancels this run's context because launch_initiated was observed but
		// agent_ready never arrived. The kill-consumer backstop and the fast
		// dead-process reap latch the same flag before they cancel, so every
		// reaper arrives here.
		//
		// Daemon-wide shutdown cancels the same context and does NOT latch that
		// flag, and single mode must fall THROUGH on it: the terminal switch
		// below drains committed-but-unmerged work (hk-dnrg) and leaves a
		// surviving run's bead in_progress for QM-002a recovery. So the empty
		// answer here is the shutdown case, not an oversight.
		CancelReason: func() string {
			if handle, ok := handles.RunRegistry.Get(runID); ok && handle.Aborted() {
				return "never_spawned_reaper: launch_initiated but agent_ready not received within deadline"
			}
			return ""
		},
	})
	if postExit.CancelReason != "" {
		// RT7 / RSM-031 row 1b: the never-spawned-reaper abort is the Aborted
		// dispatch-terminal class; its reason rides the mode-failure event
		// (reopen + run_failed via the spine, Background ctx per RSM-022).
		//
		// The run reports the phase it was killed in, because the shared region
		// emits implementer_phase_complete before it consults this (hk-aekon).
		failRun(postExit.CancelReason, postExit.CancelReason)
		// succeeded is never assigned anywhere in this function, so the explicit
		// false is byte-equivalent to a naked return (nakedret).
		return false
	}

	// Step 8: map Wait-return to a terminal event (CHB-020 branches 1/2/3).
	term := handler.MapWaitReturnToTerminalEvent(
		artifacts.HandlerSessionID, ei.ExitCode, ei.WaitErr, socketOutcome,
	)

	// Step 9: emit terminal event and close or reopen the bead.
	//
	// Bridge-wired path (CHB-020): the terminal event type drives the decision.
	// When no stop-hook outcome arrived (branch 3) AND the handler exited 0
	// without a watcher error, we fall back to the pre-bridge close-on-exit-0
	// heuristic so that existing test fixtures (shell scripts that exit 0) and
	// twin-blind runs continue to work as expected.
	//
	// The fallback does NOT apply when a stop-hook outcome was observed but
	// contained FAILURE_SIGNAL (branch 2), or when the watcher itself failed
	// (malformed NDJSON, panic, line-too-long) — those are genuine failures.
	//
	// hk-wfbxf: CloseBead errors must not be silently discarded. If CloseBead
	// fails the bead remains in_progress while JSONL would record
	// run_completed=true — split-brain. Emit run_failed instead.
	// Substrate path: watcher is nil; treat as no watcher error.
	var watcherErr error
	if watcher != nil {
		watcherErr = watcher.Err()
	}
	watcherFailed := watcherErr != nil && !isWatcherErrCanceled(watcherErr)
	transitionTID, _ := handles.TIDGen.Next()

	// RT7: wire the single-mode terminal-spine hooks (gate → code-sync → merge →
	// close/reopen) now that the merge-window context is in scope (runbridge.go).
	bridge.WireSpine(runloop.SpineArgs{
		RunRunner:       runRunner,
		WTPath:          wtPath,
		HeadSHA:         headSHA,
		PreMergeSync:    preMergeSync,
		MPort:           mport,
		ActiveRepo:      activeRepo,
		ProtectBranches: effectiveMergeProtectBranches,
		TransitionTID:   transitionTID,
		EmitBeadClosed: func(c context.Context) {
			emitBeadClosedAndMaybeEpic(c, rp, handles, runID, beadID)
		},
		MergeTarget: mergeTarget, // hk-lgykq: per-bead integration-branch landing target (resolved baseBranch w/ fallback)
	})

	// ── Implementer-escaped-worktree guard (hk-6zylj) ─────────────────
	//
	// Defense-in-depth check for implementer cross-contamination: after the
	// implementer exits, inspect the MAIN repo's working tree. If dirty
	// paths exist outside the normal harmonik churn allowlist
	// (.harmonik/, .claude/, .beads/issues.jsonl), the implementer wrote
	// files into the main repo via absolute MAIN-repo paths instead of
	// staying inside its worktree — its run branch will have no commit
	// but main is now dirty. Layer 1 of the fix (worktree-discipline
	// guidance injected into agent-task.md) prevents the escape at the
	// source; this Layer 2 check catches escapes that slip past Layer 1
	// and fails the run loudly instead of letting it appear as a
	// silent no-commit run.
	//
	// Fires BEFORE the no-commit guard so that escape (the more specific
	// failure mode) is reported as such rather than as a generic
	// "no commit" failure. We do NOT auto-restore main: forensic state is
	// more useful than a clean tree, and the operator can recover via
	// `git -C <main> diff` + manual cherry-pick or via the
	// /tmp/escape-recovery.patch pattern.
	//
	// Bead: hk-6zylj, hk-zguy6.
	//
	// hk-zguy6 / RSM-018: run the escape check inside the merge exclusion domain
	// (mergeq) so that a sibling's commit-phase update-ref → reset-hard sequence
	// cannot race with this read. The commit phase runs via the same queue, so
	// while this read-only slot holds the domain no sibling can be in a transient
	// dirty state — race-free without any path-exclusion heuristic.
	//
	// Bead: hk-6zylj, hk-zguy6, hk-xux36.
	var mainDirty bool
	var dirtyFiles []string
	var escapeErr error
	if subErr := mport.Submit()(ctx, "escape-check", func(qctx context.Context) error {
		mainDirty, dirtyFiles, escapeErr = runmerge.CheckMainWorkingTreeDirty(qctx, activeRepo, preRunUntracked)
		return nil
	}); subErr != nil {
		// Domain unavailable (shutdown) — treat as an errored check (no escape flag).
		escapeErr = subErr
	}
	if escapeErr == nil && mainDirty {
		// The full-payload durable event is emitted here (the machine's reopen
		// spine then carries the classified reason, RSM-031/033 row 2 — the
		// guards run for every dispatch-terminal class, so escape maps onto the
		// mode-failure edge rather than the close-class-only Guarding phase).
		emitImplementerEscapedWorktree(ctx, emit, runID, beadID, activeRepo, dirtyFiles)
		failReason := fmt.Sprintf("implementer_escaped_worktree: %d file(s) dirty in main: %s",
			len(dirtyFiles), strings.Join(dirtyFiles, ", "))
		failRun(failReason, failReason)
		return
	}

	// ── No-commit guard (hk-mmh8f) ────────────────────────────────────
	//
	// Mirror of the review-loop no-commit guard (hk-9c1v4, reviewloop.go).
	// If the single-mode implementer exits without advancing the worktree
	// HEAD past parentSHA, there is no work to merge or close.  Previously
	// this fell through to the auto-close branch (mergeRes.noChange=true
	// → outcome_emitted=approved + bead_closed + run_completed success=true)
	// even though no code was produced.
	//
	// Per EM-015d (implementer MUST advance HEAD): short-circuit with a
	// failed run when HEAD == headSHA.
	//
	// Bead: hk-mmh8f.
	//
	// REMOTE: route the worktree-HEAD probe via runRunner so the no-commit guard
	// reads the WORKER's run-branch HEAD (nil runRunner ⇒ box-A-local, NFR7). The
	// noCommitGuardShouldReopen checks if THIS bead's code landed in the target
	// repo's main branch (cross-repo: activeRepo; local: env.ProjectDir).
	//
	// The probe error is its own terminal, not a reason to skip the guard
	// (hk-fmere). The two questions used to share one condition — "did the probe
	// succeed" AND "should the run reopen" — so a probe that errored answered the
	// first `false`, the whole condition went false, and the guard never ran. An
	// exit-0 run that produced nothing then fell through to the clean-exit
	// classification below, merged as no-change, and CLOSED the bead green. A
	// guard whose own instrument is broken must refuse, never pass.
	//
	// specs/execution-model.md EM-058 component C states the obligation for both
	// modes: "A worktree whose HEAD cannot be resolved at all is a daemon-side
	// error in BOTH modes (a broken worktree is a real failure)." The graph node
	// already refuses at the same probe, before and after its launch.
	//
	// The graph expresses that refusal as `return core.Outcome{}, err`, because a
	// node has a caller — driveDotWorkflow — that turns the error into a run
	// failure. Single mode has no such caller: this region IS the run's terminal
	// decision, so its equivalent of a hard fail is the reopen spine, the same
	// terminal the escape guard above and the no-commit guard below already take.
	// The bead goes back to the queue rather than closing on an unread fact.
	//
	// The refusal is scoped to a LIVE run. A cancelled ctx fails this probe too,
	// and there the failure says the daemon is going away, not that the worktree
	// is broken. Reopening on it would hand a surviving run's bead to a second
	// agent while the first one is still working it.
	curHeadSHA, curHeadErr := gitprobe.ResolveWorktreeHEADVia(ctx, runRunner, wtPath)
	if curHeadErr != nil && ctx.Err() == nil {
		failReason := fmt.Sprintf("worktree_head_unreadable: resolve worktree HEAD after implementer exit=%d: %v", ei.ExitCode, curHeadErr)
		failRun(failReason, failReason)
		// succeeded is never assigned anywhere in this function, so the explicit
		// false is byte-equivalent to a naked return (nakedret).
		return false
	}
	// The shutdown terminal is the switch below's to decide, and it leaves the
	// bead alone: it drains committed-but-unmerged work (hk-dnrg) and leaves a
	// surviving run's bead in_progress for QM-002a. curHeadSHA is empty when the
	// probe failed, and the guard cannot fire on an empty SHA because a real
	// parent is never the empty string, so a failed probe never reopens here.
	if noCommitGuardShouldReopen(ctx, activeRepo, curHeadSHA, headSHA, beadID) {
		// hk-4ie1z: the implementer's worktree HEAD never advanced past the
		// parent (NO commit) AND this bead's own work is not on main. The prior
		// escape hatch (hk-cwxow) bypassed the guard whenever refs/heads/main had
		// moved at all — but under concurrent/wave dispatch a SIBLING bead merging
		// to main satisfies "main moved" while THIS bead's code is still absent,
		// so a genuine no-commit run was falsely closed as success (hk-tigaf.4).
		// The positive per-bead Refs-trailer check inside noCommitGuardShouldReopen
		// replaces "did main move?" with "did THIS bead land?". Removing the escape
		// does NOT reintroduce the hk-cwxow false-`non_ff`: mergeRunBranchToMain
		// independently short-circuits to noChange when runTip == headSHA
		// (workloop.go ~2804), regardless of where main points. This mirrors the
		// review-loop no-commit guard (reviewloop.go ~567), which never had the
		// escape.
		failReason := fmt.Sprintf("no_commit_during_implementer: HEAD did not advance past parent %s at iteration 1 exit=%d", headSHA, ei.ExitCode)
		failRun(failReason, failReason)
		return
	}

	// ── RT7: dispatch-terminal classification → the Run machine ──────────────
	//
	// The shell classifies the Dispatch terminal (CHB-020 branches + the frozen-
	// commit watchdog) and synthesizes the corresponding Run event (A1 §3); the
	// machine then owns the gate → code-sync → merge → close/reopen tail via the
	// spine hooks wired above.
	switch {
	case term.Type == handlercontract.ProgressMsgTypeAgentCompleted:
		// CHB-020 branch 1: stop-hook WORK_COMPLETE or REVIEWER_VERDICT. Latches
		// path label "agent_completed" + its close summary (RSM-033).
		bridge.Feed(ctx, runexec.Event{Kind: runexec.EvAgentCompleted, Detail: "agent_completed: stop-hook outcome"})

	case socketOutcome == nil && ei.ExitCode == exitCodeClean && !watcherFailed:
		// No stop-hook arrived AND handler exited 0 without watcher error: the
		// pre-bridge close-on-exit-0 heuristic for twin-blind runs. Latches
		// path label "auto-close" (RSM-033).
		bridge.Feed(ctx, runexec.Event{Kind: runexec.EvCleanExit, Detail: "auto-close: exit=0"})

	default:
		// noChange-timeout path (hk-trjef): pasteInjectQuitOnCommit killed the
		// session after commitPollTimeout fired without a new commit.  Check whether
		// the bead was already subsumed by a prior run that landed on main.
		select {
		case <-noChangeTimeoutCh:
			if shared.MainHistoryHasRefsTrailer(ctx, activeRepo, beadID) {
				// RSM-035: subsumed-but-stalled closes with an approved outcome;
				// the emit-approved flag + close summary ride the event.
				bridge.Feed(ctx, runexec.Event{
					Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
					EmitOutcome: true, Detail: "noChange-subsumed: bead found in main",
				})
			} else {
				// The reopen hook applies the hk-e3fy Background fallback when the
				// stale watcher cancelled the per-run ctx (RSM-022).
				failRun("noChange-timeout", "noChange-timeout: no commit in commitPollTimeout window")
			}
		default:
			// hk-ly0hg Fix-1: context-cancel path — daemon is shutting down.
			if ctx.Err() != nil {
				// hk-o85ye: independent session path — leave session and worktree alive.
				// The session survives SIGKILL; on next boot the adoption pass detects
				// it, waits for Claude to finish, then resets the bead for re-dispatch.
				// The deferred cleanup (wtCleanup, runlaunch.ForceTeardownSession) is skipped by
				// the useIndepSession guard. No ReopenBead: bead stays in_progress so
				// QM-002a on next boot leaves the queue item dispatched (alive) ✓.
				if useIndepSession {
					return
				}
				// hk-dnrg: drain committed-but-unmerged runs on shutdown instead of
				// abandoning. A run that already committed in its worktree must be
				// merged before exit — abandoning it causes re-dispatch (wasted or
				// duplicated work) on next boot. RT9 / RSM-021: the drain rides the
				// machine's EvShutdownDrain edge; the background-context and
				// no-sessiondata policies live in the effector (runbridge), the
				// drain summaries + requeue-recovery reopen reason in the machine.
				// No commit (or HEAD probe failure) feeds an empty SHA → the
				// requeue reopen with no run terminal (QM-002a reverts the queue
				// item to pending at next startup, hk-ly0hg Fix-1 / hk-1h5q).
				drainSHA := ""
				if curHeadSHA, headErr := gitprobe.ResolveWorktreeHEAD(context.Background(), wtPath); headErr == nil && curHeadSHA != "" && curHeadSHA != headSHA {
					drainSHA = curHeadSHA
				}
				bridge.Drain(ctx, drainSHA)
				return bridge.Success()
			}

			// CHB-020 branch 2 (FAILURE_SIGNAL), branch 3 with non-zero exit, or
			// watcher failure (malformed NDJSON, panic, etc.).
			var failReason string
			if watcherFailed {
				failReason = fmt.Sprintf("watcher error: %v exit=%d run_id=%s",
					watcherErr, ei.ExitCode, runID.String())
			} else if term.SubReason != "" {
				failReason = fmt.Sprintf("agent_failed class=%s sub_reason=%s exit=%d run_id=%s",
					term.Class, term.SubReason, ei.ExitCode, runID.String())
			} else {
				failReason = fmt.Sprintf("exit=%d run_id=%s", ei.ExitCode, runID.String())
			}
			// Surface stderr tail when available — helps diagnose exit=-1 crashes
			// where the agent produced no NDJSON output (hk-ajhqw).
			if len(ei.StderrTail) > 0 {
				const maxTailInReason = 200
				tail := ei.StderrTail
				truncated := ""
				if len(tail) > maxTailInReason {
					tail = tail[len(tail)-maxTailInReason:]
					truncated = " (truncated)"
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s stderr tail%s:\n%s\n",
					beadID, runID.String(), truncated, tail)
				failReason += fmt.Sprintf(" stderr_tail%s=%q", truncated, tail)
			}
			failRun(failReason, "auto-reopen: "+failReason)
		}
	}
	return bridge.Success()
}

// isWatcherErrCanceled reports whether err is the ErrCanceled sentinel that
// the watcher sets when the session context is cancelled cleanly (not a
// genuine watcher failure).
//
// This mirrors the pre-bridge check in the original single-mode path:
// "watcherFailed := watcherErr != nil && !errors.Is(watcherErr, handlercontract.ErrCanceled)"
//
// Bead ref: hk-gql20.14.
func isWatcherErrCanceled(err error) bool {
	return errors.Is(err, handlercontract.ErrCanceled)
}

// noCommitGuardShouldReopen decides whether the single-mode no-commit guard
// must fail the run as `no_commit` and reopen the bead.
//
// It returns true when BOTH:
//   - the implementer's worktree HEAD never advanced past the parent
//     (curHeadSHA == parentSHA → no commit was produced), AND
//   - THIS bead's own work is not already on main (no `Refs: <beadID>` trailer
//     in the recent main history).
//
// This is the positive per-bead replacement for the buggy hk-cwxow
// `mainAdvanced` escape, which asked "did refs/heads/main move at all?" — a
// question that a SIBLING bead landing concurrently answers `true` even though
// THIS bead's code never landed, falsely closing a genuine no-commit run as
// success (hk-4ie1z, observed live on hk-tigaf.4). The only legitimate
// fall-through (the run made no commit, but the bead's work is genuinely on
// main because a prior run subsumed it) is preserved via
// shared.MainHistoryHasRefsTrailer. Mirrors the review-loop guard
// (reviewloop.go ~567), which compares HEAD == parentSHA with no escape.
//
// Bead: hk-4ie1z.
func noCommitGuardShouldReopen(ctx context.Context, projectDir, curHeadSHA, parentSHA string, beadID core.BeadID) bool {
	if curHeadSHA != parentSHA {
		// The implementer advanced HEAD — a commit exists; the guard does not fire.
		return false
	}
	// No commit. Fail (reopen) UNLESS this bead's own work is already on main.
	return !shared.MainHistoryHasRefsTrailer(ctx, projectDir, beadID)
}

// productionWorktreeFactory is the default worktreeFactory: creates a real git
// worktree under the project's .harmonik/worktrees/ directory and returns the
// path plus a cleanup function that removes it.
//
// After the git worktree is ready, it symlinks <projectDir>/.tools into the
// worktree so that Makefile fmt/lint targets (gci, golangci-lint, gofumpt) can
// resolve their pinned binaries. .tools/ is gitignored and only installed in the
// main repo; without this link agents skip formatting silently and codex
// auto-commits land unchecked (hk-gb3ln). The symlink itself is ignored by
// /.tools in the root .gitignore (no trailing slash) so git add -A in the
// worktree never stages it. Creation is best-effort: if .tools/ is absent from
// projectDir (e.g. a fresh clone without `make tools`) the worktree still
// launches normally.
//
// Bead ref: hk-kqdpf.1, hk-gb3ln.
func productionWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	if err := workspace.CreateWorktree(ctx, projectDir, runID, headSHA, workspace.NoWorktreeRootOverride()); err != nil {
		return "", nil, err
	}
	wtPath := workspace.WorktreePath(projectDir, runID, workspace.NoWorktreeRootOverride())

	// Symlink .tools from the project root into the worktree so Makefile
	// fmt/lint targets resolve their pinned binaries (hk-gb3ln).
	toolsSrc := filepath.Join(projectDir, ".tools")
	if _, statErr := os.Lstat(toolsSrc); statErr == nil {
		_ = os.Symlink(toolsSrc, filepath.Join(wtPath, ".tools"))
	}

	// The cleanup uses background context so removal is attempted even when the
	// per-bead context has been cancelled (e.g. on daemon shutdown or test
	// cancellation). This mirrors the intent of the original `defer removeWorktree`
	// call — git worktree prune is best-effort.
	cleanup := func() {
		if cleanupErr := runmerge.RemoveWorktree(context.Background(), projectDir, wtPath); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: worktree reclaim failed for run %s at %s; "+
				"the worktree remains because cleanup failed, not because evidence was retained: %v\n",
				runID, wtPath, cleanupErr)
		}
	}
	return wtPath, cleanup, nil
}

// resolveHEAD resolves the current HEAD commit SHA of the git repository at
// repoRoot. Used as the parent-commit start-point for CreateWorktree.
func resolveHEAD(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD: %w", err)
	}
	sha := string(out)
	// Trim trailing newline.
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD returned empty output")
	}
	return sha, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Event helpers
// ─────────────────────────────────────────────────────────────────────────────

// workloopRunCompletedPayload is the minimal run_completed / run_failed payload
// emitted by the work loop.
//
// QueueID and QueueGroupIndex are optional: set when the run was dispatched
// from a queue submission per QM-011 / QM-012 (EM-015b).
//
// WorktreeTipSHA is set on run_failed when the implementer's HEAD advanced past
// the parent (a commit was produced) but the run still failed — e.g. when the
// commit_gate bounced a valid commit into a no-progress loop. Operators can use
// this SHA to salvage the committed work from the stranded run branch (hk-8b35c).
//
// BeadID, OwningEpicID, and OwningEpicAssignee are denormalized attribution fields
// (hk-7evda, logmine F13) that eliminate captain br round-trips after observing a
// terminal event.
type workloopRunCompletedPayload struct {
	RunID              string  `json:"run_id"`
	BeadID             string  `json:"bead_id"`
	Success            bool    `json:"success"`
	Summary            string  `json:"summary"`
	EndedAt            string  `json:"ended_at"`
	OwningEpicID       *string `json:"owning_epic_id,omitempty"`
	OwningEpicAssignee *string `json:"owning_epic_assignee,omitempty"`
	QueueID            *string `json:"queue_id,omitempty"`
	QueueGroupIndex    *int    `json:"queue_group_index,omitempty"`
	WorktreeTipSHA     *string `json:"worktree_tip_sha,omitempty"`
}

type d2Refusal string

//nolint:gosec // G101: refusal text names an environment variable; it contains no credential.
const d2APIKeyRefusal d2Refusal = "remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)"

// d2RemoteAPIKeyRefusal makes the post-build, pre-launch D2 decision. Keeping
// the remote/local distinction in this predicate makes every harness use the
// same fail-closed behavior without coupling the decision to an agent type.
func d2RemoteAPIKeyRefusal(remote bool, env []string) (d2Refusal, bool) {
	if remote && hasAPIKeyInEnv(env) {
		return d2APIKeyRefusal, true
	}
	return "", false
}

// hasAPIKeyInEnv reports whether any element of env would forward a *live*
// ANTHROPIC_API_KEY to a remote worker. Used by the D2 fail-closed check to
// prevent billing the worker's own API quota (B10).
//
// Two forms are dangerous and must be refused:
//   - "ANTHROPIC_API_KEY=<value>" with a non-empty value — an explicit live key.
//   - "ANTHROPIC_API_KEY" (bare, no '=') — inherits the daemon process's value.
//
// The empty-override form "ANTHROPIC_API_KEY=" (value after '=' is empty) is
// SAFE and must NOT be refused: ClaudeEnvVars always appends it as a CI-003
// credential-zeroing override (specs/credential-isolation.md §4 CI-003), so it
// appears in spec.Env for *every* launch, local or remote. Treating that empty
// override as a live key would fail-close every remote dispatch (the B12
// localhost e2e surfaced exactly this).
func hasAPIKeyInEnv(env []string) bool {
	for _, e := range env {
		if e == "ANTHROPIC_API_KEY" {
			// Bare key with no '=' → inherits the parent process value. Refuse.
			return true
		}
		if v, ok := strings.CutPrefix(e, "ANTHROPIC_API_KEY="); ok && v != "" {
			// KEY=<non-empty> → a live key. Refuse. KEY= (empty) is the CI-003
			// zeroing override and is safe to forward.
			return true
		}
	}
	return false
}

func emitRunStarted(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	queueID *string,
	queueGroupIndex *int,
	descriptor core.WorkflowDescriptor,
	workflowMode core.WorkflowMode,
	reviewPolicy core.ReviewPolicy,
	selectionSource core.WorkflowSelectionSource,
	workerName *string,
	workerOS *string,
) {
	pl := core.RunStartedPayload{
		RunID:                   runID,
		WorkflowID:              descriptor.WorkflowID,
		WorkflowVersion:         descriptor.WorkflowVersion,
		WorkflowMode:            workflowMode,
		ReviewPolicy:            reviewPolicy,
		WorkflowSelectionSource: selectionSource,
		BeadID:                  &beadID,
		WorkspacePath:           wtPath,
		InputRef:                "bead:" + string(beadID),
		StartedAt:               time.Now().UTC(),
		WorkerName:              workerName,
		WorkerOS:                workerOS,
		QueueID:                 queueID,
		QueueGroupIndex:         queueGroupIndex,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b)
}

func emitRunCompleted(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID, owningEpicID, owningEpicAssignee string, success bool, summary string, queueID *string, queueGroupIndex *int, worktreeTipSHA *string) {
	var epicIDPtr, epicAssigneePtr *string
	if owningEpicID != "" {
		epicIDPtr = &owningEpicID
	}
	if owningEpicAssignee != "" {
		epicAssigneePtr = &owningEpicAssignee
	}
	pl := workloopRunCompletedPayload{
		RunID:              runID.String(),
		BeadID:             beadID,
		Success:            success,
		Summary:            summary,
		EndedAt:            time.Now().UTC().Format(time.RFC3339),
		OwningEpicID:       epicIDPtr,
		OwningEpicAssignee: epicAssigneePtr,
		QueueID:            queueID,
		QueueGroupIndex:    queueGroupIndex,
		WorktreeTipSHA:     worktreeTipSHA,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	eventType := core.EventTypeRunCompleted
	if !success {
		eventType = core.EventTypeRunFailed
	}
	_ = bus.EmitWithRunID(ctx, runID, eventType, b)
}

// emitImplPresence emits an agent_presence event for a daemon-spawned implementer
// so that peers on the comms bus can attribute and route messages from the identity
// "<beadID>-impl" (hk-xnnd). Errors are silently swallowed — presence is
// best-effort and must not gate the run lifecycle.
func emitImplPresence(ctx context.Context, bus handlercontract.EventEmitter, beadID core.BeadID, status core.AgentPresenceStatus, reason core.AgentPresenceReason) {
	pl := core.AgentPresencePayload{
		Agent:    string(beadID) + "-impl",
		Status:   status,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
		Reason:   reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventType("agent_presence"), b)
}

// resolveOwningEpicFromRecord scans beadRecord.Edges for a parent-child edge
// where the bead is the child and returns the parent epic's bead ID and assignee.
// Returns ("", "") when no parent-child edge is found.
// Returns (epicID, "") when the epic exists but the br show call fails or the
// epic has no assignee. Best-effort: errors are silently swallowed.
// Bead ref: hk-7evda (logmine F13 — kill attribution round-trips).
func resolveOwningEpicFromRecord(ctx context.Context, br beadLedger, record core.BeadRecord) (epicID, assignee string) {
	for _, e := range record.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.FromBeadID == record.BeadID {
			epicID = string(e.ToBeadID)
			break
		}
	}
	if epicID == "" {
		return "", ""
	}
	epicRecord, err := br.ShowBead(ctx, core.BeadID(epicID))
	if err != nil {
		return epicID, ""
	}
	return epicID, epicRecord.Assignee
}

// ─────────────────────────────────────────────────────────────────────────────
// bead-closed / epic-completion emission (the merge path itself moved to
// internal/runmerge in P2 unit E5 RT13; these helpers stay in the daemon shell
// but now reach the emittedEpics dedupe set / mutex and the ledger+emitter
// through the SharedHandles + RunPorts bundles rather than raw legacy aggregate,
// so the runBridge close hook can drop deps entirely (RT18.9)).
// ─────────────────────────────────────────────────────────────────────────────

// beadClosedPayload is the JSON payload for the bead_closed event.
type beadClosedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
}

// epicCompletedPayload is the JSON payload for the epic_completed event (hk-w6y70).
type epicCompletedPayload struct {
	EpicID          string `json:"epic_id"`
	LastChildBeadID string `json:"last_child_bead_id"`
	ClosedAt        string `json:"closed_at"`
}

// emitBeadClosed emits a bead_closed event after a successful CloseBead call.
//
// Spec ref: specs/execution-model.md §4.12.EM-052.
// Bead: hk-ftyvo.
func emitBeadClosed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID) {
	pl := beadClosedPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeBeadClosed, b)
}

// emitBeadClosedAndMaybeEpic emits bead_closed then checks whether the closed
// bead's parent epic just completed (hk-w6y70 C1). It is the single insertion
// point replacing the seven raw emitBeadClosed call sites.
func emitBeadClosedAndMaybeEpic(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, beadID core.BeadID) {
	emitBeadClosed(ctx, ports.Emitter, runID, beadID)
	maybeEmitEpicCompleted(ctx, ports, handles, runID, beadID)
}

// maybeEmitEpicCompleted checks whether closedBeadID's parent epic now has all
// children closed, and if so emits epic_completed exactly once (AC-1 at-most-once
// per daemon session). Zero-emit on: no parent (AC-4), still-open sibling (AC-3),
// or already-emitted guard hit.
//
// Bead: hk-w6y70.
func maybeEmitEpicCompleted(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, closedBeadID core.BeadID) {
	ledger := ports.Ledger
	// Step 1: ShowBead(closedBead) to find the parent via a parent-child edge.
	// The closed bead's outgoing parent-child edge has FromBeadID == closedBead,
	// ToBeadID == parent (per brcli/show.go: dependencies[] → outgoing edges).
	closedRecord, err := ledger.ShowBead(ctx, closedBeadID)
	if err != nil {
		return
	}

	var parentID core.BeadID
	for _, e := range closedRecord.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.FromBeadID == closedBeadID {
			parentID = e.ToBeadID
			break
		}
	}
	if parentID == "" {
		// AC-4: no parent → zero emit.
		return
	}

	// Step 2: ShowBead(parent) to enumerate all children and check their statuses.
	// Incoming parent-child edges on the parent have ToBeadID == parent,
	// FromBeadID == child (per brcli/show.go: dependents[] → incoming edges).
	parentRecord, err := ledger.ShowBead(ctx, parentID)
	if err != nil {
		return
	}

	for _, e := range parentRecord.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.ToBeadID == parentID {
			// Terminal, not merely closed. A tombstoned child is finished — it
			// will never close, so testing Closed alone let one tombstone
			// suppress epic_completed for its parent forever and silently stop
			// the lane waiting on it.
			if !e.EndpointStatus.IsTerminal() {
				// AC-3: at least one child is not terminal → zero emit.
				return
			}
		}
	}

	// Every child is terminal (or there are none — edge case: epic with no
	// children recorded yet; we emit to avoid silent gaps, consistent with AC-1).

	// Step 3: claim under emittedEpicsMu BEFORE emit (at-most-once guard AC-1).
	handles.EmittedEpicsMu.Lock()
	if _, already := handles.EmittedEpics[parentID]; already {
		handles.EmittedEpicsMu.Unlock()
		return
	}
	handles.EmittedEpics[parentID] = struct{}{}
	handles.EmittedEpicsMu.Unlock()

	// Step 4: emit epic_completed.
	pl := epicCompletedPayload{
		EpicID:          string(parentID),
		LastChildBeadID: string(closedBeadID),
		ClosedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = ports.Emitter.EmitWithRunID(ctx, runID, core.EventTypeEpicCompleted, b)
}

// transitionToTerminated advances the per-session lifecycle Machine from its
// current state to StateTerminated (clean exit) or StateFailed (error exit),
// driving through StateTerminating if needed (HC-065).
//
// Called once the completion wait has returned, by the single-mode tail and by
// each graph node (hk-b4xf2). Transitions that are invalid for the current
// Machine state — a machine already in StateFailed from an agent_failed
// progress-stream event, say — are silently ignored.
//
// It does NOT cover every exit path, and the earlier claim that it did was
// wrong. A launch that never produced a session has no machine to transition,
// which is correct and needs nothing. Two paths DO leave a live session behind
// and still return above this call. An agent_ready timeout does so in both
// modes. A per-run abort does so in single mode only, because the graph node
// checks the cancelled context BELOW this call. Runs that take either path end
// with a machine that never reached a terminal state.
//
// A lifecycle_transition event is emitted to the bus for each successful
// Machine transition. ctx SHOULD be a live (non-cancelled) context so that
// the emission reaches the bus; callers MUST pass context.Background() if
// the run context may already be cancelled.
//
// Spec ref: handler-contract.md §4.13 HC-065; event-model.md §8.3.14.
// Bead ref: hk-xrygh.
func transitionToTerminated(ctx context.Context, m *hclifecycle.Machine, runID core.RunID, bus handlercontract.EventEmitter, exitCode int, waitErr error) {
	if m == nil {
		return
	}
	// Step 1: Terminating (current → Terminating). The machine may already be
	// there (e.g. Kill was called earlier) — the Machine silently rejects
	// invalid transitions.
	emitWorkloopLifecycleTransition(ctx, m, runID, bus,
		hclifecycle.StateTerminating, hclifecycle.ReasonTerminateRequested, "", "")

	// Step 2: Terminal state based on exit outcome.
	if exitCode == 0 && waitErr == nil {
		emitWorkloopLifecycleTransition(ctx, m, runID, bus,
			hclifecycle.StateTerminated, hclifecycle.ReasonTerminateComplete, "", "")
	} else {
		errCode := "exit_error"
		errMsg := fmt.Sprintf("exit=%d", exitCode)
		if waitErr != nil {
			errMsg = waitErr.Error()
		}
		emitWorkloopLifecycleTransition(ctx, m, runID, bus,
			hclifecycle.StateFailed, hclifecycle.ReasonError, errCode, errMsg)
	}
}

// emitWorkloopLifecycleTransition performs a lifecycle Machine transition and
// emits a lifecycle_transition event to the bus (HC-065, §8.3.14).
// Invalid transitions are silently ignored; emission failures are best-effort.
func emitWorkloopLifecycleTransition(ctx context.Context, m *hclifecycle.Machine, runID core.RunID, bus handlercontract.EventEmitter, to hclifecycle.LifecycleState, reason hclifecycle.TransitionReason, errCode, errMsg string) {
	from := m.Current()
	if err := m.Transition(to, reason, errCode, errMsg); err != nil {
		return // invalid transition (e.g. already terminal): silent no-op
	}
	p := core.LifecycleTransitionPayload{
		SessionID:      core.SessionID(m.SessionID()),
		FromState:      from.String(),
		ToState:        to.String(),
		Reason:         string(reason),
		TransitionedAt: time.Now().Format(time.RFC3339Nano),
		ErrCode:        errCode,
		ErrMsg:         errMsg,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeLifecycleTransition, b)
}

// emitImplementerEscapedWorktree emits an implementer_escaped_worktree event
// (hk-6zylj) when the daemon detects post-implementer-exit dirty state in the
// main repo working tree outside the churn allowlist.
func emitImplementerEscapedWorktree(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, mainPath string, dirtyFiles []string) {
	pl := core.ImplementerEscapedWorktreePayload{
		RunID:      runID,
		BeadID:     string(beadID),
		MainPath:   mainPath,
		DirtyFiles: dirtyFiles,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerEscapedWorktree, b)
}
