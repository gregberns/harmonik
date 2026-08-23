package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

type reconcileState struct {
	projectHash core.ProjectHash

	beadLedger          lifecycle.InFlightBeadLedger
	dispatchClaimLedger dispatchReplayClaimLedger
	beadResetter        lifecycle.BeadResetter
	orphanStatusReader  beadStatusReader
	beadCat3cCloser     lifecycle.BeadCat3cCloser
	intentGCLedger      lifecycle.IntentGCLedger
	intentRedriveWriter lifecycle.IntentRedriveWriter
	intentLogDir        string

	queueDispatched  lifecycle.QueueDispatchedSet
	queueOwned       lifecycle.QueueOwnedSet
	sweepTmuxAdapter ltmux.Adapter
	daemonOwnSession string

	sweepResult       OrphanSweepResult
	dispatchOwnership DispatchReplayOwnership
}

func (bs *bootState) runStartupReconcile(ctx context.Context, daemonStartTime time.Time, resolvedTargetBranch string) error {
	cfg := bs.cfg
	if cfg.ProjectDir == "" {
		return nil
	}
	if err := bs.ensureWorkerRegistry(ctx); err != nil {
		return fmt.Errorf("daemon: build worker registry before dispatch replay: %w", err)
	}
	if err := lifecycle.PrepareQueueNamespaceAtStartup(ctx, cfg.ProjectDir, nil); err != nil {
		return fmt.Errorf("daemon: prepare queue namespace before dispatch replay: %w", err)
	}
	st := &reconcileState{projectHash: lifecycle.ComputeProjectHash(cfg.ProjectDir)}
	intents, ownership, err := loadDispatchReplayAuthority(cfg.ProjectDir)
	if err != nil {
		return fmt.Errorf("daemon: read dispatch replay authority: %w", err)
	}
	st.dispatchOwnership = ownership

	if err := bs.buildReconcileAdapters(ctx, st); err != nil {
		return err
	}
	if err := bs.preflightDispatchReplay(ctx, st, intents); err != nil {
		return err
	}
	bs.runOrphanSweepAndAdopt(ctx, daemonStartTime, st)
	bs.runCatBLSweeps(ctx, resolvedTargetBranch)
	return nil
}

func loadDispatchReplayAuthority(projectDir string) ([]dispatch.Intent, DispatchReplayOwnership, error) {
	store := dispatchstore.New(projectDir)
	intents, err := store.List()
	if err != nil {
		return nil, DispatchReplayOwnership{}, err
	}
	receipts, err := store.ListSessionStartReceipts()
	if err != nil {
		return nil, DispatchReplayOwnership{}, err
	}
	ownership, err := dispatchReplayOwnership(intents, receipts)
	return intents, ownership, err
}

func (bs *bootState) preflightDispatchReplay(
	ctx context.Context,
	st *reconcileState,
	intents []dispatch.Intent,
) error {
	if len(intents) == 0 {
		return nil
	}
	reader := filesystemDispatchReplayReader{
		projectDir: bs.cfg.ProjectDir,
		beads:      st.orphanStatusReader,
		resolve:    newSessionStartAdapterResolver(st.sweepTmuxAdapter, bs.cfg.Workers),
		worktrees:  newDispatchWorktreeObserverResolver(bs.cfg.Workers, bs.workerRegistry),
	}
	steps, err := preflightDispatchReplayWithReader(ctx, intents, reader)
	if err != nil {
		return err
	}
	if err := executeDispatchReplayPlan(ctx, steps, dispatchReplayExecutor{
		projectDir:   bs.cfg.ProjectDir,
		intentLogDir: st.intentLogDir,
		claimLedger:  st.dispatchClaimLedger,
		now:          time.Now,
		workers:      bs.workerRegistry,
		localKind:    replayLocalExecutionKind(bs.cfg.Substrate),
		worktrees:    newDispatchWorktreeObserverResolver(bs.cfg.Workers, bs.workerRegistry),
	}); err != nil {
		return err
	}
	return fmt.Errorf("daemon: dispatch replay made durable progress; restart is required before orphan sweep")
}

