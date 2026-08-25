package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/orchestrator"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/workers"
)

const workloopPollInterval = 2 * time.Second

const shutdownDrainTimeout = 10 * time.Second

const groupCompletionDrainTimeout = 3 * time.Second

const claimSkipInProgressCooldown = 5 * time.Minute

type windowCleaner interface {
	KillAllWindows(ctx context.Context) error
}

type loopLifecyclePort struct {
	cancelOnQueueDrain    context.CancelFunc
	cancelOnQueueExit     context.CancelFunc
	stopDispatchCtx       context.Context //nolint:containedctx // Config.StopDispatchCtx is a context by design.
	spawnSubstrateReadyCh <-chan struct{}
}

func newLoopLifecyclePort(cfg Config) loopLifecyclePort {
	return loopLifecyclePort{
		cancelOnQueueDrain: cfg.CancelOnQueueDrain,
		cancelOnQueueExit:  cfg.CancelOnQueueExit,
		stopDispatchCtx:    cfg.StopDispatchCtx,
	}
}

type ledgerRepairPort struct {
	staleBlockerCloser         lifecycle.BeadCat3cCloser
	strandedInProgressResetter strandedInProgressResetter
	strandedResetProjectHash   core.ProjectHash
	strandedResetDaemonNS      int64
}

type loopCollaborators struct {
	lifecycle       loopLifecyclePort
	ledgerRepair    ledgerRepairPort
	schedule        schedulePort
	coordinatorReap coordinatorReapPort
	diskReclaim     diskReclaimPort
	eagerRefill     eagerRefillPort
	governor        governorPort
	governorEnabled bool
	capacity        capacityPort
	queueSurface    queueSurfacePort
	dispatchGates   dispatchGatesPort
	queueIdleWait   func(queueIdleSnapshot) queueIdleWait
}

