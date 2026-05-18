package daemon

// workloop_handlerpause_kac8g.go — dispatcher skip-on-paused gate helpers (hk-kac8g).
//
// Owns emitHeldEvent: emits queue_item_held_for_handler_pause at-most-once per
// (bead_id, paused_epoch) pair per event-model.md §8.11.3 dedup contract.
//
// The dispatch-time gate itself is inlined in workloop.go alongside the queue-pull
// and br-ready paths it guards, for readability.
//
// Spec ref: docs/components/internal/handler-pause-and-resume.md §4.
// Event ref: specs/event-model.md §8.11.3.
// Bead ref: hk-kac8g.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/core"
)

// heldDedupKey returns the dedup map key for a (bead_id, paused_epoch) pair.
func heldDedupKey(beadID core.BeadID, epoch int) string {
	return fmt.Sprintf("%s:%d", string(beadID), epoch)
}

// emitHeldEvent emits a queue_item_held_for_handler_pause event for
// (beadID, agentType, epoch), subject to the at-most-once-per-(bead_id, epoch)
// dedup contract from event-model.md §8.11.3.
//
// The function is a no-op when:
//   - deps.bus is nil.
//   - The (beadID, epoch) pair is already in deps.heldEventDedup (dedup hit).
//
// On dedup miss: emit the event and record the key so future calls are suppressed.
// Emit failures are non-fatal: logged to stderr; dedup key NOT recorded (retried
// next tick).
//
// MUST be called only from the outer poll loop goroutine (single-threaded access
// to deps.heldEventDedup — no locking needed).
//
// Durability class: O (ordinary — reconstructible; low frequency per dedup).
// Bead ref: hk-kac8g.
func emitHeldEvent(ctx context.Context, deps workLoopDeps, beadID core.BeadID, agentType core.AgentType, epoch int) {
	if deps.bus == nil {
		return
	}
	if deps.heldEventDedup != nil {
		key := heldDedupKey(beadID, epoch)
		if _, seen := deps.heldEventDedup[key]; seen {
			return // dedup hit
		}
	}

	payload := core.QueueItemHeldForHandlerPausePayload{
		BeadID:      string(beadID),
		AgentType:   agentType,
		PausedEpoch: epoch,
	}
	if !payload.Valid() {
		fmt.Fprintf(os.Stderr,
			"daemon: workloop: emitHeldEvent: invalid payload bead=%s agent=%s epoch=%d (skipping)\n",
			string(beadID), string(agentType), epoch)
		return
	}

	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emitHeldEvent: marshal: %v\n", marshalErr)
		return
	}
	if emitErr := deps.bus.Emit(ctx, core.EventTypeQueueItemHeldForHandlerPause, payloadJSON); emitErr != nil {
		// Non-fatal: don't record dedup key so next tick retries.
		fmt.Fprintf(os.Stderr, "daemon: workloop: emitHeldEvent: emit: %v\n", emitErr)
		return
	}

	// Record dedup key only after successful emit.
	if deps.heldEventDedup != nil {
		deps.heldEventDedup[heldDedupKey(beadID, epoch)] = struct{}{}
	}
}