func replayLocalExecutionKind(substrate handler.Substrate) runpkg.ExecutionKind {
	tmuxSubstrate, ok := substrate.(*tmuxSubstrate)
	if !ok {
		return runpkg.ExecutionLocalShared
	}
	if _, ok := tmuxSubstrate.adapter.(sessionCreator); ok {
		return runpkg.ExecutionLocalIndependent
	}
	return runpkg.ExecutionLocalShared
}

func preflightDispatchReplayWithReader(
	ctx context.Context,
	intents []dispatch.Intent,
	reader dispatchReplayFactReader,
) ([]dispatchReplayStep, error) {
	steps, err := planDispatchReplay(ctx, intents, reader)
	if err != nil {
		return nil, fmt.Errorf("daemon: plan dispatch replay before orphan sweep: %w", err)
	}
	return steps, nil
}

func dispatchReplayOwnership(
	intents []dispatch.Intent,
	receipts []dispatch.SessionStartReceipt,
) (DispatchReplayOwnership, error) {
	ownership := DispatchReplayOwnership{
		Beads:     make(map[core.BeadID]struct{}, len(intents)),
		Runs:      make(map[core.RunID]struct{}, len(intents)),
		Sessions:  make(map[string]struct{}),
		Worktrees: make(map[core.RunID]struct{}),
		Receipts:  make(map[core.RunID]dispatch.SessionStartReceipt, len(receipts)),
	}
	queueItems := make(map[string]core.RunID, len(intents))
	type dispatchTarget struct{ session, window string }
	targets := make(map[dispatchTarget]core.RunID, len(intents))
	for _, intent := range intents {
		if err := intent.Validate(); err != nil {
			return DispatchReplayOwnership{}, err
		}
		itemKey := fmt.Sprintf("%s\x00%s\x00%d\x00%d", intent.Binding.QueueID, intent.Binding.QueueName, intent.Binding.GroupIndex, intent.Binding.ItemIndex)
		if prior, exists := queueItems[itemKey]; exists && prior != intent.Binding.RunID {
			return DispatchReplayOwnership{}, fmt.Errorf("dispatch intents %s and %s claim one queue item", prior, intent.Binding.RunID)
		}
		queueItems[itemKey] = intent.Binding.RunID
		if _, exists := ownership.Beads[intent.Binding.BeadID]; exists {
			return DispatchReplayOwnership{}, fmt.Errorf("more than one dispatch intent claims bead %s", intent.Binding.BeadID)
		}
		ownership.Beads[intent.Binding.BeadID] = struct{}{}
		ownership.Runs[intent.Binding.RunID] = struct{}{}
		switch intent.Phase {
		case dispatch.PhaseRunDurable, dispatch.PhaseHandoffDurable:
			ownership.Worktrees[intent.Binding.RunID] = struct{}{}
		case dispatch.PhasePrepared, dispatch.PhaseClaimRefused, dispatch.PhaseClaimDurable:
		}
		if intent.Handoff != nil {
			targetKey := dispatchTarget{session: intent.Handoff.SessionName, window: intent.Handoff.WindowName}
			if prior, exists := targets[targetKey]; exists && prior != intent.Binding.RunID {
				return DispatchReplayOwnership{}, fmt.Errorf(
					"dispatch intents %s and %s claim target %s:%s",
					prior, intent.Binding.RunID, intent.Handoff.SessionName, intent.Handoff.WindowName,
				)
			}
			targets[targetKey] = intent.Binding.RunID
			ownership.Sessions[intent.Handoff.SessionName] = struct{}{}
		}
	}
	if err := joinDispatchReceipts(&ownership, intents, receipts); err != nil {
		return DispatchReplayOwnership{}, err
	}
	return ownership, nil
}