type workLoopInput struct {
	baseEnv       runloop.RunEnv
	basePorts     runloop.RunPorts
	handles       runloop.SharedHandles
	ledger        beadLedger
	queueStore    *queuewiring.QueueStore
	runRegistry   *runregistry.RunRegistry
	substrate     handler.Substrate
	mergeQueue    *mergeq.Queue
	launchBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

type workLoopState struct {
	wg                        sync.WaitGroup
	runs                      *runSupervisor
	effectiveMax              int
	claimSem                  chan struct{}
	lastSeenPauseEpoch        int
	rrCursor                  int
	maintenance               *loopMaintenance
	reapPort                  reapSeamPort
	completionPort            runCompletionPort
	itemRefusedUntil          map[core.BeadID]time.Time
	tickRefusals              map[core.BeadID]bool
	walkingThisTick           bool
	readyPathAttempts         map[core.BeadID]int
	queuePreClaimShowAttempts map[queuePreClaimAttemptKey]int
	crossQueueCollisions      map[queuePreClaimAttemptKey]crossQueueCollisionState
}

func (s *workLoopState) wait() {
	s.runs.Wait()
	s.wg.Wait()
}

func newLedgerRepairPort(adapter beadLedger, projectDir string) ledgerRepairPort {
	var closer lifecycle.BeadCat3cCloser
	if value, ok := adapter.(lifecycle.BeadCat3cCloser); ok {
		closer = value
	}
	var resetter strandedInProgressResetter
	if value, ok := adapter.(strandedInProgressResetter); ok {
		resetter = value
	}
	return ledgerRepairPort{
		staleBlockerCloser:         closer,
		strandedInProgressResetter: resetter,
		strandedResetProjectHash:   lifecycle.ComputeProjectHash(projectDir),
		strandedResetDaemonNS:      time.Now().UnixNano(),
	}
}

type reapSeamPort struct {
	bus                handlercontract.EventEmitter
	projectDir         string
	queueStore         *queuewiring.QueueStore
	queueLedger        queue.BeadLedger
	cancelOnQueueDrain context.CancelFunc
	cancelOnQueueExit  context.CancelFunc
	maxConcurrent      int
	concurrencyCtrl    *ConcurrencyController
	runRegistry        *runregistry.RunRegistry
	targetBranch       string
	eagerRefill        eagerRefillPort
	completionStore    queue.CompletionStore
}

func newReapSeamPort(bus handlercontract.EventEmitter, projectDir, targetBranch string, queueStore *queuewiring.QueueStore, runRegistry *runregistry.RunRegistry, loopLifecycle loopLifecyclePort, capacity capacityPort, queueSurface queueSurfacePort, eagerRefill eagerRefillPort) reapSeamPort {
	return reapSeamPort{
		bus:                bus,
		projectDir:         projectDir,
		queueStore:         queueStore,
		queueLedger:        queueSurface.queueLedger,
		cancelOnQueueDrain: loopLifecycle.cancelOnQueueDrain,
		cancelOnQueueExit:  loopLifecycle.cancelOnQueueExit,
		maxConcurrent:      capacity.maxConcurrent,
		concurrencyCtrl:    capacity.concurrencyCtrl,
		runRegistry:        runRegistry,
		targetBranch:       targetBranch,
		eagerRefill:        eagerRefill,
		completionStore:    queueStore,
	}
}

type runCompletionPort struct {
	reapSeamPort
	brPath string
}

func newRunCompletionPort(brPath string, reapPort reapSeamPort) runCompletionPort {
	return runCompletionPort{
		reapSeamPort: reapPort,
		brPath:       brPath,
	}
}

const maxItemAttempts = queue.MaxItemAttempts

type queuePreClaimAttemptKey struct {
	queueID    string
	groupIndex int
	itemIdx    int
	beadID     core.BeadID
}

type queueSelection struct {
	queueName        string
	queueID          string
	groupIndex       int
	itemIdx          int
	itemBeadID       core.BeadID
	itemContext      string
	itemWorkflow     runloop.QueueWorkflowInput
	itemTemplateMap  map[string]string
	anyEligible      bool // true if any queue had an active group with eligible items
	anyPausedOrEmpty bool // true if at least one queue existed but contributed nothing
	// Per-queue routing fields (hk-f10xl [L5 Move 2]).
	queueLocalOnly      bool           // mirrors Queue.LocalOnly — skip SelectWorker when true
	queueWorkerTarget   string         // mirrors Queue.WorkerTarget — pin to named worker when non-empty
	queueDefaultHarness core.AgentType // mirrors Queue.DefaultHarness — tier-2 harness default
}

type queueIdleSnapshot struct {
	HasDeferredItems bool
	HasSkippedBeads  bool
}

type queueIdleWait uint8

const (
	queueIdleScheduleAware queueIdleWait = iota
	queueIdlePoll
)

func decideQueueIdleWait(snapshot queueIdleSnapshot) queueIdleWait {
	if snapshot.HasDeferredItems || snapshot.HasSkippedBeads {
		return queueIdlePoll
	}
	return queueIdleScheduleAware
}

func effectiveQueueWorkers(q *queue.Queue, globalCap int) int {
	return queue.DefaultWorkers(q.Workers, globalCap)
}

func selectNextQueue(lq *queuewiring.LockedQueueStore, reg *runregistry.RunRegistry, globalCap, rrCursor int, blockedQueues, skipBeads map[string]bool) (queueSelection, bool) {
	sel, ok := orchestrator.SelectNextQueue(snapshotFleet(lq, reg, globalCap, rrCursor, blockedQueues, skipBeads))
	if !ok {
		return queueSelection{anyPausedOrEmpty: sel.SawNonContributing}, false
	}
	return queueSelection{
		queueName:   sel.QueueName,
		queueID:     sel.QueueID,
		groupIndex:  sel.GroupIndex,
		itemIdx:     sel.Item.ItemIdx,
		itemBeadID:  sel.Item.BeadID,
		itemContext: sel.Item.Context,
		itemWorkflow: runloop.QueueWorkflowInput{
			Mode: sel.Item.WorkflowMode,
			Ref:  sel.Item.WorkflowRef,
		},
		itemTemplateMap:     sel.Item.TemplateParams,
		anyEligible:         true,
		queueLocalOnly:      sel.LocalOnly,
		queueWorkerTarget:   sel.WorkerTarget,
		queueDefaultHarness: sel.DefaultHarness,
	}, true
}

func snapshotFleet(lq *queuewiring.LockedQueueStore, reg *runregistry.RunRegistry, globalCap, rrCursor int, blockedQueues, skipBeads map[string]bool) orchestrator.FleetSnapshot {
	names := lq.LockedAllQueueNames()
	queues := make([]orchestrator.QueueSnapshot, 0, len(names))
	for _, name := range names {
		q := lq.LockedQueueByName(name)
		if q == nil {
			continue
		}
		queues = append(queues, orchestrator.QueueSnapshot{
			Name:           name,
			QueueID:        q.QueueID,
			Active:         q.Status == queue.QueueStatusActive,
			Blocked:        blockedQueues[name],
			LocalInFlight:  reg.LenForQueueLocal(name),
			WorkerCap:      effectiveQueueWorkers(q, globalCap),
			LocalOnly:      q.LocalOnly,
			WorkerTarget:   q.WorkerTarget,
			DefaultHarness: q.DefaultHarness,
			ActiveGroup:    projectActiveGroup(q),
		})
	}
	return orchestrator.FleetSnapshot{Queues: queues, RRCursor: rrCursor, SkipBeads: skipBeads}
}

func offerableSkipSet(refusedUntil map[core.BeadID]time.Time, tickRefusals map[core.BeadID]bool, now time.Time) map[string]bool {
	if len(refusedUntil) == 0 && len(tickRefusals) == 0 {
		return nil
	}
	skip := make(map[string]bool, len(refusedUntil)+len(tickRefusals))
	for id, expiry := range refusedUntil {
		if now.Before(expiry) {
			skip[string(id)] = true
			continue
		}
		delete(refusedUntil, id)
	}
	for id := range tickRefusals {
		skip[string(id)] = true
	}
	if len(skip) == 0 {
		return nil
	}
	return skip
}

// projectActiveGroup projects q's FIRST active group into a GroupSnapshot (nil
// when none), stamping each eligible item's ABSOLUTE index into Group.Items so
// the dispatch stamp lands on the right item (addendum fix #1). The absolute
// index is resolved exactly as the legacy selectNextQueue did: the first
// Items entry matching the eligible item's BeadID with ItemStatusPending.
//
//nolint:gocognit // pre-existing: Seam A moved this code out of workloop.go unchanged
func projectActiveGroup(q *queue.Queue) *orchestrator.GroupSnapshot {
	for gi := range q.Groups {
		if q.Groups[gi].Status != queue.GroupStatusActive {
			continue
		}
		g := &q.Groups[gi]
		pendingCount := 0
		for ii := range g.Items {
			if g.Items[ii].Status == queue.ItemStatusPending {
				pendingCount++
			}
		}
		eligible := queue.EligibleItems(g)
		items := make([]orchestrator.ItemSnapshot, 0, len(eligible))
		for _, ep := range eligible {
			idx := -1
			for j := range g.Items {
				if g.Items[j].BeadID == ep.BeadID && g.Items[j].Status == queue.ItemStatusPending {
					idx = j
					break
				}
			}
			if idx < 0 {
				continue
			}
			it := &g.Items[idx]
			items = append(items, orchestrator.ItemSnapshot{
				ItemIdx:        idx,
				BeadID:         it.BeadID,
				Context:        it.Context,
				WorkflowMode:   it.WorkflowMode,
				WorkflowRef:    it.WorkflowRef,
				TemplateParams: it.TemplateParams,
			})
		}
		return &orchestrator.GroupSnapshot{
			GroupIndex:   g.GroupIndex,
			Eligible:     items,
			Kind:         string(g.Kind),
			PendingCount: pendingCount,
		}
	}
	return nil
}

//nolint:gocognit,cyclop,funlen // Decisions leave this effect shell one proven extraction at a time.
func runWorkLoop(ctx context.Context, input workLoopInput, collaborators loopCollaborators, noAutoPull bool) error {
	baseEnv := input.baseEnv
	basePorts := input.basePorts
	handles := input.handles
	ledger := input.ledger
	queueStore := input.queueStore
	runRegistry := input.runRegistry
	substratePort := input.substrate
	mergeQueue := input.mergeQueue
	launchBuilder := input.launchBuilder
	loopLifecycle := collaborators.lifecycle
	ledgerRepair := collaborators.ledgerRepair
	capacity := collaborators.capacity
	queueSurface := collaborators.queueSurface
	dispatchGates := collaborators.dispatchGates
	queueIdleWaitDecision := collaborators.queueIdleWait
	if queueIdleWaitDecision == nil {
		queueIdleWaitDecision = decideQueueIdleWait
	}

	if mergeQueue == nil {
		mergeQueue = mergeq.New(nil)
		mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
		mergeQueue.Start(mergeQCtx)
		defer mergeQCancel()
	}
	basePorts.Merge = newMergePort(mergeQueue)

	state := workLoopState{effectiveMax: capacity.maxConcurrent}
	state.runs = newRunSupervisor(runRegistry)
	if state.effectiveMax <= 0 {
		state.effectiveMax = 1
	}
	state.claimSem = make(chan struct{}, state.effectiveMax)
	state.maintenance = newLoopMaintenance(baseEnv.ProjectCfg, collaborators, os.Stderr)
	state.reapPort = newReapSeamPort(basePorts.Emitter, baseEnv.ProjectDir, baseEnv.TargetBranch, queueStore, runRegistry, loopLifecycle, capacity, queueSurface, collaborators.eagerRefill)
	state.completionPort = newRunCompletionPort(baseEnv.BrPath, state.reapPort)
	state.itemRefusedUntil = make(map[core.BeadID]time.Time)
	state.tickRefusals = make(map[core.BeadID]bool)
	state.readyPathAttempts = make(map[core.BeadID]int)
	state.queuePreClaimShowAttempts = make(map[queuePreClaimAttemptKey]int)
	state.crossQueueCollisions = make(map[queuePreClaimAttemptKey]crossQueueCollisionState)
	dispatchCtx := ctx //nolint:contextcheck // Config.StopDispatchCtx is a context by design.
	if loopLifecycle.stopDispatchCtx != nil {
		dispatchCtx = loopLifecycle.stopDispatchCtx
	}

	exitClean := func() error { //nolint:unparam // pre-existing: Seam A moved this code out of workloop.go unchanged
		drainDone := make(chan struct{})
		go func() {
			state.wait()
			close(drainDone)
		}()
		select {
		case <-drainDone:
		case <-time.After(shutdownDrainTimeout):
			remaining := runRegistry.Len()
			fmt.Fprintf(os.Stderr,
				"daemon: workloop: shutdown: drain timeout after %v with %d run(s) still in-flight; exiting (QM-002a recovers on next start)\n",
				shutdownDrainTimeout, remaining)
		}
		if wc, ok := substratePort.(windowCleaner); ok {
			_ = wc.KillAllWindows(context.Background()) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
		}
		drainQueuesForRestart(context.Background(), queueStore, baseEnv.ProjectDir, basePorts.Emitter)
		return nil
	}

	exitFatal := func(err error) error {
		_ = exitClean() //nolint:errcheck // exitClean is documented to return nil; the fatal error is the one that matters
		return err
	}

	if loopLifecycle.spawnSubstrateReadyCh != nil {
		select {
		case <-loopLifecycle.spawnSubstrateReadyCh:
		case <-ctx.Done():
			return exitClean()
		}
	}

	if baseEnv.ProjectDir != "" {
		if tmuxAdp := extractTmuxAdapterFromSubstrate(substratePort); tmuxAdp != nil {
			if liveRecs, listErr := legacyRunSessionsForAdoption(baseEnv.ProjectDir); listErr == nil {
				for _, rec := range liveRecs {
					//nolint:copyloopvar // pre-existing: Seam A moved this code out of workloop.go unchanged
					rec := rec // capture loop variable
					state.wg.Add(1)
					go func() {
						defer state.wg.Done()
						adoptLiveRunSession(ctx, ledger, baseEnv, queueStore, handles.TIDGen, rec, tmuxAdp)
					}()
				}
			}
		}
	}

	for {
		if !state.walkingThisTick {
			clear(state.tickRefusals)
		}
		state.walkingThisTick = false

		select {
		case <-dispatchCtx.Done():
			return exitClean()
		default:
		}

		preObs := state.maintenance.tickBeforeDispatch(ctx)
		if preObs.halt {
			return exitClean()
		}
		if preObs.diskLow {
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		controllerMax, controllerPresent := 0, false
		if capacity.concurrencyCtrl != nil {
			controllerMax, controllerPresent = capacity.concurrencyCtrl.Get(), true
		}
		gateMax := orchestrator.LocalGateMax(state.effectiveMax, controllerMax, controllerPresent)

		tickVerdict, tickAdmitErr := orchestrator.AdmitAtTick(orchestrator.AdmissionInput{
			Path:          orchestrator.PathAny,
			GateMax:       gateMax,
			LocalInFlight: int(handles.LocalInFlight.Load()),
			// HasFreeSlot is a non-consuming peek (internal/workers Registry), so
			// asking on every tick reserves nothing and changes no state.
			WorkerHasFreeSlot: handles.Workers != nil && handles.Workers.HasFreeSlot(),
		})
		if tickAdmitErr != nil {
			state.wait()
			return fmt.Errorf("daemon: workloop: tick admission: %w", tickAdmitErr)
		}
		if !tickVerdict.Admitted {
			if tickVerdict.Message != "" {
				fmt.Fprint(os.Stderr, tickVerdict.Message)
			}
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		selObs := state.maintenance.tickBeforeSelect(ctx, baseEnv.ProjectDir, basePorts.Emitter, state.reapPort, governorInputPort{projectDir: baseEnv.ProjectDir, brPath: baseEnv.BrPath, ledger: ledger}, time.Now())

		var (
			beadRecord core.BeadRecord
			// reservedRunID is set by the queue path's reservation transaction,
			// which generates the RunID BEFORE the write so that status and
			// identity land together. The br-ready path leaves it unset and
			// generates its own further down.
			reservedRunID               core.RunID
			runIDReserved               bool
			claimTID                    core.TransitionID
			claimTIDReserved            bool
			queueItemIndex              int    // item index within the group (-1 = no queue)
			capturedQueueName           string // NQ-B1: name of the dispatching queue ("" = br-ready)
			queueIDField                *string
			queueGroupIdxFd             *int
			capturedExtraContext        string                     // hk-boiwe: per-item context from queue.Item.Context
			capturedItemWorkflow        runloop.QueueWorkflowInput // raw queue.Item workflow fields
			capturedItemTemplateParams  map[string]string          // hk-55zv2 / WG-045: template params from queue.Item.TemplateParams
			capturedQueueLocalOnly      bool                       // hk-f10xl [L5 Move 2]: per-queue local-only routing gate
			capturedQueueWorkerTarget   string                     // hk-f10xl [L5 Move 2]: per-queue worker-target pin
			capturedQueueDefaultHarness core.AgentType             // per-queue tier-2 harness default
		)
		queueItemIndex = -1 // sentinel: not queue-dispatched

		if queueStore != nil {
			var (
				//nolint:staticcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
				snapItemIdx            int = -1 // -1 → no item found (no queue can contribute)
				snapItemBeadID         core.BeadID
				snapItemContext        string
				snapItemWorkflow       runloop.QueueWorkflowInput
				snapItemTemplateParams map[string]string
				snapGroupIndex         int
				snapQueueID            string
				snapQueueName          string
			)
			{

				hasDeferredItems := reevaluateDeferredQueues(ctx, queueStore, baseEnv.ProjectDir, queueSurface, dispatchGates)

				lq := queueStore.LockForMutation()

				bootstrapped := false
				var bootstrapEvents []queue.EventIntent
				for _, name := range lq.LockedAllQueueNames() {
					q := lq.LockedQueueByName(name)
					if q == nil || q.Status != queue.QueueStatusActive {
						continue
					}
					hasActiveGroup := false
					for i := range q.Groups {
						if q.Groups[i].Status == queue.GroupStatusActive {
							hasActiveGroup = true
							break
						}
					}
					if hasActiveGroup {
						continue
					}
					if ok, evts := activateFirstPendingGroupLocked(ctx, baseEnv.ProjectDir, lq, q); ok {
						bootstrapped = true
						bootstrapEvents = append(bootstrapEvents, evts...)
					}
				}
				if bootstrapped {
					lq.Done()
					for _, evt := range bootstrapEvents {
						_ = basePorts.Emitter.Emit(ctx, evt.Type, evt.Payload) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
					}
					continue
				}

				skipBeads := offerableSkipSet(state.itemRefusedUntil, state.tickRefusals, time.Now())
				sel, ok := selectNextQueue(lq, runRegistry, state.effectiveMax, state.rrCursor, selObs.blockedQueues, skipBeads)
				loadedQueueCount := len(lq.LockedAllQueueNames())
				lq.Done()
				if !ok {
					if loadedQueueCount > 0 {
						switch queueIdleWaitDecision(queueIdleSnapshot{
							HasDeferredItems: hasDeferredItems,
							HasSkippedBeads:  len(skipBeads) > 0,
						}) {
						case queueIdlePoll:
							if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
								return exitClean()
							}
						case queueIdleScheduleAware:
							if sleepErr := scheduleAwareIdleWait(dispatchCtx, collaborators.schedule, queueSurface.submitWakeC); sleepErr != nil {
								return exitClean()
							}
						}
						continue
					}
				} else {
					state.rrCursor++

					snapItemIdx = sel.itemIdx
					snapItemBeadID = sel.itemBeadID
					snapItemContext = sel.itemContext
					snapItemWorkflow = sel.itemWorkflow
					snapItemTemplateParams = sel.itemTemplateMap
					snapGroupIndex = sel.groupIndex
					snapQueueID = sel.queueID
					snapQueueName = sel.queueName
					capturedQueueLocalOnly = sel.queueLocalOnly
					capturedQueueWorkerTarget = sel.queueWorkerTarget
					capturedQueueDefaultHarness = sel.queueDefaultHarness
				}
			}

			if snapItemIdx >= 0 {
				if expiry, ok := state.itemRefusedUntil[snapItemBeadID]; ok && time.Now().Before(expiry) {
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				if dispatchGates.handlerPauseController != nil {
					epoch, isPaused := dispatchGates.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
					state.lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(dispatchGates, epoch, state.lastSeenPauseEpoch)
					if isPaused {
						emitHeldEvent(ctx, dispatchGates, snapItemBeadID, epoch)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				preLookupVerdict, preLookupErr := orchestrator.AdmitBeforeLookup(orchestrator.AdmissionInput{
					Path:            orchestrator.PathQueue,
					BeadID:          string(snapItemBeadID),
					DecisionBlocked: dispatchGates.decisionBlocker != nil && dispatchGates.decisionBlocker.IsBeadBlocked(snapItemBeadID),
					SentinelBlocked: state.maintenance.sentinelBlocksDispatch(dispatchGates),
				})
				if preLookupErr != nil {
					state.wait()
					return fmt.Errorf("daemon: workloop: before-lookup admission (queue path): %w", preLookupErr)
				}
				if !preLookupVerdict.Admitted {
					if preLookupVerdict.Message != "" {
						fmt.Fprint(os.Stderr, preLookupVerdict.Message)
					}
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				var preClaimRecord core.BeadRecord
				preClaimLoaded := false
				{
					preClaimKey := queuePreClaimAttemptKey{
						queueID:    snapQueueID,
						groupIndex: snapGroupIndex,
						itemIdx:    snapItemIdx,
						beadID:     snapItemBeadID,
					}
					rec, preClaimErr := ledger.ShowBead(ctx, snapItemBeadID)
					if preClaimErr != nil {
						if dispatchCtx.Err() != nil {
							return exitClean()
						}
						state.queuePreClaimShowAttempts[preClaimKey]++
						preClaimAttempts := state.queuePreClaimShowAttempts[preClaimKey]
						if preClaimAttempts >= maxItemAttempts {
							delete(state.queuePreClaimShowAttempts, preClaimKey)
							fmt.Fprintf(os.Stderr,
								"daemon: workloop: ShowBead pre-claim (queue-path) %s failed %d times — failing queue item so the group can advance (hk-pina9): %v\n",
								snapItemBeadID, preClaimAttempts, preClaimErr)
							markQueueItemFailureReason(ctx, queueStore, snapQueueName, snapGroupIndex, snapItemIdx, snapItemBeadID, "show_bead_failed")
							evaluateGroupAdvanceWithOutcome(ctx, state.reapPort, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false, time.Now())
							continue
						}
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: ShowBead pre-claim (queue-path) %s error (attempt %d/%d, will retry): %v\n",
							snapItemBeadID, preClaimAttempts, maxItemAttempts, preClaimErr)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
					delete(state.queuePreClaimShowAttempts, preClaimKey)
					preClaimRecord = rec
					preClaimLoaded = true
					if preClaimRecord.Status != core.CoarseStatusOpen && preClaimRecord.Status != core.CoarseStatusBlocked {
						fmt.Fprintf(os.Stderr,
							"daemon: workloop: bead_claim_skipped %s observed_status=%s reason=status_changed_between_select_and_claim (BI-013c)\n",
							snapItemBeadID, preClaimRecord.Status)
						skipPayload := core.BeadClaimSkippedPayload{
							BeadID:         string(snapItemBeadID),
							ObservedStatus: string(preClaimRecord.Status),
							Reason:         "status_changed_between_select_and_claim",
							DetectedAt:     time.Now().UTC().Format(time.RFC3339),
						}
						if raw, mErr := json.Marshal(skipPayload); mErr == nil {
							_ = basePorts.Emitter.Emit(ctx, core.EventTypeBeadClaimSkipped, raw) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
						}
						if preClaimRecord.Status.IsTerminal() {
							fmt.Fprintf(os.Stderr,
								"daemon: workloop: bead %s is %s — advancing its queue item to completed rather than failing it "+
									"(§3.2b QM-002b Class A, hk-rern1)\n",
								snapItemBeadID, preClaimRecord.Status)
							evaluateGroupAdvanceWithOutcome(ctx, state.reapPort, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, true, time.Now())
						} else {
							if preClaimRecord.Status == core.CoarseStatusInProgress &&
								ledgerRepair.strandedInProgressResetter != nil &&
								!runRegistry.HasBeadRun(snapItemBeadID) &&
								!strandedBeadHasOnDiskRun(baseEnv.ProjectDir, snapItemBeadID) {
								if resetErr := ledgerRepair.strandedInProgressResetter.ResetBead(
									ctx, baseEnv.IntentLogDir, baseEnv.BrTimeoutCfg,
									snapItemBeadID,
									ledgerRepair.strandedResetProjectHash,
									ledgerRepair.strandedResetDaemonNS,
								); resetErr != nil {
									fmt.Fprintf(os.Stderr,
										"daemon: workloop: stranded_bead_auto_reset FAILED bead=%s: %v — bead stays in_progress until next restart\n",
										snapItemBeadID, resetErr)
								} else {
									fmt.Fprintf(os.Stderr,
										"daemon: workloop: stranded_bead_auto_reset bead=%s reason=in_progress_with_no_run (hk-l2xd1)\n",
										snapItemBeadID)
									if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
										return exitClean()
									}
									continue
								}
							}
							if preClaimRecord.Status == core.CoarseStatusInProgress {
								now := time.Now()
								for id, exp := range state.itemRefusedUntil {
									if now.After(exp) {
										delete(state.itemRefusedUntil, id)
									}
								}
								state.itemRefusedUntil[snapItemBeadID] = now.Add(claimSkipInProgressCooldown)
							}
							if queueStore != nil {
								lq := queueStore.LockForMutation()
								liveQ := lq.LockedQueueByName(snapQueueName)
								if liveQ != nil {
									for gi := range liveQ.Groups {
										if liveQ.Groups[gi].Status != queue.GroupStatusActive {
											continue
										}
										if liveQ.Groups[gi].GroupIndex != snapGroupIndex {
											continue
										}
										if snapItemIdx < len(liveQ.Groups[gi].Items) &&
											liveQ.Groups[gi].Items[snapItemIdx].BeadID == snapItemBeadID &&
											liveQ.Groups[gi].Items[snapItemIdx].Status == queue.ItemStatusPending {
											liveQ.Groups[gi].Items[snapItemIdx].Status = queue.ItemStatusDeferredForLedgerDep
										}
									}
									lq.LockedSetQueueByName(snapQueueName, liveQ)
									if persistErr := queue.Persist(ctx, baseEnv.ProjectDir, liveQ); persistErr != nil {
										fmt.Fprintf(os.Stderr, "daemon: workloop: Persist bead_claim_skipped deferred-for-ledger-dep queueID=%s: %v\n",
											liveQ.QueueID, persistErr)
									}
								}
								lq.Done()
							}
						}
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				afterLookupVerdict, afterLookupErr := orchestrator.AdmitAfterLookup(orchestrator.AdmissionInput{
					Path:             orchestrator.PathQueue,
					BeadID:           string(snapItemBeadID),
					BeadRecordLoaded: preClaimLoaded,
					BeadLabels:       preClaimRecord.Labels,
				})
				if afterLookupErr != nil {
					state.wait()
					return fmt.Errorf("daemon: workloop: after-lookup admission (queue path): %w", afterLookupErr)
				}
				if !afterLookupVerdict.Admitted {
					if afterLookupVerdict.Message != "" {
						fmt.Fprint(os.Stderr, afterLookupVerdict.Message)
					}
					state.tickRefusals[snapItemBeadID] = true
					state.walkingThisTick = true
					continue
				}

				beforeStampVerdict, beforeStampErr := orchestrator.AdmitBeforeStamp(orchestrator.AdmissionInput{
					Path:           orchestrator.PathQueue,
					BeadID:         string(snapItemBeadID),
					GateMax:        gateMax,
					LocalInFlight:  int(handles.LocalInFlight.Load()),
					QueueLocalOnly: capturedQueueLocalOnly,
				})
				if beforeStampErr != nil {
					state.wait()
					return fmt.Errorf("daemon: workloop: before-stamp admission (queue path): %w", beforeStampErr)
				}
				if !beforeStampVerdict.Admitted {
					if beforeStampVerdict.Message != "" {
						fmt.Fprint(os.Stderr, beforeStampVerdict.Message)
					}
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}

				{
					runUUID, uuidErr := uuid.NewV7()
					if uuidErr != nil {
						state.wait()
						return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
					}
					reservedRunID = core.RunID(runUUID)
					runIDReserved = true
					generatedClaimTID, claimTIDErr := handles.TIDGen.Next()
					if claimTIDErr != nil {
						return exitFatal(fmt.Errorf("daemon: workloop: generate claim TransitionID before reservation: %w", claimTIDErr))
					}
					claimTID = generatedClaimTID
					claimTIDReserved = true

					reservation := reserveQueueItem(ctx, queueStore, baseEnv.ProjectDir, queueReservation{
						QueueName:         snapQueueName,
						QueueID:           snapQueueID,
						GroupIndex:        snapGroupIndex,
						ItemIndex:         snapItemIdx,
						BeadID:            snapItemBeadID,
						RunID:             reservedRunID,
						ClaimTransitionID: claimTID,
					})

					collisionSite := crossQueueCollisionSite{
						QueueName:         snapQueueName,
						QueueID:           snapQueueID,
						GroupIndex:        snapGroupIndex,
						ItemIndex:         snapItemIdx,
						BeadID:            snapItemBeadID,
						RunID:             reservedRunID,
						ClaimTransitionID: claimTID,
						Now:               time.Now(),
					}

					switch reservation.Verdict {
					case reservationReserved:
						delete(state.crossQueueCollisions, queuePreClaimAttemptKey{
							queueID:    snapQueueID,
							groupIndex: snapGroupIndex,
							itemIdx:    snapItemIdx,
							beadID:     snapItemBeadID,
						})

					case reservationCrossQueueCollision:
						if resolveCrossQueueCollision(ctx, crossQueueCollisionPorts{
							emitter:      basePorts.Emitter,
							queueStore:   queueStore,
							projectDir:   baseEnv.ProjectDir,
							reap:         state.reapPort,
							collisions:   state.crossQueueCollisions,
							tickRefusals: state.tickRefusals,
							refusedUntil: state.itemRefusedUntil,
						}, collisionSite, reservation.Collision) {
							state.walkingThisTick = true
						}
						continue

					case reservationItemFailed:
						switch reservation.FailureReason {
						case "max_attempts_exceeded":
							fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d — failing queue item (hk-6pspu)\n",
								snapItemBeadID, maxItemAttempts)
						default:
							fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s failed at reservation: %s\n",
								snapItemBeadID, reservation.FailureReason)
						}
						evaluateGroupAdvanceWithOutcome(ctx, state.reapPort, snapQueueName, snapQueueID, snapGroupIndex, snapItemIdx, false, time.Now())
						continue

					case reservationWriteFailed:
						reportQueueWriteError(ctx, dispatchGates, snapQueueName, reservation)
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue

					default: // reservationRetryLater
						if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
							return exitClean()
						}
						continue
					}
				}

				beadRecord = preClaimRecord
				queueItemIndex = snapItemIdx
				capturedQueueName = snapQueueName // NQ-B1: tag the run with its queue
				qID := snapQueueID
				gIdx := snapGroupIndex
				queueIDField = &qID
				queueGroupIdxFd = &gIdx
				capturedExtraContext = snapItemContext              // hk-boiwe
				capturedItemWorkflow = snapItemWorkflow             // raw queue workflow input
				capturedItemTemplateParams = snapItemTemplateParams // hk-55zv2 / WG-045
			}
		}

		var beadID core.BeadID
		if queueItemIndex < 0 {
			if noAutoPull {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			if dispatchGates.operatorPauseCtrl != nil && dispatchGates.operatorPauseCtrl.IsPaused() {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			readyRecords, err := ledger.Ready(ctx)
			if err != nil {
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: Ready poll error (will retry): %v\n", err)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			if len(readyRecords) == 0 {
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			beadRecord = readyRecords[0]

			if state.readyPathAttempts[beadRecord.BeadID] >= maxItemAttempts {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s exceeded maxItemAttempts=%d on br-ready path — skipping (hk-6pspu)\n",
					beadRecord.BeadID, maxItemAttempts)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}

			if dispatchGates.handlerPauseController != nil {
				epoch, isPaused := dispatchGates.handlerPauseController.PausedEpochFor(core.AgentTypeClaudeCode)
				state.lastSeenPauseEpoch = pruneHeldDedupOnEpochChange(dispatchGates, epoch, state.lastSeenPauseEpoch)
				if isPaused {
					emitHeldEvent(ctx, dispatchGates, beadRecord.BeadID, epoch)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
			}

			readyPreLookupVerdict, readyPreLookupErr := orchestrator.AdmitBeforeLookup(orchestrator.AdmissionInput{
				Path:            orchestrator.PathBrReady,
				BeadID:          string(beadRecord.BeadID),
				DecisionBlocked: dispatchGates.decisionBlocker != nil && dispatchGates.decisionBlocker.IsBeadBlocked(beadRecord.BeadID),
				SentinelBlocked: state.maintenance.sentinelBlocksDispatch(dispatchGates),
			})
			if readyPreLookupErr != nil {
				state.wait()
				return fmt.Errorf("daemon: workloop: before-lookup admission (br-ready path): %w", readyPreLookupErr)
			}
			if !readyPreLookupVerdict.Admitted {
				if readyPreLookupVerdict.Message != "" {
					fmt.Fprint(os.Stderr, readyPreLookupVerdict.Message)
				}
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
		}
		beadID = beadRecord.BeadID

		runID := reservedRunID
		if !runIDReserved {
			runUUID, uuidErr := uuid.NewV7()
			if uuidErr != nil {
				state.wait()
				return fmt.Errorf("daemon: workloop: generate RunID: %w", uuidErr)
			}
			runID = core.RunID(runUUID)
		}

		if !claimTIDReserved {
			generatedClaimTID, tidErr := handles.TIDGen.Next()
			if tidErr != nil {
				return exitFatal(fmt.Errorf("daemon: workloop: generate claim TransitionID: %w", tidErr))
			}
			claimTID = generatedClaimTID
		}

		if queueItemIndex < 0 {
			showRecord, showErr := ledger.ShowBead(ctx, beadID)
			if showErr != nil {
				if dispatchCtx.Err() != nil {
					return exitClean()
				}
				state.readyPathAttempts[beadID]++
				if state.readyPathAttempts[beadID] >= maxItemAttempts {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s failed %d times, skipping bead (hk-kupeo): %v\n",
						beadID, state.readyPathAttempts[beadID], showErr)
					if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
						return exitClean()
					}
					continue
				}
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead pre-claim check %s error (attempt %d/%d, will retry): %v\n",
					beadID, state.readyPathAttempts[beadID], maxItemAttempts, showErr)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			if showRecord.Status != core.CoarseStatusOpen {
				fmt.Fprintf(os.Stderr, "daemon: workloop: bead_claim_skipped %s status=%s (competing claim won)\n", beadID, showRecord.Status)
				if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
					return exitClean()
				}
				continue
			}
			beadRecord = showRecord
		}

		select {
		case state.claimSem <- struct{}{}:
		case <-dispatchCtx.Done():
			return exitClean()
		}
		claimErr := ledger.ClaimBead(ctx, baseEnv.IntentLogDir, baseEnv.BrTimeoutCfg, runID, claimTID, beadID)
		<-state.claimSem
		if claimErr != nil {
			if dispatchCtx.Err() != nil {
				return exitClean()
			}

			if queueItemIndex >= 0 && queueStore != nil && queueIDField != nil && queueGroupIdxFd != nil {
				kind := orchestrator.ClaimFailureOther
				if errors.Is(claimErr, brcli.ErrClaimDependencyBlocked) {
					kind = orchestrator.ClaimFailureDependencyBlocked
				}
				showRecord, showErr := ledger.ShowBead(ctx, beadID)
				disposition := orchestrator.DecideClaimFailure(
					true, kind, showRecord.Status, showErr == nil,
				)
				if disposition == orchestrator.ClaimFailureFailQueueItem {
					fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s bead is blocked (deps or status) — failing queue item (hk-n91y0)\n", beadID)
					failed := failQueueItem(ctx, queueStore, baseEnv.ProjectDir, queueReservation{
						QueueName:         capturedQueueName,
						QueueID:           *queueIDField,
						GroupIndex:        *queueGroupIdxFd,
						ItemIndex:         queueItemIndex,
						BeadID:            beadID,
						RunID:             runID,
						ClaimTransitionID: claimTID,
					}, "claim_dependency_refusal", queue.PreclaimTerminalDependencyRefusal)
					if finishErr := finishDependencyRefusal(failed, func() {
						evaluateGroupAdvanceWithOutcome(ctx, state.reapPort, capturedQueueName, *queueIDField, *queueGroupIdxFd, queueItemIndex, false, time.Now())
					}); finishErr != nil {
						return exitFatal(fmt.Errorf("daemon: workloop: %w", finishErr))
					}
					continue
				}
			}

			fmt.Fprintf(os.Stderr, "daemon: workloop: ClaimBead %s error (will retry): %v\n", beadID, claimErr)
			if queueItemIndex < 0 {
				state.readyPathAttempts[beadID]++
			}
			autoCloseStaleBlockersOnClaimFailure(ctx, ledger, baseEnv.ProjectDir, baseEnv.TargetBranch, baseEnv.BrTimeoutCfg, ledgerRepair, beadID)
			if queueItemIndex >= 0 && queueStore != nil && queueGroupIdxFd != nil {
				release := releaseReservation(ctx, queueStore, baseEnv.ProjectDir, queueReservation{
					QueueName:  capturedQueueName,
					GroupIndex: *queueGroupIdxFd,
					ItemIndex:  queueItemIndex,
					BeadID:     beadID,
					RunID:      runID,
				}, claimErr.Error())
				switch release.Verdict {
				case reservationReleased:
				case reservationWriteFailed:
					reportQueueWriteError(ctx, dispatchGates, capturedQueueName, release)
				default:
					fmt.Fprintf(os.Stderr,
						"daemon: workloop: release claim-revert queue=%q bead=%s run=%s verdict=%s: %v — %s\n",
						capturedQueueName, beadID, runID, release.Verdict, release.Err,
						releaseOutcomeAdvice(release.Verdict))
				}
			}
			if sleepErr := workloopSleep(dispatchCtx, workloopPollInterval, queueSurface.submitWakeC); sleepErr != nil {
				return exitClean()
			}
			continue
		}

		if queueItemIndex >= 0 {
			showRecord, showErr := ledger.ShowBead(ctx, beadID)
			if showErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: workloop: ShowBead record refresh %s error (using pre-claim record): %v\n", beadID, showErr)
			} else {
				beadRecord = showRecord
			}
		}

		capturedQueueID := queueIDField
		capturedQueueGroupIdx := queueGroupIdxFd
		capturedItemIndex := queueItemIndex
		capturedCtx := capturedExtraContext // hk-boiwe
		capturedWorkflow := capturedItemWorkflow
		capturedTmplParams := capturedItemTemplateParams // hk-55zv2 / WG-045
		capturedLocalOnly := capturedQueueLocalOnly
		capturedWorkerTarget := capturedQueueWorkerTarget
		capturedDefaultHarness := capturedQueueDefaultHarness

		dispatchedHandle := &runregistry.RunHandle{
			BeadID: beadID,
			// QueueName tags the run with its dispatching queue so the per-queue
			// capacity tally (LenForQueue/LenForQueueLocal) bounds this queue
			// independently of the global ceiling (NQ-B1). Empty for
			// br-ready-fallback runs.
			QueueName: capturedQueueName,
			// hk-mdus1: denormalize the durable queue coordinates so the
			// force-reap watchdog can advance the owning queue item terminal
			// when this run's goroutine wedges and never runs the completion
			// path itself.
			QueueID:         capturedQueueID,
			QueueGroupIndex: capturedQueueGroupIdx,
			QueueItemIndex:  capturedItemIndex,
			Labels:          beadRecord.Labels,
			StartedAt:       time.Now(),
		}

		var preSelectedWorker *workers.Worker
		if !capturedLocalOnly && handles.Workers != nil {
			if capturedWorkerTarget != "" {
				preSelectedWorker = handles.Workers.SelectWorkerByName(capturedWorkerTarget)
			} else {
				preSelectedWorker = handles.Workers.SelectWorker()
			}
		}
		isLocalDispatch := preSelectedWorker == nil
		if isLocalDispatch && handles.LocalInFlight != nil {
			handles.LocalInFlight.Add(1)
		} else if !isLocalDispatch {
			dispatchedHandle.Remote.Store(true)
		}

		env := runEnvWithDispatch(baseEnv, runID, beadRecord, capturedQueueName, capturedQueueID,
			capturedQueueGroupIdx, capturedItemIndex, capturedWorkflow,
			capturedTmplParams, capturedLocalOnly, capturedWorkerTarget, capturedDefaultHarness)
		rp, runHandles := buildRunBundles(basePorts, handles, env, launchBuilder)
		state.runs.Start(ctx, runID, dispatchedHandle, func(runCtx context.Context) bool {
			return beadRunOne(runCtx, env, rp, runHandles, capturedCtx, preSelectedWorker, isLocalDispatch)
		}, func(result runTerminalResult) {
			completeDispatchedBead(ctx, env, state.completionPort, result)
		})
	}
}

func completeDispatchedBead(daemonCtx context.Context, env runloop.RunEnv, completion runCompletionPort,
	result runTerminalResult,
) {
	runOK := result.succeeded
	completionCtx := daemonCtx
	if runOK && daemonCtx.Err() != nil {
		var cancelCompletion context.CancelFunc
		completionCtx, cancelCompletion = context.WithTimeout(
			context.WithoutCancel(daemonCtx), groupCompletionDrainTimeout)
		defer cancelCompletion()
	}
	if env.QueueItemIndex >= 0 && completion.queueStore != nil && env.QueueID != nil && env.QueueGroupIndex != nil && completionCtx.Err() == nil {
		evaluateGroupAdvanceWithOutcome(completionCtx, completion.reapSeamPort, env.QueueName,
			*env.QueueID, *env.QueueGroupIndex, env.QueueItemIndex, runOK, time.Now())
	}
	if runOK && daemonCtx.Err() == nil {
		stagedBeadGeneratorEvalWithPort(daemonCtx, completion, env.BeadRecord.BeadID, env.BeadRecord.Labels)
	}
}

func autoCloseStaleBlockersOnClaimFailure(ctx context.Context, ledger beadLedger, projectDir, targetBranch string, timeout brcli.TimeoutConfig, repair ledgerRepairPort, beadID core.BeadID) {
	if repair.staleBlockerCloser == nil {
		return
	}
	record, err := ledger.ShowBead(ctx, beadID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: autoCloseStaleBlockers ShowBead %s: %v\n", beadID, err)
		return
	}
	if record.Status != core.CoarseStatusBlocked {
		return
	}
	seen := make(map[core.BeadID]struct{})
	for _, edge := range record.Edges {
		if edge.FromBeadID != beadID {
			seen[edge.FromBeadID] = struct{}{}
		}
		if edge.ToBeadID != beadID {
			seen[edge.ToBeadID] = struct{}{}
		}
	}
	for blockerID := range seen {
		if !beadWorkLandedOn(ctx, projectDir, targetBranch, blockerID) {
			continue
		}
		fmt.Fprintf(os.Stderr, "daemon: workloop: claim-failure auto-close stale blocker %s (merged on %s, unblocks %s)\n", blockerID, targetBranch, beadID)
		if closeErr := repair.staleBlockerCloser.SweepCloseBead(ctx, timeout, blockerID); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: SweepCloseBead stale blocker %s: %v\n", blockerID, closeErr)
		}
	}
}

func drainQueuesForRestart(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, emitter runloop.EmitterPort) { //nolint:gocognit // One pass must park every named queue and emit its matching fact.
	if queueStore == nil {
		return
	}
	snapshot := queueStore.AllQueues()
	for name, q := range snapshot {
		if q == nil || q.Status != queue.QueueStatusActive {
			continue
		}
		if err := queue.PauseQueueForRestart(q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: drainQueuesForRestart queueID=%s name=%q: %v\n",
				q.QueueID, name, err)
			continue
		}
		if err := queue.Persist(ctx, projectDir, q); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: drainQueuesForRestart persist queueID=%s name=%q: %v\n",
				q.QueueID, name, err)
			continue
		}
		queueStore.SetQueueByName(name, q)
		if emitter != nil {
			groupIndex := 0
			for _, group := range q.Groups {
				if group.Status == queue.GroupStatusActive {
					groupIndex = group.GroupIndex
					break
				}
			}
			payload, marshalErr := json.Marshal(core.QueuePausedPayload{
				QueueID: q.QueueID, GroupIndex: groupIndex,
				PausedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "operator_drain",
			})
			if marshalErr == nil {
				if emitErr := emitter.Emit(ctx, core.EventTypeQueuePaused, payload); emitErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: workloop: drainQueuesForRestart emit queueID=%s name=%q: %v\n", q.QueueID, name, emitErr)
				}
			}
		}
	}
}

