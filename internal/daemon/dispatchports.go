package daemon

import (
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queue"
)

type capacityPort struct {
	maxConcurrent   int
	concurrencyCtrl *ConcurrencyController
}

func newCapacityPort(maxConcurrent int, concurrencyCtrl *ConcurrencyController) capacityPort {
	return capacityPort{maxConcurrent: maxConcurrent, concurrencyCtrl: concurrencyCtrl}
}

type queueSurfacePort struct {
	submitWakeC  <-chan struct{}
	queueLedger  queue.BeadLedger
	completionGC completionGCStore
	projectDir   string
}

type completionGCStore interface {
	GarbageCollectCompletionReceipts(string, queue.CompletionGCObservation) ([]queue.CompletionGCResult, error)
}

func newQueueSurfacePort(submitWakeC <-chan struct{}, queueLedger queue.BeadLedger) queueSurfacePort {
	return queueSurfacePort{submitWakeC: submitWakeC, queueLedger: queueLedger}
}

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