func joinDispatchReceipts(
	ownership *DispatchReplayOwnership,
	intents []dispatch.Intent,
	receipts []dispatch.SessionStartReceipt,
) error {
	for _, receipt := range receipts {
		if err := receipt.Validate(); err != nil {
			return err
		}
		intent, exists := intentForRun(intents, receipt.Binding.RunID)
		if !exists || intent.Phase != dispatch.PhaseHandoffDurable || receipt.Binding != intent.Binding ||
			receipt.SessionName != intent.Handoff.SessionName || receipt.WindowName != intent.Handoff.WindowName {
			return fmt.Errorf("session receipt %s has no exact handoff intent", receipt.Binding.RunID)
		}
		if _, exists := ownership.Receipts[receipt.Binding.RunID]; exists {
			return fmt.Errorf("more than one session receipt claims run %s", receipt.Binding.RunID)
		}
		ownership.Receipts[receipt.Binding.RunID] = receipt
	}
	return nil
}

func intentForRun(intents []dispatch.Intent, runID core.RunID) (dispatch.Intent, bool) {
	for _, intent := range intents {
		if intent.Binding.RunID == runID {
			return intent, true
		}
	}
	return dispatch.Intent{}, false
}

func (bs *bootState) buildReconcileAdapters(ctx context.Context, st *reconcileState) error {
	cfg := bs.cfg

	if err := bs.buildBeadAdapters(ctx, st); err != nil {
		return err
	}
	bs.loadQueueProvenance(ctx, st)

	if sa, ok := cfg.Substrate.(substrateWithAdapter); ok {
		st.sweepTmuxAdapter = sa.tmuxAdapter()
	}

	if ss, ok := cfg.Substrate.(substrateWithSessionName); ok {
		st.daemonOwnSession = ss.daemonSessionName()
	}

	return nil
}

func (bs *bootState) buildBeadAdapters(ctx context.Context, st *reconcileState) error {
	cfg := bs.cfg
	if cfg.BrPath == "" {
		return nil
	}
	brAdapter, brAdapterErr := newBrAdapter(bs.hooks, cfg.BrPath, cfg.ProjectDir)
	if brAdapterErr != nil {
		_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brAdapterErr, "br-new-for-project-sweep", bs.bus)
		return nil
	}
	if err := bs.ensureBrRunnable(ctx, brAdapter); err != nil {
		return err
	}
	st.beadLedger = brAdapter
	st.dispatchClaimLedger = brAdapter
	st.beadResetter = brAdapter
	st.orphanStatusReader = brAdapter  // hk-mdus1 B3: in_progress guard reader
	st.beadCat3cCloser = brAdapter     // Cat 3c auto-reconciler (hk-lgtq2)
	st.intentGCLedger = brAdapter      // GCRetiredIntentsWithRedrive ledger (hk-cizvu)
	st.intentRedriveWriter = brAdapter // BI-031 step-4 re-drive (hk-aev8t)
	st.intentLogDir = lifecycle.BeadsIntentsDir(cfg.ProjectDir)
	return nil
}

func (bs *bootState) ensureBrRunnable(ctx context.Context, brAdapter *brcli.Adapter) error {
	banner, runnableErr := brAdapter.CheckBrRunnable(ctx)
	if runnableErr == nil {
		log.Printf("NOTICE: daemon.Start: br is runnable (BI-024a); br --version reports %q", banner)
		return nil
	}
	failedPayload := core.DaemonStartupFailedPayload{
		FailedAt:    time.Now().UTC().Format(time.RFC3339),
		ExitCode:    8,
		FailureMode: "br-unavailable",
	}
	if failedBytes, marshalErr := json.Marshal(failedPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeDaemonStartupFailed, failedBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit daemon_startup_failed: %v", emitErr)
		}
	}
	return fmt.Errorf("daemon.Start: br is not runnable (BI-024a, exit code 8): %w", runnableErr)
}