// workloopSleep sleeps for d or until ctx is cancelled. Returns a non-nil
// error only when ctx is cancelled. wakeC may be nil: receive from a nil
// channel blocks forever, so the nil case is never selected and the function
// degrades to a plain timer sleep. Bead ref: hk-24xn1 (wakeC parameter).
//
//nolint:unparam // pre-existing: Seam A moved this code out of workloop.go unchanged
func workloopSleep(ctx context.Context, d time.Duration, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	case <-wakeC:
		return nil
	}
}

func workloopIdleWait(ctx context.Context, wakeC <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wakeC:
		return nil
	}
}

func scheduleAwareIdleWait(ctx context.Context, schedule schedulePort, submitWakeC <-chan struct{}) error {
	if schedule.store == nil || !hasEnabledScheduledJob(schedule.store) {
		return workloopIdleWait(ctx, submitWakeC)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(workloopPollInterval):
		return nil
	case <-submitWakeC:
		return nil
	case <-schedule.wakeC:
		return nil
	}
}

func hasEnabledScheduledJob(s *schedule.Store) bool {
	for _, j := range s.List() {
		if j.Enabled {
			return true
		}
	}
	return false
}

func activateFirstPendingGroupLocked(ctx context.Context, projectDir string, lq *queuewiring.LockedQueueStore, q *queue.Queue) (bool, []queue.EventIntent) {
	if q == nil {
		return false, nil
	}
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusActive {
			return false, nil
		}
	}
	groupPos := -1
	for i := range q.Groups {
		if q.Groups[i].Status == queue.GroupStatusPending {
			groupPos = i
			break
		}
	}
	if groupPos < 0 {
		return false, nil
	}

	newStatus, events, advErr := queue.AdvanceGroup(ctx, &q.Groups[groupPos], q.Status, q.QueueID, time.Now())
	if advErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked AdvanceGroup queueID=%s groupIndex=%d: %v\n",
			q.QueueID, q.Groups[groupPos].GroupIndex, advErr)
		return false, nil
	}
	if newStatus != queue.GroupStatusActive {
		return false, nil
	}

	q.Groups[groupPos].Status = newStatus
	if err := queue.Persist(ctx, projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: activateFirstPendingGroupLocked Persist queueID=%s: %v\n",
			q.QueueID, err)
		events = nil // describe only durable state
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(q.Name), q)

	return true, events
}

