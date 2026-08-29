//nolint:contextcheck,nakedret // existing orchestration file; contexts and named results encode run lifecycles
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/runregistry"
	"github.com/gregberns/harmonik/internal/sessiondata"
	codesyncpkg "github.com/gregberns/harmonik/internal/transport/codesync"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workspace"
)

const dashboardGateEvalInterval = 30 * time.Second

const diskLowWatermarkDefault uint64 = 10 * 1024 * 1024 * 1024 // 10 GiB

const diskCheckInterval = 10 * time.Minute

type beadLedger = runloop.BeadLedger

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

func newLocalRunRegistry() *runregistry.RunRegistry {
	return runregistry.NewRunRegistry()
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
	var sessionDataWriter sync.WaitGroup
	defer sessionDataWriter.Wait()

	runID, beadRecord := env.RunID, env.BeadRecord
	queueName, queueID := env.QueueName, env.QueueID
	queueGroupIndex, queueItemIndex := env.QueueGroupIndex, env.QueueItemIndex
	mport := rp.Merge
	emit := rp.Emitter
	beadID := beadRecord.BeadID

	runHandle, _ := handles.RunRegistry.Get(runID)

	daemonStopping := func() bool {
		return ctx.Err() != nil && (runHandle == nil || !runHandle.Aborted())
	}

	runScope := &runlease.Scope{}
	runExit := func() runlease.Exit { return runlease.Exit{DaemonStopping: daemonStopping()} }
	defer func() {
		if relErr := runScope.Close(runlease.Decide(runExit())).Err(); relErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s run %s: giving resources back: %v\n",
				beadID, runID.String(), relErr)
		}
	}()

	localSlot := runlease.Hold(runlease.LocalSlot, nil)
	if localSlotHeld && handles.LocalInFlight != nil {
		localSlot = runScope.Hold(runlease.LocalSlot, func() error {
			handles.LocalInFlight.Add(-1)
			return nil
		})
	}

	if preSelectedWorker != nil && handles.Workers != nil {
		runScope.Hold(runlease.WorkerSlot, func() error {
			handles.Workers.ReleaseSlot()
			return nil
		})
	}

	var runTipSHA *string

	owningEpicID, owningEpicAssignee := resolveOwningEpicFromRecord(ctx, handles.BrAdapter, beadRecord)
	if runHandle != nil {
		runHandle.SetOwningEpic(owningEpicID, owningEpicAssignee)
	}

	sdStartedAt := rp.Clock.Now()
	var sdModel, sdHarness string

	emitRunTerminalEff := func(emitCtx context.Context, success bool, summary string, draining bool) { //nolint:contextcheck // hk-e3fy: a cancelled per-run ctx must not drop the terminal; Background swap by design
		if emitCtx.Err() != nil {
			emitCtx = context.Background()
		}
		emitRunCompleted(emitCtx, emit, runID, string(beadID), owningEpicID, owningEpicAssignee, success, summary, queueID, queueGroupIndex, runTipSHA)
		if draining {
			return // RSM-021: the drain batch collects no sessiondata.
		}
		sdEndedAt := rp.Clock.Now()
		sdQID := ""
		if queueID != nil {
			sdQID = *queueID
		}
		sdCommitSHA := ""
		if runTipSHA != nil {
			sdCommitSHA = *runTipSHA
		}
		sessionDataWriter.Add(1)
		go func() {
			defer sessionDataWriter.Done()
			if collectErr := sessiondata.Collect(sessiondata.CollectParams{
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
				ClaudeProjectsDir: workspace.DefaultClaudeProjectsDir(),
			}); collectErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: collect session data for run %s: %v\n", runID, collectErr)
			}
		}()
	}

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

	workflowMode := plan.Workflow.Mode
	resolvedModel, resolvedEffort := plan.Model, plan.Effort
	resolvedProfile := plan.PiProfile
	activeRepo := plan.ActiveRepo
	effectiveMergeProtectBranches := plan.MergeProtectBranches
	headSHA := plan.ParentSHA
	baseBranch := plan.BaseBranch
	mergeTarget := plan.MergeTarget
	sdModel = resolvedModel

	bridge := runloop.NewRunBridge(env, rp, handles, runID, beadID, workflowMode, emitRunTerminalEff)
	failRun := func(reason, summary string) { bridge.Fail(ctx, reason, summary) }

	type remoteBeadCtx struct {
		worker         workers.Worker
		sshRunner      tmuxpkg.CommandRunner
		workerHookSock string
	}
	var rbc *remoteBeadCtx
	if preSelectedWorker != nil {
		rbc = &remoteBeadCtx{
			worker: *preSelectedWorker,
			// hk-zexsj: pin the tmux SSHRunner off the shared SSH ControlMaster.
			sshRunner: tmuxpkg.SSHRunner{Host: preSelectedWorker.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
		}
	}
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
			if h, ok := handles.RunRegistry.Get(runID); ok {
				h.SetRemote(true)
			}
		}
	}

	if rbc != nil {
		daemonHookSock := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")

		tunnelPort, portErr := tunnelpkg.AllocatePort()
		if portErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "port alloc", daemonHookSock, portErr))
			return false
		}
		rbc.workerHookSock = tunnelpkg.WorkerTCPEndpoint(tunnelPort)
		runScope.Hold(runlease.TunnelPort, func() error {
			tunnelpkg.ReleasePort(tunnelPort)
			return nil
		})

		if mkErr := tunnelpkg.EnsureWorkerHarmonikDir(ctx, rbc.sshRunner, rbc.worker.RepoPath); mkErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: tunnel.EnsureWorkerHarmonikDir bead %s run %s: %v (non-fatal; readiness gate is authority)\n",
				beadID, runID.String(), mkErr)
		}

		if lenErr := lifecycle.ValidateSocketPathLength(daemonHookSock); lenErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "socket-path", daemonHookSock, lenErr))
			return false
		}
		tunnelHost, tunnelOpts, hostOK := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if !hostOK {
			tunnelHost = rbc.worker.Host
		}
		tunnelArgs := tunnelpkg.BuildArgs(tunnelPort, daemonHookSock, tunnelHost, tunnelOpts)
		tunnelCmd := tunnelpkg.ReverseTunnelRunner(ctx, "ssh", tunnelArgs...)
		if startErr := tunnelCmd.Start(); startErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: reverse-tunnel start bead %s run %s: %v\n",
				beadID, runID.String(), startErr)
		} else {
			runScope.Hold(runlease.TunnelProcess, func() error {
				_ = tunnelCmd.Process.Kill() //nolint:errcheck // best-effort kill of a tunnel that is ending either way (pre-RT8 idiom)
				_ = tunnelCmd.Wait()         //nolint:errcheck // reaps the killed process; the error is the signal we sent
				return nil
			})
		}

		if waitErr := tunnelpkg.WaitWorkerSocketLive(ctx, rbc.sshRunner, rbc.workerHookSock, tunnelpkg.WorkerSocketReadyTimeout); waitErr != nil {
			refuseRunPlan(ctx, env, handles, emit,
				tunnelRefusal(runID, beadID, rbc.worker, "readiness gate", rbc.workerHookSock, waitErr))
			return false
		}
	}

	notifyWorkerOffline := func(phase, detail string) {
		if rbc == nil {
			return
		}
		workers.EmitWorkerOfflineEvent(ctx, rbc.worker.Name, rbc.worker.Host, phase, detail, emit.Emit)
		if handles.Workers != nil {
			handles.Workers.SetEnabled(false)
		}
	}

	preMergeSync := func(syncCtx context.Context) string {
		if rbc == nil {
			return ""
		}
		workerHost, sshOpts, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
		if err := codesyncpkg.FetchRunBranchBoxA(syncCtx, nil, env.ProjectDir, runID.String(), workerHost, rbc.worker.RepoPath, sshOpts); err != nil {
			if tmuxpkg.IsSSHConnectionFailure(err) {
				notifyWorkerOffline("spawn", fmt.Sprintf("codesync.FetchRunBranchBoxA: %v", err))
			}
			return fmt.Sprintf("fetch run branch from worker on box A: %v", err)
		}
		return ""
	}

	wtFactory := handles.WorktreeFactory
	if wtFactory == nil {
		if rbc != nil {
			sshRunner := rbc.sshRunner
			workerRepoPath := rbc.worker.RepoPath
			wtFactory = func(ctx context.Context, _, runID, headSHA string) (string, func(), error) {
				cfg := workspace.NoWorktreeRootOverride().WithRunner(sshRunner).WithCreateMutex(handles.WorktreeCreateMu)
				if err := workspace.CreateWorktree(ctx, workerRepoPath, runID, headSHA, cfg); err != nil {
					return "", nil, err
				}
				wtPath := workspace.WorktreePath(workerRepoPath, runID, workspace.NoWorktreeRootOverride())
				cleanup := func() {
					cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					rmCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "remove", "--force", "--force", wtPath)
					if rmErr := rmCmd.Run(); rmErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: remote worktree remove %s on %s: %v\n", wtPath, workerRepoPath, rmErr)
					}
					pruneCmd := sshRunner.Command(cleanCtx, "git", "-C", workerRepoPath, "worktree", "prune")
					if pruneErr := pruneCmd.Run(); pruneErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: workloop: remote worktree prune on %s: %v\n", workerRepoPath, pruneErr)
					}
				}
				return wtPath, cleanup, nil
			}
		} else {
			wtFactory = productionWorktreeFactory
		}
	}
	rp.Worktree = worktreePort(wtFactory)
	var baseSyncErr error
	var wtPath string
	var wtCleanup func()
	var wtErr error
	if subErr := mport.Submit()(ctx, "base-sync-create", func(qctx context.Context) error {
		if rbc != nil {
			workerHostEBOW, sshOptsEBOW, _ := tunnelpkg.SSHHostOpts(rbc.sshRunner)
			baseSyncErr = codesyncpkg.EnsureBaseOnWorker(qctx, rbc.sshRunner, rbc.worker.RepoPath, headSHA,
				nil, env.ProjectDir, workerHostEBOW, sshOptsEBOW)
		}
		if baseSyncErr == nil {
			wtPath, wtCleanup, wtErr = rp.Worktree.Create(qctx, activeRepo, runID.String(), headSHA)
		}
		return nil
	}); subErr != nil {
		wtErr = subErr
	}
	if baseSyncErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: codesync.EnsureBaseOnWorker bead %s run %s: %v (reopening)\n",
			beadID, runID.String(), baseSyncErr)
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
		failRun(fmt.Sprintf("create worktree failed: %v", wtErr),
			fmt.Sprintf("worktree_create_failed: %v", wtErr))
		return
	}
	useIndepSession := false

	runExit = func() runlease.Exit {
		return runlease.Exit{
			SessionRunsIndependently: useIndepSession,
			DaemonStopping:           daemonStopping(),
			EvidenceWorthKeeping:     runHandle != nil && runHandle.CapturedAgentOutput() && !bridge.Success(),
		}
	}

	if wtCleanup != nil {
		runScope.Hold(runlease.Worktree, func() error {
			wtCleanup()
			return nil
		})
		defer func() {
			d := runlease.Decide(runExit())
			if d.Releases(runlease.Worktree) {
				return
			}
			capture := ""
			if d == runlease.RetainEvidence {
				capture = fmt.Sprintf(" — the captured output is under %s/.harmonik/pi-agent/", wtPath)
			}
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: run %s (bead %s) ends %s — its worktree is kept at %s%s\n",
				runID.String(), beadID, d, wtPath, capture)
		}()
	}

	var runStartedWorkerName, runStartedWorkerOS *string
	if rbc != nil {
		runStartedWorkerName = &rbc.worker.Name
		runStartedWorkerOS = &rbc.worker.OS
	}
	emitRunStarted(ctx, emit, runID, beadID, wtPath, queueID, queueGroupIndex,
		plan.Workflow.Descriptor, workflowMode, plan.Workflow.ReviewPolicy, plan.Workflow.SelectionSource,
		runStartedWorkerName, runStartedWorkerOS)

	graph := plan.Workflow.Graph

	dotExtraContext := extraContext
	if graph.Goal != "" {
		goalLine := "Workflow goal: " + graph.Goal
		if dotExtraContext != "" {
			dotExtraContext = goalLine + "\n\n" + dotExtraContext
		} else {
			dotExtraContext = goalLine
		}
	}

	var dotRunner tmuxpkg.CommandRunner
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

	useIndepSession = setUpRunSession(&env, rp, handles, runScope, dotRunner != nil, runID, beadID)

	dotResult := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, beadRecord, beadRecord.Title, beadRecord.Description,
		activeRepo, wtPath, headSHA, graph, plan.Workflow.Descriptor, resolvedModel, resolvedEffort, resolvedProfile, dotExtraContext, baseBranch, dotRunner,
		dotWorkerBinary, dotWorkerHookSock, dotWorkerSession, dotWorkerCwd)

	transitionTID, tidErr := handles.TIDGen.Next()
	if tidErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: tidGen.Next (DOT spine transition) bead %s: %v\n", beadID, tidErr)
	}
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
			if amendErr := runmerge.ReplaceReviewTrailersOnHEAD(c, wtPath, dotResult.approveVerdict); amendErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: runmerge.ReplaceReviewTrailersOnHEAD (dot, merge retry %d) bead %s: %v (non-fatal)\n",
					retry, beadID, amendErr)
			}
		},
		// hk-whru3: advisory-RC + rebase_dropped_commits → work already on
		// main; a prior run merged the same patch and the rebase dropped the
		// commit. hk-vbv3b: extended to the genuine APPROVE terminal path
		// (terminalNodeID == "close") and the hk-8ps7q approved-and-done path
		// (approveVerdict != nil). Falls through to CloseBead so the infinite
		// re-dispatch loop terminates instead of re-queuing.
		//
		// hk-no-merge-silent-success-hc1jr: this carve-out turns a merge FAILURE
		// into a close-as-success, and it used to do that without a word
		// anywhere. Say it out loud, so an operator reading the daemon log can
		// tell a run that merged from a run that only concluded its work was
		// already on the target.
		CarveOut: func(reason string) bool {
			alreadyApprovedOnMain := dotResult.advisoryRC ||
				dotResult.terminalNodeID == "close" ||
				dotResult.approveVerdict != nil
			carved := alreadyApprovedOnMain && strings.Contains(reason, "rebase_dropped_commits")
			if carved {
				fmt.Fprintf(os.Stderr,
					"daemon: workloop: bead %s (dot): closing as success WITHOUT a merge — the rebase found the work already on %q: %s\n",
					beadID, mergeTarget, reason)
			}
			return carved
		},
	})
	if daemonStopping() {
		drainCtx := context.WithoutCancel(ctx)
		tipSHA, tipErr := gitprobe.ResolveWorktreeHEADVia(drainCtx, dotRunner, wtPath)
		if tipErr != nil || tipSHA == headSHA {
			tipSHA = ""
		}
		bridge.Drain(ctx, tipSHA)
		return bridge.Success()
	}
	switch {
	case dotResult.success:
		bridge.Feed(ctx, runexec.Event{
			Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSuccess,
			PathLabel: "dot", Detail: dotResult.summary,
		})
	case dotResult.subsumed:
		bridge.Feed(ctx, runexec.Event{
			Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
			EmitOutcome: true, PathLabel: "dot noChange-subsumed",
			Detail: "noChange-subsumed: the bead's work is already merged on the branch this run lands on",
		})
	default:
		tipResolveCtx := ctx
		if ctx.Err() != nil {
			tipResolveCtx = context.Background()
		}
		if tipSHA, tipErr := gitprobe.ResolveWorktreeHEADVia(tipResolveCtx, dotRunner, wtPath); tipErr == nil && tipSHA != "" && tipSHA != headSHA {
			runTipSHA = &tipSHA
		}

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
}

