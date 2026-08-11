package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/queue"
)

type groupCompletionExecution struct {
	durability groupCompletionDurability
	err        error
}

func executeGroupCompletion(
	ctx context.Context,
	port reapSeamPort,
	snapshot queue.QueueSnapshot,
	decision queue.GroupCompletionResult,
	input queue.GroupCompletionInput,
) groupCompletionExecution {
	if decision.Disposition == queue.GroupCompletionDispositionNoChange {
		return groupCompletionExecution{durability: groupCompletionDurability{Disposition: decision.Disposition}}
	}
	if decision.Disposition == queue.GroupCompletionDispositionQueueCompleted {
		return executeFinalGroupCompletion(ctx, port, snapshot, decision, input)
	}
	transactionID, err := newGroupCompletionID()
	if err != nil {
		return failedGroupCompletionExecution(decision.Disposition, err)
	}
	result := port.queueStore.Transact(ctx, queue.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    port.projectDir,
		TransactionID: transactionID,
		OperationKind: queue.OperationAdvance,
		Mutate: func(candidate *queue.Queue) error {
			*candidate = *queue.CloneQueue(decision.NextQueue)
			return nil
		},
	})
	return groupCompletionExecution{
		durability: groupCompletionDurability{
			Disposition:  decision.Disposition,
			Outcome:      result.Outcome,
			CleanupError: result.CleanupErr != nil,
		},
		err: errors.Join(result.Err, result.CleanupErr),
	}
}

func executeFinalGroupCompletion(
	ctx context.Context,
	port reapSeamPort,
	snapshot queue.QueueSnapshot,
	decision queue.GroupCompletionResult,
	input queue.GroupCompletionInput,
) groupCompletionExecution {
	transactionID, err := newGroupCompletionID()
	if err != nil {
		return failedGroupCompletionExecution(decision.Disposition, err)
	}
	store := port.completionStore
	if store == nil {
		store = port.queueStore
	}
	result := store.Complete(ctx, queue.CompletionRequest{
		Snapshot:      snapshot,
		Candidate:     decision.NextQueue,
		DecisionInput: input,
		ProjectDir:    port.projectDir,
		TransactionID: transactionID,
		ReceiptID:     input.CompletionReceiptID,
		CompletedAt:   input.CompletedAt,
		ReleaseTime:   time.Now,
		Observe: func(queue.CompletionReceipt) error {
			var observedErr error
			for _, intent := range decision.Intents {
				observedErr = errors.Join(observedErr, port.bus.Emit(ctx, intent.Type, intent.Payload))
			}
			return observedErr
		},
	})
	return groupCompletionExecution{
		durability: groupCompletionDurability{
			Disposition:      decision.Disposition,
			Outcome:          result.Outcome,
			Phase:            result.Phase,
			ObservationError: result.ObservationErr != nil,
			CleanupError:     result.CleanupErr != nil,
			MarkerError:      result.MarkerErr != nil,
		},
		err: errors.Join(result.Err, result.ObservationErr, result.CleanupErr, result.MarkerErr),
	}
}

func failedGroupCompletionExecution(disposition queue.GroupCompletionDisposition, err error) groupCompletionExecution {
	phase := queue.CompletionPhase("")
	if disposition == queue.GroupCompletionDispositionQueueCompleted {
		phase = queue.CompletionPhaseRejected
	}
	return groupCompletionExecution{
		durability: groupCompletionDurability{Disposition: disposition, Outcome: queue.OutcomeRejected, Phase: phase},
		err:        err,
	}
}

func applyGroupCompletionEffects(
	ctx context.Context,
	port reapSeamPort,
	intents []queue.EventIntent,
	effects groupCompletionEffects,
	diagnostic error,
) {
	performGroupCompletionEffects(intents, effects, diagnostic, groupCompletionEffectSink{
		log:         func(label string, err error) { fmt.Fprintf(os.Stderr, "daemon: workloop: %s: %v\n", label, err) },
		emit:        func(intent queue.EventIntent) error { return port.bus.Emit(ctx, intent.Type, intent.Payload) },
		wake:        port.queueStore.Wake,
		cancelDrain: port.cancelOnQueueDrain,
		cancelExit:  port.cancelOnQueueExit,
		refill:      func() { eagerRefillEval(ctx, port) },
	})
}

type groupCompletionEffectSink struct {
	log         func(string, error)
	emit        func(queue.EventIntent) error
	wake        func()
	cancelDrain func()
	cancelExit  func()
	refill      func()
}

func performGroupCompletionEffects(intents []queue.EventIntent, effects groupCompletionEffects, diagnostic error, sink groupCompletionEffectSink) {
	if effects.LogFailure && diagnostic != nil && sink.log != nil {
		sink.log("group completion durability", diagnostic)
	}
	performGroupCompletionEmits(intents, effects.EmitIntents, sink)
	if effects.Wake && sink.wake != nil {
		sink.wake()
	}
	if effects.CancelQueueDrain && sink.cancelDrain != nil {
		sink.cancelDrain()
	}
	if effects.CancelQueueExit && sink.cancelExit != nil {
		sink.cancelExit()
	}
	if effects.Refill && sink.refill != nil {
		sink.refill()
	}
}

func performGroupCompletionEmits(intents []queue.EventIntent, enabled bool, sink groupCompletionEffectSink) {
	if !enabled || sink.emit == nil {
		return
	}
	var emitErr error
	for _, intent := range intents {
		emitErr = errors.Join(emitErr, sink.emit(intent))
	}
	if emitErr != nil && sink.log != nil {
		sink.log("emit group completion intents", emitErr)
	}
}

func newGroupCompletionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint group completion ID: %w", err)
	}
	return id.String(), nil
}