func reevaluateDeferredQueues(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, queueSurface queueSurfacePort, dispatchGates dispatchGatesPort) bool {
	if queueStore == nil {
		return false
	}
	anyDeferred := false
	for name, loaded := range queueStore.AllQueues() {
		observed := loaded
		if queueSurface.queueLedger != nil && hasDeferredItem(loaded) {
			if post := commitDeferredReevaluation(ctx, queueStore, projectDir, queueSurface, dispatchGates, name); post != nil {
				observed = post
			}
		}
		if hasDeferredItem(observed) {
			anyDeferred = true
		}
	}
	return anyDeferred
}

func commitDeferredReevaluation(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, queueSurface queueSurfacePort, dispatchGates dispatchGatesPort, name string) *queue.Queue {
	snapshot := queueStore.Snapshot(name)
	if snapshot.Queue == nil {
		return nil
	}
	result := queueStore.Transact(ctx, queuewiring.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationMaintenance,
		Mutate: func(q *queue.Queue) error {
			g := firstActiveGroup(q)
			if g == nil {
				return nil
			}
			_, err := queue.ReevaluateDeferred(ctx, g, queueSurface.queueLedger)
			return err
		},
	})

	switch {
	case result.Committed():
		return result.Snapshot.Queue

	case errors.Is(result.Err, queuewiring.ErrQueueQuarantined):
		reportQueueWriteError(ctx, dispatchGates, name, reservationResult{Outcome: result.Outcome, Err: result.Err})
		return nil

	case result.Outcome == queue.OutcomeRejected:
		fmt.Fprintf(os.Stderr, "daemon: workloop: re-evaluate deferred items queue=%q: %v\n", name, result.Err)
		return nil

	default:
		reportQueueWriteError(ctx, dispatchGates, name, reservationResult{Outcome: result.Outcome, Err: result.Err})
		return nil
	}
}

