package daemon

import (
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queue"
)

// capacityPort holds the live dispatch ceiling. It is built once at boot and
// shared with every path that admits or reaps work.
type capacityPort struct {
	maxConcurrent   int
	concurrencyCtrl *ConcurrencyController
}

// newCapacityPort keeps the static ceiling and its live override together.
func newCapacityPort(maxConcurrent int, concurrencyCtrl *ConcurrencyController) capacityPort {
	return capacityPort{maxConcurrent: maxConcurrent, concurrencyCtrl: concurrencyCtrl}
}

// queueSurfacePort holds the queue-specific inputs of the dispatch loop. The
// QueueStore remains a shared run handle on workLoopDeps.
type queueSurfacePort struct {
	submitWakeC <-chan struct{}
	queueLedger queue.BeadLedger
}

func newQueueSurfacePort(submitWakeC <-chan struct{}, queueLedger queue.BeadLedger) queueSurfacePort {
	return queueSurfacePort{submitWakeC: submitWakeC, queueLedger: queueLedger}
}

// dispatchGatesPort owns the controllers and loop-local dedup state used by
// dispatch admission. Maps are always allocated because only the loop touches
// them.
type dispatchGatesPort struct {
	bus                     handlercontract.EventEmitter
	handlerPauseController  *HandlerPauseController
	heldEventDedup          map[string]struct{}
	queueWriteErrorReported map[string]struct{}
	operatorPauseCtrl       *OperatorPauseController
	decisionBlocker         *DecisionBlocker
}

func newDispatchGatesPort(bus handlercontract.EventEmitter, handlerPauseController *HandlerPauseController, operatorPauseCtrl *OperatorPauseController, decisionBlocker *DecisionBlocker) dispatchGatesPort {
	return dispatchGatesPort{
		bus:                     bus,
		handlerPauseController:  handlerPauseController,
		heldEventDedup:          make(map[string]struct{}),
		queueWriteErrorReported: make(map[string]struct{}),
		operatorPauseCtrl:       operatorPauseCtrl,
		decisionBlocker:         decisionBlocker,
	}
}

// newDispatchGatesPortFromDeps is the test-export bridge for callers that
// still construct workLoopDeps directly. Production builds the port at boot.
func newDispatchGatesPortFromDeps(deps workLoopDeps) dispatchGatesPort {
	port := deps.testDispatchGates
	if port.heldEventDedup == nil {
		port.heldEventDedup = make(map[string]struct{})
	}
	if port.queueWriteErrorReported == nil {
		port.queueWriteErrorReported = make(map[string]struct{})
	}
	return port
}