func isWatcherErrCanceled(err error) bool {
	return errors.Is(err, handlercontract.ErrCanceled)
}

func productionWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	if err := workspace.CreateWorktree(ctx, projectDir, runID, headSHA, workspace.NoWorktreeRootOverride()); err != nil {
		return "", nil, err
	}
	wtPath := workspace.WorktreePath(projectDir, runID, workspace.NoWorktreeRootOverride())

	writeWorktreeLease(wtPath, runID)

	toolsSrc := filepath.Join(projectDir, ".tools")
	if _, statErr := os.Lstat(toolsSrc); statErr == nil {
		if linkErr := os.Symlink(toolsSrc, filepath.Join(wtPath, ".tools")); linkErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: symlink .tools into %s: %v\n", wtPath, linkErr)
		}
	}

	cleanup := func() {
		if err := workspace.ReleaseLeaseLock(workspace.LeaseLockPath(wtPath)); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: release the worktree lease for run %s at %s: %v\n",
				runID, wtPath, err)
		}
		if cleanupErr := runmerge.RemoveWorktree(context.Background(), projectDir, wtPath); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: worktree reclaim failed for run %s at %s; "+
				"the worktree remains because cleanup failed, not because evidence was retained: %v\n",
				runID, wtPath, cleanupErr)
		}
	}
	return wtPath, cleanup, nil
}