func firstActiveGroup(q *queue.Queue) *queue.Group {
	if q == nil || q.Status != queue.QueueStatusActive {
		return nil
	}
	for gi := range q.Groups {
		if q.Groups[gi].Status == queue.GroupStatusActive {
			return &q.Groups[gi]
		}
	}
	return nil
}

func hasDeferredItem(q *queue.Queue) bool {
	g := firstActiveGroup(q)
	if g == nil {
		return false
	}
	for i := range g.Items {
		if g.Items[i].Status == queue.ItemStatusDeferredForLedgerDep {
			return true
		}
	}
	return false
}

func markQueueItemFailureReason(_ context.Context, queueStore *queuewiring.QueueStore, queueName string, groupIndex, itemIdx int, beadID core.BeadID, reason string) {
	if queueStore == nil {
		return
	}
	lq := queueStore.LockForMutation()
	defer lq.Done()
	q := lq.LockedQueueByName(queue.NormaliseQueueName(queueName))
	if q == nil {
		return
	}
	for gi := range q.Groups {
		if q.Groups[gi].GroupIndex != groupIndex {
			continue
		}
		if itemIdx < len(q.Groups[gi].Items) && q.Groups[gi].Items[itemIdx].BeadID == beadID {
			q.Groups[gi].Items[itemIdx].LastFailureReason = reason
		}
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(queueName), q)
}