func (bs *bootState) loadQueueProvenance(ctx context.Context, st *reconcileState) {
	names, enumErr := queue.EnumerateQueueNames(bs.cfg.ProjectDir)
	if enumErr != nil {
		log.Printf("warn: pre-sweep EnumerateQueueNames failed: %v — falling back to intent-log-only provenance", enumErr)
		return
	}
	if len(names) == 0 {
		return
	}
	st.queueDispatched = make(lifecycle.QueueDispatchedSet)
	st.queueOwned = make(lifecycle.QueueOwnedSet)
	for _, name := range names {
		rawQ, rawQErr := queue.Load(ctx, bs.cfg.ProjectDir, name)
		if rawQErr != nil || rawQ == nil {
			log.Printf("warn: pre-sweep queue.Load(%q) failed: %v — skipping this queue's provenance", name, rawQErr)
			continue
		}
		for gi := range rawQ.Groups {
			for _, item := range rawQ.Groups[gi].Items {
				st.queueOwned[item.BeadID] = struct{}{}
				if item.Status == queue.ItemStatusDispatched {
					st.queueDispatched[item.BeadID] = struct{}{}
				}
			}
		}
	}
}

func (bs *bootState) runOrphanSweepAndAdopt(ctx context.Context, daemonStartTime time.Time, st *reconcileState) {
	cfg := bs.cfg
	bus := bs.bus

	sweepResult, sweepErr := RunOrphanSweep(
		ctx,
		cfg.ProjectDir,
		st.projectHash,
		daemonStartTime,
		bs.orphanSweepConfig(daemonStartTime, st),
	)
	st.sweepResult = sweepResult

	sweepPayloadBytes, sweepMarshalErr := json.Marshal(sweepResult.ToPayload())
	if sweepMarshalErr != nil {
		sweepPayloadBytes = []byte(`{}`)
	}
	if sweepEmitErr := bus.Emit(ctx, core.EventTypeDaemonOrphanSweepCompleted, sweepPayloadBytes); sweepEmitErr != nil {
		_ = sweepEmitErr
	}
	_ = sweepErr

	adoptDeadRunSessions(
		ctx,
		cfg.ProjectDir,
		st.projectHash,
		daemonStartTime.UnixNano(),
		st.intentLogDir,
		st.sweepTmuxAdapter,
		st.beadResetter,
	)

	bs.reconcileInFlightRuns(ctx, daemonStartTime, st)

	bs.emitReconciliationMarkers(ctx, sweepResult)
}

func (bs *bootState) orphanSweepConfig(daemonStartTime time.Time, st *reconcileState) OrphanSweepConfig {
	cfg := bs.cfg
	return OrphanSweepConfig{
		DispatchOwnership:   st.dispatchOwnership,
		BeadLedger:          st.beadLedger,
		BeadResetter:        st.beadResetter,
		BeadCat3cCloser:     st.beadCat3cCloser,
		IntentGCLedger:      st.intentGCLedger,
		IntentRedriveWriter: st.intentRedriveWriter, // BI-031 step-4 re-drive (hk-aev8t)
		// BeadProvenance: sentinel-file checker (hk-11xkn) — provenance when
		// all intent files have been cleared by prior crash-recovery runs.
		BeadProvenance: lifecycle.NewSentinelFileProvenanceChecker(
			lifecycle.BeadsOwnedDir(cfg.ProjectDir),
		),
		MergeCommitScanner: lifecycle.GitMergeCommitScanner{
			ProjectDir:   cfg.ProjectDir,
			TargetBranch: "", // defaults to "main" inside the scanner
		},
		IntentLogDir:       st.intentLogDir,
		DaemonStartNS:      daemonStartTime.UnixNano(),
		QueueDispatched:    st.queueDispatched,
		QueueOwned:         st.queueOwned,
		TmuxAdapter:        st.sweepTmuxAdapter, // hk-xb5yi: reap orphan windows from prior crash
		DaemonSpawnSession: st.daemonOwnSession, // hk-9vp51: never sweep the daemon's own session
	}
}