const worktreeLeaseTTLSec = 24 * 60 * 60

func writeWorktreeLease(wtPath, runID string) {
	runUUID, parseErr := uuid.Parse(runID)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: lease for worktree %s: run id %q is not a uuid: %v\n",
			wtPath, runID, parseErr)
		return
	}
	lock := &core.LeaseLockFile{
		RunID: core.RunID(runUUID),
		// The daemon, because the daemon is what holds the worktree. A run that
		// outlives this process leaves a lease whose holder is dead, which is why
		// the boot sweep asks the run registry before it acts on that.
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC(),
		TTLSec:    worktreeLeaseTTLSec,
	}
	if err := workspace.WriteLeaseLockAtomic(workspace.LeaseLockPath(wtPath), lock); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: take the lease on worktree %s for run %s: %v\n",
			wtPath, runID, err)
	}
}

func resolveHEAD(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD: %w", err)
	}
	sha := string(out)
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: resolveHEAD: git rev-parse HEAD returned empty output")
	}
	return sha, nil
}

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

func d2RemoteAPIKeyRefusal(remote bool, env []string) (d2Refusal, bool) {
	if remote && hasAPIKeyInEnv(env) {
		return d2APIKeyRefusal, true
	}
	return "", false
}

func hasAPIKeyInEnv(env []string) bool {
	for _, e := range env {
		if e == "ANTHROPIC_API_KEY" {
			return true
		}
		if v, ok := strings.CutPrefix(e, "ANTHROPIC_API_KEY="); ok && v != "" {
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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeRunStarted, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit run_started: %v\n", emitErr)
	}
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
	if emitErr := bus.EmitWithRunID(ctx, runID, eventType, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit run_completed/run_failed: %v\n", emitErr)
	}
}

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
	if emitErr := bus.Emit(ctx, core.EventType("agent_presence"), b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit agent_presence: %v\n", emitErr)
	}
}

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