func propagateFailedDependents(ctx context.Context, port reapSeamPort, completionQueue *queue.Queue, queueID string, groupIndex int, itemIdx int) bool {
	for i := range completionQueue.Groups {
		if completionQueue.Groups[i].GroupIndex != groupIndex || itemIdx >= len(completionQueue.Groups[i].Items) {
			continue
		}
		failedBead := completionQueue.Groups[i].Items[itemIdx].BeadID
		if _, propagateErr := queue.FailDeferredDependents(ctx, &completionQueue.Groups[i], failedBead, port.queueLedger); propagateErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: propagate failed dependency queueID=%s groupIndex=%d: %v\n", queueID, groupIndex, propagateErr)
			return false
		}
	}
	return true
}

const groupCompletionRetryBudget = 3

type groupCompletionVerdict string

const (
	groupCompletionSettled groupCompletionVerdict = "settled"

	groupCompletionContended groupCompletionVerdict = "contended"
)

type groupCompletion struct {
	// QueueName is the NORMALISED name of the queue the run was dispatched
	// from (NQ-B1), captured at dispatch time. The completion path MUST resolve
	// the queue by name — the main-only lq.Queue() shim would, for a non-"main"
	// queue, return the wrong queue (or nil), trip the QueueID guard, and return
	// early WITHOUT marking the item terminal, stalling that queue's group
	// forever (hk-tigaf.4).
	QueueName   string
	QueueID     string
	GroupIndex  int
	ItemIndex   int
	Success     bool
	CompletedAt time.Time
}

