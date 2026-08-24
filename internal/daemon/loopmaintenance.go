package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
)

const periodicCoordinatorReapInterval = 5 * time.Minute

type coordinatorReapPort struct {
	projectDir  string
	projectHash core.ProjectHash
	adapter     ltmux.Adapter
	interval    time.Duration
}

type loopMaintenanceState struct {
	// lastCoordinatorReap records when the periodic coordinator reaper last ran.
	// Zero → the first tick always fires (hk-t08m).
	lastCoordinatorReap time.Time

	// lastDiskCheck records when the periodic disk free-space probe last ran.
	// Zero → the first tick fires after diskCheckInterval elapses (hk-sxlb).
	lastDiskCheck time.Time

	// diskLow is true when the most recent disk probe found available space below
	// diskLowWatermarkDefault. The dispatch loop skips bead claiming while this
	// flag is set (hk-sxlb).
	diskLow bool
}

type maintenanceObservation struct {
	// halt asks runWorkLoop to drain in-flight runs and exit cleanly. Set only
	// by tickBeforeDispatch, and only for a governor halt that was ARMED on an
	// EARLIER tick — see tickBeforeSelect for why the arming tick does not
	// report it.
	halt bool

	// diskLow is true when the most recent disk probe found free space below the
	// watermark (hk-sxlb). The loop skips bead claiming for this tick.
	diskLow bool

	// blockedQueues is the dashboard forcing-gate verdict (hk-xg6rw): queue names
	// present with value true are captain-curated queues withheld from NEW item
	// dispatch this tick. Nil disables the gate, which is also what an absent
	// subsystem returns.
	blockedQueues map[string]bool
}

type loopMaintenance struct {
	// lifecycle supplies queue terminal cancels to the periodic eager-refill path.
	lifecycle loopLifecyclePort

	// state is the periodic-maintenance timing and latch state (RSM-011).
	state loopMaintenanceState

	// coordinatorReap holds the periodic coordinator-session reaper inputs.
	coordinatorReap coordinatorReapPort

	// diskReclaim holds the disk probe and reactive reclaim inputs. It is also
	// passed to the dispatch registration seam so both use one cache-reap lock.
	diskReclaim diskReclaimPort

	// eagerRefill holds the eager-refill and staged-follow-up inputs.
	eagerRefill eagerRefillPort

	// schedule holds the recurring-job path inputs. It has no event bus and no
	// queue wake channel. Those values stay with their owning surfaces.
	schedule schedulePort

	capacity      capacityPort
	queueSurface  queueSurfacePort
	dispatchGates dispatchGatesPort

	// dashGate is the dashboard staleness forcing gate, or nil when
	// `subsystems.dashboard_gate.enabled: false`. Every method tolerates nil.
	dashGate *dashboardGate

	// governor is the sentinel movement governor, or nil when
	// `subsystems.movement_governor.enabled: false`. Every method tolerates nil.
	governor *movementGovernor

	logW io.Writer
}

func newLoopMaintenance(projectCfg projectconfig.ProjectConfig, collaborators loopCollaborators, logW io.Writer) *loopMaintenance {
	return &loopMaintenance{
		lifecycle:       collaborators.lifecycle,
		coordinatorReap: collaborators.coordinatorReap,
		diskReclaim:     collaborators.diskReclaim,
		eagerRefill:     collaborators.eagerRefill,
		schedule:        collaborators.schedule,
		capacity:        collaborators.capacity,
		queueSurface:    collaborators.queueSurface,
		dispatchGates:   collaborators.dispatchGates,
		dashGate:        newDashboardGateIfEnabled(projectCfg, logW),
		governor:        newMovementGovernorIfEnabled(collaborators.governor, collaborators.governorEnabled, logW),
		logW:            logW,
	}
}

func (m *loopMaintenance) tickBeforeDispatch(ctx context.Context) maintenanceObservation {
	if m.governor.halted() {
		return maintenanceObservation{halt: true}
	}

	runScheduleTick(ctx, m.schedule)

	m.reapCoordinatorSessions(ctx)

	runPeriodicDiskCheck(ctx, m.diskReclaim, &m.state)
	m.runCompletionReceiptGC()

	return maintenanceObservation{diskLow: m.state.diskLow}
}

func (m *loopMaintenance) runCompletionReceiptGC() {
	if m.queueSurface.completionGC == nil {
		return
	}
	results, err := m.queueSurface.completionGC.GarbageCollectCompletionReceipts(
		m.queueSurface.projectDir,
		queue.CompletionGCObservation{},
	)
	if err != nil {
		m.logCompletionGCFault("", "", "", err)
		return
	}
	for _, result := range results {
		if result.Err != nil {
			m.logCompletionGCFault(result.QueueID, result.ReceiptID, string(result.Phase), result.Err)
		}
	}
}

func (m *loopMaintenance) logCompletionGCFault(queueID, receiptID, phase string, err error) {
	w := m.logW
	if w == nil {
		w = os.Stderr
	}
	if _, writeErr := fmt.Fprintf(w, "queue: completion receipt GC failed queue_id=%s receipt_id=%s phase=%s error=%v\n",
		queueID, receiptID, phase, err); writeErr != nil {
		return
	}
}

func (m *loopMaintenance) reapCoordinatorSessions(ctx context.Context) {
	interval := m.coordinatorReap.interval
	if interval <= 0 {
		interval = periodicCoordinatorReapInterval
	}
	if m.coordinatorReap.adapter != nil && time.Since(m.state.lastCoordinatorReap) >= interval {
		runPeriodicCoordinatorReap(ctx, m.coordinatorReap.projectDir, m.coordinatorReap.projectHash, m.coordinatorReap.adapter, nil)
		m.state.lastCoordinatorReap = time.Now()
	}
}

func (m *loopMaintenance) tickBeforeSelect(ctx context.Context, projectDir string, bus handlercontract.EventEmitter, reapPort reapSeamPort, governorInput governorInputPort, now time.Time) maintenanceObservation {
	m.dashGate.tick(ctx, projectDir, bus, now)

	eagerRefillEval(ctx, reapPort)

	m.governor.tick(ctx, governorInput, m.schedule, m.dispatchGates)

	return maintenanceObservation{blockedQueues: m.dashGate.blockedQueueSet()}
}

func (m *loopMaintenance) sentinelBlocksDispatch(dispatchGates dispatchGatesPort) bool {
	return m.governor.dispatchBlocked(dispatchGates)
}