type beadClosedPayload struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
}

type epicCompletedPayload struct {
	EpicID          string `json:"epic_id"`
	LastChildBeadID string `json:"last_child_bead_id"`
	ClosedAt        string `json:"closed_at"`
}

func emitBeadClosed(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID) {
	pl := beadClosedPayload{
		RunID:  runID.String(),
		BeadID: string(beadID),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.Emit(ctx, core.EventTypeBeadClosed, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit bead_closed: %v\n", emitErr)
	}
}

func emitBeadClosedAndMaybeEpic(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, beadID core.BeadID) {
	emitBeadClosed(ctx, ports.Emitter, runID, beadID)
	maybeEmitEpicCompleted(ctx, ports, handles, runID, beadID)
}

func maybeEmitEpicCompleted(ctx context.Context, ports runloop.RunPorts, handles runloop.SharedHandles, runID core.RunID, closedBeadID core.BeadID) {
	ledger := ports.Ledger
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
		return
	}

	parentRecord, err := ledger.ShowBead(ctx, parentID)
	if err != nil {
		return
	}

	for _, e := range parentRecord.Edges {
		if e.EdgeKind == core.EdgeKindParentChild && e.ToBeadID == parentID {
			if !e.EndpointStatus.IsTerminal() {
				return
			}
		}
	}

	handles.EmittedEpicsMu.Lock()
	if _, already := handles.EmittedEpics[parentID]; already {
		handles.EmittedEpicsMu.Unlock()
		return
	}
	handles.EmittedEpics[parentID] = struct{}{}
	handles.EmittedEpicsMu.Unlock()

	pl := epicCompletedPayload{
		EpicID:          string(parentID),
		LastChildBeadID: string(closedBeadID),
		ClosedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := ports.Emitter.EmitWithRunID(ctx, runID, core.EventTypeEpicCompleted, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit epic_completed: %v\n", emitErr)
	}
}

func transitionToTerminated(ctx context.Context, m *hclifecycle.Machine, runID core.RunID, bus handlercontract.EventEmitter, exit runloop.ExitInfo) {
	if m == nil {
		return
	}
	exitCode, waitErr := exit.ExitCode, exit.WaitErr
	if exit.AgentAnnouncedEnd {
		exitCode, waitErr = 0, nil
	}
	emitWorkloopLifecycleTransition(ctx, m, runID, bus,
		hclifecycle.StateTerminating, hclifecycle.ReasonTerminateRequested, "", "")

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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeLifecycleTransition, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emit lifecycle_transition: %v\n", emitErr)
	}
}