func evaluateGroupAdvanceWithOutcome(ctx context.Context, port reapSeamPort, queueName string, queueID string, groupIndex int, itemIdx int, success bool, completedAt time.Time) {
	if port.queueStore == nil {
		return
	}
	completion := groupCompletion{
		QueueName:   queue.NormaliseQueueName(queueName),
		QueueID:     queueID,
		GroupIndex:  groupIndex,
		ItemIndex:   itemIdx,
		Success:     success,
		CompletedAt: completedAt,
	}
	evaluateGroupAdvanceFrom(ctx, port, port.queueStore.Snapshot(completion.QueueName), completion)
}

func evaluateGroupAdvanceFrom(ctx context.Context, port reapSeamPort, snapshot queuewiring.Snapshot, completion groupCompletion) {
	for attempt := 0; attempt < groupCompletionRetryBudget; attempt++ {
		if groupCompletionAttempt(ctx, port, snapshot, completion) == groupCompletionSettled {
			return
		}
		snapshot = port.queueStore.Snapshot(completion.QueueName)
	}

	fmt.Fprintf(os.Stderr,
		"daemon: workloop: GROUP COMPLETION STRANDED — queue %q group %d item %d lost the snapshot race on all %d "+
			"attempts, so its outcome was never recorded. The item is still dispatched, its group cannot reach "+
			"all-terminal, and nothing re-selects a dispatched item, so this queue does not advance until the next "+
			"daemon start reconciles it.\n",
		completion.QueueName, completion.GroupIndex, completion.ItemIndex, groupCompletionRetryBudget)
	eagerRefillEval(ctx, port)
}

