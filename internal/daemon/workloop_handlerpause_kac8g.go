package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/core"
)

func heldDedupKey(beadID core.BeadID, epoch int) string {
	return fmt.Sprintf("%s:%d", string(beadID), epoch)
}

func emitHeldEvent(ctx context.Context, gates dispatchGatesPort, beadID core.BeadID, epoch int) {
	if gates.bus == nil {
		return
	}
	key := heldDedupKey(beadID, epoch)
	if _, seen := gates.heldEventDedup[key]; seen {
		return // dedup hit
	}

	payload := core.QueueItemHeldForHandlerPausePayload{
		BeadID:      string(beadID),
		AgentType:   core.AgentTypeClaudeCode,
		PausedEpoch: epoch,
	}
	if !payload.Valid() {
		fmt.Fprintf(os.Stderr,
			"daemon: workloop: emitHeldEvent: invalid payload bead=%s agent=%s epoch=%d (skipping)\n",
			string(beadID), core.AgentTypeClaudeCode, epoch)
		return
	}

	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emitHeldEvent: marshal: %v\n", marshalErr)
		return
	}
	if emitErr := gates.bus.Emit(ctx, core.EventTypeQueueItemHeldForHandlerPause, payloadJSON); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: emitHeldEvent: emit: %v\n", emitErr)
		return
	}

	gates.heldEventDedup[key] = struct{}{}
}

func pruneHeldDedupOnEpochChange(gates dispatchGatesPort, epoch, lastSeenEpoch int) int {
	if epoch != lastSeenEpoch {
		clear(gates.heldEventDedup)
	}
	return epoch
}