func (bs *bootState) reconcileInFlightRuns(ctx context.Context, daemonStartTime time.Time, st *reconcileState) {
	cfg := bs.cfg
	if cfg.JSONLLogPath == "" {
		return
	}
	registry, liveErr := runpkg.ScanRegistry(cfg.ProjectDir)
	if liveErr != nil {
		log.Printf("daemon: reconcileInFlightRuns: THE RUN REGISTRY COULD NOT BE READ (%v). "+
			"This boot cannot tell a live run from one the crash left behind, so it will mark no "+
			"run failed and reset no bead. A queue item held by a crashed run stays dispatched "+
			"until the registry reads again. Repair or move the unreadable file under "+
			".harmonik/runs/ and restart the daemon.", liveErr)
		return
	}
	liveRunBeadIDs := make(map[core.BeadID]struct{})
	for _, rec := range registry.Legacy {
		if rec.BeadID != "" {
			liveRunBeadIDs[core.BeadID(rec.BeadID)] = struct{}{}
		}
	}
	for _, rec := range registry.Dispatch {
		liveRunBeadIDs[rec.BeadID] = struct{}{}
	}
	reconcileStatusReader := st.orphanStatusReader
	if lister, ok := st.orphanStatusReader.(bulkBeadLister); ok && lister != nil {
		if cached := newCachedOrphanStatusReader(ctx, lister); cached != nil {
			reconcileStatusReader = cached
		}
	}
	_ = reconcileOrphanedRunsOnResume(
		ctx,
		cfg.JSONLLogPath,
		bs.bus,
		st.beadResetter,
		reconcileStatusReader,
		st.intentLogDir,
		st.projectHash,
		daemonStartTime.UnixNano(),
		st.queueDispatched,
		liveRunBeadIDs,
	)
}

func (bs *bootState) emitReconciliationMarkers(ctx context.Context, sweepResult OrphanSweepResult) {
	startupRunUID, startupUIDErr := uuid.NewV7()
	if startupUIDErr != nil {
		return
	}
	startupRunID := core.RunID(startupRunUID)
	startupRecPayload := core.ReconciliationStartedPayload{
		ReconciliationRunID: startupRunID,
		Trigger:             core.ReconciliationTriggerStartup,
	}
	if startupRecBytes, marshalErr := json.Marshal(startupRecPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeReconciliationStarted, startupRecBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit reconciliation_started: %v", emitErr)
		}
	}
	startupCompPayload := core.ReconciliationCompletedPayload{
		ReconciliationRunID: startupRunID,
		Trigger:             core.ReconciliationTriggerStartup,
		BeadsExamined:       sweepResult.BeadInProgressReset + sweepResult.BeadCat3cClosed,
		BeadsClosed:         sweepResult.BeadCat3cClosed,
		BeadsReset:          sweepResult.BeadInProgressReset,
		CompletedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if startupCompBytes, marshalErr := json.Marshal(startupCompPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeReconciliationCompleted, startupCompBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit reconciliation_completed: %v", emitErr)
		}
	}
}

func (bs *bootState) runCatBLSweeps(ctx context.Context, resolvedTargetBranch string) {
	cfg := bs.cfg
	bus := bs.bus

	if err := RunCatBL1StartupSweep(ctx, CatBL1StartupSweepConfig{
		ProjectDir:   cfg.ProjectDir,
		BrPath:       cfg.BrPath,
		TargetBranch: resolvedTargetBranch,
		Emitter:      bus,
	}); err != nil {
		log.Printf("warn: daemon.Start: Cat-BL1 startup sweep (non-fatal): %v", err)
	}

	if err := RunCatBL3StartupSweep(ctx, CatBL3StartupSweepConfig{
		ProjectDir: cfg.ProjectDir,
		Emitter:    bus,
	}); err != nil {
		log.Printf("warn: daemon.Start: Cat-BL3 startup sweep (non-fatal): %v", err)
	}
}