func groupCompletionAttempt(ctx context.Context, port reapSeamPort, snapshot queuewiring.Snapshot, completion groupCompletion) groupCompletionVerdict {
	if snapshot.Queue == nil {
		eagerRefillEval(ctx, port)
		return groupCompletionSettled
	}
	outcome := queue.GroupCompletionOutcomeFailed
	if completion.Success {
		outcome = queue.GroupCompletionOutcomeCompleted
	}
	input := queue.GroupCompletionInput{
		ExpectedQueueID: completion.QueueID,
		Location:        queue.GroupCompletionLocation{GroupIndex: completion.GroupIndex, ItemIndex: completion.ItemIndex},
		Outcome:         outcome,
		CompletedAt:     completion.CompletedAt,
	}
	completionQueue := queue.CloneQueue(snapshot.Queue)
	if !completion.Success && port.queueLedger != nil {
		if !propagateFailedDependents(ctx, port, completionQueue, completion.QueueID, completion.GroupIndex, completion.ItemIndex) {
			eagerRefillEval(ctx, port)
			return groupCompletionSettled
		}
	}
	decision, err := queue.DecideGroupCompletion(*completionQueue, input) //nolint:contextcheck // The value-only decision checks cancellation inside queue.AdvanceGroup.
	if err == nil && decision.Disposition == queue.GroupCompletionDispositionReceiptRequired {
		input.CompletionReceiptID, err = newGroupCompletionID()
		if err == nil {
			decision, err = queue.DecideGroupCompletion(*completionQueue, input) //nolint:contextcheck // The value-only decision checks cancellation inside queue.AdvanceGroup.
		}
	}
	if err == nil && decision.Disposition == queue.GroupCompletionDispositionReceiptRequired {
		err = errors.New("group completion still requires a receipt after retry")
	}
	if err == nil {
		err = decision.Validate()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: decide group completion queueID=%s groupIndex=%d: %v\n", completion.QueueID, completion.GroupIndex, err)
		eagerRefillEval(ctx, port)
		return groupCompletionSettled
	}
	execution := executeGroupCompletion(ctx, port, snapshot, decision, input)
	if lostGroupCompletionSnapshotRace(execution) {
		return groupCompletionContended
	}
	effects, policyErr := decideGroupCompletionEffects(execution.durability)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: group completion policy queueID=%s groupIndex=%d: %v\n", completion.QueueID, completion.GroupIndex, policyErr)
		eagerRefillEval(ctx, port)
		return groupCompletionSettled
	}
	applyGroupCompletionEffects(ctx, port, decision.Intents, effects, execution.err)
	return groupCompletionSettled
}

func lostGroupCompletionSnapshotRace(execution groupCompletionExecution) bool {
	return execution.durability.Outcome == queue.OutcomeRejected &&
		errors.Is(execution.err, queuewiring.ErrStaleSnapshot)
}

func extractTmuxAdapterFromSubstrate(sub handler.Substrate) tmuxpkg.Adapter {
	if sa, ok := sub.(substrateWithAdapter); ok {
		return sa.tmuxAdapter()
	}
	return nil
}

func adoptLiveRunSession(ctx context.Context, ledger beadLedger, env runloop.RunEnv, queueStore *queuewiring.QueueStore, tidGen runloop.TransitionIDSource, rec runpkg.Record, adapter tmuxpkg.Adapter) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // daemon shutting down; leave for next boot's adoption pass
		case <-ticker.C:
		}
		sessions, listErr := adapter.ListSessions(ctx)
		if listErr != nil {
			continue // transient error; retry on next tick
		}
		found := false
		for _, s := range sessions {
			if s == rec.SessionName {
				found = true
				break
			}
		}
		if !found {
			break // session gone — Claude has exited
		}
	}

	if ctx.Err() != nil {
		return
	}

	bgCtx := context.Background()
	runUUID, parseErr := uuid.Parse(rec.RunID)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: adoptLiveRunSession: parse runID %q: %v — bead %s is not reopened and its queue item is not released\n",
			rec.RunID, parseErr, rec.BeadID)
	} else {
		adoptRunID := core.RunID(runUUID)
		reopenTID, _ := tidGen.Next()                                                                                                                                                //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
		if reopenErr := ledger.ReopenBead(bgCtx, env.IntentLogDir, env.BrTimeoutCfg, adoptRunID, reopenTID, core.BeadID(rec.BeadID), "run_session_adopted_dead"); reopenErr != nil { //nolint:contextcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
			fmt.Fprintf(os.Stderr, "daemon: adoptLiveRunSession: ReopenBead %s: %v\n", rec.BeadID, reopenErr)
		}
		releaseAdoptedRunItem(bgCtx, queueStore, env.ProjectDir, rec, adoptRunID) //nolint:contextcheck // the daemon context is cancelled by the time this runs; the release must still reach disk
	}

	if env.ProjectDir != "" {
		_ = runpkg.Remove(env.ProjectDir, rec.RunID) //nolint:errcheck // pre-existing: Seam A moved this code out of workloop.go unchanged
	}
}

func releaseAdoptedRunItem(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, rec runpkg.Record, runID core.RunID) {
	if queueStore == nil || rec.QueueName == "" || rec.QueueID == "" || rec.GroupIndex < 0 || rec.ItemIndex < 0 {
		return
	}
	queueName := queue.NormaliseQueueName(rec.QueueName)
	release := releaseReservation(ctx, queueStore, projectDir, queueReservation{
		QueueName:  queueName,
		GroupIndex: rec.GroupIndex,
		ItemIndex:  rec.ItemIndex,
		BeadID:     core.BeadID(rec.BeadID),
		RunID:      runID,
	}, "run_session_adopted_dead")

	if report := adoptedReleaseReport(queueName, rec.BeadID, runID, release); report != "" {
		fmt.Fprintln(os.Stderr, report)
		return
	}

	queueStore.Wake()
}

func adoptedReleaseReport(queueName, beadID string, runID core.RunID, release reservationResult) string {
	switch release.Verdict {
	case reservationReleased:
		return ""

	case reservationWriteFailed:
		return fmt.Sprintf(
			"daemon: adoptLiveRunSession: QUEUE WRITE FAILED releasing queue=%q bead=%s run=%s outcome=%s: %v — "+
				"queue %q now refuses further writes and the daemon is degraded. Check free disk space and the "+
				".harmonik/queues directory, then restart the daemon.",
			queueName, beadID, runID, release.Outcome, release.Err, queueName)

	default:
		return fmt.Sprintf(
			"daemon: adoptLiveRunSession: release adopted-dead queue=%q bead=%s run=%s verdict=%s: %v — %s",
			queueName, beadID, runID, release.Verdict, release.Err,
			releaseOutcomeAdvice(release.Verdict))
	}
}

func legacyRunSessionsForAdoption(projectDir string) ([]runpkg.Record, error) {
	registry, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		return nil, err
	}
	return registry.Legacy, nil
}

func strandedBeadHasOnDiskRun(projectDir string, beadID core.BeadID) bool {
	if projectDir == "" {
		return false
	}
	registry, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		return true
	}
	for _, r := range registry.Legacy {
		if r.BeadID == string(beadID) {
			return true
		}
	}
	for _, r := range registry.Dispatch {
		if r.BeadID == beadID {
			return true
		}
	}
	return false
}
